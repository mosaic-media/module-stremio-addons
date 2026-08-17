# Claude Instructions — module-stremio-addons

Fleet-wide conventions — commits, decision records, citation form, the roadmap —
are in [`architecture`](https://github.com/mosaic-media/architecture/blob/main/CLAUDE.md).
This file is what is specific to `module-stremio-addons`.

This repository is a **host for Stremio addons**: a user pastes an addon's
manifest URL into the module's settings and Mosaic sources from whatever that
addon declares. What this module can reach is whatever a user chose to add to it
— the premise, not a caveat about it.

It is an **extension module**: nothing requires it, and a Platform gains it only
when a user installs it from the signed registry index. The evidence is in this
repository — `release.yml` cross-compiles binaries and a `manifest.json` and
dispatches `module-released` at the registry instead of moving a `require` line
anywhere, and `cmd/module-stremio-addons` serves it out of process. **`README.md`
and `cmd/module-stremio-addons/main.go` both still say the Platform composes this
module statically; both sentences are stale.**

## What it declares, and how it refuses

`Capability.Manifest` in `capability.go` declares `RoleMetadata`, `RoleSearch`,
`RoleCatalog`, `RoleStream`, `RoleSubtitles` and `RoleSettingsUI`. The
compile-time assertions above it fail the build if a declared role loses its
method; keep them in step with the manifest.

- **No configured addon is an error**, not an empty answer: `clientFrom` refuses,
  because every provider role needs something to ask.
- **`SettingsUI` deliberately bypasses `clientFrom`.** No addons is what a fresh
  install looks like and that screen is the only way out of it, so it builds its
  clients directly. Do not "tidy" it onto `clientFrom`.
- **`Streams` and `Subtitles` return an empty response with no error** when
  `addressOf` cannot address the ref — every stream provider is asked about
  content some other module sourced.

## The boundary

`boundary_test.go` parses every non-test import and allows only the standard
library, `sdk/…`, `contracts/…` and this module's own path; `contracts` is
allowed because the module authors its own settings screen.

**It walks the tree rather than reading the root**, deliberately:
`cmd/module-stremio-addons/main.go` is the one file that imports `sdk/host`, and
a check reading only the package directory would declare the boundary clean while
never opening it. Keep the walk. That command file is one line by design — the
module builds and behaves identically whether or not it is used, and
`stremio.New` stays what a host calls.

**The `Caller` is a handle, not a session.** The Platform mints it per invocation
and revokes it on return, so it stops resolving the instant that invocation
returns and cannot usefully be stored. Forward the one you were given.

## Settings

User-managed opaque JSON, handed in on every invocation; this module owns their
meaning. `moduleSettings` in `capability.go` is the shape: `addons`, plus
`addAddon` — a single pending addition, because a form submits *values* and
appending to a list is not writing a field. `addonsFrom` folds it in and dedupes,
and unknown keys are ignored, so an older document needs no migration.
`configureModule` replaces the whole document, so every control sends the
complete addon list (`configureInput`).

**Configured order is the priority order.** `MetaMerged` sorts by it and takes
identity whole from the first source that has one, artwork as a set from the
first that has any, and unions the supplementary lists.

## Reading what a user pasted, and what an addon sent

- **`normaliseAddonURL` trims a suffix, never a path.** It accepts `stremio://`,
  drops a query or fragment and strips a trailing `/manifest.json` and slashes,
  but preserves the configuration segment an addon encodes before it, so
  `https://host/providers=yts/manifest.json` becomes `https://host/providers=yts`
  — dropping the path would silently turn a configured addon into a different
  one. The table in `client_internal_test.go` is the pin; its `strem.io` and
  `strem.fun` strings are literals and nothing dials them.
- **One universal parse, not a per-source one.** `parseStreamMeta` applies the
  same regexes to every addon's free text and reads no manifest id. A new
  spelling seen in the wild is a new alternative in an existing pattern; a
  per-source extractor is a decision, not an edit. Normalise onto the names
  ffprobe uses so a parsed guess and a probed fact compare.
- **The two outbound paths must stay in step.** `attachStream` fills an
  `AttachContentPartCommand` and `streamLinkFrom` fills a `StreamLink` from the
  same parse. `Container`, `VideoCodec` and `AudioCodec` are what a playability
  decision reads, and an empty field is not neutral.
- **`selectCandidates` bounds how many releases an item keeps *per resolution*.**
  A source ranks by quality descending, so a flat head-of-list cap keeps only the
  largest and least playable ones and selection has nothing playable to choose
  from. An unparsed resolution keeps its own bucket rather than being dropped.
- **Refresh is additive and walks one level.** `refreshCandidates` adds what the
  source now offers and removes nothing, and stops after the work's first item
  child rather than making a round trip per episode of a series.
- **`deniedAddonIDs` is a deny list, not a dialect** — the one place a manifest
  id is keyed on, and only to hide non-content addons from the browse grid.

- **The User-Agent is load-bearing for reachability.** `getJSON` sets it on every
  request because Cloudflare-fronted addons answer Go's default
  `Go-http-client/1.1` with a 403. `TestClientSetsUserAgent` asserts both that
  ours is sent and that the Go default is not.
- **`addonCatalogSource` is a discovery surface, never a content source** —
  reached only by `browseSection`, never merged into the addons the roles read.
- **Content is bound under `imdb`** (`providerScheme`). Stremio ids *are* IMDb
  ids, so that scheme is what makes a title added here the same Work another
  IMDb-keyed source added rather than a duplicate; changing it doubles a library.

## The gate

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That is the record-index check, the citation lint, gofmt, `go build`, `go vet`
and `go test`, against the Go version pinned in the compose file — keep that
version equal to `go.mod`'s. Append `bash` for a shell in the same environment.
`.github/workflows/verify.yml` runs the same checks on a `setup-go` runner and is
what refuses a push; keep the two in step.

Do not run any of them on the host: a populated module cache, a leftover
`go.work` or a stray `replace` can satisfy an import a third party's machine
could not, and `boundary_test.go` passes anyway because the import resolved.

**The suite is hermetic** — every HTTP test stands up an `httptest` server, and
the capability tests pair it with an in-memory `ContentService`. Keep it that
way; the addons are somebody else's service. *`docker-compose.test.yml`'s header
comment still describes the suite as reaching real addons over TLS; it is stale.*

## Release

A change is a minor bump, tagged and pushed; **a `replace` must never land in a
commit**, and the version is read from the build graph by `v1.ModuleVersion`
rather than held in a constant. Nothing bumps a `require` afterwards.

`release.yml` reuses `verify.yml`, proves the tag resolvable through the public
proxy, cross-compiles binaries and a `manifest.json` in `binaries`, then
dispatches `module-released` at `mosaic-media/registry`. **`dispatch` needs
`[release, binaries]`**, because the registry catalogues by downloading that
`manifest.json` from the release assets; it fails rather than warns when
`REGISTRY_DISPATCH_TOKEN` is unset.

## Records, licence, observability

[`docs/adr/README.md`](docs/adr/README.md) is the generated index of the records
this repository owns; read it rather than counting files, and never hand-edit it.
`scripts/adr_index.py` and `scripts/adr_lint.py` are **vendored** from
`architecture/scripts/` and run by this gate — change them there and re-vendor.

MIT-licensed; files carry **no SPDX header** — match the files already present.
Observability goes through `v1.TelemetryFrom(ctx)`, and **nothing may be written
to stdout**, where go-plugin's handshake lives. An addon URL a user pasted may
carry configuration they consider private: classify it, never log it verbatim.
