package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func retentionTestPolicy() (storageRetentionPolicy, storagebudget.RegisteredWorkspace) {
	return storageRetentionInitial(storagebudget.ID{1}), storagebudget.RegisteredWorkspace{ID: storagebudget.ID{2}, Generation: 1}
}
func retentionTestComparison(p storageRetentionPolicy, w storagebudget.RegisteredWorkspace, rev string) storageRetentionComparison {
	return storageRetentionComparison{p.Installation, rev, fmt.Sprintf("%x", w.ID), fmt.Sprint(w.Generation)}
}
func TestStorageRetentionPolicySticky(t *testing.T) {
	t.Run("equal-override-defaults-future-copy-once", func(t *testing.T) {
		p, w := retentionTestPolicy()
		old := p.DefaultRules
		eq := old
		cmp := retentionTestComparison(p, w, "absent")
		p, e := storageRetentionApply(p, []storagebudget.RegisteredWorkspace{w}, w, "absent", storageRetentionEdit{&cmp, "workspace", &eq})
		storageTestMust(t, e)
		other := old
		other[0].MaxBytes = "1024"
		cmp = retentionTestComparison(p, w, strings.Repeat("1", 64))
		p, e = storageRetentionApply(p, []storagebudget.RegisteredWorkspace{w}, w, strings.Repeat("1", 64), storageRetentionEdit{&cmp, "default", &other})
		storageTestMust(t, e)
		got, e := p.workspace(cmp.Workspace, "1")
		storageTestMust(t, e)
		if got != old {
			t.Fatal("equal override silently followed default")
		}
		future := storagebudget.RegisteredWorkspace{ID: storagebudget.ID{3}, Generation: 7}
		f, e := p.workspace(fmt.Sprintf("%x", future.ID), "7")
		storageTestMust(t, e)
		if f != other {
			t.Fatal("new registration lost current default")
		}
		cmp = retentionTestComparison(p, w, strings.Repeat("2", 64))
		p, e = storageRetentionApply(p, []storagebudget.RegisteredWorkspace{w, future}, w, strings.Repeat("2", 64), storageRetentionEdit{&cmp, "copy-default", nil})
		storageTestMust(t, e)
		third := other
		third[0].MaxBytes = "2048"
		cmp = retentionTestComparison(p, w, strings.Repeat("3", 64))
		p, e = storageRetentionApply(p, []storagebudget.RegisteredWorkspace{w, future}, w, strings.Repeat("3", 64), storageRetentionEdit{&cmp, "default", &third})
		storageTestMust(t, e)
		for _, member := range []storagebudget.RegisteredWorkspace{w, future} {
			got, e := p.workspace(fmt.Sprintf("%x", member.ID), fmt.Sprint(member.Generation))
			storageTestMust(t, e)
			if got != other {
				t.Fatal("copy/default baseline was not sticky")
			}
		}
	})
	for _, name := range []string{"stale-revision", "foreign-installation", "stale-generation", "removed-member", "duplicate-registration", "history-automatic", "copy-with-rules"} {
		t.Run(name, func(t *testing.T) {
			p, w := retentionTestPolicy()
			cmp := retentionTestComparison(p, w, strings.Repeat("4", 64))
			rules := p.DefaultRules
			edit := storageRetentionEdit{&cmp, "workspace", &rules}
			all := []storagebudget.RegisteredWorkspace{w}
			switch name {
			case "stale-revision":
				cmp.Revision = strings.Repeat("5", 64)
			case "foreign-installation":
				cmp.Installation = strings.Repeat("f", 32)
			case "stale-generation":
				cmp.Generation = "2"
			case "removed-member":
				all = nil
			case "duplicate-registration":
				all = append(all, w)
			case "history-automatic":
				rules[3].Mode = "automatic"
			case "copy-with-rules":
				edit.Scope = "copy-default"
			}
			before, _ := json.Marshal(p)
			if _, e := storageRetentionApply(p, all, w, strings.Repeat("4", 64), edit); e == nil {
				t.Fatal("invalid/stale policy accepted")
			}
			after, _ := json.Marshal(p)
			if !bytes.Equal(before, after) {
				t.Fatal("refusal mutated original")
			}
		})
	}
}
func TestStorageRetentionStrictCodec(t *testing.T) {
	p, _ := retentionTestPolicy()
	raw, e := json.Marshal(p)
	storageTestMust(t, e)
	t.Run("exact-roundtrip", func(t *testing.T) {
		got, e := decodeStorageRetention(raw)
		storageTestMust(t, e)
		if !reflect.DeepEqual(got, p) {
			t.Fatal("policy changed")
		}
	})
	for _, name := range []string{"unknown", "duplicate-escaped", "trailing-json", "null", "missing", "null-workspaces", "extra-rule", "short-rule", "numeric", "leading-zero", "oversized", "history-automatic"} {
		t.Run(name, func(t *testing.T) {
			bad := append([]byte(nil), raw...)
			switch name {
			case "unknown":
				bad = append([]byte(`{"unknown":0,`), raw[1:]...)
			case "duplicate-escaped":
				bad = append([]byte(`{"versi\u006fn":1,`), raw[1:]...)
			case "trailing-json":
				bad = append(bad, []byte(` {}`)...)
			case "null":
				bad = []byte(`null`)
			case "missing":
				bad = []byte(`{}`)
			case "null-workspaces":
				bad = bytes.Replace(bad, []byte(`"workspaces":[]`), []byte(`"workspaces":null`), 1)
			case "extra-rule", "short-rule":
				var m map[string]json.RawMessage
				storageTestMust(t, json.Unmarshal(raw, &m))
				var rules []storageRetentionRule
				storageTestMust(t, json.Unmarshal(m["defaultRules"], &rules))
				if name == "extra-rule" {
					rules = append(rules, rules[0])
				} else {
					rules = rules[:3]
				}
				m["defaultRules"], e = json.Marshal(rules)
				storageTestMust(t, e)
				bad, e = json.Marshal(m)
				storageTestMust(t, e)
			case "numeric":
				bad = bytes.Replace(bad, []byte(`"maxAgeDays":"1"`), []byte(`"maxAgeDays":1`), 1)
			case "leading-zero":
				bad = bytes.Replace(bad, []byte(`"maxAgeDays":"1"`), []byte(`"maxAgeDays":"01"`), 1)
			case "oversized":
				bad = append(bad, bytes.Repeat([]byte(" "), storageRetentionBytes)...)
			case "history-automatic":
				bad = bytes.Replace(bad, []byte(`"class":"history","mode":"manual"`), []byte(`"class":"history","mode":"automatic"`), 1)
			}
			if _, e := decodeStorageRetention(bad); e == nil {
				t.Fatal("malformed policy accepted", name)
			}
		})
	}
}
func TestStorageRetentionPolicyCapacity(t *testing.T) {
	t.Run("registered-defaults-encode-or-refuse-before-admission", func(t *testing.T) {
		p, w := retentionTestPolicy()
		all := make([]storagebudget.RegisteredWorkspace, storagebudget.MaxWorkspaces)
		for i := range all {
			all[i].ID[0] = byte(i)
			all[i].ID[1] = byte(i >> 8)
			all[i].ID[15] = 1
			all[i].Generation = 1
		}
		w = all[0]
		cmp := retentionTestComparison(p, w, "absent")
		rules := p.DefaultRules
		next, e := storageRetentionApply(p, all, w, "absent", storageRetentionEdit{&cmp, "default", &rules})
		storageTestMust(t, e)
		raw, e := json.Marshal(next)
		storageTestMust(t, e)
		if len(raw) > storageRetentionBytes {
			t.Fatal("default full registration exceeds declared document bound", len(raw))
		}
		decoded, e := decodeStorageRetention(raw)
		storageTestMust(t, e)
		if len(decoded.Workspaces) != storagebudget.MaxWorkspaces {
			t.Fatal("registration omitted")
		}
	})
	t.Run("document-limit-plus-one", func(t *testing.T) {
		p, _ := retentionTestPolicy()
		raw, e := json.Marshal(p)
		storageTestMust(t, e)
		exact := append(raw, bytes.Repeat([]byte(" "), storageRetentionBytes-len(raw))...)
		if _, e := decodeStorageRetention(exact); e != nil {
			t.Fatal(e)
		}
		if _, e := decodeStorageRetention(append(exact, ' ')); e == nil {
			t.Fatal("limit+1 accepted")
		}
	})
}
func retentionTestSource() storageRetentionSource {
	return storageRetentionSource{ID: "source", Kind: "closed", Revision: strings.Repeat("a", 64), Complete: true, Reasons: []string{}, References: []storageRetentionReference{}}
}
func retentionTestObject(id string) storageRetentionObject {
	return storageRetentionObject{ID: id, Kind: "attachment", Class: "history", Revision: strings.Repeat("b", 64), Bytes: 7, Owned: true, Current: true, Private: true, SingleLink: true, Released: true}
}
func TestStorageRetentionEligibility(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"manual-unreferenced", "referenced", "unknown-source-blocks-all", "pending", "unjoined", "hardlink", "foreign", "stale", "pinned", "disposable-age", "completion-unknown", "within-window", "count-pressure-oldest", "bytes-pressure-oldest", "manual-disposable", "alias-independent"} {
		t.Run(name, func(t *testing.T) {
			source := retentionTestSource()
			objects := []storageRetentionObject{retentionTestObject("one")}
			rules := storageRetentionDefaultRules()
			want := "protected"
			switch name {
			case "manual-unreferenced":
				want = "manual-candidate"
			case "referenced":
				source.References = []storageRetentionReference{{"attachment", "one", ""}}
			case "unknown-source-blocks-all":
				source.Complete = false
				want = "unknown"
			case "pending":
				objects[0].Pending = true
			case "unjoined":
				objects[0].Released = false
				want = "unknown"
			case "hardlink":
				objects[0].SingleLink = false
			case "foreign":
				objects[0].Owned = false
			case "stale":
				objects[0].Current = false
			case "pinned":
				objects[0].Pinned = true
			default:
				objects[0].Disposable = true
				objects[0].Class = "scratch"
				objects[0].Completed = now.Add(-48 * time.Hour)
				want = "automatic-candidate"
				switch name {
				case "completion-unknown":
					objects[0].Completed = time.Time{}
					want = "unknown"
				case "within-window":
					objects[0].Completed = now.Add(-time.Hour)
					want = "protected"
				case "manual-disposable":
					rules[0].Mode = "manual"
					want = "manual-candidate"
				case "count-pressure-oldest", "bytes-pressure-oldest":
					objects[0].Completed = now.Add(-2 * time.Hour)
					two := objects[0]
					two.ID = "two"
					two.Completed = now.Add(-time.Hour)
					objects = append(objects, two)
					if name == "count-pressure-oldest" {
						rules[0].MaxObjects = "1"
					} else {
						rules[0].MaxBytes = "7"
					}
				}
			}
			if name == "alias-independent" {
				source.Reasons = []string{"original"}
				source.References = []storageRetentionReference{{"retained", "other", "original"}}
			}
			before := append([]storageRetentionObject(nil), objects...)
			out, e := storageRetentionSelect(now, rules, []storageRetentionSource{source}, objects)
			storageTestMust(t, e)
			if out.Objects[0].State != want {
				t.Fatal(out.Objects)
			}
			if !reflect.DeepEqual(before, objects) {
				t.Fatal("caller mutable operands changed")
			}
			if name == "unknown-source-blocks-all" || name == "unjoined" || name == "completion-unknown" {
				if out.ManualCandidateBytes != nil || out.AutomaticCandidateBytes != nil {
					t.Fatal("unknown became definite zero")
				}
			}
			if len(objects) == 2 && out.Objects[1].State != "protected" {
				t.Fatal("finite pressure removed extra current candidate")
			}
			if name == "alias-independent" {
				out.Sources[0].Reasons[0] = "changed"
				out.Sources[0].References[0].Revision = "changed"
				if source.Reasons[0] != "original" || source.References[0].Revision != "original" {
					t.Fatal("output shared caller reference buffers")
				}
			}
			if out.AllocatedEstimate != nil || out.DestructiveActions {
				t.Fatal("preview invented physical/execution authority")
			}
		})
	}
}
func TestStorageRetentionTypedReferences(t *testing.T) {
	namespace := strings.Repeat("a", 64)
	id := "00000000-0000-0000-0000-000000000001"
	digest := "sha256:" + strings.Repeat("b", 64)
	raw := []byte(fmt.Sprintf(`{"version":1,"chats":[{"id":"chat-one","messages":[],"documents":[{"id":%q,"digest":%q,"pages":[1],"allowPartial":false}]}]}`, id, digest))
	t.Run("declared-attachment-exact", func(t *testing.T) {
		before := bytes.Clone(raw)
		s := retentionConversationReferences("conversations-"+namespace+".json", raw)
		if !s.Complete || !reflect.DeepEqual(s.References, []storageRetentionReference{{"attachment", namespace + ":" + id, digest}}) || !bytes.Equal(raw, before) {
			t.Fatal(s)
		}
	})
	t.Run("opaque-checkpoint-protects-other-source", func(t *testing.T) {
		bad := bytes.Replace(raw, []byte(`"messages":[]`), []byte(`"messages":[],"checkpoint":{"opaque":true}`), 1)
		s := retentionConversationReferences("conversations-"+namespace+".json", bad)
		if s.Complete || len(s.References) != 1 {
			t.Fatal(s)
		}
		out, e := storageRetentionSelect(time.Now(), storageRetentionDefaultRules(), []storageRetentionSource{s, retentionTestSource()}, []storageRetentionObject{retentionTestObject("unrelated")})
		storageTestMust(t, e)
		if out.Complete || out.Objects[0].State != "unknown" || out.ManualCandidateBytes != nil {
			t.Fatal("unknown source leaked negative proof")
		}
	})
	t.Run("invalid-reference-protects", func(t *testing.T) {
		bad := bytes.Replace(raw, []byte(`"pages":[1]`), []byte(`"pages":[]`), 1)
		if retentionConversationReferences("conversations-"+namespace+".json", bad).Complete {
			t.Fatal("malformed ref accepted")
		}
	})
	t.Run("empty-run-source-supported", func(t *testing.T) {
		raw := []byte(fmt.Sprintf(`{"version":1,"project":%q,"runs":[]}`, namespace))
		s := retentionRunReferences("assistant-runs-"+namespace+".json", raw)
		if !s.Complete {
			t.Fatal(s)
		}
	})
	t.Run("memory-chat-edge", func(t *testing.T) {
		raw := []byte(`{"version":1,"settings":{"use":true,"suggest":false},"entries":[{"id":"one","key":"key","text":"remember","state":"active","createdAt":"then","updatedAt":"then","source":{"chatId":"chat-one","turnId":"turn-one"},"sourceDigest":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}],"blocked":[]}`)
		s := retentionMemoryReferences("memory", raw)
		if !s.Complete || len(s.References) != 1 || s.References[0].ID != "chat-one" {
			t.Fatal(s)
		}
	})
	t.Run("publication-exact-content-closure", func(t *testing.T) {
		ref := artifactReference([]byte("original reviewed configuration"))
		p := artifactPublication{At: "2026-11-01T12:00:00Z", Config: &ref, Documents: []publishedDocument{}}
		p.ID = publicationID(p)
		ledger := publicationLedger{Version: 1, Items: []artifactPublication{p}}
		raw, e := json.Marshal(ledger)
		storageTestMust(t, e)
		got, e := decodePublicationLedger(raw)
		storageTestMust(t, e)
		s := retentionPublicationReferences(raw, got)
		if !s.Complete || !reflect.DeepEqual(s.References, []storageRetentionReference{{"retained", ref.SHA256, fmt.Sprint(ref.Bytes)}}) {
			t.Fatal(s)
		}
		bad := bytes.Replace(raw, []byte(ref.SHA256), []byte(strings.Repeat("f", 64)), 1)
		if _, e := decodePublicationLedger(bad); e == nil {
			t.Fatal("original publication commitment lost")
		}
	})
}
func TestStorageRetentionExportClosure(t *testing.T) {
	t.Run("exact-inline-chat-closure", func(t *testing.T) {
		c := storageRetentionCapture{Sources: []storageRetentionSource{retentionTestSource()}, ChatNames: map[string]string{"one": "sha256:original"}, Retained: map[retainedArtifactRef][]byte{}}
		c.Sources[0].References = []storageRetentionReference{{"attachment", "one", "sha256:original"}}
		if !c.exportable() {
			t.Fatal("complete supported closure refused")
		}
		for _, revision := range []string{"sha256:changed", ""} {
			c.Sources[0].References[0].Revision = revision
			if c.exportable() {
				t.Fatal("mismatched exact original allowed")
			}
		}
	})
	t.Run("missing-attachment", func(t *testing.T) {
		c := storageRetentionCapture{Sources: []storageRetentionSource{retentionTestSource()}}
		c.Sources[0].References = []storageRetentionReference{{"attachment", "missing", "sha256:original"}}
		if c.exportable() {
			t.Fatal("missing external operand hidden")
		}
	})
	t.Run("captured-project-bytes-not-chat-admission", func(t *testing.T) {
		ref := artifactReference([]byte("exact original"))
		c := storageRetentionCapture{Sources: []storageRetentionSource{retentionTestSource()}, Retained: map[retainedArtifactRef][]byte{ref: []byte("exact original")}}
		c.Sources[0].References = []storageRetentionReference{{"retained", ref.SHA256, fmt.Sprint(ref.Bytes)}}
		if c.exportable() {
			t.Fatal("project bytes appended outside original archive plan")
		}
	})
	t.Run("unknown-even-with-decoded-row", func(t *testing.T) {
		s := retentionTestSource()
		s.Complete = false
		c := storageRetentionCapture{Sources: []storageRetentionSource{retentionTestSource(), s}}
		if c.exportable() {
			t.Fatal("partial closure called complete")
		}
	})
	t.Run("joint-data-bound", func(t *testing.T) {
		c := storageRetentionCapture{Total: maxBackupData - 1}
		storageTestMust(t, c.charge([]byte("x")))
		if c.charge([]byte("y")) == nil || c.Total != maxBackupData {
			t.Fatal("joint bound exceeded or failure mutated total")
		}
	})
}
func TestStorageRetentionCheckedRead(t *testing.T) {
	t.Run("raw-and-no-creation", func(t *testing.T) {
		root, e := os.OpenRoot(t.TempDir())
		storageTestMust(t, e)
		defer root.Close()
		raw := []byte("{  \"n\": 1.0 }")
		storageTestMust(t, os.WriteFile(filepath.Join(root.Name(), "original"), raw, 0600))
		got, _, e := storageRetentionRead(root, "original", len(raw))
		storageTestMust(t, e)
		if !bytes.Equal(raw, got) {
			t.Fatal("lexical raw changed")
		}
		if _, _, e = storageRetentionRead(root, "absent", 100); !errors.Is(e, os.ErrNotExist) {
			t.Fatal(e)
		}
		if _, e = root.Lstat("absent"); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("readonly capture created file")
		}
	})
	t.Run("bound-before-read", func(t *testing.T) {
		root, e := os.OpenRoot(t.TempDir())
		storageTestMust(t, e)
		defer root.Close()
		storageTestMust(t, os.WriteFile(filepath.Join(root.Name(), "object"), []byte("four"), 0600))
		if _, _, e := storageRetentionRead(root, "object", 3); e == nil {
			t.Fatal("oversized raw read accepted")
		}
	})
}
func TestStorageRetentionHTTPGuards(t *testing.T) {
	s := policyHTTPServer(t)
	token := policyTestSession(t, s)
	for _, tc := range []struct {
		name, method, path, body, auth, origin, media string
		status                                        int
	}{
		{"session-first", "POST", "/api/storage/retention", "{", "", "https://evil.test", "application/json", 401},
		{"origin", "POST", "/api/storage/retention/preview", "{}", "Bearer " + token, "https://evil.test", "application/json", 403},
		{"method", "PUT", "/api/storage/retention", "", "Bearer " + token, "", "", 405},
		{"no-delete-route", "POST", "/api/storage/retention/delete", "{}", "Bearer " + token, "", "application/json", 400},
		{"query", "GET", "/api/storage/retention?path=foreign", "", "Bearer " + token, "", "", 400},
		{"get-body", "GET", "/api/storage/retention", "{}", "Bearer " + token, "", "application/json", 400},
		{"media", "POST", "/api/storage/retention/preview", "{}", "Bearer " + token, "", "text/plain", 400},
		{"unknown", "POST", "/api/storage/retention/preview", `{"path":"foreign"}`, "Bearer " + token, "", "application/json", 400},
		{"duplicate", "POST", "/api/storage/retention/preview", `{"revision":"a","revi\u0073ion":"b"}`, "Bearer " + token, "", "application/json", 400},
		{"null", "POST", "/api/storage/retention/preview", "null", "Bearer " + token, "", "application/json", 400},
		{"null-revision", "POST", "/api/storage/retention/preview", `{"revision":null}`, "Bearer " + token, "", "application/json", 400},
		{"alias-null-revision", "POST", "/api/storage/retention/preview", `{"Revision":null}`, "Bearer " + token, "", "application/json", 400},
		{"trailing", "POST", "/api/storage/retention/preview", "{} {}", "Bearer " + token, "", "application/json", 400},
		{"missing-edit", "POST", "/api/storage/retention", "{}", "Bearer " + token, "", "application/json", 400},
		{"export-needs-snapshot", "POST", "/api/storage/retention/export", "{}", "Bearer " + token, "", "application/json", 400},
		{"read-no-preparation", "GET", "/api/storage/retention", "", "Bearer " + token, "", "", 503},
		{"preview-no-preparation", "POST", "/api/storage/retention/preview", "{}", "Bearer " + token, "", "application/json", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.body != "" {
				r = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			}
			r.Header.Set("Authorization", tc.auth)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", tc.media)
			w := httptest.NewRecorder()
			s.handleStorageRetention(w, r)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if s.storageEnrollment != nil {
				t.Fatal("read/preview established hidden owner")
			}
		})
	}
}
func TestStorageRetentionQualifiedPolicyCASRestart(t *testing.T) {
	requireStorageLine(t)
	storagePolicyHostRequireRecord(t)
	cfg := ownerActualConfig(t)
	s := ownerActualNew(t, cfg)
	storageTestMust(t, s.prepareStoragePolicy(t.Context()))
	first, e := s.readStorageRetention(t.Context())
	storageTestMust(t, e)
	original := first.WorkspaceRules
	defaults := first.DefaultRules
	defaults[0].MaxBytes = "1024"
	changed, e := s.updateStorageRetention(t.Context(), storageRetentionEdit{&first.Expected, "default", &defaults})
	storageTestMust(t, e)
	if changed.WorkspaceRules != original || changed.DefaultRules != defaults || changed.DestructiveActions {
		t.Fatal("sticky settings or disabled effects changed")
	}
	before := ownerReviewDurableState(t, s.storageConstruction.original.config)
	if _, e = s.updateStorageRetention(t.Context(), storageRetentionEdit{&first.Expected, "default", &defaults}); !errors.Is(e, storagebudget.ErrStale) {
		t.Fatal("stale original did not refuse", e)
	}
	if !reflect.DeepEqual(before, ownerReviewDurableState(t, s.storageConstruction.original.config)) {
		t.Fatal("stale policy wrote durable prefix")
	}
	storageTestMust(t, s.Close())
	next := ownerActualNew(t, cfg)
	got, e := next.readStorageRetention(context.Background())
	storageTestMust(t, e)
	if got.Expected.Revision != changed.Expected.Revision || got.WorkspaceRules != original || got.DefaultRules != defaults {
		t.Fatal("restart changed exact policy")
	}
}

