package desk

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// The existing handwritten Armed fixture supplies the expected complete image.
// A path2 plan has no serialized Staged/Board section to inherit its count from.
func TestStorageEnrollmentPeakArmPlannedNoGrowthBoardSection(t *testing.T) {
	fixture, _, _ := epExisting(t, 2, 1)
	armed := epDecode(t, fixture)
	planned := armed
	planned.Phase = 0
	planned.Raw = nil
	planned.Sections[8] = storageEnrollmentPeakSection{}
	placeholder := storageEnrollmentPeakQCensus{}
	for _, row := range armed.Census.Roots {
		placeholder.Roots = append(placeholder.Roots, storageEnrollmentPeakQRootMeasurement{ID: row.ID, Generation: row.Generation})
	}
	for _, row := range armed.Census.Workspaces {
		placeholder.Workspaces = append(placeholder.Workspaces, storageEnrollmentPeakQWorkspaceMeasurement{ID: row.ID, Generation: row.Generation})
	}
	var err error
	planned.Sections[7].Raw, err = storageEnrollmentPeakWriteCensus(placeholder, planned.Predictions[1], 0)
	storageTestMust(t, err)
	planned.CensusDigest = sha256.Sum256(planned.Sections[7].Raw)
	raw, err := storageEnrollmentPeakArmPlanned(planned, storageEnrollmentFactualCensus(armed.Census), armed.Generation, armed.Previous, nil, fixture.record, fixture.claim)
	storageTestMust(t, err)
	if !bytes.Equal(raw, fixture.raw) {
		t.Fatal("planned path2 did not encode the exact handwritten complete Armed image")
	}
	decoded, err := storageEnrollmentPeakDecodeEvidence(raw, fixture.record, fixture.claim)
	storageTestMust(t, err)
	if decoded.Phase != 1 || decoded.Scope.path() != 2 || decoded.Sections[8].Count != 1 {
		t.Fatal("planned Armed publication lost its one complete Board section")
	}
	// Even a complete Board cannot make a serialized path2 phase0 legal.
	planned.Sections[8] = decoded.Sections[8]
	if _, err = storageEnrollmentPeakEncodeEvidence(planned, fixture.record, fixture.claim); !errors.Is(err, storagebudget.ErrBounds) {
		t.Fatal("strict encoder accepted a serialized no-growth phase0", err)
	}
}

