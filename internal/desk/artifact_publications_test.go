package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactPublicationRetainsAndRefusesIdentityReuse(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	rig.answers(t, "valid")
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	if status, body := confirm(t, ts, "", readReview(t, ts, "").Token); status != 200 {
		t.Fatalf("%d %s", status, body)
	}
	historyPath := filepath.Join(project, publicationDirectory, "history.json")
	original := readFile(t, historyPath)
	if status, body := confirm(t, ts, "", readReview(t, ts, "").Token); status != 200 {
		t.Fatalf("%d %s", status, body)
	}
	if readFile(t, historyPath) != original {
		t.Fatal("no-op confirmation rewrote history")
	}
	lock := readFile(t, filepath.Join(project, runtimeLockName))
	edited := strings.Replace(reviewPack, "Minimal literal decision", "Changed title", 1)
	writeProject(t, project, map[string]string{"packs/a.json": edited})
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	status, body := confirm(t, ts, "", readReview(t, ts, "").Token)
	if status != http.StatusConflict || !bytes.Contains(body, []byte("already identifies different reviewed content")) {
		t.Fatalf("%d %s", status, body)
	}
	if readFile(t, historyPath) != original || readFile(t, filepath.Join(project, runtimeLockName)) != lock {
		t.Fatal("refused publication changed history or lock")
	}
	status, body = reviewCall(t, ts, "GET", "/api/artifacts/versions?kind=pack&id=https%3A%2F%2Fexample.com%2Fjudgment-packs%2Fminimal-literal", "", nil, bearer)
	if status != 200 || !bytes.Contains(body, []byte(`\"title\":\"Minimal literal decision\"`)) {
		t.Fatalf("retained history %d %s", status, body)
	}
	var response struct {
		Items []struct {
			Publication artifactPublication `json:"publication"`
		}
	}
	if json.Unmarshal(body, &response) != nil || len(response.Items) != 1 || len(response.Items[0].Publication.Documents) != 2 {
		t.Fatal("review closure missing", string(body))
	}
}

func TestArtifactPublicationHigherVersionAndRestore(t *testing.T) {
	bin := os.Getenv("JPACK_TEST_BIN")
	if bin == "" {
		t.Skip("set JPACK_TEST_BIN for exact Runtime version checks")
	}
	_, ts, rig, project := reviewProject(t, "", "")
	rig.answers(t, "valid")
	// Delegate only arithmetic to the real Runtime, preserving the lock race rig.
	script := readFile(t, rig.bin)
	script = strings.Replace(script, "#!/bin/sh\n", "#!/bin/sh\nif [ \"$1 $2\" = 'artifact version' ]; then exec '"+bin+"' \"$@\"; fi\n", 1)
	if err := os.WriteFile(rig.bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	publish := func(content string) (int, []byte) {
		writeProject(t, project, map[string]string{"packs/a.json": content})
		rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
		return confirm(t, ts, "", readReview(t, ts, "").Token)
	}
	if status, body := publish(reviewPack); status != 200 {
		t.Fatalf("%d %s", status, body)
	}
	ledgerPath := filepath.Join(project, publicationDirectory, "history.json")
	oldLedger := readFile(t, ledgerPath)
	changed := strings.Replace(reviewPack, `"version":"0.1.0"`, `"version":"0.2.0"`, 1)
	if status, body := publish(changed); status != 200 {
		t.Fatalf("%d %s", status, body)
	}
	// Simulate the process stopping after the v0.2 lock but before its receipt.
	if err := os.WriteFile(ledgerPath, []byte(oldLedger), 0600); err != nil {
		t.Fatal(err)
	}
	if status, body := publish(strings.Replace(changed, "Minimal literal decision", "Different bytes", 1)); status != 409 || !bytes.Contains(body, []byte("already identifies")) {
		t.Fatalf("unrecorded version was reused: %d %s", status, body)
	}
	if readFile(t, ledgerPath) == oldLedger {
		t.Fatal("the recovered lock was not committed before the refused candidate")
	}
	if status, body := publish(reviewPack); status != 409 || !bytes.Contains(body, []byte("Restoring earlier content")) {
		t.Fatalf("old content restored without a new version: %d %s", status, body)
	}
	changed = strings.Replace(reviewPack, `"version":"0.1.0"`, `"version":"0.3.0"`, 1)
	if status, body := publish(changed); status != 200 {
		t.Fatalf("%d %s", status, body)
	}
}

func TestArtifactPublicationCorruptionStopsPublication(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	rig.answers(t, "valid")
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}))
	if status, body := confirm(t, ts, "", readReview(t, ts, "").Token); status != 200 {
		t.Fatalf("%d %s", status, body)
	}
	path := filepath.Join(project, publicationDirectory, "history.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"items":[{}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if status, _ := confirm(t, ts, "", readReview(t, ts, "").Token); status != 409 {
		t.Fatal("corruption accepted", status)
	}
}

