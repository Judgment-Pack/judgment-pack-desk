package desk

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Handwritten owner vectors, independent of a Gateway run or reader derivation.
const catalogV5Read = `{"operation":"read","effect":"read-external"}`
const catalogV5Act = `{"operation":"act","effect":"act"}`
const catalogV5Run = `{"operation":"run","effect":"act"}`
const catalogV5Source = `"taxonomy":` + catalogV5Read + `,"profile":{"effect":"read-external","operations":["read"]},"capabilityTaxonomies":{},"dispatchTaxonomies":{"read":` + catalogV5Read + `}`
const catalogV5Fixture = `{"version":5,"discoveryTaxonomy":{"operation":"discover","effect":"pure"},"sources":[{"id":"web","input":"url","mediaTypes":["text/html"],"maxBytes":4194304,"adapter":"adapter-web","capabilities":["page"],"taxonomy":` + catalogV5Read + `,"profile":{"effect":"read-external","operations":["read"]},"capabilityTaxonomies":{"page":` + catalogV5Read + `},"dispatchTaxonomies":{"page":` + catalogV5Read + `}}],"providers":[{"id":"obsidian","auth":"local-folder","registration":"none","selection":"source-search","queryRequired":false,"operations":["status"],"protocol":"connection-v1","queryMode":"text","presentation":{"icon":"","name":"Notes","description":{"en":"Notes"},"instructions":{"en":"Choose notes"}},"setup":[],"authorizationEndpoints":[],"source":{"id":"obsidian","shape":"command","record":"note-v1","capabilities":[],` + catalogV5Source + `},"taxonomy":` + catalogV5Run + `,"profile":{"effect":"act","operations":["read","act"]},"operationTaxonomies":{"status":` + catalogV5Run + `},"operationDeclarations":{"status":{"taxonomy":` + catalogV5Run + `,"profile":{"effect":"act","operations":["read","act"]},"components":{"operation":{"operation":"read","effect":"read"},"operator-policy":` + catalogV5Act + `}}}}]}`

func TestConnectionCatalogV5OwnerDeclarations(t *testing.T) {
	for _, tt := range []struct {
		name, old, new string
		ok             bool
	}{
		{name: "complete", ok: true},
		{"missing discovery", `"discoveryTaxonomy":{"operation":"discover","effect":"pure"},`, "", false},
		{"wrong discovery", `"operation":"discover","effect":"pure"`, `"operation":"discover","effect":"read"`, false},
		{"missing profile", `"profile":{"effect":"read-external","operations":["read"]},`, "", false},
		{"null profile", `"profile":{"effect":"read-external","operations":["read"]}`, `"profile":null`, false},
		{"missing declarations", `"operationDeclarations":`, `"otherDeclarations":`, false},
		{"extra declaration", `"operationDeclarations":{"status":`, `"operationDeclarations":{"configure":{},"status":`, false},
		{"empty dispatch", `"dispatchTaxonomies":{"page":` + catalogV5Read + `}`, `"dispatchTaxonomies":{}`, false},
		{"null dispatch", `"dispatchTaxonomies":{"page":` + catalogV5Read + `}`, `"dispatchTaxonomies":null`, false},
		{"missing capability", `"capabilityTaxonomies":{"page":` + catalogV5Read + `}`, `"capabilityTaxonomies":{}`, false},
		{"extra capability", `"capabilityTaxonomies":{}`, `"capabilityTaxonomies":{"page":` + catalogV5Read + `}`, false},
		{"unknown selector", `"dispatchTaxonomies":{"read":`, `"dispatchTaxonomies":{"other":`, false},
		{"duplicate selector", `"dispatchTaxonomies":{"page":`, `"dispatchTaxonomies":{"page":` + catalogV5Read + `,"page":`, false},
		{"duplicate taxonomy", `"taxonomy":` + catalogV5Read, `"taxonomy":` + catalogV5Read + `,"taxonomy":` + catalogV5Read, false},
		{"duplicate taxonomy field", `"operation":"read","effect":"read-external"`, `"operation":"read","operation":"read","effect":"read-external"`, false},
		{"case folded field", `"operation":"read","effect":"read-external"`, `"Operation":"read","effect":"read-external"`, false},
		{"taxonomy extension", `"operation":"read","effect":"read-external"`, `"operation":"read","effect":"read-external","approval":true`, false},
		{"wrong effect", `"operation":"read","effect":"read-external"`, `"operation":"read","effect":"act"`, false},
		{"check kind noncheck", `"operation":"read","effect":"read-external"`, `"operation":"read","effect":"read-external","checkKind":"test"`, false},
		{"explicit null operation", `"operation":"read","effect":"read-external"`, `"operation":null,"effect":"read-external"`, false},
		{"invalid experimental", `"operation":"read","effect":"read-external"`, `"operation":"read","effect":"read-external","experimental":null`, false},
		{"experimental contradiction", `"operation":"read","effect":"read-external"`, `"operation":"read","effect":"read-external","experimental":true`, false},
		{"profile order", `"operations":["read","act"]`, `"operations":["act","read"]`, false},
		{"profile duplicate", `"operations":["read","act"]`, `"operations":["read","read","act"]`, false},
		{"missing operator policy", `"operator-policy":` + catalogV5Act, `"other":` + catalogV5Act, false},
		{"weaker policy", `"operator-policy":` + catalogV5Act, `"operator-policy":{"operation":"read","effect":"read"}`, false},
		{"intrinsic contradiction", `"operation":{"operation":"read","effect":"read"}`, `"operation":{"operation":"discover","effect":"read"}`, false},
		{"empty operations", `"operations":["status"]`, `"operations":[]`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := catalogV5Fixture
			if tt.old != "" {
				if !strings.Contains(raw, tt.old) {
					t.Fatal("mutation target absent")
				}
				raw = strings.Replace(raw, tt.old, tt.new, 1)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDeskCatalogHelper$")
			cmd.Env = append(os.Environ(), "DESK_CATALOG_HELPER="+raw)
			got, err := runConnectionCatalog(ctx, cmd)
			if (err == nil) != tt.ok {
				t.Fatalf("admission: %v", err)
			}
			if tt.ok && string(got) != raw {
				t.Fatal("catalog changed")
			}
		})
	}
}
func TestConnectionCatalogV5ExplicitArtifact(t *testing.T) {
	path := os.Getenv("DESK_GATEWAY_CONNECTIONS_ARTIFACT")
	if path == "" {
		t.Skip("requires explicit verified scratch artifact")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := runConnectionCatalog(ctx, exec.CommandContext(ctx, path, "--catalog-v5"))
	if err != nil {
		t.Fatal(err)
	}
	var c connectionCatalog
	if json.Unmarshal(raw, &c) != nil || c.Version != 5 || len(c.Providers) == 0 || len(c.Sources) == 0 {
		t.Fatal("missing actual catalog")
	}
	if out := os.Getenv("DESK_GATEWAY_CATALOG5_OBSERVATION"); out != "" {
		if err := os.WriteFile(out, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
