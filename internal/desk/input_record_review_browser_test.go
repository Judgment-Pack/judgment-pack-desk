package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Runs real review writes against disposable records and storage. No AI provider
// or user's workspace, credentials or review decisions are used.
func TestInputRecordReviewBrowser(t *testing.T) {
	script := os.Getenv("JPS_INPUT_REVIEW_BROWSER")
	if script == "" {
		t.Skip("set JPS_INPUT_REVIEW_BROWSER after building web/dist")
	}
	s, ts := startDesk(t, Config{Static: os.DirFS("../../web/dist"), ProjectDir: t.TempDir(), DeskConfigDir: t.TempDir(), Token: testToken})
	defer ts.Close()
	defer s.Close()
	root, err := s.openInputPreparations(true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	doc := inputPreparation{Version: 1, ID: "prep_" + strings.Repeat("e", 32), Name: "Review selection", RecipeVersion: 1, Recipe: inputRecipe{Intent: "Review example companies"}, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), Records: []preparedInputRecord{}, Imports: []inputImport{}, Activity: []inputActivity{}}
	for i := 1; i <= 7; i++ {
		name := fmt.Sprintf("Example company %d", i)
		doc.Records = append(doc.Records, preparedInputRecord{ID: fmt.Sprintf("row-%d", i), Name: name, Class: "generated", Status: "researched", Research: &inputResearchRecord{RunID: "fixture-run", RecipeVersion: 1, Entity: inputResearchEntity{ID: fmt.Sprintf("entity-%d", i), Kind: "company", Name: name, URL: "https://example.com/", Fields: map[string]inputResearchValue{"employee_count": {Status: "missing", Note: "No public employee count was retained."}, "location_status": {Status: "supported", Value: json.RawMessage(`"match"`), Sources: []inputResearchCitation{{SourceID: "fixture-source", Quote: "The office is located in Canada."}}}, "routing_status": {Status: "interpretation", Value: json.RawMessage(`"documented"`), Note: "Published contact categories suggest routing; internal assignment is not established."}, "intake_channels": {Status: "supported", Value: json.RawMessage(`"Appointment contact form, office phone and separate billing, general-inquiry and patient-access contacts."`), Sources: []inputResearchCitation{{SourceID: "fixture-source", Quote: "Use the appointment form, call the office, or contact our billing and general-inquiry teams."}}}}}}})
	}
	if _, err = writeInputPreparation(root, doc); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, ts.URL, testToken, doc.ID)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %s %v", out, err)
	} else {
		t.Log(string(out))
	}
}
