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

func TestConnectionCatalogV4PreservesContracts(t *testing.T) {
	command := strings.Replace(strings.Replace(catalogV3Fixture, `"version":3`, `"version":4`, 1), `"record":"resource-v1"`, `"record":"resource-v1","capabilities":[]`, 1)
	http := strings.Replace(strings.Replace(command, `"shape":"command"`, `"shape":"http"`, 1), `"capabilities":[]`, `"adapter":"adapter-sources","capabilities":["answer","search"]`, 1)
	for _, tt := range []struct {
		name, raw string
		ok        bool
	}{
		{"command without adapter", command, true}, {"http with separate adapter identity", http, true},
		{"adapter absent", strings.Replace(http, `"adapter":"adapter-sources",`, "", 1), false},
		{"command program forbidden", strings.Replace(http, `"shape":"http"`, `"shape":"command"`, 1), false},
		{"capabilities absent", strings.Replace(command, `,"capabilities":[]`, "", 1), false},
		{"null declaration", strings.Replace(command, `"capabilities":[]`, `"capabilities":null`, 1), false},
		{"unknown capability", strings.Replace(http, `["answer","search"]`, `["crawl"]`, 1), false},
		{"duplicates", strings.Replace(http, `["answer","search"]`, `["answer","answer"]`, 1), false},
		{"order", strings.Replace(http, `["answer","search"]`, `["search","answer"]`, 1), false},
		{"duplicate member", strings.Replace(command, `"capabilities":[]`, `"capabilities":[],"capabilities":[]`, 1), false},
		{"unsafe program", strings.Replace(http, "adapter-sources", "../adapter", 1), false},
		{"explicit page source", strings.Replace(command, `"sources":[]`, `"sources":[{"id":"web","input":"url","mediaTypes":["text/html"],"maxBytes":4194304,"adapter":"adapter-web","capabilities":["page"]}]`, 1), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDeskCatalogHelper$")
			cmd.Env = append(os.Environ(), "DESK_CATALOG_HELPER="+tt.raw)
			got, err := runConnectionCatalog(ctx, cmd)
			if (err == nil) != tt.ok {
				t.Fatalf("admission: %v", err)
			}
			if tt.ok && string(got) != tt.raw {
				t.Fatal("catalog was rewritten")
			}
		})
	}
}

func TestConnectionCatalogV4ActualGateway(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/testing/gatewayCatalogV4.json")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDeskCatalogHelper$")
	cmd.Env = append(os.Environ(), "DESK_CATALOG_HELPER="+string(raw))
	got, err := runConnectionCatalog(ctx, cmd)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("Gateway catalog not admitted unchanged: %v", err)
	}
}

func TestConnectionCatalogRecurringSourceOperationBound(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/testing/gatewayCatalogV4.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	_ = json.Unmarshal(raw, &catalog)
	for _, row := range catalog["providers"].([]any) {
		provider := row.(map[string]any)
		if provider["id"] == "google-drive" {
			ops := provider["operations"].([]any)
			for _, op := range []string{"source-bind", "source-bindings", "source-revoke", "source-observe"} {
				ops = append(ops, op)
			}
			provider["operations"] = ops
			if len(ops) != 18 {
				t.Fatalf("real Drive control fixture changed: %d", len(ops))
			}
		}
	}
	raw, _ = json.Marshal(catalog)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDeskCatalogHelper$")
	cmd.Env = append(os.Environ(), "DESK_CATALOG_HELPER="+string(raw))
	if _, err = runConnectionCatalog(ctx, cmd); err != nil {
		t.Fatal("bounded new Drive operations made catalog unavailable", err)
	}
}
