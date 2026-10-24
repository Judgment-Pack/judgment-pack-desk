package desk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func enrichedMemoryFixture() memoryEntry {
	entry := manualMemoryFixture()
	entry.Source, entry.Dependencies = nil, nil
	entry.Text = "Keep outreach concise."
	entry.Canonical = &memoryCanonical{ID: "proposed-outreach", Kind: "preference", Source: contract.MemorySource{Run: "original-agent-run", ProposedBy: "model"}, Digest: "sha256:" + runcontext.ContextDigest(entry.Text)}
	return entry
}

func seedCanonicalMemory(t *testing.T, s *Server, entries ...memoryEntry) {
	t.Helper()
	assistantRunMust(t, s.withMemory(func(root *os.Root, reply *memoryReply) error {
		reply.Content.Settings.Use = true
		reply.Content.Entries = entries
		raw, err := json.Marshal(reply.Content)
		if err != nil {
			return err
		}
		if err = validateMemoryStorage(raw); err != nil {
			return err
		}
		return writePrivateData(root, reply.Name, raw)
	}))
}

func TestAssistantMemoryPrivateCanonicalReaders(t *testing.T) {
	s := promptFixture(t, "")
	legacy, enriched := manualMemoryFixture(), enrichedMemoryFixture()
	legacy.Dependencies = nil
	enriched.ID = "enriched"
	seedCanonicalMemory(t, s, legacy, enriched)
	var before []byte
	assistantRunMust(t, s.withMemory(func(root *os.Root, reply *memoryReply) error {
		var err error
		before, err = readPrivateData(root, reply.Name, memoryLimit)
		if err != nil {
			return err
		}
		if err = validateStorageFile(reply.Name, before); err != nil {
			return err
		}
		if !reflect.DeepEqual(reply.Content.Entries, []memoryEntry{legacy, enriched}) {
			t.Fatal("private roundtrip changed metadata")
		}
		return nil
	}))
	host, err := s.assistantPromptHost(t.Context())
	assistantRunMust(t, err)
	_, err = host.MemoryFor(t.Context(), "outreach")
	assistantRunMust(t, err)
	assistantRunMust(t, s.withMemory(func(root *os.Root, reply *memoryReply) error {
		after, err := readPrivateData(root, reply.Name, memoryLimit)
		if string(before) != string(after) {
			t.Fatal("read wrote private storage")
		}
		return err
	}))
	background, err := s.memoryBackground("outreach")
	assistantRunMust(t, err)
	if strings.Contains(background, "original-agent-run") || strings.Contains(background, "proposed-outreach") || strings.Contains(background, "canonical") {
		t.Fatal("private metadata leaked into legacy recalled text", background)
	}
	for _, kind := range []string{"fact", "preference", "decision", "reference"} {
		e := enrichedMemoryFixture()
		e.Canonical.Kind = kind
		record, err := assistantMemoryRecord("project", e)
		assistantRunMust(t, err)
		if record.ID != e.Canonical.ID || record.Text != e.Text || record.Digest != e.Canonical.Digest || record.Kind != kind || record.Source != e.Canonical.Source {
			t.Fatal("canonical proposal rewritten", record)
		}
	}
	for _, author := range []string{"host", "person", "model"} {
		entry := enrichedMemoryFixture()
		entry.Canonical.Source.ProposedBy = author
		raw, _ := json.Marshal(memoryDocument{Version: 1, Entries: []memoryEntry{entry}, Blocked: []string{}})
		assistantRunMust(t, validateMemoryStorage(raw))
	}
	duplicate := enrichedMemoryFixture()
	duplicate.ID = "another-private-row"
	raw, _ := json.Marshal(memoryDocument{Version: 1, Entries: []memoryEntry{enrichedMemoryFixture(), duplicate}, Blocked: []string{}})
	if validateMemoryStorage(raw) == nil {
		t.Fatal("duplicate canonical identity admitted")
	}
	for _, mutate := range []func(*memoryEntry){
		func(e *memoryEntry) { e.Canonical.ID = "" },
		func(e *memoryEntry) { e.Canonical.Kind = "other" },
		func(e *memoryEntry) { e.Canonical.Source.Run = "" },
		func(e *memoryEntry) { e.Canonical.Source.ProposedBy = "other" },
		func(e *memoryEntry) { e.Canonical.Digest = "sha256:" + strings.Repeat("0", 64) },
		func(e *memoryEntry) { e.Text += " changed" },
		func(e *memoryEntry) { expiry := "yesterday"; e.Canonical.ExpiresAt = &expiry },
	} {
		e := enrichedMemoryFixture()
		mutate(&e)
		raw, _ := json.Marshal(memoryDocument{Version: 1, Entries: []memoryEntry{e}, Blocked: []string{}})
		if validateMemoryStorage(raw) == nil || validateStorageFile(s.memoryName(), raw) == nil {
			t.Fatal("malformed enriched storage accepted", string(raw))
		}
	}
	for _, bad := range []string{`null`, `{}`, `{"id":"x","unknown":true}`} {
		raw := []byte(`{"version":1,"settings":{"use":true,"suggest":false},"entries":[{"id":"one","key":"outreach","text":"Concise","state":"active","createdAt":"2026-10-22T10:00:00Z","updatedAt":"2026-10-22T10:00:00Z","canonical":` + bad + `}],"blocked":[]}`)
		if validateMemoryStorage(raw) == nil {
			t.Fatal("malformed metadata accepted", bad)
		}
	}
	for _, bad := range []string{`"expiresAt":null,`, `"unknown":true,`, `"source":null,`} {
		raw, _ := json.Marshal(memoryDocument{Version: 1, Entries: []memoryEntry{enrichedMemoryFixture()}, Blocked: []string{}})
		raw = []byte(strings.Replace(string(raw), `"canonical":{`, `"canonical":{`+bad, 1))
		if validateMemoryStorage(raw) == nil {
			t.Fatal("closed metadata accepted", bad)
		}
	}
}

