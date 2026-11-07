package desk

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const dialectBackupTestLedger = "assistant-runs-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.json"
const dialectBackupTestRun = "arun_11111111111111111111111111111111"
const dialectBackupTestObject = "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881"

func dialectBackupTestManifest() dialectBackupManifest {
	return dialectBackupManifest{Version: 2, CreatedAt: "2026-10-29T12:00:00Z",
		Files:          []backupEntry{{Name: dialectBackupTestLedger, Bytes: 2, SHA256: "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"}},
		Objects:        []backupEntry{{Name: "objects/" + dialectBackupTestObject, Bytes: 1, SHA256: dialectBackupTestObject}},
		DialectClosure: []dialectBackupClosure{{Ledger: dialectBackupTestLedger, RunID: dialectBackupTestRun, HostOperationID: "host-one", Stage: "candidate", Availability: "present"}}}
}

func dialectBackupTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func dialectBackupTestWire(files, objects, closure string) string {
	return `{"version":2,"createdAt":"2026-10-29T12:00:00Z","files":` + files + `,"objects":` + objects + `,"dialectClosure":` + closure + `}`
}

func TestDialectBackupClosedManifest(t *testing.T) {
	m := dialectBackupTestManifest()
	raw := dialectBackupTestJSON(t, m)
	got, err := readDialectBackupManifest(raw)
	if err != nil || !reflect.DeepEqual(got, m) {
		t.Fatal("legal controls", got, err)
	}
	empty := dialectBackupTestWire("[]", "[]", "[]")
	if _, err := readDialectBackupManifest([]byte(empty)); err != nil {
		t.Fatal("empty tables are syntactically valid", err)
	}
	// Missing availability is a per-slot historical fact, not a generic error.
	m.DialectClosure[0].Availability = "missing"
	m.Objects = []backupEntry{}
	if _, err := readDialectBackupManifest(dialectBackupTestJSON(t, m)); err != nil {
		t.Fatal("unknown missing controls", err)
	}
	// Existing saved run syntax permits hex case; preserve the exact saved ID.
	m.DialectClosure[0].RunID = "arun_ABCDEF0123456789ABCDEF0123456789"
	m.DialectClosure[0].HostOperationID = "host-🙂"
	if _, err := readDialectBackupManifest(dialectBackupTestJSON(t, m)); err != nil {
		t.Fatal("existing identity domain", err)
	}
	for _, name := range []string{"files", "objects", "dialectClosure"} {
		t.Run("required-"+name, func(t *testing.T) {
			for _, replacement := range []string{"null", "{}", `"[]"`, "false", "0"} {
				bad := strings.Replace(empty, `"`+name+`":[]`, `"`+name+`":`+replacement, 1)
				if _, err := readDialectBackupManifest([]byte(bad)); err == nil {
					t.Fatal("accepted array type", bad)
				}
			}
			missing := strings.Replace(empty, `,"`+name+`":[]`, "", 1)
			if _, err := readDialectBackupManifest([]byte(missing)); err == nil {
				t.Fatal("accepted omission", missing)
			}
		})
	}
	badRoots := map[string]string{
		"null": "null", "array": "[]", "trailing": empty + "{}",
		"missing-version":   strings.Replace(empty, `"version":2,`, "", 1),
		"null-version":      strings.Replace(empty, `"version":2`, `"version":null`, 1),
		"string-version":    strings.Replace(empty, `"version":2`, `"version":"2"`, 1),
		"fraction-version":  strings.Replace(empty, `"version":2`, `"version":2.0`, 1),
		"exponent-version":  strings.Replace(empty, `"version":2`, `"version":2e0`, 1),
		"old-version":       strings.Replace(empty, `"version":2`, `"version":1`, 1),
		"future-version":    strings.Replace(empty, `"version":2`, `"version":3`, 1),
		"version-overflow":  strings.Replace(empty, `"version":2`, `"version":18446744073709551616`, 1),
		"case":              strings.Replace(empty, `"version":`, `"Version":`, 1),
		"duplicate":         strings.Replace(empty, `"version":2`, `"version":2,"version":2`, 1),
		"escaped-duplicate": strings.Replace(empty, `"version":2`, `"version":2,"\u0076ersion":2`, 1),
		"unknown":           strings.Replace(empty, `"version":2`, `"version":2,"owner":"a"`, 1),
		"null-time":         strings.Replace(empty, `"2026-10-29T12:00:00Z"`, "null", 1),
		"number-time":       strings.Replace(empty, `"2026-10-29T12:00:00Z"`, "0", 1),
		"invalid-time":      strings.Replace(empty, `"2026-10-29T12:00:00Z"`, `"2026-99-29"`, 1),
		"non-utc":           strings.Replace(empty, `"2026-10-29T12:00:00Z"`, `"2026-10-29T12:00:00+01:00"`, 1),
	}
	for name, bad := range badRoots {
		t.Run(name, func(t *testing.T) {
			if _, err := readDialectBackupManifest([]byte(bad)); err == nil {
				t.Fatal("accepted", bad)
			}
		})
	}
}

