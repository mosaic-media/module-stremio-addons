package stremio

import "strings"

// Candidate labelling. An addon's descriptive fields are free text with several
// facts packed into them, so naming a release is its own small problem — and it
// matters, because the label is what a person choosing between two candidates by
// hand actually reads.

// perQualityCandidates bounds how many releases one item keeps *per resolution*.
//
// It replaces a flat head-of-list cap, which was actively harmful. An aggregator
// ranks by quality descending, so taking the first N takes the largest and least
// playable releases and discards everything else: for one film the source
// offered 318 streams — 99 at 2160p, 141 at 1080p, 38 at 720p — and a cap of 40
// kept forty 2160p releases and not a single one below it. Selection then had
// nothing playable to choose from and the reported symptom was a browser
// rendering Dolby Vision as purple and green.
//
// The lesson generalises: a source's ranking answers "which is best", and
// selection needs "which are *different*". Sampling across the range keeps the
// set bounded without letting one resolution crowd out the rest.
const perQualityCandidates = 12

// selectCandidates samples a source's listing across resolutions, preserving the
// source's own order within each.
//
// An unparsed resolution gets its own bucket rather than being dropped: the
// parse is best-effort, plenty of perfectly good releases carry no resolution in
// their name, and discarding them would repeat the mistake this function exists
// to fix on a different axis.
func selectCandidates(streams []Stream) []Stream {
	seen := map[string]int{}
	out := make([]Stream, 0, len(streams))
	for _, s := range streams {
		q := parseStreamMeta(s).quality
		if q == "" {
			q = "unknown"
		}
		if seen[q] >= perQualityCandidates {
			continue
		}
		seen[q]++
		out = append(out, s)
	}
	return out
}

// releaseLabel is the human name for a candidate — the filename where an addon
// gives one, otherwise the first line of its descriptive text.
func releaseLabel(s Stream) string {
	if s.BehaviorHints.Filename != "" {
		return s.BehaviorHints.Filename
	}
	for _, candidate := range []string{s.Title, s.Name, s.Description} {
		if line := strings.TrimSpace(firstLine(candidate)); line != "" {
			return line
		}
	}
	return ""
}

// firstLine takes the first line of an addon's descriptive text. Addons pack
// several facts into one field separated by newlines, and only the first is the
// release name.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}
