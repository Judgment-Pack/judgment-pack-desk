package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeskHomePathSumClosedCodec(t *testing.T) {
	const encoded = `{"bytes":"L3NvdXJjZS//"}`
	original := "/source/" + string([]byte{0xff})
	var path homePath
	if err := json.Unmarshal([]byte(encoded), &path); err != nil || string(path) != original {
		t.Fatal("raw path changed", err)
	}
	raw, err := json.Marshal(path)
	if err != nil || string(raw) != encoded {
		t.Fatal("raw spelling changed", err)
	}
	for _, literal := range []string{`"/plain"`, `"/quote\"-backslash\\-tab\t"`, `"/literal-�"`} {
		if err := json.Unmarshal([]byte(literal), &path); err != nil {
			t.Fatal("Unicode path refused", literal, err)
		}
		raw, err := json.Marshal(path)
		if err != nil || string(raw) != literal {
			t.Fatal("Unicode spelling changed", err)
		}
	}
	for _, mutation := range []string{
		`null`, `{}`, `{"bytes":null}`, `{"bytes":""}`, `{"bytes":"L3BsYWlu"}`, `{"Bytes":"L3NvdXJjZS//"}`,
		`{"bytes":"L3NvdXJjZS//","bytes":"L3NvdXJjZS//"}`, `{"bytes":"L3NvdXJjZS//","extra":0}`,
		`{"bytes":"L3NvdXJjZS//\n"}`, `{"bytes":[255]}`, `"\/source"`, `"/\ud800"`, `42`,
	} {
		if err := json.Unmarshal([]byte(mutation), &path); err == nil {
			t.Fatal("open path sum", mutation)
		}
	}
}

// These are authored wire bytes, not a fixture captured from MarshalJSON.
func homeJournalV2Vector() []byte {
	const path = `{"bytes":"L3NvdXJjZS//"}`
	return []byte(`{"runnerBinary":` + path + `,"runnerDigest":"` + strings.Repeat("a", 64) + `","runnerRelocations":[{"version":1,"id":"` + strings.Repeat("b", 32) + `","dir":` + path + `,"workspace":"` + strings.Repeat("c", 32) + `","owner":"local-owner:` + strings.Repeat("d", 64) + `","previousInputRoot":` + path + `,"inputRoot":` + path + `}],"version":2,"destination":` + path + `,"config":` + path + `,"prelude":[{"path":` + path + `,"before":"absent","data":"AQI=","executable":true}],"renames":[{"from":` + path + `,"to":` + path + `,"identity":"1:2"}],"writes":[{"path":` + path + `,"before":"absent","data":"//4="}],"parent":` + path + `,"parentIdentity":"1:3","complete":false}`)
}
func TestDeskHomeJournalV2ExactPathsAndControls(t *testing.T) {
	raw := homeJournalV2Vector()
	tx, err := decodeHomeTransaction(raw)
	if err != nil {
		t.Fatal(err)
	}
	original := "/source/" + string([]byte{0xff})
	for _, path := range []string{tx.Config, tx.Destination, tx.Parent, tx.RunnerBinary, tx.Renames[0].From, tx.Renames[0].To, tx.Writes[0].Path, tx.Prelude[0].Path, tx.RunnerRelocations[0].Dir, tx.RunnerRelocations[0].PreviousInputRoot, tx.RunnerRelocations[0].InputRoot} {
		if path != original {
			t.Fatal("path bytes changed")
		}
	}
	if !bytes.Equal(tx.Writes[0].Data, []byte{0xff, 0xfe}) {
		t.Fatal("metadata bytes changed")
	}
	after, err := marshalHomeTransaction(*tx)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("v2 journal changed", err)
	}
	for _, mutation := range []string{
		strings.Replace(string(raw), `"version":2`, `"version":2,"version":2`, 1),
		strings.Replace(string(raw), `"version":2`, `"Version":2`, 1),
		strings.Replace(string(raw), `"version":2`, `"version":null`, 1),
		strings.Replace(string(raw), `"complete":false`, `"complete":null`, 1),
		strings.Replace(string(raw), `"complete":false`, `"complete":false,"extra":0`, 1),
		strings.Replace(string(raw), `"complete":false`, `"complete":false,"Complete":false`, 1),
		strings.Replace(string(raw), `"renames":[{"from":`, `"renames":[{"From":`, 1),
		strings.Replace(string(raw), `"before":"absent"`, `"before":null`, 1),
		strings.Replace(string(raw), `"data":"//4="`, `"data":null`, 1),
		strings.Replace(string(raw), `"bytes":"L3NvdXJjZS//"`, `"bytes":"L3NvdXJjZS//","bytes":"L3NvdXJjZS//"`, 1),
		strings.Replace(string(raw), `"parentIdentity":"1:3",`, ``, 1),
		string(raw) + "\n",
	} {
		if _, err := decodeHomeTransaction([]byte(mutation)); err == nil {
			t.Fatal("open v2 journal", mutation)
		}
	}
	// Emission upgrades only a raw path (including a nested Runner path).
	legacy := deskHomeTransaction{Version: 1, Destination: "/new", Config: "/old", Renames: []homeRename{}, Writes: []homeWrite{}}
	normal, err := marshalHomeTransaction(legacy)
	const expected = `{"version":1,"destination":"/new","config":"/old","renames":[],"writes":[],"parent":"","parentIdentity":"","complete":false}`
	if err != nil || string(normal) != expected {
		t.Fatal("UTF8 v1 shape changed", err, string(normal))
	}
	legacy.RunnerRelocations = []homeRunnerRelocation{{Version: 1, Dir: "/jobs", PreviousInputRoot: original, InputRoot: "/new"}}
	upgraded, err := marshalHomeTransaction(legacy)
	if err != nil || !bytes.Contains(upgraded, []byte(`"version":2`)) || !bytes.Contains(upgraded, []byte(`"previousInputRoot":{"bytes":`)) {
		t.Fatal("nested raw path not upgraded", err)
	}
	decoded, err := decodeHomeTransaction(upgraded)
	if err != nil || decoded.RunnerRelocations[0].PreviousInputRoot != original {
		t.Fatal("nested raw path lost", err)
	}
}

