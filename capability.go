package stremio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	v1 "github.com/mosaic-media/mosaic-sdk/contracts/platform/v1"
)

const (
	// CapabilityID is the id the Platform registers this module under and a
	// caller names to invoke it.
	CapabilityID = "stremio"
	// moduleVersion is this module's own version, reported in its Manifest.
	moduleVersion = "0.1.0"
	// providerScheme is the external-id scheme and source-binding provider the
	// module keys content under: Stremio content is identified by IMDB id.
	providerScheme = "imdb"
	// streamProvider names the resolving service recorded on a RemoteLocation
	// Part. The bytes are resolved later (the future Remote Media module); the
	// binding only records where the reference came from.
	streamProvider = "stremio"
)

// Capability satisfies the SDK's capability contract and every provider role it
// declares in its Manifest. The assertions fail to compile if the module drifts
// from what the Platform invokes or from a role it claims to fill (ADR 0027).
var (
	_ v1.Capability       = (*Capability)(nil)
	_ v1.MetadataProvider = (*Capability)(nil)
	_ v1.SearchProvider   = (*Capability)(nil)
	_ v1.CatalogProvider  = (*Capability)(nil)
	_ v1.StreamProvider   = (*Capability)(nil)
)

// Capability is the Stremio addon-source module (ADR 0008's capability
// surface, first populated). It holds only an HTTP client; the addons it
// sources from come from its user-managed settings at invocation time (ADR
// 0021), so the same registered module serves whatever addons each user
// configures. It owns no schema and imports no Platform internals.
type Capability struct {
	httpClient *http.Client
}

// New builds the capability over an HTTP client (nil for a default). Addon
// URLs are not supplied here — they arrive as settings on each Import.
func New(httpClient *http.Client) *Capability {
	return &Capability{httpClient: httpClient}
}

// moduleSettings is the shape the Stremio module reads from its user-managed
// settings document: the list of Stremio addon base URLs to source from.
type moduleSettings struct {
	Addons []string `json:"addons"`
}

