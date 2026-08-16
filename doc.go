// Package stremio is the Stremio addon-source module, built exactly as a third
// party would build one. It is its own Go module
// (github.com/mosaic-media/module-stremio-addons) importing only the published
// SDK (contracts/platform/v1) and the standard library, and it reaches the
// Platform through the capability registry (sdk#1).
//
// It is an extension module (architecture#3): the Platform does not depend on
// it, it appears in no go.mod of the Platform's, and an install gains it only
// when a user installs it from the signed registry index (platform#51). It runs
// out of process behind a harness (platform#39).
//
// It consumes the Stremio addon protocol as a client: it points at one or more
// addon HTTP endpoints and, guided by each addon's manifest, uses whatever
// resources that addon declares. Metadata (the meta resource) creates the Work
// and its season/episode tree with an external-id source binding; streams (the
// stream resource) attach a RemoteLocation Part. The two are independent — a
// meta-only addon yields metadata with no Parts, so a user can enrich local
// media through Stremio addons without adopting remote streaming. Streams are
// opt-in by which addons are configured, not by the module.
//
// It owns no schema (platform#8): everything it does to the graph goes through
// ContentService, acting as the Caller the Platform hands it (platform#13). Stream
// locations are snapshotted at import; resolving or transcoding them at play
// time is a separate concern (the Remote Media module), deliberately not here.
package stremio