func TestStorageRetentionAttachmentIdentity(t *testing.T) {
	t.Run("result-digest-not-file-digest", func(t *testing.T) {
		result := "sha256:" + strings.Repeat("a", 64)
		response := fmt.Sprintf(`{"receipt":{"receiptVersion":"3","kind":"acquisition","source":"fixture","sessionId":"session","callIndex":0,"resultDigest":%q}}`, result)
		raw, e := json.Marshal(attachmentObject{Version: 1, Original: attachmentOriginal{Name: "original.txt", MediaType: "text/plain", Bytes: "eA==", SHA256: "sha256:" + digestOf([]byte("x"))}, Proof: &attachmentProof{Session: "session", Source: "fixture", Authority: "fixture", PublicKey: strings.Repeat("1", 64), Response: response}})
		storageTestMust(t, e)
		storageTestMust(t, validateAttachmentObject(raw))
		if retentionAttachmentResultDigest(raw) != result || result == "sha256:"+digestOf(raw) {
			t.Fatal("acquisition identity was relabeled as stored bytes")
		}
		bad := bytes.Replace(raw, []byte(`session`), []byte(`other`), 1)
		if retentionAttachmentResultDigest(bad) != "" {
			t.Fatal("receipt/object session mismatch accepted")
		}
	})
	t.Run("bare-original-has-no-acquisition-identity", func(t *testing.T) {
		raw, e := json.Marshal(attachmentObject{Version: 1, Original: attachmentOriginal{Name: "original.txt", MediaType: "text/plain", Bytes: "eA==", SHA256: "sha256:" + digestOf([]byte("x"))}})
		storageTestMust(t, e)
		if retentionAttachmentResultDigest(raw) != "" {
			t.Fatal("invented acquisition identity")
		}
	})
}