// addonsFrom parses the module's settings document into a clean addon list.
func addonsFrom(settings []byte) ([]string, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var s moduleSettings
	if err := json.Unmarshal(settings, &s); err != nil {
		return nil, fmt.Errorf("parse module settings: %w", err)
	}
	var out []string
	for _, a := range s.Addons {
		if t := strings.TrimSpace(a); t != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

// clientFrom builds a client over the addons configured in settings, or returns
// an error naming how to configure one when none are set. Every entry point —
// Import and each provider role — starts here, so the "no addons" message is
// identical wherever a user hits it.
func (c *Capability) clientFrom(settings []byte) (*Client, error) {
	addons, err := addonsFrom(settings)
	if err != nil {
		return nil, err
	}
	if len(addons) == 0 {
		return nil, fmt.Errorf(`no Stremio addons configured; add one with configureModule (settings {"addons":["<manifest-url>"]})`)
	}
	return NewClient(c.httpClient, addons...), nil
}

// Manifest is the module's self-declaration, including the provider roles it
// fills (ADR 0027). It sources metadata and searches and browses catalogs, and
// resolves streams — the four Stremio addon resources.
func (c *Capability) Manifest() v1.Manifest {
	return v1.Manifest{
		ID: CapabilityID, Version: moduleVersion, Name: "Stremio addon source",
		Provides: []v1.Role{v1.RoleMetadata, v1.RoleSearch, v1.RoleCatalog, v1.RoleStream},
	}
}

// Import materialises the virtual item named by req.Ref — a result a search or
// catalog browse produced (ADR 0028) — from the configured addons into the
// Platform. It fetches metadata (required), searches to avoid duplicating,
// creates the Work with an external-id binding, builds the tree, and attaches
// a RemoteLocation Part wherever a stream addon serves one. Metadata alone is
// a complete import; streams are additive.
func (c *Capability) Import(ctx context.Context, svc v1.ContentService, req v1.ImportRequest) (v1.ImportResult, error) {
	client, err := c.clientFrom(req.Settings)
	if err != nil {
		return v1.ImportResult{}, err
	}
	caller := req.Caller

	typ, id := req.Ref.NativeType, req.Ref.NativeID
	if typ == "" || id == "" {
		return v1.ImportResult{}, fmt.Errorf("ref needs a native type and id, got type=%q id=%q", typ, id)
	}

	meta, ok, err := client.Meta(ctx, typ, id)
	if err != nil {
		return v1.ImportResult{}, fmt.Errorf("fetch metadata: %w", err)
	}
	if !ok {
		return v1.ImportResult{}, fmt.Errorf("no configured addon served metadata for %s/%s", typ, id)
	}

	// Search existing content: if this id already resolves to a work, return
	// it rather than creating a second copy.
	if existing, ok, err := c.find(ctx, svc, caller, id); err != nil {
		return v1.ImportResult{}, err
	} else if ok {
		return v1.ImportResult{WorkID: existing, AlreadyKnown: true}, nil
	}

	title := meta.Name
	if title == "" {
		title = id
	}
	work, err := svc.AddContentWork(ctx, v1.AddContentWorkCommand{
		Caller: caller, MediaType: mediaTypeFor(typ), Title: title,
		ExternalIDs: externalIDs(id),
	})
	if err != nil {
		return v1.ImportResult{}, fmt.Errorf("create work: %w", err)
	}
	result := v1.ImportResult{WorkID: work.Work.ID}

	if _, err := svc.BindContentSource(ctx, v1.BindContentSourceCommand{
		Caller: caller, NodeID: work.Work.ID,
		SourceProvider: providerScheme, SourceRef: id,
		MatchConfidence: 1, MatchMethod: v1.MatchExternalIDExact, Status: v1.BindingConfirmed,
	}); err != nil {
		return v1.ImportResult{}, fmt.Errorf("bind source: %w", err)
	}

	switch typ {
	case "movie":
		err = c.importMovie(ctx, client, svc, caller, work.Work.ID, id, &result)
	case "series":
		err = c.importSeries(ctx, client, svc, caller, work.Work.ID, id, meta, &result)
	default:
		// An unknown type still has a Work and a binding; there is simply no
		// tree shape defined for it here, so it lands as a bare work.
	}
	if err != nil {
		return v1.ImportResult{}, err
	}

	return result, nil
}

// importMovie builds a film as Work -> feature item, attaching the stream to
// the item (a Part attaches to an item, never a work — ADR 0013).
func (c *Capability) importMovie(ctx context.Context, client *Client, svc v1.ContentService, caller v1.Caller, workID v1.NodeID, id string, result *v1.ImportResult) error {
	item, err := svc.AddContentChild(ctx, v1.AddContentChildCommand{
		Caller: caller, ParentID: workID,
		Kind: v1.NodeItem, ItemType: v1.ItemFeature,
		Title: "Feature", NaturalOrder: 1,
	})
	if err != nil {
		return fmt.Errorf("create feature item: %w", err)
	}
	result.Items++
	return c.attachStream(ctx, client, svc, caller, item.Node.ID, "movie", id, result)
}

// importSeries builds a series as Work -> season container -> episode item,
// grouping the meta's flat video list by season, and attaching each episode's
// stream to its item.
func (c *Capability) importSeries(ctx context.Context, client *Client, svc v1.ContentService, caller v1.Caller, workID v1.NodeID, id string, meta Meta, result *v1.ImportResult) error {
	for _, season := range groupBySeason(meta.Videos) {
		container, err := svc.AddContentChild(ctx, v1.AddContentChildCommand{
			Caller: caller, ParentID: workID,
			Kind: v1.NodeContainer, ContainerType: v1.ContainerSeason,
			Title: fmt.Sprintf("Season %d", season.number), NaturalOrder: float64(season.number),
		})
		if err != nil {
			return fmt.Errorf("create season %d: %w", season.number, err)
		}
		result.Containers++

		for _, ep := range season.episodes {
			item, err := svc.AddContentChild(ctx, v1.AddContentChildCommand{
				Caller: caller, ParentID: container.Node.ID,
				Kind: v1.NodeItem, ItemType: v1.ItemEpisode,
				Title: ep.EpisodeTitle(), NaturalOrder: float64(ep.Episode),
			})
			if err != nil {
				return fmt.Errorf("create episode %d of season %d: %w", ep.Episode, season.number, err)
			}
			result.Items++

			episodeID := fmt.Sprintf("%s:%d:%d", id, season.number, ep.Episode)
			if err := c.attachStream(ctx, client, svc, caller, item.Node.ID, "series", episodeID, result); err != nil {
				return err
			}
		}
	}
	return nil
}

// attachStream fetches a stream for the content id and, if a stream addon
// served one, attaches it as a RemoteLocation Part. No stream is not an error:
// a meta-only import creates the tree without Parts.
func (c *Capability) attachStream(ctx context.Context, client *Client, svc v1.ContentService, caller v1.Caller, itemID v1.NodeID, typ, id string, result *v1.ImportResult) error {
	stream, ok, err := client.Stream(ctx, typ, id)
	if err != nil {
		return fmt.Errorf("fetch stream for %s: %w", id, err)
	}
	if !ok {
		return nil
	}
	if _, err := svc.AttachContentPart(ctx, v1.AttachContentPartCommand{
		Caller: caller, NodeID: itemID, Role: v1.PartEdition,
		Location: v1.MediaLocation{Scheme: v1.RemoteLocation, Provider: streamProvider, Ref: stream.Ref()},
	}); err != nil {
		return fmt.Errorf("attach stream part for %s: %w", id, err)
	}
	result.Parts++
	return nil
}

// find looks for an existing work already bound to the IMDB id.
func (c *Capability) find(ctx context.Context, svc v1.ContentService, caller v1.Caller, id string) (v1.NodeID, bool, error) {
	found, err := svc.FindContentByExternalID(ctx, v1.FindContentByExternalIDQuery{
		Caller: caller, Scheme: providerScheme, Value: id,
	})
	if err != nil {
		return "", false, fmt.Errorf("search existing content: %w", err)
	}
	for _, node := range found.Nodes {
		if node.IsRoot() {
			return node.ID, true, nil
		}
	}
	return "", false, nil
}

// Metadata resolves descriptive detail for a ref (RoleMetadata — the addon
// `meta` resource). It is the enrichment surface: the descriptive fields, not
// the containment tree, which Import builds where the source's structure is
// known (ADR 0027).
func (c *Capability) Metadata(ctx context.Context, req v1.MetadataRequest) (v1.ContentMetadata, error) {
	client, err := c.clientFrom(req.Settings)
	if err != nil {
		return v1.ContentMetadata{}, err
	}
	meta, ok, err := client.Meta(ctx, req.Ref.NativeType, req.Ref.NativeID)
	if err != nil {
		return v1.ContentMetadata{}, fmt.Errorf("fetch metadata: %w", err)
	}
	if !ok {
		return v1.ContentMetadata{}, fmt.Errorf("no configured addon served metadata for %s/%s", req.Ref.NativeType, req.Ref.NativeID)
	}
	return v1.ContentMetadata{
		Ref:      req.Ref,
		Title:    meta.Name,
		Year:     parseYear(meta.ReleaseInfo),
		Overview: meta.Description,
		Poster:   meta.Poster,
		Backdrop: meta.Background,
		Genres:   meta.Genres,
	}, nil
}

// Search returns virtual candidates for free text (RoleSearch — the addon
// `catalog/…/search` resource). No raw id: this is what makes user search in
// Mosaic work over source content that is not in the library (ADR 0028).
func (c *Capability) Search(ctx context.Context, req v1.SearchRequest) (v1.SearchResponse, error) {
	client, err := c.clientFrom(req.Settings)
	if err != nil {
		return v1.SearchResponse{}, err
	}
	metas, err := client.Search(ctx, req.Text)
	if err != nil {
		return v1.SearchResponse{}, fmt.Errorf("search: %w", err)
	}
	results := make([]v1.SearchResult, 0, len(metas))
	for _, m := range metas {
		if req.MediaType != "" && mediaTypeFor(m.Type) != req.MediaType {
			continue
		}
		results = append(results, v1.SearchResult{
			Ref: refFrom(m), Title: m.Name, Year: parseYear(m.ReleaseInfo), Poster: m.Poster,
		})
	}
	return v1.SearchResponse{Results: results}, nil
}

// Catalogs lists the collections the configured addons expose (RoleCatalog —
// the addon `catalog` resource), for the admin collection browser.
func (c *Capability) Catalogs(ctx context.Context, req v1.CatalogsRequest) (v1.CatalogsResponse, error) {
	client, err := c.clientFrom(req.Settings)
	if err != nil {
		return v1.CatalogsResponse{}, err
	}
	decls, err := client.Catalogs(ctx)
	if err != nil {
		return v1.CatalogsResponse{}, fmt.Errorf("list catalogs: %w", err)
	}
	cats := make([]v1.Catalog, 0, len(decls))
	for _, d := range decls {
		cats = append(cats, v1.Catalog{ID: d.ID, NativeType: d.Type, Name: catalogName(d)})
	}
	return v1.CatalogsResponse{Catalogs: cats}, nil
}

// CatalogItems lists one collection's entries as virtual candidates the admin
// can select to publish (ADR 0028). It does not touch the object graph.
func (c *Capability) CatalogItems(ctx context.Context, req v1.CatalogItemsRequest) (v1.CatalogItemsResponse, error) {
	client, err := c.clientFrom(req.Settings)
	if err != nil {
		return v1.CatalogItemsResponse{}, err
	}
	metas, err := client.CatalogItems(ctx, req.NativeType, req.CatalogID, req.Skip)
	if err != nil {
		return v1.CatalogItemsResponse{}, fmt.Errorf("list catalog items: %w", err)
	}
	items := make([]v1.CatalogItem, 0, len(metas))
	for _, m := range metas {
		items = append(items, v1.CatalogItem{
			Ref: refFrom(m), Title: m.Name, Year: parseYear(m.ReleaseInfo), Poster: m.Poster,
		})
	}
	return v1.CatalogItemsResponse{Items: items}, nil
}

// Streams resolves playable locations for a materialised item's ref (RoleStream
// — the addon `stream` resource). Import snapshots streams at materialise time;
// this exposes the same resolution as a role other flows can call. It returns an
// empty response, no error, when no addon serves a stream (the meta-only case).
func (c *Capability) Streams(ctx context.Context, req v1.StreamRequest) (v1.StreamResponse, error) {
	client, err := c.clientFrom(req.Settings)
	if err != nil {
		return v1.StreamResponse{}, err
	}
	stream, ok, err := client.Stream(ctx, req.Ref.NativeType, req.Ref.NativeID)
	if err != nil {
		return v1.StreamResponse{}, fmt.Errorf("resolve streams: %w", err)
	}
	if !ok {
		return v1.StreamResponse{}, nil
	}
	return v1.StreamResponse{Streams: []v1.StreamLink{{
		Label:    stream.Name,
		Location: v1.MediaLocation{Scheme: v1.RemoteLocation, Provider: streamProvider, Ref: stream.Ref()},
	}}}, nil
}

// refFrom builds a ContentRef from a catalog/search preview. Stremio content is
// keyed by IMDB id, so the native id doubles as the external id the Platform
// dedups and binds on (ADR 0028).
func refFrom(m MetaPreview) v1.ContentRef {
	return v1.ContentRef{
		Provider: CapabilityID, NativeID: m.ID, NativeType: m.Type,
		MediaType: mediaTypeFor(m.Type), ExternalScheme: providerScheme, ExternalID: m.ID,
	}
}

// parseYear reads the leading year from a Stremio releaseInfo ("2017",
// "2008-2013"), returning 0 when there is none.
func parseYear(s string) int {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return 0
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil {
		return 0
	}
	return y
}

// catalogName is the catalog's declared name, or a type/id fallback when it
// declares none.
func catalogName(d CatalogDecl) string {
	if d.Name != "" {
		return d.Name
	}
	return d.Type + " " + d.ID
}

// mediaTypeFor maps a Stremio content type to a Platform media type, using the
// known constants for the two Stremio types and canonicalising anything else
// as open text (ADR 0015).
func mediaTypeFor(typ string) v1.MediaType {
	switch typ {
	case "movie":
		return v1.MediaMovie
	case "series":
		return v1.MediaTVSeries
	default:
		return v1.NormaliseMediaType(typ)
	}
}

// externalIDs builds the Work's external-id document — the flat scheme-to-id
// shape FindContentByExternalID reads.
func externalIDs(id string) []byte {
	b, _ := json.Marshal(map[string]string{providerScheme: id})
	return b
}

// season groups a series' episodes under one season number.
type season struct {
	number   int
	episodes []Video
}

// groupBySeason collects the meta's flat video list into ordered seasons, each
// with its episodes ordered by episode number.
func groupBySeason(videos []Video) []season {
	byNumber := make(map[int][]Video)
	for _, v := range videos {
		byNumber[v.Season] = append(byNumber[v.Season], v)
	}
	seasons := make([]season, 0, len(byNumber))
	for number, eps := range byNumber {
		sort.Slice(eps, func(i, j int) bool { return eps[i].Episode < eps[j].Episode })
		seasons = append(seasons, season{number: number, episodes: eps})
	}
	sort.Slice(seasons, func(i, j int) bool { return seasons[i].number < seasons[j].number })
	return seasons
}
