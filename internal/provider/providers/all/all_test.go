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
