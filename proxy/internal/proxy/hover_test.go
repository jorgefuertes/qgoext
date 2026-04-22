package proxy

import (
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

func TestFormatHoverTailEmptyWhenBothNil(t *testing.T) {
	if got := formatHoverTail(nil, nil); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestFormatHoverTailRefsOnlyLinksToFirst(t *testing.T) {
	refs := []protocol.Location{
		{URI: "file:///a/foo.go", Range: protocol.Range{Start: protocol.Position{Line: 9}}},
		{URI: "file:///a/bar.go", Range: protocol.Range{Start: protocol.Position{Line: 41}}},
	}
	got := formatHoverTail(refs, nil)
	if !strings.Contains(got, "[**2 references**](file:///a/foo.go#10)") {
		t.Errorf("missing refs link to first location: %q", got)
	}
	if strings.Contains(got, "implementation") {
		t.Errorf("implementations slot should be absent: %q", got)
	}
	if strings.Contains(got, "bar.go") {
		t.Errorf("file list should no longer appear: %q", got)
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
	if !strings.Contains(got, "[**1 reference**](file:///r.go#1)") {
		t.Errorf("missing refs link: %q", got)
	}
	if !strings.Contains(got, "[**2 implementations**](file:///i1.go#5)") {
		t.Errorf("missing impls link to first impl: %q", got)
	}
	if !strings.Contains(got, " · ") {
		t.Errorf("missing separator between parts: %q", got)
	}
}

func TestFormatHoverTailZeroRefsPlainText(t *testing.T) {
	got := formatHoverTail([]protocol.Location{}, []protocol.Location{})
	if !strings.Contains(got, "**0 references**") {
		t.Errorf("missing zero refs header: %q", got)
	}
	if strings.Contains(got, "](") {
		t.Errorf("no link should be rendered when there are no locations: %q", got)
	}
	if strings.Contains(got, "implementation") {
		t.Errorf("zero impls should not be advertised: %q", got)
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
