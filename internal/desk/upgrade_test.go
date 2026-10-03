package desk

// The upgrade offer for existing desks (ADR-0009, section 4): nothing is
// written before the owner confirms, a confirmation writes exactly what the
// offer showed, and the configuration and the first lock go together. These
// tests drive a stand-in runtime by absolute path, built from shell builtins,
// so they run where no runtime is installed. The last test drives the real
// runtime and skips without one.

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The configuration of a project Desk did not write: hand-formatted, with a
// description, at configVersion "3".
const upgradeBefore = `{
  "configVersion": "3",
  "packs": {
    "alpha": {"path": "packs/a.json"},
    "beta": {"path": "packs/b.json", "description": "Second"}
  }
}
`

// What the upgrade writes over it, spelled out here, so that any change to
// what the upgrade writes is a change to a test.
const upgradeAfter = `{
  "configVersion": "5",
  "requireReviewed": true,
  "requireComparableFacts": true,
  "audit": {
    "dir": ".desk-private/audit"
  },
  "packs": {
    "alpha": {"path": "packs/a.json"},
    "beta": {"path": "packs/b.json", "description": "Second"}
  }
}
`

// The same, with requireComparableFacts declined.
const upgradeAfterDeclined = `{
  "configVersion": "5",
  "requireReviewed": true,
  "audit": {
    "dir": ".desk-private/audit"
  },
  "packs": {
    "alpha": {"path": "packs/a.json"},
    "beta": {"path": "packs/b.json", "description": "Second"}
  }
}
`

func TestUpgradedConfigKeepsEveryOtherByte(t *testing.T) {
	for _, tc := range []struct {
		name, before string
		to           string
		facts        bool
		want         string
		changed      []string
	}{
		{"a desk made before new desks were gated", `{"configVersion":"3","packs":{}}` + "\n", "5", true,
			wantGatedConfig, []string{"configVersion", "requireReviewed", "requireComparableFacts", "audit"}},
		{"the same, declining requireComparableFacts", `{"configVersion":"3","packs":{}}` + "\n", "5", false,
			`{"configVersion":"5","requireReviewed":true,"audit":{"dir":".desk-private/audit"},"packs":{}}` + "\n", []string{"configVersion", "requireReviewed", "audit"}},
		{"the same, under a runtime that reads 4", `{"configVersion":"3","packs":{}}` + "\n", "4", false,
			wantGatedConfigV4, []string{"configVersion", "requireReviewed", "audit"}},
		{"a hand-formatted project", upgradeBefore, "5", true, upgradeAfter, []string{"configVersion", "requireReviewed", "requireComparableFacts", "audit"}},
		{"members before and after configVersion, their own escapes and numbers", "{\n    \"packs\": {\"alpha\": {\"path\": \"packs/a.json\", \"description\": \"Caf\\u00e9 \\/ rule\"}},\n    \"configVersion\": \"2\",\n    \"graphs\": { },\n    \"x\": 12345678901234567890.0e0\n}",
			"5", false,
			"{\n    \"packs\": {\"alpha\": {\"path\": \"packs/a.json\", \"description\": \"Caf\\u00e9 \\/ rule\"}},\n    \"configVersion\": \"5\",\n    \"requireReviewed\": true,\n    \"audit\": {\n        \"dir\": \".desk-private/audit\"\n    },\n    \"graphs\": { },\n    \"x\": 12345678901234567890.0e0\n}",
			[]string{"configVersion", "requireReviewed", "audit"}},
		{"members already there are changed in place, and an audit directory is kept", "{\r\n\t\"configVersion\" : \"4\",\r\n\t\"requireReviewed\" : false,\r\n\t\"requireComparableFacts\" : false,\r\n\t\"audit\" : {\"dir\": \"records\"},\r\n\t\"packs\" : {}\r\n}",
			"5", true,
			"{\r\n\t\"configVersion\" : \"5\",\r\n\t\"requireReviewed\" : true,\r\n\t\"requireComparableFacts\" : true,\r\n\t\"audit\" : {\"dir\": \"records\"},\r\n\t\"packs\" : {}\r\n}",
			[]string{"configVersion", "requireReviewed", "requireComparableFacts"}},
		{"line endings and tabs carried into what is added", "{\r\n\t\"configVersion\": \"3\",\r\n\t\"packs\": {}\r\n}\r\n", "5", false,
			"{\r\n\t\"configVersion\": \"5\",\r\n\t\"requireReviewed\": true,\r\n\t\"audit\": {\r\n\t\t\"dir\": \".desk-private/audit\"\r\n\t},\r\n\t\"packs\": {}\r\n}\r\n",
			[]string{"configVersion", "requireReviewed", "audit"}},
		{"an added member follows the one before it in a new desk's order", `{"configVersion":"3","packs":{},"requireReviewed":true}`, "5", true,
			`{"configVersion":"5","packs":{},"requireReviewed":true,"requireComparableFacts":true,"audit":{"dir":".desk-private/audit"}}`,
			[]string{"configVersion", "requireComparableFacts", "audit"}},
		{"configVersion last", `{"packs": {}, "configVersion": "4"}`, "5", true,
			`{"packs": {}, "configVersion": "5", "requireReviewed": true, "requireComparableFacts": true, "audit": {"dir": ".desk-private/audit"}}`,
			[]string{"configVersion", "requireReviewed", "requireComparableFacts", "audit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			members, err := configMembers([]byte(tc.before))
			if err != nil {
				t.Fatal(err)
			}
			got, changed := upgradedConfig([]byte(tc.before), members, tc.to, tc.facts)
			if string(got) != tc.want {
				t.Errorf("upgraded to\n%s\nwant\n%s", got, tc.want)
			}
			if !reflect.DeepEqual(changed, tc.changed) {
				t.Errorf("changed %v, want %v", changed, tc.changed)
			}
		})
	}
}