func TestAssistantMemoryCanonicalPendingApproval(t *testing.T) {
	s := promptFixture(t, "")
	entry := enrichedMemoryFixture()
	entry.State = "pending"
	seedCanonicalMemory(t, s, entry)
	host, err := s.assistantPromptHost(t.Context())
	assistantRunMust(t, err)
	answer, err := host.MemoryFor(t.Context(), "outreach")
	assistantRunMust(t, err)
	if len(answer.([]contract.Memory)) != 0 {
		t.Fatal("pending canonical proposal was automatically accepted")
	}
	changeMemory(t, s, memoryChange{Action: "approve", ID: entry.ID})
	answer, err = host.MemoryFor(t.Context(), "outreach")
	assistantRunMust(t, err)
	rows := answer.([]contract.Memory)
	if len(rows) != 1 || rows[0].ID != entry.Canonical.ID || rows[0].Digest != entry.Canonical.Digest {
		t.Fatal("explicit unchanged approval replaced proposed identity", rows)
	}
}

func memoryCanonicalSession(t *testing.T, s *Server, saved *contract.Checkpoint, enabled bool, query string, native bool) runcontext.Session {
	t.Helper()
	host, err := s.assistantPromptHost(t.Context())
	assistantRunMust(t, err)
	session := runcontext.Session{Prompt: query, Critic: "off", Host: host, Context: &runcontext.Input{Phase: "work", Execution: saved, Memory: &enabled}}
	if native {
		session.Agent = &runcontext.AgentSession{Model: "fixture"}
	} else {
		session.Model = &runcontext.ModelSession{Family: "openai-compatible", Model: "fixture"}
	}
	return session
}

func memoryReload(t *testing.T, checkpoint contract.Checkpoint) *contract.Checkpoint {
	t.Helper()
	raw, err := json.Marshal(checkpoint)
	assistantRunMust(t, err)
	value, err := runcontext.ParseJSON(raw)
	assistantRunMust(t, err)
	saved, err := runcontext.ReadExecution(value)
	assistantRunMust(t, err)
	return saved
}