func TestDialectBackupRecursiveControls(t *testing.T) {
	file := `{"name":"` + dialectBackupTestLedger + `","bytes":2,"sha256":"` + strings.Repeat("a", 64) + `"}`
	closure := `{"ledger":"` + dialectBackupTestLedger + `","runId":"` + dialectBackupTestRun + `","hostOperationId":"host-one","stage":"candidate","availability":"present"}`
	badFiles := map[string]string{
		"null-row": "null", "array-row": "[]", "scalar-row": `"row"`,
		"null-name":         strings.Replace(file, `"`+dialectBackupTestLedger+`"`, "null", 1),
		"null-bytes":        strings.Replace(file, `"bytes":2`, `"bytes":null`, 1),
		"string-bytes":      strings.Replace(file, `"bytes":2`, `"bytes":"2"`, 1),
		"bool-bytes":        strings.Replace(file, `"bytes":2`, `"bytes":true`, 1),
		"fraction-bytes":    strings.Replace(file, `"bytes":2`, `"bytes":2.0`, 1),
		"overflow-bytes":    strings.Replace(file, `"bytes":2`, `"bytes":9223372036854775808`, 1),
		"missing-bytes":     strings.Replace(file, `,"bytes":2`, "", 1),
		"duplicate-bytes":   strings.Replace(file, `"bytes":2`, `"bytes":2,"bytes":2`, 1),
		"escaped-duplicate": strings.Replace(file, `"bytes":2`, `"bytes":2,"\u0062ytes":2`, 1),
		"case-bytes":        strings.Replace(file, `"bytes":2`, `"Bytes":2`, 1),
		"null-digest":       strings.Replace(file, `"sha256":"`+strings.Repeat("a", 64)+`"`, `"sha256":null`, 1),
		"unknown-object":    strings.Replace(file, `"bytes":2`, `"bytes":2,"source":{"path":"x"}`, 1),
	}
	for name, bad := range badFiles {
		for _, table := range []string{"files", "objects"} {
			t.Run(table+"/"+name, func(t *testing.T) {
				f, o := "["+bad+"]", "[]"
				if table == "objects" {
					f, o = "[]", "["+strings.Replace(bad, dialectBackupTestLedger, "objects/"+strings.Repeat("a", 64), 1)+"]"
				}
				if _, err := readDialectBackupManifest([]byte(dialectBackupTestWire(f, o, "[]"))); err == nil {
					t.Fatal("accepted", bad)
				}
			})
		}
	}
	for _, key := range []string{"ledger", "runId", "hostOperationId", "stage", "availability"} {
		for _, kind := range []string{"null", "type", "missing", "duplicate", "case"} {
			t.Run(key+"/"+kind, func(t *testing.T) {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal([]byte(closure), &fields); err != nil {
					t.Fatal(err)
				}
				member := `"` + key + `":` + string(fields[key])
				bad := closure
				switch kind {
				case "null":
					bad = strings.Replace(bad, member, `"`+key+`":null`, 1)
				case "type":
					bad = strings.Replace(bad, member, `"`+key+`":{}`, 1)
				case "missing":
					bad = strings.Replace(strings.Replace(bad, member+",", "", 1), ","+member, "", 1)
				case "duplicate":
					bad = strings.Replace(bad, member, member+","+member, 1)
				case "case":
					bad = strings.Replace(bad, `"`+key+`":`, `"`+strings.ToUpper(key)+`":`, 1)
				}
				if _, err := readDialectBackupManifest([]byte(dialectBackupTestWire("["+file+"]", "[]", "["+bad+"]"))); err == nil {
					t.Fatal("accepted", bad)
				}
			})
		}
	}
	for name, host := range map[string]string{"surrogate": `"\ud800"`, "invalid-utf8": "\"\xff\""} {
		t.Run(name, func(t *testing.T) {
			bad := strings.Replace(closure, `"host-one"`, host, 1)
			if _, err := readDialectBackupManifest([]byte(dialectBackupTestWire("["+file+"]", "[]", "["+bad+"]"))); err == nil {
				t.Fatal("accepted invalid scalar")
			}
		})
	}
}

