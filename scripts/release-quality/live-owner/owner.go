package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

const (
	caseTimeout      = 120 * time.Second
	aggregateTimeout = 480 * time.Second
)

type command struct {
	Action   string `json:"action"`
	Case     int    `json:"case,omitempty"`
	ChatID   string `json:"chatId,omitempty"`
	ThreadID string `json:"threadId,omitempty"`
	TurnID   string `json:"turnId,omitempty"`
}

type caseEvidence struct {
	CaseID      string                        `json:"caseId"`
	ChatID      string                        `json:"chatId,omitempty"`
	StartedAt   time.Time                     `json:"startedAt,omitempty"`
	CompletedAt time.Time                     `json:"completedAt,omitempty"`
	Completed   bool                          `json:"completed"`
	Accounting  *codexbridge.AccountingReport `json:"accounting,omitempty"`
}

type lifecycleEvidence struct {
	SchemaVersion           int                           `json:"schemaVersion"`
	Reason                  string                        `json:"reason"`
	NormalOwnerShutdown     bool                          `json:"normalOwnerShutdown"`
	ShutdownError           bool                          `json:"shutdownError"`
	DeadlineCallbacksJoined bool                          `json:"deadlineCallbacksJoined"`
	Dropped                 uint64                        `json:"accountingDropped"`
	Cases                   [2]caseEvidence               `json:"cases"`
	Unexpected              *codexbridge.AccountingReport `json:"unexpectedAccounting,omitempty"`
	// These are observations/completion criteria, never admission caps. Browser
	// callback counts, full answers, provenance and authorization are separate.
	InvocationCriteria string `json:"invocationCriteria"`
	UsageFinality      string `json:"usageFinality"`
	MonetaryCost       *int   `json:"monetaryCost"`
}

type deadlineWatch struct {
	ctx      context.Context
	cancel   context.CancelFunc
	stop     func() bool
	done     chan struct{}
	joinOnce sync.Once
	allowed  bool
}

func watchDeadline(parent context.Context, duration time.Duration, expire func()) *deadlineWatch {
	ctx, cancel := context.WithTimeout(parent, duration)
	w := &deadlineWatch{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	w.stop = context.AfterFunc(ctx, func() {
		defer close(w.done)
		expire()
	})
	return w
}

func (w *deadlineWatch) withinDeadline(now time.Time) bool {
	deadline, ok := w.ctx.Deadline()
	return ok && now.Before(deadline)
}

// Stopping an AfterFunc only prevents its callback; it does not prove that the
// deadline is still open. Check expiry after stopping, before our own cancel.
// If the callback has already won, join the actual Close outside the owner lock.
func (w *deadlineWatch) join() bool {
	if w == nil {
		return true
	}
	w.joinOnce.Do(func() {
		stopped := w.stop()
		w.allowed = stopped && w.ctx.Err() == nil && w.withinDeadline(time.Now())
		w.cancel()
		if !stopped {
			<-w.done
		}
	})
	return w.allowed
}

type owner struct {
	mu                              sync.Mutex
	closing                         bool
	reason                          string
	closeErr                        error
	closed                          chan struct{}
	closeFn                         func() error
	options                         options
	reports                         <-chan codexbridge.AccountingReport
	dropped                         func() uint64
	setup, aggregate, current       *deadlineWatch
	active, completed               int
	cases                           [2]caseEvidence
	unexpected                      *codexbridge.AccountingReport
	caseDuration, aggregateDuration time.Duration
}

func newOwner(o options, reports <-chan codexbridge.AccountingReport, dropped func() uint64, closeFn func() error) *owner {
	h := &owner{options: o, reports: reports, dropped: dropped, closeFn: closeFn, closed: make(chan struct{}), caseDuration: caseTimeout, aggregateDuration: aggregateTimeout}
	h.cases[0].CaseID, h.cases[1].CaseID = o.Case1, o.Case2
	h.setup = watchDeadline(context.Background(), o.SetupTimeout, func() { h.close("setup_deadline") })
	return h
}

// Closing admission happens before invoking the potentially blocking real
// Server.Close. Every caller can wait for the same actual cleanup completion.
func (h *owner) close(reason string) {
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		<-h.closed
		return
	}
	h.closing, h.reason = true, reason
	h.mu.Unlock()
	err := h.closeFn()
	h.mu.Lock()
	h.closeErr = err
	h.mu.Unlock()
	close(h.closed)
}

func (h *owner) isClosing() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closing
}

