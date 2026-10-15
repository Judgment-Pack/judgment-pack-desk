package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"regexp"
	"strings"
)

func researchCanContinue(run inputResearchRun) bool {
	return len(run.Sources) > 0 && contains([]string{"stopped", "completed", "completed-with-gaps", "partial", "blocked", "interrupted"}, run.State)
}

// The caller holds the research-root admission lock. Fail closed if any saved
// state cannot be inspected; another window must not start a duplicate run.
func researchInputAvailable(root *os.Root, prep, except string) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !inputResearchID.MatchString(entry.Name()) || entry.Name() == except {
			continue
		}
		rr, err := openInputResearch(root, entry.Name(), false)
		if err != nil {
			return err
		}
		run, err := readInputResearch(rr)
		rr.Close()
		if err != nil {
			return err
		}
		if run.PreparationID == prep && (run.State == "queued" || run.State == "running" || run.State == "paused") {
			return errors.New("Research is already queued or running, or is waiting for budget approval for these inputs. Finish or stop it before starting another run.")
		}
	}
	return nil
}

var researchProofID = regexp.MustCompile(`^attempt_[a-f0-9]{64}(_[1-4])?$`)

func writeResearchReview(root *os.Root, run inputResearchRun, parent *os.Root) error {
	// Publish the review directory only when all copied evidence is durable.
	staging := ".review-" + run.ID
	rr, _, err := openOwnFolder(root, []string{staging}, true, "research review")
	if err != nil {
		return err
	}
	defer rr.Close()
	defer root.RemoveAll(staging)
	if parent != nil || run.Reprocess {
		for _, source := range run.Sources {
			sourceRoot := parent
			if run.Reprocess {
				var err error
				sourceRoot, err = openInputResearch(root, run.SourceRuns[source.ID], false)
				if err != nil {
					return err
				}
				defer sourceRoot.Close()
			}
			if !researchProofID.MatchString(source.ID) {
				return errors.New("A saved source reference is invalid.")
			}
			originID := source.ID
			if run.Reprocess && run.SourceOriginIDs[source.ID] != "" {
				originID = run.SourceOriginIDs[source.ID]
			}
			if !researchProofID.MatchString(originID) {
				return errors.New("A saved source origin is invalid.")
			}
			raw, err := readPrivateData(sourceRoot, originID+".proof.json", inputResearchSourceProofLimit)
			if err != nil {
				return errors.New("A retained source proof is unavailable. Start new research instead.")
			}
			var proof inputResearchProof
			if decodeDataJSON(raw, &proof) != nil || proof.Pin != run.Gateway {
				return errors.New("A saved source proof no longer matches this research.")
			}
			run.RetrievedBytes += len(raw)
			if run.RetrievedBytes > inputResearchRetainedByteLimit-inputResearchSourceMetadataReserve {
				return errors.New("Retained sources exceed the continuation storage limit.")
			}
			if err = writePrivateData(rr, source.ID+".proof.json", raw); err != nil {
				return err
			}
		}
	}
	if err = writeInputResearch(rr, run); err != nil {
		return err
	}
	return root.Rename(staging, run.ID)
}

// Match exact person URLs. Companies additionally require the same normalized
// name and host, avoiding domain-only merges of parents, subsidiaries or people.
func researchEntityKey(entity inputResearchEntity) string {
	u, err := url.Parse(entity.URL)
	if err != nil || u.Host == "" {
		return entity.Kind + "\n" + entity.URL + "\n" + entity.Name
	}
	if entity.Kind == "company" {
		return "company\n" + strings.TrimPrefix(strings.ToLower(u.Host), "www.") + "\n" + strings.ToLower(strings.Join(strings.Fields(entity.Name), " "))
	}
	return entity.Kind + "\n" + entity.URL
}

func (s *Server) researchRecordIsNewer(record preparedInputRecord, incoming inputResearchRun) (bool, error) {
	if record.Research.RunID == incoming.ID {
		return false, nil
	}
	root, err := s.inputResearchRoot(false)
	if err != nil {
		return false, err
	}
	defer root.Close()
	rr, err := openInputResearch(root, record.Research.RunID, false)
	if err != nil {
		return false, err
	}
	defer rr.Close()
	previous, err := readInputResearch(rr)
	if err != nil {
		return false, err
	}
	return previous.CreatedAt > incoming.CreatedAt || previous.CreatedAt == incoming.CreatedAt && previous.ID > incoming.ID, nil
}

// Continuation re-verifies retained bytes locally before permitting new AI
// work. It never reacquires a source or repeats an uncertain operation.
func (s *Server) verifyResearchContinuation(ctx context.Context, root *os.Root, run inputResearchRun) error {
	program, err := s.researchVerifier()
	if err != nil {
		return err
	}
	for _, source := range run.Sources {
		if !researchProofID.MatchString(source.ID) {
			return errors.New("A saved source reference is invalid.")
		}
		raw, err := readPrivateData(root, source.ID+".proof.json", inputResearchSourceProofLimit)
		if err != nil {
			return errors.New("A retained source proof is unavailable.")
		}
		var proof inputResearchProof
		if decodeDataJSON(raw, &proof) != nil || proof.Pin != run.Gateway {
			return errors.New("A saved source proof changed.")
		}
		result, digest, err := verifyResearchProof(ctx, program, proof)
		if err != nil || digest != source.Digest {
			return errors.New("A retained source proof could not be verified.")
		}
		decoded, err := decodeInputResearchSource(inputResearchSource{ID: source.ID, Kind: source.Kind, At: source.At, Digest: digest, Verification: "verified", Bytes: source.Bytes}, proof.Arguments, result)
		// Older journals did not carry this display-only truncation marker.
		if !source.TextTruncated {
			decoded.TextTruncated = false
		}
		a, _ := json.Marshal(decoded)
		b, _ := json.Marshal(source)
		if err != nil || string(a) != string(b) {
			return errors.New("Saved research text no longer matches its source proof.")
		}
	}
	return nil
}
