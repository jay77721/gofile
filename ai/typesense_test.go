package ai

import "testing"

func TestOwnershipFilterIsLiteralAndExact(t *testing.T) {
	got := ownershipFilter("alice` || username:=bob")
	want := "username:=`alice\\` || username:=bob`"
	if got != want {
		t.Fatalf("ownership filter = %q, want %q", got, want)
	}
}

func TestOwnershipFilterGroupsAdditionalFilter(t *testing.T) {
	// This mirrors the composition in SearchHybrid: an OR in the type filter
	// must remain inside its group and cannot bypass the ownership predicate.
	ownership := ownershipFilter("alice")
	filter := "(" + ownership + ") && (tags:=[图片] || tags:=[文档])"
	want := "(username:=`alice`) && (tags:=[图片] || tags:=[文档])"
	if filter != want {
		t.Fatalf("composed filter = %q, want %q", filter, want)
	}
}
