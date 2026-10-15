package desk

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDialectTemplatesBrowser(t *testing.T) {
	script := os.Getenv("JPS_DIALECT_TEMPLATES_BROWSER")
	if script == "" {
		t.Skip("set JPS_DIALECT_TEMPLATES_BROWSER after building web/dist")
	}
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken, CodexBin: "off"})
	defer ts.Close()
	defer s.Close()
	saveTestAIRegistry(t, s, ts, aiRegistry{Version: 1, DefaultConnection: testAIAlpha, Connections: []aiConnection{aiTestConnection(testAIAlpha, "Fixture AI", "https://api.example.invalid/v1")}})
	root, e := s.openInputPreparations(true)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	doc := inputPreparation{Version: 1, ID: "prep_" + strings.Repeat("e", 32), Name: "Fixture companies", RecipeVersion: 1, Recipe: inputRecipe{Intent: "Fixture scope"}, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), Records: []preparedInputRecord{}, Imports: []inputImport{}, Activity: []inputActivity{}}
	for _, name := range []string{"Alpha", "Beta"} {
		doc.Records = append(doc.Records, preparedInputRecord{ID: "row-" + name, Name: name, Values: map[string]string{"name": name}, Class: "generated", Status: "researched", Research: &inputResearchRecord{RunID: "fixture", RecipeVersion: 1, Entity: inputResearchEntity{ID: "entity-" + name, Name: name, Kind: "company", URL: "https://example.com/" + name, Fields: map[string]inputResearchValue{}}}})
	}
	if _, e = writeInputPreparation(root, doc); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken, doc.ID)
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("browser: %s %v", output, e)
	} else {
		t.Log(string(output))
	}
}
