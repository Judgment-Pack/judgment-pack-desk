package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageInstallationRecordVectors(t *testing.T) {
	base := storageInstallationRecord{Version: 1, Kind: "installation", Installation: storagebudget.ID{1}, Nonce: storagebudget.ID{2}, Config: storageDirectoryIdentity{3, 4}, Anchor: storageDirectoryIdentity{5, 6}, HomeOwner: "synthetic-original-home"}
	vectors := []struct {
		name   string
		change func(*storageInstallationRecord)
	}{
		{"version", func(r *storageInstallationRecord) { r.Version = 2 }},
		{"kind", func(r *storageInstallationRecord) { r.Kind = "foreign" }},
		{"installation", func(r *storageInstallationRecord) { r.Installation = storagebudget.ID{} }},
		{"nonce", func(r *storageInstallationRecord) { r.Nonce = storagebudget.ID{} }},
		{"config", func(r *storageInstallationRecord) { r.Config = storageDirectoryIdentity{} }},
		{"anchor", func(r *storageInstallationRecord) { r.Anchor = storageDirectoryIdentity{} }},
		{"home-owner", func(r *storageInstallationRecord) { r.HomeOwner = "" }},
		{"home-owner-oversize", func(r *storageInstallationRecord) { r.HomeOwner = strings.Repeat("x", 257) }},
		{"home-owner-newline", func(r *storageInstallationRecord) { r.HomeOwner = "a\nb" }},
		{"claim-with-anchor", func(r *storageInstallationRecord) { r.Kind = "claim" }},
	}
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			r := base
			v.change(&r)
			if _, e := storageRecordBytes(r); !errors.Is(e, errStorageCustody) {
				t.Fatalf("invalid member accepted: %v", e)
			}
		})
	}
	t.Run("canonical-parser", func(t *testing.T) {
		path := t.TempDir()
		storageTestMust(t, os.Chmod(path, 0700))
		root, e := os.OpenRoot(path)
		storageTestMust(t, e)
		defer root.Close()
		canonical, e := storageRecordBytes(base)
		storageTestMust(t, e)
		malformed := [][]byte{nil, []byte("{}"), []byte(strings.Repeat("x", storageRecordLimit+1)), append([]byte(" "), canonical...), append(canonical, []byte("{}")...), []byte(strings.Replace(string(canonical), "\"Version\":1", "\"Version\":1,\"Foreign\":1", 1)), []byte(strings.Replace(string(canonical), "\"Version\":1", "\"Version\":2,\"Version\":1", 1))}
		for i, b := range malformed {
			name := string(rune('a' + i))
			storageTestMust(t, os.WriteFile(filepath.Join(path, name), b, 0600))
			if _, n, _, e := storageReadRecord(root, name); e == nil {
				n.file.Close()
				t.Fatalf("noncanonical vector %d accepted", i)
			}
		}
		storageTestMust(t, os.WriteFile(filepath.Join(path, "valid"), canonical, 0600))
		r, n, b, e := storageReadRecord(root, "valid")
		storageTestMust(t, e)
		storageTestMust(t, n.file.Close())
		if r != base || string(b) != string(canonical) {
			t.Fatal("canonical members lost")
		}
		claim := base
		claim.Kind = "claim"
		claim.Anchor = storageDirectoryIdentity{}
		if _, e = storageRecordBytes(claim); e != nil {
			t.Fatal("valid initial claim refused")
		}
	})
}

