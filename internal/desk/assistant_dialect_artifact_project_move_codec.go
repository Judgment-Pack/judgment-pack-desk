package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Only current locator bytes are changed by a same-inode project move. The
// immutable owner, scope, marker and possibly long work pathname are selected
// by exact hashes, and must still exist at replay. They are never reconstructed
// from this journal. This closed form is not an object-read capability.
type dialectArtifactProjectMovePlan struct {
	Version                int    `json:"version"`
	Owner                  string `json:"owner"`
	Kind                   string `json:"kind"`
	LegacySHA256           string `json:"legacySHA256"`
	MarkerSHA256           string `json:"markerSHA256"`
	Namespace              string `json:"namespace"`
	NativeScopeSHA256      string `json:"nativeScopeSHA256"`
	WorkDomain             string `json:"workDomain"`
	WorkSHA256             string `json:"workSHA256"`
	Before                 []byte `json:"before,omitempty"`
	After                  []byte `json:"after,omitempty"`
	Provider               string `json:"provider,omitempty"`
	ProviderIdentitySHA256 string `json:"providerIdentitySHA256,omitempty"`
	ProviderOwnerSHA256    string `json:"providerOwnerSHA256,omitempty"`
	ProviderBindingSHA256  string `json:"providerBindingSHA256,omitempty"`
}

// The accepted identity/path domain bounds an entire newly produced confirmed
// intent by 5898 bytes (including a future fixed receipt); existing v2 rows are
// smaller. The existing 16 KiB legacy decoder remains unchanged. The companion
// codec remains v2 until the separately reviewed b9 identity transition exists.
const dialectArtifactProjectMovePlanBytes = 4839

func (p dialectArtifactProjectMovePlan) valid() bool {
	if p.Version != 1 || !assistantRunDigest(p.Owner) || !assistantRunDigest(p.LegacySHA256) ||
		!assistantRunDigest(p.MarkerSHA256) || !assistantRunDigest(p.Namespace) ||
		!assistantRunDigest(p.NativeScopeSHA256) || !assistantRunDigest(p.WorkDomain) || !assistantRunDigest(p.WorkSHA256) {
		return false
	}
	switch p.Kind {
	case "file":
		if p.Provider != "" || p.ProviderIdentitySHA256 != "" || p.ProviderOwnerSHA256 != "" || p.ProviderBindingSHA256 != "" {
			return false
		}
		before, e1 := readDialectArtifactLocationRecord(p.Before)
		after, e2 := readDialectArtifactLocationRecord(p.After)
		return e1 == nil && e2 == nil && before.Owner == p.Owner && after.Owner == p.Owner &&
			before.LegacySHA256 == p.LegacySHA256 && after.LegacySHA256 == p.LegacySHA256 &&
			before.FolderIdentity == after.FolderIdentity && before.WorkDomain == p.WorkDomain && after.WorkDomain == p.WorkDomain &&
			before.Location.Kind != "home" && after.Location.Kind != "home" && !bytes.Equal(p.Before, p.After)
	case "injected":
		return p.Before == nil && p.After == nil && assistantRunDigest(p.Provider) &&
			assistantRunDigest(p.ProviderIdentitySHA256) && assistantRunDigest(p.ProviderOwnerSHA256) && assistantRunDigest(p.ProviderBindingSHA256)
	}
	return false
}

func (p *dialectArtifactProjectMovePlan) UnmarshalJSON(raw []byte) error {
	value, err := readDialectArtifactProjectMovePlan(raw)
	if err != nil {
		return err
	}
	*p = value
	return nil
}

func readDialectArtifactProjectMovePlan(raw []byte) (dialectArtifactProjectMovePlan, error) {
	type plain dialectArtifactProjectMovePlan
	var value plain
	if err := readDialectArtifactOwnerJSON(raw, dialectArtifactProjectMovePlanBytes, &value); err != nil {
		return dialectArtifactProjectMovePlan{}, err
	}
	if !dialectArtifactProjectMovePlan(value).valid() {
		return dialectArtifactProjectMovePlan{}, errDialectArtifactOwner
	}
	return dialectArtifactProjectMovePlan(value), nil
}

// The locator is compared with recorded history paths as data; this function
// never opens either pathname. Actual root/work descriptor checks are separate.
func dialectArtifactMoveLocation(path, config, work string) dialectArtifactLocation {
	rel, err := filepath.Rel(config, path)
	if err == nil && filepath.IsLocal(rel) {
		return dialectArtifactLocation{Kind: "work", Anchor: work, PathBytes: []byte(rel)}
	}
	return dialectArtifactLocation{Kind: "absolute", PathBytes: []byte(path)}
}