func TestArtifactPublicationMixedVersionAliases(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	rig.answers(t, "valid")
	second := strings.Replace(reviewPack, `"version":"0.1.0"`, `"version":"0.2.0"`, 1)
	writeProject(t, project, map[string]string{"packs/b.json": second})
	lock := lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"})
	writeProject(t, project, map[string]string{runtimeLockName: string(lock)})
	rig.locks(t, lock)
	for range 2 {
		if status, body := confirm(t, ts, "", readReview(t, ts, "").Token); status != 200 {
			t.Fatalf("unchanged aliases: %d %s", status, body)
		}
	}
	status, body := reviewCall(t, ts, "GET", "/api/artifacts/versions?kind=pack&id=https%3A%2F%2Fexample.com%2Fjudgment-packs%2Fminimal-literal", "", nil, bearer)
	var response struct {
		Items []struct {
			Document publishedDocument `json:"document"`
		}
	}
	if status != 200 || json.Unmarshal(body, &response) != nil || len(response.Items) != 2 {
		t.Fatalf("version aliases hidden: %d %s", status, body)
	}
}

func TestArtifactPublicationValidatesFirstVersion(t *testing.T) {
	bin := os.Getenv("JPACK_TEST_BIN")
	if bin == "" {
		t.Skip("set JPACK_TEST_BIN for exact Runtime checks")
	}
	s, ts, rig, project := reviewProject(t, "", "")
	rig.answers(t, "valid")
	keepCopy(t, s, twoPacks)
	script := strings.Replace(readFile(t, rig.bin), "#!/bin/sh\n", "#!/bin/sh\nif [ \"$1 $2\" = 'artifact version' ]; then exec '"+bin+"' \"$@\"; fi\n", 1)
	if err := os.WriteFile(rig.bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	// The new identity has no published version to compare against.
	invalid := strings.Replace(strings.Replace(reviewPack, "minimal-literal", "new-artifact", 1), `"version":"0.1.0"`, `"version":"banana"`, 1)
	writeProject(t, project, map[string]string{"packs/c.json": invalid, "jpack.json": strings.Replace(twoPacks, `"packs":{`, `"packs":{"new":{"path":"packs/c.json"},`, 1)})
	// Retain the initial configuration, as an actual Desk-created project does.
	// The original artifacts remain readable, so their bytes need no recovery copy.
	if status, body := confirm(t, ts, "", readReview(t, ts, "").Token); status != 409 || !bytes.Contains(body, []byte("Runtime could not verify")) {
		t.Fatalf("bad first version: %d %s", status, body)
	}
}

// Retained bytes can precede a lock for crash recovery. Only a non-imported
// publication receipt would incorrectly claim that a rolled-back candidate won.
func assertNoNewPublication(t *testing.T, project string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(project, publicationDirectory, "history.json"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var ledger publicationLedger
	if json.Unmarshal(raw, &ledger) != nil {
		t.Fatal("invalid ledger")
	}
	for _, p := range ledger.Items {
		if !p.Imported {
			t.Fatal("rolled-back candidate was published")
		}
	}
}

func TestArtifactPublicationAliasMoveCannotRestoreOldVersion(t *testing.T) {
	bin := os.Getenv("JPACK_TEST_BIN")
	if bin == "" {
		t.Skip("real Runtime required")
	}
	_, ts, rig, project := reviewProject(t, "", "")
	rig.answers(t, "valid")
	script := strings.Replace(readFile(t, rig.bin), "#!/bin/sh\n", "#!/bin/sh\nif [ \"$1 $2\" = 'artifact version' ]; then exec '"+bin+"' \"$@\"; fi\n", 1)
	if err := os.WriteFile(rig.bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	publish := func(paths map[string]string) (int, []byte) {
		rig.locks(t, lockOf(t, project, paths))
		return confirm(t, ts, "", readReview(t, ts, "").Token)
	}
	if status, body := publish(map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}); status != 200 {
		t.Fatalf("%d %s", status, body)
	}
	writeProject(t, project, map[string]string{"packs/a.json": strings.Replace(reviewPack, `"version":"0.1.0"`, `"version":"0.2.0"`, 1)})
	if status, body := publish(map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}); status != 200 {
		t.Fatalf("%d %s", status, body)
	}
	writeProject(t, project, map[string]string{"packs/old.json": reviewPack, "jpack.json": strings.Replace(twoPacks, "packs/a.json", "packs/old.json", 1)})
	if status, body := publish(map[string]string{"alpha": "packs/old.json", "beta": "packs/b.json"}); status != 409 || !bytes.Contains(body, []byte("Restoring earlier content")) {
		t.Fatalf("moved alias restored without bump: %d %s", status, body)
	}
	if readFile(t, filepath.Join(project, publicationDirectory, ".gitignore")) != "*\n" {
		t.Fatal("private history is not ignored")
	}
}

func TestArtifactVersionHistoryOnlyReadsItsPage(t *testing.T) {
	s, ts, _, project := reviewProject(t, "", "")
	config, err := s.artifactRepository().Put(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	ledger := publicationLedger{Version: 1}
	for i := range 26 {
		raw := []byte(fmt.Sprintf(`{"id":"urn:history","version":"0.%d.0"}`, i))
		ref, e := s.artifactRepository().Put(context.Background(), raw)
		if e != nil {
			t.Fatal(e)
		}
		p := artifactPublication{At: "2026-10-15T00:00:00Z", Config: &config, Documents: []publishedDocument{{Kind: "pack", Alias: "main", ID: "urn:history", Version: fmt.Sprintf("0.%d.0", i), Path: "pack.json", Content: ref}}}
		p.ID = publicationID(p)
		ledger.Items = append(ledger.Items, p)
	}
	root, _, err := openOwnFolder(s.root, strings.Split(publicationDirectory, "/"), true, "artifact versions")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = writePublicationLedger(root, ledger); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(project, retainedArtifactDirectory, ledger.Items[0].Documents[0].Content.SHA256)); err != nil {
		t.Fatal(err)
	}
	status, body := reviewCall(t, ts, "GET", "/api/artifacts/versions?kind=pack&id=urn%3Ahistory", "", nil, bearer)
	var page struct {
		Items []json.RawMessage `json:"items"`
		Next  int               `json:"next"`
	}
	if status != 200 || json.Unmarshal(body, &page) != nil || len(page.Items) != 25 || page.Next != 2 {
		t.Fatalf("valid first page failed: %d %s", status, body)
	}
	status, body = reviewCall(t, ts, "GET", "/api/artifacts/versions?kind=pack&id=urn%3Ahistory&before=2", "", nil, bearer)
	if status != 503 || !bytes.Contains(body, []byte("could not be verified")) {
		t.Fatalf("missing old bytes not reported: %d %s", status, body)
	}
}

func TestArtifactPublicationCanAdoptExistingNewerVersion(t *testing.T) {
	bin := os.Getenv("JPACK_TEST_BIN")
	if bin == "" {
		t.Skip("real Runtime required")
	}
	_, ts, rig, project := reviewProject(t, "", "")
	rig.answers(t, "valid")
	script := strings.Replace(readFile(t, rig.bin), "#!/bin/sh\n", "#!/bin/sh\nif [ \"$1 $2\" = 'artifact version' ]; then exec '"+bin+"' \"$@\"; fi\n", 1)
	if err := os.WriteFile(rig.bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	newer := strings.Replace(reviewPack, `"version":"0.1.0"`, `"version":"0.2.0"`, 1)
	writeProject(t, project, map[string]string{"packs/b.json": newer})
	original := lockOf(t, project, map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"})
	writeProject(t, project, map[string]string{runtimeLockName: string(original)})
	rig.locks(t, original)
	if status, body := confirm(t, ts, "", readReview(t, ts, "").Token); status != 200 {
		t.Fatalf("%d %s", status, body)
	}
	writeProject(t, project, map[string]string{"jpack.json": strings.Replace(twoPacks, "packs/a.json", "packs/b.json", 1)})
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/b.json", "beta": "packs/b.json"}))
	if status, body := confirm(t, ts, "", readReview(t, ts, "").Token); status != 200 {
		t.Fatalf("existing forward version refused: %d %s", status, body)
	}
}

func TestFailedCreationNeverCleansThroughSubstitutedReviewedSymlink(t *testing.T) {
	project := t.TempDir()
	writeProject(t, project, map[string]string{runtimeConfigName: `{}`, "unrelated/.gitignore": "keep"})
	if err := os.Chmod(filepath.Join(project, runtimeConfigName), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".desk-private"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../unrelated", filepath.Join(project, reviewedCopiesDir)); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = unmakeDeskFolder(root, project); err == nil || !strings.Contains(err.Error(), "reviewed") {
		t.Fatal("substituted reviewed folder was not refused", err)
	}
	if readFile(t, filepath.Join(project, "unrelated/.gitignore")) != "keep" {
		t.Fatal("unrelated file changed")
	}
}
