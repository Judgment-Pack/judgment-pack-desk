package desk

import (
	"errors"
	"testing"
)

// **An evaluation review on a fresh Desk enrolls first-use history before it
// reads the saved tests.** The review held no private-data transaction and read
// the private store directly, so on a Desk whose history did not exist yet it
// was refused with "storage original owner constraint transition required" and
// no Dialects evaluation could be reviewed. The read itself still creates
// nothing unaccounted: it runs only after the enrolled route.
func TestInputEvaluationReviewEnrollsFirstUseHistory(t *testing.T) {
	s, _, _ := assistantServer(t)
	writeProjectFile(t, s.projectDir, "jpack.json", `{"configVersion":"5","packs":{"one":{"path":"one.pack.json"}}}`)
	writeProjectFile(t, s.projectDir, "one.pack.json", reviewPack)
	if s.storageEnrollment == nil {
		t.Fatal("the fixture Desk is not enrolled for storage")
	}
	q := &inputQualification{Key: "one", Pack: reviewPack}
	fingerprint, request, _, err := s.inputReviewReleaseSnapshot(t.Context(), q)
	if errors.Is(err, errStorageOwnerTransitionRequired) || err != nil {
		t.Fatalf("a fresh Desk's evaluation review was refused: %v", err)
	}
	if fingerprint == "" || request["pack"] != reviewPack {
		t.Fatalf("the review read %q %v", fingerprint, request)
	}
	// The unchanged inner read, inside the enrolled store, gives the same answer.
	again, _, _, err := s.inputReleaseSnapshot(q)
	if err != nil || again != fingerprint {
		t.Fatalf("the enrolled read changed: %q %v", again, err)
	}
}
