package desk

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in only: uses a private temporary copy of configured API/search keys.
// Never copies native account tokens, touches existing records, or sends outreach.
func TestDialectLiveProviderSmoke(t *testing.T) {
	platform, bundle := os.Getenv("JPS_LIVE_DIALECT_PLATFORM"), os.Getenv("JPS_TEST_GATEWAY_BUNDLE")
	if platform == "" || bundle == "" {
		t.Skip("explicit live-provider opt-in required")
	}
	project, config := t.TempDir(), t.TempDir()
	os.Chmod(config, 0700)
	copyFile := func(from, to string) {
		t.Helper()
		raw, e := os.ReadFile(from)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.MkdirAll(filepath.Dir(to), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(to, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	copyFile(filepath.Join(platform, "desk.json"), filepath.Join(config, "desk.json"))
	copyFile(filepath.Join(platform, "secrets", "assistant"), filepath.Join(config, "secrets", "assistant"))
	copyFile(filepath.Join(platform, "web-access.json"), filepath.Join(config, "web-access.json"))
	// Search state only; no mailbox, drive, native sign-in or write connectors.
	searchRoot := filepath.Join(platform, "connections", "web-search")
	filepath.WalkDir(searchRoot, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && d.Name() == "state.json" {
			rel, _ := filepath.Rel(searchRoot, path)
			copyFile(path, filepath.Join(config, "gateway-connections", "web-search", rel))
		}
		return nil
	})
	s, ts := startDesk(t, Config{ProjectDir: project, DeskConfigDir: config, Token: testToken, LocalGatewayBundle: bundle, CodexBin: "off"})
	defer ts.Close()
	defer s.Close()
	registry, e := s.readAIRegistry()
	if e != nil {
		t.Fatal(e)
	}
	var ai inputResearchAI
	for _, c := range registry.Connections {
		if c.ID == "legacy-api" && c.Assistant.Endpoint != nil && c.Assistant.Endpoint.Model != nil {
			ai = inputResearchAI{Connection: c.ID, Revision: c.Revision, Model: *c.Assistant.Endpoint.Model}
		}
	}
	if ai.Connection == "" {
		t.Fatal("No configured API connection for isolated live smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	companion := s.connectionCompanion("web-search", true)
	raw, e := companion.call(ctx, bundle, filepath.Join(config, "gateway-connections"), "status", json.RawMessage(`{}`), "web-search", false)
	if e != nil {
		t.Fatal(e)
	}
	var status struct {
		Connections []struct{ ID, Revision string }
	}
	if json.Unmarshal(raw, &status) != nil || len(status.Connections) == 0 {
		t.Fatal("No available search connection in isolated copy")
	}
	search := &inputResearchSearch{Connection: status.Connections[0].ID, Revision: status.Connections[0].Revision}
	call := func(path string, body any, revision string) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, strings.NewReader(string(raw))).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code >= 300 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	reports := []map[string]any{}
	var company inputPreparationReply
	for _, target := range []string{"companies", "people-at-companies"} {
		intent := "Find Help Scout, the customer support software company, using official public sources. Retain its business description. This is a connectivity smoke test, not an ICP qualification."
		fields := []inputResearchField{{Name: "description", Description: "What this company does, from an official public source", Type: "string"}}
		recipe := inputResearchRecipe{Version: 1, Target: target, AI: ai, Search: search, Fields: fields, Criteria: []inputResearchCriterion{}, Limits: inputResearchLimits{Records: 1, Searches: 2, Pages: 3, ModelCalls: 10}}
		if target == "people-at-companies" {
			intent = "Find one named professional at the selected company from an official public team or company page. Retain their current professional role and the selected company relationship. Do not infer email addresses or other contact details."
			recipe.Fields = []inputResearchField{{Name: "role", Description: "Current professional role stated on the public source", Type: "string"}}
			recipe.CompanyInputs = []inputResearchCompanyRef{{PreparationID: company.Document.ID, Revision: company.SHA256, EntityID: company.Document.Records[0].Research.Entity.ID}}
		}
		var saved inputPreparationReply
		json.Unmarshal(call("/api/input-preparations", map[string]any{"name": "Live smoke " + target, "recipe": inputRecipe{Intent: intent, Research: &recipe}}, ""), &saved)
		path := "/api/input-preparations/" + saved.Document.ID + "/research"
		var run inputResearchRun
		json.Unmarshal(call(path, map[string]any{}, saved.SHA256), &run)
		call(path+"/"+run.ID+"/confirm", map[string]any{}, "")
		for ctx.Err() == nil {
			root, err := s.inputResearchRoot(false)
			if err != nil {
				t.Fatal(err)
			}
			rr, err := openInputResearch(root, run.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			run, err = readInputResearch(rr)
			rr.Close()
			root.Close()
			if err != nil {
				t.Fatal(err)
			}
			if run.State != "running" && run.State != "queued" {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		report := map[string]any{"target": target, "state": run.State, "records": len(run.Records), "sources": len(run.Sources), "requests": len(run.Attempts), "problem": run.Problem}
		reports = append(reports, report)
		summary, _ := json.MarshalIndent(reports, "", "  ")
		if out := os.Getenv("JPS_LIVE_DIALECT_REPORT"); out != "" {
			os.WriteFile(out, summary, 0600)
		}
		t.Log(string(summary))
		if len(run.Records) != 1 {
			t.Fatal("Live provider did not retain the requested record; see state and problem above")
		}
		if target == "companies" {
			company = v2Rows(t, s, saved.Document.ID)
		} else if run.Records[0].CompanyID != company.Document.Records[0].Research.Entity.ID {
			t.Fatal("Live person lost selected company relationship")
		}
	}
}