func TestAssistantMemoryCanonicalResumeLifecycle(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, action := range []string{"forget", "clear", "edit", "disable", "dependency", "expiry"} {
			t.Run(action+map[bool]string{false: "-api", true: "-native"}[native], func(t *testing.T) {
				s := promptFixture(t, "")
				entry := enrichedMemoryFixture()
				if action == "dependency" {
					assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "notes.txt"), []byte("first"), 0600))
					entry.Dependencies = []memoryDependency{{Path: "notes.txt", SHA256: digestOf([]byte("first"))}}
				}
				seedCanonicalMemory(t, s, entry)
				run, err := runcontext.ContextualSession(t.Context(), memoryCanonicalSession(t, s, nil, true, "outreach", native))
				assistantRunMust(t, err)
				before, err := run.Context.Snapshot("stopped")
				assistantRunMust(t, err)
				if len(before.Memories.Recalled) != 1 || before.Memories.Recalled[0].ID != entry.Canonical.ID {
					t.Fatal("canonical identity omitted", before.Memories)
				}
				frozen := run.Context.System("guidance", "")
				switch action {
				case "forget":
					changeMemory(t, s, memoryChange{Action: "forget", ID: entry.ID})
				case "clear":
					changeMemory(t, s, memoryChange{Action: "clear"})
				case "edit":
					changeMemory(t, s, memoryChange{Action: "edit", ID: entry.ID, Text: "Updated outreach guidance."})
				case "disable":
					changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Use: false}})
				case "dependency":
					assistantRunMust(t, os.WriteFile(filepath.Join(s.projectDir, "notes.txt"), []byte("second"), 0600))
				case "expiry":
					expired := "2020-01-01T00:00:00Z"
					entry.Canonical.ExpiresAt = &expired
					seedCanonicalMemory(t, s, entry)
				}
				if run.Context.System("guidance", "") != frozen {
					t.Fatal("management changed live immutable recall")
				}
				run.Context.Close()
				saved := memoryReload(t, before)
				resumed, err := runcontext.ContextualSession(t.Context(), memoryCanonicalSession(t, s, saved, action != "disable", "outreach", native))
				assistantRunMust(t, err)
				defer resumed.Context.Close()
				after, err := resumed.Context.Snapshot("stopped")
				assistantRunMust(t, err)
				want := "revoked"
				if action == "irrelevant" {
					want = "accepted"
				}
				if action == "expiry" {
					want = "expired"
				}
				if len(after.Memories.Recalled) == 0 || after.Memories.Recalled[0].ID != entry.Canonical.ID || after.Memories.Recalled[0].State != want || after.Memories.Recalled[0].Digest != entry.Canonical.Digest {
					t.Fatal("lifecycle lost", after.Memories)
				}
				if strings.Contains(resumed.Context.System("guidance", ""), entry.Text) {
					t.Fatal("prior provenance reconstructed deleted or irrelevant text")
				}
				if memoryReload(t, after).Memories.Recalled[0].State != want {
					t.Fatal("lifecycle lost on reload")
				}
			})
		}
	}
}