func (p *dialectArtifactProjectMovePlan) binds(previous, destination, folder, config string) bool {
	if p == nil {
		return true
	}
	if !p.valid() {
		return false
	}
	if p.Kind == "injected" {
		return true // actual immutable provider binding supplies the folder pin
	}
	before, _ := readDialectArtifactLocationRecord(p.Before)
	after, _ := readDialectArtifactLocationRecord(p.After)
	a, _ := json.Marshal(before.Location)
	b, _ := json.Marshal(dialectArtifactMoveLocation(previous, config, p.WorkDomain))
	c, _ := json.Marshal(after.Location)
	d, _ := json.Marshal(dialectArtifactMoveLocation(destination, config, p.WorkDomain))
	return before.FolderIdentity == folder && bytes.Equal(a, b) && bytes.Equal(c, d)
}

type storageMovePublication struct {
	RequestDigest storagebudget.Digest
	Receipt       storageOwnerV5Receipt
	Body          []byte
}

const storageMoveFrameHeader = 64
const storageMoveReceiptBytes = 261

func storageMoveTokenBytes(t storagebudget.Token) []byte {
	out := make([]byte, 33)
	out[0] = t.Slot
	copy(out[1:17], t.ID[:])
	binary.LittleEndian.PutUint64(out[17:25], t.Sequence)
	binary.LittleEndian.PutUint64(out[25:33], t.Generation)
	return out
}
func storageMoveReadToken(r *storageEnrollmentPeakReader) storagebudget.Token {
	var t storagebudget.Token
	t.Slot = r.u8()
	copy(t.ID[:], r.take(16))
	t.Sequence = r.u64()
	t.Generation = r.u64()
	return t
}
func storageMoveReceiptWire(v storageOwnerV5Receipt) []byte {
	b := make([]byte, 0, storageMoveReceiptBytes)
	b = append(b, v.OperationID[:]...)
	b = append(b, storageMoveTokenBytes(v.Terminal)...)
	b = append(b, storageMoveTokenBytes(v.PersistedApplied)...)
	for _, n := range []uint64{v.PersistedNextSequence, v.RetirementEpoch} {
		b = binary.LittleEndian.AppendUint64(b, n)
	}
	b = append(b, v.IntentDigest[:]...)
	b = append(b, v.TicketDigest[:]...)
	b = append(b, storageEnrollmentPeakNativeBytes(v.PublishedObject)...)
	b = append(b, v.PublishedBytesDigest[:]...)
	b = append(b, v.PublicationEvidence[:]...)
	return b
}
func storageMoveReadReceipt(r *storageEnrollmentPeakReader) storageOwnerV5Receipt {
	var v storageOwnerV5Receipt
	copy(v.OperationID[:], r.take(16))
	v.Terminal = storageMoveReadToken(r)
	v.PersistedApplied = storageMoveReadToken(r)
	v.PersistedNextSequence = r.u64()
	v.RetirementEpoch = r.u64()
	copy(v.IntentDigest[:], r.take(32))
	copy(v.TicketDigest[:], r.take(32))
	v.PublishedObject = r.native()
	copy(v.PublishedBytesDigest[:], r.take(32))
	copy(v.PublicationEvidence[:], r.take(32))
	return v
}
func storageMoveDigestBytes(s string) []byte { b, _ := hex.DecodeString(s); return b }
func storageMoveAppendBytes(out, b []byte) []byte {
	out = binary.LittleEndian.AppendUint16(out, uint16(len(b)))
	return append(out, b...)
}
func storageMoveReadBytes(r *storageEnrollmentPeakReader, limit int) []byte {
	n := int(r.u16())
	if n > limit {
		r.bad = true
		return nil
	}
	return bytes.Clone(r.take(n))
}
func storageMoveSeal(magic string, body []byte) []byte {
	out := make([]byte, storageMoveFrameHeader, len(body)+storageMoveFrameHeader)
	copy(out[:8], magic)
	binary.LittleEndian.PutUint32(out[8:12], uint32(len(body)+storageMoveFrameHeader))
	d := sha256.Sum256(body)
	copy(out[16:48], d[:])
	return append(out, body...)
}
func storageMoveOpen(raw []byte, magic string, limit int) ([]byte, bool) {
	if len(raw) < storageMoveFrameHeader || len(raw) > limit || string(raw[:8]) != magic || uint64(binary.LittleEndian.Uint32(raw[8:12])) != uint64(len(raw)) || !bytes.Equal(raw[12:16], make([]byte, 4)) || !bytes.Equal(raw[48:64], make([]byte, 16)) {
		return nil, false
	}
	d := sha256.Sum256(raw[64:])
	if !bytes.Equal(d[:], raw[16:48]) {
		return nil, false
	}
	return raw[64:], true
}

