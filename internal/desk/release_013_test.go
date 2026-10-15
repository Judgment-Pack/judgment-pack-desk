package desk

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundRepairRuntimeFloor(t *testing.T) {
	for _, version := range []string{"0.30.0", "v0.30.1", "0.31.0+build", "1.0.0"} {
		if !runtimeSupportsBoundRepair(version) {
			t.Errorf("%q was refused", version)
		}
	}
	for _, version := range []string{"", "development", "0.29.99", "0.30.0-rc.1", "0.30", "0.030.0", " 0.30.0", "0.+30.0"} {
		if runtimeSupportsBoundRepair(version) {
			t.Errorf("%q was accepted", version)
		}
	}
}

func TestOlderRuntimeNeverReceivesAnUnboundRepair(t *testing.T) {
	r := newRepairRig(t, withAuditVersions)
	token := r.offer(t).Token
	script := strings.ReplaceAll(readFile(t, r.rig.bin), `"version":"0.30.0"`, `"version":"0.29.0"`)
	if err := os.WriteFile(r.rig.bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_, answer, _ := readAudit(t, r.ts, "")
	if answer.Repair == nil || answer.Repair.State != repairUnavailable || answer.Repair.Token != "" || answer.Repair.Reason != repairBoundRuntimeWords {
		t.Fatalf("old Runtime offered a repair: %+v", answer.Repair)
	}
	got := r.repair(t, token)
	if got.status != http.StatusConflict || got.error != repairBoundRuntimeWords || r.rig.repairs(t) != 0 {
		t.Fatalf("old Runtime received a repair: %d %s", got.status, got.data)
	}
}

func TestChangedDamageAfterConfirmationWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv(runtimeConfigEnv, "")
	t.Setenv(runtimeSigningKeyEnv, "")
	t.Setenv(runtimeSigningPublicKeyEnv, "")
	for _, scenario := range []string{"grows", "shrinks", "same length", "completed", "empty", "moved"} {
		t.Run(scenario, func(t *testing.T) {
			project := t.TempDir()
			config := `{"configVersion":"6","audit":{"dir":".desk-private/audit"},"packs":{"alpha":{"path":"packs/a.json"}}}` + "\n"
			writeProject(t, project, map[string]string{"jpack.json": config, "packs/a.json": reviewPack})
			jpackIn(t, bin, project, "packs", "lock", "--config", "jpack.json", "--format", "json")
			if code, out := decidingRun(t, bin, project); code != 0 {
				t.Fatalf("setup: %d %s", code, out)
			}
			cutTrail(t, project)
			trail := trailOf(project)
			original := readFile(t, trail)
			changed := original
			switch scenario {
			case "grows":
				changed += "x"
			case "shrinks":
				changed = strings.TrimSuffix(original, tornBytes) + tornBytes[:len(tornBytes)-1]
			case "same length":
				changed = strings.TrimSuffix(original, tornBytes) + `{"evil":`
			case "completed":
				changed += "\n"
			case "empty":
				changed = ""
			}
			wrapperDir := t.TempDir()
			wrapper := filepath.Join(wrapperDir, "jpack")
			calls := filepath.Join(wrapperDir, "repairs")
			mutate := "printf '%s' " + shellQuote(changed) + " > " + shellQuote(trail) + "\n"
			if scenario == "moved" {
				mutate = "mv " + shellQuote(trail) + " " + shellQuote(trail+".moved") + "\n"
			}
			script := "#!/bin/sh\nif [ \"$1 $2\" = 'audit repair' ]; then\n" +
				"printf 'attempt\\n' >> " + shellQuote(calls) + "\n" + mutate + "fi\nexec " + shellQuote(bin) + " \"$@\"\n"
			if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: wrapper, Token: testToken})
			t.Cleanup(func() { ts.Close(); s.Close() })
			token := offerOn(t, ts, "").Token
			got := repairOn(t, ts, "", token)
			if got.status != http.StatusConflict || got.code != CodeStale || got.error != repairStaleWords ||
				len(got.diagnostics) != 1 || got.diagnostics[0].Code != "JPS-AUDIT-REPAIR-TAIL-CHANGED" {
				t.Fatalf("changed damage was not refused as stale: %d %s", got.status, got.data)
			}
			if scenario == "moved" {
				if _, err := os.Stat(trail); !os.IsNotExist(err) {
					t.Fatalf("repair recreated moved trail: %v", err)
				}
				if got := readFile(t, trail+".moved"); got != original {
					t.Fatal("moved trail was changed")
				}
			} else if got := readFile(t, trail); got != changed {
				t.Fatal("refused repair changed the trail")
			}
			again := repairOn(t, ts, "", token)
			if again.error != repairUsedWords || readFile(t, calls) != "attempt\n" {
				t.Fatalf("a spent confirmation retried a repair: %s", again.data)
			}
		})
	}
}

