package desk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectArtifactCompatibilityReview(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		t.Run(backend, func(t *testing.T) {
			server, ts, rig, project := reviewProject(t, "", "")
			if backend == "memory" {
				server.retainedArtifacts = newMemoryArtifacts()
			}
			if err := os.Remove(filepath.Join(project, "jpack.lock.json")); err != nil {
				t.Fatal(err)
			}
			rig.answers(t, "valid")
			rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
			review := readReview(t, ts, "")
			if len(review.ArtifactReviews) == 0 {
				t.Fatal("no artifact versions")
			}
			// A valid file and review token alone never provide compatibility judgment.
			status, raw := reviewCall(t, ts, "POST", "/api/review/lock", "", map[string]any{"token": review.Token}, bearer)
			if status != 409 || !strings.Contains(string(raw), "compatibility") {
				t.Fatal(status, string(raw))
			}
			impacts := map[string]artifactImpact{}
			for _, row := range review.ArtifactReviews {
				if row.Required {
					impacts[row.Key] = artifactImpact{"initial", "Reviewed policy and consumer expectations"}
				}
			}
			status, raw = reviewCall(t, ts, "POST", "/api/review/artifacts", "", map[string]any{"token": review.Token, "impacts": impacts}, bearer)
			if status != 200 {
				t.Fatal(status, string(raw))
			}
			var checked struct {
				Items []projectArtifactReview `json:"items"`
			}
			json.Unmarshal(raw, &checked)
			refs := map[string]retainedArtifactRef{}
			for _, row := range checked.Items {
				if row.Required {
					refs[row.Key] = row.Review.Reference
				}
			}
			// A later review does not replace the earlier review selected for commit.
			for key, value := range impacts {
				value.Rationale = "A later review not selected for publication"
				impacts[key] = value
			}
			status, raw = reviewCall(t, ts, "POST", "/api/review/artifacts", "", map[string]any{"token": review.Token, "impacts": impacts}, bearer)
			if status != 200 {
				t.Fatal(status, string(raw))
			}
			restored := readReview(t, ts, "")
			for _, row := range restored.ArtifactReviews {
				if row.Required && row.Review == nil {
					t.Fatal("review lost on refresh")
				}
			}
			status, raw = reviewCall(t, ts, "POST", "/api/review/lock", "", map[string]any{"token": review.Token, "reviews": refs}, bearer)
			if status != 200 {
				t.Fatal(status, string(raw))
			}
			ledger, err := server.readDialectPublicationLedger()
			if err != nil || len(ledger.Items) != 1 {
				t.Fatal(err, ledger)
			}
			accepted := ledger.Items[0]
			for _, ref := range accepted.Reviews {
				found := false
				for _, want := range refs {
					if ref == want {
						found = true
					}
				}
				if !found {
					t.Fatal("wrong accepted review", ref)
				}
			}
			if len(accepted.Reviews) != len(refs) {
				t.Fatal("accepted reviews lost")
			}
			// Simulate termination after the public lock, before the history receipt.
			if err := os.Remove(filepath.Join(project, publicationDirectory, "history.json")); err != nil {
				t.Fatal(err)
			}
			status, raw = confirm(t, ts, "", readReview(t, ts, "").Token)
			if status != 200 {
				t.Fatal(status, string(raw))
			}
			ledger, err = server.readDialectPublicationLedger()
			if err != nil || len(ledger.Items) != 1 || ledger.Items[0].ID != accepted.ID {
				t.Fatal("staged publication recovery lost accepted reviews", err, ledger)
			}
		})
	}
}

