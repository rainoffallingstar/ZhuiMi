package model

import "testing"

func TestBuildArticleIDPrefersDOI(t *testing.T) {
	id := BuildArticleID("10.1000/abc", "https://example.com?a=1", "Title")
	if id != "doi:10.1000/abc" {
		t.Fatalf("unexpected id: %s", id)
	}
}

func TestCanonicalizeLink(t *testing.T) {
	got := CanonicalizeLink("https://example.com/path/?a=1#x")
	if got != "https://example.com/path" {
		t.Fatalf("unexpected canonical link: %s", got)
	}
}
