package desk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
)

// This interface deliberately has neither current View nor Update. Retained
// history can explain past outcomes, never establish live authority or replay.
type policyHistoryReader interface {
	ReadHistory(context.Context, func(policyLedger) error) error
}
type filePolicyHistoryReader struct {
	open            func(bool) (*os.Root, error)
	requireExisting bool
	admit           func() (func(), error)
}

// These are the only production constructors. Binding is captured during New,
// before workers and request handling; requests cannot replace or drop it.
func (s *Server) policyRepository() policyRepository {
	return filePolicyRepository{open: s.openPolicies, anchor: s.policyAuthorityAnchor, admit: s.policyMachineAdmission(), currentGuard: func() error {
		if s.policyAuthorityMachine != nil {
			anchor, err := s.policyAuthorityMachine.resolve(s.project)
			if err != nil || anchor != nil {
				return policyMachineRefusal()
			}
		}
		return policyMachineNoDemand(s.root)
	}}
}
func (s *Server) policyHistoryReader() policyHistoryReader {
	return filePolicyHistoryReader{open: s.openPolicies, requireExisting: s.policyAuthorityAnchor != nil, admit: s.policyMachineAdmission()}
}
func (s *Server) policyMachineAdmission() func() (func(), error) {
	if s.policyAuthorityMachine == nil {
		return nil
	}
	return s.policyAuthorityMachine.admit
}
func policyAuthorityStoreFailure(err error) error {
	// An unverifiable head cannot establish whether this logical operation
	// committed before a lost response or rollback. Never clear its journal.
	f := policyRefusal(503, "authority-unavailable", "Current Policy authority storage is unavailable. Preserve the operation and use read-only recovery.")
	var anchor *policyAuthorityAnchorError
	if errors.As(err, &anchor) {
		switch anchor.Code {
		case "policy-authority-anchor-pending", "policy-authority-anchor-uncertain":
			f = policyRefusal(503, "authority-unresolved", "Policy authority cannot confirm its current write state. Use read-only history recovery before another write.")
		case "policy-authority-anchor-busy":
			f = policyRefusal(503, "storage-busy", "Policy authority storage is busy. Preserve the original operation for recovery.")
		case "policy-authority-anchor-mismatch", "policy-authority-anchor-identity", "policy-authority-anchor-integrity":
			f = policyRefusal(409, "authority-conflict", "The retained Policy ledger does not establish the bound current authority. Read-only history remains available.")
		}
	}
	f.Uncertain = true
	return f
}

func readExistingPolicyLedger(root *os.Root) ([]byte, policyLedger, error) {
	var ledger policyLedger
	raw, err := readPrivateData(root, "ledger.json", policyLedgerLimit)
	if err != nil {
		return nil, ledger, err
	}
	ledger, err = decodePolicyLedger(raw)
	if err != nil {
		return nil, ledger, policyRefusal(409, "storage-integrity", "Retained Policy storage is inconsistent.")
	}
	return raw, ledger, nil
}
func (f filePolicyRepository) anchoredTransaction(ctx context.Context, write bool, use func(*policyLedger) (bool, error)) error {
	if ctx.Err() != nil {
		return policyAuthorityStoreFailure(ctx.Err())
	}
	root, err := f.open(false)
	if err != nil {
		return policyAuthorityStoreFailure(err)
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		return policyAuthorityStoreFailure(err)
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return policyAuthorityStoreFailure(policyAuthorityAnchorRefusal("busy"))
	}
	raw, ledger, err := readExistingPolicyLedger(root)
	if err != nil {
		var known *policyFailure
		if errors.As(err, &known) {
			copy := *known
			copy.Uncertain = true
			return &copy
		}
		return policyAuthorityStoreFailure(err)
	}
	var precommit error
	err = f.anchor.WithCurrent(ctx, raw, func(held *policyAuthorityAnchorHeld) error {
		changed, e := use(&ledger)
		if e != nil {
			if write {
				precommit = policyBeforeCommitFailure(e)
			} else {
				precommit = e
			}
			return precommit
		}
		if !write || !changed {
			return nil
		}
		if e = validatePolicyLedger(ledger); e != nil {
			precommit = policyBeforeCommitFailure(e)
			return precommit
		}
		next, e := json.Marshal(ledger)
		if e != nil {
			precommit = policyBeforeCommitFailure(e)
			return precommit
		}
		if len(next) > policyLedgerLimit {
			precommit = policyRefusal(409, "storage-capacity", "Policy storage has reached its supported capacity.")
			return precommit
		}
		if e = ctx.Err(); e != nil {
			precommit = policyBeforeCommitFailure(e)
			return precommit
		}
		// Both locks remain held. Advance persists intent before this single exact
		// existing durable write; no domain closure or write is replayed on failure.
		return held.Advance(ctx, next, func(exact []byte) error { return writePrivateData(root, "ledger.json", exact) })
	})
	if err == nil {
		return nil
	}
	var anchor *policyAuthorityAnchorError
	if precommit != nil && errors.As(err, &anchor) && anchor.Code == "policy-authority-anchor-callback-refused" {
		return precommit
	}
	return policyAuthorityStoreFailure(err)
}
func (f filePolicyHistoryReader) ReadHistory(ctx context.Context, use func(policyLedger) error) error {
	if f.admit != nil {
		release, err := f.admit()
		if err != nil {
			return err
		}
		defer release()
	}
	if ctx.Err() != nil {
		return policyRefusal(503, "storage-unavailable", "Retained Policy history could not be read.")
	}
	root, err := f.open(false)
	if errors.Is(err, os.ErrNotExist) && !f.requireExisting {
		return use(emptyPolicyLedger())
	}
	if err != nil {
		return policyRefusal(503, "storage-unavailable", "Retained Policy history is unavailable.")
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		return policyRefusal(503, "storage-unavailable", "Retained Policy history is unavailable.")
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, false); err != nil {
		return policyRefusal(503, "storage-busy", "Retained Policy history is busy.")
	}
	_, ledger, err := readExistingPolicyLedger(root)
	if errors.Is(err, os.ErrNotExist) && !f.requireExisting {
		ledger = emptyPolicyLedger()
		err = nil
	}
	if err != nil {
		var known *policyFailure
		if errors.As(err, &known) {
			return err
		}
		return policyRefusal(503, "storage-unavailable", "Retained Policy history is unavailable.")
	}
	if ctx.Err() != nil {
		return policyRefusal(503, "storage-unavailable", "Retained Policy history could not be read.")
	}
	return use(ledger)
}
func (s policyService) readHistory(ctx context.Context, use func(policyLedger) error) error {
	if err := s.authorized(ctx); err != nil {
		return err
	}
	if s.history == nil {
		return policyRefusal(503, "storage-unavailable", "Retained Policy history is unavailable.")
	}
	err := s.history.ReadHistory(ctx, func(l policyLedger) error {
		if err := s.authorized(ctx); err != nil {
			return err
		}
		err := use(l)
		if authErr := s.authorized(ctx); authErr != nil {
			return authErr
		}
		return err
	})
	// Authorization takes precedence even when a retained read failed. Revocation
	// must not be disguised as storage failure or historical absence.
	if authErr := s.authorized(ctx); authErr != nil {
		return authErr
	}
	return err
}