func TestDialectBackupMetadata(t *testing.T) {
	cases := map[string]func(*dialectBackupManifest){
		"file-traversal":         func(m *dialectBackupManifest) { m.Files[0].Name = "../" + m.Files[0].Name },
		"file-absolute":          func(m *dialectBackupManifest) { m.Files[0].Name = "/" + m.Files[0].Name },
		"file-backslash":         func(m *dialectBackupManifest) { m.Files[0].Name = "x\\" + m.Files[0].Name },
		"file-unknown":           func(m *dialectBackupManifest) { m.Files[0].Name = "secrets.json" },
		"file-negative":          func(m *dialectBackupManifest) { m.Files[0].Bytes = -1 },
		"file-over":              func(m *dialectBackupManifest) { m.Files[0].Bytes = maxConversationBytes + 1 },
		"file-duplicate":         func(m *dialectBackupManifest) { m.Files = append(m.Files, m.Files[0]) },
		"digest-uppercase":       func(m *dialectBackupManifest) { m.Files[0].SHA256 = strings.Repeat("A", 64) },
		"digest-short":           func(m *dialectBackupManifest) { m.Files[0].SHA256 = "a" },
		"digest-nonhex":          func(m *dialectBackupManifest) { m.Files[0].SHA256 = strings.Repeat("g", 64) },
		"digest-size-conflict":   func(m *dialectBackupManifest) { m.Files[0].SHA256 = m.Objects[0].SHA256 },
		"object-zero":            func(m *dialectBackupManifest) { m.Objects[0].Bytes = 0 },
		"object-negative":        func(m *dialectBackupManifest) { m.Objects[0].Bytes = -1 },
		"object-over":            func(m *dialectBackupManifest) { m.Objects[0].Bytes = (8 << 20) + 1 },
		"object-digest-mismatch": func(m *dialectBackupManifest) { m.Objects[0].SHA256 = strings.Repeat("a", 64) },
		"object-duplicate":       func(m *dialectBackupManifest) { m.Objects = append(m.Objects, m.Objects[0]) },
		"object-size-conflict":   func(m *dialectBackupManifest) { x := m.Objects[0]; x.Bytes++; m.Objects = append(m.Objects, x) },
		"closure-duplicate": func(m *dialectBackupManifest) {
			x := m.DialectClosure[0]
			x.Availability = "missing"
			m.DialectClosure = append(m.DialectClosure, x)
		},
		"closure-unlisted-ledger": func(m *dialectBackupManifest) {
			m.DialectClosure[0].Ledger = "assistant-runs-" + strings.Repeat("b", 64) + ".json"
		},
		"closure-not-ledger": func(m *dialectBackupManifest) {
			m.DialectClosure[0].Ledger = "conversations-" + strings.Repeat("a", 64) + ".json"
		},
		"run-prefix":           func(m *dialectBackupManifest) { m.DialectClosure[0].RunID = "other" },
		"run-nonhex":           func(m *dialectBackupManifest) { m.DialectClosure[0].RunID = "arun_" + strings.Repeat("g", 32) },
		"host-empty":           func(m *dialectBackupManifest) { m.DialectClosure[0].HostOperationID = "" },
		"host-over":            func(m *dialectBackupManifest) { m.DialectClosure[0].HostOperationID = strings.Repeat("h", 129) },
		"host-newline":         func(m *dialectBackupManifest) { m.DialectClosure[0].HostOperationID = "host\n" },
		"stage-unknown":        func(m *dialectBackupManifest) { m.DialectClosure[0].Stage = "draft" },
		"stage-case":           func(m *dialectBackupManifest) { m.DialectClosure[0].Stage = "Candidate" },
		"availability-unknown": func(m *dialectBackupManifest) { m.DialectClosure[0].Availability = "unavailable" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			m := dialectBackupTestManifest()
			change(&m)
			if validateDialectBackupManifest(m) == nil {
				t.Fatal("accepted", m)
			}
		})
	}
	for _, name := range []string{"objects/../" + dialectBackupTestObject, "objects\\" + dialectBackupTestObject, "/objects/" + dialectBackupTestObject, "objects/" + strings.Repeat("A", 64), "objects/" + dialectBackupTestObject + "/", "objects/" + dialectBackupTestObject + ".json"} {
		t.Run(name, func(t *testing.T) {
			m := dialectBackupTestManifest()
			m.Objects[0].Name = name
			if validateDialectBackupManifest(m) == nil {
				t.Fatal("accepted object path", name)
			}
		})
	}
	// A final-stage reference's 4 MiB cap cannot be inferred without its saved
	// ledger ref join. The structural table permits the general 8 MiB object.
	m := dialectBackupTestManifest()
	m.Objects[0].Bytes = 8 << 20
	m.DialectClosure[0].Stage = "final"
	if err := validateDialectBackupManifest(m); err != nil {
		t.Fatal("metadata pretended to know final ref join", err)
	}
}