func TestConfigMembersRefusesWhatItCannotCarry(t *testing.T) {
	for _, data := range []string{
		`{"configVersion":"3","configVersion":"4","packs":{}}`,
		`["configVersion"]`,
		`{"configVersion":"3"} {}`,
		`{"configVersion":"3"`,
		`{"configVersion":"3",}`,
	} {
		if _, err := configMembers([]byte(data)); err == nil {
			t.Errorf("%s was read", data)
		}
	}
}

func TestIgnoresDeskPrivate(t *testing.T) {
	for data, want := range map[string]bool{
		"":                                    false,
		"node_modules/\n":                     false,
		".desk-private/\n":                    true,
		"/.desk-private\r\n":                  true,
		".desk-private/*   \n":                true,
		"# .desk-private/\n":                  false,
		".desk-private/audit\n":               false,
		".desk-private/\n!.desk-private/a\n":  false,
		"!.desk-private/\n.desk-private/\n":   true,
		"x/.desk-private/\n":                  false,
		"node_modules/\n/.desk-private/**\n":  true,
		".desk-private/\n# !.desk-private/\n": true,
	} {
		if got := ignoresDeskPrivate([]byte(data)); got != want {
			t.Errorf("%q: %v, want %v", data, got, want)
		}
	}
	if got := string(withDeskPrivateIgnored([]byte("a/\r\nb/"))); got != "a/\r\nb/\r\n.desk-private/\r\n" {
		t.Errorf("appended as %q", got)
	}
}

// upgradeProject is a startup desk over a project Desk did not write: two
// packs, a configuration at "3", in a Git work tree whose .gitignore does not
// ignore .desk-private, and the stand-in behind it. files replace or add to
// it; a name mapped to "" is removed.
func upgradeProject(t *testing.T, versions, lockFirst string, files map[string]string) (*Server, *httptest.Server, *reviewRig, string) {
	t.Helper()
	rig := newReviewRigReading(t, versions, "", lockFirst)
	project := t.TempDir()
	base := map[string]string{"jpack.json": upgradeBefore, "packs/a.json": reviewPack, "packs/b.json": otherPack, ".gitignore": "node_modules/\n", ".git/HEAD": "ref: refs/heads/main\n"}
	for name, body := range files {
		base[name] = body
	}
	for name, body := range base {
		if body != "" {
			writeProject(t, project, map[string]string{name: body})
		}
	}
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s.Close() })
	t.Cleanup(ts.Close)
	return s, ts, rig, project
}

func readUpgrade(t *testing.T, ts *httptest.Server, desk string, facts bool) upgradeAnswer {
	t.Helper()
	path := "/api/upgrade"
	if !facts {
		path += "?requireComparableFacts=false"
	}
	status, data := reviewCall(t, ts, "GET", path, desk, nil, bearer)
	var answer upgradeAnswer
	if status != 200 || json.Unmarshal(data, &answer) != nil {
		t.Fatalf("the offer answered %d %s", status, data)
	}
	return answer
}

func confirmUpgrade(t *testing.T, ts *httptest.Server, desk, token string, facts bool) (int, []byte) {
	t.Helper()
	return reviewCall(t, ts, "POST", "/api/upgrade", desk, map[string]any{"token": token, "requireComparableFacts": facts}, bearer)
}

// upgradeLock is the lock the runtime writes over config and the project's
// packs as they are.
func upgradeLock(t *testing.T, project, config string, packs map[string]string) []byte {
	t.Helper()
	lock := map[string]any{"lockVersion": "1", "config": map[string]string{"digest": sha256Digest([]byte(config))}}
	entries := map[string]lockEntry{}
	for id, path := range packs {
		entries[id] = lockEntry{Path: path, Digest: sha256Digest([]byte(readFile(t, filepath.Join(project, path))))}
	}
	lock["packs"] = entries
	data, _ := json.MarshalIndent(lock, "", "  ")
	return append(data, '\n')
}

var bothPacks = map[string]string{"alpha": "packs/a.json", "beta": "packs/b.json"}

// treeEntry is one entry of a project, as a test compares it.
type treeEntry struct {
	mode fs.FileMode
	data string
}

