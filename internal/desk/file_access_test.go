package desk

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectFilesReadOnlyOutputs(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", `{"configVersion":"6","audit":{"dir":"records/decisions"},"packs":{}}`)
	for _, tc := range []struct{ path, reason string }{
		{"jpack.lock.json", "runtime-lock"},
		{"audit/evaluations.jsonl", "audit-record"},
		{"records/decisions/signatures.jsonl", "audit-record"},
		{"records/decisions/stamps.jsonl", "audit-record"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			abs := writeProjectFile(t, project, tc.path, "{}\n")
			status, body := getJSON(t, ts, "/api/file?path="+url.QueryEscape(tc.path))
			if status != http.StatusOK || body["readOnlyReason"] != tc.reason {
				t.Fatalf("read: %d %v", status, body)
			}
			for _, override := range []bool{false, true} {
				status, body = putJSON(t, ts, WriteRequest{Path: tc.path, Content: "changed", BaseSHA256: digestOf([]byte("{}\n")), Override: override})
				if status != http.StatusForbidden || body["code"] != CodeForbidden {
					t.Fatalf("write: %d %v", status, body)
				}
			}
			data, err := os.ReadFile(abs)
			if err != nil || string(data) != "{}\n" {
				t.Fatalf("protected bytes changed: %q %v", data, err)
			}
		})
	}
	status, body := getJSON(t, ts, "/api/files")
	if status != http.StatusOK {
		t.Fatal(body)
	}
	for _, raw := range body["files"].([]any) {
		file := raw.(map[string]any)
		if file["path"] == "jpack.json" {
			if file["readOnlyReason"] != nil {
				t.Fatal("authored config marked read-only")
			}
			continue
		}
		if file["readOnlyReason"] == nil {
			t.Fatalf("missing access metadata: %v", file)
		}
	}
}

func TestProjectFilesCannotCreateRuntimeLock(t *testing.T) {
	_, ts, _ := filesServer(t)
	status, body := putJSON(t, ts, WriteRequest{Path: "jpack.lock.json", Content: "{}", Override: true})
	if status != http.StatusForbidden {
		t.Fatalf("create lock: %d %v", status, body)
	}
}

func TestProjectFilesKeepsAuthoredInputsAndJobDraftWorkflowWritable(t *testing.T) {
	_, ts, _ := filesServer(t)
	for _, name := range []string{"jpack.json", "jpack-assistant.json", "evidence.json", "packs/example.pack.json", "notes/evaluations.jsonl", ".desk/job-drafts/example.json"} {
		status, body := putJSON(t, ts, WriteRequest{Path: name, Content: "{}", CreateParents: true})
		if status != http.StatusOK && status != http.StatusCreated {
			t.Fatalf("authored %s: %d %v", name, status, body)
		}
	}
}

// A jpack.json that cannot be read now says nothing about where the records
// are: every file with a record's name stays read-only until it can be read.
// An absent jpack.json declares no directory, and the same name elsewhere is
// an authored file.
func TestProjectFilesHoldRecordsWhenTheConfigurationCannotBeRead(t *testing.T) {
	_, ts, project := filesServer(t)
	abs := writeProjectFile(t, project, "records/decisions/stamps.jsonl", "{}\n")
	write := func() (int, map[string]any) {
		return putJSON(t, ts, WriteRequest{Path: "records/decisions/stamps.jsonl", Content: "changed", BaseSHA256: digestOf([]byte("{}\n"))})
	}
	writeProjectFile(t, project, "jpack.json", `{"configVersion":"6","audit":{"dir":"records/decisions"`)
	status, body := getJSON(t, ts, "/api/file?path="+url.QueryEscape("records/decisions/stamps.jsonl"))
	if status != http.StatusOK || body["readOnlyReason"] != "audit-record" {
		t.Fatalf("read with an unreadable configuration: %d %v", status, body)
	}
	if status, body = write(); status != http.StatusForbidden || body["code"] != CodeForbidden {
		t.Fatalf("write with an unreadable configuration: %d %v", status, body)
	}
	if data, err := os.ReadFile(abs); err != nil || string(data) != "{}\n" {
		t.Fatalf("protected bytes changed: %q %v", data, err)
	}
	if err := os.Remove(filepath.Join(project, "jpack.json")); err != nil {
		t.Fatal(err)
	}
	if status, body = write(); status != http.StatusOK {
		t.Fatalf("write with no configuration: %d %v", status, body)
	}
}

