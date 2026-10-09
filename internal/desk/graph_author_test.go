package desk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const graphRow3Config = "{\n  \"configVersion\": \"2\",\n  \"packs\": {},\n  \"other\": {\"number\":12345678901234567890}\n}\n"
const graphRow3Content = "{\n  \"id\": \"draft\", \"nodes\": []\n}\n"

func graphRow3Desk(t *testing.T, config string) (*Server, *graphRig) {
	t.Helper()
	rig := newGraphRig(t)
	rig.answers(t, "validate", `{"outputVersion":"2","command":"experimental graph validate","status":"valid","kind":"non-normative-runtime-convention","graphSha256":"`+digestOf([]byte(graphRow3Content))+`"}`, 0)
	rig.answers(t, "explain", explainAnswer, 0)
	project := t.TempDir()
	writeProjectFile(t, project, runtimeConfigName, config)
	s, err := New(Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, Port: testPort, DeskConfigDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, rig
}

// Recorder exercises routing, session and origin guards without a listener.
func graphRow3Post(t *testing.T, s *Server, route string, body any, auth bool, origin string) (int, []byte) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://localhost/api/graphs/"+route, strings.NewReader(string(data)))
	if auth {
		bearer(r)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}
func graphRow3Offer(t *testing.T, s *Server, proposal graphProposal) graphWriteOffer {
	t.Helper()
	status, data := graphRow3Post(t, s, "proposal", proposal, true, "")
	if status != 200 {
		t.Fatalf("proposal: %d %s", status, data)
	}
	var offer graphWriteOffer
	if json.Unmarshal(data, &offer) != nil || offer.Token == "" {
		t.Fatalf("no offer: %s", data)
	}
	return offer
}
func graphRow3Proposal() graphProposal {
	return graphProposal{ID: "draft", Path: "draft.graph.json", Content: graphRow3Content, Description: "A proposed composition"}
}

func TestGraphRow3Confirmation(t *testing.T) {
	for _, change := range []string{"none", "bytes", "declaration", "token", "config", "reuse", "failed-use"} {
		t.Run(change, func(t *testing.T) {
			s, _ := graphRow3Desk(t, graphRow3Config)
			offer := graphRow3Offer(t, s, graphRow3Proposal())
			original := offer
			switch change {
			case "bytes":
				offer.Content += " "
			case "declaration":
				offer.ConfigContent += " "
			case "token":
				offer.Token = strings.Repeat("0", 64)
			case "config":
				writeProjectFile(t, s.projectDir, runtimeConfigName, graphRow3Config+" ")
			case "reuse":
				status, data := graphRow3Post(t, s, "write", offer, true, "")
				if status != 200 {
					t.Fatalf("first write: %d %s", status, data)
				}
			case "failed-use":
				offer.Content += " "
				graphRow3Post(t, s, "write", offer, true, "")
				offer = original
			}
			before := treeOf(t, s.projectDir)
			status, data := graphRow3Post(t, s, "write", offer, true, "")
			if change != "none" {
				if status != 409 {
					t.Fatalf("write %s: %d %s", change, status, data)
				}
				sameProject(t, before, treeOf(t, s.projectDir), change)
				return
			}
			if status != 200 {
				t.Fatalf("write: %d %s", status, data)
			}
			graph, _ := os.ReadFile(filepath.Join(s.projectDir, offer.Path))
			config, _ := os.ReadFile(filepath.Join(s.projectDir, runtimeConfigName))
			if string(graph) != offer.Content || string(config) != offer.ConfigContent {
				t.Fatalf("not the bytes shown: %q %q", graph, config)
			}
			if offer.Before != graphRow3Config {
				t.Fatal("the whole base was not shown")
			}
			if !strings.Contains(string(config), `"number":12345678901234567890`) || !strings.Contains(string(config), `"configVersion": "2"`) {
				t.Fatalf("configuration changed: %s", config)
			}
			var decoded map[string]json.RawMessage
			_ = json.Unmarshal(config, &decoded)
			var entries map[string]map[string]any
			_ = json.Unmarshal(decoded["graphs"], &entries)
			if len(entries["draft"]) != 2 || entries["draft"]["rows"] != nil {
				t.Fatal("declaration contains more than path and description")
			}
			if len(treeOf(t, s.projectDir)) != len(before)+1 {
				t.Fatal("wrote something beyond graph and declaration")
			}
		})
	}
}

func TestGraphRow3FolderLock(t *testing.T) {
	s, _ := graphRow3Desk(t, graphRow3Config)
	offer := graphRow3Offer(t, s, graphRow3Proposal())
	release, err := s.lockProject(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	shortProjectWait(t, time.Millisecond)
	before := treeOf(t, s.projectDir)
	status, data := graphRow3Post(t, s, "write", offer, true, "")
	if status != 409 || !strings.Contains(string(data), "another Desk process") {
		t.Fatalf("lock: %d %s", status, data)
	}
	sameProject(t, before, treeOf(t, s.projectDir), "held folder lock")
	release()
	status, _ = graphRow3Post(t, s, "write", offer, true, "")
	if status != 409 {
		t.Fatal("a refused use was not spent")
	}
}

func TestGraphRow3GuardedWriter(t *testing.T) {
	t.Run("create never overrides", func(t *testing.T) {
		s, _ := graphRow3Desk(t, graphRow3Config)
		offer := graphRow3Offer(t, s, graphRow3Proposal())
		writeProjectFile(t, s.projectDir, offer.Path, "another writer")
		before := treeOf(t, s.projectDir)
		status, data := graphRow3Post(t, s, "write", offer, true, "")
		if status != 409 || !strings.Contains(string(data), `"exists"`) || strings.Contains(string(data), "override") || !strings.Contains(string(data), "Review the proposal again; nothing was written.") {
			t.Fatalf("create: %d %s", status, data)
		}
		sameProject(t, before, treeOf(t, s.projectDir), "create conflict")
	})
	t.Run("config never overrides", func(t *testing.T) {
		s, _ := graphRow3Desk(t, graphRow3Config)
		offer := graphRow3Offer(t, s, graphRow3Proposal())
		// Another writer between the two commits: the graph remains undeclared.
		previous := testHookAfterLockEntry
		testHookAfterLockEntry = func(path string) {
			if path == runtimeConfigName {
				writeProjectFile(t, s.projectDir, runtimeConfigName, graphRow3Config+" ")
			}
		}
		t.Cleanup(func() { testHookAfterLockEntry = previous })
		status, data := graphRow3Post(t, s, "write", offer, true, "")
		if status != 409 || !strings.Contains(string(data), `"graphWritten":true`) || !strings.Contains(string(data), `"declared":false`) {
			t.Fatalf("partial: %d %s", status, data)
		}
		config, _ := os.ReadFile(filepath.Join(s.projectDir, runtimeConfigName))
		graph, _ := os.ReadFile(filepath.Join(s.projectDir, offer.Path))
		if string(config) != graphRow3Config+" " || string(graph) != offer.Content {
			t.Fatal("partial write did not preserve both writers")
		}
	})
	t.Run("audit named graph", func(t *testing.T) {
		s, _ := graphRow3Desk(t, `{"configVersion":"5","packs":{},"audit":{"dir":"records"}}`)
		proposal := graphRow3Proposal()
		proposal.Path = "records/evaluations.jsonl"
		if err := os.Mkdir(filepath.Join(s.projectDir, "records"), 0700); err != nil {
			t.Fatal(err)
		}
		before := treeOf(t, s.projectDir)
		status, data := graphRow3Post(t, s, "proposal", proposal, true, "")
		if status != 403 {
			t.Fatalf("audit: %d %s", status, data)
		}
		sameProject(t, before, treeOf(t, s.projectDir), "audit file")
	})
}

func TestGraphRow3Edit(t *testing.T) {
	for _, kind := range []string{"other file", "undeclared id"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := graphRow3Desk(t, `{"configVersion":"2","packs":{},"graphs":{"draft":{"path":"draft.graph.json"}}}`)
			writeProjectFile(t, s.projectDir, "README.md", "{}")
			proposal := graphRow3Proposal()
			proposal.BaseSHA256 = digestOf([]byte("{}"))
			if kind == "other file" {
				proposal.Path = "README.md"
			} else {
				proposal.ID = "unknown"
				proposal.Path = "README.md"
			}
			before := treeOf(t, s.projectDir)
			status, data := graphRow3Post(t, s, "proposal", proposal, true, "")
			if status != 409 {
				t.Fatalf("edit guard: %d %s", status, data)
			}
			sameProject(t, before, treeOf(t, s.projectDir), kind)
		})
	}

	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "stale"}[changed], func(t *testing.T) {
			config := `{"configVersion":"2","packs":{},"graphs":{"draft":{"path":"draft.graph.json","rows":"existing.rows.json"}}}`
			s, _ := graphRow3Desk(t, config)
			writeProjectFile(t, s.projectDir, "draft.graph.json", "{}")
			proposal := graphRow3Proposal()
			proposal.BaseSHA256 = digestOf([]byte("{}"))
			offer := graphRow3Offer(t, s, proposal)
			if changed {
				writeProjectFile(t, s.projectDir, proposal.Path, "{} ")
			}
			commits := []string{}
			previous := testHookAfterLockEntry
			testHookAfterLockEntry = func(path string) { commits = append(commits, path) }
			t.Cleanup(func() { testHookAfterLockEntry = previous })
			status, data := graphRow3Post(t, s, "write", offer, true, "")
			if strings.Join(commits, ",") != proposal.Path {
				t.Fatalf("an edit must commit only its graph: %v", commits)
			}
			want := 200
			if changed {
				want = 409
			}
			if changed && (strings.Contains(string(data), "override") || !strings.Contains(string(data), "Review the proposal again; nothing was written.")) {
				t.Fatalf("route wording: %s", data)
			}
			if status != want {
				t.Fatalf("edit: %d %s", status, data)
			}
			after, _ := os.ReadFile(filepath.Join(s.projectDir, runtimeConfigName))
			if string(after) != config {
				t.Fatal("an edit changed configuration")
			}
		})
	}
}