func TestDeskHomeJournalLegacyAndAggregateBounds(t *testing.T) {
	const legacy = `{"version":1,"destination":"/new","config":"/old","renames":[],"writes":[],"parent":"","parentIdentity":"","complete":false}`
	padded := []byte(legacy[:len(legacy)-1] + strings.Repeat(" ", homeTransactionLimit-len(legacy)) + "}")
	if len(padded) != homeTransactionLimit {
		t.Fatal("bad handwritten legacy size")
	}
	if _, err := decodeHomeTransaction(padded); err != nil {
		t.Fatal("legacy at unchanged reader limit refused", err)
	}
	if _, err := decodeHomeTransaction(append(padded, ' ')); err == nil {
		t.Fatal("oversize legacy admitted")
	}
	for _, version := range []string{"1", "2"} {
		t.Run("version-"+version, func(t *testing.T) {
			config := `"/old"`
			if version == "2" {
				config = `{"bytes":"L3NvdXJjZS//"}`
			}
			// Fixed authored JSON plus arithmetic padding gives exact4MiB and+1.
			shape := `{"version":` + version + `,"destination":"/new","config":` + config + `,"renames":[],"writes":[{"path":"/target","before":"absent","data":""}],"parent":"","parentIdentity":"","complete":false}`
			pathPadding := (homeTransactionLimit - len(shape)) % 4
			payloadChars := homeTransactionLimit - len(shape) - pathPadding
			vector := strings.Replace(shape, `"/target"`, `"/target`+strings.Repeat("x", pathPadding)+`"`, 1)
			vector = strings.Replace(vector, `"data":""`, `"data":"`+strings.Repeat("A", payloadChars)+`"`, 1)
			if len(vector) != homeTransactionLimit {
				t.Fatal("bad authored boundary", len(vector))
			}
			tx, err := decodeHomeTransaction([]byte(vector))
			if err != nil {
				t.Fatal("boundary decode", err)
			}
			encoded, err := marshalHomeTransaction(*tx)
			if err != nil || string(encoded) != vector {
				t.Fatal("boundary emission changed", err)
			}
			tx.Writes[0].Path += "x"
			if _, err := marshalHomeTransaction(*tx); err == nil {
				t.Fatal("oversize emission admitted")
			}
		})
	}
	tx := deskHomeTransaction{Version: 1, Destination: "/new", Config: "/old", Renames: make([]homeRename, 10001)}
	if _, err := marshalHomeTransaction(tx); err == nil {
		t.Fatal("rename count raised")
	}
	tx.Renames = nil
	tx.Writes = make([]homeWrite, 2001)
	if _, err := marshalHomeTransaction(tx); err == nil {
		t.Fatal("write count raised")
	}
	tx.Writes = nil
	tx.Prelude = make([]homeWrite, 2)
	if _, err := marshalHomeTransaction(tx); err == nil {
		t.Fatal("prelude count raised")
	}
}