func TestStorageBootstrapCapacityVectors(t *testing.T) {
	opt := storageTestOptions()
	opt.HeadroomBytes = 0
	opt.HeadroomInodes = 0
	vectors := []struct {
		name          string
		bytes, inodes uint64
		known         bool
		age           time.Duration
		want          error
	}{
		{"exact", storagebudget.MetadataBudget + storageHostBudget, 37, true, 0, nil},
		{"bytes", storagebudget.MetadataBudget + storageHostBudget - 1, 37, true, 0, storagebudget.ErrHeadroom},
		{"inodes", storagebudget.MetadataBudget + storageHostBudget, 36, true, 0, storagebudget.ErrHeadroom},
		{"unknown", 1 << 30, 4096, false, 0, storagebudget.ErrUnknown},
		{"stale", 1 << 30, 4096, true, 2 * time.Minute, storagebudget.ErrUnknown},
		{"future", 1 << 30, 4096, true, -time.Minute, storagebudget.ErrUnknown},
	}
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			e := storageBootstrapCapacity(opt, storagebudget.Observation{AvailableBytes: v.bytes, AvailableInodes: v.inodes, Known: v.known, At: time.Now().Add(-v.age)})
			if !errors.Is(e, v.want) {
				t.Fatalf("want %v got %v", v.want, e)
			}
		})
	}
	t.Run("headroom", func(t *testing.T) {
		opt.HeadroomBytes = 1
		opt.HeadroomInodes = 1
		if e := storageBootstrapCapacity(opt, storagebudget.Observation{AvailableBytes: storagebudget.MetadataBudget + storageHostBudget, AvailableInodes: 37, Known: true, At: time.Now()}); !errors.Is(e, storagebudget.ErrHeadroom) {
			t.Fatal(e)
		}
	})
	t.Run("overflow", func(t *testing.T) {
		opt.HeadroomBytes = ^uint64(0)
		if e := storageBootstrapCapacity(opt, storagebudget.Observation{Known: true, At: time.Now()}); !errors.Is(e, storagebudget.ErrBounds) {
			t.Fatal(e)
		}
	})
}

func TestStorageBootstrapPrerequisiteRefusal(t *testing.T) {
	vectors := []struct {
		name    string
		prepare func(*testing.T, string, *storageBootstrapOptions) func()
		want    error
	}{
		{"missing-lease", func(t *testing.T, c string, o *storageBootstrapOptions) func() {
			storageTestMust(t, os.Remove(filepath.Join(c, ".home-use.lock")))
			return func() {}
		}, errStoragePrerequisite},
		{"unsafe-config", func(t *testing.T, c string, o *storageBootstrapOptions) func() {
			storageTestMust(t, os.Chmod(c, 0755))
			return func() {}
		}, errStorageCustody},
		{"unsafe-parent", func(t *testing.T, c string, o *storageBootstrapOptions) func() {
			storageTestMust(t, os.Chmod(filepath.Dir(c), 0755))
			return func() {}
		}, errStorageCustody},
		{"busy-original-lease", func(t *testing.T, c string, o *storageBootstrapOptions) func() {
			f, e := os.OpenFile(filepath.Join(c, ".home-use.lock"), os.O_RDWR, 0)
			storageTestMust(t, e)
			storageTestMust(t, lockPrivateData(f, false))
			return func() { storageTestMust(t, f.Close()) }
		}, storagebudget.ErrBusy},
		{"policy-installation", func(t *testing.T, c string, o *storageBootstrapOptions) func() {
			o.Policy.Installation.Total.Bytes = 1
			return func() {}
		}, storagebudget.ErrBounds},
		{"policy-control", func(t *testing.T, c string, o *storageBootstrapOptions) func() {
			o.Policy.Installation.Category[storagebudget.Control].Files = 31
			return func() {}
		}, storagebudget.ErrBounds},
		{"policy-workspace", func(t *testing.T, c string, o *storageBootstrapOptions) func() {
			o.Policy.Workspace.Total.Directories = 0
			return func() {}
		}, storagebudget.ErrBounds},
		{"observation-age", func(t *testing.T, c string, o *storageBootstrapOptions) func() {
			o.Policy.ObservationMaxAge = time.Hour + 1
			return func() {}
		}, storagebudget.ErrBounds},
		{"headroom-overflow", func(t *testing.T, c string, o *storageBootstrapOptions) func() {
			o.HeadroomInodes = ^uint64(0)
			return func() {}
		}, storagebudget.ErrBounds},
		{"external-journal", func(t *testing.T, c string, o *storageBootstrapOptions) func() {
			storageTestMust(t, os.WriteFile(homeJournalPath(c), []byte("synthetic pending journal"), 0600))
			return func() {}
		}, errStorageBootstrap},
	}
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			config := storageTestConfig(t)
			opt := storageTestOptions()
			release := v.prepare(t, config, &opt)
			defer release()
			outer := filepath.Dir(config)
			before := storageTestTree(t, outer)
			if ref, e := bootstrapStorageInstallation(t.Context(), config, "synthetic-original-home", opt, nil); !errors.Is(e, v.want) || ref != nil {
				t.Fatalf("want %v got %v", v.want, e)
			}
			if !reflect.DeepEqual(before, storageTestTree(t, outer)) {
				t.Fatal("refusal grew or repaired storage")
			}
		})
	}
	t.Run("missing-config", func(t *testing.T) {
		parent := t.TempDir()
		before := storageTestTree(t, parent)
		if ref, e := bootstrapStorageInstallation(t.Context(), filepath.Join(parent, "missing"), "home", storageTestOptions(), nil); e == nil || ref != nil {
			t.Fatal("created config base")
		}
		if !reflect.DeepEqual(before, storageTestTree(t, parent)) {
			t.Fatal("missing config mutated")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		config := storageTestConfig(t)
		before := storageTestTree(t, config)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, e := bootstrapStorageInstallation(ctx, config, "home", storageTestOptions(), nil); !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(before, storageTestTree(t, config)) {
			t.Fatal("cancelled bootstrap grew")
		}
	})
}