func TestArtifactImpactUsesRuntimeArithmetic(t *testing.T) {
	bin := os.Getenv("JPACK_TEST_BIN")
	if bin == "" {
		t.Skip("real Runtime required")
	}
	s, _ := startDesk(t, Config{JpackBin: bin, ProjectDir: t.TempDir(), Token: testToken})
	defer s.Close()
	dir, e := holdDeskFolder(s.root, s.root.Name())
	if e != nil {
		t.Fatal(e)
	}
	defer dir.file.Close()
	for _, tc := range []struct {
		base, version, kind string
		ok                  bool
	}{
		{"", "1.0.0", "initial", true}, {"", "1.0.0", "patch", false},
		{"1.0.0", "2.0.0", "patch", false}, {"1.0.0", "2.0.0", "major", true},
		{"1.0.0", "1.1.0", "minor", true}, {"1.0.0", "1.0.1", "patch", true},
		{"9007199254740993.0.0", "9007199254740994.0.0", "major", true},
		{"1.0.0", "2.0.0", "unknown", false},
	} {
		err := s.checkArtifactImpact(context.Background(), dir, tc.base, tc.version, artifactImpact{tc.kind, "Reviewed"})
		if (err == nil) != tc.ok {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}

func delegateVersionRuntime(t *testing.T, rig *reviewRig) {
	t.Helper()
	bin := os.Getenv("JPACK_TEST_BIN")
	if bin == "" {
		t.Skip("real Runtime required")
	}
	script := strings.Replace(readFile(t, rig.bin), "#!/bin/sh\n", "#!/bin/sh\nif [ \"$1 $2\" = 'artifact version' ]; then exec '"+bin+"' \"$@\"; fi\n", 1)
	if err := os.WriteFile(rig.bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}
func TestProjectReviewUsesHighestPublishedAliasVersion(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	delegateVersionRuntime(t, rig)
	rig.answers(t, "valid")
	writeProject(t, project, map[string]string{"packs/b.json": strings.Replace(reviewPack, `"version":"0.1.0"`, `"version":"0.2.0"`, 1)})
	original := lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"})
	writeProject(t, project, map[string]string{runtimeLockName: string(original)})
	rig.locks(t, original)
	if code, raw := confirm(t, ts, "", readReview(t, ts, "").Token); code != 200 {
		t.Fatal(code, string(raw))
	}
	config := `{"configVersion":"5","requireReviewed":true,"packs":{"alpha":{"path":"packs/a.json"},"beta":{"path":"packs/b.json"},"gamma":{"path":"packs/c.json"}}}`
	writeProject(t, project, map[string]string{"jpack.json": config, "packs/c.json": strings.Replace(reviewPack, `"version":"0.1.0"`, `"version":"0.3.0"`, 1)})
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json", "gamma": "packs/c.json"}))
	review := readReview(t, ts, "")
	found := false
	for _, row := range review.ArtifactReviews {
		if row.Key == "pack:gamma" {
			found = true
			if row.BaseVersion != "0.2.0" || row.BaseArtifact == nil {
				t.Fatal("wrong publication floor", row)
			}
		}
	}
	if !found {
		t.Fatal("missing new alias")
	}
	if code, raw := confirm(t, ts, "", review.Token); code != 200 {
		t.Fatal(code, string(raw))
	}
}
func TestConfigurationUpgradeCannotPublishChangedArtifact(t *testing.T) {
	_, ts, rig, project := upgradeProject(t, allConfigVersions, "", nil)
	delegateVersionRuntime(t, rig)
	rig.answers(t, "valid")
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	if code, raw := confirm(t, ts, "", readReview(t, ts, "").Token); code != 200 {
		t.Fatal(code, string(raw))
	}
	beforeConfig, beforeLock := readFile(t, filepath.Join(project, "jpack.json")), readFile(t, filepath.Join(project, runtimeLockName))
	writeProject(t, project, map[string]string{"packs/a.json": strings.Replace(reviewPack, `"version":"0.1.0"`, `"version":"0.2.0"`, 1)})
	offer := readUpgrade(t, ts, "", true)
	code, raw := confirmUpgrade(t, ts, "", offer.Token, true)
	if code != 409 || !strings.Contains(string(raw), "Review and lock") {
		t.Fatal("upgrade bypassed compatibility review", code, string(raw))
	}
	if readFile(t, filepath.Join(project, "jpack.json")) != beforeConfig || readFile(t, filepath.Join(project, runtimeLockName)) != beforeLock {
		t.Fatal("refused upgrade changed project")
	}
}

func TestConfigurationUpgradeCannotForgetUnledgeredIdentity(t *testing.T) {
	s, ts, rig, project := upgradeProject(t, allConfigVersions, "", nil)
	rig.answers(t, "valid")
	writeProject(t, project, map[string]string{runtimeLockName: string(lockOf(t, project, bothPacks))})
	original, err := s.readSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.storeReviewedCopies(original); err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(original.config, &config); err != nil {
		t.Fatal(err)
	}
	delete(config["packs"].(map[string]any), "beta")
	raw, _ := json.Marshal(config)
	writeProject(t, project, map[string]string{runtimeConfigName: string(raw)})
	offer := readUpgrade(t, ts, "", true)
	before := treeOf(t, project)
	code, result := confirmUpgrade(t, ts, "", offer.Token, true)
	if code != 409 || !strings.Contains(string(result), "Review and lock") {
		t.Fatal("configuration upgrade discarded the only identity index", code, string(result))
	}
	sameProject(t, before, treeOf(t, project), "refused retirement of an un-ledgered release")
}

func TestConfigurationUpgradeRequiresPendingPublicationRecovery(t *testing.T) {
	_, ts, rig, project := upgradeProject(t, allConfigVersions, "", nil)
	rig.answers(t, "valid")
	rig.locks(t, lockOf(t, project, bothPacks))
	if code, raw := confirm(t, ts, "", readReview(t, ts, "").Token); code != 200 {
		t.Fatal(code, string(raw))
	}
	// Runtime committed the lock but the receipt write was interrupted.
	if err := os.Remove(filepath.Join(project, publicationDirectory, "history.json")); err != nil {
		t.Fatal(err)
	}
	offer := readUpgrade(t, ts, "", true)
	before := treeOf(t, project)
	code, raw := confirmUpgrade(t, ts, "", offer.Token, true)
	if code != 409 || !strings.Contains(string(raw), "Reconcile the previous publication") {
		t.Fatal(code, string(raw))
	}
	sameProject(t, before, treeOf(t, project), "pending review recovery before configuration upgrade")
	review := readReview(t, ts, "")
	if !review.PublicationPending {
		t.Fatal("matching-lock recovery not offered")
	}
	calls := countCalls(t, rig.calls, "packs lock")
	for i := 0; i < 2; i++ {
		code, raw = reviewCall(t, ts, "POST", "/api/review/lock", "", map[string]any{"token": review.Token, "reconcile": true}, bearer)
		if code != 200 || !strings.Contains(string(raw), `"reconciled":true`) {
			t.Fatal(code, string(raw))
		}
	}
	if countCalls(t, rig.calls, "packs lock") != calls {
		t.Fatal("history recovery replayed Runtime publication")
	}
	if readReview(t, ts, "").PublicationPending {
		t.Fatal("confirmed receipt remains pending")
	}
	rig.locks(t, upgradeLock(t, project, upgradeAfter, bothPacks))
	code, raw = confirmUpgrade(t, ts, "", readUpgrade(t, ts, "", true).Token, true)
	if code != 200 {
		t.Fatal("reconciled upgrade remains blocked", code, string(raw))
	}
}