func homeRegistryV2Vector() []byte {
	return []byte(`{"version":2,"owner":"` + strings.Repeat("a", 64) + `","startup":"` + strings.Repeat("b", 32) + `","desks":[{"id":"` + strings.Repeat("b", 32) + `","name":"Startup","folder":"startup","kind":"project","historyIdentity":{"bytes":"L3NvdXJjZS//"},"storageOwner":"` + strings.Repeat("a", 64) + `","runnerWorkspace":"` + strings.Repeat("c", 64) + `"}]}`)
}
func TestDeskHomeRegistryV2ClosedAndPublication(t *testing.T) {
	raw := homeRegistryV2Vector()
	registry, err := decodeHomeRegistry(raw)
	original := "/source/" + string([]byte{0xff})
	if err != nil || registry.Version != 2 || registry.Desks[0].HistoryIdentity != original {
		t.Fatal("history identity lost", err)
	}
	encoded, err := json.Marshal(registry)
	if err != nil || !bytes.Equal(raw, encoded) {
		t.Fatal("registry spelling changed", err)
	}
	var tx deskHomeTransaction
	if err := tx.addHomeRegistry("/new/_platform/desks.json", nil, registry); err != nil || !bytes.Equal(tx.Writes[0].Data, raw) {
		t.Fatal("transaction did not freeze canonical v2 registry", err)
	}
	for _, mutation := range []string{
		strings.Replace(string(raw), `"version":2`, `"version":2,"version":2`, 1),
		strings.Replace(string(raw), `"version":2`, `"Version":2`, 1),
		strings.Replace(string(raw), `"kind":"project"`, `"kind":"project","extra":0`, 1),
		strings.Replace(string(raw), `"kind":"project"`, `"kind":null`, 1),
		strings.Replace(string(raw), `"historyIdentity":{"bytes":"L3NvdXJjZS//"}`, `"historyIdentity":null`, 1),
		strings.Replace(string(raw), `"historyIdentity":`, `"HistoryIdentity":`, 1),
		strings.Replace(string(raw), `"storageOwner":"`+strings.Repeat("a", 64)+`",`, ``, 1),
		string(raw) + "\n",
	} {
		if _, err := decodeHomeRegistry([]byte(mutation)); err == nil {
			t.Fatal("open registry v2", mutation)
		}
	}
	config := filepath.Join(t.TempDir(), deskHomePlatform)
	if err := os.Mkdir(config, 0700); err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(config, deskHomeRegistryName), string(raw))
	root, err := os.OpenRoot(config)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	s := &Server{configDir: config, assistant: &assistantStore{root: root}, home: &deskHome{Path: filepath.Dir(config), Registry: registry}}
	if err := s.publishHomeEntry(strings.Repeat("d", 32), "Added", "added"); err != nil {
		t.Fatal("publication", err)
	}
	home, err := readDeskHome(config)
	if err != nil || home.Registry.Version != 2 || home.Registry.Desks[0].HistoryIdentity != original || len(home.Registry.Desks) != 2 {
		t.Fatal("publication downgraded raw identity", err)
	}
	namespace, err := assistantRunNamespace(root, home.Registry.Desks[0].HistoryIdentity)
	if err != nil || namespace != digestOf([]byte(original)) {
		t.Fatal("publication changed namespace", err)
	}
	// Existing UTF8 v1 registries retain their original codec and whitespace.
	legacy := strings.Replace(string(raw), `"version":2`, `"version":1`, 1)
	legacy = strings.Replace(legacy, `{"bytes":"L3NvdXJjZS//"}`, `"/original"`, 1)
	v1, err := decodeHomeRegistry([]byte(" \n" + legacy + "\n"))
	if err != nil {
		t.Fatal("legacy whitespace refused", err)
	}
	after, err := json.Marshal(v1)
	if err != nil || string(after) != legacy {
		t.Fatal("UTF8 registry v1 changed", err)
	}
	var legacyTx deskHomeTransaction
	if err := legacyTx.addHomeRegistry("/new/_platform/desks.json", nil, v1); err != nil || string(legacyTx.Writes[0].Data) != legacy+"\n" {
		t.Fatal("legacy registry transaction spelling changed", err)
	}
}

