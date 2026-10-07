package desk

// One transaction at a time on a project's jpack.json and its lock, in every
// Desk process, and a rollback that puts back only over what it wrote (issue
// #284). A second Desk process is a descriptor of the test's own holding the
// project folder's lock, as the signing lock's tests stand one in, or a
// second server on the same project with a configuration folder of its own,
// whose review and write mutexes the first's know nothing of.

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shortProjectWait makes an owner's action wait for this project's lock for
// at most wait.
func shortProjectWait(t *testing.T, wait time.Duration) {
	t.Helper()
	was := projectLockWait
	projectLockWait = wait
	t.Cleanup(func() { projectLockWait = was })
}

// **An upgrade waits for this project's lock a bounded time, then refuses,
// and goes on once it is released.** While another Desk process holds the
// project folder's lock, a confirmation, signed or not, waits its bound,
// writes nothing, makes no key, and says so in plain words with no path.
func TestAnUpgradeWaitsForThisProjectsLockAndThenRefuses(t *testing.T) {
	for _, sign := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsigned", true: "signed"}[sign], func(t *testing.T) {
			u := newSigningUpgrade(t, nil)
			answer := u.offer(t, sign)
			config := upgradeAfter
			if sign {
				config = u.signed()
			}
			u.rig.locks(t, upgradeLock(t, u.project, config, bothPacks))
			before := treeOf(t, u.project)
			release := holdSigningLock(t, u.project)
			shortProjectWait(t, 300*time.Millisecond)
			started := time.Now()
			status, data := u.confirm(t, answer.Token, sign)
			if waited := time.Since(started); waited < 300*time.Millisecond {
				t.Errorf("the confirmation refused after %v, without waiting", waited)
			}
			if status != http.StatusConflict || refusalOf(data) != "Nothing was written: another Desk process is changing this project's jpack.json or its lock; try again." {
				t.Errorf("the confirmation answered %d %s", status, data)
			}
			if strings.Contains(string(data), u.project) || strings.Contains(string(data), u.s.configDir) {
				t.Errorf("the refusal names a path: %s", data)
			}
			sameProject(t, before, treeOf(t, u.project), "a confirmation refused for the lock")
			if got := u.keyFiles(t); len(got) != 0 {
				t.Errorf("a refused confirmation made %q", got)
			}

			shortProjectWait(t, 10*time.Second)
			go func() {
				time.Sleep(200 * time.Millisecond)
				release()
			}()
			started = time.Now()
			status, data = u.confirm(t, answer.Token, sign)
			if waited := time.Since(started); waited < 200*time.Millisecond {
				t.Errorf("the confirmation went on after %v, while the lock was held", waited)
			}
			if status != http.StatusOK || readFile(t, filepath.Join(u.project, "jpack.json")) != config {
				t.Errorf("once the lock was released the confirmation answered %d %s", status, data)
			}
		})
	}
}

