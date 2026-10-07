package desk

// The signing key of the project Desk was started on (ADR-0010, section 1 and
// question 2; delivery row 4): offered through the upgrade as an item of its
// own, never pre-selected, only where the runtime reads "6" and Desk's
// custody can keep a key; made first, under the signing folder's lock, then
// named by jpack.json at "6" and locked, or nothing written and no key left;
// a stopped upgrade swept at the next start by whether jpack.json names the
// key; and, after it, the decision record reading the project as a desk with
// a key. A stand-in runtime, by absolute path, answers each command; the last
// test drives the published runtime and skips without one.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// signingUpgrade is a startup desk over a project Desk did not write, on a
// stand-in that reads "6": it reviews and locks as the upgrade's stand-in
// does (`upgradeProject`), makes and reads keys as a new desk's does, and
// verifies as the decision record's does (`newAuditRig`), steered by verify.
type signingUpgrade struct {
	s       *Server
	ts      *httptest.Server
	rig     *reviewRig
	audit   *auditRig
	project string
	// seed is where the project's key is made: Desk's signing folder, under
	// the hex SHA-256 of the project's resolved path, spelled out here.
	seed string
}

func newSigningUpgrade(t *testing.T, files map[string]string) *signingUpgrade {
	t.Helper()
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	s, ts, rig, project := upgradeProject(t, withAuditVersions, "", files)
	rig.answers(t, "error")
	audit := &auditRig{bin: rig.bin, calls: rig.calls, answer: filepath.Join(t.TempDir(), "verify.json")}
	script, err := os.ReadFile(rig.bin)
	if err != nil {
		t.Fatal(err)
	}
	script = bytes.Replace(script, []byte("'packs lock')\n"), []byte(auditVerifyCase(audit)+"'packs lock')\n"), 1)
	if err := os.WriteFile(rig.bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
	audit.answers(t, 0, auditValidReport)
	seed := filepath.Join(s.configDir, "secrets", "signing", digestOf([]byte(s.projectDir))+".seed")
	return &signingUpgrade{s: s, ts: ts, rig: rig, audit: audit, project: project, seed: seed}
}

// offer is the offer with the signing key chosen or not, requireComparableFacts
// left on.
func (u *signingUpgrade) offer(t *testing.T, sign bool) upgradeAnswer {
	t.Helper()
	path := "/api/upgrade"
	if sign {
		path += "?signingKey=true"
	}
	status, data := reviewCall(t, u.ts, "GET", path, "", nil, bearer)
	var answer upgradeAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil {
		t.Fatalf("the offer answered %d %s", status, data)
	}
	return answer
}

// confirm sends the token and the choice.
func (u *signingUpgrade) confirm(t *testing.T, token string, sign bool) (int, []byte) {
	t.Helper()
	return reviewCall(t, u.ts, "POST", "/api/upgrade", "", map[string]any{"token": token, "requireComparableFacts": true, "signingKey": sign}, bearer)
}

// abandon sends a confirmation whose handler is stopped, as a crash would
// stop it, and ignores the broken answer.
func (u *signingUpgrade) abandon(t *testing.T, path string, body map[string]any) {
	t.Helper()
	data, _ := json.Marshal(body)
	request, _ := http.NewRequest("POST", u.ts.URL+path, bytes.NewReader(data))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	if response, err := u.ts.Client().Do(request); err == nil {
		response.Body.Close()
	}
}

// signed is upgradeAfter at "6", with the audit member naming the seed.
func (u *signingUpgrade) signed() string {
	return strings.Replace(strings.Replace(upgradeAfter, `"configVersion": "5"`, `"configVersion": "6"`, 1),
		`"dir": ".desk-private/audit"`, `"dir": ".desk-private/audit",
    "signingKey": "`+u.seed+`"`, 1)
}

// keyFiles is every name in Desk's signing folder that starts with the
// project's name, sorted.
func (u *signingUpgrade) keyFiles(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, name := range namesIn(t, filepath.Dir(u.seed)) {
		if strings.HasPrefix(name, strings.TrimSuffix(filepath.Base(u.seed), ".seed")) {
			names = append(names, name)
		}
	}
	return names
}

// auditVerifyCase is the stand-in's `audit verify` as `newAuditRig` writes
// it: each `--public-key` file's line appended to `<calls>.keys`, and the
// answer `answers` prepared.
func auditVerifyCase(rig *auditRig) string {
	return "'audit verify')\n" +
		"  prev=; for arg do if [ \"$prev\" = --public-key ]; then IFS= read -r key < \"$arg\"; printf '%s\\n' \"$key\" >> '" + rig.calls + ".keys'; fi; prev=$arg; done\n" +
		"  answer='" + rig.answer + "'\n" +
		"  while IFS= read -r line || [ -n \"$line\" ]; do printf '%s\\n' \"$line\"; done < \"$answer\"\n" +
		"  IFS= read -r code < \"$answer.exit\"\n  exit \"$code\"\n  ;;\n"
}

// **The signing key goes inside the audit member, and every other byte is
// the file's own.** In an audit member the upgrade adds, after `dir`; in one
// that is there, after its last member, spaced as that member is; alone in
// an empty one. A desk made before PR B comes out exactly as a new signed
// desk is written. The path is a JSON string as a new desk's is: a tab and a
// line separator escaped, "<" not.
func TestTheSigningKeyIsAddedInsideTheAuditMember(t *testing.T) {
	const key = "/home/owner/.config/jpack-desk/secrets/signing/k.seed"
	for _, tc := range []struct {
		name, before, to string
		facts            bool
		key, want        string
		changed          []string
	}{
		{"a desk made before new desks were gated", `{"configVersion":"3","packs":{}}` + "\n", "6", true, key,
			string(signedDeskConfig(key)), []string{"configVersion", "requireReviewed", "requireComparableFacts", "audit", "signingKey"}},
		{"a hand-formatted project", upgradeBefore, "6", true, key,
			strings.Replace(strings.Replace(upgradeAfter, `"5"`, `"6"`, 1), `"dir": ".desk-private/audit"`, `"dir": ".desk-private/audit",
    "signingKey": "`+key+`"`, 1), []string{"configVersion", "requireReviewed", "requireComparableFacts", "audit", "signingKey"}},
		{"one line with spaces", `{"packs": {}, "configVersion": "4"}`, "6", false, key,
			`{"packs": {}, "configVersion": "6", "requireReviewed": true, "audit": {"dir": ".desk-private/audit", "signingKey": "` + key + `"}}`,
			[]string{"configVersion", "requireReviewed", "audit", "signingKey"}},
		{"a gated project's own audit member, over lines", "{\n  \"configVersion\": \"5\",\n  \"requireReviewed\": true,\n  \"audit\": {\n    \"dir\": \"records\",\n    \"chain\": true\n  },\n  \"packs\": {}\n}\n", "6", false, key,
			"{\n  \"configVersion\": \"6\",\n  \"requireReviewed\": true,\n  \"audit\": {\n    \"dir\": \"records\",\n    \"chain\": true,\n    \"signingKey\": \"" + key + "\"\n  },\n  \"packs\": {}\n}\n",
			[]string{"configVersion", "signingKey"}},
		{"an audit member on one line, kept as it is", "{\r\n\t\"configVersion\" : \"6\",\r\n\t\"requireReviewed\" : true,\r\n\t\"audit\" : {\"dir\": \"records\"},\r\n\t\"packs\" : {}\r\n}", "6", false, key,
			"{\r\n\t\"configVersion\" : \"6\",\r\n\t\"requireReviewed\" : true,\r\n\t\"audit\" : {\"dir\": \"records\",\"signingKey\": \"" + key + "\"},\r\n\t\"packs\" : {}\r\n}",
			[]string{"signingKey"}},
		{"an empty audit member", `{"configVersion":"5","requireReviewed":true,"audit":{},"packs":{}}`, "6", false, key,
			`{"configVersion":"6","requireReviewed":true,"audit":{"signingKey":"` + key + `"},"packs":{}}`,
			[]string{"configVersion", "signingKey"}},
		{"a path with a tab, a line separator and a <", `{"configVersion":"5","requireReviewed":true,"audit":{"dir":"a"},"packs":{}}`, "6", false, "/Top SECRET\tdir <x>/k.seed",
			`{"configVersion":"6","requireReviewed":true,"audit":{"dir":"a","signingKey":"/Top SECRET\tdir\u2028<x>/k.seed"},"packs":{}}`,
			[]string{"configVersion", "signingKey"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			members, err := configMembers([]byte(tc.before))
			if err != nil {
				t.Fatal(err)
			}
			got, changed := upgradedConfig([]byte(tc.before), members, tc.to, tc.facts, tc.key)
			if string(got) != tc.want {
				t.Errorf("upgraded to\n%s\nwant\n%s", got, tc.want)
			}
			if !reflect.DeepEqual(changed, tc.changed) {
				t.Errorf("changed %v, want %v", changed, tc.changed)
			}
			var parsed struct {
				Audit struct {
					SigningKey string `json:"signingKey"`
				} `json:"audit"`
			}
			if err := json.Unmarshal(got, &parsed); err != nil || parsed.Audit.SigningKey != tc.key {
				t.Errorf("the configuration names %q (%v), want %q", parsed.Audit.SigningKey, err, tc.key)
			}
		})
	}
}

// **The item is shown on the project Desk was started on, never chosen for
// the owner, and only where it can be kept.** Offered, it is not chosen: the
// offer is the one without it, at "5". Where the runtime reads no "6", where
// JPACK_SIGNING_KEY is inherited, where custody keeps no key, where something
// is kept under the project's name, or where the signing folder is inside the
// project, it says why in one sentence, and the rest of the offer is
// unchanged. Where jpack.json names a key it says so; where its chain is off,
// or its audit member is not an object, why not. A desk Desk made has no
// such item. No reading makes anything in Desk's custody.
func TestTheSigningItemIsShownOnlyWhereItCanBeKept(t *testing.T) {
	t.Run("offered, and not chosen", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		answer := u.offer(t, false)
		if answer.SigningKey == nil || *answer.SigningKey != (upgradeSigning{State: signingOffered}) || answer.Sign || answer.To != "5" || answer.ConfigAfter != upgradeAfter {
			t.Errorf("the offer answered %+v %+v", answer, answer.SigningKey)
		}
		if _, err := os.Lstat(filepath.Dir(u.seed)); !os.IsNotExist(err) {
			t.Errorf("an offer made the signing folder: %v", err)
		}
	})

	for _, tc := range []struct {
		name, versions string
		files          map[string]string
		prepare        func(t *testing.T, u *signingUpgrade)
		want           upgradeSigning
	}{
		{"a runtime that reads no 6", allConfigVersions, nil, nil,
			upgradeSigning{signingNotOffered, "This project is not offered a signing key: a project names its signing key at configVersion 6, and the runtime this Desk runs (jpack 0.0.0-stand-in) does not read it. A runtime of 0.26.0 or later does."}},
		{"an inherited JPACK_SIGNING_KEY", withAuditVersions, nil, func(t *testing.T, u *signingUpgrade) {
			t.Setenv("JPACK_SIGNING_KEY", filepath.Join(t.TempDir(), "owner.seed"))
		}, upgradeSigning{signingNotOffered, keyNotOfferedInherited}},
		{"a signing folder custody refuses", withAuditVersions, nil, func(t *testing.T, u *signingUpgrade) {
			if err := os.MkdirAll(filepath.Join(u.s.configDir, "secrets"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), filepath.Dir(u.seed)); err != nil {
				t.Fatal(err)
			}
		}, upgradeSigning{signingNotOffered, "This project is not offered a signing key, because Desk could not keep one for it: Desk's signing folder is a symbolic link; a key is not kept anywhere reached through one."}},
		{"something kept under the project's name", withAuditVersions, nil, func(t *testing.T, u *signingUpgrade) {
			if err := os.MkdirAll(filepath.Dir(u.seed), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(strings.TrimSuffix(u.seed, ".seed")+".creating", nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, upgradeSigning{signingNotOffered, "This project is not offered a signing key, because Desk could not keep one for it: Desk's signing folder already holds a key, or an unfinished one, under this project's name, and a key is never written over anything."}},
		{"a key jpack.json names already", withAuditVersions, map[string]string{"jpack.json": `{"configVersion":"6","requireReviewed":true,"audit":{"dir":"a","signingKey":"/elsewhere/k.seed"},"packs":{}}`}, nil,
			upgradeSigning{State: signingNamed}},
		{"a chain turned off", withAuditVersions, map[string]string{"jpack.json": `{"configVersion":"6","audit":{"dir":"a","chain":false},"packs":{}}`}, nil,
			upgradeSigning{signingNotOffered, keyNotOfferedUnchained}},
		{"an audit member that is not an object", withAuditVersions, map[string]string{"jpack.json": `{"configVersion":"5","audit":true,"packs":{}}`}, nil,
			upgradeSigning{signingNotOffered, keyNotOfferedAuditShape}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := newSigningUpgrade(t, tc.files)
			if tc.versions != withAuditVersions {
				// The same stand-in, reading what this case names.
				script := strings.Replace(readFile(t, u.rig.bin), withAuditVersions, tc.versions, 1)
				if err := os.WriteFile(u.rig.bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tc.prepare != nil {
				tc.prepare(t, u)
			}
			before := treeOf(t, u.project)
			signing := namesIn(t, filepath.Dir(u.seed))
			chosen, plain := u.offer(t, true), u.offer(t, false)
			if chosen.SigningKey == nil || *chosen.SigningKey != tc.want || chosen.Sign {
				t.Errorf("the offer answered %+v, want %+v", chosen.SigningKey, tc.want)
			}
			// Asking for the key changes nothing in the offer: the same
			// configuration, and the same token.
			if chosen.ConfigAfter != plain.ConfigAfter || chosen.Token != plain.Token || chosen.To != plain.To {
				t.Errorf("asked for the key, the offer is %q at %s, without it %q at %s", chosen.ConfigAfter, chosen.To, plain.ConfigAfter, plain.To)
			}
			sameProject(t, before, treeOf(t, u.project), "an item not offered")
			if got := namesIn(t, filepath.Dir(u.seed)); !slices.Equal(got, signing) {
				t.Errorf("the signing folder holds %q, want %q", got, signing)
			}
		})
	}

	t.Run("a signing folder inside the project", func(t *testing.T) {
		t.Setenv("JPACK_CONFIG", "")
		t.Setenv("JPACK_SIGNING_KEY", "")
		rig := newReviewRigReading(t, withAuditVersions, "", "")
		rig.answers(t, "error")
		project := t.TempDir()
		writeProject(t, project, map[string]string{"jpack.json": upgradeBefore, "packs/a.json": reviewPack, "packs/b.json": otherPack})
		config := filepath.Join(project, "desk-config")
		if err := os.Mkdir(config, 0o700); err != nil {
			t.Fatal(err)
		}
		s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, DeskConfigDir: config, Logger: log.New(io.Discard, "", 0)})
		t.Cleanup(func() { s.Close(); ts.Close() })
		status, data := reviewCall(t, ts, "GET", "/api/upgrade?signingKey=true", "", nil, bearer)
		var answer upgradeAnswer
		if status != http.StatusOK || json.Unmarshal(data, &answer) != nil {
			t.Fatalf("the offer answered %d %s", status, data)
		}
		want := "This project is not offered a signing key, because Desk could not keep one for it: Desk's signing folder is inside this project's folder, and the runtime refuses a key there."
		if answer.SigningKey == nil || answer.SigningKey.State != signingNotOffered || answer.SigningKey.Reason != want || answer.Sign {
			t.Errorf("the offer answered %+v", answer.SigningKey)
		}
	})

	t.Run("a desk Desk made", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		row := createSignedDesk(t, u.s, u.ts, u.rig.calls, "a4000000000000000000000000000001")
		writeProject(t, row.Folder, map[string]string{"jpack.json": `{"configVersion":"3","packs":{}}` + "\n"})
		status, data := reviewCall(t, u.ts, "GET", "/api/upgrade?signingKey=true", row.ID, nil, bearer)
		var answer upgradeAnswer
		if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.SigningKey != nil || answer.Sign || answer.To != "5" || strings.Contains(string(data), "signingKey\":\"/") {
			t.Errorf("a desk Desk made answered %d %s", status, data)
		}
	})
}

// **Chosen, the key is made first, then jpack.json at "6" names it, and the
// lock pins it.** The runtime writes the seed at the project's name in Desk's
// signing folder, before it locks; Desk keeps its public key; jpack.json is
// exactly the offer's, every other byte its own; the marker is gone; and the
// answer gives the public key. A gated project at "5" is offered the key
// alone, and keeps its own audit member.
func TestChoosingTheSigningKeyWritesSixAndTheKeyTogether(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	answer := u.offer(t, true)
	if answer.State != "offer" || !answer.Sign || answer.To != "6" || answer.ConfigAfter != u.signed() ||
		!reflect.DeepEqual(answer.Changes, []string{"configVersion", "requireReviewed", "requireComparableFacts", "audit", "signingKey"}) {
		t.Fatalf("the offer answered %+v", answer)
	}
	if without := u.offer(t, false); without.Token == answer.Token {
		t.Error("the offers with and without the key gave one token")
	}
	u.rig.locks(t, upgradeLock(t, u.project, u.signed(), bothPacks))
	os.Remove(u.rig.calls)
	status, data := u.confirm(t, answer.Token, true)
	var result struct {
		ConfigVersion string         `json:"configVersion"`
		SigningKey    *deskPublicKey `json:"signingKey"`
	}
	if status != http.StatusOK || json.Unmarshal(data, &result) != nil || result.ConfigVersion != "6" || result.SigningKey == nil || *result.SigningKey != key1 {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	if got := readFile(t, filepath.Join(u.project, "jpack.json")); got != u.signed() {
		t.Errorf("jpack.json is\n%s\nwant\n%s", got, u.signed())
	}
	calls := strings.Split(strings.TrimSpace(readFile(t, u.rig.calls)), "\n")
	generate := slices.Index(calls, "audit key generate "+u.seed+" --format json [JPACK_CONFIG=unset]")
	lock := slices.IndexFunc(calls, func(call string) bool { return strings.HasPrefix(call, "packs lock") })
	if generate < 0 || lock < 0 || generate > lock {
		t.Errorf("the runtime was run as %q, want the key made before the lock", calls)
	}
	name := digestOf([]byte(u.s.projectDir))
	if got := u.keyFiles(t); !slices.Equal(got, []string{name + ".keys.jsonl", name + ".seed"}) {
		t.Errorf("the signing folder holds %q", got)
	}
	if got := readFile(t, filepath.Join(filepath.Dir(u.seed), name+".keys.jsonl")); got != wantKeyLine(standInPublicKey, standInKeyID, 0) {
		t.Errorf("the list of public keys is %q", got)
	}
	if got := permOf(t, u.seed); got != 0o600 {
		t.Errorf("the seed is %v", got)
	}
	if again := u.offer(t, true); again.State != "unchanged" || again.SigningKey == nil || again.SigningKey.State != signingNamed {
		t.Errorf("after the upgrade the offer answered %+v %+v", again, again.SigningKey)
	}

	t.Run("a project at 6 is never written at a lower version", func(t *testing.T) {
		u := newSigningUpgrade(t, map[string]string{"jpack.json": `{"configVersion":"6","requireReviewed":true,"audit":{"dir":"a","chain":false},"packs":{}}` + "\n"})
		answer := u.offer(t, false)
		want := `{"configVersion":"6","requireReviewed":true,"requireComparableFacts":true,"audit":{"dir":"a","chain":false},"packs":{}}` + "\n"
		if answer.State != "offer" || answer.From != "6" || answer.To != "6" || answer.ConfigAfter != want || !reflect.DeepEqual(answer.Changes, []string{"requireComparableFacts"}) {
			t.Errorf("the offer answered %+v", answer)
		}
	})

	t.Run("a gated project at 5 is offered the key alone", func(t *testing.T) {
		gated := "{\n  \"configVersion\": \"5\",\n  \"requireReviewed\": true,\n  \"requireComparableFacts\": true,\n  \"audit\": {\"dir\": \"records\"},\n  \"packs\": {}\n}\n"
		u := newSigningUpgrade(t, map[string]string{"jpack.json": gated})
		if plain := u.offer(t, false); plain.State != "unchanged" || plain.SigningKey == nil || plain.SigningKey.State != signingOffered {
			t.Fatalf("without the key the offer answered %+v", plain)
		}
		answer := u.offer(t, true)
		want := strings.Replace(strings.Replace(gated, `"5"`, `"6"`, 1), `{"dir": "records"}`, `{"dir": "records","signingKey": "`+u.seed+`"}`, 1)
		if answer.State != "offer" || answer.ConfigAfter != want || !reflect.DeepEqual(answer.Changes, []string{"configVersion", "signingKey"}) || answer.Audit.State != "kept" {
			t.Fatalf("the offer answered %+v", answer)
		}
		u.rig.locks(t, upgradeLock(t, u.project, want, nil))
		if status, data := u.confirm(t, answer.Token, true); status != http.StatusOK {
			t.Fatalf("the confirmation answered %d %s", status, data)
		}
		if got := readFile(t, filepath.Join(u.project, "jpack.json")); got != want {
			t.Errorf("jpack.json is %q", got)
		}
	})
}

// **A confirmation writes the offer it names, and only that.** The token of
// the offer with the key does not confirm a request without it, nor the
// other way round: each is stale, and neither writes a file or makes a key,
// though the runtime would lock the configuration the key's offer showed.
func TestTheSigningChoiceIsBoundToTheOfferShown(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	before := treeOf(t, u.project)
	with, without := u.offer(t, true), u.offer(t, false)
	u.rig.locks(t, upgradeLock(t, u.project, u.signed(), bothPacks))
	for _, tc := range []struct {
		token string
		sign  bool
	}{{with.Token, false}, {without.Token, true}} {
		status, data := u.confirm(t, tc.token, tc.sign)
		if status != http.StatusConflict || refusalOf(data) != "The project changed after you reviewed the upgrade, so nothing was written. Review it again." {
			t.Errorf("a confirmation with the key %v answered %d %s", tc.sign, status, data)
		}
	}
	sameProject(t, before, treeOf(t, u.project), "a confirmation of another offer")
	if got := u.keyFiles(t); len(got) != 0 {
		t.Errorf("the signing folder holds %q", got)
	}
}

// **A failure at any step leaves no key and no change.** The marker that
// cannot be written, a key the runtime does not make, a list of public keys
// that cannot be linked into place, a signing folder replaced before
// jpack.json names the key, a jpack.json that changed before it was written,
// a lock the runtime refuses, and a lock that does not pin what was shown:
// each puts every file back, removes what the key's creation made, and
// releases the signing folder's lock, so the next upgrade is made.
func TestAFailedSigningUpgradeLeavesNoKeyAndNoChange(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, u *signingUpgrade)
		status  int
		says    string
	}{
		{"the marker", func(t *testing.T, u *signingUpgrade) {
			// The marker's name taken by a folder once the confirmation holds
			// the signing folder: the marker is never written over anything.
			testHookAfterCustodyCheck = func(path string) {
				if path == filepath.Dir(u.seed) {
					if err := os.Mkdir(strings.TrimSuffix(u.seed, ".seed")+".creating", 0o700); err != nil {
						t.Error(err)
					}
					testHookAfterCustodyCheck = nil
				}
			}
		}, http.StatusInternalServerError, "Nothing was written: something is already kept as " + "%NAME%.creating, and a key is never written over anything."},
		{"generate", func(t *testing.T, u *signingUpgrade) {
			generatingAs(t, u.rig.calls, "  printf '%s\\n' "+shellQuote(`{"outputVersion":"2","command":"audit key generate","status":"error","diagnostics":[{"code":"JPS-AUDIT-KEY-REFUSED","message":"No seed was written."}]}`)+"\n  exit 1")
		}, http.StatusInternalServerError, "Nothing was written: the runtime did not generate its signing key: No seed was written."},
		{"the list", func(t *testing.T, u *signingUpgrade) {
			testHookKeyBetween = func(at string) {
				if at == "after generate" {
					os.Mkdir(strings.TrimSuffix(u.seed, ".seed")+".keys.jsonl", 0o700)
				}
			}
		}, http.StatusInternalServerError, "the list of its public keys could not be written"},
		{"a signing folder replaced before jpack.json names the key", func(t *testing.T, u *signingUpgrade) {
			testHookKeyBetween = func(at string) {
				if at == "upgrade: before naming" {
					swapSigningFolder(t, filepath.Dir(u.seed))
				}
			}
		}, http.StatusConflict, "Desk's signing folder was replaced before jpack.json named the key, so every file was put back as it was."},
		// Review round 1 of #261: the folder replaced after the seed's
		// pathname was checked and before jpack.json is published, while its
		// bytes are staged; and after it is published and locked, before the
		// marker goes.
		{"a signing folder replaced while jpack.json is staged", func(t *testing.T, u *signingUpgrade) {
			testHookBeforeUpgradePublish = func(target, staged string) {
				if target == runtimeConfigName {
					swapSigningFolder(t, filepath.Dir(u.seed))
				}
			}
		}, http.StatusConflict, "Desk's signing folder was replaced before jpack.json named the key, so every file was put back as it was."},
		{"a signing folder replaced while the project is locked", func(t *testing.T, u *signingUpgrade) {
			testHookKeyBetween = func(at string) {
				if at == "upgrade: named" {
					swapSigningFolder(t, filepath.Dir(u.seed))
				}
			}
		}, http.StatusConflict, "Desk's signing folder was replaced while the project was being locked, so every file was put back as it was."},
		{"jpack.json changed before it was written", func(t *testing.T, u *signingUpgrade) {
			testHookBeforeUpgradePublish = func(target, staged string) {
				if target == runtimeConfigName {
					if err := os.WriteFile(filepath.Join(u.project, "jpack.json"), []byte(upgradeBefore+" "), 0); err != nil {
						t.Error(err)
					}
				}
			}
		}, http.StatusConflict, "The project changed after you reviewed the upgrade, so every file was put back as it was."},
		{"a lock the runtime refuses", func(t *testing.T, u *signingUpgrade) {
			os.Remove(u.rig.lock)
			script := strings.Replace(readFile(t, u.rig.bin), "'packs lock')\n", "'packs lock')\n  exit 1\n", 1)
			if err := os.WriteFile(u.rig.bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
		}, http.StatusInternalServerError, "The runtime did not lock the project"},
		{"a lock that pins something else", func(t *testing.T, u *signingUpgrade) {
			u.rig.locks(t, upgradeLock(t, u.project, upgradeAfter, bothPacks))
		}, http.StatusConflict, "A file changed while the project was being locked, so every file was put back as it was."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := newSigningUpgrade(t, nil)
			answer := u.offer(t, true)
			if answer.State != "offer" || !answer.Sign {
				t.Fatalf("the offer answered %+v", answer)
			}
			u.rig.locks(t, upgradeLock(t, u.project, u.signed(), bothPacks))
			before := treeOf(t, u.project)
			tc.prepare(t, u)
			t.Cleanup(func() { testHookKeyBetween, testHookBeforeUpgradePublish, testHookAfterCustodyCheck = nil, nil, nil })
			status, data := u.confirm(t, answer.Token, true)
			testHookKeyBetween, testHookBeforeUpgradePublish, testHookAfterCustodyCheck = nil, nil, nil
			says := strings.ReplaceAll(tc.says, "%NAME%", digestOf([]byte(u.s.projectDir)))
			if status != tc.status || !strings.Contains(refusalOf(data), says) {
				t.Errorf("the confirmation answered %d %s, want %d saying %q", status, data, tc.status, says)
			}
			if strings.Contains(string(data), u.s.configDir) || strings.Contains(string(data), u.project) {
				t.Errorf("the refusal names a path: %s", data)
			}
			if tc.name == "jpack.json changed before it was written" {
				// The edit made meanwhile is kept, and nothing else changed.
				edited := before["jpack.json"]
				edited.data = upgradeBefore + " "
				before["jpack.json"] = edited
			}
			sameProject(t, before, treeOf(t, u.project), "a failed upgrade")
			for _, folder := range []string{filepath.Dir(u.seed), filepath.Dir(u.seed) + ".held"} {
				for _, name := range namesIn(t, folder) {
					if info, err := os.Lstat(filepath.Join(folder, name)); err == nil && info.Mode().IsRegular() && strings.HasPrefix(name, digestOf([]byte(u.s.projectDir))) {
						t.Errorf("%s was left in %s", name, filepath.Base(folder))
					}
				}
			}
			// The lock was let go: another Desk process could take it now.
			dir, err := u.s.assistant.openSigning(false)
			if err == nil {
				unlock, err := lockSigning(dir)
				if err != nil {
					t.Errorf("the signing folder's lock is still held: %v", err)
				} else {
					unlock()
				}
				dir.Close()
			}
		})
	}
}

