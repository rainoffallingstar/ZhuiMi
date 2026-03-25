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

func TestExtractDOIHandlesEscapes(t *testing.T) {
	if got := ExtractDOI("https://doi.org/10.1000%2Fabc"); got != "10.1000/abc" {
		t.Fatalf("unexpected doi: %s", got)
	}
	if got := ExtractDOI("doi: 10.2000&#x2F;def."); got != "10.2000/def" {
		t.Fatalf("unexpected doi: %s", got)
	}
}

func TestHashContentSortsInputs(t *testing.T) {
	left := HashContent("b", "a", "c")
	right := HashContent("c", "b", "a")
	if left != right {
		t.Fatalf("expected stable hash ordering, got %q vs %q", left, right)
	}
}
