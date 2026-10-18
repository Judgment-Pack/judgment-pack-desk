package desk

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Exercise the same journal contract on file and future-hosted storage. No
// browser reconstruction of matrix or signed acquisition bytes is involved.
func TestReviewedInputJobUpgradeRecovery(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
			if bin == "" || runtime == "" {
				t.Skip("requires Runner and Runtime")
			}
			project, config := t.TempDir(), t.TempDir()
			os.Chmod(config, 0700)
			writeProject(t, project, map[string]string{"pack.json": qualificationTestPack, "jpack.json": `{"configVersion":"2","packs":{"example":{"path":"pack.json"}}}`})
			s, ts := startDesk(t, Config{RunnerBin: bin, JpackBin: runtime, ProjectDir: project, DeskConfigDir: config, Token: testToken, RunnerAllowUntestedReleases: true})
			defer ts.Close()
			defer s.Close()
			s.inputEvaluationCancel()
			<-s.inputEvaluationDone
			if backend == "memory" {
				s.evaluationJournals = newMemoryEvaluationJournals()
			}
			call := func(path string, body any, revision string) (int, []byte) {
				t.Helper()
				raw, _ := json.Marshal(body)
				r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
				r.Header.Set("Authorization", "Bearer "+policyTestSession(t, s))
				r.Header.Set("If-Match", revision)
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				return w.Code, w.Body.Bytes()
			}
			var saved inputPreparationReply
			code, raw := call("/api/input-preparations", map[string]any{"name": "Governance", "recipe": inputRecipe{Qualification: qualificationFixture()}}, "")
			if code != 201 {
				t.Fatal(code, string(raw))
			}
			json.Unmarshal(raw, &saved)
			path := "/api/input-preparations/" + saved.Document.ID
			code, raw = call(path+"/imports", map[string]any{"kind": "csv", "name": "records.csv", "content": "Name,Country,License\nExample,CA,present\n"}, saved.SHA256)
			if code != 200 {
				t.Fatal(code, string(raw))
			}
			json.Unmarshal(raw, &saved)
			review := func() inputEvaluation {
				t.Helper()
				code, raw := call(path+"/evaluations", map[string]any{"recordIds": []string{saved.Document.Records[0].ID}}, saved.SHA256)
				if code != 201 {
					t.Fatal(code, string(raw))
				}
				var b inputEvaluation
				json.Unmarshal(raw, &b)
				return b
			}
			first := review()
			jobRaw, err := s.inputRunner(context.Background(), "jobs", map[string]any{"name": "Governance", "releaseId": first.ReleaseID, "reviewed": true}, "")
			if err != nil {
				t.Fatal(err)
			}
			var job struct {
				ID        string `json:"id"`
				ReleaseID string `json:"releaseId"`
				Revision  int    `json:"revision"`
			}
			json.Unmarshal(jobRaw, &job)
			next := review()
			request := map[string]any{"reviewed": true, "upgrade": inputUpgradeReview{JobID: job.ID, BaseReleaseID: job.ReleaseID, BaseRevision: job.Revision}}
			code, raw = call(path+"/evaluations/"+next.ID+"/upgrade", request, saved.SHA256)
			if code != 200 {
				t.Fatal("upgrade", code, string(raw))
			}
			json.Unmarshal(raw, &job)
			// Persist intent, then let Runner commit while Desk loses its acknowledgement.
			candidate := review()
			candidate.Upgrade = &inputUpgradeReview{JobID: job.ID, BaseReleaseID: job.ReleaseID, BaseRevision: job.Revision}
			root, err := s.inputEvaluationRoot(false)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			br, err := openInputEvaluation(root, candidate.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			defer br.Close()
			journal := s.evaluationJournal(br, candidate.ID)
			snapshot, err := journal.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = journal.CompareAndSwap(context.Background(), candidate, snapshot.SHA256); err != nil {
				t.Fatal(err)
			}
			request = map[string]any{"reviewed": true, "upgrade": candidate.Upgrade}
			accepted, err := s.inputRunner(context.Background(), "jobs/"+job.ID+"/upgrade", map[string]any{"reviewed": true, "releaseId": candidate.ReleaseID, "baseReleaseId": job.ReleaseID, "baseRevision": job.Revision}, "")
			if err != nil {
				t.Fatal(err)
			}
			json.Unmarshal(accepted, &job)
			later := review()
			_, err = s.inputRunner(context.Background(), "jobs/"+job.ID+"/upgrade", map[string]any{"reviewed": true, "releaseId": later.ReleaseID, "baseReleaseId": job.ReleaseID, "baseRevision": job.Revision}, "")
			if err != nil {
				t.Fatal(err)
			}
			// Fresh validation now fails, but recovery must still return the exact prior
			// revision and may not upgrade the current job or start record runs.
			os.WriteFile(filepath.Join(project, "jpack.json"), []byte(`{"configVersion":"2","packs":{"example":{"path":"pack.json"}},"audit":{"dir":"changed"}}`), 0600)
			code, raw = call(path+"/evaluations/"+candidate.ID+"/upgrade", request, saved.SHA256)
			if code != 200 || !sameEvaluationValue(json.RawMessage(raw), accepted) {
				t.Fatal("recovery", code, string(raw), string(accepted))
			}

			receiptPath := "/api/operations/jobs/" + candidate.Upgrade.JobID + "/upgrade-receipt?baseRevision=" + strconv.Itoa(candidate.Upgrade.BaseRevision) + "&baseReleaseId=" + candidate.Upgrade.BaseReleaseID + "&releaseId=" + candidate.ReleaseID
			get := httptest.NewRequest("GET", receiptPath, nil)
			get.Header.Set("Authorization", "Bearer "+testToken)
			recorder := httptest.NewRecorder()
			s.ServeHTTP(recorder, get)
			if recorder.Code != 200 || !sameEvaluationValue(json.RawMessage(recorder.Body.Bytes()), accepted) {
				t.Fatal("browser recovery proxy", recorder.Code, recorder.Body.String())
			}
			snapshot, err = journal.Read(context.Background())
			if err != nil || snapshot.Document.ConfirmedAt != "" || len(snapshot.Document.Upgrade.Result) == 0 {
				t.Fatal("receipt or execution state", err, snapshot.Document)
			}
			code, raw = call(path+"/evaluations/"+candidate.ID+"/confirm", map[string]any{"reviewed": true}, saved.SHA256)
			if code != 409 {
				t.Fatal("upgrade review enqueued", code, string(raw))
			}
			code, raw = call(path+"/evaluations/"+later.ID+"/upgrade", map[string]any{"reviewed": true, "upgrade": inputUpgradeReview{JobID: job.ID, BaseReleaseID: later.ReleaseID, BaseRevision: job.Revision + 1}}, saved.SHA256)
			if code != 409 {
				t.Fatal("stale fresh upgrade admitted", code, string(raw))
			}
			s.resumeInputEvaluations(context.Background())
			runs, err := s.inputRunner(context.Background(), "runs", nil, "")
			if err != nil || !strings.Contains(string(runs), `"items":[]`) {
				t.Fatal("unexpected runs", err, string(runs))
			}
		})
	}
}