func TestStorageBootstrapBoundedAndPartial(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux bootstrap observation required")
	}
	t.Run("exact-plan-preserves-unknown", func(t *testing.T) {
		config := storageTestConfig(t)
		storageTestMust(t, os.WriteFile(filepath.Join(config, "foreign"), []byte("synthetic retained unknown"), 0600))
		storageTestMust(t, os.Mkdir(filepath.Join(config, "unknown-directory"), 0700))
		ref, e := bootstrapStorageInstallation(t.Context(), config, "synthetic-original-home", storageTestOptions(), nil)
		storageTestMust(t, e)
		defer ref.Close()
		names, e := os.ReadDir(config)
		storageTestMust(t, e)
		want := map[string]bool{".home-use.lock": true, "foreign": true, "unknown-directory": true, storageClaimName: true, storageRecordName: true, storageBootstrapName: true, storageUseName: true, storageIntentName: true, storageReadyName: true, storageAnchorName: true}
		if len(names) != len(want) {
			t.Fatalf("unplanned namespace: %v", names)
		}
		for _, n := range names {
			if !want[n.Name()] {
				t.Fatal(n.Name())
			}
		}
		b, e := os.ReadFile(filepath.Join(config, "foreign"))
		storageTestMust(t, e)
		if string(b) != "synthetic retained unknown" {
			t.Fatal("unknown bytes changed")
		}
		qnames, e := os.ReadDir(filepath.Join(config, storageAnchorName))
		storageTestMust(t, e)
		if len(qnames) != 6 {
			t.Fatal("unplanned Q namespace")
		}
		var actual int64
		for _, n := range qnames {
			info, e := n.Info()
			storageTestMust(t, e)
			actual += info.Size()
		}
		if actual != storagebudget.CoordinatorBytes {
			t.Fatalf("fixed Q lengths %d", actual)
		}
		snap, e := ref.cell.q.ConfigurationSnapshot(t.Context())
		storageTestMust(t, e)
		if !snap.Maintenance || snap.Revision != 1 || len(snap.Filesystems) != 0 {
			t.Fatal("bootstrap asserted enrollment/census")
		}
		before := storageTestTree(t, config)
		if next, e := bootstrapStorageInstallation(t.Context(), config, "synthetic-original-home", storageTestOptions(), nil); !errors.Is(e, errStorageBootstrap) || next != nil {
			t.Fatal("second bootstrap accepted")
		}
		if !reflect.DeepEqual(before, storageTestTree(t, config)) {
			t.Fatal("second bootstrap changed original")
		}
	})
	// These cases are handwritten syscall boundaries, not recorded outputs. A
	// valid final ready record after uncertain acknowledgement can be inspected
	// read-only, still in maintenance; it never authorizes InitializeHeld again.
	names := []string{storageClaimName, storageBootstrapName, storageUseName, "lock", storageIntentName, storageRecordName, storageReadyName}
	phases := []string{"create", "write", "sync", "close", "readback"}
	for _, name := range names {
		for _, phase := range phases {
			name, phase := name, phase
			t.Run(name+"-"+phase, func(t *testing.T) {
				config := storageTestConfig(t)
				if ref, e := bootstrapStorageInstallation(t.Context(), config, "synthetic-original-home", storageTestOptions(), &storageBootstrapFault{name, phase}); e == nil || ref != nil {
					t.Fatal("fault acknowledged success")
				}
				before := storageTestTree(t, config)
				ref, e := openStorageInstallation(t.Context(), config, "synthetic-original-home")
				if e == nil {
					if name != storageReadyName || phase == "create" || phase == "write" {
						ref.Close()
						t.Fatal("partial installation adopted")
					}
					s, se := ref.cell.q.ConfigurationSnapshot(t.Context())
					storageTestMust(t, se)
					if !s.Maintenance {
						t.Fatal("uncertain acknowledgement enabled growth")
					}
					storageTestMust(t, ref.Close())
				} else if ref != nil {
					t.Fatal("failed open leaked owner")
				}
				if !reflect.DeepEqual(before, storageTestTree(t, config)) {
					t.Fatal("read-only reopen changed partial state")
				}
				// Before first O_EXCL succeeds there is no durable claim. All other
				// interrupted cases must preserve the exact original claim and refuse retry.
				if name == storageClaimName && phase == "create" {
					return
				}
				if next, e := bootstrapStorageInstallation(t.Context(), config, "synthetic-original-home", storageTestOptions(), nil); !errors.Is(e, errStorageBootstrap) || next != nil {
					t.Fatal("partial bootstrap resumed/reinitialized")
				}
				if !reflect.DeepEqual(before, storageTestTree(t, config)) {
					t.Fatal("retry changed partial identity or fixed files")
				}
			})
		}
	}
	for _, v := range []storageBootstrapFault{{storageAnchorName, "mkdir"}, {storageAnchorName, "initialize"}, {storageAnchorName, "initialized"}, {storageAnchorName, "directory-sync"}, {storageAnchorName, "directory-close"}, {"config", "directory-sync"}, {"config", "directory-close"}, {"ready", "directory-sync"}, {"ready", "directory-close"}} {
		v := v
		t.Run(v.Name+"-"+v.Phase, func(t *testing.T) {
			config := storageTestConfig(t)
			if ref, e := bootstrapStorageInstallation(t.Context(), config, "synthetic-original-home", storageTestOptions(), &v); e == nil || ref != nil {
				t.Fatal("fault acknowledged success")
			}
			before := storageTestTree(t, config)
			ref, e := openStorageInstallation(t.Context(), config, "synthetic-original-home")
			if e == nil {
				if v.Name != "ready" {
					ref.Close()
					t.Fatal("missing ready marker accepted")
				}
				storageTestMust(t, ref.Close())
			} else if ref != nil {
				t.Fatal("failed open leaked owner")
			}
			if _, e = bootstrapStorageInstallation(t.Context(), config, "synthetic-original-home", storageTestOptions(), nil); !errors.Is(e, errStorageBootstrap) {
				t.Fatal("interrupted bootstrap reset identity")
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("interrupted state changed")
			}
		})
	}
}

