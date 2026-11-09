package desk

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func manualMemoryFixture() memoryEntry {
	return memoryEntry{ID: "manual-one", Key: "outreach", Text: "Keep absent evidence explicit. 😀", State: "active", CreatedAt: "2026-10-22T10:00:00Z", UpdatedAt: "2026-10-23T10:00:00Z", Source: &memorySource{ChatID: "saved-chat", TurnID: "saved-turn"}, SourceDigest: strings.Repeat("a", 64), Dependencies: []memoryDependency{{Path: "notes.txt", SHA256: strings.Repeat("b", 64)}}}
}

func TestAssistantMemoryRecordsIdentityAndProvenance(t *testing.T) {
	entry := manualMemoryFixture()
	read := func(project string, e memoryEntry) contract.Memory {
		t.Helper()
		rows, err := assistantMemoryRecords(project, []memoryEntry{e})
		if err != nil || len(rows) != 1 {
			t.Fatalf("projection: %#v %v", rows, err)
		}
		return rows[0]
	}
	original := read("project-alpha", entry)
	if !reflect.DeepEqual(original, read("project-alpha", entry)) {
		t.Fatal("identity changed for identical stored version")
	}
	sum := sha256.Sum256([]byte("outreach: Keep absent evidence explicit. 😀"))
	if original.Text != "outreach: Keep absent evidence explicit. 😀" || original.Digest != "sha256:"+hex.EncodeToString(sum[:]) || original.Kind != "reference" || original.Scope != "project" || original.State != "accepted" || original.CreatedAt != entry.CreatedAt || original.Source.ProposedBy != "host" {
		t.Fatalf("manual provenance changed: %#v", original)
	}
	if !strings.HasPrefix(original.ID, "desk-memory:") || !strings.HasPrefix(original.Source.Run, "desk-memory-import:") || len(original.ID) > 100 || len(original.Source.Run) > 100 {
		t.Fatal("unbounded or historical run identity", original)
	}
	variants := []struct {
		name   string
		change func(*memoryEntry)
	}{
		{"id", func(e *memoryEntry) { e.ID = "manual-two" }}, {"key", func(e *memoryEntry) { e.Key = "outreach-style" }},
		{"text", func(e *memoryEntry) { e.Text += " revised" }}, {"created", func(e *memoryEntry) { e.CreatedAt = "2026-10-21T10:00:00Z" }},
		{"updated", func(e *memoryEntry) { e.UpdatedAt = "2026-10-24T10:00:00Z" }}, {"source", func(e *memoryEntry) { e.Source = &memorySource{ChatID: "another-chat", TurnID: "saved-turn"} }},
		{"source absence", func(e *memoryEntry) { e.Source = nil }}, {"source digest", func(e *memoryEntry) { e.SourceDigest = strings.Repeat("c", 64) }},
		{"dependency", func(e *memoryEntry) {
			e.Dependencies = []memoryDependency{{Path: "other.txt", SHA256: strings.Repeat("b", 64)}}
		}},
	}
	for _, tc := range variants {
		t.Run(tc.name, func(t *testing.T) {
			next := entry
			tc.change(&next)
			row := read("project-alpha", next)
			if row.ID == original.ID || row.Source.Run == original.Source.Run {
				t.Fatal("stored version reused import identity")
			}
		})
	}
	other := read("project-beta", entry)
	if other.ID == original.ID || other.Source.Run == original.Source.Run {
		t.Fatal("cross-project import identity")
	}
}

