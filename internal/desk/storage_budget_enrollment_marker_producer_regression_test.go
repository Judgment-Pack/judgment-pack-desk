package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Use the real initial/successor producers, original native holds and startup
// recovery. No encoded envelope, native qualification or expected packet is
// seeded by this fixture. The installed native qualification remains required.
func TestStorageEnrollmentMarkerProducersKeepBeforeCodecThroughRecovery(t *testing.T) {
	paths := map[byte]bool{}
	for _, family := range []byte{1, 4} {
		t.Run(fmt.Sprint(family), func(t *testing.T) {
			cfg := ownerActualConfig(t)
			cfg.JpackBin = standInRuntime(t)
			cfg.Logger = log.New(io.Discard, "", 0)
			if family == 4 {
				storageTestMust(t, os.Mkdir(cfg.DeskConfigDir, 0700))
				privateFixture(t, filepath.Join(cfg.DeskConfigDir, "conversations-"+strings.Repeat("a", 64)+".json"), `{"version":1,"chats":[]}`)
			}
			s, err := New(cfg)
			storageTestMust(t, err)
			t.Cleanup(func() {
				if s != nil {
					storageTestMust(t, s.Close())
				}
			})
			if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
				t.Fatal("required actual original startup unavailable")
			}
			c := s.storageConstruction
			if c.ref.cell.enrollmentEndpoint != nil {
				t.Fatal("fixture must start before initial enrollment")
			}
			storageTestMust(t, c.enrollOriginalProjectParent(t.Context()))
			initial := c.ref.cell.enrollmentEndpoint
			if initial == nil || initial.Scope.path() != 1 {
				t.Fatal("actual initial producer did not complete path1")
			}
			read := func(phase, codec byte) storageEnrollmentPeakEnvelope {
				t.Helper()
				c := s.storageConstruction
				var banks [2][]byte
				for bank, name := range []string{"storage-owner.a", "storage-owner.b"} {
					var err error
					banks[bank], err = os.ReadFile(filepath.Join(s.configDir, name))
					storageTestMust(t, err)
				}
				outer, _, err := storageEnrollmentPeakDecodePair(banks[0], banks[1], *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
				storageTestMust(t, err)
				if outer.Kind != 2 || outer.BeforeCodec != codec || outer.Marker == nil || outer.Marker.Phase != phase || outer.Sequence != storageEnrollmentPeakMarkerSequence(outer.Scope, phase) {
					t.Fatal("actual producer lost marker phase, sequence or before-image selector")
				}
				wrong := outer
				wrong.BeforeCodec = 1 - codec
				if _, err := storageEnrollmentPeakEncodeEnvelope(wrong, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim); !errors.Is(err, storagebudget.ErrBounds) {
					t.Fatal("strict decoder accepted opposite before-image selector", err)
				}
				return outer
			}
			read(3, 0)
			entered := make(chan struct{})
			c.enrollmentFault = &storageEnrollmentFault{point: "successor-first-prepared-family" + fmt.Sprint(family), entered: entered, err: errors.New("handwritten marker producer restart cut")}
			response := policyCall(s, policyTestSession(t, s), http.MethodGet, "/api/conversations", nil)
			select {
			case <-entered:
			default:
				t.Fatal("actual successor producer did not reach prepared marker")
			}
			if response.Code == http.StatusOK {
				t.Fatal("interrupted successor reported success")
			}
			prepared := read(1, 1)
			paths[prepared.Scope.path()] = true
			packet, err := os.ReadFile(filepath.Join(s.configDir, storageEnrollmentNativeName(1+prepared.Marker.Bank)))
			storageTestMust(t, err)
			armed, err := storageEnrollmentPeakDecodeEvidence(packet, *c.ref.cell.ownerRecord, c.ref.cell.ownerClaim)
			storageTestMust(t, err)
			if storageEnrollmentSuccessorFamily(armed.Board.SourceKind, armed.Board.Source) != family || storageEnrollmentPeakJoinMarker(prepared, armed, armed.Ready) != nil {
				t.Fatal("prepared producer changed exact successor family or evidence")
			}
			catalog, err := json.Marshal(armed.Inner.Catalog)
			storageTestMust(t, err)
			hosts, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
			storageTestMust(t, err)
			storageTestMust(t, s.Close())
			s = nil
			s, err = New(cfg)
			storageTestMust(t, err)
			if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
				t.Fatal("actual prepared recovery did not complete")
			}
			final := read(3, 1)
			completed := s.storageConstruction.ref.cell.enrollmentEndpoint
			if completed == nil || completed.Scope != prepared.Scope || !storageEnrollmentPeakJoinArmed(*completed, armed) || storageEnrollmentPeakJoinMarker(final, *completed, completed.Ready) != nil {
				t.Fatal("recovery definitions/final producers changed exact Scope or Armed/Completed join")
			}
			actualCatalog, err := json.Marshal(completed.Inner.Catalog)
			storageTestMust(t, err)
			if sha256.Sum256(catalog) != sha256.Sum256(actualCatalog) {
				t.Fatal("marker recovery changed intended complete catalog")
			}
			actualHosts, err := os.ReadFile(filepath.Join(s.configDir, storageIntentName))
			storageTestMust(t, err)
			if !bytes.Equal(hosts, actualHosts) {
				t.Fatal("marker recovery replayed or rewrote creation records")
			}
			// A prepared restart publishes C/C but does not reopen that cut.
			// Exercise its separate actual startup branch without another demand.
			var finalCatalogs [2][]byte
			for bank, name := range []string{"storage-owner.a", "storage-owner.b"} {
				finalCatalogs[bank], err = os.ReadFile(filepath.Join(s.configDir, name))
				storageTestMust(t, err)
			}
			if !bytes.Equal(finalCatalogs[0], finalCatalogs[1]) {
				t.Fatal("prepared recovery did not publish the actual completed C/C cut")
			}
			identity, err := s.inspectStorageStartupKey()
			storageTestMust(t, err)
			selectedPath := string(completed.Board.Source.Default.SelectedPath)
			selectedInfo, err := os.Stat(selectedPath)
			storageTestMust(t, err)
			var pointer, marker []byte
			var pointerInfo, markerInfo os.FileInfo
			pointer, err = os.ReadFile(filepath.Join(s.configDir, dataLocationName))
			storageTestMust(t, err)
			pointerInfo, err = os.Stat(filepath.Join(s.configDir, dataLocationName))
			storageTestMust(t, err)
			if family == 1 {
				marker, err = os.ReadFile(filepath.Join(selectedPath, dataMarkerName))
				storageTestMust(t, err)
				markerInfo, err = os.Stat(filepath.Join(selectedPath, dataMarkerName))
				storageTestMust(t, err)
			}
			storageTestMust(t, s.Close())
			s = nil
			s, err = New(cfg)
			storageTestMust(t, err)
			if s.storageReadOnly || s.storageConstruction == nil || s.storageConstruction.owner == nil || s.storageEnrollment == nil {
				t.Fatal("actual completed C/C restart did not retain startup authority")
			}
			restarted := s.storageConstruction.ref.cell.enrollmentEndpoint
			if restarted == nil || !bytes.Equal(restarted.Raw, completed.Raw) || restarted.Scope != completed.Scope || restarted.AfterEpoch != completed.AfterEpoch {
				t.Fatal("completed restart changed exact endpoint, scope or census epoch")
			}
			read(3, 1)
			for bank, name := range []string{"storage-owner.a", "storage-owner.b"} {
				actual, err := os.ReadFile(filepath.Join(s.configDir, name))
				storageTestMust(t, err)
				if !bytes.Equal(actual, finalCatalogs[bank]) {
					t.Fatal("completed restart republished the completed catalog")
				}
			}
			actualHosts, err = os.ReadFile(filepath.Join(s.configDir, storageIntentName))
			storageTestMust(t, err)
			identityAfter, err := s.inspectStorageStartupKey()
			storageTestMust(t, err)
			selectedAfter, err := os.Stat(selectedPath)
			storageTestMust(t, err)
			if !bytes.Equal(hosts, actualHosts) || identity != identityAfter || !os.SameFile(selectedInfo, selectedAfter) {
				t.Fatal("completed restart changed retained hosts, automatic identity or original source")
			}
			actualPointer, err := os.ReadFile(filepath.Join(s.configDir, dataLocationName))
			storageTestMust(t, err)
			actualInfo, err := os.Stat(filepath.Join(s.configDir, dataLocationName))
			storageTestMust(t, err)
			if !bytes.Equal(pointer, actualPointer) || !os.SameFile(pointerInfo, actualInfo) {
				t.Fatal("completed restart replayed the selected pointer effect")
			}
			if family == 1 {
				actualMarker, err := os.ReadFile(filepath.Join(selectedPath, dataMarkerName))
				storageTestMust(t, err)
				actualInfo, err := os.Stat(filepath.Join(selectedPath, dataMarkerName))
				storageTestMust(t, err)
				if !bytes.Equal(marker, actualMarker) || !os.SameFile(markerInfo, actualInfo) {
					t.Fatal("completed restart replayed the history marker effect")
				}
			}
			storageTestMust(t, s.storageConstruction.sources.current(t.Context()))
		})
	}
	for _, prepared := range []bool{false, true} {
		name := "family2-completed"
		if prepared {
			name = "family2-prepared"
		}
		t.Run(name, func(t *testing.T) {
			paths[storageEnrollmentActualNoGrowthMarkerRestart(t, prepared)] = true
		})
	}
	if !paths[2] || !paths[3] {
		t.Error("actual producer fixtures must establish both no-growth and growth successors", paths)
	}
}
