package proxy

import "testing"

func TestFormatCountsLine(t *testing.T) {
	tests := []struct {
		name  string
		refs  int
		impls int
		want  string
	}{
		{"both unknown", -1, -1, ""},
		{"zero refs no impls", 0, -1, "\n\n---\n**0 references**"},
		{"one ref", 1, -1, "\n\n---\n**1 reference**"},
		{"many refs", 42, 0, "\n\n---\n**42 references**"},
		{"refs + one impl", 3, 1, "\n\n---\n**3 references** · **1 implementation**"},
		{"refs + many impls", 3, 5, "\n\n---\n**3 references** · **5 implementations**"},
		{"refs missing impls present", -1, 2, "\n\n---\n**2 implementations**"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatCountsLine(tc.refs, tc.impls)
			if got != tc.want {
				t.Fatalf("refs=%d impls=%d\nwant %q\n got %q", tc.refs, tc.impls, tc.want, got)
			}
		})
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