// Reuse the measured real ancestry setup. Only the first public original parent
// is enrolled; no creator, encoded packet, endpoint or native proof is seeded.
// Both ordinary completion and a real first-Prepared cut feed the aggregate
// marker-producer coverage, then exercise the separate Completed startup route.
func storageEnrollmentActualNoGrowthMarkerRestart(t *testing.T, preparedCut bool) byte {
	t.Helper()
	cfg := ownerActualConfig(t)
	cfg.JpackBin = standInRuntime(t)
	cfg.Logger = log.New(io.Discard, "", 0)
	base := t.TempDir()
	configHome := filepath.Join(base, "configuration")
	parent := filepath.Join(base, "public-original-data-parent")
	storageTestMust(t, os.Mkdir(configHome, 0700))
	storageTestMust(t, os.Mkdir(parent, 0755))
	storageTestMust(t, os.Chmod(parent, 0755))
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(parent, "missing-one", "missing-two"))
	cfg.DeskConfigDir = ""
	s, err := New(cfg)
	storageTestMust(t, err)
	t.Cleanup(func() {
		if s != nil {
			storageTestMust(t, s.Close())
		}
	})
	ready := func() *storageBudgetConstruction {
		t.Helper()
		if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
			t.Fatal("real no-growth startup lost required authority")
		}
		return s.storageConstruction
	}
	c := ready()
	storageTestMust(t, c.enrollOriginalProjectParent(t.Context()))
	prior := c.ref.cell.enrollmentEndpoint
	if prior == nil || prior.Scope.path() != 1 {
		t.Fatal("real no-growth fixture did not complete its original path1")
	}
	identity, err := s.inspectStorageStartupKey()
	storageTestMust(t, err)
	parentInfo, err := os.Stat(parent)
	storageTestMust(t, err)
	hosts, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
	storageTestMust(t, err)
	var targets [2]os.FileInfo
	for bank := range targets {
		targets[bank], err = os.Stat(filepath.Join(s.configDir, storageEnrollmentNativeName(byte(1+bank))))
		storageTestMust(t, err)
	}
	unchangedOriginals := func() {
		t.Helper()
		for bank, before := range targets {
			after, err := os.Stat(filepath.Join(s.configDir, storageEnrollmentNativeName(byte(1+bank))))
			storageTestMust(t, err)
			if before.Size() != after.Size() || !os.SameFile(before, after) {
				t.Fatal("path2 changed an original evidence target or extent")
			}
		}
		currentHosts, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
		storageTestMust(t, err)
		currentParent, err := os.Stat(parent)
		storageTestMust(t, err)
		if !bytes.Equal(hosts, currentHosts) || !os.SameFile(parentInfo, currentParent) {
			t.Fatal("family2 changed original creation hosts or selected parent")
		}
		if _, err := os.Lstat(filepath.Join(parent, "missing-one")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("family2 issued or replayed an ancestor creation", err)
		}
		currentIdentity, err := s.inspectStorageStartupKey()
		storageTestMust(t, err)
		if currentIdentity != identity {
			t.Fatal("no-growth restart changed automatic startup identity")
		}
		storageTestMust(t, ready().sources.current(t.Context()))
	}
	read := func(phase byte) storageEnrollmentPeakEnvelope {
		t.Helper()
		c := ready()
		var physical [2][]byte
		for bank, name := range []string{"storage-owner.a", "storage-owner.b"} {
			physical[bank], err = os.ReadFile(filepath.Join(s.configDir, name))
			storageTestMust(t, err)
		}
		cores, _, err := storageEnrollmentOriginalCorePair(physical, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
		storageTestMust(t, err)
		outer, _, err := storageEnrollmentPeakDecodePairVariant(cores[0], cores[1], *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim, prior.CompositionLineage != nil)
		storageTestMust(t, err)
		if outer.Kind != 2 || outer.BeforeCodec != 1 || outer.Marker == nil || outer.Marker.Phase != phase || outer.Scope.path() != 2 || outer.Sequence != storageEnrollmentPeakMarkerSequence(outer.Scope, phase) {
			t.Fatal("real no-growth marker lost phase, path, sequence or before codec")
		}
		for bank, before := range targets {
			if storageEnrollmentPeakU64(outer.Scope[328+bank*8:]) != uint64(before.Size()) || outer.Scope.final(byte(bank)) != uint64(before.Size()) {
				t.Fatal("no-growth scope changed an actual retained extent")
			}
		}
		return outer
	}
	qBefore, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	entered := make(chan struct{})
	sentinel := errors.New("handwritten family2 first Prepared restart cut")
	if preparedCut {
		c.enrollmentFault = &storageEnrollmentFault{point: "successor-first-prepared-family2", entered: entered, err: sentinel}
	}
	complete, enrollmentErr := c.enrollNextHistoryAncestor(t.Context(), s)
	if complete {
		t.Fatal("one parent enrollment unexpectedly completed missing ancestry")
	}
	var prepared *storageEnrollmentPeakEnvelope
	var armed *storageEnrollmentPeakEvidence
	if preparedCut {
		select {
		case <-entered:
		default:
			t.Fatal("real path2 producer did not reach first Prepared")
		}
		if !errors.Is(enrollmentErr, sentinel) {
			t.Fatal("real path2 stopped outside the requested Prepared cut", enrollmentErr)
		}
		outer := read(1)
		prepared = &outer
		packet, err := os.ReadFile(filepath.Join(s.configDir, storageEnrollmentNativeName(1+outer.Marker.Bank)))
		storageTestMust(t, err)
		actual, err := storageEnrollmentPeakDecodeEvidenceWithLineage(packet, prior.CompositionLineage, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
		storageTestMust(t, err)
		armed = &actual
		if actual.Phase != 1 || storageEnrollmentSuccessorFamily(actual.Board.SourceKind, actual.Board.Source) != 2 || storageEnrollmentPeakJoinMarker(outer, actual, actual.Ready) != nil || !storageEnrollmentPeakRetainsOrigins(actual, *prior) {
			t.Fatal("Prepared did not select exact real family2 Armed evidence")
		}
		qPrepared, err := c.ref.cell.q.InspectEnrollment(t.Context())
		storageTestMust(t, err)
		if !reflect.DeepEqual(qBefore, qPrepared) {
			t.Fatal("first Prepared cut executed Q before recovery")
		}
		unchangedOriginals()
		storageTestMust(t, s.Close())
		s = nil
		s, err = New(cfg)
		storageTestMust(t, err)
	} else {
		storageTestMust(t, enrollmentErr)
	}
	c = ready()
	final := read(3)
	completed := c.ref.cell.enrollmentEndpoint
	if completed == nil || completed.Phase != 2 || completed.Scope != final.Scope || storageEnrollmentSuccessorFamily(completed.Board.SourceKind, completed.Board.Source) != 2 || storageEnrollmentPeakJoinMarker(final, *completed, completed.Ready) != nil || !storageEnrollmentPeakRetainsOrigins(*completed, *prior) {
		t.Fatal("real no-growth completion lost exact family2 endpoint or retained origins")
	}
	if prepared != nil && (completed.Scope != prepared.Scope || !storageEnrollmentPeakJoinArmed(*completed, *armed)) {
		t.Fatal("Prepared recovery changed its exact Scope or Armed/Completed join")
	}
	unchangedOriginals()
	var finalCatalogs [2][]byte
	for bank, name := range []string{"storage-owner.a", "storage-owner.b"} {
		finalCatalogs[bank], err = os.ReadFile(filepath.Join(s.configDir, name))
		storageTestMust(t, err)
	}
	if !bytes.Equal(finalCatalogs[0], finalCatalogs[1]) {
		t.Fatal("real path2 completion did not publish exact C/C")
	}
	qCompleted, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	storageTestMust(t, s.Close())
	s = nil
	s, err = New(cfg)
	storageTestMust(t, err)
	c = ready()
	restarted := c.ref.cell.enrollmentEndpoint
	if restarted == nil || !bytes.Equal(restarted.Raw, completed.Raw) || restarted.Scope != completed.Scope || restarted.AfterEpoch != completed.AfterEpoch {
		t.Fatal("Completed no-growth restart changed endpoint, Scope or epoch")
	}
	read(3)
	for bank, name := range []string{"storage-owner.a", "storage-owner.b"} {
		actual, err := os.ReadFile(filepath.Join(s.configDir, name))
		storageTestMust(t, err)
		if !bytes.Equal(actual, finalCatalogs[bank]) {
			t.Fatal("Completed no-growth restart republished the physical catalog")
		}
	}
	qRestarted, err := c.ref.cell.q.InspectEnrollment(t.Context())
	storageTestMust(t, err)
	if !reflect.DeepEqual(qCompleted, qRestarted) {
		t.Fatal("Completed no-growth restart changed checkpoint or ticket state")
	}
	unchangedOriginals()
	return final.Scope.path()
}