func TestGraphRow3Refusals(t *testing.T) {
	t.Run("version", func(t *testing.T) {
		s, _ := graphRow3Desk(t, `{"configVersion":"1","packs":{}}`)
		before := treeOf(t, s.projectDir)
		status, _ := graphRow3Post(t, s, "proposal", graphRow3Proposal(), true, "")
		if status != 409 {
			t.Fatal(status)
		}
		sameProject(t, before, treeOf(t, s.projectDir), "version 1")
	})
	t.Run("digest", func(t *testing.T) {
		s, rig := graphRow3Desk(t, graphRow3Config)
		rig.answers(t, "validate", `{"outputVersion":"2","command":"experimental graph validate","status":"valid","graphSha256":"wrong"}`, 0)
		status, _ := graphRow3Post(t, s, "proposal", graphRow3Proposal(), true, "")
		if status != 409 {
			t.Fatal(status)
		}
	})
	for _, route := range []string{"proposal", "write", "validate", "explain"} {
		t.Run(route, func(t *testing.T) {
			s, rig := graphRow3Desk(t, graphRow3Config)
			before := treeOf(t, s.projectDir)
			for _, guard := range []string{"session", "origin"} {
				origin := ""
				auth := false
				if guard == "origin" {
					auth = true
					origin = "https://elsewhere.example"
				}
				status, _ := graphRow3Post(t, s, route, graphRow3Proposal(), auth, origin)
				if status != 401 && status != 403 {
					t.Fatalf("%s: %d", guard, status)
				}
			}
			if len(rig.asked(t)) != 0 {
				t.Fatal("guard ran runtime")
			}
			sameProject(t, before, treeOf(t, s.projectDir), "guards")
		})
	}
	t.Run("override input", func(t *testing.T) {
		s, _ := graphRow3Desk(t, graphRow3Config)
		status, _ := graphRow3Post(t, s, "write", map[string]any{"override": true}, true, "")
		if status != 400 {
			t.Fatal(status)
		}
	})
}

