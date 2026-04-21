package proxy

import (
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

func TestFormatHoverTailEmptyWhenBothNil(t *testing.T) {
	got := formatHoverTail(nil, nil)
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestFormatHoverTailRefsOnly(t *testing.T) {
	refs := []protocol.Location{
		{URI: "file:///a/foo.go", Range: protocol.Range{Start: protocol.Position{Line: 9}}},
		{URI: "file:///a/bar.go", Range: protocol.Range{Start: protocol.Position{Line: 41}}},
	}
	got := formatHoverTail(refs, nil)
	if !strings.Contains(got, "**2 references**") {
		t.Errorf("missing count summary: %q", got)
	}
	if !strings.Contains(got, "[foo.go:10](file:///a/foo.go#10)") {
		t.Errorf("missing first link: %q", got)
	}
	if !strings.Contains(got, "[bar.go:42](file:///a/bar.go#42)") {
		t.Errorf("missing second link: %q", got)
	}
	// No implementations section when impls is nil.
	if strings.Contains(got, "Implementations") {
		t.Errorf("unexpected impls section: %q", got)
	}
}

func TestFormatHoverTailRefsAndImpls(t *testing.T) {
	refs := []protocol.Location{
		{URI: "file:///r.go", Range: protocol.Range{Start: protocol.Position{Line: 0}}},
	}
	impls := []protocol.Location{
		{URI: "file:///i1.go", Range: protocol.Range{Start: protocol.Position{Line: 4}}},
		{URI: "file:///i2.go", Range: protocol.Range{Start: protocol.Position{Line: 99}}},
	}
	got := formatHoverTail(refs, impls)
	if !strings.Contains(got, "**1 reference** · **2 implementations**") {
		t.Errorf("missing combined header: %q", got)
	}
	if !strings.Contains(got, "**Implementations**") {
		t.Errorf("missing impls heading: %q", got)
	}
	if !strings.Contains(got, "[i2.go:100](file:///i2.go#100)") {
		t.Errorf("missing second impl link: %q", got)
	}
}

func TestFormatHoverTailZeroCounts(t *testing.T) {
	got := formatHoverTail([]protocol.Location{}, []protocol.Location{})
	// Header still shows "0 references"; impls with 0 entries collapses
	// (it would just say "0 implementations" — we suppress that to keep
	// the tooltip tidy).
	if !strings.Contains(got, "**0 references**") {
		t.Errorf("missing zero refs header: %q", got)
	}
	if strings.Contains(got, "implementation") {
		t.Errorf("zero impls should not be advertised: %q", got)
	}
}

func TestFormatHoverTailLinkLimit(t *testing.T) {
	locs := make([]protocol.Location, hoverLinkLimit+5)
	for i := range locs {
		locs[i] = protocol.Location{
			URI:   "file:///x.go",
			Range: protocol.Range{Start: protocol.Position{Line: uint32(i)}},
		}
	}
	got := formatHoverTail(locs, nil)
	if !strings.Contains(got, "_and 5 more_") {
		t.Errorf("link cap not applied: %q", got)
	}
}

func TestCountLabelSingularPlural(t *testing.T) {
	cases := map[int]string{0: "0 references", 1: "1 reference", 2: "2 references", 10: "10 references"}
	for n, want := range cases {
		if got := countLabel(n, "reference"); got != want {
			t.Errorf("countLabel(%d) = %q want %q", n, got, want)
		}
	}
}
