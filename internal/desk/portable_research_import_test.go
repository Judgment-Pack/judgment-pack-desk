package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// This is fresh execution of one identical unbound definition on separate Desk,
// Runner and Gateway installations. Providers are deterministic test servers;
// this is not a live website or published Exchange-release acceptance claim.
func TestPortableResearchImportOnTwoInstallations(t *testing.T) {
	if os.Getenv("JPS_TEST_GATEWAY_BUNDLE") == "" || os.Getenv("JPACK_RUNNER_TEST_BIN") == "" || os.Getenv("JPACK_BIN") == "" {
		t.Skip("requires pinned Gateway, Runner and Runtime")
	}
	var published json.RawMessage
	var publishedDigest, firstProfileDigest, firstPublic string
	var firstInput json.RawMessage
	var calls atomic.Int32
	for installation := 0; installation < 2; installation++ {
		t.Run([]string{"first", "second"}[installation], func(t *testing.T) {
			s, _, basis := portableResearchFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				portableTestReply(w, `{"kind":"answer","fields":{"eligible":{"status":"found","value":false,"cites":[0]}}}`)
			})
			var original struct {
				Source struct {
					Mapping map[string]any  `json:"mapping"`
					Case    json.RawMessage `json:"case"`
				} `json:"source"`
			}
			if err := json.Unmarshal(basis.Portable.Frozen.Input, &original); err != nil {
				t.Fatal(err)
			}
			source := original.Source.Mapping["sources"].([]any)[0].(map[string]any)
			profileID, profileDigest := source["profile"].(string), source["profileDigest"].(string)
			delete(source, "profile")
			delete(source, "profileDigest")
			delete(source, "webProfiles")
			raw, _ := json.Marshal(original.Source.Mapping)
			_, checked, err := s.checkDialectAuthoring(t.Context(), raw, "")
			if err != nil || checked.Status != "document-valid" {
				t.Fatalf("portable definition rejected: %s %v %+v", checked.Status, err, checked.Diagnostics)
			}
			if installation == 0 {
				published = raw
				publishedDigest = checked.Digest
				firstProfileDigest = profileDigest
				firstPublic = basis.Gateway.Public
			} else {
				if !bytes.Equal(raw, published) || checked.Digest != publishedDigest {
					t.Fatal("installation changed the portable definition")
				}
				if profileDigest == firstProfileDigest || basis.Gateway.Public == firstPublic {
					t.Fatal("fixture did not isolate installation identities")
				}
			}
			input, _ := json.Marshal(map[string]any{"source": map[string]any{"import": map[string]any{"dialect": json.RawMessage(published), "binding": map[string]any{"dialect": publishedDigest, "sources": map[string]any{"research": map[string]any{"profile": profileID, "profileDigest": profileDigest, "webProfiles": map[string]any{}}}}}, "case": original.Source.Case, "sources": map[string]any{}}})
			f := basis.Portable.Frozen
			run, err := s.reviewPortableResearch(t.Context(), input, f.Choice, f.Limits, "", f.SessionScope)
			if err != nil {
				t.Fatal(err)
			}
			store := portableResearchStoreFixture(t, run, false)
			if _, err = store.change(func(r *inputResearchRun) error { r.State = "running"; return nil }); err != nil {
				t.Fatal(err)
			}
			s.executePortableResearch(t.Context(), store, run)
			held, err := store.read()
			if err != nil || held.State != "completed" {
				t.Fatalf("research %s: %s %v", held.State, held.Problem, err)
			}
			completed, err := portableResearchInputWithResponse(held, held.Portable.Response)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				result, err := s.inputRunner(t.Context(), "inputs/preview", completed, "")
				if err != nil || !bytes.Contains(result, []byte(`"eligible":false`)) || !bytes.Contains(result, []byte(`"generatedInfluence":true`)) {
					t.Fatalf("replay failed: %s %v", result, err)
				}
			}
			if installation == 0 {
				firstInput = completed
			} else {
				// Another installation's completed receipt cannot acquire this binding's
				// identity merely by copying the response into its input envelope.
				var foreign, local map[string]json.RawMessage
				_ = json.Unmarshal(firstInput, &foreign)
				_ = json.Unmarshal(completed, &local)
				var oldSource, newSource map[string]json.RawMessage
				_ = json.Unmarshal(foreign["source"], &oldSource)
				_ = json.Unmarshal(local["source"], &newSource)
				newSource["sources"] = oldSource["sources"]
				local["source"], _ = json.Marshal(newSource)
				wrong, _ := json.Marshal(local)
				_, err = s.inputRunner(t.Context(), "inputs/preview", json.RawMessage(wrong), "")
				var refused *inputRunnerError
				if !errors.As(err, &refused) || refused.Status != 422 || refused.Code != "input_preparation_failed" || !strings.HasSuffix(refused.Message, "(profile-mismatch). No decision was requested.") {
					t.Fatalf("expected profile refusal, got %+v", err)
				}
			}
		})
	}
	if calls.Load() != 2 {
		t.Fatalf("replay spent model requests: %d", calls.Load())
	}
}
