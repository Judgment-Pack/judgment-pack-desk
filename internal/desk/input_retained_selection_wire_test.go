package desk

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type selectionWireFixtureRow struct {
	Index         int    `json:"index"`
	ID            string `json:"id"`
	InputHex      string `json:"inputHex"`
	InputBytes    int64  `json:"inputBytes"`
	InputSHA256   string `json:"inputSha256"`
	MetadataJSON  string `json:"metadataJSON"`
	FrameOffset   int64  `json:"frameOffset"`
	PayloadOffset int64  `json:"payloadOffset"`
}
type selectionWireFixture struct {
	Name            string                      `json:"name"`
	File            string                      `json:"file"`
	Source          retainedSelectionWireSource `json:"source"`
	ViewID          string                      `json:"viewId"`
	HeaderJSON      string                      `json:"headerJSON"`
	HeaderBytes     int                         `json:"headerBytes"`
	SelectionSHA256 string                      `json:"selectionSha256"`
	IndexSHA256     string                      `json:"indexSha256"`
	EndJSON         string                      `json:"endJSON"`
	EndOffset       int64                       `json:"endOffset"`
	Rows            []selectionWireFixtureRow   `json:"rows"`
	Bytes           int64                       `json:"bytes"`
	SHA256          string                      `json:"sha256"`
}
type selectionWireEdit struct {
	Offset int    `json:"offset"`
	Remove int    `json:"remove"`
	Hex    string `json:"hex"`
}
type selectionWireMutation struct {
	Name          string              `json:"name"`
	Base          string              `json:"base"`
	Edits         []selectionWireEdit `json:"edits"`
	ContentLength *int64              `json:"contentLength"`
	RecordIDs     []string            `json:"recordIds"`
}
type selectionWireManifest struct {
	Positives       []selectionWireFixture  `json:"positives"`
	InvalidFixtures []selectionWireFixture  `json:"invalidFixtures"`
	Mutations       []selectionWireMutation `json:"mutations"`
}