func TestStorageInstallationIdentityMemberCurrentness(t *testing.T) {
	changes := []struct {
		name   string
		change func(*storageInstallationRecord)
	}{
		{"config-device", func(r *storageInstallationRecord) { r.Config.Device++ }},
		{"config-inode", func(r *storageInstallationRecord) { r.Config.Inode++ }},
		{"anchor-device", func(r *storageInstallationRecord) { r.Anchor.Device++ }},
		{"anchor-inode", func(r *storageInstallationRecord) { r.Anchor.Inode++ }},
		{"installation", func(r *storageInstallationRecord) { r.Installation[0] ^= 1 }},
		{"nonce", func(r *storageInstallationRecord) { r.Nonce[0] ^= 1 }},
		{"home-owner", func(r *storageInstallationRecord) { r.HomeOwner = "foreign" }},
	}
	for _, v := range changes {
		t.Run(v.name, func(t *testing.T) {
			ref, config := storageTestInstallation(t)
			record := ref.cell.record
			v.change(&record)
			b, e := json.Marshal(record)
			storageTestMust(t, e)
			b = append(b, '\n')
			storageTestMust(t, os.WriteFile(filepath.Join(config, storageRecordName), b, 0600))
			before := storageTestTree(t, config)
			if _, e = ref.acquire(); !errors.Is(e, errStorageCustody) {
				t.Fatalf("changed identity accepted: %v", e)
			}
			if other, e := openStorageInstallation(t.Context(), config, "synthetic-original-home"); e == nil || other != nil {
				t.Fatal("copied identity adopted")
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("identity refusal mutated storage")
			}
		})
	}
}

