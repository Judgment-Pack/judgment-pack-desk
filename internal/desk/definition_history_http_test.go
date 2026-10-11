//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package desk

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestDefinitionHistoryGuardAndContainment(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	ts, rig, _, audit := trailDesk(t, auditedConfig)
	path := filepath.Join(audit, "evaluations.jsonl")
	raw := `{"recordVersion":"1","pack":{"id":"one"},"inputs":{"n":9007199254740993}}`
	if err := os.WriteFile(path, []byte(raw+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	status, _ := reviewCall(t, ts, "GET", "/api/definitions/history?kind=pack&id=one", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated history: %d", status)
	}
	status, data := reviewCall(t, ts, "GET", "/api/definitions/history?kind=pack&id=one", "", nil, bearer)
	var result definitionRecords
	if status != http.StatusOK || json.Unmarshal(data, &result) != nil || len(result.Records) != 1 || result.Records[0].Raw != raw {
		t.Fatalf("history: %d %s", status, data)
	}
	for _, query := range []string{"kind=pack&id=one&id=two", "kind=pack&id=one&path=elsewhere", "kind=pack&id=one&before=-1", "kind=pack&id=one&bad=%zz"} {
		status, _ = reviewCall(t, ts, "GET", "/api/definitions/history?"+query, "", nil, bearer)
		if status != http.StatusBadRequest {
			t.Fatalf("query accepted: %s %d", query, status)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "private.jsonl")
	if err := os.WriteFile(external, []byte(raw+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, path); err != nil {
		t.Fatal(err)
	}
	status, _ = reviewCall(t, ts, "GET", "/api/definitions/history?kind=pack&id=one", "", nil, bearer)
	if status != http.StatusForbidden {
		t.Fatalf("linked history exposed: %d", status)
	}
	if calls := rig.ran(t); len(calls) != 0 {
		t.Fatalf("history must not evaluate or verify: %v", calls)
	}
}
