package stremio

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// Unioning metadata across addons.
//
// A Stremio setup is several addons, and more than one of them answers `meta`.
// This used to take the first that replied, which made the answer an accident of
// list order — and since the bundled Cinemeta was prepended, it always won and
// every richer source a user had deliberately installed was never even asked.
//
// The fix is to ask all of them and merge, which is what a union means for a
// resource that is a *record* rather than a list:
//
//   - **Scalars** (title, overview, poster, logo, rating, runtime) coalesce:
//     the first non-empty value in priority order. That is where the gap-filling
//     lives — a source with no clearlogo no longer costs you the one another
//     source had.
//   - **Lists** (cast, genres) genuinely union, deduped.
//   - **Episodes** merge by season/episode, then coalesce field by field, so a
//     per-episode still from one source and a synopsis from another end up on
//     the same episode.
//
// Everything is fetched concurrently. Sequentially this would be one round trip
// per addon on the detail-screen path, and detail views are the most latency-
// sensitive thing in the product; concurrently it costs the slowest addon rather
// than the sum of them.

// metadataPriority ranks an addon as a metadata source. Lower wins a conflict.
//
// TMDB first, by the owner's call and for a defensible reason: it is a
// maintained, broadly-populated database, where an addon aggregating scrapers is
// primarily a *stream* source that happens to carry some metadata. Cinemeta
// sits last — ADR 0035 bundles it so a fresh install has metadata at all, and
// that argues for it being the floor rather than the first voice. A user who
// installed something else chose it deliberately.
//
// This is a default, not a policy: the intent is for a user to order their own
// sources in settings, and this is the shape that preference will slot into.
func metadataPriority(m Manifest, baseURL string) int {
	hay := strings.ToLower(m.ID + " " + m.Name + " " + baseURL)
	switch {
	case strings.Contains(hay, "tmdb"):
		return 0
	case strings.Contains(hay, "cinemeta"):
		return 2
	default:
		return 1
	}
}

// addonMeta pairs one addon's answer with its rank, so the merge can order
// sources without re-deriving the ranking.
type addonMeta struct {
	priority int
	meta     Meta
}

// MetaMerged asks every addon that serves `meta` and merges their answers.
//
// An addon that fails or has nothing is skipped rather than failing the whole
// read: metadata assembled from three sources out of four is a better detail
// screen than an error, and one unreachable addon must not blank a page.
func (c *Client) MetaMerged(ctx context.Context, typ, id string) (Meta, bool, error) {
	var (
		mu      sync.Mutex
		results []addonMeta
		wg      sync.WaitGroup
	)

	for _, a := range c.addons {
		// The manifest is fetched inside the goroutine too, so a slow manifest
		// costs the same as a slow meta call rather than serialising ahead of it.
		wg.Add(1)
		go func(a *resolvedAddon) {
			defer wg.Done()
			if err := c.ensureManifest(ctx, a); err != nil {
				return
			}
			if !supports(a.manifest, "meta", typ, id) {
				return
			}
			var resp struct {
				Meta Meta `json:"meta"`
			}
			if err := c.getJSON(ctx, a.baseURL+"/meta/"+typ+"/"+id+".json", &resp); err != nil {
				return
			}
			if resp.Meta.ID == "" && resp.Meta.Name == "" {
				return
			}
			mu.Lock()
			results = append(results, addonMeta{priority: metadataPriority(a.manifest, a.baseURL), meta: resp.Meta})
			mu.Unlock()
		}(a)
	}
	wg.Wait()

	if len(results) == 0 {
		return Meta{}, false, nil
	}
	// Stable sort so two sources of equal rank keep their configured order,
	// which is the only tie-break a user has expressed.
	sort.SliceStable(results, func(i, j int) bool { return results[i].priority < results[j].priority })

	merged := results[0].meta
	for _, r := range results[1:] {
		mergeMeta(&merged, r.meta)
	}
	return merged, true, nil
}

// mergeMeta folds src into dst without overwriting anything dst already has.
//
// "Without overwriting" is the whole rule, and it is what makes priority order
// meaningful: the highest-ranked source that had an opinion keeps it, and every
// later source can only fill blanks.
func mergeMeta(dst *Meta, src Meta) {
	coalesce(&dst.ID, src.ID)
	coalesce(&dst.Type, src.Type)
	coalesce(&dst.Name, src.Name)
	coalesce(&dst.Poster, src.Poster)
	coalesce(&dst.Background, src.Background)
	coalesce(&dst.Logo, src.Logo)
	coalesce(&dst.Description, src.Description)
	coalesce(&dst.ReleaseInfo, src.ReleaseInfo)
	coalesce(&dst.Runtime, src.Runtime)
	coalesce(&dst.ImdbRating, src.ImdbRating)

	dst.Genres = unionStrings(dst.Genres, src.Genres)
	// Cast appears both as a legacy top-level name list and inside Links; both
	// are read downstream, so both have to survive the merge.
	dst.Cast = unionStrings(dst.Cast, src.Cast)
	dst.Links = unionLinks(dst.Links, src.Links)
	dst.Videos = mergeVideos(dst.Videos, src.Videos)
}

// coalesce fills a string only when it is empty.
func coalesce(dst *string, src string) {
	if strings.TrimSpace(*dst) == "" {
		*dst = src
	}
}

// unionStrings appends what is new, case-insensitively, preserving order.
func unionStrings(dst, src []string) []string {
	seen := make(map[string]bool, len(dst))
	for _, s := range dst {
		seen[strings.ToLower(strings.TrimSpace(s))] = true
	}
	for _, s := range src {
		k := strings.ToLower(strings.TrimSpace(s))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		dst = append(dst, s)
	}
	return dst
}

// unionLinks merges the links array, which is where cast and crew live. Deduped
// on category plus name, so the same actor from two sources appears once.
func unionLinks(dst, src []Link) []Link {
	key := func(l Link) string {
		return strings.ToLower(l.Category + "\x00" + l.Name)
	}
	seen := make(map[string]bool, len(dst))
	for _, l := range dst {
		seen[key(l)] = true
	}
	for _, l := range src {
		if l.Name == "" || seen[key(l)] {
			continue
		}
		seen[key(l)] = true
		dst = append(dst, l)
	}
	return dst
}

// mergeVideos merges episode lists by season and episode number rather than
// appending, so two sources describing the same episode enrich it instead of
// duplicating it — a still from one and a synopsis from the other.
func mergeVideos(dst, src []Video) []Video {
	index := make(map[[2]int]int, len(dst))
	for i, v := range dst {
		index[[2]int{v.Season, v.Episode}] = i
	}
	for _, v := range src {
		k := [2]int{v.Season, v.Episode}
		if i, ok := index[k]; ok {
			coalesce(&dst[i].Name, v.Name)
			coalesce(&dst[i].Title, v.Title)
			coalesce(&dst[i].Overview, v.Overview)
			coalesce(&dst[i].Thumbnail, v.Thumbnail)
			coalesce(&dst[i].Released, v.Released)
			continue
		}
		index[k] = len(dst)
		dst = append(dst, v)
	}
	return dst
}