func TestStorageAnchorPinnedNameRaceSnapshots(t *testing.T) {
	config := storageTestConfig(t)
	o, e := storagePinOriginal(config)
	storageTestMust(t, e)
	defer o.close()
	parent := o.roots[len(o.roots)-2].root
	old := o.roots[len(o.roots)-1]
	// Exact snapshots model rename after named stat and before opened-root check.
	storageTestMust(t, parent.Rename("config", "config-old"))
	storageTestMust(t, parent.Mkdir("config", 0700))
	replacement, e := parent.OpenRoot("config")
	storageTestMust(t, e)
	defer replacement.Close()
	raced := storageNamedRoot{parent: parent, name: "config", root: replacement, identity: old.identity, private: true}
	before := storageTestTree(t, filepath.Dir(config))
	if e = raced.current(); !errors.Is(e, errStorageCustody) {
		t.Fatal("open/check race accepted replacement")
	}
	if e = old.current(); !errors.Is(e, errStorageCustody) {
		t.Fatal("held renamed-away root remained current")
	}
	if !reflect.DeepEqual(before, storageTestTree(t, filepath.Dir(config))) {
		t.Fatal("raced validation mutated either root")
	}
}

func TestStorageBootstrapPlatformObservation(t *testing.T) {
	config := storageTestConfig(t)
	root, e := os.OpenRoot(config)
	storageTestMust(t, e)
	defer root.Close()
	before := storageTestTree(t, config)
	obs, e := storagebudget.ObserveHeld(root, storagebudget.Filesystem{ID: storagebudget.ID{1}, Generation: 1})
	if runtime.GOOS == "linux" {
		storageTestMust(t, e)
		if !obs.Known || obs.At.IsZero() {
			t.Fatal("Linux descriptor observation unknown")
		}
	} else if !errors.Is(e, storagebudget.ErrUnsupported) || obs.Known {
		t.Fatalf("unsupported platform presented zero as known: %+v %v", obs, e)
	}
	if !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("observation wrote storage")
	}
}

func TestStorageBootstrapFiniteLeafPlan(t *testing.T) {
	config := storageTestConfig(t)
	root, e := os.OpenRoot(config)
	storageTestMust(t, e)
	defer root.Close()
	before := storageTestTree(t, config)
	for _, row := range []struct {
		name string
		size int64
	}{{"arbitrary", 0}, {"nested/leaf", 0}, {"../escape", 0}, {"checkpoint.a", storagebudget.CheckpointBytes}, {storageIntentName, storageIntentBytes - 1}, {storageReadyName, 0}, {storageRecordName, storageRecordLimit + 1}, {storageUseName, 1}} {
		if e := storageCreateControl(root, row.name, nil, row.size, nil); !errors.Is(e, storagebudget.ErrBounds) {
			t.Fatalf("unplanned leaf %q accepted: %v", row.name, e)
		}
	}
	if !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("invalid leaf plan mutated storage")
	}
}

func TestStorageInstallationWriteLifetimeGate(t *testing.T) {
	ref, _ := storageTestInstallation(t)
	use, e := ref.acquire()
	storageTestMust(t, e)
	independent, e := storageOpenFile(ref.cell.original.config, storageUseName, 0)
	storageTestMust(t, e)
	defer independent.file.Close()
	if e = lockPrivateData(independent.file, true); e == nil || !lockHeld(e) {
		t.Fatalf("maintenance bypassed writer lifetime: %v", e)
	}
	storageTestMust(t, ref.Close())
	storageTestMust(t, use.Current())
	if e = lockPrivateData(independent.file, true); e == nil || !lockHeld(e) {
		t.Fatalf("parent Close released active writer gate: %v", e)
	}
	storageTestMust(t, use.Close())
	storageTestMust(t, lockPrivateData(independent.file, true))
	storageTestMust(t, storageUnlock(independent.file))
}