func TestAssistantMemoryLifecycleSnapshotAndIdentity(t *testing.T) {
	s := promptFixture(t, "")
	entry := enrichedMemoryFixture()
	seedCanonicalMemory(t, s, entry)
	host, err := s.assistantPromptHost(t.Context())
	assistantRunMust(t, err)
	_, err = host.MemoryLifecycle(t.Context(), nil)
	assistantRunMust(t, err)
	changeMemory(t, s, memoryChange{Action: "edit", ID: entry.ID, Text: "Changed outreach text."})
	prior := []contract.ProvenanceMemoryRecall{{ID: entry.Canonical.ID, Digest: entry.Canonical.Digest, Kind: entry.Canonical.Kind, State: "accepted"}}
	// A child sharing this host may assemble between the parent's callbacks.
	// It must not replace the parent's already held execution snapshot.
	shared, err := host.MemoryLifecycle(t.Context(), prior)
	assistantRunMust(t, err)
	if len(shared.([]contract.MemoryLifecycleUpdate)) != 0 {
		t.Fatal("shared callback replaced the execution snapshot")
	}
	answer, err := host.MemoryFor(t.Context(), "outreach")
	assistantRunMust(t, err)
	if answer.([]contract.Memory)[0].Text != entry.Text {
		t.Fatal("lifecycle and recall used different snapshots")
	}
	next, err := s.assistantPromptHost(t.Context())
	assistantRunMust(t, err)
	answer, err = next.MemoryFor(t.Context(), "outreach")
	assistantRunMust(t, err)
	row := answer.([]contract.Memory)[0]
	if row.ID == entry.Canonical.ID || row.Digest == entry.Canonical.Digest || row.Source.ProposedBy != "host" {
		t.Fatal("edit reused canonical identity", row)
	}
	updates, err := next.MemoryLifecycle(context.Background(), prior)
	assistantRunMust(t, err)
	if len(updates.([]contract.MemoryLifecycleUpdate)) != 1 {
		t.Fatal("old edited identity not revoked")
	}
}

func TestAssistantMemoryQueryOmissionDoesNotRevoke(t *testing.T) {
	s := promptFixture(t, "")
	entry := enrichedMemoryFixture()
	seedCanonicalMemory(t, s, entry)
	host, err := s.assistantPromptHost(t.Context())
	assistantRunMust(t, err)
	prior := []contract.ProvenanceMemoryRecall{{ID: entry.Canonical.ID, Digest: entry.Canonical.Digest, Kind: entry.Canonical.Kind, State: "accepted"}}
	updates, err := host.MemoryLifecycle(t.Context(), prior)
	assistantRunMust(t, err)
	answer, err := host.MemoryFor(t.Context(), "unrelated astronomy")
	assistantRunMust(t, err)
	if len(answer.([]contract.Memory)) != 0 || len(updates.([]contract.MemoryLifecycleUpdate)) != 0 {
		t.Fatal("ordinary relevance omission revoked an eligible identity", updates, answer)
	}
}

func TestAssistantMemorySharedHostConcurrentSnapshots(t *testing.T) {
	s := promptFixture(t, "")
	entry := enrichedMemoryFixture()
	seedCanonicalMemory(t, s, entry)
	host, err := s.assistantPromptHost(t.Context())
	assistantRunMust(t, err)
	_, err = host.MemoryLifecycle(t.Context(), nil)
	assistantRunMust(t, err)
	changeMemory(t, s, memoryChange{Action: "forget", ID: entry.ID})
	prior := []contract.ProvenanceMemoryRecall{{ID: entry.Canonical.ID, Digest: entry.Canonical.Digest, Kind: entry.Canonical.Kind, State: "accepted"}}
	results := make(chan bool, 12)
	for range 12 {
		go func() {
			updates, err := host.MemoryLifecycle(t.Context(), prior)
			if err != nil || len(updates.([]contract.MemoryLifecycleUpdate)) != 0 {
				results <- false
				return
			}
			answer, err := host.MemoryFor(t.Context(), "outreach")
			if err != nil {
				results <- false
				return
			}
			rows := answer.([]contract.Memory)
			results <- len(rows) == 1 && rows[0].ID == entry.Canonical.ID && rows[0].Text == entry.Text
		}()
	}
	for range 12 {
		if !<-results {
			t.Fatal("shared host callbacks replaced or changed frozen recall")
		}
	}
}

