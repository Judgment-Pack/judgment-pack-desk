package desk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func dialectArtifactWorkDomainPath(s *Server, id string) string {
	return dialectArtifactOwnerTablePath(s, "work-domains", id[:2], id[2:]+".json")
}

func TestDialectArtifactWorkDomainNativeProducer(t *testing.T) {
	for _, contained := range []bool{false, true} {
		t.Run(fmt.Sprint("contained-settings-", contained), func(t *testing.T) {
			// These legal native paths also witness that no history-path
			// normalization is applied to physical work locators.
			config := filepath.Join(t.TempDir(), "config-quote\"-backslash\\-newline\n")
			s, r := dialectArtifactOwnerFixture(t, filepath.Join(t.TempDir(), "project"), config)
			if contained {
				s.cfg.deskID = "desk_work_domain_fixture"
				private, err := openDialectArtifactOwnerFolder(t.Context(), s.root, true, "fixture", ".desk-private")
				if err != nil {
					t.Fatal(err)
				}
				if err = private.Close(); err != nil {
					t.Fatal(err)
				}
				lock, err := s.privateDataLockContext(t.Context(), true)
				if err != nil {
					t.Fatal(err)
				}
				data, openErr := s.openChatData()
				if openErr == nil {
					openErr = data.root.Close()
				}
				if err = errors.Join(openErr, lock.Close()); err != nil {
					t.Fatal(err)
				}
				settings, err := s.storageSettingsRoot()
				if err != nil {
					t.Fatal(err)
				}
				settingsInfo, statErr := settings.Stat(".")
				actual, actualErr := s.assistant.root.Stat(".")
				if err = errors.Join(statErr, actualErr, settings.Close()); err != nil || os.SameFile(settingsInfo, actual) {
					t.Fatal("fixture did not separate settings from actual work root", err)
				}
			}
			before := dialectArtifactOwnerNames(t, s.assistant.root)
			v, err := s.enrollDialectArtifactOwner(r)
			if err != nil {
				t.Fatal(err)
			}
			path := dialectArtifactWorkDomainPath(s, v.WorkDomain)
			raw := dialectArtifactOwnerRead(t, path)
			row, err := readDialectArtifactWorkDomainRecord(raw)
			if err != nil {
				t.Fatal(err)
			}
			folder, folderErr := s.assistant.root.Stat(".")
			lock, lockErr := s.assistant.root.Stat(".data-work.lock")
			if folderErr != nil || lockErr != nil {
				t.Fatal(folderErr, lockErr)
			}
			formula, formulaErr := json.Marshal([]string{"dialect-artifact-work-v1", identityKey(folder), identityKey(lock)})
			if folderErr != nil || lockErr != nil || formulaErr != nil || row.ID != digestOf(formula) || row.ID != v.WorkDomain ||
				row.FolderIdentity != identityKey(folder) || row.LockIdentity != identityKey(lock) || !bytes.Equal(row.PathBytes, []byte(config)) ||
				row.Kind != "settings" || v.WorkSHA256 != digestOf(raw) || v.Namespace != digestOf([]byte(s.conversationIdentity())) {
				t.Fatal("work provenance differs from original actual pins", row, v, folderErr, lockErr, formulaErr)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			info, statErr := file.Stat()
			if err = errors.Join(statErr, file.Close()); err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("work row custody", err)
			}
			if again, err := s.enrollDialectArtifactOwner(r); err != nil || again != v || !bytes.Equal(raw, dialectArtifactOwnerRead(t, path)) {
				t.Fatal("repeat selected work enrollment changed exact identity", again, err)
			}
			// A newly opened original project/store must select the same row.
			if !contained {
				reopened, request := dialectArtifactOwnerFixture(t, s.projectDir, s.configDir)
				if again, err := reopened.enrollDialectArtifactOwner(request); err != nil || again != v {
					t.Fatal("actual reopen changed work provenance", again, err)
				}
			}
			if !reflect.DeepEqual(before, dialectArtifactOwnerNames(t, s.assistant.root)) {
				t.Fatal("work metadata changed legacy chat-root enumeration")
			}
			dialectArtifactOwnerNoEffects(t, s)
		})
	}
}