func TestDialectBackupBounds(t *testing.T) {
	for name, limit := range map[string]int{
		"assistant-runs-" + strings.Repeat("a", 64) + ".json":                                  16 << 20,
		"conversations-" + strings.Repeat("a", 64) + ".json":                                   16 << 20,
		"ai-memory-" + strings.Repeat("a", 64) + ".json":                                       512 << 10,
		"attachment-" + strings.Repeat("a", 64) + "-11111111-1111-1111-1111-111111111111.json": 64 << 20,
	} {
		t.Run(name, func(t *testing.T) {
			m := dialectBackupTestManifest()
			m.Files = []backupEntry{{Name: name, Bytes: int64(limit), SHA256: strings.Repeat("a", 64)}}
			m.Objects = []backupEntry{}
			m.DialectClosure = []dialectBackupClosure{}
			if err := validateDialectBackupManifest(m); err != nil {
				t.Fatal("exact flat cap", err)
			}
			m.Files[0].Bytes++
			if validateDialectBackupManifest(m) == nil {
				t.Fatal("flat cap+1")
			}
		})
	}
	m := dialectBackupTestManifest()
	m.Objects = []backupEntry{}
	m.DialectClosure = []dialectBackupClosure{}
	m.Files = []backupEntry{}
	for i := 0; i < 2; i++ {
		m.Files = append(m.Files, backupEntry{Name: "attachment-" + fmt.Sprintf("%064x", i) + "-11111111-1111-1111-1111-111111111111.json", Bytes: 64 << 20, SHA256: fmt.Sprintf("%064x", i)})
	}
	if err := validateDialectBackupManifest(m); err != nil {
		t.Fatal("128MiB exact", err)
	}
	m.Files = append(m.Files, backupEntry{Name: "conversations-" + strings.Repeat("c", 64) + ".json", Bytes: 1, SHA256: strings.Repeat("c", 64)})
	if validateDialectBackupManifest(m) == nil {
		t.Fatal("128MiB+1")
	}
	m = dialectBackupTestManifest()
	m.Objects = []backupEntry{}
	m.DialectClosure = []dialectBackupClosure{}
	m.Files = []backupEntry{}
	for i := 0; i < 4095; i++ {
		m.Files = append(m.Files, backupEntry{Name: fmt.Sprintf("conversations-%064x.json", i), Bytes: 0, SHA256: strings.Repeat("a", 64)})
	}
	if err := validateDialectBackupManifest(m); err != nil {
		t.Fatal("4096 incl manifest", err)
	}
	m.Files = append(m.Files, backupEntry{Name: "conversations-" + strings.Repeat("b", 64) + ".json", Bytes: 0, SHA256: strings.Repeat("a", 64)})
	if validateDialectBackupManifest(m) == nil {
		t.Fatal("4097 incl manifest")
	}
	for _, v := range []struct {
		total, next, limit int64
		ok                 bool
	}{{0, 0, 0, true}, {math.MaxInt64 - 1, 1, math.MaxInt64, true}, {math.MaxInt64, 1, math.MaxInt64, false}, {1, math.MaxInt64, math.MaxInt64, false}, {-1, 1, 128 << 20, false}, {0, -1, 128 << 20, false}, {2, 0, 1, false}} {
		got, err := dialectBackupAddBytes(v.total, v.next, v.limit)
		if (err == nil) != v.ok {
			t.Fatal("checked sum", v, got, err)
		}
	}
	raw := []byte(dialectBackupTestWire("[]", "[]", "[]"))
	raw = append(raw, bytes.Repeat([]byte(" "), (1<<20)-len(raw))...)
	if _, err := readDialectBackupManifest(raw); err != nil {
		t.Fatal("actual manifest exact", err)
	}
	if _, err := readDialectBackupManifest(append(raw, ' ')); err == nil {
		t.Fatal("actual manifest +1")
	}
}