// **Two Desk processes on one project upgrade it one at a time** (the line
// audit's finding 2). Desk B, with a configuration folder of its own, upgrades
// the project unsigned: it publishes jpack.json and stops in its `packs lock`.
// Desk A reviews that configuration and confirms a signing upgrade over it:
// it waits for the project's lock, and refuses, with no key made. B's lock
// then fails, and B puts the project back as it was; A's confirmation never
// wrote anything for B's rollback to undo.
func TestTwoDesksOnOneProjectUpgradeOneAtATime(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	project := t.TempDir()
	writeProject(t, project, map[string]string{"jpack.json": upgradeBefore, "packs/a.json": reviewPack, "packs/b.json": otherPack, ".gitignore": "node_modules/\n", ".git/HEAD": "ref: refs/heads/main\n"})
	p := newPause(t)
	rigA := newReviewRigReading(t, withAuditVersions, "", "")
	rigA.answers(t, "error")
	rigB := newReviewRigReading(t, withAuditVersions, "", p.fragment)
	rigB.answers(t, "error")
	sA, tsA := startDesk(t, Config{ProjectDir: project, JpackBin: rigA.bin, Token: testToken, DeskConfigDir: t.TempDir(), Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { sA.Close(); tsA.Close() })
	sB, tsB := startDesk(t, Config{ProjectDir: project, JpackBin: rigB.bin, Token: testToken, DeskConfigDir: t.TempDir(), Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { sB.Close(); tsB.Close() })
	before := treeOf(t, project)

	offerB := readUpgrade(t, tsB, "", true)
	// B's lock will pin another configuration than the one it wrote.
	rigB.locks(t, upgradeLock(t, project, upgradeBefore, bothPacks))
	answered := make(chan answer, 1)
	go func() {
		status, data := confirmUpgrade(t, tsB, "", offerB.Token, true)
		answered <- answer{status: status, body: data}
	}()
	p.reached(t)
	if got := readFile(t, filepath.Join(project, "jpack.json")); got != upgradeAfter {
		t.Fatalf("B has not published its configuration: %q", got)
	}

	status, data := reviewCall(t, tsA, "GET", "/api/upgrade?signingKey=true", "", nil, bearer)
	var offerA upgradeAnswer
	if status != http.StatusOK || json.Unmarshal(data, &offerA) != nil || !offerA.Sign || offerA.ConfigBefore != upgradeAfter {
		t.Fatalf("A's offer, over B's configuration, answered %d %s", status, data)
	}
	shortProjectWait(t, 300*time.Millisecond)
	status, data = reviewCall(t, tsA, "POST", "/api/upgrade", "", map[string]any{"token": offerA.Token, "requireComparableFacts": true, "signingKey": true}, bearer)
	if status != http.StatusConflict || refusalOf(data) != "Nothing was written: another Desk process is changing this project's jpack.json or its lock; try again." {
		t.Errorf("A's signing upgrade, while B's upgrade ran, answered %d %s", status, data)
	}
	if names := namesIn(t, filepath.Join(sA.configDir, "secrets", "signing")); len(names) != 0 {
		t.Errorf("A made %q while B's upgrade ran", names)
	}

	p.release(t)
	var got answer
	select {
	case got = <-answered:
	case <-time.After(30 * time.Second):
		t.Fatal("B never answered")
	}
	if got.status != http.StatusConflict || !strings.Contains(refusalOf(got.body), "so every file was put back as it was") {
		t.Errorf("B answered %d %s", got.status, got.body)
	}
	sameProject(t, before, treeOf(t, project), "B's upgrade put back")
}

// **A rollback puts back only over what it wrote.** An upgrade publishes
// jpack.json and `.gitignore`, and its lock fails. Where another writer has
// changed jpack.json meanwhile, or changes the lock, or jpack.json, between
// the old bytes being staged and published, that file is left as the other
// writer left it, and the answer says so; every other file is put back.
func TestARollbackPutsBackOnlyWhatItWrote(t *testing.T) {
	const theirs = `{"configVersion":"3","packs":{},"theirs":true}` + "\n"
	const theirLock = `{"lockVersion":"1","theirs":true}` + "\n"
	for _, tc := range []struct {
		name, lockFirst, at, file, content string
	}{
		{"jpack.json, written while the runtime locked", "  printf '%s\\n' '" + strings.TrimSuffix(theirs, "\n") + "' > jpack.json", "", "jpack.json", theirs},
		{"jpack.json, written after the old bytes were staged", "", "jpack.json", "jpack.json", theirs},
		{"the lock, written after the runtime ran", "", runtimeLockName, runtimeLockName, theirLock},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ts, rig, project := upgradeProject(t, allConfigVersions, tc.lockFirst, nil)
			rig.answers(t, "error")
			offer := readUpgrade(t, ts, "", true)
			rig.locks(t, upgradeLock(t, project, upgradeBefore, bothPacks))
			if tc.at != "" {
				testHookBeforePutBack = func(name string) {
					if name == tc.at {
						if err := os.WriteFile(filepath.Join(project, tc.file), []byte(tc.content), 0o644); err != nil {
							t.Error(err)
						}
					}
				}
				t.Cleanup(func() { testHookBeforePutBack = nil })
			}
			status, data := confirmUpgrade(t, ts, "", offer.Token, true)
			testHookBeforePutBack = nil
			if status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), "the project could not be put back as it was: "+tc.file+": another writer changed it after Desk wrote it, so it was left as it is") {
				t.Errorf("the confirmation answered %d %s", status, data)
			}
			if got := readFile(t, filepath.Join(project, tc.file)); got != tc.content {
				t.Errorf("%s holds %q, not what the other writer wrote", tc.file, got)
			}
			if got := readFile(t, filepath.Join(project, ".gitignore")); got != "node_modules/\n" {
				t.Errorf(".gitignore was not put back: %q", got)
			}
			if tc.file != "jpack.json" {
				if got := readFile(t, filepath.Join(project, "jpack.json")); got != upgradeBefore {
					t.Errorf("jpack.json was not put back: %q", got)
				}
			}
		})
	}
}

// **A review's lock waits for this project's lock, and puts the previous
// lock back only over its own.** While another Desk process holds the
// project's lock, a confirmation locks nothing and says so; where the lock it
// put in place fails its check and another writer replaces it before the
// previous one is published, that writer's lock is left, and the answer says
// the previous lock could not be put back.
func TestAReviewLockWaitsForThisProjectsLockAndPutsBackOnlyItsOwn(t *testing.T) {
	_, ts, rig, project := reviewProject(t, "", "")
	rig.answers(t, "valid")
	review := readReview(t, ts, "")
	lockBefore := readFile(t, filepath.Join(project, runtimeLockName))

	release := holdSigningLock(t, project)
	shortProjectWait(t, 300*time.Millisecond)
	status, data := confirm(t, ts, "", review.Token)
	if status != http.StatusConflict || refusalOf(data) != "Nothing was locked: another Desk process is changing this project's jpack.json or its lock; try again." {
		t.Errorf("the confirmation answered %d %s", status, data)
	}
	if got := readFile(t, filepath.Join(project, runtimeLockName)); got != lockBefore {
		t.Errorf("a refused confirmation wrote the lock: %q", got)
	}
	release()

	const theirLock = `{"lockVersion":"1","theirs":true}` + "\n"
	rig.locks(t, lockOf(t, project, map[string]string{"alpha": "packs/a.json"}))
	testHookBeforePutBack = func(name string) {
		if name == runtimeLockName {
			if err := os.WriteFile(filepath.Join(project, runtimeLockName), []byte(theirLock), 0o644); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(func() { testHookBeforePutBack = nil })
	status, data = confirm(t, ts, "", review.Token)
	testHookBeforePutBack = nil
	if status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), "the previous lock could not be put back: another writer changed it after Desk wrote it, so it was left as it is") {
		t.Errorf("the confirmation answered %d %s", status, data)
	}
	if got := readFile(t, filepath.Join(project, runtimeLockName)); got != theirLock {
		t.Errorf("the lock is %q, not the other writer's", got)
	}
}