func TestDialectArtifactWorkDomainClosedCodec(t *testing.T) {
	// Hand-authored maximal fixed shape. The ID is independently calculated
	// SHA256 of ["dialect-artifact-work-v1", maxIdentity, maxIdentity].
	const maxIdentity = "18446744073709551615:18446744073709551615"
	const maxID = "38eaf0bb0a44340d02c84c1ec4d4d934b98d687b42a491a2a701cd801f1124e9"
	path := []byte("/" + strings.Repeat("x", 4095))
	encodedPath := base64.StdEncoding.EncodeToString(path)
	maximum := []byte(`{"version":1,"id":"` + maxID + `","kind":"settings","pathBytes":"` + encodedPath + `","folderIdentity":"` + maxIdentity + `","lockIdentity":"` + maxIdentity + `"}`)
	if len(path) != 4096 || len(encodedPath) != 5464 || len(maximum) != 5702 {
		t.Fatal("handwritten work row bound changed", len(path), len(encodedPath), len(maximum))
	}
	row, err := readDialectArtifactWorkDomainRecord(maximum)
	if err != nil || row.ID != maxID || !bytes.Equal(row.PathBytes, path) {
		t.Fatal("maximal closed work shape refused", err)
	}
	oversize := append(append([]byte{}, maximum...), ' ')
	if len(oversize) != 5703 {
		t.Fatal("oversize witness changed")
	}
	if _, err := readDialectArtifactWorkDomainRecord(oversize); err == nil {
		t.Fatal("5703-byte work row accepted")
	}
	for _, identity := range []string{"0:0", "-1:1", "-9223372036854775808:9223372036854775807", maxIdentity} {
		v := row
		v.FolderIdentity, v.LockIdentity = identity, identity
		v.ID = dialectArtifactWorkDomainID(identity, identity)
		raw, _ := json.Marshal(v)
		if _, err := readDialectArtifactWorkDomainRecord(raw); err != nil {
			t.Fatal("native signed/unsigned identity refused", identity, err)
		}
	}
	for _, identity := range []string{"", ":1", "1:", "+1:1", "01:1", "-0:1", "1:00", "-01:1", "18446744073709551616:1", "-9223372036854775809:1", "1:2:3"} {
		for _, member := range []string{"folder", "lock"} {
			v := row
			if member == "folder" {
				v.FolderIdentity = identity
			} else {
				v.LockIdentity = identity
			}
			v.ID = dialectArtifactWorkDomainID(v.FolderIdentity, v.LockIdentity)
			raw, _ := json.Marshal(v)
			if _, err := readDialectArtifactWorkDomainRecord(raw); err == nil {
				t.Fatal("non-native identity accepted", member, identity)
			}
		}
	}
	for _, path := range [][]byte{nil, {}, []byte("relative"), []byte("/nul\x00"), append([]byte{'/'}, bytes.Repeat([]byte{'x'}, 4096)...)} {
		v := row
		v.PathBytes = path
		raw, _ := json.Marshal(v)
		if _, err := readDialectArtifactWorkDomainRecord(raw); err == nil {
			t.Fatal("invalid work locator accepted", len(path))
		}
	}
	exact := []byte{'/', 'q', '"', '\\', '\n', 0xff}
	row.PathBytes = exact
	valid, _ := json.Marshal(row)
	decoded, err := readDialectArtifactWorkDomainRecord(valid)
	if err != nil || !bytes.Equal(decoded.PathBytes, exact) {
		t.Fatal("raw work path was normalized", err)
	}
	for name, mutation := range map[string][]byte{
		"empty":               nil,
		"truncated":           valid[:len(valid)-1],
		"newline":             append(append([]byte{}, valid...), '\n'),
		"duplicate":           bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		"case":                bytes.Replace(valid, []byte(`"lockIdentity"`), []byte(`"LockIdentity"`), 1),
		"escaped-key":         bytes.Replace(valid, []byte(`"id"`), []byte(`"\u0069d"`), 1),
		"unknown":             bytes.Replace(valid, []byte(`"version":1`), []byte(`"extra":0,"version":1`), 1),
		"null-kind":           bytes.Replace(valid, []byte(`"settings"`), []byte(`null`), 1),
		"null-path":           bytes.Replace(valid, []byte(`"pathBytes":"L3EiXAr/"`), []byte(`"pathBytes":null`), 1),
		"base64-newline":      bytes.Replace(valid, []byte(`L3EiXAr/`), []byte(`L3Ei\nXAr/`), 1),
		"base64-url":          bytes.Replace(valid, []byte(`L3EiXAr/`), []byte(`L3EiXAr_`), 1),
		"base64-padding-bits": bytes.Replace(valid, []byte(`L3EiXAr/`), []byte(`Lx==`), 1),
		"path-array":          bytes.Replace(valid, []byte(`"pathBytes":"L3EiXAr/"`), []byte(`"pathBytes":[47]`), 1),
		"version-type":        bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":"1"`), 1),
		"version-number":      bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":1.0`), 1),
		"version-unknown":     bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":2`), 1),
		"kind":                bytes.Replace(valid, []byte(`"settings"`), []byte(`"home"`), 1),
		"wrong-id":            bytes.Replace(valid, []byte(maxID), []byte(strings.Repeat("a", 64)), 1),
		"upper-id":            bytes.Replace(valid, []byte(maxID), []byte(strings.ToUpper(maxID)), 1),
		"missing-lock":        bytes.Replace(valid, []byte(`,"lockIdentity":"`+maxIdentity+`"`), nil, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(valid, mutation) {
				t.Fatal("handwritten mutation did not change its target")
			}
			if _, err := readDialectArtifactWorkDomainRecord(mutation); err == nil {
				t.Fatal("noncanonical work row accepted", string(mutation))
			}
		})
	}
}