// **The key is made under the signing folder's lock.** While another Desk
// process holds it, the confirmation waits its bound, then writes nothing,
// makes no key, and says so in plain words.
func TestTheSigningUpgradeWaitsForTheSigningFolder(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	answer := u.offer(t, true)
	before := treeOf(t, u.project)
	dir, err := u.s.assistant.openSigning(true)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	other, err := dir.root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := lockPrivateData(other, true); err != nil {
		t.Fatalf("the test could not hold the lock: %v", err)
	}
	was := signingLockWait
	signingLockWait = 50 * 1e6
	t.Cleanup(func() { signingLockWait = was })
	status, data := u.confirm(t, answer.Token, true)
	if status != http.StatusConflict || refusalOf(data) != "Nothing was written: "+signingBusyWords {
		t.Errorf("the confirmation answered %d %s", status, data)
	}
	sameProject(t, before, treeOf(t, u.project), "an upgrade that could not take the lock")
	if got := u.keyFiles(t); len(got) != 0 {
		t.Errorf("the signing folder holds %q", got)
	}
}

// **A stopped upgrade leaves no key past the next start that jpack.json does
// not name.** Stopped before the runtime ran, after it, or before jpack.json
// named the key, the next start removes the key, its list and its marker;
// stopped once jpack.json names the key and it is locked, only the marker. A
// jpack.json that cannot be read now leaves everything for a later start, and
// a marker of another project's name is left for a start on that project.
func TestAStoppedSigningUpgradeIsSweptAtTheNextStart(t *testing.T) {
	name := func(u *signingUpgrade) string { return digestOf([]byte(u.s.projectDir)) }
	for _, tc := range []struct {
		at   string
		left func(n string) []string
		kept func(n string) []string
	}{
		{"before generate", func(n string) []string { return []string{n + ".creating"} }, func(string) []string { return nil }},
		{"after generate", func(n string) []string { return []string{n + ".creating", n + ".seed"} }, func(string) []string { return nil }},
		{"upgrade: before naming", func(n string) []string { return []string{n + ".creating", n + ".keys.jsonl", n + ".seed"} }, func(string) []string { return nil }},
		{"upgrade: named", func(n string) []string { return []string{n + ".creating", n + ".keys.jsonl", n + ".seed"} },
			func(n string) []string { return []string{n + ".keys.jsonl", n + ".seed"} }},
	} {
		t.Run(tc.at, func(t *testing.T) {
			u := newSigningUpgrade(t, nil)
			answer := u.offer(t, true)
			u.rig.locks(t, upgradeLock(t, u.project, u.signed(), bothPacks))
			testHookKeyBetween = func(at string) {
				if at == tc.at {
					panic(http.ErrAbortHandler)
				}
			}
			t.Cleanup(func() { testHookKeyBetween = nil })
			u.abandon(t, "/api/upgrade", map[string]any{"token": answer.Token, "requireComparableFacts": true, "signingKey": true})
			testHookKeyBetween = nil
			if got := u.keyFiles(t); !slices.Equal(got, tc.left(name(u))) {
				t.Fatalf("the stopped upgrade left %q, want %q", got, tc.left(name(u)))
			}
			u.ts.Close()
			restartedServer(t, u.s)
			if got := u.keyFiles(t); !slices.Equal(got, tc.kept(name(u))) {
				t.Errorf("after the next start %q, want %q", got, tc.kept(name(u)))
			}
		})
	}

	t.Run("a jpack.json that cannot be read now", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		answer := u.offer(t, true)
		testHookKeyBetween = func(at string) {
			if at == "upgrade: before naming" {
				panic(http.ErrAbortHandler)
			}
		}
		t.Cleanup(func() { testHookKeyBetween = nil })
		u.abandon(t, "/api/upgrade", map[string]any{"token": answer.Token, "requireComparableFacts": true, "signingKey": true})
		testHookKeyBetween = nil
		left := u.keyFiles(t)
		config := filepath.Join(u.project, "jpack.json")
		if err := os.Rename(config, config+".aside"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("jpack.json.aside", config); err != nil {
			t.Fatal(err)
		}
		u.ts.Close()
		again, logged := restartedServer(t, u.s)
		if got := u.keyFiles(t); !slices.Equal(got, left) || len(left) != 3 {
			t.Errorf("a start that could not read jpack.json left %q, want %q", got, left)
		}
		if !strings.Contains(logged.String(), "could not be told") {
			t.Errorf("the start did not say why it left the key: %s", logged)
		}
		if err := os.Remove(config); err != nil || os.Rename(config+".aside", config) != nil {
			t.Fatal("could not put jpack.json back")
		}
		restartedServer(t, again)
		if got := u.keyFiles(t); len(got) != 0 {
			t.Errorf("the start after it left %q", got)
		}
	})

	t.Run("no jpack.json at the next start", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		answer := u.offer(t, true)
		testHookKeyBetween = func(at string) {
			if at == "upgrade: before naming" {
				panic(http.ErrAbortHandler)
			}
		}
		t.Cleanup(func() { testHookKeyBetween = nil })
		u.abandon(t, "/api/upgrade", map[string]any{"token": answer.Token, "requireComparableFacts": true, "signingKey": true})
		testHookKeyBetween = nil
		if err := os.Remove(filepath.Join(u.project, "jpack.json")); err != nil {
			t.Fatal(err)
		}
		u.ts.Close()
		restartedServer(t, u.s)
		if got := u.keyFiles(t); len(got) != 0 {
			t.Errorf("a start with no jpack.json left %q", got)
		}
	})

	t.Run("another project's marker", func(t *testing.T) {
		u := newSigningUpgrade(t, nil)
		other := digestOf([]byte("/another/project"))
		folder := filepath.Dir(u.seed)
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{other + ".creating", other + ".seed"} {
			if err := os.WriteFile(filepath.Join(folder, file), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		u.ts.Close()
		restartedServer(t, u.s)
		if got := namesIn(t, folder); !slices.Equal(got, []string{other + ".creating", other + ".seed"}) {
			t.Errorf("a start on another project left %q", got)
		}
	})
}

// **After the upgrade the decision record reads the project as a desk with a
// key.** Before it, Desk keeps no key for the project it was started on, and
// says so; after it, the panel passes the key Desk made, shows its public
// key, and offers its rotation, with a token; and the rotation, confirmed,
// hands signing over under the project's own name.
func TestTheDecisionRecordReadsTheStartupKeyAfterTheUpgrade(t *testing.T) {
	// A project that keeps a trail, and no gates.
	u := newSigningUpgrade(t, map[string]string{"jpack.json": strings.Replace(upgradeBefore, `"configVersion": "3",`, `"configVersion": "5",
  "audit": {"dir": ".desk-private/audit"},`, 1)})
	status, before, refusal := readAudit(t, u.ts, "")
	if status != http.StatusOK || before.Keys == nil || before.Keys.State != keysStartup || before.Rotation == nil ||
		before.Rotation.Reason != "Desk keeps no signing key for the project it was started on, so it has none to rotate." {
		t.Fatalf("before the upgrade the panel answered %d %+v %+v %q", status, before.Keys, before.Rotation, refusal)
	}
	u.audit.keysSeen(t)
	answer := u.offer(t, true)
	if !strings.Contains(answer.ConfigAfter, `"audit": {"dir": ".desk-private/audit","signingKey": "`+u.seed+`"}`) {
		t.Fatalf("the offer answered %+v", answer)
	}
	u.rig.locks(t, upgradeLock(t, u.project, answer.ConfigAfter, bothPacks))
	if status, data := u.confirm(t, answer.Token, true); status != http.StatusOK {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	if err := os.MkdirAll(filepath.Join(u.project, ".desk-private", "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(u.project, ".desk-private", "audit", "evaluations.jsonl"), []byte("{\"line\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(u.project, ".desk-private", "audit", "signatures.jsonl"), []byte(recordLine(standInKeyID, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	status, after, refusal := readAudit(t, u.ts, "")
	if status != http.StatusOK || after.Keys == nil || after.Keys.State != keysKept || !slices.Equal(after.Keys.Public, []deskPublicKey{key1}) {
		t.Fatalf("after the upgrade the panel answered %d %+v %q", status, after.Keys, refusal)
	}
	if seen := u.audit.keysSeen(t); !slices.Equal(seen, []string{standInPublicKey}) {
		t.Errorf("audit verify was given %q", seen)
	}
	if after.Rotation == nil || after.Rotation.State != rotationAvailable || len(after.Rotation.Token) != 64 {
		t.Fatalf("after the upgrade the panel offers %+v", after.Rotation)
	}
	generatingAs(t, u.rig.calls, generateBySeed(u.rig.calls))
	readingKeyAs(t, u.rig.calls, publicBySeed)
	rotatingAs(t, u.rig.calls, rotateAsTheRuntime(u.rig.calls))
	status, data := reviewCall(t, u.ts, "POST", "/api/audit/key/rotate", "", map[string]string{"token": after.Rotation.Token}, bearer)
	var rotated rotationAnswer
	if status != http.StatusOK || json.Unmarshal(data, &rotated) != nil || rotated.At != 1 || rotated.Next.PublicKey != secondPublicKey {
		t.Fatalf("the rotation answered %d %s", status, data)
	}
	list := readFile(t, strings.TrimSuffix(u.seed, ".seed")+".keys.jsonl")
	if list != wantKeyLine(standInPublicKey, standInKeyID, 0)+wantKeyLine(secondPublicKey, secondKeyID, 1) || seedIs(u.seed) != "2" {
		t.Errorf("after the rotation the list is %q and the seed %s", list, seedIs(u.seed))
	}
}

// **No key action where the runtime would act on another key.** Where the
// project keeps a key and JPACK_SIGNING_KEY is inherited, the panel offers no
// rotation, and a confirmation rotates nothing.
func TestAnInheritedKeyHoldsTheStartupKeyStill(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	answer := u.offer(t, true)
	u.rig.locks(t, upgradeLock(t, u.project, u.signed(), bothPacks))
	if status, data := u.confirm(t, answer.Token, true); status != http.StatusOK {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	t.Setenv("JPACK_SIGNING_KEY", filepath.Join(t.TempDir(), "owner.seed"))
	_, panel, _ := readAudit(t, u.ts, "")
	want := "JPACK_SIGNING_KEY is set where Desk was started, and the runtime signs this project's records with the key it names, not with the key Desk keeps, so Desk rotates no key here."
	if panel.Rotation == nil || panel.Rotation.State != rotationUnavailable || panel.Rotation.Reason != want {
		t.Errorf("the panel offers %+v", panel.Rotation)
	}
	before := u.keyFiles(t)
	status, data := reviewCall(t, u.ts, "POST", "/api/audit/key/rotate", "", map[string]string{"token": strings.Repeat("a", 64)}, bearer)
	if status != http.StatusConflict || refusalOf(data) != "Nothing was rotated. "+want {
		t.Errorf("the rotation answered %d %s", status, data)
	}
	if got := u.keyFiles(t); !slices.Equal(got, before) {
		t.Errorf("the signing folder holds %q, want %q", got, before)
	}
}

// **The start finishes a rotation of the project's key that a stop cut
// short**, as it does a desk's: the runtime wrote the line, so the next key
// is named and the marker removed.
func TestTheStartFinishesARotationOfTheStartupKey(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	answer := u.offer(t, true)
	u.rig.locks(t, upgradeLock(t, u.project, u.signed(), bothPacks))
	if status, data := u.confirm(t, answer.Token, true); status != http.StatusOK {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	audit := filepath.Join(u.project, ".desk-private", "audit")
	if err := os.WriteFile(filepath.Join(audit, "evaluations.jsonl"), []byte("{\"line\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(audit, "signatures.jsonl"), []byte(recordLine(standInKeyID, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	generatingAs(t, u.rig.calls, generateBySeed(u.rig.calls))
	readingKeyAs(t, u.rig.calls, publicBySeed)
	rotatingAs(t, u.rig.calls, rotateAsTheRuntime(u.rig.calls))
	_, panel, _ := readAudit(t, u.ts, "")
	testHookKeyBetween = func(at string) {
		if at == "rotation: line written" {
			panic(http.ErrAbortHandler)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	u.abandon(t, "/api/audit/key/rotate", map[string]any{"token": panel.Rotation.Token})
	testHookKeyBetween = nil
	n := digestOf([]byte(u.s.projectDir))
	if got := u.keyFiles(t); !slices.Contains(got, n+".rotating") {
		t.Fatalf("the stopped rotation left %q", got)
	}
	u.ts.Close()
	_, logged := restartedServer(t, u.s)
	if got := u.keyFiles(t); !slices.Equal(got, []string{n + ".keys.jsonl", n + ".seed"}) || seedIs(u.seed) != "2" {
		t.Errorf("after the next start %q, seed %s: %s", got, seedIs(u.seed), logged)
	}
}

// **No path reaches the page.** The project's folder and Desk's
// configuration folder have a space, a tab and a line separator in their
// names: neither the item's reasons, a refusal to make the key in the
// runtime's words, nor the decision record afterwards names any part of
// either.
func TestNoPathOfTheStartupKeyReachesThePage(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	root := t.TempDir()
	project := filepath.Join(root, "Top SECRET\tproject TAIL")
	config := filepath.Join(root, "Owner SECRET\tKEYS TAIL dir")
	if os.Mkdir(project, 0o700) != nil || os.Mkdir(config, 0o700) != nil {
		t.Skip("this file system refuses the names")
	}
	writeProject(t, project, map[string]string{"jpack.json": upgradeBefore, "packs/a.json": reviewPack, "packs/b.json": otherPack})
	rig := newReviewRigReading(t, withAuditVersions, "", "")
	rig.answers(t, "error")
	s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: rig.bin, Token: testToken, DeskConfigDir: config, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { s.Close(); ts.Close() })
	seed := filepath.Join(config, "secrets", "signing", digestOf([]byte(s.projectDir))+".seed")
	leaks := func(t *testing.T, what, said string) {
		t.Helper()
		for _, part := range []string{"SECRET", "TAIL", "KEYS", root, runtimePrints(config), runtimePrints(project)} {
			if strings.Contains(said, part) {
				t.Errorf("%s names %q: %s", what, part, said)
			}
		}
	}
	status, data := reviewCall(t, ts, "GET", "/api/upgrade?signingKey=true", "", nil, bearer)
	var answer upgradeAnswer
	if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || !answer.Sign {
		t.Fatalf("the offer answered %d %s", status, data)
	}
	// A refusal in the runtime's words, naming the seed and its folders as
	// the runtime prints them.
	generatingAs(t, rig.calls, "  printf '%s\\n' "+shellQuote(`{"outputVersion":"2","command":"audit key generate","status":"error","diagnostics":[{"code":"JPS-AUDIT-KEY-REFUSED","message":`+
		jsonString("No seed was written to "+runtimePrints(seed)+", where a signing key is refused: the directory "+runtimePrints(config)+" is not yours; the project is "+runtimePrints(project)+".")+`}]}`)+"\n  exit 1")
	status, data = reviewCall(t, ts, "POST", "/api/upgrade", "", map[string]any{"token": answer.Token, "requireComparableFacts": true, "signingKey": true}, bearer)
	if status != http.StatusInternalServerError || !strings.HasPrefix(refusalOf(data), "Nothing was written: the runtime did not generate its signing key: No seed was written to …, where a signing key is refused") {
		t.Errorf("the refusal is %d %s", status, data)
	}
	leaks(t, "the refusal", string(data))

	// A custody refusal, in Desk's words.
	signing := filepath.Join(config, "secrets", "signing")
	if err := os.RemoveAll(signing); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), signing); err != nil {
		t.Fatal(err)
	}
	status, data = reviewCall(t, ts, "GET", "/api/upgrade?signingKey=true", "", nil, bearer)
	if status != http.StatusOK || !strings.Contains(string(data), "Desk's signing folder is a symbolic link") {
		t.Errorf("the offer answered %d %s", status, data)
	}
	leaks(t, "the item's reason", string(data))
}

// **With the runtime: the project Desk was started on, upgraded with its key,
// signs.** The project's folder and Desk's configuration folder each have a
// space, a tab and a line separator in their names. Where the runtime reads
// "6", the offer names the key; the confirmation has the runtime make it,
// writes jpack.json at "6" naming it, and locks; `packs verify` agrees; a
// deciding run, as an outside caller makes one, is recorded; and the decision
// record, with the project's public key, reports that record signed by it,
// and `packs validate` that the key signs, with no part of either path in
// what the page is told. A key the runtime refuses to make, for a folder made
// open after custody's check, writes nothing, leaves no key, and is told with
// no part of any path.
func TestSigningTheStartupProjectWithTheRuntime(t *testing.T) {
	bin := requireBinary(t)
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	start := func(t *testing.T) (string, string, string, *Server, *httptest.Server) {
		t.Helper()
		root := t.TempDir()
		project := filepath.Join(root, "Top SECRET\tproject TAIL")
		config := filepath.Join(root, "Owner SECRET\tKEYS TAIL dir")
		if os.Mkdir(project, 0o700) != nil || os.Mkdir(config, 0o700) != nil {
			t.Skip("this file system refuses the names")
		}
		writeProject(t, project, map[string]string{"jpack.json": `{"configVersion":"3","packs":{"alpha":{"path":"packs/a.json"}}}` + "\n", "packs/a.json": reviewPack})
		s, ts := startDesk(t, Config{ProjectDir: project, JpackBin: bin, Token: testToken, DeskConfigDir: config, Logger: log.New(io.Discard, "", 0)})
		t.Cleanup(func() { s.Close(); ts.Close() })
		return root, project, config, s, ts
	}
	leaks := func(t *testing.T, root, what, said string) {
		t.Helper()
		for _, part := range []string{"SECRET", "TAIL", "KEYS", root, "/tmp", "/home"} {
			if strings.Contains(said, part) {
				t.Errorf("%s names %q: %s", what, part, said)
			}
		}
	}
	offer := func(t *testing.T, ts *httptest.Server) upgradeAnswer {
		t.Helper()
		status, data := reviewCall(t, ts, "GET", "/api/upgrade?signingKey=true", "", nil, bearer)
		var answer upgradeAnswer
		if status != http.StatusOK || json.Unmarshal(data, &answer) != nil {
			t.Fatalf("the offer answered %d %s", status, data)
		}
		return answer
	}

	root, project, config, s, ts := start(t)
	answer := offer(t, ts)
	if answer.SigningKey == nil || answer.SigningKey.State != signingOffered {
		var schema struct {
			Supported []string `json:"supportedConfigVersions"`
		}
		if json.Unmarshal(jpackIn(t, bin, project, "packs", "schema", "--format", "json"), &schema) == nil && !slices.Contains(schema.Supported, "6") {
			t.Skip("this runtime reads no configVersion 6")
		}
		t.Fatalf("the offer answered %+v", answer.SigningKey)
	}
	seed := filepath.Join(config, "secrets", "signing", digestOf([]byte(s.projectDir))+".seed")
	want := `{"configVersion":"6","requireReviewed":true,"requireComparableFacts":true,"audit":{"dir":".desk-private/audit","signingKey":` + jsonString(seed) + `},"packs":{"alpha":{"path":"packs/a.json"}}}` + "\n"
	if !answer.Sign || answer.To != "6" || answer.ConfigAfter != want {
		t.Fatalf("the offer answered %+v", answer)
	}
	status, data := reviewCall(t, ts, "POST", "/api/upgrade", "", map[string]any{"token": answer.Token, "requireComparableFacts": true, "signingKey": true}, bearer)
	var result struct {
		ConfigVersion string         `json:"configVersion"`
		SigningKey    *deskPublicKey `json:"signingKey"`
	}
	if status != http.StatusOK || json.Unmarshal(data, &result) != nil || result.ConfigVersion != "6" || result.SigningKey == nil {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	leaks(t, root, "the confirmation", string(data))
	if got := readFile(t, filepath.Join(project, "jpack.json")); got != want {
		t.Errorf("jpack.json is %q, want %q", got, want)
	}
	var public struct {
		PublicKey string `json:"publicKey"`
		KeyID     string `json:"keyId"`
	}
	if err := json.Unmarshal(jpackIn(t, bin, project, "audit", "key", "public", seed, "--format", "json"), &public); err != nil {
		t.Fatal(err)
	}
	if result.SigningKey.PublicKey != public.PublicKey || result.SigningKey.KeyID != public.KeyID {
		t.Errorf("the confirmation answered key %+v, the runtime reads %+v from the seed", result.SigningKey, public)
	}
	jpackIn(t, bin, project, "packs", "verify", "--config", "jpack.json", "--format", "json")

	// A deciding run, as an outside caller makes one.
	facts := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(facts, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	jpackIn(t, bin, project, "experimental", "evaluate", "--config", "jpack.json", "--pack-id", "alpha", "--facts", facts, "--format", "json")

	status, data = reviewCall(t, ts, "GET", "/api/audit/verify", "", nil, bearer)
	var record auditAnswer
	if status != http.StatusOK || json.Unmarshal(data, &record) != nil || record.Report == nil {
		t.Fatalf("the panel answered %d %s", status, data)
	}
	report := record.Report
	if report.Status != "valid" || report.Coverage.Signed != (auditCoverageState{Status: "through", Through: 1}) || report.Coverage.SignedRecords != 1 ||
		report.Signatures == nil || report.Signatures.KeyInForce != public.KeyID || report.Signatures.KeysSupplied != 1 {
		t.Errorf("the panel reports %+v, signatures %+v; want the record signed by key %s", report, report.Signatures, public.KeyID)
	}
	if record.Keys == nil || record.Keys.State != keysKept || !slices.Equal(record.Keys.Public, []deskPublicKey{{public.PublicKey, public.KeyID, 0}}) {
		t.Errorf("the panel shows the keys %+v", record.Keys)
	}
	if record.Signing == nil || record.Signing.Status != "passed" {
		t.Errorf("the panel shows the key check %+v", record.Signing)
	}
	if record.Rotation == nil || record.Rotation.State != rotationAvailable {
		t.Errorf("the panel offers %+v", record.Rotation)
	}
	leaks(t, root, "the panel", string(data))

	t.Run("a key the runtime refuses to make", func(t *testing.T) {
		if !runtimeChecksKeyFolders(t, bin) {
			t.Skip("this runtime does not check the folders on a key's path")
		}
		root, project, config, _, ts := start(t)
		answer := offer(t, ts)
		before := treeOf(t, project)
		signing := filepath.Join(config, "secrets", "signing")
		testHookAfterCustodyCheck = func(path string) {
			if path == signing {
				os.Chmod(signing, 0o777)
			}
		}
		t.Cleanup(func() { testHookAfterCustodyCheck = nil; os.Chmod(signing, 0o700) })
		status, data := reviewCall(t, ts, "POST", "/api/upgrade", "", map[string]any{"token": answer.Token, "requireComparableFacts": true, "signingKey": true}, bearer)
		testHookAfterCustodyCheck = nil
		if status != http.StatusInternalServerError || !strings.HasPrefix(refusalOf(data), "Nothing was written: the runtime did not generate its signing key: No seed was written to …, where a signing key is refused: the directory … on the signing key's path can be written by its group or by other users (mode 0777)") {
			t.Errorf("the refusal is %d %s", status, data)
		}
		leaks(t, root, "the refusal", string(data))
		sameProject(t, before, treeOf(t, project), "a key the runtime refused")
		if names := namesIn(t, signing); len(names) != 0 {
			t.Errorf("a refused generation left %q", names)
		}
	})
}

// **An upgrade that could not put the project back leaves the key for the
// next start.** jpack.json may name it: the key and its marker stay, the
// refusal says so, and the next start keeps the key once jpack.json, put back
// by hand as the upgrade wrote it, names it, and removes only the marker.
func TestAnUpgradeNotPutBackLeavesTheKeyForTheNextStart(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	answer := u.offer(t, true)
	// The lock fails after jpack.json was replaced by a link, which the
	// upgrade will not write through to put it back.
	script := strings.Replace(readFile(t, u.rig.bin), "'packs lock')\n", "'packs lock')\n  mv jpack.json jpack.json.moved; ln -s jpack.json.moved jpack.json; exit 1\n", 1)
	if err := os.WriteFile(u.rig.bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	status, data := u.confirm(t, answer.Token, true)
	if status != http.StatusInternalServerError || !strings.Contains(refusalOf(data), "the project could not be put back as it was") ||
		!strings.Contains(refusalOf(data), "The signing key Desk made for this project was left with its creation marker: when Desk next starts, it removes the key unless jpack.json names it.") {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	n := digestOf([]byte(u.s.projectDir))
	if got := u.keyFiles(t); !slices.Equal(got, []string{n + ".creating", n + ".keys.jsonl", n + ".seed"}) {
		t.Fatalf("the signing folder holds %q", got)
	}
	config := filepath.Join(u.project, "jpack.json")
	if readFile(t, config+".moved") != u.signed() {
		t.Fatalf("the upgrade wrote %q", readFile(t, config+".moved"))
	}
	if err := os.Remove(config); err != nil || os.Rename(config+".moved", config) != nil {
		t.Fatal("could not put jpack.json back")
	}
	u.ts.Close()
	restartedServer(t, u.s)
	if got := u.keyFiles(t); !slices.Equal(got, []string{n + ".keys.jsonl", n + ".seed"}) {
		t.Errorf("after the next start %q", got)
	}
}

// **Bounds on both ends: the upgrade writes no jpack.json larger than Desk
// reads back.** The review, the decision record and a start's sweep read the
// configuration within reviewTextLimit; an upgrade whose jpack.json would be
// one byte past it is not offered, and one exactly at it is.
func TestTheUpgradeWritesNoFileLargerThanDeskReads(t *testing.T) {
	u := newSigningUpgrade(t, nil)
	config := func(n int) string {
		return `{"configVersion":"3","packs":{},"x":"` + strings.Repeat("a", n) + `"}` + "\n"
	}
	size := func(n int) int {
		members, err := configMembers([]byte(config(n)))
		if err != nil {
			t.Fatal(err)
		}
		upgraded, _ := upgradedConfig([]byte(config(n)), members, "6", true, u.seed)
		return len(upgraded)
	}
	n := reviewTextLimit - size(0)
	writeProject(t, u.project, map[string]string{"jpack.json": config(n)})
	if answer := u.offer(t, true); answer.State != "offer" || len(answer.ConfigAfter) != reviewTextLimit || answer.Token == "" {
		t.Errorf("at the bound the offer answered %s, %d bytes", answer.State, len(answer.ConfigAfter))
	}
	writeProject(t, u.project, map[string]string{"jpack.json": config(n + 1)})
	answer := u.offer(t, true)
	if want := "jpack.json as the upgrade would write it is larger than the 1048576 bytes Desk reads, so Desk does not upgrade it."; answer.State != "unavailable" || answer.Reason != want || answer.Token != "" {
		t.Errorf("past the bound the offer answered %s %q", answer.State, answer.Reason)
	}
}

// signedThenRotatable is a project upgraded with its key, with a trail of one
// record its key signed, and the stand-in steered to rotate as the runtime
// does; and the panel's token for a rotation.
func signedThenRotatable(t *testing.T) (*signingUpgrade, string) {
	t.Helper()
	u := newSigningUpgrade(t, nil)
	answer := u.offer(t, true)
	u.rig.locks(t, upgradeLock(t, u.project, u.signed(), bothPacks))
	if status, data := u.confirm(t, answer.Token, true); status != http.StatusOK {
		t.Fatalf("the confirmation answered %d %s", status, data)
	}
	audit := filepath.Join(u.project, ".desk-private", "audit")
	if err := os.WriteFile(filepath.Join(audit, "evaluations.jsonl"), []byte("{\"line\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(audit, "signatures.jsonl"), []byte(recordLine(standInKeyID, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	generatingAs(t, u.rig.calls, generateBySeed(u.rig.calls))
	readingKeyAs(t, u.rig.calls, publicBySeed)
	rotatingAs(t, u.rig.calls, rotateAsTheRuntime(u.rig.calls))
	_, panel, _ := readAudit(t, u.ts, "")
	if panel.Rotation == nil || panel.Rotation.State != rotationAvailable {
		t.Fatalf("the panel offers %+v", panel.Rotation)
	}
	return u, panel.Rotation.Token
}

// nameInConfig points the project's audit.signingKey at path.
func (u *signingUpgrade) nameInConfig(t *testing.T, path string) {
	t.Helper()
	config := strings.Replace(readFile(t, filepath.Join(u.project, "jpack.json")), `"signingKey": "`+u.seed+`"`, `"signingKey": "`+path+`"`, 1)
	if !strings.Contains(config, path) {
		t.Fatal("jpack.json did not name the key Desk keeps")
	}
	if err := os.WriteFile(filepath.Join(u.project, "jpack.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

// copyOfSeed is a private copy of the project's seed, the same key in another
// file, at path.
func (u *signingUpgrade) copyOfSeed(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(readFile(t, u.seed)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// **On the project Desk was started on, only the key its jpack.json names is
// rotated** (review round 1 of #261). Where jpack.json names a copy of the key,
// the same key in another file, no rotation is offered, and a confirmation
// with the token given before rotates nothing; a seed replaced by a copy since
// the panel makes its token stale; and where jpack.json stops naming Desk's
// key while a rotation runs, the rotation is not reported made.
func TestARotationOfTheStartupKeyFollowsWhatJpackJsonNames(t *testing.T) {
	rotatedNothing := func(t *testing.T, u *signingUpgrade) {
		t.Helper()
		list := readFile(t, strings.TrimSuffix(u.seed, ".seed")+".keys.jsonl")
		sidecar := readFile(t, filepath.Join(u.project, ".desk-private", "audit", "signatures.jsonl"))
		if seedIs(u.seed) != "1" || list != wantKeyLine(standInPublicKey, standInKeyID, 0) || strings.Contains(sidecar, "key-rotation") {
			t.Errorf("a refused rotation left the seed %s, the list %q, the sidecar %q", seedIs(u.seed), list, sidecar)
		}
	}

	t.Run("jpack.json names a copy of the key", func(t *testing.T) {
		u, token := signedThenRotatable(t)
		elsewhere := filepath.Join(t.TempDir(), "copy.seed")
		u.copyOfSeed(t, elsewhere)
		u.nameInConfig(t, elsewhere)
		want := "Desk rotates the signing key of the project it was started on only where its jpack.json names the key Desk keeps: its jpack.json names another file, or none. A rotation now would hand signing over in the trail while jpack.json named a key that signs nothing more."
		_, panel, _ := readAudit(t, u.ts, "")
		if panel.Rotation == nil || panel.Rotation.State != rotationUnavailable || panel.Rotation.Reason != want {
			t.Errorf("the panel offers %+v", panel.Rotation)
		}
		status, data := reviewCall(t, u.ts, "POST", "/api/audit/key/rotate", "", map[string]string{"token": token}, bearer)
		if status != http.StatusConflict || refusalOf(data) != "Nothing was rotated. "+want {
			t.Errorf("the rotation answered %d %s", status, data)
		}
		rotatedNothing(t, u)
	})

	t.Run("Desk's seed replaced by a copy since the panel", func(t *testing.T) {
		u, token := signedThenRotatable(t)
		staged := strings.TrimSuffix(u.seed, ".seed") + ".copy"
		u.copyOfSeed(t, staged)
		if err := os.Rename(staged, u.seed); err != nil {
			t.Fatal(err)
		}
		status, data := reviewCall(t, u.ts, "POST", "/api/audit/key/rotate", "", map[string]string{"token": token}, bearer)
		if status != http.StatusConflict || refusalOf(data) != "This desk's keys changed after the decision record showed them, so nothing was rotated. Check the decision record again." {
			t.Errorf("the rotation answered %d %s", status, data)
		}
		rotatedNothing(t, u)
		if _, panel, _ := readAudit(t, u.ts, ""); panel.Rotation == nil || panel.Rotation.State != rotationAvailable || panel.Rotation.Token == token {
			t.Errorf("after the copy the panel offers %+v", panel.Rotation)
		}
	})

	t.Run("jpack.json changed while the rotation runs", func(t *testing.T) {
		u, token := signedThenRotatable(t)
		elsewhere := filepath.Join(t.TempDir(), "copy.seed")
		u.copyOfSeed(t, elsewhere)
		testHookKeyBetween = func(at string) {
			if at == "rotation: line written" {
				u.nameInConfig(t, elsewhere)
			}
		}
		t.Cleanup(func() { testHookKeyBetween = nil })
		status, data := reviewCall(t, u.ts, "POST", "/api/audit/key/rotate", "", map[string]string{"token": token}, bearer)
		testHookKeyBetween = nil
		if status != http.StatusConflict || !strings.HasPrefix(refusalOf(data), "The runtime handed signing over after record 1 and Desk now keeps the next key, but this project's jpack.json no longer names the key Desk keeps: its jpack.json names another file, or none.") ||
			strings.Contains(string(data), `"rotated"`) || strings.Contains(string(data), elsewhere) {
			t.Errorf("the rotation answered %d %s", status, data)
		}
	})
}

// **The project's key is named by the project's resolved path** (review round
// 1 of #261): the same name, and so the same key, whether Desk is started on
// the project through a linked checkout, with a trailing slash, or by a
// relative path; another name for another project. A stopped upgrade made
// through the link is finished at a start through the trailing slash, and the
// key it made is the one the decision record finds there, and from a relative
// start.
func TestTheStartupKeyIsNamedByTheResolvedProject(t *testing.T) {
	t.Setenv("JPACK_CONFIG", "")
	t.Setenv("JPACK_SIGNING_KEY", "")
	base := t.TempDir()
	real := filepath.Join(base, "real")
	other := filepath.Join(base, "other")
	for _, dir := range []string{real, other} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		writeProject(t, dir, map[string]string{"jpack.json": upgradeBefore, "packs/a.json": reviewPack, "packs/b.json": otherPack})
	}
	link := filepath.Join(base, "checkout")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(resolved))
	want := hex.EncodeToString(sum[:])

	rig := newReviewRigReading(t, withAuditVersions, "", "")
	rig.answers(t, "error")
	audit := &auditRig{bin: rig.bin, calls: rig.calls, answer: filepath.Join(t.TempDir(), "verify.json")}
	script := bytes.Replace([]byte(readFile(t, rig.bin)), []byte("'packs lock')\n"), []byte(auditVerifyCase(audit)+"'packs lock')\n"), 1)
	if err := os.WriteFile(rig.bin, script, 0o755); err != nil {
		t.Fatal(err)
	}
	audit.answers(t, 0, auditValidReport)
	config := t.TempDir()
	start := func(t *testing.T, dir string) (*Server, *httptest.Server) {
		t.Helper()
		s, ts := startDesk(t, Config{ProjectDir: dir, JpackBin: rig.bin, Token: testToken, DeskConfigDir: config, Logger: log.New(io.Discard, "", 0)})
		t.Cleanup(func() { s.Close(); ts.Close() })
		return s, ts
	}

	s, ts := start(t, link)
	if got := s.signingKeyName(); got != want {
		t.Fatalf("started through a link, the key is named %s, want %s", got, want)
	}
	// A stopped upgrade, made through the link.
	status, data := reviewCall(t, ts, "GET", "/api/upgrade?signingKey=true", "", nil, bearer)
	var offer upgradeAnswer
	if status != http.StatusOK || json.Unmarshal(data, &offer) != nil || !offer.Sign {
		t.Fatalf("the offer answered %d %s", status, data)
	}
	rig.locks(t, upgradeLock(t, real, offer.ConfigAfter, bothPacks))
	testHookKeyBetween = func(at string) {
		if at == "upgrade: named" {
			panic(http.ErrAbortHandler)
		}
	}
	t.Cleanup(func() { testHookKeyBetween = nil })
	body, _ := json.Marshal(map[string]any{"token": offer.Token, "requireComparableFacts": true, "signingKey": true})
	request, _ := http.NewRequest("POST", ts.URL+"/api/upgrade", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	if response, err := ts.Client().Do(request); err == nil {
		response.Body.Close()
	}
	testHookKeyBetween = nil
	signing := filepath.Join(config, "secrets", "signing")
	if names := namesIn(t, signing); !slices.Equal(names, []string{want + ".creating", want + ".keys.jsonl", want + ".seed"}) {
		t.Fatalf("the stopped upgrade left %q", names)
	}
	s.Close()
	ts.Close()

	again, ts := start(t, real+string(filepath.Separator))
	if got := again.signingKeyName(); got != want {
		t.Errorf("started with a trailing slash, the key is named %s, want %s", got, want)
	}
	if names := namesIn(t, signing); !slices.Equal(names, []string{want + ".keys.jsonl", want + ".seed"}) {
		t.Errorf("the start with a trailing slash left %q", names)
	}
	if _, panel, refusal := readAudit(t, ts, ""); panel.Keys == nil || panel.Keys.State != keysKept || !slices.Equal(panel.Keys.Public, []deskPublicKey{key1}) {
		t.Errorf("started with a trailing slash, the panel shows %+v %q", panel.Keys, refusal)
	}
	again.Close()
	ts.Close()

	t.Chdir(base)
	relative, ts := start(t, "real")
	if got := relative.signingKeyName(); got != want {
		t.Errorf("started by a relative path, the key is named %s, want %s", got, want)
	}
	if _, panel, refusal := readAudit(t, ts, ""); panel.Keys == nil || panel.Keys.State != keysKept {
		t.Errorf("started by a relative path, the panel shows %+v %q", panel.Keys, refusal)
	}
	if elsewhere, _ := start(t, other); elsewhere.signingKeyName() == want {
		t.Errorf("another project's key is named %s too", want)
	}
}
