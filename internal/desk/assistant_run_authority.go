package desk

import (
	"context"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// assistantRunPhysicalAuthority retains the actual original objects, rather
// than a nonnil lease pointer or a new wrapper around the same journal.
type assistantRunPhysicalAuthority struct {
	execution *assistantRunExecution
	writer    *assistantRunWriter
	lease     *assistantRunLease
	store     *assistantCanonicalRunStore
	id        string
	binding   *runcontext.RunBinding
}

func holdAssistantRunPhysicalAuthority(e *assistantRunExecution) (*assistantRunPhysicalAuthority, error) {
	e.canonicalMu.Lock()
	defer e.canonicalMu.Unlock()
	if e.stopRequested || e.writer == nil || e.writer.lease == nil || e.writer.id != e.record.ID || e.ctx.Err() != nil {
		return nil, errAssistantRunScope
	}
	a := &assistantRunPhysicalAuthority{execution: e, writer: e.writer, lease: e.writer.lease, id: e.writer.id, binding: e.canonicalBinding}
	if a.binding != nil {
		var ok bool
		a.store, ok = a.binding.Writer.Store.(*assistantCanonicalRunStore)
		if !ok || a.store.writer != a.writer || a.store.id != a.binding.Writer.ID {
			return nil, errAssistantRunScope
		}
	}
	return a, nil
}

// write linearizes physical admission with Stop under canonicalMu, after all
// potentially blocking source/owner observations. Lock order is canonicalMu ->
// writer.mu -> lease.mu, matching canonical Commit. The exact lease remains
// live through Write; writer.mu and canonicalMu are released BEFORE pipe I/O.
// Stop that wins this admission lock refuses. Stop after admission cancels the
// transport, whose context hook closes stdin independently of these locks, so
// a blocked write cannot prevent cancellation. A dispatched/partial write stays
// uncertain; no cancellation result is evidence of zero dispatch or permission
// to replay.
func (a *assistantRunPhysicalAuthority) write(ctx context.Context, write func() error) error {
	e := a.execution
	e.canonicalMu.Lock()
	if e.stopRequested || e.writer != a.writer || e.canonicalBinding != a.binding || e.ctx.Err() != nil || ctx.Err() != nil || a.writer.id != a.id || a.writer.lease != a.lease {
		e.canonicalMu.Unlock()
		return errAssistantRunScope
	}
	if a.binding != nil && (a.binding.Writer.Store != a.store || a.store.writer != a.writer || a.binding.Writer.ID != a.store.id) {
		e.canonicalMu.Unlock()
		return errAssistantRunScope
	}
	a.writer.mu.Lock()
	if a.writer.failure != nil {
		err := a.writer.failure
		a.writer.mu.Unlock()
		e.canonicalMu.Unlock()
		return err
	}
	a.lease.mu.Lock()
	if a.lease.file == nil || a.lease.recoveryErr != nil || e.ctx.Err() != nil || ctx.Err() != nil {
		a.lease.mu.Unlock()
		a.writer.mu.Unlock()
		e.canonicalMu.Unlock()
		return errAssistantRunScope
	}
	a.writer.mu.Unlock()
	e.canonicalMu.Unlock()
	defer a.lease.mu.Unlock()
	return write()
}