// With the published runtime: what the runtime itself wrote — the lock and a
// deciding run's record in the declared directory — is listed and read as
// read-only and refused on write, byte for byte unchanged, while the
// configuration and the pack beside them stay editable. The project's path
// holds a space, a tab and U+2028; no answer carries it.
func TestProjectFilesHoldTheRuntimesOutputsWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	project := filepath.Join(t.TempDir(), "a project\twith breaks")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	config := `{"configVersion":"4","audit":{"dir":"records/decisions"},"packs":{"alpha":{"path":"packs/a.json"}}}` + "\n"
	writeProject(t, project, map[string]string{"packs/a.json": reviewPack, "jpack.json": config})
	jpackIn(t, bin, project, "packs", "lock", "--config", "jpack.json", "--format", "json")
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	jpackIn(t, bin, project, "experimental", "evaluate", "--config", "jpack.json", "--pack-id", "alpha", "--facts", facts, "--format", "json")

	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, Logger: log.New(io.Discard, "", 0), DeskConfigDir: t.TempDir()})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)

	status, listing := getJSON(t, ts, "/api/files")
	if status != http.StatusOK {
		t.Fatalf("listing: %d %v", status, listing)
	}
	reasons := map[string]any{}
	for _, raw := range listing["files"].([]any) {
		file := raw.(map[string]any)
		reasons[file["path"].(string)] = file["readOnlyReason"]
	}
	want := map[string]any{"jpack.json": nil, "packs/a.json": nil, "jpack.lock.json": "runtime-lock", "records/decisions/evaluations.jsonl": "audit-record"}
	if len(reasons) != len(want) {
		t.Fatalf("listed %v, want %v", reasons, want)
	}
	for name, reason := range want {
		if got, listed := reasons[name]; !listed || got != reason {
			t.Fatalf("%s listed with %v, want %v (%v)", name, got, reason, reasons)
		}
	}
	for _, name := range []string{"jpack.lock.json", "records/decisions/evaluations.jsonl"} {
		before, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		status, body := getJSON(t, ts, "/api/file?path="+url.QueryEscape(name))
		if status != http.StatusOK || body["readOnlyReason"] != want[name] || body["content"] != string(before) {
			t.Fatalf("read %s: %d %v", name, status, body["readOnlyReason"])
		}
		for _, override := range []bool{false, true} {
			status, body = putJSON(t, ts, WriteRequest{Path: name, Content: "{}\n", BaseSHA256: digestOf(before), Override: override})
			if status != http.StatusForbidden || body["code"] != CodeForbidden {
				t.Fatalf("write %s: %d %v", name, status, body)
			}
			if strings.Contains(fmt.Sprint(body), project) {
				t.Fatalf("the refusal names the project's path: %v", body)
			}
		}
		after, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("%s changed: %v", name, err)
		}
	}
	pack, err := os.ReadFile(filepath.Join(project, "packs", "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if status, body := putJSON(t, ts, WriteRequest{Path: "packs/a.json", Content: string(pack), BaseSHA256: digestOf(pack)}); status != http.StatusOK {
		t.Fatalf("the pack is not editable: %d %v", status, body)
	}
}

