package config

import "testing"

func TestMergeRecentMovesToFrontDedupedAndCapped(t *testing.T) {
	// Moving an existing entry to the front dedupes it.
	got := mergeRecent([]string{"/a", "/b", "/c"}, "/b")
	want := []string{"/b", "/a", "/c"}
	if !eq(got, want) {
		t.Fatalf("mergeRecent = %v, want %v", got, want)
	}

	// A new entry goes to the front.
	got = mergeRecent([]string{"/a"}, "/new")
	if !eq(got, []string{"/new", "/a"}) {
		t.Fatalf("new entry not at front: %v", got)
	}

	// The list is capped at maxRecents.
	var long []string
	for i := 0; i < maxRecents+5; i++ {
		long = append(long, string(rune('a'+i)))
	}
	got = mergeRecent(long, "/front")
	if len(got) != maxRecents {
		t.Fatalf("len = %d, want cap %d", len(got), maxRecents)
	}
	if got[0] != "/front" {
		t.Fatalf("front = %q, want /front", got[0])
	}
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
