package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Exercise the actual planner with its original holds and installed native
// qualification. Expected commitments are authored independently of the codec;
// no packet, qualification member or expected result is recorded from a run.
func TestStorageEnrollmentInitialFenceProducerOriginalPayloadDomain(t *testing.T) {
	requireStorageLine(t)
	s := ownerActualNew(t, ownerActualConfig(t))
	c := s.storageConstruction
	if c.ref.cell.enrollmentEndpoint != nil {
		t.Fatal("initial producer fixture already has an enrollment endpoint")
	}
	beforeQ, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	p, err := c.beginEnrollmentEpoch(t.Context())
	storageTestMust(t, err)
	defer func() {
		if err := p.finish(nil); err != nil {
			t.Error(err)
		}
	}()
	storageTestMust(t, p.selectOriginalRequester(t.Context(), s))
	storageTestMust(t, p.acquire(t.Context()))
	storageTestMust(t, p.captureInitialOriginal(t.Context()))
	storageTestMust(t, p.qualifyOriginalNative(t.Context()))
	originals := [2][]byte{append([]byte{}, p.original.Catalogs[0]...), append([]byte{}, p.original.Catalogs[1]...)}
	fenceRaw, _, err := p.planInitialProjectEnrollment(t.Context())
	storageTestMust(t, err)
	record, claim := *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim
	fence, err := storageEnrollmentPeakDecodeEnvelope(fenceRaw, record, claim, true)
	storageTestMust(t, err)
	if fence.Kind != 0 || fence.BeforeCodec != 0 || fence.Snapshot == nil || p.initialPacket == nil {
		t.Fatal("initial planner did not produce its original V3 Fence")
	}
	cores, _, err := storageEnrollmentOriginalCorePair(originals, record, claim)
	storageTestMust(t, err)
	original := cores[fence.Scope[322]]
	n := int(binary.LittleEndian.Uint32(original[24:28]))
	preimage := append([]byte("desk-enrollment-original-v3-inner-peak-v1\x00"), original[64:64+n]...)
	want := sha256.Sum256(preimage)
	plain := sha256.Sum256(original[64 : 64+n])
	if want == plain || p.initialPacket.BeforeInner != plain {
		t.Fatal("fixture failed to distinguish Fence payload domain from packet catalog commitment")
	}
	x := fence.Snapshot
	if x.Inner != want || x.Full != sha256.Sum256(original) || !bytes.Equal(x.Before, original) {
		t.Fatal("initial Fence did not retain the exact selected original core and domain commitment")
	}
	if p.initialPacket.Version == 2 {
		physical, err := storageCompositionCaptureProjection(originals[0], originals[1], record, claim)
		storageTestMust(t, err)
		if x.Encoding != 4 || x.Physical != physical {
			t.Fatal("initial Fence lost its actual physical-source projection")
		}
	} else if x.Physical != (storageCompositionProjection{}) {
		t.Fatal("initial Fence invented a physical-source projection")
	}
	bad := fence
	badSnapshot := *x
	badSnapshot.Inner = p.initialPacket.BeforeInner
	bad.Snapshot = &badSnapshot
	if _, err := storageEnrollmentPeakEncodeEnvelope(bad, record, claim); !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal("initial Fence accepted the packet's plain catalog hash")
	}
	if !bytes.Equal(p.original.Catalogs[0], originals[0]) || !bytes.Equal(p.original.Catalogs[1], originals[1]) || p.scope != nil || p.fenced || p.reservation.phase != nil {
		t.Fatal("read-only Fence planning changed originals or granted publication authority")
	}
	storageTestMust(t, p.finish(nil))
	afterQ, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(beforeQ, afterQ) || !c.owner.admissionOpen() {
		t.Fatal("read-only Fence planning changed Q or retained enrollment admission")
	}
}