func TestExpectedSigningKeyIsIsolatedInBothRuntimeLaunchPaths(t *testing.T) {
	t.Setenv(runtimeSigningKeyEnv, "startup-seed")
	t.Setenv(runtimeSigningPublicKeyEnv, "startup-public-key")
	script := "printf '%s|%s' \"${JPACK_SIGNING_KEY-unset}\" \"${JPACK_SIGNING_PUBLIC_KEY-unset}\""
	for _, startup := range []bool{false, true} {
		id := "child"
		want := "unset|unset"
		if startup {
			id = ""
			want = "startup-seed|startup-public-key"
		}
		s := &Server{cfg: Config{deskID: id}}
		cmd := exec.Command("/bin/sh", "-c", script)
		cmd.Env = s.runtimeEnv()
		out, err := cmd.Output()
		if err != nil || string(out) != want {
			t.Fatalf("relay startup=%v: %q %v", startup, out, err)
		}
		path := t.TempDir()
		dir, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer dir.Close()
		info, err := dir.Stat()
		if err != nil {
			t.Fatal(err)
		}
		out, err = runRuntime(context.Background(), "/bin/sh", heldDir{file: dir, path: path, info: info, startup: startup}, "-c", script)
		if err != nil || string(out) != want {
			t.Fatalf("command startup=%v: %q %v", startup, out, err)
		}
	}
}

func TestManagedProjectRejectsSubstitutedSigningSeed(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv(runtimeConfigEnv, "")
	t.Setenv(runtimeSigningKeyEnv, "")
	t.Setenv(runtimeSigningPublicKeyEnv, "")
	s, ts, _ := gatesServer(t, bin)
	row := createGatedDesk(t, ts)
	config := strings.Replace(wantSignedConfig(s.configDir, row.ID), `"packs":{}`, `"packs":{"alpha":{"path":"packs/a.json"}}`, 1)
	writeProject(t, row.Folder, map[string]string{"packs/a.json": reviewPack, "jpack.json": config, "facts.json": "{}\n"})
	jpackIn(t, bin, row.Folder, "packs", "lock", "--config", "jpack.json", "--format", "json")
	// Opening the named desk supplies the same server used by its relay and CLI.
	status, data := reviewCall(t, ts, "GET", "/api/audit/verify", row.ID, nil, bearer)
	if status != 200 {
		t.Fatalf("open %d %s", status, data)
	}
	s.desksMu.Lock()
	child := s.desks[row.ID]
	s.desksMu.Unlock()
	if child == nil {
		t.Fatal("no child server")
	}
	expected := child.runtimeSigningExpectation()
	if len(expected) != 64 {
		t.Fatalf("no retained expectation %q", expected)
	}
	dir, _, ok := child.projectRuntime()
	if !ok {
		t.Fatal("no project")
	}
	evaluate := func() {
		t.Helper()
		out, err := runRuntime(context.Background(), bin, dir, "experimental", "evaluate", "--config", "jpack.json", "--pack-id", "alpha", "--facts", "facts.json", "--format", "json")
		if err != nil {
			t.Fatalf("decision lost: %v %s", err, out)
		}
	}
	evaluate()
	sidecar := filepath.Join(row.Folder, ".desk-private", "audit", "signatures.jsonl")
	before, err := os.ReadFile(sidecar)
	if err != nil || len(before) == 0 {
		t.Fatalf("not signed: %v", err)
	}
	seed := filepath.Join(signingFolderOf(s), row.ID+seedSuffix)
	original, err := os.ReadFile(seed)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "replacement.seed")
	jpackIn(t, bin, row.Folder, "audit", "key", "generate", other, "--format", "json")
	replacement, err := os.ReadFile(other)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(seed, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	if child.runtimeSigningExpectation() != expected {
		t.Fatal("a substituted seed changed the expectation")
	}
	evaluate()
	after, err := os.ReadFile(sidecar)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("substituted key signed: %v", err)
	}
	checked := child.signingCheck(context.Background(), dir)
	if checked.Status != "failed" {
		t.Fatalf("signing failure hidden: %+v", checked)
	}
	// Losing the independent list must not quietly turn off expectation checks.
	if err = os.Rename(filepath.Join(signingFolderOf(s), row.ID+keysSuffix), filepath.Join(signingFolderOf(s), "saved-list")); err != nil {
		t.Fatal(err)
	}
	if child.runtimeSigningExpectation() == "" {
		t.Fatal("missing list disabled the expectation")
	}
	if err = os.WriteFile(seed, original, 0600); err != nil {
		t.Fatal(err)
	}
	evaluate()
	after, err = os.ReadFile(sidecar)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("missing list allowed signing: %v", err)
	}
}
