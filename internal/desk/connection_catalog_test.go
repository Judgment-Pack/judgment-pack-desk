package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const catalogFixture = `{"version":1,"providers":[{"id":"obsidian","auth":"local-folder","registration":"none","selection":"source-search","queryRequired":false,"operations":["status","configure","search","select","disconnect"]}]}`

func TestDeskCatalogHelper(t *testing.T) {
	mode := os.Getenv("DESK_CATALOG_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "huge":
		fmt.Print(strings.Repeat("x", connectionCatalogLimit+1))
	case "hang":
		time.Sleep(time.Minute)
	case "fail":
		fmt.Print(catalogFixture)
		fmt.Fprint(os.Stderr, "private diagnostic")
		os.Exit(1)
	default:
		fmt.Print(mode)
	}
	os.Exit(0)
}

func TestConnectionCatalogProcessBoundsAndValidation(t *testing.T) {
	for _, example := range []struct {
		mode string
		ok   bool
	}{
		{catalogFixture, true}, {`{"version":1,"providers":[]}`, true},
		{"huge", false}, {"hang", false}, {"fail", false}, {"null", false},
		{strings.Replace(catalogFixture, `"version":1`, `"version":2`, 1), false},
		{strings.Replace(catalogFixture, `"obsidian"`, `"../../bin/sh"`, 1), false},
		{strings.Replace(catalogFixture, `"operations":[`, `"operations":["status",`, 1), false},
		{strings.Replace(catalogFixture, `"auth":"local-folder"`, `"auth":"local-folder","token":"secret"`, 1), false},
		{catalogFixture + catalogFixture, false},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDeskCatalogHelper$")
		cmd.Env = append(os.Environ(), "DESK_CATALOG_HELPER="+example.mode)
		raw, err := runConnectionCatalog(cmd)
		cancel()
		if (err == nil) != example.ok || !example.ok && raw != nil {
			t.Fatalf("catalog result mismatch for %.60s: %s %v", example.mode, raw, err)
		}
		if example.ok && !json.Valid(raw) {
			t.Fatal("invalid catalog passed")
		}
	}
}

func TestConnectionCatalogRequiresSessionAndNeverFallsBack(t *testing.T) {
	s, ts, _ := assistantServer(t)
	request, _ := http.NewRequest("POST", ts.URL+"/api/connections/catalog", strings.NewReader(`{}`))
	response, err := ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal(response.StatusCode)
	}
	writeDeskConfig(t, s, researchDeskFile("http://127.0.0.1:1"))
	status, _ := sendJSON(t, ts, "POST", "/api/connections/catalog", map[string]string{})
	if status != 503 || s.connections.cmd != nil {
		t.Fatal("catalog used local fallback", status)
	}
	for _, path := range []string{"/api/connections/gmail/catalog", "/api/connections/catalog?provider=gmail"} {
		status, _ := sendJSON(t, ts, "POST", path, map[string]string{})
		if status != 400 {
			t.Fatal(path, status)
		}
	}
	status, _ = sendJSON(t, ts, "POST", "/api/connections/catalog", map[string]string{"provider": "gmail"})
	if status != 400 {
		t.Fatal("catalog accepted parameters", status)
	}
}
