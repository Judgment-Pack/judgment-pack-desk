package desk

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayCanonicalJSONFrozenCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/research/verify/fixtures/canon.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Vectors []struct {
			Note, InputJSON, ExpectedHex string
			Reject                       bool
		}
	}
	if err = json.Unmarshal(raw, &corpus); err != nil || len(corpus.Vectors) < 20 {
		t.Fatal("invalid frozen corpus", err)
	}
	for _, v := range corpus.Vectors {
		t.Run(v.Note, func(t *testing.T) {
			got, err := gatewayCanonicalJSON(context.Background(), []byte(v.InputJSON))
			if v.Reject {
				if err == nil {
					t.Fatal("accepted rejected vector")
				}
				return
			}
			if err != nil || hex.EncodeToString(got) != v.ExpectedHex {
				t.Fatalf("%x, %v; expected %s", got, err, v.ExpectedHex)
			}
		})
	}
}

func archiveTestCut(t *testing.T, s *Server) policyArchiveCut {
	t.Helper()
	var cut policyArchiveCut
	err := (filePolicyRepository{open: s.openPolicies}).View(context.Background(), func(ledger policyLedger) error {
		var err error
		cut, err = policyArchiveClosure(context.Background(), ledger, s.artifactRepository())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return cut
}
func archiveTestFile(t *testing.T, cut policyArchiveCut, artifacts retainedArtifactRepository) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "archive-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if err = writePolicyArchive(context.Background(), f, cut, artifacts); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPolicyArchiveCanonicalFrozenProfile(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`{"z":-0,"a":"<>&/\u2028中","x":"\u0001"}`, "{\"a\":\"<>&/\u2028中\",\"x\":\"\\u0001\",\"z\":0}"},
		{`{"\ud800\udc00":1,"\ue000":2}`, "{\"\ue000\":2,\"𐀀\":1}"},
		{`[9007199254740991,-9007199254740991,true,null]`, `[9007199254740991,-9007199254740991,true,null]`},
	} {
		got, err := gatewayCanonicalJSON(context.Background(), []byte(tc.input))
		if err != nil || string(got) != tc.want {
			t.Fatalf("%s => %q %v", tc.input, got, err)
		}
	}
	for _, raw := range []string{`1.0`, `1e2`, `9007199254740992`, `"\ud800"`, `{"a":1,"a":2}`, `[] true`} {
		if _, err := gatewayCanonicalJSON(context.Background(), []byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestPolicyArchiveClosedLocalHistoryAndNoOrphans(t *testing.T) {
	s, _, policy, service := lifecycleFixture(t)
	q := lifecycleBody(t, service, policy, "activate")
	lifecycleDo(t, service, policy, q)
	q = lifecycleBody(t, service, policy, "withdraw")
	q.Rationale = "Retain withdrawal"
	lifecycleDo(t, service, policy, q)
	orphan, err := s.artifactRepository().Put(context.Background(), []byte("unrelated retained secret"))
	if err != nil {
		t.Fatal(err)
	}
	cut := archiveTestCut(t, s)
	for _, ref := range cut.Refs {
		if ref == orphan {
			t.Fatal("included unrelated retained bytes")
		}
	}
	f := archiveTestFile(t, cut, s.artifactRepository())
	restored, manifest, reader, err := readPolicyArchive(context.Background(), f)
	if err != nil || manifest.Format != policyArchiveFormat || !bytes.Equal(cut.Raw, restored.Raw) || restored.Counts != cut.Counts {
		t.Fatal(restored.Counts, cut.Counts, err)
	}
	for _, ref := range cut.Refs {
		a, _ := s.artifactRepository().Get(context.Background(), ref)
		b, e := reader.Get(context.Background(), ref)
		if e != nil || !bytes.Equal(a, b) {
			t.Fatal(ref, e)
		}
	}
	if len(restored.Ledger.Lifecycles[policy.Document.ID].Events) != 2 {
		t.Fatal("lost lifecycle history")
	}
}

func TestPolicyArchiveSourceAuthorityClosure(t *testing.T) {
	f := authorityFixture(t)
	f.configureStatus(t, "published")
	f.ownStatus(t, "externally-managed")
	f.statusDo(t, f.statusBody(t, "apply-external"))
	cut := archiveTestCut(t, f.s)
	archive := archiveTestFile(t, cut, f.s.artifactRepository())
	got, _, _, err := readPolicyArchive(context.Background(), archive)
	if err != nil || got.Counts.SourceObservations != 1 || got.Counts.ExternalVersions != 1 {
		t.Fatal(got.Counts, err)
	}
	// Acquisition and validation must be independent of provider availability.
	f.s.policySourceCall = nil
	f.s.cfg.JpackBin = "/unavailable/runtime"
	if _, _, _, err = readPolicyArchive(context.Background(), archive); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyArchiveRefusesMissingAndInconsistentClosure(t *testing.T) {
	s, token := policyTestServer(t)
	p := policyCreate(t, s, token)
	cut := archiveTestCut(t, s)
	bad := cut.Ledger
	for key, op := range bad.Operations {
		op.Result.Bytes++
		bad.Operations[key] = op
		break
	}
	if _, err := policyArchiveClosure(context.Background(), bad, s.artifactRepository()); err == nil {
		t.Fatal("accepted wrong length")
	}
	if err := os.Remove(filepath.Join(s.projectDir, retainedArtifactDirectory, p.Document.Candidate.Content.SHA256)); err != nil {
		t.Fatal(err)
	}
	if _, err := policyArchiveClosure(context.Background(), cut.Ledger, s.artifactRepository()); err == nil {
		t.Fatal("accepted missing candidate")
	}
}

func TestPolicyArchiveHostileEntriesAndManifest(t *testing.T) {
	s, token := policyTestServer(t)
	policyCreate(t, s, token)
	cut := archiveTestCut(t, s)
	base := archiveTestFile(t, cut, s.artifactRepository())
	stat, _ := base.Stat()
	z, err := zip.NewReader(base, stat.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"extra", "traversal", "duplicate", "link", "wrong-length", "extra-blob", "missing", "unknown-schema", "duplicate-member"} {
		t.Run(kind, func(t *testing.T) {
			out, e := os.CreateTemp(t.TempDir(), "hostile")
			if e != nil {
				t.Fatal(e)
			}
			defer out.Close()
			w := zip.NewWriter(out)
			for _, entry := range z.File {
				if kind == "missing" && strings.HasPrefix(entry.Name, "blobs/") {
					continue
				}
				data, e := readBackupFile(entry, policyArchiveDataLimit)
				if e != nil {
					t.Fatal(e)
				}
				if entry.Name == "manifest.json" {
					var m policyArchiveManifest
					json.Unmarshal(data, &m)
					switch kind {
					case "wrong-length":
						m.Blobs[0].Bytes++
					case "unknown-schema":
						m.PolicySchemaVersion = 999
					case "extra-blob":
						m.Blobs = append(m.Blobs, artifactReference([]byte("orphan")))
					}
					data, _ = json.Marshal(m)
					if kind == "duplicate-member" {
						data = append([]byte(`{"version":1,`), data[1:]...)
					}
				}
				header := &zip.FileHeader{Name: entry.Name, Method: zip.Deflate}
				if kind == "link" && entry.Name == "policy-ledger.json" {
					header.SetMode(os.ModeSymlink | 0600)
				}
				item, e := w.CreateHeader(header)
				if e != nil {
					t.Fatal(e)
				}
				item.Write(data)
			}
			extra := ""
			switch kind {
			case "extra":
				extra = "credentials.json"
			case "traversal":
				extra = "../outside"
			case "duplicate":
				extra = "manifest.json"
			case "extra-blob":
				extra = "blobs/" + digestOf([]byte("orphan"))
			}
			if extra != "" {
				item, _ := w.Create(extra)
				item.Write([]byte("orphan"))
			}
			if e = w.Close(); e != nil {
				t.Fatal(e)
			}
			if _, _, _, e = readPolicyArchive(context.Background(), out); e == nil {
				t.Fatalf("accepted %s", kind)
			}
		})
	}
}

func TestPolicyArchiveEmptyCut(t *testing.T) {
	s, _ := policyTestServer(t)
	cut := archiveTestCut(t, s)
	f := archiveTestFile(t, cut, s.artifactRepository())
	if _, _, _, err := readPolicyArchive(context.Background(), f); err != nil {
		t.Fatal(err)
	}
}