func TestAssistantMemoryConcurrentManagementAndRead(t *testing.T) {
	s, ts, _ := assistantServer(t)
	seedCanonicalMemory(t, s, enrichedMemoryFixture())
	var revision string
	assistantRunMust(t, s.withMemory(func(_ *os.Root, reply *memoryReply) error { revision = reply.SHA256; return nil }))
	get, err := http.NewRequest("GET", ts.URL+"/api/ai-memory", nil)
	assistantRunMust(t, err)
	bearer(get)
	response, err := ts.Client().Do(get)
	assistantRunMust(t, err)
	var read memoryReply
	assistantRunMust(t, json.NewDecoder(response.Body).Decode(&read))
	assistantRunMust(t, response.Body.Close())
	if response.StatusCode != 200 || read.SHA256 != revision || read.Content.Entries[0].Canonical.ID != "proposed-outreach" {
		t.Fatal("GET changed or dropped private metadata", read)
	}
	assistantRunMust(t, s.withMemory(func(_ *os.Root, reply *memoryReply) error {
		if reply.SHA256 != revision {
			t.Fatal("GET silently wrote private metadata")
		}
		return nil
	}))
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, text := range []string{"First outreach revision.", "Second outreach revision."} {
		go func(text string) {
			<-start
			body, _ := json.Marshal(memoryChange{Action: "edit", ID: "manual-one", Text: text})
			request, err := http.NewRequest("POST", ts.URL+"/api/ai-memory", strings.NewReader(string(body)))
			if err != nil {
				t.Error(err)
				results <- 0
				return
			}
			bearer(request)
			request.Header.Set("If-Match", revision)
			response, err := ts.Client().Do(request)
			if err != nil {
				t.Error(err)
				results <- 0
				return
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			results <- response.StatusCode
		}(text)
	}
	close(start)
	// Read concurrently through the same canonical private transaction path.
	host, err := s.assistantPromptHost(t.Context())
	assistantRunMust(t, err)
	_, err = host.MemoryFor(t.Context(), "outreach")
	assistantRunMust(t, err)
	first, second := <-results, <-results
	if !((first == 200 && second == 409) || (first == 409 && second == 200)) {
		t.Fatal("concurrent edit bypassed CAS", first, second)
	}
	assistantRunMust(t, s.withMemory(func(_ *os.Root, reply *memoryReply) error {
		if len(reply.Content.Entries) != 1 || reply.Content.Entries[0].Canonical != nil {
			t.Fatal("edit lost row or reused canonical metadata", reply.Content)
		}
		return nil
	}))
}

func TestAssistantMemoryCanonicalStorageLifecycle(t *testing.T) {
	for _, operation := range []string{"backup", "project-move"} {
		t.Run(operation, func(t *testing.T) {
			original, ts, _ := assistantServer(t)
			entry := enrichedMemoryFixture()
			seedCanonicalMemory(t, original, entry)
			var restored *Server
			if operation == "backup" {
				archive := backupBytes(t, ts)
				cfg := original.cfg
				cfg.DeskConfigDir = t.TempDir()
				var restoredHTTP *httptest.Server
				restored, restoredHTTP = startDesk(t, cfg)
				t.Cleanup(func() { _ = restored.Close() })
				t.Cleanup(restoredHTTP.Close)
				empty := storageGet(t, restoredHTTP)
				if status, body := restoreBackup(t, restoredHTTP, moveChatData{Path: filepath.Join(t.TempDir(), "restored"), Revision: empty.Revision}, archive); status != 200 {
					t.Fatal(status, body)
				}
			} else {
				destination := filepath.Join(t.TempDir(), "moved")
				request := movePreview(t, ts, destination)
				if status, body := moveRequest(t, ts, "move", request); status != 200 {
					t.Fatal(status, string(body))
				}
				restored, _, _ = startedAt(t, original, destination)
			}
			host, err := restored.assistantPromptHost(t.Context())
			assistantRunMust(t, err)
			answer, err := host.MemoryFor(t.Context(), "outreach")
			assistantRunMust(t, err)
			rows := answer.([]contract.Memory)
			if len(rows) != 1 || rows[0].ID != entry.Canonical.ID || rows[0].Digest != entry.Canonical.Digest || rows[0].Source != entry.Canonical.Source {
				t.Fatal("storage lifecycle changed canonical identity", rows)
			}
		})
	}
}