func TestGraphRow3HostToolsAndStandardInput(t *testing.T) {
	s, _ := graphRow3Desk(t, graphRow3Config)
	// The child prints only what arrived on stdin. No shell output file.
	bin := filepath.Join(t.TempDir(), "stdin-runtime")
	script := "#!/bin/sh\n[ \"$1 $2\" = 'experimental graph' ] || exit 64\n[ \"$4 $5 $6 $7 $8\" = '- --config jpack.json --format json' ] || exit 64\nIFS= read -r line\nprintf '%s' \"$line\"\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	s.cfg.JpackBin = bin
	for _, command := range []string{"validate", "explain"} {
		content := `{"outputVersion":"2","command":"experimental graph ` + command + `","status":"invalid","kind":"non-normative-runtime-convention"}`
		before := treeOf(t, s.projectDir)
		status, data := graphRow3Post(t, s, command, map[string]string{"content": content}, true, "")
		if status != 200 || string(answerOf(t, data)) != content {
			t.Fatalf("stdin %s: %d %s", command, status, data)
		}
		sameProject(t, before, treeOf(t, s.projectDir), command)
	}
	dir, _ := s.graphRuntime()
	limitBin := filepath.Join(t.TempDir(), "input-limit")
	if err := os.WriteFile(limitBin, []byte("#!/bin/sh\ncat >/dev/null\nprintf '{}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := runRuntimeInput(context.Background(), limitBin, dir, []byte(strings.Repeat("x", maxFileBytes+1)), "experimental", "graph", "validate", "-"); err == nil {
		t.Fatal("unbounded input")
	}
	status, _ := graphRow3Post(t, s, "validate", map[string]string{"content": strings.Repeat("x", maxFileBytes+1)}, true, "")
	if status != 413 {
		t.Fatal(status)
	}
}

func TestGraphRow3RealRuntime(t *testing.T) {
	bin := os.Getenv("JPACK_BIN")
	if bin == "" {
		t.Skip("JPACK_BIN is not set")
	}
	bin, err := filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	// go test runs in internal/desk; accept the coordinator's repository-relative path.
	if _, err := os.Stat(bin); err != nil {
		bin, err = filepath.Abs(filepath.Join("../..", os.Getenv("JPACK_BIN")))
		if err != nil {
			t.Fatal(err)
		}
	}
	s, _ := graphRow3Desk(t, graphRow3Config)
	s.cfg.JpackBin = bin
	files, err := os.ReadDir("testdata/graphs")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join("testdata/graphs", file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeProjectFile(t, s.projectDir, file.Name(), string(data))
	}
	content, err := os.ReadFile(filepath.Join(s.projectDir, "onboarding.graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"validate", "explain"} {
		before := treeOf(t, s.projectDir)
		status, data := graphRow3Post(t, s, command, map[string]string{"content": string(content)}, true, "")
		if status != 200 {
			t.Fatalf("%s: %d %s", command, status, data)
		}
		var answer struct {
			Status      string `json:"status"`
			GraphSHA256 string `json:"graphSha256"`
			Kind        string `json:"kind"`
			GraphPath   string `json:"graphPath"`
		}
		_ = json.Unmarshal(answerOf(t, data), &answer)
		if answer.Kind != graphLabel || answer.GraphPath != "-" {
			t.Fatalf("%s: %s", command, data)
		}
		if command == "validate" && (answer.Status != "valid" || answer.GraphSHA256 != digestOf(content)) {
			t.Fatalf("findings: %s", data)
		}
		if command == "explain" && answer.Status != "planned" {
			t.Fatalf("plan: %s", data)
		}
		sameProject(t, before, treeOf(t, s.projectDir), command)
	}
	proposal := graphRow3Proposal()
	proposal.Content = string(content)
	offer := graphRow3Offer(t, s, proposal)
	if offer.Findings == "" || offer.Plan == "" {
		t.Fatal("no runtime answers")
	}
}

func TestGraphRow3BothCommitsHoldBothLocks(t *testing.T) {
	s, _ := graphRow3Desk(t, graphRow3Config)
	offer := graphRow3Offer(t, s, graphRow3Proposal())
	seen := []string{}
	previous := testHookAfterLockEntry
	testHookAfterLockEntry = func(path string) {
		seen = append(seen, path)
		if s.writes.TryLock() {
			s.writes.Unlock()
			t.Error("commit without Desk's one write mutex")
		}
		release, err := s.lockProject(context.Background(), 0)
		if err == nil {
			release()
			t.Error("commit without the project folder lock")
		}
	}
	t.Cleanup(func() { testHookAfterLockEntry = previous })
	status, data := graphRow3Post(t, s, "write", offer, true, "")
	if status != 200 {
		t.Fatalf("write: %d %s", status, data)
	}
	if strings.Join(seen, ",") != "draft.graph.json,jpack.json" {
		t.Fatalf("not both guarded commits, graph first: %v", seen)
	}
}

func TestGraphRow3ReservedPaths(t *testing.T) {
	for _, path := range []string{"pack.json", "PACK.JSON", "pack.rows.json", "other.graph.json", "graph.rows.json", "abs.rows.json", runtimeConfigName, runtimeLockName} {
		t.Run(path, func(t *testing.T) {
			s, _ := graphRow3Desk(t, `{"configVersion":"2","packs":{"pack":{"path":"Pack.json","matrix":"Pack.Rows.json"}},"graphs":{"other":{"path":"Other.Graph.json","rows":"graph.rows.json"}}}`)
			proposal := graphRow3Proposal()
			proposal.Path = path
			if path == "abs.rows.json" {
				config := `{"configVersion":"2","packs":{"abs":{"path":"pack.json","matrix":` + string(mustMarshal(filepath.Join(s.projectDir, path))) + `}}}`
				writeProjectFile(t, s.projectDir, runtimeConfigName, config)
			}
			before := treeOf(t, s.projectDir)
			status, data := graphRow3Post(t, s, "proposal", proposal, true, "")
			if status != 409 {
				t.Fatalf("reserved path: %d %s", status, data)
			}
			sameProject(t, before, treeOf(t, s.projectDir), "reserved path")
		})
	}
}

func TestGraphRow3HasLock(t *testing.T) {
	for _, present := range []bool{false, true} {
		s, _ := graphRow3Desk(t, graphRow3Config)
		if present {
			writeProjectFile(t, s.projectDir, runtimeLockName, "{}")
		}
		if offer := graphRow3Offer(t, s, graphRow3Proposal()); offer.HasLock != present {
			t.Fatalf("hasLock = %v, want %v", offer.HasLock, present)
		}
	}
}

func TestGraphRow3Preflight(t *testing.T) {
	for _, stage := range []string{"proposal", "write"} {
		for _, kind := range []string{"config readonly", "graph readonly", "missing", "symlink", "file parent"} {
			t.Run(stage+"/"+kind, func(t *testing.T) {
				s, _ := graphRow3Desk(t, graphRow3Config)
				proposal := graphRow3Proposal()
				proposal.Path = "folder/draft.graph.json"
				if err := os.Mkdir(filepath.Join(s.projectDir, "folder"), 0700); err != nil {
					t.Fatal(err)
				}
				var offer graphWriteOffer
				if stage == "write" {
					offer = graphRow3Offer(t, s, proposal)
				}
				switch kind {
				case "config readonly":
					if err := os.Chmod(filepath.Join(s.projectDir, runtimeConfigName), 0400); err != nil {
						t.Fatal(err)
					}
				case "graph readonly":
					writeProjectFile(t, s.projectDir, proposal.Path, "{}")
					if err := os.Chmod(filepath.Join(s.projectDir, proposal.Path), 0400); err != nil {
						t.Fatal(err)
					}
				default:
					if err := os.Remove(filepath.Join(s.projectDir, "folder")); err != nil {
						t.Fatal(err)
					}
					if kind == "symlink" {
						if err := os.Symlink(s.projectDir, filepath.Join(s.projectDir, "folder")); err != nil {
							t.Fatal(err)
						}
					}
					if kind == "file parent" {
						writeProjectFile(t, s.projectDir, "folder", "{}")
					}
				}
				before := treeOf(t, s.projectDir)
				commits := 0
				previous := testHookAfterLockEntry
				testHookAfterLockEntry = func(string) { commits++ }
				defer func() { testHookAfterLockEntry = previous }()
				var body any = proposal
				if stage == "write" {
					body = offer
				}
				status, data := graphRow3Post(t, s, stage, body, true, "")
				if status < 400 || commits != 0 {
					t.Fatalf("preflight: %d %s, commits %d", status, data, commits)
				}
				sameProject(t, before, treeOf(t, s.projectDir), "preflight")
			})
		}
	}
}

func TestGraphRow3ProposalPreflightRefusesPaths(t *testing.T) {
	for _, kind := range []string{"in-project alias", "link outside", "missing parent"} {
		t.Run(kind, func(t *testing.T) {
			s, rig := graphRow3Desk(t, graphRow3Config)
			outside := t.TempDir()
			proposal := graphRow3Proposal()
			var wantStatus int
			var wantCode, wantError string
			switch kind {
			case "in-project alias":
				if err := os.Mkdir(filepath.Join(s.projectDir, "graphs"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("graphs", filepath.Join(s.projectDir, "alias")); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
				proposal.Path = "alias/draft.graph.json"
				wantStatus, wantCode = http.StatusForbidden, CodeSymlink
				wantError = "alias/draft.graph.json passes through a symbolic link, which this desk does not edit"
			case "link outside":
				if err := os.Symlink(outside, filepath.Join(s.projectDir, "outside")); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
				proposal.Path = "outside/draft.graph.json"
				wantStatus, wantCode = http.StatusForbidden, CodeSymlink
				wantError = "outside/draft.graph.json passes through a symbolic link, which this desk does not edit"
			case "missing parent":
				proposal.Path = "missing/draft.graph.json"
				wantStatus, wantCode = http.StatusNotFound, CodeDirectoryMissing
				wantError = "The graph's folder must exist in the project before writing."
			}

			before := treeOf(t, s.projectDir)
			outsideBefore := treeOf(t, outside)
			status, data := graphRow3Post(t, s, "proposal", proposal, true, "")
			var refusal struct {
				Code  string `json:"code"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(data, &refusal); err != nil {
				t.Fatalf("proposal refusal: %d %s", status, data)
			}
			if status != wantStatus || refusal.Code != wantCode || refusal.Error != wantError {
				t.Fatalf("proposal refusal: %d %+v, want %d %s %q", status, refusal, wantStatus, wantCode, wantError)
			}
			if len(rig.asked(t)) != 0 {
				t.Fatal("proposal preflight ran the runtime")
			}
			s.writes.Lock()
			offered := s.graphOffer != nil
			s.writes.Unlock()
			if offered {
				t.Fatal("a refused proposal stored an offer")
			}

			status, data = graphRow3Post(t, s, "write", graphWriteOffer{graphProposal: proposal, Token: "any"}, true, "")
			if status != http.StatusConflict || refusalOf(data) != "The graph confirmation is spent or changed. Review the proposal again; nothing was written." {
				t.Fatalf("write after refused proposal: %d %s", status, data)
			}
			sameProject(t, before, treeOf(t, s.projectDir), kind)
			sameProject(t, outsideBefore, treeOf(t, outside), kind+" outside")
		})
	}
}

func TestGraphRow3ProposalRunsWithoutLocks(t *testing.T) {
	s, rig := graphRow3Desk(t, graphRow3Config)
	script, err := os.ReadFile(rig.bin)
	if err != nil {
		t.Fatal(err)
	}
	gate := "\nprintf '' > '" + rig.dir + "/'\"$3\"'.started'\nwhile [ ! -e '" + rig.dir + "/'\"$3\"'.release' ]; do sleep 0.01; done\n"
	script = []byte(strings.Replace(string(script), "f='", gate+"f='", 1))
	if err := os.WriteFile(rig.bin, script, 0700); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() { status, _ := graphRow3Post(t, s, "proposal", graphRow3Proposal(), true, ""); done <- status }()
	for _, command := range []string{"validate", "explain"} {
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(rig.dir, command+".started")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("runtime never started")
			}
			time.Sleep(time.Millisecond)
		}
		if s.writes.TryLock() {
			s.writes.Unlock()
		} else {
			t.Error(command + " held writes")
		}
		release, err := s.lockProject(context.Background(), 0)
		if err != nil {
			t.Error(command + " held folder lock")
		} else {
			release()
		}
		if err := os.WriteFile(filepath.Join(rig.dir, command+".release"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if status := <-done; status != 200 {
		t.Fatal(status)
	}
}

func TestGraphRow3RuntimeOptionalInput(t *testing.T) {
	s, _ := graphRow3Desk(t, graphRow3Config)
	dir, _ := s.graphRuntime()
	bin := filepath.Join(t.TempDir(), "stdin-kind")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nif [ -p /dev/stdin ]; then printf pipe; else printf null; fi\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, input := range [][]byte{nil, {}, []byte("graph")} {
		got, err := runRuntimeInput(context.Background(), bin, dir, input)
		want := "pipe"
		if input == nil {
			want = "null"
		}
		if err != nil || string(got) != want {
			t.Fatalf("input %v: %s %v, want %s", input, got, err, want)
		}
	}
}
