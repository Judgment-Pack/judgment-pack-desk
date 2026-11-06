package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Extend the existing independently authored Active V3 and retired-origin
// fixtures with the same handwritten Evidence2 framing/physical operands used
// by the direct composition-codec vectors. Decode the complete input first.
func compositionRetiredOriginEvidence2(t *testing.T) (epComplete, storageEnrollmentPeakEvidence, [2][]byte) {
	t.Helper()
	f := compositionTestActiveEvidencePhase(t, 1)
	e, _ := epRetiredEvidence(t, f, false, false)
	core := ownerAdvisoryWire(f.before)
	pair, _ := compositionTestBasePair(core, 1, f.before.PreviousCatalogDigest)
	pair = [2][]byte{pair[1], pair[0]} // independently declared selected physical bank1
	physical := compositionTestProjection(pair)
	lineage := storageCompositionEmptyLineage()
	s := e.Scope
	if s[322] != 1 {
		t.Fatal("fixture lost its declared selected physical bank1")
	}
	binary.LittleEndian.PutUint16(s[:2], 2)
	d := sha256.Sum256(core)
	copy(s[1640:1672], d[:])
	copy(s[1672:1704], f.before.PreviousCatalogDigest[:])
	s[1704] = 1
	normalized := s
	clear(normalized[184:216])
	normalized[321] = 0
	clear(normalized[1080:1220])
	d = epHash("desk-enrollment-control-peak-v2", normalized[:])
	copy(s[184:216], d[:])
	e.Scope, e.Version, e.CompositionLineage = s, 2, &lineage
	e.Sections[8].Raw = append([]byte{}, e.Sections[8].Raw...)
	copy(e.Sections[8].Raw[88:120], s[184:216])
	cp := e.Checkpoint
	// The retired fixture declares identical original Q banks; supply its
	// exact opposite bank to the existing peer-aware checkpoint constructor.
	cp.PeerCheckpoint = append([]byte{}, cp.Checkpoint...)
	var err error
	e.Sections[6].Raw, err = storageEnrollmentPeakWriteCheckpoint(cp, e.Inner.Catalog, e.Views, s)
	storageTestMust(t, err)
	e.Sections[4].Raw = append(append([]byte{}, e.Sections[4].Raw...), physical[:]...)
	decoded, err := storageEnrollmentPeakDecodeEvidenceWithLineage(compositionTestEvidenceV2Frame(e), &lineage, f.record, f.claim)
	if err != nil || decoded.Version != 2 || decoded.CompositionLineage == nil || len(decoded.Board.Origins) != 1 || decoded.Projections[0].Physical != physical {
		t.Fatal("independent composed retired-origin fixture", err)
	}
	return f, decoded, pair
}

