module github.com/mosaic-media/mosaic-module-stremio

go 1.25.0

require github.com/mosaic-media/mosaic-sdk v0.4.0

// Local cross-repo dev (ADR 0034): build against the unreleased SDK v0.5.0 fields.
// Release step: tag mosaic-sdk v0.5.0, bump the require above, drop this replace.
replace github.com/mosaic-media/mosaic-sdk => ../mosaic-sdk
