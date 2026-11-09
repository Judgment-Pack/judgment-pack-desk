package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Source graphs come from the existing handwritten legacy fixtures. Q digests
// below are deliberately synthetic comparison operands, never native evidence.
func ownerV6Fixture(t *testing.T, version int, from bool) (storageOwnerV6Catalog, []byte, storageOwnerRecordV2, storageOwnerClaimV2) {
	t.Helper()
	var payload, frame []byte
	var r storageOwnerRecordV2
	var cl storageOwnerClaimV2
	var err error
	switch version {
	case 3:
		_, frame, r, cl = ownerV4Fixture(t, from)
		v, e := storageOwnerDecodeCatalogV3(frame, r, cl)
		if e != nil {
			t.Fatal(e)
		}
		payload, err = json.Marshal(v)
	case 4:
		var c storageOwnerV4Catalog
		c, _, r, cl = ownerV4Fixture(t, from)
		c = ownerV4Active(t, c, r, cl)
		frame = ownerV4Wire(t, c, r, cl)
		payload, err = json.Marshal(c)
	case 5:
		var c storageOwnerV5Catalog
		c, r, cl = ownerV5Fixture(t, from)
		c = ownerV5Active(t, c, r, cl)
		frame = ownerV5Wire(t, c, r, cl)
		payload, err = json.Marshal(c)
	default:
		t.Fatal("fixture version")
	}
	if err != nil {
		t.Fatal(err)
	}
	source, err := storageOwnerV6Source(payload, r, cl)
	if err != nil {
		t.Fatal(err)
	}
	_, before, err := source.definitions()
	if err != nil {
		t.Fatal(err)
	}
	after := before.Policy
	after.Workspace.Total.Bytes--
	p := storagebudget.PolicyTransition{Original: storagebudget.PolicyCommitment{Installation: r.Installation, Binding: r.QBinding, Revision: 7, Epoch: 1, Checkpoint: storagebudget.Digest{40}, Tickets: [2]storagebudget.Digest{{41}, {42}}, Manifests: storagebudget.Digest{43}}, Before: storageOwnerV6Policy(before.Policy), After: storageOwnerV6Policy(after)}
	inverse, err := storagebudget.EncodePolicyTransition(p)
	if err != nil {
		t.Fatal(err)
	}
	c := storageOwnerV6Catalog{Version: 6, TransitionKind: storageOwnerV6PolicyTransition, Sequence: source.Sequence + 1, Phase: "prepared", Transition: storagebudget.ID{90}, Source: append([]byte{}, payload...), EffectivePolicy: after, Overrides: []storageOwnerV6Override{}, PolicyTransition: inverse}
	copy(c.PreviousCatalogDigest[:], frame[32:64])
	c.BeforeEffectiveDigest, err = storageOwnerV6EffectiveDigest(before)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := storageOwnerV6Expand(before, after, c.Overrides)
	if err != nil {
		t.Fatal(err)
	}
	c.AfterEffectiveDigest, err = storageOwnerV6EffectiveDigest(effective)
	if err != nil {
		t.Fatal(err)
	}
	return c, frame, r, cl
}
func ownerV6Clone(c storageOwnerV6Catalog) storageOwnerV6Catalog {
	b, _ := json.Marshal(c)
	var out storageOwnerV6Catalog
	_ = json.Unmarshal(b, &out)
	return out
}
func ownerV6Rehash(raw []byte) {
	n := int(binary.LittleEndian.Uint32(raw[24:28]))
	h := sha256.New()
	h.Write(raw[:32])
	h.Write(raw[64 : 64+n])
	copy(raw[32:64], h.Sum(nil))
}
func TestStorageOwnerCatalogV6StrictCodec(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int
	}{{"source-v3", 3}, {"source-v4", 4}, {"source-v5", 5}} {
		t.Run(tc.name, func(t *testing.T) {
			c, prior, r, cl := ownerV6Fixture(t, tc.version, true)
			if err := storageOwnerV6Starts(prior, c, r, cl); err != nil {
				t.Fatal(err)
			}
			raw, err := storageOwnerV6Encode(c, r, cl)
			if err != nil {
				t.Fatal(err)
			}
			out, err := storageOwnerV6Decode(raw, r, cl)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(c, out) {
				t.Fatal("lossy V6/source round trip")
			}
			if _, err = storageOwnerDecodeCatalogV3(raw, r, cl); err == nil {
				t.Fatal("V3 accepted V6")
			}
			if _, err = storageOwnerV4Decode(raw, r, cl); err == nil {
				t.Fatal("V4 accepted V6")
			}
			if _, err = storageOwnerV5Decode(raw, r, cl); err == nil {
				t.Fatal("V5 accepted V6")
			}
			if _, err = storageOwnerV6Decode(prior, r, cl); err == nil {
				t.Fatal("V6 silently upgraded old frame")
			}
			out.Source[0] = 0
			out.PolicyTransition[0] = 0
			if _, err = storageOwnerV6Decode(raw, r, cl); err != nil {
				t.Fatal("decoder retained caller buffers")
			}
		})
	}
	for _, name := range []string{"magic", "version", "header", "reserved", "sequence", "checksum", "padding", "duplicate", "missing", "unknown", "null-overrides", "unsorted-fields", "bad-inverse", "age", "source-mutation", "unknown-variant", "policy-empty-inverse", "prepared-sequence-wrap"} {
		t.Run(name, func(t *testing.T) {
			c, _, r, cl := ownerV6Fixture(t, 5, true)
			b, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "duplicate":
				b = bytes.Replace(b, []byte(`"Version":6,`), []byte(`"Version":6,"Version":6,`), 1)
			case "missing":
				b = bytes.Replace(b, []byte(`"Version":6,`), nil, 1)
			case "unknown":
				b = bytes.Replace(b, []byte(`"Version":6,`), []byte(`"Version":6,"Unknown":0,`), 1)
			case "null-overrides":
				b = bytes.Replace(b, []byte(`"Overrides":[]`), []byte(`"Overrides":null`), 1)
			case "unsorted-fields":
				b = bytes.Replace(b, []byte(`"Version":6,"Sequence":`), []byte(`"Sequence":6,"Version":`), 1)
			case "bad-inverse":
				c.PolicyTransition[0] ^= 1
				b, _ = json.Marshal(c)
			case "age":
				c.EffectivePolicy.ObservationMaxAgeNanos++
				b, _ = json.Marshal(c)
			case "unknown-variant":
				c.TransitionKind = 3
				b, _ = json.Marshal(c)
			case "policy-empty-inverse":
				c.PolicyTransition = []byte{}
				b, _ = json.Marshal(c)
			case "prepared-sequence-wrap":
				c.Sequence = math.MaxUint64
				b, _ = json.Marshal(c)
			case "source-mutation":
				var source storageOwnerV5Catalog
				_ = json.Unmarshal(c.Source, &source)
				source.Workspaces[0].Definition.Limits.Total.Bytes--
				c.Source, _ = json.Marshal(source)
				b, _ = json.Marshal(c)
			}
			raw, err := storageOwnerV6Frame(b, c.Sequence)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "magic":
				raw[0] ^= 1
			case "version":
				binary.LittleEndian.PutUint32(raw[8:12], 5)
				ownerV6Rehash(raw)
			case "header":
				raw[12]++
				ownerV6Rehash(raw)
			case "reserved":
				raw[28] = 1
				ownerV6Rehash(raw)
			case "sequence":
				binary.LittleEndian.PutUint64(raw[16:24], c.Sequence+1)
				ownerV6Rehash(raw)
			case "checksum":
				raw[32] ^= 1
			case "padding":
				raw[len(raw)-1] = 1
			}
			if _, err = storageOwnerV6Decode(raw, r, cl); err == nil {
				t.Fatal("accepted malformed V6")
			}
		})
	}
}
func TestStorageOwnerCatalogV6StickyOverrides(t *testing.T) {
	c, _, r, cl := ownerV6Fixture(t, 5, true)
	source, err := c.sourceDefinitions(r, cl)
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte{}, c.Source...)
	w := source.Workspaces[0]
	t.Run("insert-replace-remove-and-detached", func(t *testing.T) {
		a := w.Limits
		a.Total.Bytes--
		rows, err := storageOwnerV6CanonicalSet(source, []storageOwnerV6Override{}, w.ID, w.Generation, a)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Limits != a {
			t.Fatal("missing sticky row")
		}
		b := a
		b.Total.Bytes--
		next, err := storageOwnerV6CanonicalSet(source, rows, w.ID, w.Generation, b)
		if err != nil {
			t.Fatal(err)
		}
		if len(next) != 1 || next[0].Limits != b || rows[0].Limits != a {
			t.Fatal("replace mutated prior")
		}
		defaults := source.Policy
		defaults.Workspace = a
		expanded, err := storageOwnerV6Expand(source, defaults, next)
		if err != nil {
			t.Fatal(err)
		}
		if expanded.Workspaces[0].Limits != b || source.Workspaces[0].Limits != w.Limits {
			t.Fatal("default rewrote explicit/source limit")
		}
		removed, err := storageOwnerV6CanonicalSet(source, next, w.ID, w.Generation, w.Limits)
		if err != nil {
			t.Fatal(err)
		}
		expanded, err = storageOwnerV6Expand(source, defaults, removed)
		if err != nil {
			t.Fatal(err)
		}
		if len(removed) != 0 || expanded.Workspaces[0].Limits != w.Limits {
			t.Fatal("baseline became inheritance")
		}
		expanded.Workspaces[0].Limits.Total.Bytes--
		if source.Workspaces[0] != w || !bytes.Equal(original, c.Source) {
			t.Fatal("effective alias changed immutable source")
		}
	})
	for _, name := range []string{"duplicate", "unsorted", "unknown", "stale-generation", "redundant", "invalid", "nil"} {
		t.Run(name, func(t *testing.T) {
			localSource := source
			a := w.Limits
			a.Total.Bytes--
			row := storageOwnerV6Override{w.ID, w.Generation, a}
			rows := []storageOwnerV6Override{row}
			switch name {
			case "duplicate":
				rows = append(rows, row)
			case "unsorted":
				other := row
				other.ID = storageOwnerV5MaxID()
				localSource.Workspaces = append(append([]storagebudget.RegisteredWorkspace{}, source.Workspaces...), storagebudget.RegisteredWorkspace{ID: other.ID, Generation: other.Generation, Limits: w.Limits})
				rows = []storageOwnerV6Override{other, row}
			case "unknown":
				rows[0].ID = storagebudget.ID{255}
			case "stale-generation":
				rows[0].Generation++
			case "redundant":
				rows[0].Limits = w.Limits
			case "invalid":
				rows[0].Limits.Category[0].Files = 0
			case "nil":
				rows = nil
			}
			if _, err := storageOwnerV6Expand(localSource, localSource.Policy, rows); err == nil {
				t.Fatal("accepted noncanonical sparse table")
			}
		})
	}
}
func TestStorageOwnerCatalogV6PolicyRelations(t *testing.T) {
	t.Run("commit-abort-and-source-retention", func(t *testing.T) {
		prepared, _, r, cl := ownerV6Fixture(t, 5, true)
		final := ownerV6Clone(prepared)
		final.Phase = "active"
		final.Sequence++
		d, err := storageOwnerV6Digest(prepared)
		if err != nil {
			t.Fatal(err)
		}
		final.PreviousCatalogDigest = d
		if err = storageOwnerV6Publication(prepared, final, r, cl); err != nil {
			t.Fatal(err)
		}
		abort := ownerV6Clone(final)
		abort.Phase = "aborted"
		p, err := storagebudget.DecodePolicyTransition(prepared.PolicyTransition)
		if err != nil {
			t.Fatal(err)
		}
		abort.EffectivePolicy = storageOwnerV6HostPolicy(p.Before)
		if err = storageOwnerV6Publication(prepared, abort, r, cl); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(final.Source, prepared.Source) || !bytes.Equal(abort.Source, prepared.Source) {
			t.Fatal("rewrote retained lineage/source")
		}
		final.Transition[0]++
		if err = storageOwnerV6Publication(prepared, final, r, cl); err == nil {
			t.Fatal("foreign operation publication")
		}
	})
	t.Run("repeated-effective-before-not-baseline", func(t *testing.T) {
		c, _, r, cl := ownerV6Fixture(t, 5, true)
		source, err := c.sourceDefinitions(r, cl)
		if err != nil {
			t.Fatal(err)
		}
		w := source.Workspaces[0]
		old := w.Limits
		old.Total.Bytes--
		now := old
		now.Total.Bytes--
		c.Overrides = []storageOwnerV6Override{{w.ID, w.Generation, now}}
		p, err := storagebudget.DecodePolicyTransition(c.PolicyTransition)
		if err != nil {
			t.Fatal(err)
		}
		p.Workspace = w.ID
		p.WorkspaceGeneration = w.Generation
		p.BeforeLimits = old
		p.AfterLimits = now
		c.PolicyTransition, err = storagebudget.EncodePolicyTransition(p)
		if err != nil {
			t.Fatal(err)
		}
		before, err := storageOwnerV6Expand(source, storageOwnerV6HostPolicy(p.Before), []storageOwnerV6Override{{w.ID, w.Generation, old}})
		if err != nil {
			t.Fatal(err)
		}
		after, err := storageOwnerV6Expand(source, c.EffectivePolicy, c.Overrides)
		if err != nil {
			t.Fatal(err)
		}
		c.BeforeEffectiveDigest, _ = storageOwnerV6EffectiveDigest(before)
		c.AfterEffectiveDigest, _ = storageOwnerV6EffectiveDigest(after)
		if err = c.validatePolicyRelation(r, cl); err != nil {
			t.Fatal(err)
		}
		p.BeforeLimits = w.Limits
		c.PolicyTransition, err = storagebudget.EncodePolicyTransition(p)
		if err != nil {
			t.Fatal(err)
		}
		if err = c.validatePolicyRelation(r, cl); err == nil {
			t.Fatal("replaced effective before with admission baseline")
		}
	})
	t.Run("foreign-before-and-overflow", func(t *testing.T) {
		c, prior, r, cl := ownerV6Fixture(t, 5, false)
		c.BeforeEffectiveDigest[0] ^= 1
		if err := storageOwnerV6Starts(prior, c, r, cl); err == nil {
			t.Fatal("wrong predecessor")
		}
		c, _, r, cl = ownerV6Fixture(t, 5, false)
		c.Sequence = math.MaxUint64
		next := ownerV6Clone(c)
		next.Sequence = 1
		if err := storageOwnerV6Follows(c, next, r, cl); err == nil {
			t.Fatal("wrapped catalog revision")
		}
	})
}
func TestStorageOwnerCatalogV6FutureClosure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int
		from    bool
	}{{"v3", 3, false}, {"v4-CF", 4, true}, {"v5-C", 5, false}, {"v5-CSF", 5, true}} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, r, cl := ownerV6Fixture(t, tc.version, tc.from)
			size, err := storageOwnerV6ClosureSize(c)
			if err != nil {
				t.Fatal(err)
			}
			actual, _ := json.Marshal(c)
			if size <= len(actual) || size > storageOwnerCatalogBytes-64 {
				t.Fatal("missing future override/receipt room")
			}
			if _, err = storageOwnerV6Encode(c, r, cl); err != nil {
				t.Fatal(err)
			}
			if tc.version == 5 {
				var source storageOwnerV5Catalog
				_ = json.Unmarshal(c.Source, &source)
				tree, err := storageOwnerV5ClosureTree(source)
				if err != nil {
					t.Fatal(err)
				}
				if len(tree.LineageCompletions) != len(source.LineagePlans) || tc.from && (len(tree.LineageStampCompletions) != len(source.LineagePlans) || len(tree.LineageContinuations) != len(source.LineagePlans)) {
					t.Fatal("missing pledged C/S/F shadows")
				}
			}
		})
	}
	// This independent maximum-width list shape exercises allocation/count/size
	// bounds only. It is not a semantically registered source graph.
	t.Run("bounded-maximum-workspace-shape", func(t *testing.T) {
		c, _, _, _ := ownerV6Fixture(t, 5, true)
		var source storageOwnerV5Catalog
		_ = json.Unmarshal(c.Source, &source)
		one, err := storageOwnerV6ClosureSize(c)
		if err != nil {
			t.Fatal(err)
		}
		template := source.Workspaces[0]
		source.Workspaces = nil
		for i := 0; i < storagebudget.MaxWorkspaces; i++ {
			w := template
			w.Definition.ID = storagebudget.ID{}
			binary.BigEndian.PutUint64(w.Definition.ID[8:], uint64(i+1))
			source.Workspaces = append(source.Workspaces, w)
		}
		c.Source, _ = json.Marshal(source)
		many, err := storageOwnerV6ClosureSize(c)
		if err != nil {
			t.Fatal(err)
		}
		if many <= one+storagebudget.MaxWorkspaces*264 {
			t.Fatal("did not reserve full widest table plus source")
		}
		source.Workspaces = append(source.Workspaces, template)
		c.Source, _ = json.Marshal(source)
		if _, err = storageOwnerV6ClosureSize(c); err == nil {
			t.Fatal("workspace cap expanded")
		}
	})
	t.Run("exact-payload-bound", func(t *testing.T) {
		if _, err := storageOwnerV6Frame(make([]byte, storageOwnerCatalogBytes-64), 1); err != nil {
			t.Fatal(err)
		}
		if _, err := storageOwnerV6Frame(make([]byte, storageOwnerCatalogBytes-63), 1); err == nil {
			t.Fatal("raised payload cap")
		}
	})
}