func TestDialectBackupEncodedManifestBoundAndSorting(t *testing.T) {
	m := dialectBackupTestManifest()
	m.DialectClosure = []dialectBackupClosure{}
	for i := 0; i < 3500; i++ {
		m.DialectClosure = append(m.DialectClosure, dialectBackupClosure{Ledger: dialectBackupTestLedger, RunID: dialectBackupTestRun, HostOperationID: fmt.Sprintf("host-%04d", i), Stage: "candidate", Availability: "missing"})
	}
	remaining := (1 << 20) - len(dialectBackupTestJSON(t, m))
	if remaining < 0 {
		t.Fatal("handwritten fixture already exceeds limit")
	}
	for i := range m.DialectClosure {
		n := min(128-len(m.DialectClosure[i].HostOperationID), remaining)
		m.DialectClosure[i].HostOperationID += strings.Repeat("p", n)
		remaining -= n
	}
	if remaining != 0 || len(dialectBackupTestJSON(t, m)) != 1<<20 {
		t.Fatal("fixture does not reach exact encoded cap", remaining)
	}
	if err := validateDialectBackupManifest(m); err != nil {
		t.Fatal("encoded exact", err)
	}
	for i := range m.DialectClosure {
		if len(m.DialectClosure[i].HostOperationID) < 128 {
			m.DialectClosure[i].HostOperationID += "q"
			break
		}
	}
	if len(dialectBackupTestJSON(t, m)) != (1<<20)+1 || validateDialectBackupManifest(m) == nil {
		t.Fatal("encoded cap+1")
	}
	m = dialectBackupTestManifest()
	candidate := m.DialectClosure[0]
	checked := candidate
	checked.Stage = "checked"
	final := candidate
	final.Stage = "final"
	m.DialectClosure = []dialectBackupClosure{final, candidate, checked}
	before := dialectBackupTestJSON(t, m)
	raw, err := marshalDialectBackupManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := readDialectBackupManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, dialectBackupTestJSON(t, m)) || ordered.DialectClosure[0].Stage != "candidate" || ordered.DialectClosure[1].Stage != "checked" || ordered.DialectClosure[2].Stage != "final" {
		t.Fatal("sort mutated caller or wrong stage order")
	}
	again, err := marshalDialectBackupManifest(ordered)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("unstable writer", err)
	}
}

type dialectBackupTestZIPEntry struct {
	name   string
	raw    []byte
	method uint16
	mode   os.FileMode
	extra  []byte
}

