package stremio

import (
	"context"
	"fmt"
	"strings"

	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

// Refreshing an already-imported item's candidate releases.
//
// Candidate sets go stale on their own: a better encode appears next week, a
// release stops being cached, an aggregator's scrapers disagree from one minute
// to the next. Re-importing is how a user asks for the current answer.
//
// It is deliberately **additive**. A release absent from today's listing has
// usually not gone anywhere — the source simply did not return it this time —
// and a stored candidate costs nothing to keep, while removing one risks
// deleting the release someone is part-way through. Selection already skips
// candidates that fail to resolve, so a dead entry is inert rather than
// harmful.

// refreshCandidates adds any releases the source now offers that are not already
// stored, leaving the containment tree alone.
//
// It only walks one level: a work's direct item children. That covers a film,
// and deliberately not a series' full season tree — refreshing every episode of
// a long-running show means one addon round trip per episode, which is not
// something to do behind a single click without asking.
func (c *Capability) refreshCandidates(ctx context.Context, client *Client, svc v1.ContentService, caller v1.Caller, workID v1.NodeID, typ, id string, result *v1.ImportResult) error {
	work, err := svc.GetContentNode(ctx, v1.GetContentNodeQuery{Caller: caller, NodeID: workID, WithChildren: true})
	if err != nil {
		return fmt.Errorf("read work %s: %w", workID, err)
	}

	for _, child := range work.Children {
		if child.Kind != v1.NodeItem {
			continue
		}
		if err := c.refreshItem(ctx, client, svc, caller, child.ID, typ, id, result); err != nil {
			return err
		}
		// One item per refresh: for a movie that is the feature, and for a
		// series this is the wrong entry point anyway (an episode is addressed
		// by its own id, not the series'). Refreshing a whole series is its own
		// operation, not a side effect of one.
		break
	}
	return nil
}

// refreshItem reconciles one item's parts against what the source currently
// offers.
func (c *Capability) refreshItem(ctx context.Context, client *Client, svc v1.ContentService, caller v1.Caller, itemID v1.NodeID, typ, id string, result *v1.ImportResult) error {
	existing, err := svc.ListContentParts(ctx, v1.ListContentPartsQuery{Caller: caller, NodeID: itemID})
	if err != nil {
		return fmt.Errorf("read existing parts for %s: %w", itemID, err)
	}

	have := make(map[string]bool, len(existing.Parts))
	for _, p := range existing.Parts {
		if k := candidateKey(p.EditionLabel, p.Location.Ref); k != "" {
			have[k] = true
		}
	}

	streams, err := client.Streams(ctx, typ, id)
	if err != nil {
		return fmt.Errorf("fetch streams for %s: %w", id, err)
	}
	streams = selectCandidates(streams)

	// Continue the existing ordering rather than restarting at zero, so a
	// refreshed candidate does not claim to be the source's top-ranked release
	// when it was appended later.
	order := float64(len(existing.Parts))
	for _, stream := range streams {
		label := releaseLabel(stream)
		if have[candidateKey(label, stream.Ref())] {
			continue
		}
		meta := parseStreamMeta(stream)
		if _, err := svc.AttachContentPart(ctx, v1.AttachContentPartCommand{
			Caller: caller, NodeID: itemID, Role: v1.PartEdition,
			EditionLabel: label,
			NaturalOrder: order,
			Location:     v1.MediaLocation{Scheme: v1.RemoteLocation, Provider: streamProvider, Ref: stream.Ref()},
			Container:    meta.container,
			VideoCodec:   meta.videoCodec,
			AudioCodec:   meta.audioCodec,
			Width:        meta.width,
			Height:       meta.height,
			SizeBytes:    meta.sizeBytes,
		}); err != nil {
			return fmt.Errorf("attach refreshed part for %s: %w", id, err)
		}
		order++
		result.Parts++
	}
	return nil
}

// candidateKey identifies a release across refreshes.
//
// **Not the location URL.** A debrid link is minted per request and differs on
// every fetch, so keying on it would find nothing already stored and re-attach
// the entire listing each time. The release *identity* is what persists: the
// filename or release name, and failing that a magnet's info hash. That is the
// same durable-versus-perishable split the resolution cache draws (ADR 0049),
// one level down — the file is the durable thing, the way to fetch it is not.
func candidateKey(label, ref string) string {
	if l := strings.ToLower(strings.TrimSpace(label)); l != "" {
		return "release:" + l
	}
	if h := infoHash(ref); h != "" {
		return "btih:" + h
	}
	// An unlabelled direct URL is all that is left to key on. It will not match
	// across refreshes, so such a candidate duplicates — accepted, because it is
	// rare and a duplicate is inert where a wrong match would hide a real
	// release.
	if ref != "" {
		return "ref:" + ref
	}
	return ""
}

// infoHash pulls the btih value out of a magnet URI, which is stable for a
// release where the surrounding magnet's trackers and parameters are not.
func infoHash(ref string) string {
	const marker = "urn:btih:"
	i := strings.Index(strings.ToLower(ref), marker)
	if i < 0 {
		return ""
	}
	h := ref[i+len(marker):]
	if j := strings.IndexAny(h, "&?"); j >= 0 {
		h = h[:j]
	}
	return strings.ToLower(h)
}