func TestStorageOwnerCatalogV6PairClassification(t *testing.T) {
	for _, name := range []string{"legacy-prepared", "committed", "aborted", "next-prepared", "unrelated-peer"} {
		t.Run(name, func(t *testing.T) {
			prepared, legacy, r, cl := ownerV6Fixture(t, 5, true)
			praw, err := storageOwnerV6Encode(prepared, r, cl)
			if err != nil {
				t.Fatal(err)
			}
			if name == "legacy-prepared" {
				pair, err := storageOwnerV6Classify(praw, legacy, r, cl)
				if err != nil || pair.Kind != storageOwnerV6PreparedFromLegacy {
					t.Fatal("legacy prefix", err)
				}
				return
			}
			final := ownerV6Clone(prepared)
			final.Sequence++
			final.Phase = "active"
			final.PreviousCatalogDigest, err = storageOwnerV6Digest(prepared)
			if err != nil {
				t.Fatal(err)
			}
			if name == "aborted" {
				final.Phase = "aborted"
				p, err := storagebudget.DecodePolicyTransition(prepared.PolicyTransition)
				if err != nil {
					t.Fatal(err)
				}
				final.EffectivePolicy = storageOwnerV6HostPolicy(p.Before)
			}
			fraw, err := storageOwnerV6Encode(final, r, cl)
			if err != nil {
				t.Fatal(err)
			}
			if name == "next-prepared" {
				next := ownerV6Clone(prepared)
				next.Sequence = final.Sequence + 1
				next.Transition = storagebudget.ID{91}
				next.PreviousCatalogDigest, err = storageOwnerV6Digest(final)
				if err != nil {
					t.Fatal(err)
				}
				p, err := storagebudget.DecodePolicyTransition(next.PolicyTransition)
				if err != nil {
					t.Fatal(err)
				}
				p.Original.Revision += 3
				p.Original.Epoch++
				p.Original.Checkpoint[0]++
				p.Before = p.After
				p.After.Workspace.Total.Bytes--
				next.EffectivePolicy = storageOwnerV6HostPolicy(p.After)
				next.PolicyTransition, err = storagebudget.EncodePolicyTransition(p)
				if err != nil {
					t.Fatal(err)
				}
				d, err := final.effectiveDefinitions(r, cl)
				if err != nil {
					t.Fatal(err)
				}
				next.BeforeEffectiveDigest, err = storageOwnerV6EffectiveDigest(d)
				if err != nil {
					t.Fatal(err)
				}
				d.Policy = next.EffectivePolicy
				next.AfterEffectiveDigest, err = storageOwnerV6EffectiveDigest(d)
				if err != nil {
					t.Fatal(err)
				}
				nraw, err := storageOwnerV6Encode(next, r, cl)
				if err != nil {
					t.Fatal(err)
				}
				for _, banks := range [][2][]byte{{nraw, fraw}, {fraw, nraw}} {
					pair, err := storageOwnerV6Classify(banks[0], banks[1], r, cl)
					if err != nil || pair.Kind != storageOwnerV6PreparedFromV6 {
						t.Fatal("next prefix in either physical bank order", err)
					}
				}
				return
			}
			if name == "unrelated-peer" {
				prepared.Transition[0]++
				praw, err = storageOwnerV6Encode(prepared, r, cl)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = storageOwnerV6Classify(praw, fraw, r, cl); err == nil {
					t.Fatal("accepted unrelated peer")
				}
				return
			}
			pair, err := storageOwnerV6Classify(fraw, praw, r, cl)
			want := storageOwnerV6Committed
			if name == "aborted" {
				want = storageOwnerV6Aborted
			}
			if err != nil || pair.Kind != want {
				t.Fatal("final prefix", err)
			}
		})
	}
}

