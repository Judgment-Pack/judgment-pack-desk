package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Prompt tests need rooted project/private files, not an HTTP listener or model.
func promptFixture(t *testing.T, sharedConfig string) *Server {
	t.Helper()
	if !custodyChecked {
		t.Skip("private storage requires custody support")
	}
	project := t.TempDir()
	root, err := os.OpenRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	if sharedConfig == "" {
		sharedConfig = filepath.Join(t.TempDir(), "config")
	}
	private := openAssistantStore(sharedConfig)
	if !private.usable() {
		t.Fatal(private.problem)
	}
	t.Cleanup(func() { private.Close() })
	return &Server{cfg: Config{DeskConfigDir: sharedConfig}, configDir: sharedConfig, projectDir: project, root: root, assistant: private, writes: &sync.Mutex{}}
}

func promptProfile(t *testing.T, s *Server, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.projectDir, assistantProfilePath), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAssistantPromptRawPartsAndCanonicalAssembly(t *testing.T) {
	s := promptFixture(t, "")
	const brief = "Exact project background. 😀\n"
	const instructions = "Use concise answers. Keep missing values explicit.\n"
	if err := os.WriteFile(filepath.Join(s.projectDir, "brief.md"), []byte(brief), 0600); err != nil {
		t.Fatal(err)
	}
	promptProfile(t, s, map[string]any{"profileVersion": 1, "context": map[string]any{"version": 1, "projectBrief": "brief.md"}, "projectInstructions": instructions})
	changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Use: true}})
	changeMemory(t, s, memoryChange{Action: "save", Key: "outreach-style", Text: "Outreach notes label missing evidence."})
	host, err := s.assistantPromptHost(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The explicit adapter query is used whole: the legacy message marker must
	// not discard the matching word before it, nor infer another query.
	query := "outreach\n" + personMessageMarker + "unrelated astronomy"
	parts, err := runcontext.ReadHostParts(t.Context(), host, query, runcontext.Defaults, true)
	if err != nil {
		t.Fatal(err)
	}
	if parts.Brief != brief || parts.ProjectInstructions != instructions {
		t.Fatalf("host changed raw parts: %#v", parts)
	}
	rawMemory, err := host.MemoryFor(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	records, ok := rawMemory.([]contract.Memory)
	if !ok || len(records) != 1 || records[0].Text != "outreach-style: Outreach notes label missing evidence." || records[0].State != "accepted" {
		t.Fatalf("explicit query/typed memory: %#v", rawMemory)
	}
	if !strings.Contains(parts.Memory, records[0].Text) || !strings.Contains(parts.Memory, `id: "`+records[0].ID+`"`) {
		t.Fatalf("canonical recalled record framing missing: %q", parts.Memory)
	}
	system := runcontext.SystemText("PACKAGE GUIDANCE", parts, "\nTOOLS")
	markers := []string{"PACKAGE GUIDANCE", runcontext.BriefHeader, brief, runcontext.ProjectInstructionsHeader, instructions, runcontext.MemoryHeader, parts.Memory, "\nTOOLS"}
	last := -1
	for _, marker := range markers {
		at := strings.Index(system, marker)
		if at <= last || strings.Count(system, marker) != 1 {
			t.Fatalf("layer order or duplicate %q", marker)
		}
		last = at
	}
	if strings.Contains(system, "Project background (not approval") || strings.Contains(system, "Recalled project memory (historical") {
		t.Fatal("legacy framing duplicated")
	}
	// Agent does not ask memory at all when a run disables it.
	disabled := runcontext.HostFuncs{BriefFunc: host.Brief, ProjectInstructionsFunc: host.ProjectInstructions, MemoryRecordsForFunc: func(context.Context, string) ([]contract.Memory, error) {
		t.Fatal("memory asked while disabled")
		return nil, nil
	}}
	parts, err = runcontext.ReadHostParts(t.Context(), disabled, query, runcontext.Defaults, false)
	if err != nil || parts.Memory != "" {
		t.Fatal(parts, err)
	}
	changeMemory(t, s, memoryChange{Action: "settings", Settings: &memorySettings{Use: false}})
	if memory, err := host.MemoryFor(t.Context(), query); err != nil || len(memory.([]contract.Memory)) != 0 {
		t.Fatalf("project memory off: %q %v", memory, err)
	}
}

func TestAssistantPromptSelectedProjectIsolationAndSnapshot(t *testing.T) {
	a := promptFixture(t, "")
	b := promptFixture(t, a.configDir)
	promptProfile(t, a, map[string]any{"profileVersion": 2, "inherit": true, "projectInstructions": "Alpha guidance"})
	promptProfile(t, b, map[string]any{"profileVersion": 1, "projectInstructions": "Beta guidance"})
	changeMemory(t, a, memoryChange{Action: "settings", Settings: &memorySettings{Use: true}})
	changeMemory(t, a, memoryChange{Action: "save", Key: "outreach", Text: "Alpha outreach notes."})
	changeMemory(t, b, memoryChange{Action: "settings", Settings: &memorySettings{Use: true}})
	hostA, err := a.assistantPromptHost(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	hostB, err := b.assistantPromptHost(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	promptProfile(t, a, map[string]any{"profileVersion": 1, "projectInstructions": "New run guidance"})
	if got, err := hostA.ProjectInstructions(t.Context()); err != nil || got != "Alpha guidance" {
		t.Fatal(got, err)
	}
	if got, err := hostB.ProjectInstructions(t.Context()); err != nil || got != "Beta guidance" {
		t.Fatal(got, err)
	}
	if got, err := hostB.MemoryFor(t.Context(), "outreach"); err != nil || len(got.([]contract.Memory)) != 0 {
		t.Fatalf("other project memory: %q %v", got, err)
	}
}

func TestAssistantPromptBriefRefusesInvalidSourceUnchanged(t *testing.T) {
	s := promptFixture(t, "")
	promptProfile(t, s, map[string]any{"profileVersion": 1, "context": map[string]any{"version": 1, "projectBrief": "brief.md"}})
	host, err := s.assistantPromptHost(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.projectDir, "brief.md")
	exact := []byte(strings.Repeat("é", runcontext.Defaults.BriefBytes/2))
	if err = os.WriteFile(path, exact, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := host.Brief(t.Context()); err != nil || got != string(exact) {
		t.Fatalf("exact brief byte bound: %v", err)
	}
	for _, raw := range [][]byte{[]byte(strings.Repeat("é", runcontext.Defaults.BriefBytes/2+1)), {0xff}} {
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = host.Brief(t.Context()); err == nil {
			t.Fatal("invalid brief accepted")
		}
		after, _ := os.ReadFile(path)
		if string(after) != string(raw) {
			t.Fatal("invalid source changed")
		}
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "outside.md")
	if err = os.WriteFile(external, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(external, path); err != nil {
		t.Fatal(err)
	}
	if _, err = host.Brief(t.Context()); err == nil {
		t.Fatal("symlink brief accepted")
	}
}

func TestAssistantPromptCancellationAndMemoryLock(t *testing.T) {
	s := promptFixture(t, "")
	host, err := s.assistantPromptHost(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.assistantPromptHost(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, read := range []func(context.Context) (string, error){host.Brief, host.ProjectInstructions, func(ctx context.Context) (string, error) { _, err := host.MemoryFor(ctx, "outreach"); return "", err }} {
		if _, err = read(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	lock, err := s.privateDataLockContext(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	waiting, cancelWaiting := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancelWaiting()
	if _, err = host.MemoryFor(waiting, "outreach"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("memory lock ignored cancellation: %v", err)
	}
}

func TestAssistantProfileInstructionBytesAndSource(t *testing.T) {
	s := promptFixture(t, "")
	limit := runcontext.Defaults.ProjectInstructionsBytes
	for _, version := range []int{1, 2} {
		profile := map[string]any{"profileVersion": version, "projectInstructions": strings.Repeat("é", limit/2)}
		if version == 2 {
			profile["inherit"] = true
		}
		promptProfile(t, s, profile)
		read, err := s.readAssistantProfile()
		if err != nil || read.ProjectInstructions != profile["projectInstructions"] {
			t.Fatal(read, err)
		}
		raw, _ := os.ReadFile(filepath.Join(s.projectDir, assistantProfilePath))
		restored, err := decodeAssistantProfile(raw)
		if err != nil || restored.ProjectInstructions != read.ProjectInstructions {
			t.Fatal(restored, err)
		}
		profile["projectInstructions"] = profile["projectInstructions"].(string) + "x"
		promptProfile(t, s, profile)
		before, _ := os.ReadFile(filepath.Join(s.projectDir, assistantProfilePath))
		if _, err = s.readAssistantProfile(); err == nil {
			t.Fatal("oversized multibyte instructions accepted")
		}
		after, _ := os.ReadFile(filepath.Join(s.projectDir, assistantProfilePath))
		if string(before) != string(after) {
			t.Fatal("invalid profile changed")
		}
	}
	for _, raw := range []string{`{"profileVersion":1,"projectInstructions":null}`, `{"profileVersion":1,"projectInstructions":42}`, `{"profileVersion":1,"projectInstructions":"\ud800"}`, `{"profileVersion":1,"projectInstructions":"\udc00"}`} {
		if _, err := decodeAssistantProfile([]byte(raw)); err == nil {
			t.Fatalf("invalid instructions accepted: %s", raw)
		}
	}
	if _, err := decodeAssistantProfile([]byte(`{"profileVersion":1,"projectInstructions":"\ud83d\ude00"}`)); err != nil {
		t.Fatal("valid surrogate pair refused", err)
	}
	if currentAssistantPromptPolicy().ProjectInstructionsBytes != limit {
		t.Fatal("browser bound differs from Agent")
	}
}