func TestStorageCompositionRetainedOriginProjectionRebase(t *testing.T) {
	f, previous, retainedPair := compositionRetiredOriginEvidence2(t)
	retainedPhysical := previous.Projections[0].Physical
	core := ownerAdvisoryWire(f.before)
	// A second independently authored valid physical pair has the same logical
	// core and exact empty lineage, with distinct physical generations/checksums.
	block := compositionTestBlock([]byte(compositionEmptyJSON), [4][]storagebudget.ID{}, nil)
	v := storageCompositionBank{Sequence: 50, CoreCodec: 1, Publication: 1, Transition: 1, TransitionID: storagebudget.ID{17}, Previous: storageCompositionChecksum{18}, FirstBase: storagebudget.Digest{19}}
	d := sha256.Sum256(core)
	copy(v.BeforePeer[1:33], d[:])
	copy(v.BeforePeer[33:65], f.before.PreviousCatalogDigest[:])
	prepared := compositionTestFrame(v, core, block)
	v.Sequence, v.Publication = 51, 2
	copy(v.Previous[:], prepared[32:64])
	active := compositionTestFrame(v, core, block)
	currentPair := [2][]byte{prepared, active}
	currentPhysical := compositionTestProjection(currentPair)
	if retainedPhysical == currentPhysical {
		t.Fatal("fixture requires distinct original physical pairs")
	}
	// Reuse the parsed exact retained lineage, never derive it from catalog
	// rows. Both physical pairs explicitly declare its zero historical prefix.
	projectionBody := previous.Sections[4].Raw[:len(previous.Sections[4].Raw)-storageCompositionProjectionBytes]
	makeCurrent := func() storageEnrollmentPeakEvidence {
		current := previous
		current.Views = append([]storageEnrollmentPeakView(nil), previous.Views...)
		current.Projections = append([]storageEnrollmentPeakProjection(nil), previous.Projections...)
		current.Projections[0].Physical = currentPhysical
		current.Projections[0].Raw = append(append([]byte{}, projectionBody...), currentPhysical[:]...)
		current.Sections[4].Raw = current.Projections[0].Raw
		return current
	}
	rebase := func(current *storageEnrollmentPeakEvidence, prior *storageEnrollmentPeakEvidence) error {
		epoch := &storageEnrollmentEpoch{
			c:                &storageBudgetConstruction{ref: &storageInstallationRef{cell: &storageInstallation{ownerRecord: &f.record, ownerClaim: f.claim}}},
			previousEvidence: prior,
		}
		return epoch.rebaseEnrollmentOrigins(current)
	}
	t.Run("lossless", func(t *testing.T) {
		current := makeCurrent()
		storageTestMust(t, rebase(&current, &previous))
		if len(current.Projections) != 2 || current.Sections[4].Count != 2 || !storageEnrollmentPeakRetainsOrigins(current, previous) {
			t.Fatal("composed current and retained projections were not both retained")
		}
		origin := current.Board.Origins[current.Witnesses[0].Origin]
		if current.Projections[origin.Projection].Physical != retainedPhysical {
			t.Fatal("origin remapping replaced its exact original physical operand")
		}
		want := append(append([]byte{}, projectionBody...), retainedPhysical[:]...)
		want = append(want, projectionBody...)
		want = append(want, currentPhysical[:]...)
		// Equal authored core bytes precede the physical field; generation40
		// sorts before50 in the independently fixed little-endian prefix.
		if !bytes.Equal(current.Sections[4].Raw, want) {
			t.Fatal("composed interning changed the exact independently authored bodies")
		}
		seenCurrent, seenRetained := false, false
		for _, projection := range current.Projections {
			expected := retainedPair
			switch projection.Physical {
			case retainedPhysical:
				seenRetained = true
			case currentPhysical:
				seenCurrent = true
				expected = currentPair
			default:
				t.Fatal("rebasing erased or invented a physical operand")
			}
			decoded, err := storageEnrollmentPeakReadProjectionV2(projection.Raw, current.Inner.Catalog, current.OriginalV3, current.CompositionLineage, f.record, f.claim)
			if err != nil || !reflect.DeepEqual(decoded, projection) {
				t.Fatal("full V2 projection roundtrip changed retained data", err)
			}
			logical, err := storageEnrollmentPeakProjectionCore(decoded)
			storageTestMust(t, err)
			physical, err := decoded.Physical.restore(logical, *current.CompositionLineage, f.record, f.claim)
			if err != nil || !bytes.Equal(physical[0], expected[0]) || !bytes.Equal(physical[1], expected[1]) {
				t.Fatal("rebased projection lost exact original physical banks", err)
			}
			if _, err = storageEnrollmentPeakReadProjection(projection.Raw, current.Inner.Catalog, current.OriginalV3, f.record, f.claim); err == nil {
				t.Fatal("full composed body was accepted through legacy projection codec")
			}
		}
		if !seenCurrent || !seenRetained {
			t.Fatal("distinct current/retained physical histories collapsed")
		}
	})
	for _, name := range []string{"missing-lineage", "corrupt-current-physical", "corrupt-retained-physical", "legacy-version"} {
		t.Run(name, func(t *testing.T) {
			current := makeCurrent()
			prior := previous
			prior.Projections = append([]storageEnrollmentPeakProjection(nil), previous.Projections...)
			switch name {
			case "missing-lineage":
				current.CompositionLineage = nil
			case "corrupt-current-physical":
				current.Projections[0].Physical[648] ^= 1
			case "corrupt-retained-physical":
				prior.Projections[0].Physical[648] ^= 1
			case "legacy-version":
				current.Version = 1
			}
			if err := rebase(&current, &prior); err == nil {
				t.Fatal("rebase accepted missing, corrupted or incorrectly interpreted physical history")
			}
		})
	}
}
