package all

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"testing"

	"github.com/epg-sync/epgsync/internal/provider"
)

const providersPkg = "github.com/epg-sync/epgsync/internal/provider/providers/"

// 每个 providers/ 下的目录都必须在 all.go 中导入，否则不会注册
func TestAllProvidersImported(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "all.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	imported := make(map[string]bool)
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		imported[path] = true
	}

	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "all" {
			continue
		}
		if !imported[providersPkg+e.Name()] {
			t.Errorf("provider %q is not imported in all.go", e.Name())
		}
	}
}

func TestDefinitionsComplete(t *testing.T) {
	defs := provider.Definitions()
	if len(defs) == 0 {
		t.Fatal("no providers registered")
	}
	for _, def := range defs {
		if def.BaseURL == "" {
			t.Errorf("provider %q has empty BaseURL", def.ID)
		}
	}
}

// 緯來體育同时由 vl（官方）和 homeplus 提供，同步时按优先级先取 vl，
// vl 没有数据时才用 homeplus（数字越小越优先）
func TestVLPreferredOverHomeplus(t *testing.T) {
	priority := make(map[string]int)
	for _, def := range provider.Definitions() {
		priority[def.ID] = def.Priority
	}
	vl, okVL := priority["vl"]
	homeplus, okHP := priority["homeplus"]
	if !okVL || !okHP {
		t.Fatalf("vl registered %v, homeplus registered %v", okVL, okHP)
	}
	if vl >= homeplus {
		t.Errorf("vl priority %d should be smaller than homeplus %d", vl, homeplus)
	}
}