func TestDialectArtifactWorkDomainPersistenceFailures(t *testing.T) {
	for _, reused := range []bool{false, true} {
		steps := []string{"sync", "file-close", "directory-sync", "readback", "selected-readback"}
		if !reused {
			steps = append(steps, "write", "publish")
		}
		for _, phase := range steps {
			t.Run(fmt.Sprintf("reused-%v/%s", reused, phase), func(t *testing.T) {
				s, r := dialectArtifactOwnerSetup(t)
				var prior []byte
				var path string
				if reused {
					v, err := s.enrollDialectArtifactOwner(r)
					if err != nil {
						t.Fatal(err)
					}
					path = dialectArtifactWorkDomainPath(s, v.WorkDomain)
					prior = dialectArtifactOwnerRead(t, path)
				}
				injected := errors.New("work row persistence uncertain")
				called, afterFailureWrite := false, false
				testHookDialectArtifactOwnerStep = func(step string) error {
					if called && strings.HasSuffix(step, ".write") {
						afterFailureWrite = true
					}
					if step == "work-domain."+phase {
						called = true
						return injected
					}
					return nil
				}
				t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
				if got, err := s.enrollDialectArtifactOwner(r); !called || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) || afterFailureWrite {
					t.Fatal("work row uncertainty acknowledged or retried", called, afterFailureWrite, got, err)
				}
				if reused && !bytes.Equal(prior, dialectArtifactOwnerRead(t, path)) {
					t.Fatal("exact reuse rewrote work row")
				}
				if !reused && phase != "selected-readback" {
					if _, err := s.root.Lstat(".desk-private/" + dialectArtifactOwnerMarker); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("marker published after uncertain work row", err)
					}
				}
				dialectArtifactOwnerNoEffects(t, s)
			})
		}
	}
}

func TestDialectArtifactWorkDomainSelectedEdgesAndCloseFailures(t *testing.T) {
	for _, reused := range []bool{false, true} {
		for _, phase := range []string{"directory-sync", "directory-publish", "close", "readback-close"} {
			if reused && phase == "directory-publish" {
				continue
			}
			// Two selected edges at publication and two at final reread.
			count := 4
			if phase == "directory-publish" {
				count = 2
			} else if phase == "close" || phase == "readback-close" {
				count = 1
			}
			for edge := 1; edge <= count; edge++ {
				t.Run(fmt.Sprintf("reused-%v/%s-%d", reused, phase, edge), func(t *testing.T) {
					s, r := dialectArtifactOwnerSetup(t)
					if reused {
						if _, err := s.enrollDialectArtifactOwner(r); err != nil {
							t.Fatal(err)
						}
					}
					step := "work-domain-folder." + phase
					if phase == "readback-close" {
						step = "work-domain-readback.close"
					}
					injected := errors.New("selected work edge or Close uncertain")
					calls := 0
					testHookDialectArtifactOwnerStep = func(at string) error {
						if at == step {
							calls++
							if calls == edge {
								return injected
							}
						}
						return nil
					}
					t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
					if got, err := s.enrollDialectArtifactOwner(r); calls != edge || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) {
						t.Fatal("selected work edge/Close uncertainty acknowledged", calls, got, err)
					}
				})
			}
		}
	}
}