func ownerV6WrapSource(t *testing.T, previous storageOwnerV6Catalog, source storageOwnerV5Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) storageOwnerV6Catalog {
	t.Helper()
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	c := ownerV6Clone(previous)
	c.Sequence++
	c.TransitionKind = storageOwnerV6SourceTransition
	c.Phase = source.Phase
	c.Transition = source.Transition
	c.Source = raw
	c.PolicyTransition = []byte{}
	c.PreviousCatalogDigest, err = storageOwnerV6Digest(previous)
	if err != nil {
		t.Fatal(err)
	}
	before, after, err := source.old().definitions()
	if err != nil {
		t.Fatal(err)
	}
	before, err = storageOwnerV6Expand(before, c.EffectivePolicy, c.Overrides)
	if err != nil {
		t.Fatal(err)
	}
	after, err = storageOwnerV6Expand(after, c.EffectivePolicy, c.Overrides)
	if err != nil {
		t.Fatal(err)
	}
	c.BeforeEffectiveDigest, err = storageOwnerV6EffectiveDigest(before)
	if err != nil {
		t.Fatal(err)
	}
	c.AfterEffectiveDigest, err = storageOwnerV6EffectiveDigest(after)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func ownerV6CommitSource(t *testing.T, previous, prepared storageOwnerV6Catalog, source storageOwnerV5Catalog, r storageOwnerRecordV2, cl storageOwnerClaimV2) storageOwnerV6Catalog {
	t.Helper()
	if err := storageOwnerV6Follows(previous, prepared, r, cl); err != nil {
		t.Fatal(err)
	}
	if _, err := storageOwnerV6Encode(prepared, r, cl); err != nil {
		t.Fatal(err)
	}
	active := ownerV5Active(t, source, r, cl)
	final := ownerV6WrapSource(t, prepared, active, r, cl)
	if err := storageOwnerV6Publication(prepared, final, r, cl); err != nil {
		t.Fatal(err)
	}
	if _, err := storageOwnerV6Encode(final, r, cl); err != nil {
		t.Fatal(err)
	}
	return final
}
func TestStorageOwnerCatalogV6SourceContinuity(t *testing.T) {
	// A deliberate override equals the newly selected default, but remains sticky.
	prepared, _, r, cl := ownerV6Fixture(t, 5, true)
	var baseline storageOwnerV5Catalog
	_ = json.Unmarshal(prepared.Source, &baseline)
	w := baseline.Workspaces[0].Definition
	p, err := storagebudget.DecodePolicyTransition(prepared.PolicyTransition)
	if err != nil {
		t.Fatal(err)
	}
	p.Workspace = w.ID
	p.WorkspaceGeneration = w.Generation
	p.BeforeLimits = w.Limits
	p.AfterLimits = w.Limits
	p.AfterLimits.Total.Bytes--
	prepared.PolicyTransition, err = storagebudget.EncodePolicyTransition(p)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Overrides = []storageOwnerV6Override{{w.ID, w.Generation, p.AfterLimits}}
	d, err := prepared.effectiveDefinitions(r, cl)
	if err != nil {
		t.Fatal(err)
	}
	prepared.AfterEffectiveDigest, err = storageOwnerV6EffectiveDigest(d)
	if err != nil {
		t.Fatal(err)
	}
	active := ownerV6Clone(prepared)
	active.Phase = "active"
	active.Sequence++
	active.PreviousCatalogDigest, err = storageOwnerV6Digest(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err = storageOwnerV6Publication(prepared, active, r, cl); err != nil {
		t.Fatal(err)
	}
	enrollment := ownerV5Next(t, baseline, storageOwnerV5Enrollment, storagebudget.ID{}, r, cl)
	old := baseline.old()
	ownerCatalogBeforeAll(&old)
	enrollment.BeforeDefinitionIDs = old.BeforeDefinitionIDs
	newWorkspace := storageOwnerWorkspace{Definition: storagebudget.RegisteredWorkspace{ID: storageOwnerV5MaxID(), Generation: 1, Limits: active.EffectivePolicy.Workspace}, OwnerKind: 3, OwnerKey: []byte("future-workspace"), HistoryIdentity: []byte("desk:" + strings.Repeat("b", 32))}
	enrollment.Workspaces = append(enrollment.Workspaces, newWorkspace)
	sort.Slice(enrollment.Workspaces, func(i, j int) bool {
		return bytes.Compare(enrollment.Workspaces[i].Definition.ID[:], enrollment.Workspaces[j].Definition.ID[:]) < 0
	})
	before, after, err := enrollment.old().definitions()
	if err != nil {
		t.Fatal(err)
	}
	enrollment.BeforeDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", before)
	if err != nil {
		t.Fatal(err)
	}
	enrollment.AfterDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", after)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := ownerV6WrapSource(t, active, enrollment, r, cl)
	t.Run("policy-then-enrollment-CSF-then-policy", func(t *testing.T) {
		current := ownerV6CommitSource(t, active, wrapped, enrollment, r, cl)
		var source storageOwnerV5Catalog
		_ = json.Unmarshal(current.Source, &source)
		originalRows := append([]storageOwnerV6Override{}, current.Overrides...)
		c := ownerV5Complete(t, source, r, cl)
		all := c.old()
		ownerCatalogBeforeAll(&all)
		c.BeforeDefinitionIDs = all.BeforeDefinitionIDs
		c.BeforeDefinitionsDigest = c.AfterDefinitionsDigest
		next := ownerV6WrapSource(t, current, c, r, cl)
		current = ownerV6CommitSource(t, current, next, c, r, cl)
		_ = json.Unmarshal(current.Source, &source)
		s := ownerV5Stamp(t, source, storageOwnerV5Moved, r, cl)
		next = ownerV6WrapSource(t, current, s, r, cl)
		current = ownerV6CommitSource(t, current, next, s, r, cl)
		_ = json.Unmarshal(current.Source, &source)
		f := ownerV5Continue(t, source, r, cl)
		next = ownerV6WrapSource(t, current, f, r, cl)
		current = ownerV6CommitSource(t, current, next, f, r, cl)
		_ = json.Unmarshal(current.Source, &source)
		if !reflect.DeepEqual(current.Overrides, originalRows) || source.Workspaces[0].Definition.Limits == p.AfterLimits && source.Workspaces[0].Definition.ID == w.ID {
			t.Fatal("rewrote source baseline or sticky row")
		}
		next = ownerV6Clone(current)
		next.TransitionKind = storageOwnerV6PolicyTransition
		next.Phase = "prepared"
		next.Sequence++
		next.Transition = storagebudget.ID{250}
		next.PreviousCatalogDigest, err = storageOwnerV6Digest(current)
		if err != nil {
			t.Fatal(err)
		}
		q := p
		q.Original.Revision += 3
		q.Original.Epoch++
		q.Original.Checkpoint[0]++
		q.Before = storageOwnerV6Policy(current.EffectivePolicy)
		q.After = q.Before
		q.After.Workspace.Total.Bytes--
		q.Workspace = storagebudget.ID{}
		q.WorkspaceGeneration = 0
		q.BeforeLimits = storagebudget.Limits{}
		q.AfterLimits = storagebudget.Limits{}
		next.EffectivePolicy = storageOwnerV6HostPolicy(q.After)
		next.PolicyTransition, err = storagebudget.EncodePolicyTransition(q)
		if err != nil {
			t.Fatal(err)
		}
		currentDefinitions, err := current.effectiveDefinitions(r, cl)
		if err != nil {
			t.Fatal(err)
		}
		next.BeforeEffectiveDigest, err = storageOwnerV6EffectiveDigest(currentDefinitions)
		if err != nil {
			t.Fatal(err)
		}
		afterDefinitions, err := next.effectiveDefinitions(r, cl)
		if err != nil {
			t.Fatal(err)
		}
		next.AfterEffectiveDigest, err = storageOwnerV6EffectiveDigest(afterDefinitions)
		if err != nil {
			t.Fatal(err)
		}
		if err = storageOwnerV6Follows(current, next, r, cl); err != nil {
			t.Fatal(err)
		}
		if _, err = storageOwnerV6Encode(next, r, cl); err != nil {
			t.Fatal(err)
		}
		for _, row := range afterDefinitions.Workspaces {
			if row.ID == w.ID && row.Limits != p.AfterLimits || row.ID == newWorkspace.Definition.ID && row.Limits != newWorkspace.Definition.Limits {
				t.Fatal("later default changed existing sticky limit")
			}
		}
	})
	for _, name := range []string{"mixed-policy", "mixed-inverse", "missing-override", "stale-override", "rekeyed-override", "changed-source-baseline", "wrong-new-baseline", "reversed-source-link", "source-aborted"} {
		t.Run(name, func(t *testing.T) {
			bad := ownerV6Clone(wrapped)
			switch name {
			case "source-aborted":
				bad.Phase = "aborted"
			case "mixed-policy":
				bad.EffectivePolicy.Workspace.Total.Bytes--
			case "mixed-inverse":
				bad.PolicyTransition = append([]byte{}, prepared.PolicyTransition...)
			case "missing-override":
				bad.Overrides = []storageOwnerV6Override{}
			case "stale-override":
				bad.Overrides[0].Generation++
			case "rekeyed-override":
				bad.Overrides[0].ID = storagebudget.ID{251}
			case "changed-source-baseline":
				var v storageOwnerV5Catalog
				_ = json.Unmarshal(bad.Source, &v)
				for i := range v.Workspaces {
					if v.Workspaces[i].Definition.ID == w.ID {
						v.Workspaces[i].Definition.Limits.Total.Bytes--
					}
				}
				bad.Source, _ = json.Marshal(v)
			case "wrong-new-baseline":
				var v storageOwnerV5Catalog
				_ = json.Unmarshal(bad.Source, &v)
				for i := range v.Workspaces {
					if v.Workspaces[i].Definition.ID == newWorkspace.Definition.ID {
						v.Workspaces[i].Definition.Limits.Total.Bytes--
					}
				}
				_, definitions, e := v.old().definitions()
				if e != nil {
					t.Fatal(e)
				}
				v.AfterDefinitionsDigest, e = storageOwnerDigest("desk-storage-owner-definitions-v2", definitions)
				if e != nil {
					t.Fatal(e)
				}
				if e = v.validate(r, cl); e != nil {
					t.Fatal("vector must remain a valid immutable legacy enrollment", e)
				}
				bad = ownerV6WrapSource(t, active, v, r, cl)
			case "reversed-source-link":
				if err := storageOwnerV6SourceAdjacent(wrapped.Source, active.Source, r, cl); err == nil {
					t.Fatal("accepted reversed legacy link")
				}
				return
			}
			if err := storageOwnerV6Follows(active, bad, r, cl); err == nil {
				t.Fatal("accepted mixed/foreign source successor")
			}
		})
	}
}

func TestStorageOwnerCatalogV6LegacySourceRelations(t *testing.T) {
	for _, name := range []string{"v3-enrollment", "v3-v4-original-bridge", "v4-completion", "v5-no-upgrade"} {
		t.Run(name, func(t *testing.T) {
			switch name {
			case "v3-enrollment":
				_, low, r, cl := ownerV4Fixture(t, true)
				v, err := storageOwnerDecodeCatalogV3(low, r, cl)
				if err != nil {
					t.Fatal(err)
				}
				source := storageOwnerCatalogV2(v)
				before, _ := json.Marshal(source)
				source.Sequence++
				source.Phase = "prepared"
				source.Transition = storagebudget.ID{200}
				copy(source.PreviousCatalogDigest[:], low[32:64])
				ownerCatalogBeforeAll(&source)
				b, a, err := source.definitions()
				if err != nil {
					t.Fatal(err)
				}
				source.BeforeDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", b)
				if err != nil {
					t.Fatal(err)
				}
				source.AfterDefinitionsDigest, err = storageOwnerDigest("desk-storage-owner-definitions-v2", a)
				if err != nil {
					t.Fatal(err)
				}
				after, _ := json.Marshal(source)
				if err = storageOwnerV6SourceAdjacent(before, after, r, cl); err != nil {
					t.Fatal(err)
				}
			case "v3-v4-original-bridge":
				high, low, r, cl := ownerV4Fixture(t, true)
				v, err := storageOwnerDecodeCatalogV3(low, r, cl)
				if err != nil {
					t.Fatal(err)
				}
				before, _ := json.Marshal(v)
				after, _ := json.Marshal(high)
				if err = storageOwnerV6SourceAdjacent(before, after, r, cl); err != nil {
					t.Fatal(err)
				}
				high.UpgradeOrigin.FrameDigest[0] ^= 1
				after, _ = json.Marshal(high)
				if err = storageOwnerV6SourceAdjacent(before, after, r, cl); err == nil {
					t.Fatal("relaxed original bridge relation")
				}
			case "v4-completion":
				source, _, r, cl := ownerV4Fixture(t, true)
				source = ownerV4Active(t, source, r, cl)
				before, _ := json.Marshal(source)
				source = ownerV4Complete(t, source, r, cl)
				after, _ := json.Marshal(source)
				if err := storageOwnerV6SourceAdjacent(before, after, r, cl); err != nil {
					t.Fatal(err)
				}
			case "v5-no-upgrade":
				source, r, cl := ownerV5Fixture(t, true)
				source = ownerV5Active(t, source, r, cl)
				before, _ := json.Marshal(source)
				source.TransitionKind = 5
				after, _ := json.Marshal(source)
				if err := storageOwnerV6SourceAdjacent(before, after, r, cl); err == nil {
					t.Fatal("invented V5 upgrade")
				}
			}
		})
	}
}

func TestStorageOwnerCatalogV6ClosureMaterial(t *testing.T) {
	requireStorageLine(t)
	c, _, r, cl := ownerV6Fixture(t, 5, true)
	source, err := c.sourceDefinitions(r, cl)
	if err != nil {
		t.Fatal(err)
	}
	original := ownerV6Clone(c)
	tree, err := storageOwnerV6ClosureTree(c)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, original) {
		t.Fatal("sizing changed caller source")
	}
	if len(tree.Overrides) != len(source.Workspaces) || tree.Sequence != math.MaxUint64 || tree.Phase != "prepared" || len(tree.PolicyTransition) != 2048 {
		t.Fatal("future envelope/table bound")
	}
	maximum := storagebudget.Vector{Bytes: 1 << 60, Files: 1 << 40, Directories: 1 << 40}
	for i, row := range tree.Overrides {
		if row.ID != source.Workspaces[i].ID || row.Generation != source.Workspaces[i].Generation || row.Limits.Total != maximum {
			t.Fatal("future current override lacks exact registration/full widths")
		}
		for _, v := range row.Limits.Category {
			if v != maximum {
				t.Fatal("future category width")
			}
		}
	}
	for _, l := range []storagebudget.Limits{tree.EffectivePolicy.Installation, tree.EffectivePolicy.Workspace} {
		if l.Total != maximum {
			t.Fatal("future editable policy width")
		}
		for _, v := range l.Category {
			if v != maximum {
				t.Fatal("future editable policy category width")
			}
		}
	}
	var before, after storageOwnerV5Catalog
	_ = json.Unmarshal(c.Source, &before)
	_ = json.Unmarshal(tree.Source, &after)
	if !reflect.DeepEqual(storageOwnerV4OldProjection(before.old()), storageOwnerV4OldProjection(after.old())) || len(after.LineageCompletions) != 1 || len(after.LineageStampCompletions) != 1 || len(after.LineageContinuations) != 1 || len(after.BeforeDefinitionIDs.Workspaces) != len(before.Workspaces) {
		t.Fatal("source mutation or missing full C/S/F closure")
	}
	b, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	size, err := storageOwnerV6ClosureSize(c)
	if err != nil || size != len(b) || !storageOwnerV6Preflight(b) {
		t.Fatal("size differs from exact future codec", err)
	}
}

// Handwritten outcome kinds exercise the external strict pair classifier in
// both physical bank orders. Equal policies do not merge the two abort kinds.
func TestStorageOwnerCatalogV6OutcomePairKinds(t *testing.T) {
	for _, order := range []string{"forward", "reverse"} {
		for _, outcome := range []struct {
			name, phase string
			kind        storageOwnerV6PairKind
		}{
			{"prepared", "prepared", storageOwnerV6PreparedFromLegacy},
			{"committed", "active", storageOwnerV6Committed},
			{"fenced-aborted", "aborted", storageOwnerV6Aborted},
			{"unfenced-aborted", "aborted-unfenced", storageOwnerV6AbortedUnfenced},
		} {
			t.Run(order+"/"+outcome.name, func(t *testing.T) {
				prepared, legacy, record, claim := ownerV6Fixture(t, 5, true)
				praw, err := storageOwnerV6Encode(prepared, record, claim)
				storageTestMust(t, err)
				left, right := praw, legacy
				if outcome.phase != "prepared" {
					final := ownerV6Clone(prepared)
					final.Sequence++
					final.Phase = outcome.phase
					final.PreviousCatalogDigest, err = storageOwnerV6Digest(prepared)
					storageTestMust(t, err)
					if outcome.phase != "active" {
						inverse, e := storagebudget.DecodePolicyTransition(prepared.PolicyTransition)
						storageTestMust(t, e)
						final.EffectivePolicy = storageOwnerV6HostPolicy(inverse.Before)
					}
					left, err = storageOwnerV6Encode(final, record, claim)
					storageTestMust(t, err)
					right = praw
				}
				if order == "reverse" {
					left, right = right, left
				}
				pair, err := storageOwnerV6Classify(left, right, record, claim)
				if err != nil || pair.Kind != outcome.kind {
					t.Fatal("distinct exact pair outcome lost", pair.Kind, err)
				}
				bad := append([]byte{}, left...)
				bad[0] ^= 1
				if _, e := storageOwnerV6Classify(bad, right, record, claim); e == nil {
					t.Fatal("malformed physical pair accepted")
				}
				foreign := claim
				foreign.Installation[0] ^= 1
				if _, e := storageOwnerV6Classify(left, right, record, foreign); e == nil {
					t.Fatal("foreign original owner accepted")
				}
				if outcome.phase != "prepared" {
					badPrepared := ownerV6Clone(prepared)
					badPrepared.Transition[0] ^= 1
					badRaw, e := storageOwnerV6Encode(badPrepared, record, claim)
					storageTestMust(t, e)
					final := left
					if order == "reverse" {
						final = right
					}
					if _, e = storageOwnerV6Classify(final, badRaw, record, claim); e == nil {
						t.Fatal("unrelated prepared/outcome pair accepted")
					}
				}
			})
		}
	}
}

// Both closed V6 preflights must admit their four declared phase literals,
// including the longest legal closure shadow, before any DTO allocation.
// This does not expand any nested legacy Source phase contract.
func TestStorageOwnerCatalogV6PhasePreflight(t *testing.T) {
	ordinary, _, _, _ := ownerV6Fixture(t, 5, true)
	ordinaryRaw, err := json.Marshal(ordinary)
	storageTestMust(t, err)
	packed, _, _, _, _ := policyPackedFixture(t)
	packedRaw, err := json.Marshal(packed)
	storageTestMust(t, err)
	for _, mode := range []struct {
		name      string
		raw       []byte
		preflight func([]byte) bool
	}{{"ordinary", ordinaryRaw, storageOwnerV6Preflight}, {"packed", packedRaw, storageOwnerV6PackedPreflight}} {
		phase := []byte(`"Phase":"prepared"`)
		if bytes.Count(mode.raw, phase) != 1 {
			t.Fatal("handwritten phase operand not unique")
		}
		for _, row := range []struct {
			name, replacement string
			allowed           bool
		}{
			{"prepared", `"Phase":"prepared"`, true},
			{"active", `"Phase":"active"`, true},
			{"aborted", `"Phase":"aborted"`, true},
			{"aborted-unfenced", `"Phase":"aborted-unfenced"`, true},
			{"unknown", `"Phase":"future"`, false},
			{"oversized", `"Phase":"aborted-unfenced-more"`, false},
			{"duplicate", `"Phase":"prepared","Phase":"prepared"`, false},
			{"malformed", `"Phase":null`, false},
			{"truncated", `"Phase":"aborted-unfenced`, false},
		} {
			t.Run(mode.name+"/"+row.name, func(t *testing.T) {
				raw := bytes.Replace(mode.raw, phase, []byte(row.replacement), 1)
				if mode.preflight(raw) != row.allowed {
					t.Fatal("closed phase literal preflight mismatch")
				}
			})
		}
	}
	t.Run("legacy-source-keeps-phase-refusal", func(t *testing.T) {
		legacyPhase := []byte(`"Phase":"active"`)
		if bytes.Count(ordinaryRaw, legacyPhase) != 1 {
			t.Fatal("independent legacy Source phase not unique")
		}
		bad := bytes.Replace(ordinaryRaw, legacyPhase, []byte(`"Phase":"aborted-unfenced"`), 1)
		if storageOwnerV6Preflight(bad) {
			t.Fatal("V6 outcome broadened nested legacy Source phase")
		}
	})
}
