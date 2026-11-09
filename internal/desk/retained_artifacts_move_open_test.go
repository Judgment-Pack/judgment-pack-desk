package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Traverse the real authenticated move, original owner acquisition and outer
// error/Close handoff. A later missing edge/queue is never initial absence.
func TestDialectArtifactProjectMoveRetainedAcquisition(t *testing.T) {
	for _, name := range []string{"initial-absent", "edge-not-exist", "queue-not-exist", "edge-not-exist-cleanup-failure"} {
		t.Run(name, func(t *testing.T) {
			s, ts, _ := assistantServer(t)
			if code, _ := chatRequest(t, ts, "PUT", "absent", storageChat, true, ""); code != 200 {
				t.Fatal(code)
			}
			stampMoveIdentity(t, s)
			owner := enrollDialectArtifactMoveServer(t, s)
			originalProject := s.projectDir
			ownerPath, scopePath := dialectArtifactOwnerRowPaths(s, owner)
			beforeOwner, beforeScope := dialectArtifactOwnerRead(t, ownerPath), dialectArtifactOwnerRead(t, scopePath)
			locationPath := dialectArtifactLocationPath(s, owner.Owner)
			beforeLocation := dialectArtifactOwnerRead(t, locationPath)
			markerPath := filepath.Join(originalProject, ".desk-private", dialectArtifactOwnerMarker)
			beforeMarker := dialectArtifactOwnerRead(t, markerPath)
			raw := []byte("unchanged retained source\n\x00")
			var ref retainedArtifactRef
			if name != "initial-absent" {
				var err error
				ref, err = s.artifactRepository().Put(context.Background(), raw)
				storageTestMust(t, err)
			}
			destination := filepath.Join(t.TempDir(), "next")
			request := movePreview(t, ts, destination)
			if name == "initial-absent" {
				if _, err := os.Lstat(filepath.Join(originalProject, retainedArtifactDirectory)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("fixture is not genuine initial absence", err)
				}
			}
			// In actual hold acquisition, move-locations opens exactly two selected
			// components; its two edge closures follow. The next edge closure is the
			// first retained-store edge, strictly AFTER initial open succeeded.
			locationSyncs, edgeCloses := 0, 0
			injected := false
			// Snapshot and final hold own distinct project-private roots but use
			// one original label. The existing location-open barrier separates
			// those lifetimes; no aggregate >=1 assertion can prove both.
			snapshotClosed, closed := map[string]int{}, map[string]int{}
			cleanupErr := errors.New("retained outer cleanup failed")
			testHookDialectArtifactOwnerStep = func(at string) error {
				if strings.HasPrefix(at, "move-") && strings.HasSuffix(at, ".close") {
					if locationSyncs == 0 {
						snapshotClosed[at]++
					} else {
						closed[at]++
					}
				}
				if at == "move-locations.directory-sync" {
					locationSyncs++
				}
				if locationSyncs == 2 && at == "injected-edge-folder.close" {
					edgeCloses++
					if edgeCloses == 3 && (name == "edge-not-exist" || name == "edge-not-exist-cleanup-failure") {
						injected = true
						return os.ErrNotExist
					}
				}
				if at == "move-queue.open" && name == "queue-not-exist" {
					injected = true
					return os.ErrNotExist
				}
				if at == "move-artifacts.close" && name == "edge-not-exist-cleanup-failure" {
					return cleanupErr
				}
				return nil
			}
			t.Cleanup(func() { testHookDialectArtifactOwnerStep = nil })
			code, response := moveRequest(t, ts, "move", request)
			testHookDialectArtifactOwnerStep = nil
			if name == "initial-absent" {
				if code != 200 {
					t.Fatal("supported initial absence refused", code, string(response))
				}
				if _, err := os.Lstat(filepath.Join(destination, retainedArtifactDirectory)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("move created retained store", err)
				}
				if !bytes.Equal(dialectArtifactOwnerRead(t, filepath.Join(destination, ".desk-private", dialectArtifactOwnerMarker)), beforeMarker) {
					t.Fatal("original marker changed")
				}
				return
			}
			if !injected || code != 409 {
				t.Fatal("later acquisition absence accepted", injected, code, string(response))
			}
			if !bytes.Contains(response, []byte(os.ErrNotExist.Error())) {
				t.Fatal("original missing error lost", string(response))
			}
			if name == "edge-not-exist-cleanup-failure" && !bytes.Contains(response, []byte(cleanupErr.Error())) {
				t.Fatal("outer cleanup error lost", string(response))
			}
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("refusal renamed project", err)
			}
			if _, err := os.Lstat(filepath.Join(s.configDir, projectMoveName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("refusal published move intent", err)
			}
			if snapshotClosed["move-project-private.close"] != 1 {
				t.Fatal("snapshot private cleanup missing/repeated", snapshotClosed)
			}
			for _, label := range []string{"move-queue.close", "move-artifacts.close", "move-locations.close", "move-owner-lock.close", "move-locks.close", "move-metadata.close", "move-marker-lock.close", "move-project-private.close"} {
				if closed[label] != 1 {
					t.Fatal("partial hold cleanup missing/repeated", label, closed)
				}
			}
			if !bytes.Equal(dialectArtifactOwnerRead(t, ownerPath), beforeOwner) || !bytes.Equal(dialectArtifactOwnerRead(t, scopePath), beforeScope) || !bytes.Equal(dialectArtifactOwnerRead(t, locationPath), beforeLocation) || !bytes.Equal(dialectArtifactOwnerRead(t, markerPath), beforeMarker) {
				t.Fatal("refusal published owner/location bytes")
			}
			got, err := s.artifactRepository().Get(t.Context(), ref)
			storageTestMust(t, err)
			if !bytes.Equal(got, raw) {
				t.Fatal("refusal changed retained source")
			}
			// Read-only fresh preview exercises release of the original outer work,
			// private and metadata locks; it is not a retry of the refused effect.
			_ = movePreview(t, ts, destination)
		})
	}
}
