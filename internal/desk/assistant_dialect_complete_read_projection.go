package desk

import (
	"context"
	"errors"
	"net/http"
	"os"
	"reflect"
)

type assistantDialectCompleteProjectionKey struct{}

// A concrete selected projection owns this actual SH work FD before writes,
// preparation or private custody. It is never constructed from a saved proof.
type assistantDialectCompleteProjectionRead struct {
	self                 *assistantDialectCompleteProjectionRead
	server               *Server
	runID                string
	binding              assistantRunBinding
	work                 *os.File
	folderInfo, workInfo os.FileInfo
	view                 inputPreparationRead
	closed               bool
	closeErr             error
}

func (p *assistantDialectCompleteProjectionRead) Check(ctx context.Context) error {
	if p == nil || p.self != p || p.closed || p.server == nil || p.server.assistant == nil || p.work == nil {
		return errAssistantRunScope
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	folder, e1 := p.server.assistant.root.Stat(".")
	entry, e2 := p.server.assistant.root.Lstat(".data-work.lock")
	held, e3 := p.work.Stat()
	if e1 != nil || e2 != nil || e3 != nil || !os.SameFile(folder, p.folderInfo) || !os.SameFile(held, p.workInfo) || !os.SameFile(entry, p.workInfo) || !sameDirectory(p.server.configDir, folder) || ownerOnlyFile(".data-work.lock", entry.Mode()) != nil || ownedByUs(".data-work.lock", entry) != nil {
		return errors.Join(errAssistantRunScope, e1, e2, e3)
	}
	return nil
}
func (p *assistantDialectCompleteProjectionRead) Get(id string) (inputPreparationReply, error) {
	if err := p.Check(context.Background()); err != nil {
		return inputPreparationReply{}, err
	}
	if p.view == nil {
		return inputPreparationReply{}, errAssistantRunScope
	}
	return p.view.Get(id)
}
func (p *assistantDialectCompleteProjectionRead) Close() error {
	if p == nil {
		return nil
	}
	if p.self != p {
		return errAssistantRunScope
	}
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	if p.view != nil {
		p.closeErr = errors.Join(p.closeErr, p.view.Close())
		p.view = nil
	}
	if p.work != nil {
		p.closeErr = errors.Join(p.closeErr, p.work.Close())
		p.work = nil
	}
	return p.closeErr
}
func (s *Server) acquireAssistantDialectCompleteProjection(r *http.Request, id string, write bool) (_ *assistantDialectCompleteProjectionRead, err error) {
	need := false
	var binding assistantRunBinding
	store := s.assistantRuns()
	_, err = store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		run, e := assistantChatProjectionRun(ledger, id)
		if e != nil {
			return false, e
		}
		if e = s.authorizeAssistantRun(r, *run); e != nil {
			return false, e
		}
		// Preserve the exact already-persisted-output acknowledgement path: it
		// needs no fresh complete source proof and no new work admission.
		if !write && run.ChatProjection != nil {
			return false, nil
		}
		if write && run.ChatProjection != nil {
			root, _, _, selected, e := s.assistantChatProjectionOwner(r, store, run)
			if e != nil {
				return false, e
			}
			e = root.Close()
			if e != nil {
				return false, e
			}
			if digestOf(selected) == run.ChatProjection.Digest {
				return false, nil
			}
		}
		for _, out := range run.DomainOutputs {
			if out.Kind == "dialect-action" && out.DialectAction != nil && out.DialectAction.Read.Complete != nil {
				need = true
			}
		}
		binding = run.Binding
		return false, nil
	})
	if err != nil || !need {
		return nil, err
	}
	p := &assistantDialectCompleteProjectionRead{server: s, runID: id, binding: binding}
	p.self = p
	defer func() {
		if err != nil {
			err = errors.Join(err, p.Close())
		}
	}()
	if s.assistant == nil || !s.assistant.usable() {
		return nil, errAssistantRunScope
	}
	p.folderInfo, err = s.assistant.root.Stat(".")
	if err != nil {
		return nil, err
	}
	p.workInfo, err = s.assistant.root.Lstat(".data-work.lock")
	if err != nil {
		return nil, err
	}
	if ownerOnlyFile(".data-work.lock", p.workInfo.Mode()) != nil || ownedByUs(".data-work.lock", p.workInfo) != nil {
		return nil, errAssistantRunScope
	}
	p.work, err = s.assistant.root.OpenFile(".data-work.lock", os.O_RDWR|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	if err = p.Check(r.Context()); err != nil {
		return nil, err
	}
	if err = lockPrivateData(p.work, false); err != nil {
		return nil, err
	}
	if err = p.Check(r.Context()); err != nil {
		return nil, err
	}
	return p, nil
}
func (p *assistantDialectCompleteProjectionRead) matches(run assistantRunRecord) bool {
	return p != nil && p.self == p && p.runID == run.ID && reflect.DeepEqual(p.binding, run.Binding)
}
