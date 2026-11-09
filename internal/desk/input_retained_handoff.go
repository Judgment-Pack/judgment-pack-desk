package desk

// This private producer joins original-history custody to the actual reviewed
// snapshot. It supplies consistency evidence for a later generated channel;
// it grants no input access and makes no acquisition or signing claim.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
)

type reviewedInputRunRead struct {
	root *os.Root
	dir  *os.Root
	run  inputResearchRun
}

func (r *reviewedInputRunRead) close() {
	r.dir.Close()
	r.root.Close()
}

// Keep the ordinary producer's admission and error order. A successful read is
// the sole journal snapshot used by either producer's subsequent checks.
func (s *Server) openReviewedInputRun(ctx context.Context, doc inputPreparation, record preparedInputRecord) (*reviewedInputRunRead, error) {
	if err := readyInputReview(doc, record); err != nil {
		return nil, err
	}
	root, err := s.inputResearchRoot(false)
	if err != nil {
		return nil, err
	}
	dir, err := openInputResearch(root, record.Research.RunID, false)
	if err != nil {
		root.Close()
		return nil, err
	}
	run, err := s.researchJournal(dir, record.Research.RunID).Read(ctx)
	if err != nil {
		dir.Close()
		root.Close()
		return nil, err
	}
	return &reviewedInputRunRead{root: root, dir: dir, run: run}, nil
}

type retainedReviewedInputProduction struct {
	Carrier                retainedReviewedCarrier
	CarrierJSON            json.RawMessage
	OriginalSnapshotSHA256 string
	ProjectedFrameSHA256   string
	RunRevision            string
}

// retainedReviewedInputSnapshot is a private consistency seam for D3b. All
// history checks precede projection; source/model/review checks are the same as
// v1 and use the identical checked run. No grant, stdout or signature is emitted.
func (s *Server) retainedReviewedInputSnapshot(ctx context.Context, doc inputPreparation, record preparedInputRecord, row inputPreparedRow) (retainedReviewedInputProduction, error) {
	var empty retainedReviewedInputProduction
	retained, err := s.openReviewedInputRun(ctx, doc, record)
	if err != nil {
		return empty, err
	}
	defer retained.close()
	history, err := resolveRetainedResearchHistory(doc, record, retained.run)
	if err != nil {
		return empty, err
	}
	snapshot, err := s.reviewedInputSnapshotFromRun(ctx, doc, record, row, retained.run, retained.dir)
	if err != nil {
		return empty, err
	}
	carrier, err := projectRetainedReviewedInput(snapshot, history)
	if err != nil {
		return empty, err
	}
	raw, err := json.Marshal(carrier)
	if err != nil {
		return empty, err
	}
	checked, err := checkRetainedReviewedCarrier(raw, history)
	if err != nil || checked.OriginalSnapshotSHA256 != sha256Digest(snapshot) || !bytes.Equal([]byte(checked.OriginalSnapshotJSON), snapshot) {
		return empty, errors.New("The retained input projection changed its original snapshot.")
	}
	node, err := parseRetainedJSON(raw)
	if err != nil || node.member("retained") == nil {
		return empty, errors.New("The retained input frame is unavailable.")
	}
	return retainedReviewedInputProduction{Carrier: checked, CarrierJSON: append(json.RawMessage(nil), raw...), OriginalSnapshotSHA256: checked.OriginalSnapshotSHA256, ProjectedFrameSHA256: sha256Digest(node.member("retained").raw), RunRevision: history.RunRevision}, nil
}