func TestDialectArtifactWorkDomainPublicationReentry(t *testing.T) {
	s, r := dialectArtifactOwnerSetup(t)
	injected := errors.New("work row landed before parent sync")
	var raw []byte
	var path string
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step == "work-domain.publish" {
			folder, err := s.assistant.root.Stat(".")
			if err != nil {
				return err
			}
			lock, err := s.assistant.root.Stat(".data-work.lock")
			if err != nil {
				return err
			}
			path = dialectArtifactWorkDomainPath(s, dialectArtifactWorkDomainID(identityKey(folder), identityKey(lock)))
			raw = dialectArtifactOwnerRead(t, path)
			return injected
		}
		return nil
	}
	t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
	if got, err := s.enrollDialectArtifactOwner(r); path == "" || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("interrupted work row was acknowledged", got, err)
	}
	seen := map[string]bool{}
	testHookDialectArtifactOwnerStep = func(step string) error {
		seen[step] = true
		if step == "work-domain.directory-sync" {
			return injected
		}
		return nil
	}
	if got, err := s.enrollDialectArtifactOwner(r); !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) || !seen["work-domain.sync"] || !seen["work-domain.file-close"] || seen["work-domain.publish"] {
		t.Fatal("visible exact work row bypassed file/parent durability", seen, got, err)
	}
	seen = map[string]bool{}
	testHookDialectArtifactOwnerStep = func(step string) error { seen[step] = true; return nil }
	if _, err := s.enrollDialectArtifactOwner(r); err != nil {
		t.Fatal("explicit exact reentry failed", err)
	}
	for _, phase := range []string{"sync", "file-close", "directory-sync", "readback", "selected-readback"} {
		if !seen["work-domain."+phase] {
			t.Fatal("work reuse omitted durability/readback", phase)
		}
	}
	if seen["work-domain.publish"] || !bytes.Equal(raw, dialectArtifactOwnerRead(t, path)) {
		t.Fatal("reentry replaced immutable work row")
	}
}

func TestDialectArtifactWorkDomainClaimCollision(t *testing.T) {
	s, r := dialectArtifactOwnerSetup(t)
	var path string
	// Land another complete row at the selected final name immediately
	// before Link. Claim must refuse and preserve it without retry or repair.
	testHookDialectArtifactOwnerStep = func(step string) error {
		if step != "work-domain.file-close" {
			return nil
		}
		folder, err := s.assistant.root.Stat(".")
		if err != nil {
			return err
		}
		lock, err := s.assistant.root.Stat(".data-work.lock")
		if err != nil {
			return err
		}
		path = dialectArtifactWorkDomainPath(s, dialectArtifactWorkDomainID(identityKey(folder), identityKey(lock)))
		return os.WriteFile(path, []byte(`{}`), 0600)
	}
	t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
	if got, err := s.enrollDialectArtifactOwner(r); path == "" || !errors.Is(err, os.ErrExist) || got != (dialectArtifactOwnerEnrollment{}) {
		t.Fatal("uncertain immutable claim collision acknowledged", path, got, err)
	}
	if string(dialectArtifactOwnerRead(t, path)) != `{}` {
		t.Fatal("claim collision replaced selected metadata")
	}
	if _, err := s.root.Lstat(".desk-private/" + dialectArtifactOwnerMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("claim collision published marker", err)
	}
}

