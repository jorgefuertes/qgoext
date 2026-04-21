package proxy

import (
	"testing"

	"go.lsp.dev/protocol"
)

func mkRange(line, ch uint32) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: line, Character: ch},
		End:   protocol.Position{Line: line, Character: ch + 1},
	}
}

func TestSynthesizeLensesCoversTopLevelDecls(t *testing.T) {
	uri := protocol.DocumentURI("file:///x.go")
	syms := []protocol.DocumentSymbol{
		{Name: "TopFunc", Kind: protocol.SymbolKindFunction, Range: mkRange(1, 0), SelectionRange: mkRange(1, 5)},
		{Name: "TopStruct", Kind: protocol.SymbolKindStruct, Range: mkRange(5, 0), SelectionRange: mkRange(5, 5)},
		{Name: "TopIface", Kind: protocol.SymbolKindInterface, Range: mkRange(9, 0), SelectionRange: mkRange(9, 5)},
		{Name: "TopConst", Kind: protocol.SymbolKindConstant, Range: mkRange(15, 0), SelectionRange: mkRange(15, 5)},
	}
	got := synthesizeLenses(uri, syms)

	// TopFunc(refs) + TopStruct(refs) + TopIface(refs,impls) + TopConst(refs) = 5
	if len(got) != 5 {
		t.Fatalf("want 5 lenses, got %d: %+v", len(got), got)
	}
	var impls int
	for _, l := range got {
		d, ok := l.Data.(ourLensData)
		if !ok {
			t.Fatalf("lens data not tagged: %T %+v", l.Data, l.Data)
		}
		if d.Source != qgoextSource {
			t.Fatalf("unexpected source %q", d.Source)
		}
		if d.Kind == lensKindImplementations {
			impls++
		}
	}
	if impls != 1 {
		t.Fatalf("want 1 implementations lens (from TopIface), got %d", impls)
	}
}

func TestSynthesizeLensesInterfaceMethods(t *testing.T) {
	uri := protocol.DocumentURI("file:///y.go")
	method := protocol.DocumentSymbol{
		Name: "Read", Kind: protocol.SymbolKindMethod,
		Range: mkRange(3, 2), SelectionRange: mkRange(3, 2),
	}
	iface := protocol.DocumentSymbol{
		Name: "Reader", Kind: protocol.SymbolKindInterface,
		Range:          mkRange(2, 0),
		SelectionRange: mkRange(2, 5),
		Children:       []protocol.DocumentSymbol{method},
	}
	got := synthesizeLenses(uri, []protocol.DocumentSymbol{iface})

	// Reader: refs + impls; method: refs + impls (because parent is interface).
	if len(got) != 4 {
		t.Fatalf("want 4 lenses, got %d", len(got))
	}
	var methodImpls int
	for _, l := range got {
		d := l.Data.(ourLensData)
		if d.Line == method.SelectionRange.Start.Line && d.Kind == lensKindImplementations {
			methodImpls++
		}
	}
	if methodImpls != 1 {
		t.Fatalf("want 1 implementations lens on method, got %d", methodImpls)
	}
}

func TestSynthesizeLensesSkipsIneligibleKinds(t *testing.T) {
	uri := protocol.DocumentURI("file:///z.go")
	syms := []protocol.DocumentSymbol{
		// Namespace is not in the lens-worthy set.
		{Name: "pkg", Kind: protocol.SymbolKindNamespace, Range: mkRange(1, 0), SelectionRange: mkRange(1, 0)},
		// A nested variable inside a function is not top-level.
		{
			Name: "OuterFn", Kind: protocol.SymbolKindFunction,
			Range: mkRange(2, 0), SelectionRange: mkRange(2, 5),
			Children: []protocol.DocumentSymbol{
				{Name: "inner", Kind: protocol.SymbolKindVariable, Range: mkRange(3, 2), SelectionRange: mkRange(3, 2)},
			},
		},
	}
	got := synthesizeLenses(uri, syms)
	// Only OuterFn should get a references lens.
	if len(got) != 1 {
		t.Fatalf("want 1 lens, got %d: %+v", len(got), got)
	}
	if got[0].Data.(ourLensData).Kind != lensKindReferences {
		t.Fatalf("unexpected lens kind %q", got[0].Data.(ourLensData).Kind)
	}
}

func TestParseOurLensDataRejectsOthers(t *testing.T) {
	_, ok := parseOurLensData(nil)
	if ok {
		t.Fatal("nil data should not parse as ours")
	}
	_, ok = parseOurLensData(map[string]any{"foreign": "value"})
	if ok {
		t.Fatal("foreign data should not parse as ours")
	}
	data := ourLensData{Source: qgoextSource, Kind: lensKindReferences, URI: "file:///a", Line: 1}
	raw := map[string]any{
		"qgoext_source": string(data.Source),
		"kind":          string(data.Kind),
		"uri":           string(data.URI),
		"line":          float64(data.Line),
	}
	got, ok := parseOurLensData(raw)
	if !ok {
		t.Fatal("own data should parse")
	}
	if got.Kind != lensKindReferences || got.URI != "file:///a" || got.Line != 1 {
		t.Fatalf("decoded wrong: %+v", got)
	}
}