// The snapshots model the exact rename/open/restore interval from review R1.
// No callback, sleep, installed process or probabilistic race is used.
func TestStorageInstallationUseGateSwapOpenRestore(t *testing.T) {
	for _, phase := range []string{"grant", "current"} {
		t.Run(phase, func(t *testing.T) {
			ref, config := storageTestInstallation(t)
			original := storageTestWitness(ref, storageUseName)
			path := filepath.Join(config, storageUseName)
			storageTestMust(t, os.Rename(path, path+"-original"))
			storageTestMust(t, os.WriteFile(path, nil, 0600))
			fresh, e := storageOpenFile(ref.cell.original.config, storageUseName, 0)
			storageTestMust(t, e)
			storageTestMust(t, os.Rename(path, path+"-replacement"))
			storageTestMust(t, os.Rename(path+"-original", path))
			// Every original name now passes. The fresh descriptor still denotes the
			// private, zero-byte replacement and must not become an admitted use.
			storageTestMust(t, ref.cell.current())
			oi, e := original.Stat()
			storageTestMust(t, e)
			fi, e := fresh.file.Stat()
			storageTestMust(t, e)
			if os.SameFile(oi, fi) {
				t.Fatal("fixture did not open a different gate inode")
			}
			storageTestMust(t, lockPrivateData(original, true))
			defer storageUnlock(original)
			if phase == "grant" {
				ref.cell.mu.Lock()
				beforeRefs := ref.cell.refs
				use, err := ref.cell.acquireUseGateLocked(fresh.file)
				afterRefs := ref.cell.refs
				ref.cell.mu.Unlock()
				if use != nil {
					_ = use.Close()
					t.Fatal("granted replacement SH gate while original EX was held")
				}
				if !errors.Is(err, errStorageCustody) || afterRefs != beforeRefs {
					t.Fatalf("replacement grant changed original custody: %v", err)
				}
				if _, e = fresh.file.Stat(); !errors.Is(e, os.ErrClosed) {
					t.Fatal("refusal did not close only fresh descriptor")
				}
				if use, e = ref.acquire(); !errors.Is(e, storagebudget.ErrBusy) || use != nil {
					if use != nil {
						use.Close()
					}
					t.Fatalf("original EX did not exclude genuine SH: %v", e)
				}
				storageTestMust(t, storageUnlock(original))
				use, e = ref.acquire()
				storageTestMust(t, e)
				if e = lockPrivateData(original, true); e == nil || !lockHeld(e) {
					use.Close()
					t.Fatalf("original EX coexisted with granted genuine use: %v", e)
				}
				storageTestMust(t, use.Close())
				storageTestMust(t, lockPrivateData(original, true))
			} else {
				defer fresh.file.Close()
				storageTestMust(t, lockPrivateData(fresh.file, false))
				// Hand-constructed invalid use probes Current without borrowing or closing
				// the cell. Its replacement descriptor is closed separately by this test.
				invalid := &storageInstallationUse{cell: ref.cell, gate: fresh.file}
				invalid.self = invalid
				if e = invalid.Current(); !errors.Is(e, errStorageCustody) {
					t.Fatalf("Current accepted replacement gate under restored original names: %v", e)
				}
			}
		})
	}
}

func TestStorageBootstrapClaimDirectoryFailureStopsDependencies(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux bootstrap observation required")
	}
	for _, phase := range []string{"directory-sync", "directory-close"} {
		t.Run(phase, func(t *testing.T) {
			config := storageTestConfig(t)
			ref, e := bootstrapStorageInstallation(t.Context(), config, "synthetic-original-home", storageTestOptions(), &storageBootstrapFault{Name: "claim", Phase: phase})
			if ref != nil {
				ref.Close()
				t.Fatal("uncertain claim-directory boundary returned owner")
			}
			if !errors.Is(e, errStorageBootstrap) {
				t.Fatalf("claim-directory fault did not stop bootstrap: %v", e)
			}
			names, e := os.ReadDir(config)
			storageTestMust(t, e)
			if len(names) != 2 || names[0].Name() != ".home-use.lock" || names[1].Name() != storageClaimName {
				t.Fatalf("dependent growth followed failed claim durability: %v", names)
			}
			root, e := os.OpenRoot(config)
			storageTestMust(t, e)
			defer root.Close()
			claim, witness, _, e := storageReadRecord(root, storageClaimName)
			storageTestMust(t, e)
			storageTestMust(t, witness.file.Close())
			if claim.Kind != "claim" || claim.Installation == (storagebudget.ID{}) || claim.Nonce == (storagebudget.ID{}) || claim.Anchor != (storageDirectoryIdentity{}) {
				t.Fatal("original identity not retained in claim")
			}
			before := storageTestTree(t, config)
			if next, e := openStorageInstallation(t.Context(), config, "synthetic-original-home"); e == nil || next != nil {
				if next != nil {
					next.Close()
				}
				t.Fatal("partial claim reopened as complete")
			}
			if next, e := bootstrapStorageInstallation(t.Context(), config, "synthetic-original-home", storageTestOptions(), nil); !errors.Is(e, errStorageBootstrap) || next != nil {
				if next != nil {
					next.Close()
				}
				t.Fatal("failed claim boundary resumed bootstrap")
			}
			if !reflect.DeepEqual(before, storageTestTree(t, config)) {
				t.Fatal("claim recovery refusal changed bytes or created dependencies")
			}
		})
	}
}
