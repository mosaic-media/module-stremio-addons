package stremio

import "testing"

// TestCandidateKeyIsStableAcrossRefetches is the assertion the whole refresh
// rests on. A debrid link is minted per request, so the same release comes back
// with a different URL every time; keying on the location would match nothing
// already stored and re-attach the entire listing on each refresh.
func TestCandidateKeyIsStableAcrossRefetches(t *testing.T) {
	const release = "Thor.Ragnarok.2017.1080p.x264.AAC.mkv"

	first := candidateKey(release, "https://cdn.example/dl/AAAA1111/file.mkv?exp=1")
	second := candidateKey(release, "https://cdn.example/dl/BBBB2222/file.mkv?exp=9")

	if first != second {
		t.Fatalf("same release keyed differently across fetches:\n  %q\n  %q", first, second)
	}
}

// TestCandidateKeyFallsBackToInfoHash covers the source that gives no filename.
// A magnet's info hash identifies the file where the surrounding trackers and
// parameters do not, so it survives a re-listing that reorders them.
func TestCandidateKeyFallsBackToInfoHash(t *testing.T) {
	a := candidateKey("", "magnet:?xt=urn:btih:0123456789ABCDEF&dn=Thor&tr=udp://one")
	b := candidateKey("", "magnet:?xt=urn:btih:0123456789abcdef&tr=udp://two&dn=Thor")

	if a != b || a == "" {
		t.Fatalf("info-hash key not stable: %q vs %q", a, b)
	}
}

// TestCandidateKeyDistinguishesDifferentReleases guards the opposite failure: a
// key so loose that two releases collide would silently hide one of them, which
// is worse than a duplicate because it cannot be seen.
func TestCandidateKeyDistinguishesDifferentReleases(t *testing.T) {
	a := candidateKey("Thor.Ragnarok.2017.1080p.x264.AAC.mkv", "https://cdn.example/a")
	b := candidateKey("Thor.Ragnarok.2017.2160p.HEVC.EAC3.mkv", "https://cdn.example/b")

	if a == b {
		t.Fatal("two different releases produced the same key")
	}
}

// TestCandidateKeyIgnoresLabelCasingAndPadding — the same release arriving with
// different whitespace or casing must not read as new.
func TestCandidateKeyIgnoresLabelCasingAndPadding(t *testing.T) {
	if candidateKey("  Thor.Ragnarok.MKV ", "x") != candidateKey("thor.ragnarok.mkv", "y") {
		t.Error("keying is sensitive to casing or padding it should normalise away")
	}
}

func TestInfoHashExtraction(t *testing.T) {
	cases := map[string]string{
		"magnet:?xt=urn:btih:ABC123&dn=x": "abc123",
		"magnet:?dn=x&xt=urn:btih:abc123": "abc123",
		"https://cdn.example/file.mkv":    "",
		"":                                "",
	}
	for ref, want := range cases {
		if got := infoHash(ref); got != want {
			t.Errorf("infoHash(%q) = %q, want %q", ref, got, want)
		}
	}
}