func selectionWireManifestForTest(t *testing.T) selectionWireManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "retained-selection-wire", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest selectionWireManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}
func selectionWireHex(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(strings.TrimSpace(text))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func selectionWireRaw(t *testing.T, fixture selectionWireFixture) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "retained-selection-wire", fixture.File))
	if err != nil {
		t.Fatal(err)
	}
	body := selectionWireHex(t, string(raw))
	if int64(len(body)) != fixture.Bytes || retainedSelectionHash(body) != fixture.SHA256 {
		t.Fatal("independent fixture integrity mismatch")
	}
	return body
}
func selectionWireReadPositive(t *testing.T, fixture selectionWireFixture, in io.Reader) {
	t.Helper()
	calls := 0
	header, rows, err := readRetainedSelectionWire(in, fixture.Source, fixture.Bytes, func(row retainedSelectionWireDescriptor, input []byte) error {
		want := fixture.Rows[calls]
		if row.Index != want.Index || row.ID != want.ID || row.Offset != want.PayloadOffset || row.InputBytes != want.InputBytes || row.InputSHA256 != want.InputSHA256 || !bytes.Equal(input, selectionWireHex(t, want.InputHex)) {
			t.Fatal("row bytes/identity/offset changed")
		}
		calls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != len(fixture.Rows) || len(rows) != calls || header.SelectionSHA256 != fixture.SelectionSHA256 {
		t.Fatal("incomplete descriptors/header")
	}
	raw, err := retainedSelectionJSON(header)
	if err != nil || string(raw) != fixture.HeaderJSON {
		t.Fatal("header spelling differs")
	}
}
func TestRetainedSelectionWireHandwrittenSharedFixtures(t *testing.T) {
	manifest := selectionWireManifestForTest(t)
	for _, fixture := range manifest.Positives {
		t.Run(fixture.Name, func(t *testing.T) {
			body := selectionWireRaw(t, fixture)
			selectionWireReadPositive(t, fixture, bytes.NewReader(body))
			selectionWireReadPositive(t, fixture, &selectionWireByteReader{raw: body})
			var out bytes.Buffer
			writer, err := newRetainedSelectionWireWriter(&out, fixture.Source, fixture.ViewID)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range fixture.Rows {
				descriptor, err := writer.Add(row.ID, selectionWireHex(t, row.InputHex))
				if err != nil || descriptor.Offset != row.PayloadOffset {
					t.Fatalf("writer row: %v", err)
				}
				metadata, err := retainedSelectionJSON(descriptor.retainedSelectionWireRow)
				if err != nil || string(metadata) != row.MetadataJSON {
					t.Fatal("metadata spelling differs")
				}
				if len(metadata) != 111+len(strconvSelectionWire(row.Index))+len(strconvSelectionWire(int(row.InputBytes)))+len(selectionWireQuoted(t, row.ID)) {
					t.Fatal("row literal accounting differs")
				}
			}
			size, err := writer.Finish()
			if err != nil || size != fixture.Bytes || !bytes.Equal(out.Bytes(), body) {
				t.Fatalf("writer differs from handwritten wire: size=%d err=%v", size, err)
			}
			tuple, _ := retainedSelectionJSON(fixture.Source.ConversionContract)
			headerExpected := 412 + len(selectionWireQuoted(t, fixture.Source.PreparationID)) + len(tuple) + len(selectionWireQuoted(t, fixture.Source.Origin)) + len(strconvSelectionWire(len(fixture.Rows)))
			if fixture.HeaderBytes != headerExpected {
				t.Fatal("header literal accounting differs")
			}
			var total int64
			for _, row := range fixture.Rows {
				total += row.InputBytes
			}
			if len(fixture.EndJSON) != 109+len(strconvSelectionWire(len(fixture.Rows)))+len(strconvSelectionWire(int(total))) {
				t.Fatal("end literal accounting differs")
			}
		})
	}
}
func selectionWireQuoted(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func strconvSelectionWire(n int) string { raw, _ := json.Marshal(n); return string(raw) }

func TestRetainedSelectionWireEveryCompactSplitAndTruncation(t *testing.T) {
	fixture := selectionWireManifestForTest(t).Positives[0]
	body := selectionWireRaw(t, fixture)
	for split := 0; split <= len(body); split++ {
		selectionWireReadPositive(t, fixture, io.MultiReader(bytes.NewReader(body[:split]), bytes.NewReader(body[split:])))
	}
	selectionWireReadPositive(t, fixture, &selectionWireByteReader{raw: body})
	for cut := 0; cut < len(body); cut++ {
		if _, rows, err := readRetainedSelectionWire(bytes.NewReader(body[:cut]), fixture.Source, fixture.Bytes, nil); err == nil || rows != nil {
			t.Fatalf("accepted truncation at %d", cut)
		}
	}
}

type selectionWireByteReader struct{ raw []byte }

func (r *selectionWireByteReader) Read(out []byte) (int, error) {
	if len(r.raw) == 0 {
		return 0, io.EOF
	}
	if len(out) == 0 {
		return 0, nil
	}
	out[0] = r.raw[0]
	r.raw = r.raw[1:]
	return 1, nil
}
func TestRetainedSelectionWireSharedRefusals(t *testing.T) {
	manifest := selectionWireManifestForTest(t)
	fixtures := map[string]selectionWireFixture{}
	for _, fixture := range manifest.Positives {
		fixtures[fixture.Name] = fixture
	}
	for _, mutation := range manifest.Mutations {
		t.Run(mutation.Name, func(t *testing.T) {
			fixture := fixtures[mutation.Base]
			body := selectionWireRaw(t, fixture)
			for _, edit := range mutation.Edits {
				replacement := selectionWireHex(t, edit.Hex)
				changed := append([]byte(nil), body[:edit.Offset]...)
				changed = append(changed, replacement...)
				body = append(changed, body[edit.Offset+edit.Remove:]...)
			}
			source := fixture.Source
			if mutation.RecordIDs != nil {
				source.RecordIDs = mutation.RecordIDs
			}
			length := int64(len(body))
			if mutation.ContentLength != nil {
				length = *mutation.ContentLength
			}
			if _, rows, err := readRetainedSelectionWire(&selectionWireByteReader{raw: body}, source, length, nil); err == nil || rows != nil {
				t.Fatal("accepted shared mutation")
			}
		})
	}
	for _, fixture := range manifest.InvalidFixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			body := selectionWireRaw(t, fixture)
			if _, rows, err := readRetainedSelectionWire(bytes.NewReader(body), fixture.Source, fixture.Bytes, nil); err == nil || rows != nil {
				t.Fatal("accepted invalid UTF-8 with consistent digests")
			}
			var out bytes.Buffer
			writer, err := newRetainedSelectionWireWriter(&out, fixture.Source, fixture.ViewID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = writer.Add(fixture.Rows[0].ID, selectionWireHex(t, fixture.Rows[0].InputHex)); err == nil {
				t.Fatal("writer accepted malformed UTF-8")
			}
		})
	}
}
func TestRetainedSelectionWireWriterFailuresAndBounds(t *testing.T) {
	fixture := selectionWireManifestForTest(t).Positives[0]
	newWriter := func() *retainedSelectionWireWriter {
		t.Helper()
		w, err := newRetainedSelectionWireWriter(io.Discard, fixture.Source, fixture.ViewID)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	for _, id := range []string{"", "wrong"} {
		w := newWriter()
		if _, err := w.Add(id, []byte("{}")); err == nil {
			t.Fatal("wrong identity accepted")
		}
		if _, err := w.Finish(); err == nil {
			t.Fatal("poisoned writer finished")
		}
	}
	w := newWriter()
	if _, err := w.Finish(); err == nil {
		t.Fatal("missing row accepted")
	}
	w = newWriter()
	if _, err := w.Add("r", nil); err == nil {
		t.Fatal("empty row accepted")
	}
	w = newWriter()
	if _, err := w.Add("r", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("r", []byte("{}")); err == nil {
		t.Fatal("extra row accepted")
	}
	for _, delta := range []int64{0, -1} {
		w := newWriter()
		w.bound = fixture.Bytes + delta // Test-only downward threshold, no new production cap.
		if _, err := w.Add("r", []byte("{}")); err != nil {
			t.Fatal(err)
		}
		n, err := w.Finish()
		if delta == 0 && (err != nil || n != fixture.Bytes) {
			t.Fatal("exact bound failed")
		}
		if delta < 0 && err == nil {
			t.Fatal("one-over bound accepted")
		}
		if _, err := w.Finish(); err == nil {
			t.Fatal("finished/failed writer reused")
		}
	}
	if _, err := newRetainedSelectionWireWriter(selectionWireShortWriter{}, fixture.Source, fixture.ViewID); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write: %v", err)
	}
	if _, err := retainedSelectionAdd(math.MaxInt64, 1); err == nil {
		t.Fatal("counter overflow accepted")
	}
	if _, err := retainedSelectionAdd(-1, 0); err == nil {
		t.Fatal("negative counter accepted")
	}
	if n, err := retainedSelectionAdd(math.MaxInt64-1, 1); err != nil || n != math.MaxInt64 {
		t.Fatal("exact counter boundary failed")
	}
	// A long legitimate ID is constrained by the actual request, not a guessed
	// per-ID/header cap; no payload or aggregate needs to be allocated here.
	source := fixture.Source
	source.RecordIDs = []string{strings.Repeat("<&", 10000)}
	if _, err := newRetainedSelectionWireWriter(io.Discard, source, fixture.ViewID); err != nil {
		t.Fatal("new arbitrary ID bound", err)
	}
	source.RequestBytes = int64(len(source.RecordIDs[0])) - 1
	if _, err := newRetainedSelectionWireWriter(io.Discard, source, fixture.ViewID); err == nil {
		t.Fatal("request owner ignored")
	}
}

type selectionWireShortWriter struct{}

func (selectionWireShortWriter) Write(raw []byte) (int, error) { return len(raw) - 1, nil }

func TestRetainedSelectionWireProvisionalCallbackFailure(t *testing.T) {
	fixture := selectionWireManifestForTest(t).Positives[1]
	body := selectionWireRaw(t, fixture)
	stop := errors.New("consumer stopped")
	calls := 0
	_, rows, err := readRetainedSelectionWire(bytes.NewReader(body), fixture.Source, fixture.Bytes, func(retainedSelectionWireDescriptor, []byte) error { calls++; return stop })
	if !errors.Is(err, stop) || calls != 1 || rows != nil {
		t.Fatal("callback failure became success")
	}
}