func TestDeskHomeRegistryV2HomeAnchorReader(t *testing.T) {
	s, r, child, _ := dialectArtifactHomeAnchorFixture(t, "")
	original := "/original/" + string([]byte{0xff})
	s.home.Registry.Version = 2
	s.home.Registry.Desks[0].HistoryIdentity = original
	child.home.Registry = s.home.Registry
	raw, err := json.Marshal(s.home.Registry)
	if err != nil {
		t.Fatal(err)
	}
	privateFixture(t, filepath.Join(s.configDir, deskHomeRegistryName), string(raw))
	enrolled, err := s.enrollDialectArtifactOwner(r)
	if err != nil || enrolled.Namespace != digestOf([]byte(original)) {
		t.Fatal("home anchor reader changed raw namespace", err)
	}
	hold, err := s.holdDialectArtifactHome(context.Background())
	if err != nil {
		t.Fatal("shared registry reader", err)
	}
	if err := hold.close(); err != nil {
		t.Fatal(err)
	}
}

func TestDeskHomePlanSnapshotBindsExactPathsAndNamespace(t *testing.T) {
	original := "/source/" + string([]byte{0xff})
	plan := deskHomePlan{Destination: "/new", Config: original, Parent: "/parent", ParentIdentity: "1:2", ChatPath: "/history", ChatConversation: "conversations-" + strings.Repeat("a", 64) + ".json",
		Registry: deskHomeRegistry{Version: 1, Owner: strings.Repeat("b", 64), Startup: strings.Repeat("c", 32), Desks: []deskHomeEntry{{ID: strings.Repeat("c", 32), Name: "Startup", Folder: "startup", Kind: "project", HistoryIdentity: original}}},
		Moves:    []deskHomeMove{{Label: "Move", From: original, To: "/new/startup", Identity: "1:3"}}}
	baseline, err := homePlanSnapshot(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*deskHomePlan){
		func(p *deskHomePlan) { p.Config = "/source/" + string([]byte{0xfe}) },
		func(p *deskHomePlan) { p.ChatPath = "/other-history" },
		func(p *deskHomePlan) { p.ChatConversation = "conversations-" + strings.Repeat("d", 64) + ".json" },
		func(p *deskHomePlan) { p.Moves[0].From = "/source/" + string([]byte{0xfe}) },
		func(p *deskHomePlan) { p.Moves[0].Identity = "1:4" },
		func(p *deskHomePlan) { p.Registry.Desks[0].HistoryIdentity = "/source/" + string([]byte{0xfe}) },
		func(p *deskHomePlan) { p.Registry.Desks[0].Folder = "another" },
	} {
		next := plan
		next.Moves = append([]deskHomeMove(nil), plan.Moves...)
		next.Registry.Desks = append([]deskHomeEntry(nil), plan.Registry.Desks...)
		change(&next)
		current, err := homePlanSnapshot(next)
		if err != nil || bytes.Equal(baseline, current) {
			t.Fatal("token projection aliased a changed path/namespace", err)
		}
	}
}
