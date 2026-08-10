# Modules as anti-corruption layers: source dialects and a tested-source registry

**Status:** Accepted; **built in part — the typed fields, not the dialect
table.** `module-stremio-addons` does translate at the boundary and fills the
SDK's typed fields rather than leaving them empty: `parseStreamMeta` works out
container, video codec, audio codec, quality, size and seeders, and
`streamLinkFrom` carries all of them onto the `StreamLink`, so nothing
addon-shaped crosses and no consumer re-derives a fact from a URL. **What is not
built is everything keyed on the source's identity.** There is one universal
regex set applied to every addon's free text — the generic fallback this record
describes — and no dialect table, no per-source extractor and no tested-source
registry; the module reads a manifest id only to hide non-source addons from
browse, which is a deny list and not a dialect. `Confidence` does not travel
with a value either: no field on `StreamLink` distinguishes a fact read from a
structured place from one inferred from a release name, so a consumer cannot
prefer the former and the probe
([platform#29](https://github.com/mosaic-media/platform/blob/main/docs/adr/0029-probing-and-the-per-stream-playback-decision.md)) is doing all
of that work alone.
**Date:** 2026-07-22

## Context

[platform#2](https://github.com/mosaic-media/platform/blob/main/docs/adr/0002-module-storage-and-delivery-model.md) already defines an inbound
module as an **anti-corruption layer** — that is the word in the naming table,
and it is why a module is called `module-<system>`. The Stremio module has not
been holding up that end of the bargain, and slice 1 produced a clean example of
what leaking looks like.

An addon returned a stream whose resolved URL carried the release filename in a
**query parameter**:

```
https://comet.feels.legal/<opaque>/…MIRCrew.mkv&name=Thor%3A%20Ragnarok&media_id=tt3501632
```

The module passed that URL up as an opaque `Location.Ref` and the Platform then
tried to work out the container by looking at the path extension — and got it
wrong ([platform#29](https://github.com/mosaic-media/platform/blob/main/docs/adr/0029-probing-and-the-per-stream-playback-decision.md)). The
Platform was guessing at addon-shaped data, which is precisely the guessing an
ACL exists to prevent. `Part.Container`, `Part.VideoCodec` and the rest sat
empty the whole time: the module *had* `behaviorHints.filename` and never
translated it into the fields the contract provides.

The deeper problem is that the addon protocol is not one dialect. Torrentio,
Comet, MediaFusion and aggregators like AIOStreams all satisfy the same
endpoints while putting quality, size, filename and release name in different
fields — `title`, `name`, `description`, `behaviorHints.filename`, or free text
inside any of them. Today the module reads all of them concatenated and pattern-
matches over the blob ([module-stremio-addons#1](0001-completing-the-stremio-source-surface.md)'s
best-effort parse). That is fine as a fallback and wrong as the whole strategy:
it cannot be improved for a specific source without risking every other source,
and a community member who finds a new addon shape has nowhere to put the
knowledge.

## Decision

**A module translates its source into the contract's vocabulary at the boundary,
and does it per known dialect. Recognised sources get an exact translation, an
unknown source gets a documented best-effort fallback, and adding a dialect is a
small module change rather than a Platform one.**

- **Nothing addon-shaped crosses the boundary.** The module fills the SDK's typed
  fields — container, video codec, audio codec, resolution, size, quality — and a
  consumer never re-derives them from a URL, a filename or a release name. If the
  contract has no field for something the source knows, that is a reason to grow
  the contract deliberately, not to smuggle the value inside a string.
- **A dialect table keyed on manifest id.** An addon's manifest carries a stable
  id (`com.aiostreams.…`, Torrentio's, Comet's). The module maps that id to an
  extractor that knows where *that* source puts each fact. This is the
  **tested-source registry**: the list is explicit about what has actually been
  verified against a live addon rather than implied by whether parsing happens to
  work.
- **An unknown source is supported, not rejected.** It falls through to the
  generic best-effort extractor that exists today. The registry raises the
  ceiling for sources someone has tested; it must never become a gate that makes
  a new addon unusable until the module ships a translation.
- **Confidence travels with the value.** A field the module read from a
  structured place is not the same as one it inferred from free text, and a
  consumer ranking candidates should be able to prefer the former. A parsed guess
  is also why the *probe*
  ([platform#29](https://github.com/mosaic-media/platform/blob/main/docs/adr/0029-probing-and-the-per-stream-playback-decision.md)) confirms the
  winner before playing it: the ACL makes the list rankable, the probe makes the
  chosen one certain.
- **A new dialect is a module version bump, and nothing else moves.** No SDK
  change, no Platform change, no coordination — which is the point of the module
  boundary and the first time it will be exercised by someone who is not us.
  This is the shape a community contribution takes.
- **The registry is the module's, not the Platform's.** The Platform must not
  learn which addons exist or how they differ; that knowledge is exactly what it
  delegated when it made the module an ACL.

## Alternatives considered

**Keep the single best-effort parser over concatenated text.** *Rejected as the
whole strategy, kept as the fallback.* It cannot be tuned for one source without
risking others, it hides which sources are actually known to work, and it gives a
contributor no place to add knowledge short of editing shared pattern-matching
everyone depends on.

**Let the Platform normalise instead.** *Rejected*, and it is what accidentally
happened: the Platform inspecting a URL for a container extension is the Platform
doing the module's job with less information. It also scales wrongly — every new
source dialect would become a Platform change, and the Platform would accumulate
knowledge of an ecosystem it deliberately does not model.

**Probe everything and skip parsing entirely.** *Rejected.* A probe is a request
and a process per candidate ([platform#29](https://github.com/mosaic-media/platform/blob/main/docs/adr/0029-probing-and-the-per-stream-playback-decision.md)),
and a listing runs to dozens. Something has to make a list rankable cheaply
before one candidate is worth confirming, and that is what the ACL is for.

**Require an allowlist of tested addons.** *Rejected.* It would make Mosaic worse
than a plain Stremio client the moment someone points it at something new, and
the ecosystem's whole character is that anyone can host an addon.

## Consequences

- **The contract's typed fields finally get filled.** `Part.Container`,
  `VideoCodec`, `AudioCodec`, `Width`, `Height` and `SizeBytes` stop being
  decoration and become the input selection and the playback decision read.
- **"Tested against" becomes a claim the module can honestly make**, and a
  visible list is more useful to a user choosing addons than silence is.
- **A community contribution has an obvious shape.** Find an addon whose fields
  land oddly, add a dialect, bump the module. That is a much smaller ask than
  understanding the Platform, and it is the first concrete answer to what
  third-party module work looks like beyond writing a whole source.
- **The dialect table will rot quietly.** An addon changes its output and its
  entry becomes wrong in a way that looks like bad metadata rather than a bug.
  The generic fallback bounds the damage; treating the table as something to
  re-verify rather than write once is the discipline it needs.
- **This generalises past Stremio.** Any future inbound module consuming an
  ecosystem with dialects — trackers, metadata providers — has the same shape
  available, and [platform#2](https://github.com/mosaic-media/platform/blob/main/docs/adr/0002-module-storage-and-delivery-model.md)'s "module means ACL" stops being only a naming
  argument.

## Implementation implications

In `module-stremio-addons`: a dialect interface over the addon `Stream` object, a
table keyed on manifest id with entries for the sources actually verified (the
AIOStreams family, Torrentio, Comet), the existing best-effort parse retained as
the fallback, and the extracted values written into the SDK's typed fields on
import rather than left in the location string. `StreamLink` grows the container
and codec fields
([platform#27](https://github.com/mosaic-media/platform/blob/main/docs/adr/0027-stream-selection-against-a-client-profile.md)) that give the
translation somewhere to land, and `Import` carries them onto the Part.