func dialectBackupTestZIP(t *testing.T, entries []dialectBackupTestZIPEntry) []byte {
	t.Helper()
	var body bytes.Buffer
	w := zip.NewWriter(&body)
	for _, v := range entries {
		h := &zip.FileHeader{Name: v.name, Method: v.method, Extra: v.extra}
		if v.mode != 0 {
			h.SetMode(v.mode)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(v.raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func dialectBackupTestZIPEntries(t *testing.T) []dialectBackupTestZIPEntry {
	t.Helper()
	m := dialectBackupTestManifest()
	return []dialectBackupTestZIPEntry{{name: m.Files[0].Name, raw: []byte("{}")}, {name: m.Objects[0].Name, raw: []byte("x")}, {name: "manifest.json", raw: dialectBackupTestJSON(t, m)}}
}

func dialectBackupTestZIPFile(t *testing.T, body []byte) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "backup.zip"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if _, err = f.Write(body); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDialectBackupZIPMetadata(t *testing.T) {
	cases := map[string]func([]dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry{
		"legal":              func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry { return v },
		"deflate-object":     func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry { v[1].method = zip.Deflate; return v },
		"deflate-manifest":   func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry { v[2].method = zip.Deflate; return v },
		"duplicate-object":   func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry { return append(v, v[1]) },
		"duplicate-manifest": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry { return append(v, v[2]) },
		"missing-manifest":   func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry { return v[:2] },
		"missing-object":     func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry { return append(v[:1], v[2]) },
		"extra-object": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry {
			return append(v, dialectBackupTestZIPEntry{name: "objects/" + strings.Repeat("c", 64), raw: []byte("x")})
		},
		"unknown-file": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry { v[0].name = "secrets.json"; return v },
		"traversal": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry {
			v[1].name = "objects/../" + dialectBackupTestObject
			return v
		},
		"backslash": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry {
			v[1].name = "objects\\" + dialectBackupTestObject
			return v
		},
		"symlink": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry {
			v[1].mode = os.ModeSymlink | 0600
			return v
		},
		"fifo": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry {
			v[1].mode = os.ModeNamedPipe | 0600
			return v
		},
		"directory": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry {
			v[1].name = "objects/"
			v[1].mode = os.ModeDir | 0700
			v[1].raw = nil
			return v
		},
		"object-length": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry { v[1].raw = []byte("xx"); return v },
		"manifest-null": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry { v[2].raw = []byte("null"); return v },
		"manifest-over": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry {
			v[2].raw = bytes.Repeat([]byte(" "), (1<<20)+1)
			return v
		},
		"zip64-extra": func(v []dialectBackupTestZIPEntry) []dialectBackupTestZIPEntry {
			v[1].extra = []byte{1, 0, 0, 0}
			return v
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			body := dialectBackupTestZIP(t, change(dialectBackupTestZIPEntries(t)))
			_, _, err := validateDialectBackupArchive(dialectBackupTestZIPFile(t, body))
			if (err == nil) != (name == "legal") {
				t.Fatal(name, err)
			}
		})
	}
	// This deliberately wrong digest passes structural validation: integration
	// MUST still read/hash/schema-check payload and join exact custody.
	v := dialectBackupTestZIPEntries(t)
	m := dialectBackupTestManifest()
	m.Objects[0].SHA256 = strings.Repeat("c", 64)
	m.Objects[0].Name = "objects/" + m.Objects[0].SHA256
	v[1].name = m.Objects[0].Name
	v[2].raw = dialectBackupTestJSON(t, m)
	if _, _, err := validateDialectBackupArchive(dialectBackupTestZIPFile(t, dialectBackupTestZIP(t, v))); err != nil {
		t.Fatal("metadata claimed payload proof", err)
	}
	if _, _, err := validateChatBackup(dialectBackupTestZIPFile(t, dialectBackupTestZIP(t, dialectBackupTestZIPEntries(t)))); err == nil {
		t.Fatal("v1 reader began accepting v2")
	}
}

func TestDialectBackupZIPDirectoryGuard(t *testing.T) {
	for _, kind := range []string{"bad-count", "multidisk", "directory-over", "zip64-footer", "declared-overflow"} {
		t.Run(kind, func(t *testing.T) {
			body := dialectBackupTestZIP(t, dialectBackupTestZIPEntries(t))
			end := len(body) - 22
			offset := int(binary.LittleEndian.Uint32(body[end+16 : end+20]))
			switch kind {
			case "bad-count":
				binary.LittleEndian.PutUint16(body[end+10:end+12], 4097)
			case "multidisk":
				binary.LittleEndian.PutUint16(body[end+4:end+6], 1)
			case "directory-over":
				binary.LittleEndian.PutUint32(body[end+12:end+16], (2<<20)+1)
			case "zip64-footer":
				binary.LittleEndian.PutUint32(body[end+16:end+20], math.MaxUint32)
			case "declared-overflow":
				binary.LittleEndian.PutUint32(body[offset+20:offset+24], math.MaxUint32)
				binary.LittleEndian.PutUint32(body[offset+24:offset+28], math.MaxUint32)
			}
			if _, _, err := validateDialectBackupArchive(dialectBackupTestZIPFile(t, body)); err == nil {
				t.Fatal("accepted", kind)
			}
		})
	}
}

// A sparse prefix avoids a 130 MiB payload/allocation. ZIP permits a prefix;
// adjust both directory and local-header addresses to their actual positions.
func dialectBackupSparseZIP(t *testing.T, body []byte, size int64) *os.File {
	t.Helper()
	body = bytes.Clone(body)
	prefix := size - int64(len(body))
	if prefix < 0 {
		t.Fatal("fixture too large")
	}
	end := len(body) - 22
	offset := int(binary.LittleEndian.Uint32(body[end+16 : end+20]))
	directorySize := int(binary.LittleEndian.Uint32(body[end+12 : end+16]))
	for at := offset; at < offset+directorySize; {
		local := binary.LittleEndian.Uint32(body[at+42 : at+46])
		binary.LittleEndian.PutUint32(body[at+42:at+46], local+uint32(prefix))
		at += 46 + int(binary.LittleEndian.Uint16(body[at+28:at+30])) + int(binary.LittleEndian.Uint16(body[at+30:at+32])) + int(binary.LittleEndian.Uint16(body[at+32:at+34]))
	}
	binary.LittleEndian.PutUint32(body[end+16:end+20], uint32(int64(offset)+prefix))
	f := dialectBackupTestZIPFile(t, nil)
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(body, prefix); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDialectBackupCompleteArchiveBound(t *testing.T) {
	body := dialectBackupTestZIP(t, dialectBackupTestZIPEntries(t))
	if _, _, err := validateDialectBackupArchive(dialectBackupSparseZIP(t, body, 130<<20)); err != nil {
		t.Fatal("130MiB exact", err)
	}
	if _, _, err := validateDialectBackupArchive(dialectBackupSparseZIP(t, body, (130<<20)+1)); err == nil {
		t.Fatal("130MiB+1")
	}
}

// RB-CODEC-1: retain the independent reviewer's bounded alternate-directory
// fixture. Checking the original EOF-reaching EOCD must not authorize Go's
// allocation from a different, later EOCD embedded inside its comment.
func TestDialectBackupReviewEOCDParserAgreement(t *testing.T) {
	const selectedCount = 4097
	body := dialectBackupTestZIP(t, dialectBackupTestZIPEntries(t))
	end := len(body) - 22
	offset := int(binary.LittleEndian.Uint32(body[end+16 : end+20]))
	directorySize := int(binary.LittleEndian.Uint32(body[end+12 : end+16]))
	firstSize := 46 + int(binary.LittleEndian.Uint16(body[offset+28:offset+30])) + int(binary.LittleEndian.Uint16(body[offset+30:offset+32])) + int(binary.LittleEndian.Uint16(body[offset+32:offset+34]))
	prefix := bytes.Repeat(body[offset:offset+firstSize], selectedCount)
	for at := offset; at < offset+directorySize; {
		local := binary.LittleEndian.Uint32(body[at+42 : at+46])
		binary.LittleEndian.PutUint32(body[at+42:at+46], local+uint32(len(prefix)))
		at += 46 + int(binary.LittleEndian.Uint16(body[at+28:at+30])) + int(binary.LittleEndian.Uint16(body[at+30:at+32])) + int(binary.LittleEndian.Uint16(body[at+32:at+34]))
	}
	binary.LittleEndian.PutUint32(body[end+16:end+20], uint32(offset+len(prefix)))
	comment := make([]byte, 23)
	copy(comment, []byte{0x50, 0x4b, 0x05, 0x06})
	binary.LittleEndian.PutUint16(comment[8:10], selectedCount)
	binary.LittleEndian.PutUint16(comment[10:12], selectedCount)
	binary.LittleEndian.PutUint32(comment[12:16], uint32(len(prefix)))
	binary.LittleEndian.PutUint16(body[end+20:end+22], uint16(len(comment)))
	archiveBytes := append(append(prefix, body...), comment...)
	f := dialectBackupTestZIPFile(t, archiveBytes)
	guardErr := checkBackupDirectory(f, int64(len(archiveBytes)))
	archive, zipErr := zip.NewReader(f, int64(len(archiveBytes)))
	if zipErr != nil {
		t.Fatalf("bounded alternate-directory fixture failed to parse: %v", zipErr)
	}
	if len(archive.File) != selectedCount {
		t.Fatalf("fixture selected %d entries, want exactly %d", len(archive.File), selectedCount)
	}
	if guardErr == nil {
		t.Fatal("preallocation guard accepted Go's alternate directory above4096 entries")
	}
}

// RB-CODEC-2: this field is not exposed by zip.File, so refusal must happen in
// the bounded raw directory pass, not in later metadata/table validation.
func TestDialectBackupReviewCentralDiskNumber(t *testing.T) {
	body := dialectBackupTestZIP(t, dialectBackupTestZIPEntries(t))
	end := len(body) - 22
	at := int(binary.LittleEndian.Uint32(body[end+16 : end+20]))
	at += 46 + int(binary.LittleEndian.Uint16(body[at+28:at+30])) + int(binary.LittleEndian.Uint16(body[at+30:at+32])) + int(binary.LittleEndian.Uint16(body[at+32:at+34]))
	binary.LittleEndian.PutUint16(body[at+34:at+36], 1)
	f := dialectBackupTestZIPFile(t, body)
	if err := checkBackupDirectory(f, int64(len(body))); err == nil {
		t.Fatal("raw guard accepted central object disk-number-start1")
	}
	if _, _, err := validateDialectBackupArchive(f); err == nil {
		t.Fatal("single-disk codec accepted a central entry declared on disk1")
	}
}

func dialectBackupComment(t *testing.T, body, comment []byte) []byte {
	t.Helper()
	if len(comment) > 65535 {
		t.Fatal("fixture exceeds ordinary ZIP comment limit")
	}
	body = bytes.Clone(body)
	end := len(body) - 22
	binary.LittleEndian.PutUint16(body[end+20:end+22], uint16(len(comment)))
	return append(body, comment...)
}

func dialectBackupV1ZIP(t *testing.T, method uint16) []byte {
	t.Helper()
	name := "conversations-" + strings.Repeat("a", 64) + ".json"
	raw := []byte(storageChat)
	m := backupManifest{Version: 1, CreatedAt: "2026-10-29T12:00:00Z", Files: []backupEntry{{Name: name, Bytes: int64(len(raw)), SHA256: digestOf(raw)}}}
	return dialectBackupTestZIP(t, []dialectBackupTestZIPEntry{{name: name, raw: raw, method: method}, {name: "manifest.json", raw: dialectBackupTestJSON(t, m), method: method}})
}

func TestDialectBackupReviewLegalCommentsAndV1(t *testing.T) {
	signature := []byte{0x50, 0x4b, 0x05, 0x06}
	shortHeader := append(bytes.Clone(signature), make([]byte, 17)...)
	maximum := bytes.Repeat([]byte("c"), 65535)
	copy(maximum[len(maximum)-len(shortHeader):], shortHeader)
	comments := map[string][]byte{
		"empty":                          nil,
		"plain":                          []byte("ordinary ZIP comment"),
		"arbitrary-bytes":                []byte{'c', 0, 0xff, 'x'},
		"near-signature":                 append([]byte{0x50, 0x4b, 0x05, 0x07}, bytes.Repeat([]byte("x"), 32)...),
		"terminal-signature":             append([]byte("benign "), signature...),
		"incomplete-header":              append([]byte("benign "), shortHeader...),
		"maximum-with-incomplete-header": maximum,
	}
	for _, version := range []string{"v1-store", "v1-deflate", "v2-store"} {
		for name, comment := range comments {
			t.Run(version+"/"+name, func(t *testing.T) {
				var body []byte
				switch version {
				case "v1-store":
					body = dialectBackupV1ZIP(t, zip.Store)
				case "v1-deflate":
					body = dialectBackupV1ZIP(t, zip.Deflate)
				case "v2-store":
					body = dialectBackupTestZIP(t, dialectBackupTestZIPEntries(t))
				}
				body = dialectBackupComment(t, body, comment)
				f := dialectBackupTestZIPFile(t, body)
				if err := checkBackupDirectory(f, int64(len(body))); err != nil {
					t.Fatal("legal shared directory refused", err)
				}
				reader, err := zip.NewReader(f, int64(len(body)))
				if err != nil || reader.Comment != string(comment) {
					t.Fatal("Go selected different comment", err)
				}
				if version == "v2-store" {
					if _, _, err = validateDialectBackupArchive(f); err != nil {
						t.Fatal("legal v2 comment refused", err)
					}
				} else {
					if _, m, err := validateChatBackup(f); err != nil || m.Version != 1 {
						t.Fatal("legal existing v1 refused", err)
					}
				}
			})
		}
	}
}

func TestDialectBackupReviewTruncatedLaterEOCD(t *testing.T) {
	for _, length := range []int{23, 4096} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			body := dialectBackupTestZIP(t, dialectBackupTestZIPEntries(t))
			comment := make([]byte, length)
			copy(comment, []byte{0x50, 0x4b, 0x05, 0x06})
			// The earlier real EOCD reaches EOF, but Go must not skip this
			// later full header whose own declared comment is truncated.
			binary.LittleEndian.PutUint16(comment[20:22], uint16(length))
			body = dialectBackupComment(t, body, comment)
			f := dialectBackupTestZIPFile(t, body)
			if err := checkBackupDirectory(f, int64(len(body))); err == nil {
				t.Fatal("guard skipped later truncated EOCD")
			}
			if _, err := zip.NewReader(f, int64(len(body))); err == nil {
				t.Fatal("fixture did not exercise Go truncated-comment refusal")
			}
		})
	}
	// Keep Desk's existing stricter complete-archive rule, even when Go accepts
	// a valid last EOCD with undeclared bytes after its comment.
	body := append(dialectBackupTestZIP(t, dialectBackupTestZIPEntries(t)), 'x')
	f := dialectBackupTestZIPFile(t, body)
	if _, err := zip.NewReader(f, int64(len(body))); err != nil {
		t.Fatal("trailing-byte fixture must parse in Go", err)
	}
	if err := checkBackupDirectory(f, int64(len(body))); err == nil {
		t.Fatal("guard relaxed exact EOF rule")
	}
}

func TestDialectBackupReviewV1CentralDiskNumber(t *testing.T) {
	for _, record := range []int{0, 1} {
		t.Run(fmt.Sprint(record), func(t *testing.T) {
			body := dialectBackupV1ZIP(t, zip.Deflate)
			end := len(body) - 22
			at := int(binary.LittleEndian.Uint32(body[end+16 : end+20]))
			for i := 0; i < record; i++ {
				at += 46 + int(binary.LittleEndian.Uint16(body[at+28:at+30])) + int(binary.LittleEndian.Uint16(body[at+30:at+32])) + int(binary.LittleEndian.Uint16(body[at+32:at+34]))
			}
			binary.LittleEndian.PutUint16(body[at+34:at+36], 1)
			f := dialectBackupTestZIPFile(t, body)
			if err := checkBackupDirectory(f, int64(len(body))); err == nil {
				t.Fatal("raw guard ignored v1 entry disk-start")
			}
			if _, _, err := validateChatBackup(f); err == nil {
				t.Fatal("v1 accepted multidisk metadata")
			}
		})
	}
}