func TestAssistantMemoryRecordsCanonicalBudgetAndCount(t *testing.T) {
	one := manualMemoryFixture()
	one.Text = strings.Repeat("é", 700)
	one.Dependencies = nil
	one.Source = nil
	two := one
	two.ID = "manual-two"
	two.Key = "second"
	two.Text = "x"
	measure := func(entries []memoryEntry) int {
		t.Helper()
		rows, err := assistantMemoryRecords("project", entries)
		if err != nil {
			t.Fatal(err)
		}
		recall, err := runcontext.RecallMemory(rows, memoryLimit, "")
		if err != nil {
			t.Fatal(err)
		}
		return recall.Bytes
	}
	// Construct exact and one-byte-over boundaries using released canonical accounting.
	cost := measure([]memoryEntry{one}) + measure([]memoryEntry{two})
	two.Text += strings.Repeat("x", runcontext.Defaults.MemoryBytes-cost)
	exact, err := assistantMemoryRecords("project", []memoryEntry{one, two})
	if err != nil || len(exact) != 2 {
		t.Fatal("exact bound", exact, err)
	}
	recall, err := runcontext.RecallMemory(exact, runcontext.Defaults.MemoryBytes, "")
	if err != nil || recall.Bytes != runcontext.Defaults.MemoryBytes {
		t.Fatal("canonical byte bound", recall.Bytes, err)
	}
	two.Text += "x"
	small := one
	small.ID = "small"
	small.Text = "small outreach note"
	rows, err := assistantMemoryRecords("project", []memoryEntry{one, two, small})
	if err != nil || len(rows) != 2 || rows[1].Text != "outreach: small outreach note" {
		t.Fatal("skip-to-fit order", rows, err)
	}
	// Escaping can make a privately valid 2,000-byte entry exceed canonical recall.
	oversized := one
	oversized.Text = strings.Repeat("\n", 1989) + "x"
	rows, err = assistantMemoryRecords("project", []memoryEntry{oversized, small})
	if err != nil || len(rows) != 1 || rows[0].Text != "outreach: small outreach note" {
		t.Fatal("oversized row prevented bounded later recall", rows, err)
	}
	entries := []memoryEntry{}
	for _, id := range []string{"one", "two", "three", "four", "five", "six"} {
		e := small
		e.ID = id
		entries = append(entries, e)
	}
	rows, err = assistantMemoryRecords("project", entries)
	if err != nil || len(rows) != 5 {
		t.Fatal("five-entry bound", rows, err)
	}
}

func TestAssistantMemoryRecordsRefuseMalformedTimestamp(t *testing.T) {
	for _, date := range []string{"", "yesterday", "2026-02-30T10:00:00Z", "2026-10-22T1:00:00Z"} {
		t.Run(date, func(t *testing.T) {
			entry := manualMemoryFixture()
			entry.CreatedAt = date
			rows, err := assistantMemoryRecords("project", []memoryEntry{entry})
			if err == nil || rows != nil {
				t.Fatal("malformed stored history was repaired or recalled", rows, err)
			}
		})
	}
}

func TestAssistantMemoryRecordsPrivateSelection(t *testing.T) {
	s := promptFixture(t, "")
	if err := os.WriteFile(filepath.Join(s.projectDir, "notes.txt"), []byte("current notes"), 0600); err != nil {
		t.Fatal(err)
	}
	active := manualMemoryFixture()
	active.Source = nil
	active.Dependencies = []memoryDependency{{Path: "notes.txt", SHA256: digestOf([]byte("current notes"))}}
	pending := active
	pending.ID = "pending"
	pending.State = "pending"
	superseded := active
	superseded.ID = "superseded"
	superseded.State = "superseded"
	stale := active
	stale.ID = "stale"
	stale.Dependencies = []memoryDependency{{Path: "notes.txt", SHA256: digestOf([]byte("old notes"))}}
	unrelated := active
	unrelated.ID = "unrelated"
	unrelated.Key = "astronomy"
	unrelated.Text = "Distant galaxies"
	err := s.withMemory(func(root *os.Root, r *memoryReply) error {
		r.Content.Settings = memorySettings{Use: true, Suggest: false}
		r.Content.Entries = []memoryEntry{pending, stale, superseded, unrelated, active}
		raw, _ := json.Marshal(r.Content)
		if err := validateMemoryStorage(raw); err != nil {
			return err
		}
		return writePrivateData(root, s.memoryName(), raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	host, err := s.assistantPromptHost(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	answer, err := host.MemoryFor(t.Context(), "outreach")
	if err != nil {
		t.Fatal(err)
	}
	rows, ok := answer.([]contract.Memory)
	if !ok || len(rows) != 1 || rows[0].Text != "outreach: "+active.Text {
		t.Fatal("private selection", answer)
	}
	if err := os.WriteFile(filepath.Join(s.projectDir, "notes.txt"), []byte("changed notes"), 0600); err != nil {
		t.Fatal(err)
	}
	answer, err = host.MemoryFor(t.Context(), "outreach")
	if err != nil || len(answer.([]contract.Memory)) != 0 {
		t.Fatal("changed dependency recalled", answer, err)
	}
}