// These cases exercise the public and independently callable decoder boundaries,
// not just the typed application relation after JSON has erased original names.
func TestStorageRetentionExactJSONMembers(t *testing.T) {
	p, w := retentionTestPolicy()
	p.Workspaces = []storageRetentionWorkspace{{ID: fmt.Sprintf("%x", w.ID), Generation: "1", Rules: p.DefaultRules}}
	policy, err := json.Marshal(p)
	storageTestMust(t, err)
	cmp := retentionTestComparison(p, w, "absent")
	edit, err := json.Marshal(storageRetentionEdit{Expected: &cmp, Scope: "workspace", Rules: &p.DefaultRules})
	storageTestMust(t, err)
	rules, err := json.Marshal(p.DefaultRules)
	storageTestMust(t, err)
	for _, tc := range []struct{ name, boundary, from, to string }{
		{"policy-top-alias", "policy", `"version":1`, `"Version":1`},
		{"policy-top-alias-duplicate", "policy", `"version":1`, `"version":1,"Version":1`},
		{"policy-workspace-alias", "policy", `"generation":"1"`, `"Generation":"1"`},
		{"policy-workspace-alias-duplicate", "policy", `"generation":"1"`, `"generation":"1","Generation":"1"`},
		{"policy-rule-alias", "policy", `"maxBytes":`, `"MaxBytes":`},
		{"policy-rule-escaped-alias-duplicate", "policy", `"maxBytes":"67108864"`, `"maxBytes":"67108864","Max\u0042ytes":"1"`},
		{"http-top-alias", "http", `"scope":"workspace"`, `"Scope":"workspace"`},
		{"http-top-alias-duplicate", "http", `"scope":"workspace"`, `"scope":"workspace","Scope":"default"`},
		{"http-expected-alias", "http", `"revision":"absent"`, `"Revision":"absent"`},
		{"http-expected-escaped-alias-duplicate", "http", `"revision":"absent"`, `"revision":"absent","Revi\u0073ion":"absent"`},
		{"http-rule-alias", "http", `"maxObjects":"256"`, `"MaxObjects":"256"`},
		{"http-rule-alias-duplicate", "http", `"maxObjects":"256"`, `"maxObjects":"256","MaxObjects":"1"`},
		{"direct-rule-alias", "rules", `"maxAgeDays":"1"`, `"MaxAgeDays":"1"`},
		{"direct-rule-alias-duplicate", "rules", `"mode":"automatic"`, `"mode":"automatic","Mode":"manual"`},
		{"direct-rule-escaped-duplicate", "rules", `"class":"scratch"`, `"class":"scratch","cl\u0061ss":"scratch"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := policy
			if tc.boundary == "http" {
				original = edit
			}
			if tc.boundary == "rules" {
				original = rules
			}
			bad := bytes.Replace(original, []byte(tc.from), []byte(tc.to), 1)
			if bytes.Equal(original, bad) {
				t.Fatal("test did not alter its source operand")
			}
			var err error
			switch tc.boundary {
			case "policy":
				_, err = decodeStorageRetention(bad)
			case "http":
				var out storageRetentionEdit
				err = storageRetentionHTTPDecode(bad, &out)
			case "rules":
				var out storageRetentionRules
				err = out.UnmarshalJSON(bad)
			}
			if err == nil {
				t.Fatal("noncanonical original member accepted")
			}
		})
	}
	t.Run("canonical-http-and-direct-rules", func(t *testing.T) {
		var decoded storageRetentionEdit
		storageTestMust(t, storageRetentionHTTPDecode(edit, &decoded))
		if !storageRetentionEditValid(decoded) || !reflect.DeepEqual(decoded.Expected, &cmp) || *decoded.Rules != p.DefaultRules {
			t.Fatal(decoded)
		}
		var direct storageRetentionRules
		storageTestMust(t, direct.UnmarshalJSON(rules))
		if direct != p.DefaultRules {
			t.Fatal("direct rules changed")
		}
	})
	t.Run("copy-default-explicit-null-refused", func(t *testing.T) {
		raw, err := json.Marshal(storageRetentionEdit{Expected: &cmp, Scope: "copy-default"})
		storageTestMust(t, err)
		var out storageRetentionEdit
		if storageRetentionHTTPDecode(raw, &out) == nil {
			t.Fatal("explicit rules:null treated as omission")
		}
		omitted := bytes.Replace(raw, []byte(`,"rules":null`), nil, 1)
		storageTestMust(t, storageRetentionHTTPDecode(omitted, &out))
		if !storageRetentionEditValid(out) {
			t.Fatal("one-time copy without rules refused")
		}
	})
}

func TestStorageRetentionNestedReferenceCompleteness(t *testing.T) {
	namespace := strings.Repeat("a", 64)
	id := "00000000-0000-0000-0000-000000000001"
	digest := "sha256:" + strings.Repeat("b", 64)
	base := []byte(fmt.Sprintf(`{"version":1,"chats":[{"id":"chat-one","messages":[],"documents":[{"id":%q,"digest":%q,"pages":[1],"allowPartial":false}]}]}`, id, digest))
	for _, tc := range []struct{ name, from, to string }{
		{"opaque-document-member", `"allowPartial":false`, `"allowPartial":false,"future":{"attachment":"opaque-other"}`},
		{"case-alias-document-member", `"allowPartial":false`, `"allowPartial":false,"NeedsReview":false`},
		{"null-document-boolean", `"allowPartial":false`, `"allowPartial":null`},
		{"missing-document-boolean", `,"allowPartial":false`, ``},
		{"opaque-attachment-member", `"documents":[`, `"attachments":[`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := bytes.Replace(base, []byte(tc.from), []byte(tc.to), 1)
			if tc.name == "opaque-attachment-member" {
				raw = bytes.Replace(raw, []byte(`"allowPartial":false`), []byte(`"allowPartial":false,"future":{"retained":"other"}`), 1)
			}
			storageTestMust(t, validateConversations(raw))
			original := bytes.Clone(raw)
			source := retentionConversationReferences("conversations-"+namespace+".json", raw)
			if source.Complete || !reflect.DeepEqual(source.References, []storageRetentionReference{{Kind: "attachment", ID: namespace + ":" + id, Revision: digest}}) {
				t.Fatal(source)
			}
			objects := []storageRetentionObject{retentionTestObject("known"), retentionTestObject("otherwise-decoded")}
			out, err := storageRetentionSelect(time.Now(), storageRetentionDefaultRules(), []storageRetentionSource{retentionTestSource(), source}, objects)
			storageTestMust(t, err)
			if out.Complete || out.ManualCandidateBytes != nil || out.AutomaticCandidateBytes != nil || out.AllocatedEstimate != nil {
				t.Fatal("partial join supplied a negative total", out)
			}
			for _, row := range out.Objects {
				if row.State != "unknown" {
					t.Fatal("unknown source did not protect every object", row)
				}
			}
			capture := storageRetentionCapture{Sources: []storageRetentionSource{source}, ChatNames: map[string]string{namespace + ":" + id: digest}, Retained: map[retainedArtifactRef][]byte{}}
			if capture.exportable() {
				t.Fatal("known attachment hid unexamined closure")
			}
			if !bytes.Equal(raw, original) {
				t.Fatal("original bytes changed")
			}
		})
	}
}

// Genuine original owner/startup and preview, not a mock of its release helper.
// Parent must provide exact current-source Record3; a skipped case is not PASS.
func TestStorageRetentionQualifiedPreviewCleanup(t *testing.T) {
	requireStorageLine(t)
	storagePolicyHostRequireRecord(t)
	for _, phase := range []string{"chat-entries-close", "settings-close"} {
		t.Run(phase, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			s := ownerActualNew(t, cfg)
			storageTestMust(t, s.prepareStoragePolicy(t.Context()))
			// The ordinary genuine first-use route enrolls history and its lock;
			// preview itself is never allowed to prepare or create either.
			server := httptest.NewServer(s)
			defer server.Close()
			storageTransferNativeSelection(t, s, server)
			r := httptest.NewRequest("POST", server.URL+"/api/storage/retention/preview", nil).WithContext(t.Context())
			pageBearer(privateWorkspaceTestSession(t, server))(r)
			first, err := s.previewStorageRetention(r, "")
			storageTestMust(t, err)
			if first.ObservedAt == "" || first.ExportRevision == "" {
				t.Fatal("successful original observation absent")
			}
			before := ownerReviewDurableState(t, s.storageConstruction.original.config)
			o := s.storageConstruction.owner
			o.fault = &storageBudgetFault{Name: "retention-read", Phase: phase}
			got, err := s.previewStorageRetention(r, first.ExportRevision)
			if !errors.Is(err, errStorageOwnerRecovery) || !reflect.DeepEqual(got, storageRetentionPreview{}) || o.fault != nil {
				t.Fatal("cleanup failure published a preview or seam was not reached", got, err)
			}
			if !s.writes.TryLock() {
				t.Fatal("preview retained write exclusion")
			}
			s.writes.Unlock()
			release, _, err := s.privateDataTurns.tryAcquireOriginal(t.Context())
			storageTestMust(t, err)
			release()
			settings, err := s.storageSettingsRoot()
			storageTestMust(t, err)
			named, err := storageRetentionReadLock(settings, ".data.lock", true)
			if err != nil {
				t.Fatal(errors.Join(err, settings.Close()))
			}
			storageTestMust(t, errors.Join(named.current(), named.file.Close(), settings.Close()))
			if !reflect.DeepEqual(before, ownerReviewDurableState(t, s.storageConstruction.original.config)) {
				t.Fatal("read-only failure changed durable state")
			}
			storageTestMust(t, s.Close())
		})
	}
}

func TestStorageRetentionCheckedSourceOpen(t *testing.T) {
	t.Run("existing-exact-raw", func(t *testing.T) {
		dir := t.TempDir()
		storageTestMust(t, os.Chmod(dir, 0700))
		raw := []byte("{ \"original\": 1.0 }")
		storageTestMust(t, os.WriteFile(filepath.Join(dir, "original.json"), raw, 0600))
		root, err := openRetentionExistingRoot(dir)
		storageTestMust(t, err)
		got, _, err := storageRetentionRead(root, "original.json", 1024)
		storageTestMust(t, errors.Join(err, root.Close()))
		if !bytes.Equal(got, raw) {
			t.Fatal("exact raw bytes changed")
		}
	})
	t.Run("missing-ancestor-not-created", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "missing", "data")
		if root, err := openRetentionExistingRoot(dir); root != nil || !errors.Is(err, os.ErrNotExist) {
			t.Fatal(root, err)
		}
		if _, err := os.Stat(filepath.Dir(dir)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("read created an ancestor", err)
		}
	})
	t.Run("symlink-refused", func(t *testing.T) {
		parent := t.TempDir()
		link := filepath.Join(parent, "alias")
		storageTestMust(t, os.Symlink(t.TempDir(), link))
		if root, err := openRetentionExistingRoot(link); root != nil || err == nil {
			if root != nil {
				root.Close()
			}
			t.Fatal("symlink accepted", err)
		}
	})
	t.Run("missing-pointer-not-created", func(t *testing.T) {
		root, err := os.OpenRoot(t.TempDir())
		storageTestMust(t, err)
		defer root.Close()
		s := &Server{}
		if store, err := s.openRetentionChatData(root); store != nil || !errors.Is(err, os.ErrNotExist) {
			t.Fatal(store, err)
		}
		rows, err := storageRetentionEntries(root, nil, "")
		storageTestMust(t, err)
		if len(rows) != 0 {
			t.Fatal("read created a pointer or store", rows)
		}
	})
}

func TestStorageRetentionCheckedProjectFolders(t *testing.T) {
	for _, name := range []string{"existing-leaf", "clean-missing-child", "intermediate-close", "missing-child-close", "symlink-refused", "nonprivate-refused"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			storageTestMust(t, os.Mkdir(filepath.Join(dir, ".desk-private"), 0700))
			parts := strings.Split(publicationDirectory, "/")
			leaf := filepath.Join(dir, publicationDirectory)
			if name != "clean-missing-child" && name != "missing-child-close" {
				if name == "symlink-refused" {
					storageTestMust(t, os.Symlink(t.TempDir(), leaf))
				} else {
					storageTestMust(t, os.Mkdir(leaf, 0700))
				}
			}
			if name == "nonprivate-refused" {
				storageTestMust(t, os.Chmod(leaf, 0750))
			}
			root, err := os.OpenRoot(dir)
			storageTestMust(t, err)
			defer root.Close()
			o := &storageBudgetOwner{}
			if name == "intermediate-close" || name == "missing-child-close" {
				o.fault = &storageBudgetFault{Name: "retention-read", Phase: "publication-ancestor-close"}
			}
			opened, absent, err := storageRetentionOwnFolder(root, parts, "artifact versions", o, "publication-ancestor-close")
			switch name {
			case "existing-leaf":
				storageTestMust(t, err)
				if opened == nil || absent {
					t.Fatal("existing own leaf lost")
				}
				storageTestMust(t, opened.Close())
			case "clean-missing-child":
				if opened != nil || !absent || err != nil {
					t.Fatal("absence was not proved after cleanup", opened, absent, err)
				}
			case "intermediate-close":
				if opened != nil || absent || !errors.Is(err, errStorageOwnerRecovery) || o.fault != nil {
					t.Fatal("ancestor Close was lost", opened, absent, err)
				}
			case "missing-child-close":
				if opened != nil || absent || !errors.Is(err, os.ErrNotExist) || !errors.Is(err, errStorageOwnerRecovery) || o.fault != nil {
					t.Fatal("joined missing/Close was mistaken for clean absence", opened, absent, err)
				}
			default:
				if opened != nil || absent || err == nil {
					t.Fatal("own-folder constraint relaxed", opened, absent, err)
				}
			}
			if _, err := root.Stat("."); err != nil {
				t.Fatal("borrowed project was closed", err)
			}
			if name == "clean-missing-child" || name == "missing-child-close" {
				if _, err := os.Lstat(leaf); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("noncreating read made a child", err)
				}
			}
		})
	}
}

func TestStorageRetentionCheckedRetainedOpening(t *testing.T) {
	for _, name := range []string{"exact-original", "intermediate-close", "missing-child-close"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			storageTestMust(t, os.Mkdir(filepath.Join(dir, ".desk-private"), 0700))
			raw := []byte("{  \"original\": true }")
			ref := artifactReference(raw)
			if name != "missing-child-close" {
				storageTestMust(t, os.Mkdir(filepath.Join(dir, retainedArtifactDirectory), 0700))
				storageTestMust(t, os.WriteFile(filepath.Join(dir, retainedArtifactDirectory, ref.SHA256), raw, 0600))
			}
			project, err := os.OpenRoot(dir)
			storageTestMust(t, err)
			defer project.Close()
			o := &storageBudgetOwner{}
			if name != "exact-original" {
				o.fault = &storageBudgetFault{Name: "retention-read", Phase: "retained-ancestor-close"}
			}
			// Ordinary Get/Put's opener must not be used by this checked capture.
			repo := fileRetainedArtifactRepository{open: func(bool) (*os.Root, error) {
				t.Fatal("retention used unchecked ordinary opener")
				return nil, os.ErrInvalid
			}}
			got, err := repo.retentionRead(t.Context(), ref, project, o)
			if name == "exact-original" {
				storageTestMust(t, err)
				if !bytes.Equal(got, raw) {
					t.Fatal("original retained bytes changed")
				}
			} else if got != nil || !errors.Is(err, errStorageOwnerRecovery) || o.fault != nil {
				t.Fatal("retained ancestor Close failure hidden", got, err)
			}
			if name == "missing-child-close" && !errors.Is(err, os.ErrNotExist) {
				t.Fatal("original missing operand lost", err)
			}
			if _, err := project.Stat("."); err != nil {
				t.Fatal("borrowed project closed", err)
			}
		})
	}
}

// Original startup owns these already-existing exact project bytes. The test
// traverses the genuine populated capture, not a mock of its release helper.
func TestStorageRetentionQualifiedProjectCleanup(t *testing.T) {
	requireStorageLine(t)
	storagePolicyHostRequireRecord(t)
	for _, phase := range []string{"publication-ancestor-close", "draft-ancestor-close", "retained-ancestor-close"} {
		t.Run(phase, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			raw := []byte("original immutable retained configuration")
			ref := artifactReference(raw)
			publication := artifactPublication{At: "2026-11-01T12:00:00Z", Config: &ref, Documents: []publishedDocument{}}
			publication.ID = publicationID(publication)
			ledger, err := json.Marshal(publicationLedger{Version: 1, Items: []artifactPublication{publication}})
			storageTestMust(t, err)
			for _, folder := range []string{publicationDirectory, retainedArtifactDirectory, dialectDraftDir} {
				storageTestMust(t, os.MkdirAll(filepath.Join(cfg.ProjectDir, folder), 0700))
			}
			ledgerPath := filepath.Join(cfg.ProjectDir, publicationDirectory, "history.json")
			retainedPath := filepath.Join(cfg.ProjectDir, retainedArtifactDirectory, ref.SHA256)
			storageTestMust(t, os.WriteFile(ledgerPath, ledger, 0600))
			storageTestMust(t, os.WriteFile(retainedPath, raw, 0600))
			s := ownerActualNew(t, cfg)
			storageTestMust(t, s.prepareStoragePolicy(t.Context()))
			server := httptest.NewServer(s)
			defer server.Close()
			storageTransferNativeSelection(t, s, server)
			r := httptest.NewRequest("POST", server.URL+"/api/storage/retention/preview", nil).WithContext(t.Context())
			pageBearer(privateWorkspaceTestSession(t, server))(r)
			first, err := s.previewStorageRetention(r, "")
			storageTestMust(t, err)
			found := false
			for _, source := range first.Sources {
				if source.Kind == "publications" && source.Complete && reflect.DeepEqual(source.References, []storageRetentionReference{{Kind: "retained", ID: ref.SHA256, Revision: fmt.Sprint(ref.Bytes)}}) {
					found = true
				}
			}
			if !found || first.ObservedAt == "" || first.ExportComplete {
				t.Fatal("actual original project closure not captured or chat-only export broadened", first)
			}
			before := ownerReviewDurableState(t, s.storageConstruction.original.config)
			o := s.storageConstruction.owner
			o.fault = &storageBudgetFault{Name: "retention-read", Phase: phase}
			got, err := s.previewStorageRetention(r, first.ExportRevision)
			if !errors.Is(err, errStorageOwnerRecovery) || !reflect.DeepEqual(got, storageRetentionPreview{}) || o.fault != nil {
				t.Fatal("project Close failure published preview or seam not reached", got, err)
			}
			if !s.writes.TryLock() {
				t.Fatal("writes exclusion retained")
			}
			s.writes.Unlock()
			release, _, err := s.privateDataTurns.tryAcquireOriginal(t.Context())
			storageTestMust(t, err)
			release()
			settings, err := s.storageSettingsRoot()
			storageTestMust(t, err)
			named, err := storageRetentionReadLock(settings, ".data.lock", true)
			if err != nil {
				t.Fatal(errors.Join(err, settings.Close()))
			}
			storageTestMust(t, errors.Join(named.current(), named.file.Close(), settings.Close()))
			lease, err := s.lockProjectOwned(t.Context(), 0)
			storageTestMust(t, err)
			storageTestMust(t, lease.Close())
			if _, err := s.root.Stat("."); err != nil {
				t.Fatal("original project closed", err)
			}
			if !reflect.DeepEqual(before, ownerReviewDurableState(t, s.storageConstruction.original.config)) {
				t.Fatal("read-only failure changed durable owner/Q state")
			}
			for path, want := range map[string][]byte{ledgerPath: ledger, retainedPath: raw} {
				got, err := os.ReadFile(path)
				storageTestMust(t, err)
				if !bytes.Equal(got, want) {
					t.Fatal("original project bytes changed")
				}
			}
			storageTestMust(t, s.Close())
		})
	}
}
