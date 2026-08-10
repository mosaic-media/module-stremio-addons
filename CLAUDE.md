# Claude Instructions — module-stremio-addons

This repository is a **host for Stremio addons**: a user pastes an addon's
manifest URL and Mosaic sources from it. The addon ecosystem is community-made
and unreviewed, so what this module can reach is whatever a user chooses to add
to it — which is the module's premise, not a caveat about it.

It is an **extension** module
([architecture#3](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0003-two-module-tiers.md)):
nothing requires it, it is **not a dependency of the Platform**, and a Platform
gains it only when a user installs it from the signed registry index
([platform#51](https://github.com/mosaic-media/platform/blob/main/docs/adr/0051-extension-installation-is-user-initiated-and-persistent.md),
[platform#40](https://github.com/mosaic-media/platform/blob/main/docs/adr/0040-module-distribution-and-trust.md)).
`cmd/module-stremio-addons` is what serves it out of process
([platform#39](https://github.com/mosaic-media/platform/blob/main/docs/adr/0039-extension-module-boundary.md),
[sdk#7](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0007-go-plugin-as-the-extension-harness.md)),
and it is deliberately one line: **this module builds and behaves identically
whether or not that file is used.** `stremio.New` remains what a host calls, and
the tests run with no transport at all.

It is built exactly as a third party's module would be. "Official" describes only
its authorship, not its shape; the discipline is the point.

## The boundary is the point

- **Import only [`sdk`](https://github.com/mosaic-media/sdk),
  [`contracts`](https://github.com/mosaic-media/contracts) and the standard
  library.** `boundary_test.go` parses every import and fails on anything else.
  It **walks** the tree rather than reading the root directory, because a check
  that looked only at the root would declare the boundary clean while never
  reading `cmd/`, the one file that imports the harness. `contracts` is allowed
  because this module authors its own settings screen
  ([sdk#4](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0004-module-contributed-settings-ui.md))
  — it declares a *form*, not a screen.
- **`sdk/host` sits under the SDK prefix, and that is correct.** The harness is
  published beside the contract precisely so a module needs no dependency the
  SDK did not already sanction.
- **Nothing may be written to stdout.** go-plugin writes its handshake there and
  anything else corrupts it. Use the ambient telemetry (below); do not print.
- **The `Caller` is a handle, not a session.** It is minted per invocation and
  stops resolving when that invocation returns, so it cannot usefully be stored.
  Forward what you were given.
- **MIT-licensed**, the author's choice, unlike the Platform's AGPL
  ([architecture#1](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0001-licensing.md)).
  Files here carry **no SPDX header** — match the files already present rather
  than importing the Platform's convention.

## This module is an anti-corruption layer, and that is its job

Upstream dialects are translated **here**, at the boundary, into the SDK's typed
fields. The Platform must never learn a provider's quirks
([module-stremio-addons#2](docs/adr/0002-modules-as-anti-corruption-layers.md)).

**What exists today is one universal parse, not a per-source one.**
`parseStreamMeta` applies a single set of regexes — quality, container, video
codec, audio codec, seeders, size — to every addon's free text, and reads no
manifest id at all. The one place a manifest id *is* keyed on is
`deniedAddonIDs`, which hides non-content overlay and status addons from the
browse grid: **that is a deny list, not a dialect.** So a new spelling seen in
the wild is a new alternative in the existing regex, and a per-source extractor
is a decision rather than an edit — read
[module-stremio-addons#2](docs/adr/0002-modules-as-anti-corruption-layers.md)
and its `**Status:**` line for what it decided and where that stands, rather than
restating it here.

**Fill the typed fields rather than leaving them empty.** `Container`,
`VideoCodec` and `AudioCodec` are what a playability decision reads
([platform#27](https://github.com/mosaic-media/platform/blob/main/docs/adr/0027-stream-selection-against-a-client-profile.md)),
and an empty field is not neutral: they were left empty once and the Platform
would have relayed ten gigabytes of Matroska to a browser. **Both outbound paths
must stay in step** — `attachStream` filling an
`AttachContentPartCommand`, and `streamLinkFrom` filling a `StreamLink`. The same
walk over the same text producing a richer answer for a Part than for a link is
exactly the leak that record names, because the only place left to recover the
fact was the URL.

**Best-effort is the honest frame.** This makes a candidate list *rankable*
before anything is fetched; what a release actually contains is settled by
probing the bytes
([platform#29](https://github.com/mosaic-media/platform/blob/main/docs/adr/0029-probing-and-the-per-stream-playback-decision.md)),
because release text lies. Normalise onto the names ffprobe uses so a parsed
guess and a probed fact are comparable.

## Rules the source's own shape forces

- **Resource-aware, so streams do not gate metadata.** Use whatever resources
  each configured addon declares. A meta-only addon must yield metadata with
  **no Parts**, so a user can enrich local media without adopting remote
  streaming.
- **Sample candidates across resolutions; never take the head of the list.**
  `perQualityCandidates` bounds how many releases an item keeps *per resolution*.
  It replaced a flat cap that was actively harmful: an aggregator ranks by
  quality descending, so for one film offering 318 streams a cap of 40 kept forty
  2160p releases and not one below them. Selection had nothing playable to choose
  from, and the reported symptom was a browser rendering Dolby Vision as purple
  and green. A source's ranking answers "which is best"; selection needs "which
  are *different*". An unparsed resolution keeps its own bucket rather than being
  dropped.
- **Refresh is additive.** A release absent from today's listing has usually not
  gone anywhere — the source simply did not return it — and a stored candidate
  costs nothing to keep while removing one risks deleting the release someone is
  part-way through. It walks one level only: refreshing every episode of a
  long-running series is one round trip per episode, which is not something to do
  behind a single click without asking.
- **Content is bound under `imdb`.** Stremio ids *are* IMDb ids, and the accurate
  scheme is what makes a title added here the same Work as one another source
  added rather than a duplicate. Changing it would silently double a library.
- **Settings are user-managed opaque JSON** handed in by the Platform
  ([platform#17](https://github.com/mosaic-media/platform/blob/main/docs/adr/0017-module-settings.md)),
  not env vars and not Platform config. This module owns their meaning. `AddAddon`
  exists because a form submits *values*: appending to a list is not writing a
  field, so the document carries the pending addition and this module folds it in
  — which keeps the append here rather than putting a list operation on the wire
  for every client to implement identically.
- **The addon catalog source is a discovery surface, never a content source.** It
  is reached directly by `browseSection` and must never be merged into the addons
  the provider roles read.

## Two bugs worth knowing, because both fail silently

Both were found against real addons in use, and both are now pinned by hermetic
tests. Neither is a reason to reach the network from this suite.

- **Normalisation trims a suffix, never a path.** `normaliseAddonURL` strips a
  trailing `/manifest.json` and accepts the `stremio://` scheme, and it preserves
  the configuration segment addons encode before it
  (`.../providers=.../manifest.json`). A normaliser that dropped the whole path
  would silently turn a configured addon into a different one. The table in
  `client_internal_test.go` is the pin, and the `strem.io`/`strem.fun` strings in
  it are **literals in that table** — nothing dials them.
- **The User-Agent is load-bearing for reachability.** Cloudflare-fronted addons
  reject Go's default `Go-http-client/1.1` with a 403 while serving any honest
  custom identifier, so `getJSON` sets one on every request.
  `TestClientSetsUserAgent` asserts both that ours is sent and that the Go
  default is not.

## Everything runs in the container, nothing runs on the host

**Do not run `go build`, `go test`, `go vet` or `gofmt` directly on this
machine.** This repository's gate runs inside its test container:

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That runs gofmt, `go build ./...`, `go vet ./...` and `go test ./...` against the
Go version pinned in `docker-compose.test.yml`, which must stay equal to the one
in `go.mod`. `.github/workflows/verify.yml` runs the same four steps — **keep the
two in step.** Append `bash` for a shell in the same environment.

**What the container protects is the boundary.** A host with a populated module
cache, a leftover `go.work` or a stray `replace` can satisfy an import a third
party's machine could not, and `boundary_test.go` still passes because the import
resolved. The container resolves from the proxy exactly as a consumer does.

**The suite is hermetic, and must stay that way.** Every HTTP test stands up an
`httptest` server and points the client at it; nothing here reaches an addon.
That is not a compromise to undo — the addons are somebody else's service, a
suite that depends on one is red when they deploy, and the two bugs above are
already regression-tested without egress. *`docker-compose.test.yml`'s header
still describes this suite as reaching real addons over TLS and warns about
`ca-certificates`; that comment is stale and the suite it describes does not
exist.*

## Versioning and release

A change is a **minor** bump, tagged and pushed. **Nothing bumps a `require`
afterwards** — the Platform does not depend on this module, so there is no
version line anywhere to move. A release reaches people through the
**catalogue**: `release.yml` proves the tag resolvable, its `binaries` job
cross-compiles and assembles a `manifest.json` carrying each binary's digest,
and its `dispatch` job tells the registry there is a new version to list.

Three things about that chain, each of which has already been got wrong:

- **`dispatch` waits on `binaries`, not just `release`.** The registry
  catalogues a release by downloading `manifest.json` from its assets, so a
  dispatch that fired earlier would point the catalogue at a release whose assets
  are still uploading, and the entry would be refused for a module that is fine.
- **A missing dispatch token fails rather than warns.** It used to exit 0, which
  meant an unset token reported green while nothing was ever sent. The tag and
  the binaries already exist by then, so a red run costs nothing that can be
  undone and is how a broken chain becomes visible.
- **The dispatch goes to the registry, not to the Platform.** Dispatching a
  core-module bump here could only ever fail: it would move a require that does
  not exist, and the Platform refuses a bump for a module it does not already
  require — adding one is a human decision.

Warm the Go proxy after tagging anyway: anything building this from source
resolves it as an ordinary Go module, and the proxy and checksum database are
eventually consistent with a just-pushed tag.

**A `replace` must never land in a commit.** The module reports the version that
was **actually linked**, via `v1.ModuleVersion` reading the build graph — not a
hand-maintained constant, which nothing forces to agree with anything.

## Decision records

[`docs/adr/README.md`](docs/adr/README.md) is the generated index of the records
this repository owns, with each one's status. **Read the index rather than
counting files, and do not restate a status here** — it is generated from the
records and this file is not.

The index script and the citation lint that
[`architecture`](https://github.com/mosaic-media/architecture) owns for the fleet
are **not vendored here**, so nothing in this repository's gate checks that the
index is current or that a citation resolves. Until they are, both are on you:
regenerate the index when you add a record, and write every citation as a
`repo#N` link.

## Modules are the forcing function for the SDK

This module exists to find the contract's gaps by using it. **When something
cannot be expressed, that is a finding, not an obstacle to work around** — take
it to the SDK as an additive bump, or record it in the roadmap as an open gap.
**Do not simulate the missing surface locally.**

**Which side a finding lands on is not arbitrary.** The SDK says how a module
interacts with the Platform; the Platform holds the implementations. So a finding
takes the form of a type or a verb that names no library, and one that can only
be closed by naming a library is a Platform change reached through a declarative
surface. What the SDK currently carries, and what it has decided to stop
carrying, is [`sdk`](https://github.com/mosaic-media/sdk)'s own business — read
its instructions and its records rather than a summary here.

## Observability

Observability goes through the SDK's ambient `v1.Telemetry`
([sdk#5](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0005-modules-observe-through-the-sdk.md)),
reached as `TelemetryFrom(ctx)`. **Do not print**, and do not configure an
exporter, a sink or retention — the Platform owns the observability plane. An
addon URL a user pasted may carry configuration they consider private; classify
it rather than writing it verbatim.

<!-- shared-rules:begin -->
## Rules every Mosaic repository shares

*Generated. The source is `architecture/shared/repository-rules.md`; edit it there
and run `scripts/shared_rules.py --write` across the fleet. A copy edited in place
fails its repository's gate, which is the point: these rules were eleven
hand-kept copies in four variants, and the abridged ones had quietly dropped the
reasoning while keeping the rules — and in one case dropped a rule outright.*

### What this file may say

**A `CLAUDE.md` states rules, and facts about its own repository. It does not
state facts about another one — it links instead.**

An audit of all twelve of these files against their source found 74 stale claims.
None of roughly 180 rules was wrong; 62 of the 74 were facts about somebody
else's repository. Ownership predicts rot: a fact about this repository stays true
because whoever changes the code changes the sentence in the same session, and a
fact about another one dies the moment they edit it with nothing here going red.

The same applies to facts this repository already publishes in a generated
artefact — counts, versions, what is built. Point at the artefact.

### Decision records live with the code they govern

Each repository owns the records whose *mechanism* it holds — the spec file, the
lint gate, the conformance corpus, the composition root, the release workflow.
A decision can bind five repositories and still have exactly one steward.

- **`docs/adr/`**, numbered from 1 in every repository, with `docs/adr/README.md`
  a **generated** index. Read the index first; it is the bounded thing.
- **A record's heading carries no number.** The number lives in the filename and
  the index only, so a record's anchor survives being renumbered.
- **Cite a record as `repo#N`, and make it a link** — a relative path within a
  repository, an absolute URL across them, and the bare label only where no URL
  is possible, such as a code comment or a Dockerfile. The old `ADR NNNN`
  spelling is refused by a lint: once every repository numbers from 1, that form
  resolves quietly to a *different* record instead of dangling, and no tool in
  the fleet could detect it.
- **Cross-cutting records stay in [`architecture`](https://github.com/mosaic-media/architecture)** —
  the ones with no enforcing mechanism anywhere: licensing, repository naming and
  topology, the module tier model.

### Decision records are append-only

An ADR is an account of what was decided and why, at a time. It is evidence, not
documentation, and its value is that it was not edited afterwards.

- **Never rewrite a record's body** — not to correct it, not to annotate it, not
  to add "as built, this differs". That turns a record into a running commentary
  and destroys the thing it is for.
- **State changes go in the `**Status:**` line and nowhere else** — built, built
  in part (naming the part), or superseded, wholly or partly.
- **A changed decision earns a new record that supersedes it**, with its own
  Context / Decision / Alternatives / Consequences, and both records then point
  at each other through their Status lines. The old body stays exactly as it was.
- **An unbuilt decision is not a superseded one.** "Not done yet" belongs in the
  Status line and the roadmap; only a reversal earns a new record.

### The roadmap is maintained, not consulted

**`docs/roadmap.md` in [`architecture`](https://github.com/mosaic-media/architecture)
is the single record of where the build is, across every repository.** It stays
there because a milestone spans repositories by construction. Read it before
starting, and **update it in the same session as the change that dates it** — not
in a follow-up, which does not happen.

- A slice that lands is marked landed, **with what it left out named in the same
  sentence**. "Built" with no qualifier claims the whole slice shipped.
- Implementation that departed from its record is recorded where it departed.
  The surprises are the most valuable thing in it.
- **Do not restate the roadmap here.** A second copy of "what is built" in a
  `CLAUDE.md` is how the first copy goes stale unnoticed.
- A capability with no client path is not done — it is
  [owed](https://github.com/mosaic-media/architecture/blob/main/docs/unreachable-capability.md).

### Demonstrated, not asserted

**Say what you actually ran.** A skipped test is not a passed test, and "it should
work" is not evidence.

Each repository's container is the authority on its own gate, and the command is
in that repository's section below. It exists because the checks that matter fail
*soft*: a missing PostgreSQL skips storage tests and still prints `ok`, a missing
generator toolchain produces a drift guard that passes by not running. Where the
container cannot be run, running what you can on the host is better than running
nothing — **provided you report which checks ran and which did not.** Claiming a
gate passed when it was not executed is the one thing this rule exists to stop.

### Commit and push

- **Commit and push each repository separately.** They are siblings on disk and
  independent in git.
- **Commit author identity** must be `AdamNi-7080 <anicholls41@gmail.com>`. If git
  has no identity configured, set it repo-locally rather than globally.
- **Push once the change has been demonstrated working in this session.** Commit
  locally and say so otherwise. **Force-push always requires asking.**
<!-- shared-rules:end -->