func (h *owner) begin(c command) error {
	if c.Action != "begin" || c.Case != h.completed+1 || c.Case > 2 || h.active != 0 || !identifier(c.ChatID) || c.ThreadID != "" || c.TurnID != "" || (c.Case == 2 && c.ChatID == h.cases[0].ChatID) {
		return errControl
	}
	if c.Case == 1 {
		if !h.setup.join() {
			return errControl
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if h.closing || (c.Case == 1 && !h.setup.withinDeadline(now)) ||
		(h.aggregate != nil && (h.aggregate.ctx.Err() != nil || !h.aggregate.withinDeadline(now))) {
		return errControl
	}
	if h.aggregate == nil {
		h.aggregate = watchDeadline(context.Background(), h.aggregateDuration, func() { h.close("aggregate_deadline") })
	}
	h.active = c.Case
	h.cases[c.Case-1].ChatID = c.ChatID
	h.cases[c.Case-1].StartedAt = now.UTC()
	h.current = watchDeadline(h.aggregate.ctx, h.caseDuration, func() { h.close("case_deadline") })
	return nil
}

func (h *owner) observe(report codexbridge.AccountingReport) error {
	if h.active == 0 || h.cases[h.active-1].Accounting != nil || report.Invocation != uint64(h.active) {
		if h.unexpected == nil {
			h.unexpected = &report
		}
		return errControl
	}
	h.cases[h.active-1].Accounting = &report
	// Missing usage and unproven finality stay in the original report. Missing
	// usage makes this run incomplete; it is never turned into observed zero.
	if report.SchemaVersion != 1 || !report.Admitted || !report.OwnedClientReaped || !report.ApplicationSuccess || !report.TerminalObserved || report.TerminalStatus != "completed" || report.ExitReason != "success" || !report.UsagePresent || !report.UsageSchemaValid || report.Incomplete || report.Malformed || report.ScopeMismatch || report.Overflow || report.SequenceExhausted || report.FixedModelIneligible || report.RerouteObserved || report.RoutingDisagreement || report.RequestedModel != h.options.Model || report.AcknowledgedModel != h.options.Model || report.AcknowledgedProvider != "openai" || report.SelectedEffort != h.options.Effort || report.RequestedEffort.State != "value" || report.RequestedEffort.Value != h.options.Effort || (report.AcknowledgedEffort.State != "absent" && report.AcknowledgedEffort.State != "null" && report.AcknowledgedEffort.State != "value") || (report.AcknowledgedEffort.State == "value" && report.AcknowledgedEffort.Value != h.options.Effort) || !identifier(report.ThreadID) || !identifier(report.TurnID) {
		return errControl
	}
	return nil
}

func (h *owner) complete(c command) error {
	if c.Action != "complete" || h.active == 0 || c.Case != h.active || c.ChatID != h.cases[h.active-1].ChatID {
		return errControl
	}
	r := h.cases[h.active-1].Accounting
	if r == nil || c.ThreadID != r.ThreadID || c.TurnID != r.TurnID || !h.current.join() {
		return errControl
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if h.closing || !h.current.withinDeadline(now) || h.aggregate.ctx.Err() != nil || !h.aggregate.withinDeadline(now) {
		return errControl
	}
	h.cases[h.active-1].Completed = true
	h.cases[h.active-1].CompletedAt = now.UTC()
	h.completed = h.active
	h.active = 0
	return nil
}

func (h *owner) drain() error {
	for {
		select {
		case r := <-h.reports:
			if err := h.observe(r); err != nil {
				return err
			}
		default:
			if h.dropped() != 0 {
				return errControl
			}
			return nil
		}
	}
}

func (h *owner) finish() lifecycleEvidence {
	<-h.closed
	// An AfterFunc may still be returning even after Close finished. Join all
	// watches before publishing any app-level final shutdown assertion.
	h.setup.join()
	h.current.join()
	h.aggregate.join()
	_ = h.drain()
	h.mu.Lock()
	defer h.mu.Unlock()
	return lifecycleEvidence{SchemaVersion: 1, Reason: h.reason, NormalOwnerShutdown: h.closeErr == nil,
		ShutdownError: h.closeErr != nil, DeadlineCallbacksJoined: true, Dropped: h.dropped(), Cases: h.cases,
		Unexpected: h.unexpected, InvocationCriteria: "one completed observed invocation per case; not a prospective cap",
		UsageFinality: "unproven", MonetaryCost: nil}
}

func (h *owner) control(ctx context.Context, root *os.Root) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	next := 1
	fail := func(reason string) error { h.close(reason); return errControl }
	var controlErr error
	for !h.isClosing() {
		select {
		case <-ctx.Done():
			controlErr = fail("owner_canceled")
		case <-h.closed:
		case <-ticker.C:
			if raw, err := readPrivate(root, "abort.json"); err == nil {
				c, parseErr := decodeCommand(raw)
				if parseErr != nil || c != (command{Action: "abort"}) {
					controlErr = fail("control_refused")
				} else {
					controlErr = fail("driver_aborted")
				}
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				controlErr = fail("control_refused")
				continue
			}
			if err := h.drain(); err != nil {
				controlErr = fail("accounting_anomaly")
				continue
			}
			if h.active != 0 && h.cases[h.active-1].Accounting != nil {
				name := fmt.Sprintf("case-%d-accounting.json", h.active)
				if _, err := root.Lstat(name); errors.Is(err, os.ErrNotExist) {
					if writePrivate(root, name, h.cases[h.active-1].Accounting) != nil {
						controlErr = fail("output_refused")
						continue
					}
				}
			}
			raw, err := readPrivate(root, fmt.Sprintf("%03d.json", next))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			c, parseErr := decodeCommand(raw)
			if err != nil || parseErr != nil {
				controlErr = fail("control_refused")
				continue
			}
			if next%2 == 1 {
				err = h.begin(c)
			} else {
				err = h.complete(c)
			}
			if err != nil {
				controlErr = fail("case_refused")
				continue
			}
			if writePrivate(root, fmt.Sprintf("%03d-ack.json", next), c) != nil {
				controlErr = fail("output_refused")
				continue
			}
			next++
			if h.completed == 2 {
				h.close("completed")
			}
		}
	}
	evidence := h.finish()
	if writePrivate(root, "lifecycle.json", evidence) != nil || evidence.Reason != "completed" || evidence.ShutdownError || evidence.Dropped != 0 || evidence.Unexpected != nil {
		return errControl
	}
	return controlErr
}
