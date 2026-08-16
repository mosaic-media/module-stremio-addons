// Command module-stremio-addons runs this module as its own process, for a
// Platform that hosts it out of process (platform#39, sdk#7).
//
// It is one line because crossing the process boundary must not change what a
// module author writes (platform#39): the Capability below is the same plain Go
// value a statically composed Platform links in, its provider roles are the same
// methods, and its tests run with no transport at all.
//
// This module builds and behaves identically whether or not this file is used.
// Nothing else here imports it, and stremio.New remains what a
// statically-composed Platform calls, so the change that switches the Platform
// over is a composition change rather than a module change.
//
// Two constraints a module inherits from host.Serve, neither of them obvious
// from this file:
//
//   - Nothing may be written to stdout. go-plugin writes its handshake there,
//     and anything else corrupts it. Use the Telemetry reached from the
//     invocation's context (sdk#5).
//   - The Caller is a handle, not a session. It is minted per invocation and
//     stops resolving when that invocation returns, so it cannot usefully be
//     stored. Forward what you were given (platform#13).
package main

import (
	"github.com/mosaic-media/sdk/host"

	stremio "github.com/mosaic-media/module-stremio-addons"
)

func main() {
	// nil takes the module's default HTTP client. In process the Platform hands
	// one in so outbound calls route through its dial guard and carry trace
	// context (platform#33, seam 9); out of process it cannot, because an
	// *http.Client does not cross a process boundary.
	//
	// The seam platform#39 moves instead: egress for an out-of-process module is
	// contained by a forward proxy the Platform operates, which sees every host
	// whether the module cooperates or not. That proxy is not built yet, so until
	// it is, this process's outbound calls are unguarded in a way the in-process
	// path is not — which is why the Platform still composes this module
	// statically today.
	host.Serve(stremio.New(nil))
}