// Review round 1, finding 1. On a case-insensitive filesystem (APFS, NTFS)
// every one of these names the lock or a record, so each is held whatever its
// case, and a declared directory is matched whatever its case. Decided by
// names alone (no file to compare), so it runs the same on Linux.
func TestReadOnlyNamesAreComparedWithoutCase(t *testing.T) {
	p := fileAccessPolicy{auditDir: "Records/Decisions"}
	for name, want := range map[string]string{
		"jpack.lock.json":                      "runtime-lock",
		"JPACK.LOCK.JSON":                      "runtime-lock",
		"Jpack.Lock.Json":                      "runtime-lock",
		"AUDIT/evaluations.jsonl":              "audit-record",
		"audit/Evaluations.JSONL":              "audit-record",
		".Desk-Private/AUDIT/signatures.jsonl": "audit-record",
		"Records/Decisions/stamps.jsonl":       "audit-record",
		"records/decisions/STAMPS.jsonl":       "audit-record",
		"notes/evaluations.jsonl":              "",
		"packs/jpack.lock.json":                "",
		"audit/notes.jsonl":                    "",
		"records/evaluations.jsonl":            "",
	} {
		if got := p.readOnlyReason(name, nil); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

// Through the API: a lock or a record cannot be created under another case,
// and a directory declared as `Records` holds the records in `records`.
func TestProjectFilesRefuseTheLockAndRecordsUnderAnotherCase(t *testing.T) {
	_, ts, project := filesServer(t)
	for _, name := range []string{"JPACK.LOCK.JSON", "AUDIT/Evaluations.jsonl", "audit/STAMPS.jsonl"} {
		if status, body := putJSON(t, ts, WriteRequest{Path: name, Content: "{}", CreateParents: true}); status != http.StatusForbidden || body["code"] != CodeForbidden {
			t.Fatalf("create %s: %d %v", name, status, body)
		}
	}
	writeProjectFile(t, project, "jpack.json", `{"configVersion":"6","audit":{"dir":"Records"},"packs":{}}`)
	abs := writeProjectFile(t, project, "records/evaluations.jsonl", "{}\n")
	status, body := putJSON(t, ts, WriteRequest{Path: "records/evaluations.jsonl", Content: "changed", BaseSHA256: digestOf([]byte("{}\n"))})
	if status != http.StatusForbidden || body["code"] != CodeForbidden {
		t.Fatalf("write to the declared directory under another case: %d %v", status, body)
	}
	if data, err := os.ReadFile(abs); err != nil || string(data) != "{}\n" {
		t.Fatalf("protected bytes changed: %q %v", data, err)
	}
}

// A file that is the record, reached by another path (here a hard link, the
// one other spelling Linux has), is the record: compared by identity, it is
// listed, read and refused as one.
func TestProjectFilesHoldTheRecordUnderAnyPathThatReachesIt(t *testing.T) {
	_, ts, project := filesServer(t)
	writeProjectFile(t, project, "jpack.json", `{"configVersion":"6","audit":{"dir":"records/decisions"},"packs":{}}`)
	record := writeProjectFile(t, project, "records/decisions/evaluations.jsonl", "{}\n")
	lock := writeProjectFile(t, project, "jpack.lock.json", "{}\n")
	if err := os.MkdirAll(filepath.Join(project, "notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	for alias, target := range map[string]string{"notes/held.jsonl": record, "notes/lock-copy.json": lock} {
		if err := os.Link(target, filepath.Join(project, filepath.FromSlash(alias))); err != nil {
			t.Skipf("no hard links here: %v", err)
		}
	}
	for alias, reason := range map[string]string{"notes/held.jsonl": "audit-record", "notes/lock-copy.json": "runtime-lock"} {
		status, body := getJSON(t, ts, "/api/file?path="+url.QueryEscape(alias))
		if status != http.StatusOK || body["readOnlyReason"] != reason {
			t.Fatalf("read %s: %d %v", alias, status, body["readOnlyReason"])
		}
		if status, body = putJSON(t, ts, WriteRequest{Path: alias, Content: "changed", BaseSHA256: digestOf([]byte("{}\n"))}); status != http.StatusForbidden {
			t.Fatalf("write %s: %d %v", alias, status, body)
		}
	}
	if data, err := os.ReadFile(record); err != nil || string(data) != "{}\n" {
		t.Fatalf("the record changed: %q %v", data, err)
	}
}
