# Completing the Stremio source surface

**Status:** Accepted
**Date:** 2026-07-21

## Context

The Stremio module covered four of the addon protocol's resources — `meta`,
`catalog`, `catalog/…/search`, `stream` — but not the whole of it, and it did
nothing until a user configured an addon. Three gaps remained against a
*complete* Stremio source:

- **`subtitles`** was unsupported — no role, no client, no mapping.
- **Stream release detail** (quality, size, swarm health) that addons pack into
  a stream's title was decoded as an opaque `Label` and otherwise dropped.
- **A working default** — a fresh install returned nothing until an addon URL
  was pasted in.

The owner's call is to **complete the module's source surface now, even where no
consumer exists yet**, so Stremio module development can be treated as done. The
reasoning is specific and worth recording: whatever eventually consumes these —
a player, for subtitles and stream selection — will still be *sourcing* them
through this module. The module is the source regardless of the consumer, so
finishing the source is not premature the way inventing a *consumer* surface
would be.

## Decision

**The Stremio module implements the full source surface — adding subtitles as a
source provider role, richer stream properties, and a bundled default addon — so
it is a complete Stremio addon client.**

- **Subtitles as a source role (`RoleSubtitles` / `SubtitlesProvider`), built and
  filled ahead of its consumer.** The SDK gains the role, a `Subtitle` DTO
  (language, URL, id), and the provider interface; the module implements it over
  the addon `subtitles` resource. This is a **considered exception** to the
  "don't add surface nothing consumes yet" discipline ([platform#24](https://github.com/mosaic-media/platform/blob/main/docs/adr/0024-capability-gated-affordances.md)):
  it applies to *source* roles here because subtitles are inherently the source
  module's job, the surface is small and typed, and completing the source lets
  Stremio be ruled done. The consumer — a player ([platform#24](https://github.com/mosaic-media/platform/blob/main/docs/adr/0024-capability-gated-affordances.md)'s
  deferred playback capability) — resolves subtitles through the registry when it
  exists, exactly as it will resolve streams. Nothing consumes them today, and
  that is expected.
- **Richer stream properties on `StreamLink`.** `Quality`, `SizeBytes`,
  `Seeders` and the full `Title` are parsed best-effort from the stream's
  descriptive text and `behaviorHints` (Torrentio-style titles pack them as free
  text), so a future source-picker can rank and display candidates. Best-effort:
  a field the source does not report stays zero.
- **Cinemeta bundled by default (module-owned).** The module always includes
  Cinemeta as a baseline addon, with a `disableDefaultAddons` opt-out, so
  metadata and search work with zero configuration. This **realises
  [platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md)'s default provider as a
  *module-owned* default** rather than a Platform-seeded setting — the module
  guarantees its own baseline, and the Platform need not know Cinemeta exists.

## Alternatives considered

**Defer subtitles until a player exists (the [platform#24](https://github.com/mosaic-media/platform/blob/main/docs/adr/0024-capability-gated-affordances.md)
default).** *Rejected here by explicit choice.* The discipline exists to stop
*consumer/affordance* surface with nothing behind it; a *source* role is
different — the module is the subtitle source whether or not a player has been
built, so finishing it now completes the module. The cost (a role nothing calls
yet) is bounded, typed, and honest.

**Platform-seeded default addon ([platform#23](https://github.com/mosaic-media/platform/blob/main/docs/adr/0023-metadata-as-required-capability.md)'s
first sketch).** *Superseded* by the module-owned default: making the module
guarantee its own baseline is cleaner than the Platform writing Cinemeta into the
module's settings at bootstrap, and it keeps the Platform ignorant of a specific
addon.

**Keep the opaque `Label` and drop release detail.** *Rejected:* a source-picker
cannot rank "1080p, 45 seeders, 2.3 GB" against "720p, 3 seeders" without the
fields; parsing them once at the source is where it belongs.

## Consequences

- **The Stremio module is a complete Stremio addon source** — `meta`, `search`,
  `catalog`, `stream`, `subtitles`, plus release detail and a working default.
  The one remaining Stremio-shaped piece is `addon_catalog`, which is **not a
  source role** but a settings-UI concern handled in
  [sdk#4](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0004-module-contributed-settings-ui.md).
- **The Platform registry recognises `RoleSubtitles`** and can resolve a
  subtitles provider by module id, so the future player has a seam. Nothing
  consumes it yet — by design.
- **SDK `v0.6.0`, module `v0.4.0`.** Additive; the SDK grew the subtitles role
  and the `StreamLink` fields, the module grew the client and mappings.
- **Best-effort parsing is a maintenance surface.** Addon title formats vary, so
  quality/size/seeders extraction is heuristic and will miss some sources; that
  is acceptable for display/ranking hints and is documented as best-effort.

## Implementation implications

Built as described: SDK `RoleSubtitles`/`SubtitlesProvider`/`Subtitle` and the
richer `StreamLink`; the module's `subtitles` client + provider, stream-detail
parsing, and module-owned Cinemeta default with opt-out; the Platform registry's
subtitles case and resolver. Tested against the hermetic fake addon (subtitles
resolution, stream release-detail parsing, the default-addon merge). The
`addon_catalog` settings UI is a separate slice ([sdk#4](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0004-module-contributed-settings-ui.md)).