// treeOf is every entry under dir, hidden ones included: each file's mode
// and bytes, and each folder's mode.
func treeOf(t *testing.T, dir string) map[string]treeEntry {
	t.Helper()
	tree := map[string]treeEntry{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		row := treeEntry{mode: info.Mode()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			row.data = string(data)
		}
		tree[filepath.ToSlash(rel)] = row
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// sameProject reports every difference between two trees, as a test error.
func sameProject(t *testing.T, before, after map[string]treeEntry, why string) {
	t.Helper()
	for name, entry := range before {
		if got, ok := after[name]; !ok {
			t.Errorf("%s: %s is gone", why, name)
		} else if got != entry {
			t.Errorf("%s: %s changed from %v %q to %v %q", why, name, entry.mode, entry.data, got.mode, got.data)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			t.Errorf("%s: %s was made", why, name)
		}
	}
}

// An offer, for either choice, and an offer left unconfirmed, change no byte
// of the project and make nothing in it.
func TestDecliningTheUpgradeWritesNothing(t *testing.T) {
	_, ts, rig, project := upgradeProject(t, allConfigVersions, "", map[string]string{"jpack.lock.json": `{"lockVersion":"1","config":{"digest":"sha256:0"}}` + "\n"})
	rig.answers(t, "invalid", map[string]string{"name": "config-drift", "path": "jpack.json"})
	before := treeOf(t, project)
	for _, facts := range []bool{true, false} {
		if answer := readUpgrade(t, ts, "", facts); answer.State != "offer" || answer.Token == "" {
			t.Fatalf("the offer answered %+v", answer)
		}
	}
	sameProject(t, before, treeOf(t, project), "an offer")
	if n := countCalls(t, rig.calls, "packs lock"); n != 0 {
		t.Errorf("packs lock ran %d time(s) without a confirmation", n)
	}
}

// A confirmation writes exactly the ADR's changes: the configuration, with
// every other member and the members' order kept; the line in .gitignore;
// the owner-only audit folder; and the lock. Nothing else changes.
func TestAcceptingTheUpgradeWritesExactlyItsChanges(t *testing.T) {
	_, ts, rig, project := upgradeProject(t, allConfigVersions, "", nil)
	rig.answers(t, "error")
	before := treeOf(t, project)
	answer := readUpgrade(t, ts, "", true)
	if answer.State != "offer" || answer.From != "3" || answer.To != "5" || answer.ConfigBefore != upgradeBefore || answer.ConfigAfter != upgradeAfter ||
		!reflect.DeepEqual(answer.Changes, []string{"configVersion", "requireReviewed", "requireComparableFacts", "audit"}) ||
		answer.Gitignore != "add" || answer.Audit == nil || *answer.Audit != (auditPlan{State: "create", Dir: ".desk-private/audit"}) ||
		answer.Locked || answer.Gated || answer.ComparableFacts != "off" || !answer.RequireComparableFacts || answer.Review == nil || len(answer.Token) != 64 {
		t.Fatalf("the offer answered %+v", answer)
	}
	// The first lock's review is over the upgraded configuration.
	if shown(*answer.Review, fileOf(t, *answer.Review, "jpack.json").Now) != upgradeAfter || shown(*answer.Review, fileOf(t, *answer.Review, "packs/b.json").Now) != otherPack || answer.Review.Token != "" {
		t.Errorf("the first lock's review is %+v", answer.Review)
	}
	lock := upgradeLock(t, project, upgradeAfter, bothPacks)
	rig.locks(t, lock)
	status, data := confirmUpgrade(t, ts, "", answer.Token, true)
	if status != 200 || !bytes.Contains(data, []byte(`"files":3`)) || !bytes.Contains(data, []byte(`"configVersion":"5"`)) || !bytes.Contains(data, []byte(`"copies":"stored"`)) {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	after := treeOf(t, project)
	want := map[string]string{"jpack.json": upgradeAfter, ".gitignore": "node_modules/\n.desk-private/\n", "jpack.lock.json": string(lock)}
	for name, body := range want {
		if after[name].data != body {
			t.Errorf("%s is\n%s\nwant\n%s", name, after[name].data, body)
		}
		before[name] = after[name]
	}
	for _, folder := range []string{".desk-private", ".desk-private/audit"} {
		if entry, ok := after[folder]; !ok || !entry.mode.IsDir() || entry.mode.Perm() != 0o700 {
			t.Errorf("%s is %v, want an owner-only folder", folder, entry.mode)
		}
		before[folder] = after[folder]
	}
	// PR C's step keeps a copy of each file it locked.
	for name, entry := range after {
		if strings.HasPrefix(name, ".desk-private/reviewed") {
			before[name] = entry
		}
	}
	if len(copiesIn(t, project)) != 4 {
		t.Errorf("the copies are %v", copiesIn(t, project))
	}
	sameProject(t, before, after, "the upgrade")
	if got := readFile(t, rig.calls); !strings.Contains(got, "packs lock --config jpack.json --format json [JPACK_CONFIG=unset]") {
		t.Errorf("the runtime ran:\n%s", got)
	}
	// Once upgraded, there is nothing to offer.
	if again := readUpgrade(t, ts, "", true); again.State != "unchanged" || !again.Gated || again.ComparableFacts != "on" || again.Token != "" {
		t.Errorf("after the upgrade the offer answered %+v", again)
	}
}

// A project that declares its own audit directory keeps it, and Desk makes
// no audit folder of its own.
func TestAnExistingAuditDirectoryIsKept(t *testing.T) {
	config := strings.Replace(upgradeBefore, `"configVersion": "3",`, `"configVersion": "3",`+"\n  \"audit\": {\"dir\": \"records\"},", 1)
	_, ts, rig, project := upgradeProject(t, allConfigVersions, "", map[string]string{"jpack.json": config})
	rig.answers(t, "error")
	answer := readUpgrade(t, ts, "", true)
	want := strings.Replace(config, `"configVersion": "3",`, `"configVersion": "5",`+"\n  \"requireReviewed\": true,\n  \"requireComparableFacts\": true,", 1)
	if answer.ConfigAfter != want || *answer.Audit != (auditPlan{State: "kept", Dir: "records"}) || slicesContain(answer.Changes, "audit") {
		t.Fatalf("the offer answered %+v", answer)
	}
	rig.locks(t, upgradeLock(t, project, want, bothPacks))
	if status, data := confirmUpgrade(t, ts, "", answer.Token, true); status != 200 {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	if got := readFile(t, filepath.Join(project, "jpack.json")); got != want {
		t.Errorf("jpack.json is\n%s", got)
	}
	if _, err := os.Lstat(filepath.Join(project, ".desk-private", "audit")); !os.IsNotExist(err) {
		t.Errorf("an audit folder was made: %v", err)
	}
}

func slicesContain(list []string, item string) bool {
	for _, each := range list {
		if each == item {
			return true
		}
	}
	return false
}

// .gitignore gains `.desk-private/` only where the project is in a Git work
// tree and does not already ignore it, and is made where there is none.
func TestTheUpgradeAndGitignore(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		state string
		want  string // "" where there is no .gitignore after
	}{
		{"already ignored", map[string]string{".gitignore": "node_modules/\n/.desk-private/\n"}, "ignored", "node_modules/\n/.desk-private/\n"},
		{"not ignored, without a last newline, with CRLF", map[string]string{".gitignore": "a/\r\nb/"}, "add", "a/\r\nb/\r\n.desk-private/\r\n"},
		{"no .gitignore in a Git work tree", map[string]string{".gitignore": ""}, "create", ".desk-private/\n"},
		{"a linked work tree's .git file", map[string]string{".git/HEAD": "", ".git": "gitdir: /elsewhere\n"}, "add", "node_modules/\n.desk-private/\n"},
		{"not a Git work tree", map[string]string{".git/HEAD": ""}, "outside", "node_modules/\n"},
		{"a .git folder that is not a repository", map[string]string{".git/HEAD": "", ".git/config": "[core]\n"}, "outside", "node_modules/\n"},
		{"not a Git work tree, and no .gitignore", map[string]string{".git/HEAD": "", ".gitignore": ""}, "outside", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, ts, rig, project := upgradeProject(t, allConfigVersions, "", tc.files)
			if tc.state == "outside" && s.inGitWorkTree() {
				t.Skip("the test's temporary folder is inside a Git work tree")
			}
			rig.answers(t, "error")
			answer := readUpgrade(t, ts, "", true)
			if answer.Gitignore != tc.state {
				t.Fatalf("the offer's .gitignore is %q, want %q", answer.Gitignore, tc.state)
			}
			rig.locks(t, upgradeLock(t, project, upgradeAfter, bothPacks))
			if status, data := confirmUpgrade(t, ts, "", answer.Token, true); status != 200 {
				t.Fatalf("the confirmation answered %d %s", status, data)
			}
			data, err := os.ReadFile(filepath.Join(project, ".gitignore"))
			if tc.want == "" {
				if !os.IsNotExist(err) {
					t.Errorf(".gitignore was made: %q", data)
				}
			} else if string(data) != tc.want {
				t.Errorf(".gitignore is %q, want %q", data, tc.want)
			}
		})
	}
}

// The configuration and the first lock go together: a lock the runtime
// refuses, or one that pins anything but what was shown, puts every file
// back as it was, the lock included, and removes what the upgrade made.
func TestAFailedUpgradeLockPutsEveryFileBack(t *testing.T) {
	refuse := "  printf 'half' > jpack.lock.json\n  printf '%s\\n' '{\"outputVersion\":\"2\",\"command\":\"packs lock\",\"status\":\"error\",\"diagnostics\":[{\"code\":\"JPS-STAND-IN\",\"message\":\"The stand-in refuses.\"}]}'\n  exit 1"
	priorLock := `{"lockVersion":"1","config":{"digest":"sha256:0"}}` + "\n"
	for _, tc := range []struct {
		name, lockFirst string
		files           map[string]string
		// pinsOld makes the stand-in write a lock of the configuration as it
		// was, not as the upgrade wrote it.
		pinsOld bool
		status  int
		says    string
	}{
		{"the runtime refuses, where there was no lock", refuse, nil, false, 500, "The stand-in refuses"},
		{"the runtime refuses, over a lock", refuse, map[string]string{"jpack.lock.json": priorLock}, false, 500, "The stand-in refuses"},
		{"the runtime refuses, where there was no .gitignore", refuse, map[string]string{".gitignore": ""}, false, 500, "The stand-in refuses"},
		{"the lock pins another configuration", "", map[string]string{"jpack.lock.json": priorLock}, true, 409, "A file changed while the project was being locked"},
		{"a pack changes while the runtime locks", "  printf 'changed' > packs/b.json", nil, false, 409, "A file changed while the project was being locked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ts, rig, project := upgradeProject(t, allConfigVersions, tc.lockFirst, tc.files)
			rig.answers(t, "error")
			before := treeOf(t, project)
			answer := readUpgrade(t, ts, "", true)
			pinned, pinnedFrom := upgradeAfter, project
			if tc.pinsOld {
				pinned = upgradeBefore
			}
			if tc.name == "a pack changes while the runtime locks" {
				// The runtime locks what it finds: the changed pack.
				pinnedFrom = t.TempDir()
				writeProject(t, pinnedFrom, map[string]string{"packs/a.json": reviewPack, "packs/b.json": "changed"})
			}
			rig.locks(t, upgradeLock(t, pinnedFrom, pinned, bothPacks))
			status, data := confirmUpgrade(t, ts, "", answer.Token, true)
			if status != tc.status || !bytes.Contains(data, []byte(tc.says)) || !bytes.Contains(data, []byte("every file was put back")) {
				t.Fatalf("the confirmation answered %d %s", status, data)
			}
			after := treeOf(t, project)
			if tc.name == "a pack changes while the runtime locks" {
				before["packs/b.json"] = after["packs/b.json"]
			}
			sameProject(t, before, after, "a failed upgrade")
		})
	}
}

// A confirmation of an offer the project has since left writes nothing.
func TestAStaleUpgradeConfirmationWritesNothing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  map[string]string
		change func(t *testing.T, project string)
	}{
		{"a pack", nil, func(t *testing.T, project string) {
			writeProject(t, project, map[string]string{"packs/a.json": strings.Replace(reviewPack, "Minimal", "Edited", 1)})
		}},
		{"jpack.json", nil, func(t *testing.T, project string) {
			writeProject(t, project, map[string]string{"jpack.json": strings.Replace(upgradeBefore, "Second", "Other", 1)})
		}},
		{".gitignore", nil, func(t *testing.T, project string) {
			writeProject(t, project, map[string]string{".gitignore": "dist/\n"})
		}},
		{"the lock", map[string]string{"jpack.lock.json": `{"lockVersion":"1","config":{"digest":"sha256:0"}}` + "\n"}, func(t *testing.T, project string) {
			writeProject(t, project, map[string]string{"jpack.lock.json": `{"lockVersion":"1","config":{"digest":"sha256:1"}}` + "\n"})
		}},
		{"a lock where there was none", nil, func(t *testing.T, project string) {
			writeProject(t, project, map[string]string{"jpack.lock.json": "{}\n"})
		}},
		{"the audit folder", nil, func(t *testing.T, project string) {
			if err := os.MkdirAll(filepath.Join(project, ".desk-private", "audit"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ts, rig, project := upgradeProject(t, allConfigVersions, "", tc.files)
			rig.answers(t, "error")
			token := readUpgrade(t, ts, "", true).Token
			tc.change(t, project)
			before := treeOf(t, project)
			rig.locks(t, upgradeLock(t, project, upgradeAfter, bothPacks))
			if status, data := confirmUpgrade(t, ts, "", token, true); status != http.StatusConflict || !bytes.Contains(data, []byte(`"code":"stale"`)) {
				t.Errorf("the confirmation answered %d %s", status, data)
			}
			sameProject(t, before, treeOf(t, project), "a stale confirmation")
			if n := countCalls(t, rig.calls, "packs lock"); n != 0 {
				t.Errorf("packs lock ran %d time(s)", n)
			}
		})
	}

	// A token confirms its own choice, and a review's token no upgrade.
	_, ts, rig, project := upgradeProject(t, allConfigVersions, "", nil)
	rig.answers(t, "error")
	before := treeOf(t, project)
	withFacts := readUpgrade(t, ts, "", true).Token
	review := readReview(t, ts, "").Token
	rig.locks(t, upgradeLock(t, project, upgradeAfterDeclined, bothPacks))
	for name, token := range map[string]string{"the other choice's token": withFacts, "a review's token": review} {
		if status, data := confirmUpgrade(t, ts, "", token, false); status != http.StatusConflict {
			t.Errorf("%s answered %d %s", name, status, data)
		}
	}
	sameProject(t, before, treeOf(t, project), "a token for something else")
}

// The writes the upgrade makes are conditional: each replaces only the bytes
// the offer read.
func TestTheUpgradeWritesOnlyOverTheBytesItRead(t *testing.T) {
	s, _, _, project := upgradeProject(t, allConfigVersions, "", nil)
	undo := &upgradeUndo{s: s}
	if err := undo.write("jpack.json", []byte(upgradeAfter), true, []byte("{}")); err != errUpgradeMoved {
		t.Errorf("a write over other bytes answered %v", err)
	}
	if err := undo.write(".gitignore", nil, false, []byte(".desk-private/\n")); err != errUpgradeMoved {
		t.Errorf("a create over a file answered %v", err)
	}
	if readFile(t, filepath.Join(project, "jpack.json")) != upgradeBefore || readFile(t, filepath.Join(project, ".gitignore")) != "node_modules/\n" || len(undo.written) != 0 {
		t.Error("a write that did not hold its bytes wrote")
	}
}

// requireComparableFacts is its own item: the owner can take the rest and
// decline it, and it is still offered afterwards.
func TestRequireComparableFactsCanBeDeclinedAlone(t *testing.T) {
	_, ts, rig, project := upgradeProject(t, allConfigVersions, "", nil)
	rig.answers(t, "error")
	declined := readUpgrade(t, ts, "", false)
	if declined.ConfigAfter != upgradeAfterDeclined || declined.RequireComparableFacts || declined.ComparableFacts != "off" || slicesContain(declined.Changes, "requireComparableFacts") {
		t.Fatalf("the offer without requireComparableFacts answered %+v", declined)
	}
	rig.locks(t, upgradeLock(t, project, upgradeAfterDeclined, bothPacks))
	if status, data := confirmUpgrade(t, ts, "", declined.Token, false); status != 200 || !bytes.Contains(data, []byte(`"requireComparableFacts":false`)) {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	if got := readFile(t, filepath.Join(project, "jpack.json")); got != upgradeAfterDeclined {
		t.Fatalf("jpack.json is\n%s", got)
	}
	if again := readUpgrade(t, ts, "", false); again.State != "unchanged" || !again.Gated || again.ComparableFacts != "off" {
		t.Errorf("declined again, the offer answered %+v", again)
	}
	later := readUpgrade(t, ts, "", true)
	if later.State != "offer" || !later.Gated || later.ConfigAfter != upgradeAfter || !reflect.DeepEqual(later.Changes, []string{"requireComparableFacts"}) {
		t.Fatalf("the later offer answered %+v", later)
	}
	rig.locks(t, upgradeLock(t, project, upgradeAfter, bothPacks))
	if status, data := confirmUpgrade(t, ts, "", later.Token, true); status != 200 {
		t.Fatalf("the later confirmation answered %d %s", status, data)
	}
	if got := readFile(t, filepath.Join(project, "jpack.json")); got != upgradeAfter {
		t.Errorf("jpack.json is\n%s", got)
	}
}

// Desk offers no configuration version the runtime cannot read.
func TestTheUpgradeOffersOnlyWhatTheRuntimeReads(t *testing.T) {
	_, ts, rig, project := upgradeProject(t, upToVersion4, "", nil)
	rig.answers(t, "error")
	answer := readUpgrade(t, ts, "", true)
	want := strings.Replace(upgradeAfterDeclined, `"configVersion": "5"`, `"configVersion": "4"`, 1)
	if answer.State != "offer" || answer.To != "4" || answer.ComparableFacts != "unavailable" || answer.RequireComparableFacts || answer.ConfigAfter != want {
		t.Fatalf("under a runtime that reads 4, the offer answered %+v", answer)
	}
	rig.locks(t, upgradeLock(t, project, want, bothPacks))
	if status, data := confirmUpgrade(t, ts, "", answer.Token, true); status != 200 {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	if got := readFile(t, filepath.Join(project, "jpack.json")); got != want {
		t.Errorf("jpack.json is\n%s", got)
	}

	for _, tc := range []struct {
		name, versions, config, says string
	}{
		{"a runtime that reads no 4", upToVersion3, upgradeBefore, "runtime 0.24.0 and later"},
		{"a configuration the runtime does not read", upToVersion4, strings.Replace(upgradeBefore, `"3"`, `"5"`, 1), "and not 5"},
		{"a configuration version Desk does not know", `["1","2","3","4","5","6"]`, strings.Replace(upgradeBefore, `"3"`, `"6"`, 1), "which Desk does not upgrade"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ts, rig, project := upgradeProject(t, tc.versions, "", map[string]string{"jpack.json": tc.config})
			rig.answers(t, "error")
			before := treeOf(t, project)
			if answer := readUpgrade(t, ts, "", true); answer.State != "unavailable" || !strings.Contains(answer.Reason, tc.says) || answer.Token != "" {
				t.Errorf("the offer answered %+v", answer)
			}
			if status, _ := confirmUpgrade(t, ts, "", strings.Repeat("0", 64), true); status != http.StatusConflict {
				t.Errorf("a confirmation answered %d", status)
			}
			sameProject(t, before, treeOf(t, project), "an unavailable upgrade")
		})
	}
}

// A project that already keeps a lock is told so, and the runtime's own
// finding that the new configuration drifts from it is shown.
func TestAnExistingLockIsSaidToDrift(t *testing.T) {
	_, ts, rig, project := upgradeProject(t, allConfigVersions, "", map[string]string{"jpack.lock.json": `{"lockVersion":"1","config":{"digest":"sha256:0"}}` + "\n"})
	rig.answers(t, "invalid", map[string]string{"name": "config-drift", "path": "jpack.json", "detail": "The configuration's own bytes differ from the reviewed set."})
	answer := readUpgrade(t, ts, "", true)
	if !answer.Locked || len(answer.Review.Findings) != 1 || answer.Review.Findings[0].Name != "config-drift" || !answer.Review.Locked {
		t.Fatalf("the offer answered %+v", answer)
	}
	// The runtime verified the upgraded configuration, not the project's.
	if !strings.Contains(readFile(t, rig.calls), "packs verify --config jpack.json --format json") {
		t.Errorf("the runtime ran:\n%s", readFile(t, rig.calls))
	}
	lock := upgradeLock(t, project, upgradeAfter, bothPacks)
	rig.locks(t, lock)
	if status, data := confirmUpgrade(t, ts, "", answer.Token, true); status != 200 {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	if got := readFile(t, filepath.Join(project, "jpack.lock.json")); got != string(lock) {
		t.Errorf("the lock is %s", got)
	}
}

// Nothing is written without the owner's confirmation, sent the way only
// this desk's page can send it.
func TestNothingIsUpgradedWithoutAConfirmation(t *testing.T) {
	_, ts, rig, project := upgradeProject(t, allConfigVersions, "", nil)
	rig.answers(t, "error")
	token := readUpgrade(t, ts, "", true).Token
	rig.locks(t, upgradeLock(t, project, upgradeAfter, bothPacks))
	before := treeOf(t, project)
	for _, tc := range []struct {
		name     string
		body     any
		want     int
		decorate []func(*http.Request)
	}{
		{"no session", map[string]any{"token": token, "requireComparableFacts": true}, 401, nil},
		{"another origin", map[string]any{"token": token, "requireComparableFacts": true}, 403, []func(*http.Request){bearer, func(r *http.Request) { r.Header.Set("Origin", "http://example.com") }}},
		{"cross-site", map[string]any{"token": token, "requireComparableFacts": true}, 403, []func(*http.Request){bearer, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }}},
		{"not JSON", map[string]any{"token": token, "requireComparableFacts": true}, 415, []func(*http.Request){bearer, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }}},
		{"no token", map[string]any{"requireComparableFacts": true}, 400, []func(*http.Request){bearer}},
		{"another member", map[string]any{"token": token, "requireComparableFacts": true, "force": true}, 400, []func(*http.Request){bearer}},
		{"a token the offer did not give", map[string]any{"token": strings.Repeat("0", 64), "requireComparableFacts": true}, 409, []func(*http.Request){bearer}},
	} {
		if status, data := reviewCall(t, ts, "POST", "/api/upgrade", "", tc.body, tc.decorate...); status != tc.want {
			t.Errorf("%s: %d %s, want %d", tc.name, status, data, tc.want)
		}
	}
	sameProject(t, before, treeOf(t, project), "an unconfirmed upgrade")
	if n := countCalls(t, rig.calls, "packs lock"); n != 0 {
		t.Errorf("packs lock ran %d time(s)", n)
	}
}

// A desk Desk made gated has nothing to upgrade.
func TestAGatedDeskHasNothingToUpgrade(t *testing.T) {
	rig := newReviewRig(t, "", "")
	_, ts, _ := gatesServer(t, rig.bin)
	row := createGatedDesk(t, ts)
	if answer := readUpgrade(t, ts, row.ID, true); answer.State != "unchanged" || !answer.Gated || answer.ComparableFacts != "on" || answer.Token != "" {
		t.Errorf("a gated desk's offer answered %+v", answer)
	}
	before := treeOf(t, row.Folder)
	if status, data := confirmUpgrade(t, ts, row.ID, strings.Repeat("0", 64), true); status != http.StatusConflict {
		t.Errorf("a confirmation with nothing to change answered %d %s", status, data)
	}
	sameProject(t, before, treeOf(t, row.Folder), "a confirmation with nothing to change")
}

// The startup desk upgrades only its own jpack.json; a named desk made before
// new desks were gated upgrades to exactly what a new desk is written with,
// and never reads JPACK_CONFIG.
func TestTheUpgradeOnTheStartupDeskAndANamedDesk(t *testing.T) {
	_, ts, rig, project := upgradeProject(t, allConfigVersions, "", nil)
	rig.answers(t, "error")
	elsewhere := t.TempDir()
	writeProject(t, elsewhere, map[string]string{"jpack.json": upgradeBefore})
	before := treeOf(t, project)
	t.Setenv("JPACK_CONFIG", filepath.Join(elsewhere, "jpack.json"))
	if answer := readUpgrade(t, ts, "", true); answer.State != "unavailable" || !strings.Contains(answer.Reason, "JPACK_CONFIG names") {
		t.Errorf("JPACK_CONFIG naming another project: the offer answered %+v", answer)
	}
	if status, data := confirmUpgrade(t, ts, "", strings.Repeat("0", 64), true); status != http.StatusConflict || !bytes.Contains(data, []byte("JPACK_CONFIG names")) {
		t.Errorf("JPACK_CONFIG naming another project: the confirmation answered %d %s", status, data)
	}
	sameProject(t, before, treeOf(t, project), "an upgrade JPACK_CONFIG refused")
	t.Setenv("JPACK_CONFIG", filepath.Join(project, "jpack.json"))
	if answer := readUpgrade(t, ts, "", true); answer.State != "offer" {
		t.Errorf("JPACK_CONFIG naming this project's own: the offer answered %+v", answer)
	}

	// A named desk as Desk made them before PR B: configVersion "3", no
	// lock, no audit folder. JPACK_CONFIG still names another project.
	t.Setenv("JPACK_CONFIG", filepath.Join(elsewhere, "jpack.json"))
	row := createGatedDesk(t, ts)
	writeProject(t, row.Folder, map[string]string{"jpack.json": `{"configVersion":"3","packs":{}}` + "\n"})
	for _, name := range []string{"jpack.lock.json", ".desk-private/audit"} {
		if err := os.Remove(filepath.Join(row.Folder, name)); err != nil {
			t.Fatal(err)
		}
	}
	answer := readUpgrade(t, ts, row.ID, true)
	if answer.State != "offer" || answer.ConfigAfter != wantGatedConfig || answer.Audit.State != "create" {
		t.Fatalf("the named desk's offer answered %+v", answer)
	}
	rig.locks(t, upgradeLock(t, row.Folder, wantGatedConfig, nil))
	if status, data := confirmUpgrade(t, ts, row.ID, answer.Token, true); status != 200 {
		t.Fatalf("the named desk's confirmation answered %d %s", status, data)
	}
	assertGatedFolder(t, row.Folder, wantGatedConfig)
	if _, err := os.Lstat(filepath.Join(elsewhere, "jpack.lock.json")); !os.IsNotExist(err) {
		t.Errorf("a lock was written beside JPACK_CONFIG's configuration: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(readFile(t, rig.calls)), "\n") {
		if strings.HasPrefix(line, "packs ") && !strings.HasSuffix(line, "[JPACK_CONFIG=unset]") {
			t.Errorf("a runtime command ran as %q", line)
		}
	}
}

// A confirmation is bound to its desk: another desk's token, over identical
// bytes, writes nothing.
func TestAnUpgradeConfirmationIsBoundToItsDesk(t *testing.T) {
	_, ts, rig, project := upgradeProject(t, allConfigVersions, "", map[string]string{".git/HEAD": "", ".gitignore": ""})
	if err := os.MkdirAll(filepath.Join(project, ".desk-private", "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	rig.answers(t, "error")
	row := createGatedDesk(t, ts)
	if err := os.Remove(filepath.Join(row.Folder, "jpack.lock.json")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"jpack.json", "packs/a.json", "packs/b.json"} {
		writeProject(t, row.Folder, map[string]string{name: readFile(t, filepath.Join(project, filepath.FromSlash(name)))})
	}
	startup, named := readUpgrade(t, ts, "", true), readUpgrade(t, ts, row.ID, true)
	if startup.State != "offer" || named.State != "offer" || startup.ConfigAfter != named.ConfigAfter || startup.Gitignore != named.Gitignore || *startup.Audit != *named.Audit {
		t.Fatalf("the two desks' offers differ: %+v %+v", startup, named)
	}
	before := treeOf(t, row.Folder)
	rig.locks(t, upgradeLock(t, row.Folder, upgradeAfter, bothPacks))
	if status, data := confirmUpgrade(t, ts, row.ID, startup.Token, true); status != http.StatusConflict {
		t.Errorf("another desk's confirmation answered %d %s", status, data)
	}
	sameProject(t, before, treeOf(t, row.Folder), "another desk's confirmation")
}

// The real runtime: a project Desk did not write, keeping a lock its CI
// checks, is upgraded; the lock verifies, and a deciding run by id is
// recorded in the audit folder. Then a named desk made before PR B.
func TestUpgradeWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	project := t.TempDir()
	config := strings.Replace(upgradeBefore, `,
    "beta": {"path": "packs/b.json", "description": "Second"}`, "", 1)
	writeProject(t, project, map[string]string{"jpack.json": config, "packs/a.json": reviewPack, ".gitignore": "tmp/\n", ".git/HEAD": "ref: refs/heads/main\n"})
	jpackIn(t, bin, project, "packs", "lock", "--config", "jpack.json", "--format", "json")
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s.Close(); ts.Close() })

	answer := readUpgrade(t, ts, "", true)
	if answer.State != "offer" || !answer.Locked || answer.Review == nil || len(answer.Review.Findings) != 1 || answer.Review.Findings[0].Name != "config-drift" || answer.To != "5" {
		t.Fatalf("the offer answered %+v", answer)
	}
	if status, data := confirmUpgrade(t, ts, "", answer.Token, true); status != 200 {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	want := strings.Replace(upgradeAfter, `,
    "beta": {"path": "packs/b.json", "description": "Second"}`, "", 1)
	if got := readFile(t, filepath.Join(project, "jpack.json")); got != want {
		t.Errorf("jpack.json is\n%s", got)
	}
	if got := readFile(t, filepath.Join(project, ".gitignore")); got != "tmp/\n.desk-private/\n" {
		t.Errorf(".gitignore is %q", got)
	}
	jpackIn(t, bin, project, "packs", "verify", "--config", "jpack.json", "--format", "json")
	writeProject(t, project, map[string]string{"facts.json": "{}\n"})
	out := jpackIn(t, bin, project, "experimental", "evaluate", "--pack-id", "alpha", "--facts", "facts.json", "--format", "json")
	var evaluated struct {
		Status   string `json:"status"`
		Reviewed bool   `json:"reviewed"`
	}
	if json.Unmarshal(out, &evaluated) != nil || evaluated.Status != "evaluated" || !evaluated.Reviewed {
		t.Errorf("a deciding run by id answered %s", out)
	}
	if trail := readFile(t, filepath.Join(project, ".desk-private", "audit", "evaluations.jsonl")); strings.Count(trail, "\n") != 1 {
		t.Errorf("the audit trail holds %q", trail)
	}
	if again := readUpgrade(t, ts, "", true); again.State != "unchanged" || !again.Gated {
		t.Errorf("after the upgrade the offer answered %+v", again)
	}

	// A named desk as Desk made them before PR B.
	_, named, _ := gatesServer(t, bin)
	row := createGatedDesk(t, named)
	writeProject(t, row.Folder, map[string]string{"jpack.json": `{"configVersion":"3","packs":{}}` + "\n"})
	for _, name := range []string{"jpack.lock.json", ".desk-private/audit"} {
		if err := os.Remove(filepath.Join(row.Folder, name)); err != nil {
			t.Fatal(err)
		}
	}
	offer := readUpgrade(t, named, row.ID, true)
	if offer.State != "offer" || offer.ConfigAfter != wantGatedConfig {
		t.Fatalf("the named desk's offer answered %+v", offer)
	}
	if status, data := confirmUpgrade(t, named, row.ID, offer.Token, true); status != 200 {
		t.Fatalf("the named desk's confirmation answered %d %s", status, data)
	}
	assertGatedFolder(t, row.Folder, wantGatedConfig)
	jpackIn(t, bin, row.Folder, "packs", "verify", "--config", "jpack.json", "--format", "json")
}

// Desk neither reads nor writes `.gitignore`, nor makes the audit folder,
// through a link, even one that stays inside the project: the upgrade is not
// offered.
func TestTheUpgradeGoesThroughNoLink(t *testing.T) {
	for _, tc := range []struct {
		name, link, target string
	}{
		{".gitignore", ".gitignore", "ignored.txt"},
		{".desk-private", ".desk-private", "records"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ts, rig, project := upgradeProject(t, allConfigVersions, "", map[string]string{".gitignore": "", "ignored.txt": "node_modules/\n", "records/keep": "x"})
			if err := os.Symlink(tc.target, filepath.Join(project, tc.link)); err != nil {
				t.Skip(err)
			}
			rig.answers(t, "error")
			before := treeOf(t, project)
			if answer := readUpgrade(t, ts, "", true); answer.State != "unavailable" || answer.Token != "" {
				t.Errorf("the offer answered %+v", answer)
			}
			if status, _ := confirmUpgrade(t, ts, "", strings.Repeat("0", 64), true); status != http.StatusConflict {
				t.Errorf("a confirmation answered %d", status)
			}
			sameProject(t, before, treeOf(t, project), "an upgrade through a link")
		})
	}
}