func TestDialectArtifactWorkDomainOrderingAndEarlyRefusal(t *testing.T) {
	for _, failAt := range []string{"", "work.sync", "work.directory-sync", "marker.publish"} {
		t.Run(failAt, func(t *testing.T) {
			s, r := dialectArtifactOwnerFixtureWithWorkLock(t, filepath.Join(t.TempDir(), "project"), filepath.Join(t.TempDir(), "config"), false)
			seen := map[string]bool{}
			injected := errors.New("work/marker durability uncertain")
			testHookDialectArtifactOwnerStep = func(step string) error {
				seen[step] = true
				switch step {
				case "work-domain.write":
					if !seen["work.sync"] || !seen["work.directory-sync"] {
						t.Error("work record preceded actual work durability")
					}
				case "owner.write":
					if !seen["work-domain.readback"] {
						t.Error("owner preceded exact durable work record")
					}
				case "marker.publish":
					if !seen["owner.readback"] {
						t.Error("marker preceded exact durable owner")
					}
				case "scope.write":
					if !seen["marker.readback"] {
						t.Error("scope preceded exact durable marker")
					}
				}
				if step == failAt {
					return injected
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			got, err := s.enrollDialectArtifactOwner(r)
			if failAt == "" {
				if err != nil || !seen["work-domain.selected-readback"] || got.WorkSHA256 == "" {
					t.Fatal("actual producer omitted work acknowledgment", seen, got, err)
				}
			} else if !seen[failAt] || !errors.Is(err, injected) || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("uncertainty acknowledged", seen, got, err)
			}
			if failAt == "work.sync" || failAt == "work.directory-sync" {
				if _, err := os.Lstat(dialectArtifactOwnerTablePath(s)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("work durability refusal created metadata", err)
				}
			}
			if failAt == "marker.publish" && seen["scope.write"] {
				t.Fatal("scope followed uncertain marker")
			}
		})
	}
	for _, damage := range []string{"missing-owner", "corrupt-owner"} {
		t.Run(damage, func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			v, err := s.enrollDialectArtifactOwner(r)
			if err != nil {
				t.Fatal(err)
			}
			ownerPath, _ := dialectArtifactOwnerRowPaths(s, v)
			if damage == "missing-owner" {
				err = os.Remove(ownerPath)
			} else {
				err = os.WriteFile(ownerPath, []byte(`{}`), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			workPath := dialectArtifactWorkDomainPath(s, v.WorkDomain)
			if err = os.Remove(workPath); err != nil {
				t.Fatal(err)
			}
			testHookDialectArtifactOwnerStep = func(step string) error {
				if strings.HasPrefix(step, "work-domain") {
					t.Error("existing-marker owner validation followed work mutation", step)
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("marker-only/corrupt-owner state reconstructed", got, err)
			}
			if _, err := os.Lstat(workPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("owner validation refusal created work row", err)
			}
		})
	}
}

func TestDialectArtifactWorkDomainRefusesChangedRows(t *testing.T) {
	for _, change := range []string{"corrupt", "wrong-pins", "symlink", "directory", "mode", "table-symlink", "table-mode"} {
		t.Run(change, func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			v, err := s.enrollDialectArtifactOwner(r)
			if err != nil {
				t.Fatal(err)
			}
			path := dialectArtifactWorkDomainPath(s, v.WorkDomain)
			var changed []byte
			switch change {
			case "corrupt":
				changed = []byte(`{}`)
				err = os.WriteFile(path, changed, 0600)
			case "wrong-pins":
				row, readErr := readDialectArtifactWorkDomainRecord(dialectArtifactOwnerRead(t, path))
				if readErr != nil {
					t.Fatal(readErr)
				}
				row.LockIdentity = "0:0"
				row.ID = dialectArtifactWorkDomainID(row.FolderIdentity, row.LockIdentity)
				changed, err = json.Marshal(row)
				if err == nil {
					err = os.WriteFile(path, changed, 0600)
				}
			case "symlink", "directory":
				if err = os.Remove(path); err == nil {
					if change == "directory" {
						err = os.Mkdir(path, 0700)
					} else {
						outside := filepath.Join(t.TempDir(), "outside")
						err = os.WriteFile(outside, []byte("unrelated"), 0600)
						if err == nil {
							err = os.Symlink(outside, path)
						}
					}
				}
			case "mode":
				err = os.Chmod(path, 0644)
			case "table-symlink":
				table := dialectArtifactOwnerTablePath(s, "work-domains")
				if err = os.Rename(table, table+"-old"); err == nil {
					err = os.Symlink(table+"-old", table)
				}
			case "table-mode":
				err = os.Chmod(dialectArtifactOwnerTablePath(s, "work-domains"), 0755)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("changed work metadata acknowledged", got, err)
			}
			if changed != nil && !bytes.Equal(changed, dialectArtifactOwnerRead(t, path)) {
				t.Fatal("corrupt/different immutable work row repaired")
			}
		})
	}
}

func TestDialectArtifactWorkDomainReplacementAndCancellation(t *testing.T) {
	for _, change := range []string{"root", "lock", "metadata", "table", "shard", "row", "cancel", "revoke"} {
		t.Run(change, func(t *testing.T) {
			s, r := dialectArtifactOwnerSetup(t)
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			called := false
			testHookDialectArtifactOwnerStep = func(step string) error {
				if called || step != "selected-readback" {
					return nil
				}
				called = true
				folder, err := s.assistant.root.Stat(".")
				if err != nil {
					return err
				}
				lock, err := s.assistant.root.Stat(".data-work.lock")
				if err != nil {
					return err
				}
				id := dialectArtifactWorkDomainID(identityKey(folder), identityKey(lock))
				path := ""
				switch change {
				case "root":
					path = s.configDir
				case "lock":
					path = filepath.Join(s.configDir, ".data-work.lock")
				case "metadata":
					path = dialectArtifactOwnerTablePath(s)
				case "table":
					path = dialectArtifactOwnerTablePath(s, "work-domains")
				case "shard":
					path = filepath.Dir(dialectArtifactWorkDomainPath(s, id))
				case "row":
					return os.WriteFile(dialectArtifactWorkDomainPath(s, id), []byte(`{}`), 0600)
				case "cancel":
					cancel()
					return nil
				case "revoke":
					s.sessions.revoke(bearerOf(r))
					return nil
				}
				if err := os.Rename(path, path+"-old"); err != nil {
					return err
				}
				if change == "lock" {
					return os.WriteFile(path, nil, 0600)
				}
				return os.Mkdir(path, 0700)
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			if got, err := s.enrollDialectArtifactOwner(r); !called || err == nil || got != (dialectArtifactOwnerEnrollment{}) {
				t.Fatal("changed source/selected work metadata acknowledged", called, got, err)
			}
		})
	}
}

func TestDialectArtifactWorkDomainUnsupportedInjectedAndNoEnumeration(t *testing.T) {
	t.Run("unsupported-injected", func(t *testing.T) {
		s, r := dialectArtifactOwnerSetup(t)
		injected := &dialectArtifactOwnerInjected{}
		s.retainedArtifacts = injected
		seen := false
		testHookDialectArtifactOwnerStep = func(string) error { seen = true; return nil }
		t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
		if got, err := s.enrollDialectArtifactOwner(r); err == nil || got != (dialectArtifactOwnerEnrollment{}) || seen || injected.calls != 0 {
			t.Fatal("unsupported injected producer reached metadata or provider", seen, injected.calls, got, err)
		}
		for _, path := range []string{dialectArtifactOwnerTablePath(s), filepath.Join(s.projectDir, ".desk-private", dialectArtifactOwnerMarker)} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unsupported injected producer wrote metadata", path, err)
			}
		}
	})
	t.Run("irrelevant-work-rows", func(t *testing.T) {
		s, r := dialectArtifactOwnerSetup(t)
		shard := dialectArtifactOwnerTablePath(s, "work-domains", "ff")
		if err := os.MkdirAll(shard, 0700); err != nil {
			t.Fatal(err)
		}
		for i := 0; i <= 4096; i++ {
			if err := os.WriteFile(filepath.Join(shard, fmt.Sprintf("%062x.json", i)), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(shard, "unselected-large"), bytes.Repeat([]byte{'x'}, (1<<20)+1), 0600); err != nil {
			t.Fatal(err)
		}
		before := dialectArtifactOwnerNames(t, s.assistant.root)
		if _, err := s.enrollDialectArtifactOwner(r); err != nil {
			t.Fatal("unselected work rows imposed aggregate count/byte admission", err)
		}
		if !reflect.DeepEqual(before, dialectArtifactOwnerNames(t, s.assistant.root)) {
			t.Fatal("work table changed legacy chat-root enumeration")
		}
	})
}