// V2 keeps the original move file and its unchanged finite producer bound.
// The immutable storage request supplies the two existing paths/config CAS;
// artifact locator operands remain exact original raw bytes, when present.
// No companion or receipt includes this file's own future publication hash.
const storageMoveIntentMaximum = 5898

func storageMoveEncodeIntent(v projectMoveIntent) ([]byte, error) {
	if v.Version != 2 || v.Storage == nil || v.Previous != string(v.Storage.Previous) || v.Destination != string(v.Storage.Destination) || !startupIDForm.MatchString(v.Identity) || !fileIdentityForm.MatchString(v.Folder) || len(v.Folder) > 41 {
		return nil, errDialectArtifactOwner
	}
	expectedRevision := ""
	if v.Storage.ConfigRevision != (storagebudget.Digest{}) {
		expectedRevision = hex.EncodeToString(v.Storage.ConfigRevision[:])
	}
	if v.ConfigRevision != expectedRevision {
		return nil, errDialectArtifactOwner
	}
	if !v.Storage.artifactPlanBound(v.Retained) {
		return nil, errDialectArtifactOwner
	}
	request, err := v.Storage.encode()
	if err != nil {
		return nil, err
	}
	w := storageEnrollmentPeakWriter{max: storageMoveIntentMaximum - storageMoveFrameHeader}
	w.put(storageMoveAppendBytes(nil, request))
	w.put(storageMoveDigestBytes(v.Identity))
	w.put(storageMoveAppendBytes(nil, []byte(v.Folder)))
	if v.Retained == nil {
		w.u8(0)
	} else {
		p := v.Retained
		legacy, e := json.Marshal(p)
		if e != nil || !p.valid() || len(legacy) > dialectArtifactProjectMovePlanBytes {
			return nil, errDialectArtifactOwner
		}
		w.u8(1)
		for _, s := range []string{p.Owner, p.LegacySHA256, p.MarkerSHA256, p.Namespace, p.NativeScopeSHA256, p.WorkDomain, p.WorkSHA256} {
			w.put(storageMoveDigestBytes(s))
		}
		if p.Kind == "file" {
			w.u8(1)
			w.put(storageMoveAppendBytes(nil, p.Before))
			w.put(storageMoveAppendBytes(nil, p.After))
		} else {
			w.u8(2)
			for _, s := range []string{p.Provider, p.ProviderIdentitySHA256, p.ProviderOwnerSHA256, p.ProviderBindingSHA256} {
				w.put(storageMoveDigestBytes(s))
			}
		}
	}
	if w.bad {
		return nil, errDialectArtifactOwner
	}
	return storageMoveSeal("DSKPMOV2", w.raw), nil
}
func storageMoveReadIntent(raw []byte) (projectMoveIntent, error) {
	var out projectMoveIntent
	body, ok := storageMoveOpen(raw, "DSKPMOV2", storageMoveIntentMaximum)
	if !ok {
		return out, errDialectArtifactOwner
	}
	r := storageEnrollmentPeakReader{raw: body}
	request, err := storageMoveReadRequest(storageMoveReadBytes(&r, storageMoveRequestMaximum))
	if err != nil {
		return out, err
	}
	out.Version, out.Storage, out.Previous, out.Destination = 2, &request, string(request.Previous), string(request.Destination)
	out.Identity = hex.EncodeToString(r.take(32))
	out.Folder = string(storageMoveReadBytes(&r, 41))
	if request.ConfigRevision != (storagebudget.Digest{}) {
		out.ConfigRevision = hex.EncodeToString(request.ConfigRevision[:])
	}
	switch r.u8() {
	case 0:
	case 1:
		p := &dialectArtifactProjectMovePlan{Version: 1}
		for _, target := range []*string{&p.Owner, &p.LegacySHA256, &p.MarkerSHA256, &p.Namespace, &p.NativeScopeSHA256, &p.WorkDomain, &p.WorkSHA256} {
			*target = hex.EncodeToString(r.take(32))
		}
		switch r.u8() {
		case 1:
			p.Kind = "file"
			p.Before = storageMoveReadBytes(&r, dialectArtifactLocationRecordBytes)
			p.After = storageMoveReadBytes(&r, dialectArtifactLocationRecordBytes)
		case 2:
			p.Kind = "injected"
			for _, target := range []*string{&p.Provider, &p.ProviderIdentitySHA256, &p.ProviderOwnerSHA256, &p.ProviderBindingSHA256} {
				*target = hex.EncodeToString(r.take(32))
			}
		default:
			r.bad = true
		}
		out.Retained = p
	default:
		r.bad = true
	}
	if !r.done() {
		return projectMoveIntent{}, errDialectArtifactOwner
	}
	canonical, err := storageMoveEncodeIntent(out)
	if err != nil || !bytes.Equal(canonical, raw) {
		return projectMoveIntent{}, errDialectArtifactOwner
	}
	return out, nil
}
