package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// Exercise actual record validation, publication and the worker's next-request
// decision. Missing evidence is valid output, distinct from an unprocessed row.
func TestResearchSelectedCompletionDoesNotRequestAFinalModelAnswer(t *testing.T) {
	for _, target := range []string{"companies", "people", "records"} {
		for _, scenario := range []string{"updated", "unchanged", "partial", "correction", "publication-failure", "already-published"} {
			t.Run(target+"/"+scenario, func(t *testing.T) {
				s, root, run := researchV2Fixture(t)
				run.Recipe.Target = target
				secondSource := run.Sources[0]
				secondSource.ID = "attempt_" + strings.Repeat("2", 64)
				secondSource.URL = "https://another.example/"
				secondSource.URLs = []string{secondSource.URL}
				run.Sources = append(run.Sources, secondSource)
				if err := writeInputResearch(root, run); err != nil {
					t.Fatal(err)
				}
				makeRecord := func(name string, missing bool) map[string]any {
					record := v2Record(run, name)
					record["kind"] = researchTargetKind(target)
					source := run.Sources[0]
					if name == "Another" {
						source = secondSource
					}
					for _, ref := range researchURLReferences(run) {
						if ref.URL == source.URL {
							record["urlRef"] = ref.Ref
						}
					}
					record["fields"] = map[string]any{"country": map[string]any{"status": "supported", "value": "Canada", "sources": []any{map[string]any{"sourceId": source.ID, "quote": "operates in Canada."}}}}
					if missing {
						record["fields"] = map[string]any{"country": map[string]any{"status": "missing", "sources": []any{}, "note": "The retained evidence does not establish this value."}}
					}
					return record
				}
				run = v2Apply(t, root, makeRecord("Example", scenario == "unchanged"), makeRecord("Another", scenario == "unchanged"))
				if err := s.publishResearchRecords(run); err != nil {
					t.Fatal(err)
				}
				before := v2Rows(t, s, run.PreparationID)
				if len(before.Document.Records) != 2 {
					t.Fatalf("fixture publication failed: %+v", run.Outcomes)
				}
				run.Selection = researchRecordSnapshots(before.Document)
				for i := range run.Selection {
					for _, source := range run.Sources {
						if source.URL == run.Selection[i].Entity.URL {
							run.Selection[i].SourceIDs = []string{source.ID}
						}
					}
				}
				run.SourceRuns = map[string]string{run.Sources[0].ID: run.ID, secondSource.ID: run.ID}
				run.ID = "research_" + strings.Repeat("c", 32)
				run.Reprocess = true
				run.Existing = run.Selection
				run.Records, run.Outcomes, run.Attempts = nil, nil, nil
				run.PublicationPending = false
				run.Recipe.Limits.Records = 2
				store, err := s.inputResearchRoot(false)
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				work, err := openInputResearch(store, run.ID, true)
				if err != nil {
					t.Fatal(err)
				}
				defer work.Close()
				if err = writeInputResearch(work, run); err != nil {
					t.Fatal(err)
				}
				updates := make([]map[string]any, len(run.Selection))
				for i, selected := range run.Selection {
					updates[i] = makeRecord(selected.Entity.Name, true)
					updates[i]["recordId"] = selected.RecordID
				}
				if scenario == "already-published" {
					run = v2Apply(t, work, updates...)
					if err := s.publishResearchRecords(run); err != nil {
						t.Fatal(err)
					}
				}
				ops := recoveryOperations()
				ops.publish = s.publishResearchRecords
				if scenario == "publication-failure" {
					ops.publish = func(inputResearchRun) error { return errors.New("storage unavailable") }
				}
				ops.source = func(context.Context, *os.Root, inputResearchRun, inputResearchAttempt, json.RawMessage) (inputResearchSource, error) {
					t.Fatal("selected reprocessing acquired a source")
					return inputResearchSource{}, nil
				}
				calls := 0
				secondRef := updates[1]["urlRef"]
				ops.model = func(context.Context, inputResearchAI, string, func(bool) error) (json.RawMessage, error) {
					calls++
					if calls == 1 {
						if scenario == "partial" {
							return recoveryAction("records", map[string]any{"records": updates[:1]}), nil
						}
						if scenario == "correction" {
							updates[1]["urlRef"] = "not-retained"
						}
						return recoveryAction("records", map[string]any{"records": updates}), nil
					}
					if calls == 2 && scenario == "correction" {
						current, err := readInputResearch(work)
						if err != nil {
							t.Fatal(err)
						}
						for _, outcome := range current.Outcomes {
							if outcome.State == "needs-correction" {
								updates[1]["correctionOf"] = outcome.Key
							}
						}
						updates[1]["urlRef"] = secondRef
						return recoveryAction("records", map[string]any{"records": updates[1:]}), nil
					}
					if calls == 2 && scenario == "partial" {
						return recoveryAction("finish", map[string]string{"reason": "No usable result for the other selected record."}), nil
					}
					return nil, errors.New("unnecessary model request after completed publication")
				}
				executeInputResearch(context.Background(), work, run, ops)
				finished, err := readInputResearch(work)
				if err != nil {
					t.Fatal(err)
				}
				wantCalls, wantState := 1, "completed-with-gaps"
				if scenario == "partial" || scenario == "correction" {
					wantCalls = 2
				}
				if scenario == "already-published" {
					wantCalls = 0
				}
				if scenario == "publication-failure" {
					wantState = "blocked"
				}
				if calls != wantCalls || finished.State != wantState {
					t.Fatalf("calls=%d (want %d), state=%s (want %s), problem=%s", calls, wantCalls, finished.State, wantState, finished.Problem)
				}
				after := v2Rows(t, s, run.PreparationID)
				if len(after.Document.Records) != 2 {
					t.Fatal("reprocessing changed the number of records")
				}
				if scenario == "unchanged" || scenario == "publication-failure" {
					if after.SHA256 != before.SHA256 {
						t.Fatal("no-op or failed publication changed the saved records")
					}
				} else {
					for _, row := range after.Document.Records {
						if scenario != "partial" && row.Research.Entity.Fields["country"].Status != "missing" {
							t.Fatal("completion removed an explicit evidence gap")
						}
						if row.Review != nil {
							t.Fatal("completion approved generated records")
						}
					}
				}
			})
		}
	}
}
