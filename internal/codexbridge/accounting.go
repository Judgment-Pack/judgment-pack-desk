package codexbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
)

const accountingCapacity = 16

// AccountingSink is an optional host-only, fixed-capacity observation channel.
// It never calls consumer code, blocks a Run, closes its channel or owns IO.
// Use NewAccountingSink; a zero-value sink drops reports safely.
type AccountingSink struct {
	reports  chan AccountingReport
	dropped  atomic.Uint64
	sequence atomic.Uint64
}

func NewAccountingSink() *AccountingSink {
	return &AccountingSink{reports: make(chan AccountingReport, accountingCapacity)}
}
func (s *AccountingSink) Reports() <-chan AccountingReport { return s.reports }
func (s *AccountingSink) Dropped() uint64                  { return s.dropped.Load() }
func (s *AccountingSink) tryPublish(r AccountingReport) {
	select {
	case s.reports <- r:
	default:
		s.dropped.Add(1)
	}
}
func (s *AccountingSink) next() uint64 {
	for {
		prior := s.sequence.Load()
		if prior == math.MaxUint64 {
			return 0
		}
		if s.sequence.CompareAndSwap(prior, prior+1) {
			return prior + 1
		}
	}
}

// AccountingValue distinguishes absent, null, invalid and observed zero. Only
// State == "value" is a provider-reported nonnegative int64 observation.
type AccountingValue struct {
	State string `json:"state"`
	Value int64  `json:"value"`
}
type AccountingEffort struct {
	State string `json:"state"`
	Value string `json:"value,omitempty"`
}
type AccountingBreakdown struct {
	Input           AccountingValue `json:"input_tokens"`
	CachedInput     AccountingValue `json:"cached_input_tokens"`
	Output          AccountingValue `json:"output_tokens"`
	ReasoningOutput AccountingValue `json:"reasoning_output_tokens"`
	Total           AccountingValue `json:"total_tokens"`
	CacheWriteInput AccountingValue `json:"cache_write_input_tokens"`
}
type AccountingUsage struct {
	Observation   uint16              `json:"observation"`
	LastScope     string              `json:"last_scope"`
	TotalScope    string              `json:"total_scope"`
	Last          AccountingBreakdown `json:"last"`
	Total         AccountingBreakdown `json:"total"`
	ContextWindow AccountingValue     `json:"model_context_window"`
}
type AccountingReroute struct {
	ThreadID  string `json:"thread_id"`
	TurnID    string `json:"turn_id"`
	FromModel string `json:"from_model"`
	ToModel   string `json:"to_model"`
	Reason    string `json:"reason"`
}

// AccountingReport is private host evidence, never a RunEvent or canonical
// receipt. All strings are bounded identifiers or closed local enum values.
// Counters are last observed, never summed. No schema marker proves final usage.
type AccountingReport struct {
	SchemaVersion         uint8                `json:"schema_version"`
	Invocation            uint64               `json:"invocation"`
	SequenceExhausted     bool                 `json:"sequence_exhausted"`
	RequestedModel        string               `json:"requested_model,omitempty"`
	RequestedEffort       AccountingEffort     `json:"requested_effort"`
	SelectedEffort        string               `json:"selected_effort,omitempty"`
	AcknowledgedModel     string               `json:"acknowledged_model,omitempty"`
	AcknowledgedProvider  string               `json:"acknowledged_provider,omitempty"`
	AcknowledgedEffort    AccountingEffort     `json:"acknowledged_effort"`
	ThreadID              string               `json:"thread_id,omitempty"`
	TurnID                string               `json:"turn_id,omitempty"`
	Admitted              bool                 `json:"admitted"`
	OwnedClientReaped     bool                 `json:"owned_client_reaped"`
	ApplicationSuccess    bool                 `json:"application_success"`
	ExitReason            string               `json:"exit_reason"`
	Notifications         uint16               `json:"notifications"`
	UsageUpdates          uint16               `json:"usage_updates"`
	UsagePresent          bool                 `json:"usage_present"`
	UsageSchemaValid      bool                 `json:"usage_schema_valid"`
	Usage                 AccountingUsage      `json:"last_observed"`
	TerminalObserved      bool                 `json:"terminal_observed"`
	TerminalStatus        string               `json:"terminal_status,omitempty"`
	TerminalUsageFinality string               `json:"terminal_usage_finality"`
	Malformed             bool                 `json:"malformed"`
	ScopeMismatch         bool                 `json:"scope_mismatch"`
	Overflow              bool                 `json:"overflow"`
	RoutingDisagreement   bool                 `json:"routing_disagreement"`
	RerouteObserved       bool                 `json:"reroute_observed"`
	RerouteCount          uint8                `json:"reroute_count"`
	Reroutes              [8]AccountingReroute `json:"reroutes"`
	Incomplete            bool                 `json:"incomplete"`
	FixedModelIneligible  bool                 `json:"fixed_model_ineligible"`
}

type accountingObservation struct{ report AccountingReport }

func newAccountingObservation(s *AccountingSink, request RunRequest) *accountingObservation {
	if s == nil {
		return nil
	}
	a := &accountingObservation{report: AccountingReport{SchemaVersion: 1, Invocation: s.next(), TerminalUsageFinality: "unproven", RequestedEffort: AccountingEffort{State: "absent"}, AcknowledgedEffort: AccountingEffort{State: "absent"}, Usage: emptyAccountingUsage()}}
	a.report.SequenceExhausted = a.report.Invocation == 0
	if accountingModel(request.Model) {
		a.report.RequestedModel = strings.Clone(request.Model)
	} else {
		a.report.Malformed = true
	}
	if request.Effort != "" {
		a.report.RequestedEffort = accountingEffort(request.Effort)
		a.report.Malformed = a.report.Malformed || a.report.RequestedEffort.State != "value"
	}
	return a
}
func (a *accountingObservation) finish(s *AccountingSink, err error) {
	if a == nil {
		return
	}
	r := &a.report
	r.ApplicationSuccess = err == nil
	switch {
	case err == nil:
		r.ExitReason = "success"
	case errors.Is(err, context.Canceled):
		r.ExitReason = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		r.ExitReason = "deadline"
	case errors.Is(err, ErrBusy):
		r.ExitReason = "busy"
	case errors.Is(err, ErrSignIn):
		r.ExitReason = "sign_in_required"
	case errors.Is(err, ErrLimit):
		r.ExitReason = "limit"
	case errors.Is(err, ErrRetries):
		r.ExitReason = "retries_exhausted"
	case errors.Is(err, ErrUnavailable), errors.Is(err, ErrNotInstalled):
		r.ExitReason = "unavailable"
	default:
		r.ExitReason = "refused"
	}
	r.Incomplete = !r.ApplicationSuccess || !r.Admitted || !r.OwnedClientReaped || !r.TerminalObserved || !r.UsagePresent || !r.UsageSchemaValid || r.Malformed || r.ScopeMismatch || r.Overflow || r.SequenceExhausted
	r.FixedModelIneligible = r.RerouteObserved || r.RoutingDisagreement
	s.tryPublish(*r)
}
func accountingModel(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':' || c == '/') {
			return false
		}
	}
	return true
}
func accountingEffort(s string) AccountingEffort {
	switch s {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return AccountingEffort{State: "value", Value: strings.Clone(s)}
	}
	return AccountingEffort{State: "invalid"}
}
func emptyAccountingBreakdown() AccountingBreakdown {
	absent := AccountingValue{State: "absent"}
	return AccountingBreakdown{absent, absent, absent, absent, absent, absent}
}
func emptyAccountingUsage() AccountingUsage {
	return AccountingUsage{LastScope: "provider_last_breakdown", TotalScope: "provider_thread_total", Last: emptyAccountingBreakdown(), Total: emptyAccountingBreakdown(), ContextWindow: AccountingValue{State: "absent"}}
}

// accountingReader only traverses existing frames. Explicit depth and token
// budgets bound ignored fields too. It retains no frames, maps or unknown data.
// Duplicate known members are ambiguous and never accepted as observations.
type accountingReader struct {
	d             *json.Decoder
	tokens        int
	depth         int
	bad, overflow bool
}

func newAccountingReader(raw []byte) *accountingReader {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	return &accountingReader{d: d}
}
func (p *accountingReader) token() json.Token {
	if p.bad || p.overflow {
		return nil
	}
	p.tokens++
	if p.tokens > 512 {
		p.overflow = true
		return nil
	}
	t, e := p.d.Token()
	if e != nil {
		p.bad = true
	}
	return t
}
func accountingKey(k string) uint64 {
	// The same finite key table is used at every level; unknown keys are skipped.
	switch k {
	case "threadId":
		return 1 << 0
	case "turnId":
		return 1 << 1
	case "tokenUsage":
		return 1 << 2
	case "last":
		return 1 << 3
	case "total":
		return 1 << 4
	case "modelContextWindow":
		return 1 << 5
	case "inputTokens":
		return 1 << 6
	case "cachedInputTokens":
		return 1 << 7
	case "outputTokens":
		return 1 << 8
	case "reasoningOutputTokens":
		return 1 << 9
	case "totalTokens":
		return 1 << 10
	case "cacheWriteInputTokens":
		return 1 << 11
	case "fromModel":
		return 1 << 12
	case "toModel":
		return 1 << 13
	case "reason":
		return 1 << 14
	case "turn":
		return 1 << 15
	case "id":
		return 1 << 16
	case "status":
		return 1 << 17
	case "thread":
		return 1 << 18
	case "model":
		return 1 << 19
	case "modelProvider":
		return 1 << 20
	case "reasoningEffort":
		return 1 << 21
	}
	return 0
}
func (p *accountingReader) object(read func(string, bool)) {
	if p.token() != json.Delim('{') {
		p.bad = true
		return
	}
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 12 {
		p.overflow = true
		return
	}
	var seen uint64
	for !p.bad && !p.overflow && p.d.More() {
		key, ok := p.token().(string)
		if !ok {
			p.bad = true
			return
		}
		bit := accountingKey(key)
		duplicate := bit != 0 && seen&bit != 0
		seen |= bit
		read(key, duplicate)
	}
	if !p.bad && !p.overflow && p.token() != json.Delim('}') {
		p.bad = true
	}
}
func (p *accountingReader) skip() { p.skipToken(p.token()) }
func (p *accountingReader) skipToken(t json.Token) {
	if p.bad || p.overflow {
		return
	}
	d, ok := t.(json.Delim)
	if !ok {
		return
	}
	if d != '{' && d != '[' {
		p.bad = true
		return
	}
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 12 {
		p.overflow = true
		return
	}
	for !p.bad && !p.overflow && p.d.More() {
		if d == '{' {
			if _, ok := p.token().(string); !ok {
				p.bad = true
				return
			}
		}
		p.skip()
	}
	end := json.Delim(']')
	if d == '{' {
		end = '}'
	}
	if !p.bad && !p.overflow && p.token() != end {
		p.bad = true
	}
}
func (p *accountingReader) end() bool {
	if p.bad || p.overflow {
		return false
	}
	_, err := p.d.Token()
	if err != io.EOF {
		p.bad = true
	}
	return !p.bad
}
func (p *accountingReader) text(duplicate bool, validate func(string) bool) string {
	t := p.token()
	s, ok := t.(string)
	if !ok {
		p.skipToken(t)
	}
	if duplicate || !ok || !validate(s) {
		return ""
	}
	return strings.Clone(s)
}
func (p *accountingReader) number(duplicate, nullable bool) AccountingValue {
	t := p.token()
	if duplicate {
		p.skipToken(t)
		return AccountingValue{State: "invalid"}
	}
	if t == nil && !p.bad && !p.overflow {
		if nullable {
			return AccountingValue{State: "null"}
		}
		return AccountingValue{State: "invalid"}
	}
	if n, ok := t.(json.Number); ok {
		if v, e := strconv.ParseInt(string(n), 10, 64); e == nil && v >= 0 {
			return AccountingValue{State: "value", Value: v}
		}
	}
	p.skipToken(t)
	return AccountingValue{State: "invalid"}
}
func (p *accountingReader) breakdown(b *AccountingBreakdown) {
	p.object(func(key string, duplicate bool) {
		var value *AccountingValue
		switch key {
		case "inputTokens":
			value = &b.Input
		case "cachedInputTokens":
			value = &b.CachedInput
		case "outputTokens":
			value = &b.Output
		case "reasoningOutputTokens":
			value = &b.ReasoningOutput
		case "totalTokens":
			value = &b.Total
		case "cacheWriteInputTokens":
			value = &b.CacheWriteInput
		}
		if value == nil {
			p.skip()
			return
		}
		*value = p.number(duplicate, false)
	})
}
func validAccountingBreakdown(b AccountingBreakdown) bool {
	return b.Input.State == "value" && b.CachedInput.State == "value" && b.Output.State == "value" && b.ReasoningOutput.State == "value" && b.Total.State == "value" && (b.CacheWriteInput.State == "absent" || b.CacheWriteInput.State == "value")
}
func (a *accountingObservation) scoped(thread, turn string) bool {
	if thread == "" || turn == "" {
		a.report.Malformed = true
		return false
	}
	if thread != a.report.ThreadID || turn != a.report.TurnID || a.report.TurnID == "" {
		a.report.ScopeMismatch = true
		return false
	}
	return true
}
func (a *accountingObservation) observeUsage(raw []byte) {
	r := &a.report
	r.UsageUpdates++
	u := emptyAccountingUsage()
	u.Observation = r.UsageUpdates
	var thread, turn string
	usageSeen, duplicateUsage := false, false
	p := newAccountingReader(raw)
	p.object(func(key string, duplicate bool) {
		switch key {
		case "threadId":
			thread = p.text(duplicate, validID)
		case "turnId":
			turn = p.text(duplicate, validID)
		case "tokenUsage":
			usageSeen = true
			duplicateUsage = duplicateUsage || duplicate
			p.object(func(key string, duplicate bool) {
				switch key {
				case "last":
					p.breakdown(&u.Last)
					if duplicate {
						duplicateUsage = true
					}
				case "total":
					p.breakdown(&u.Total)
					if duplicate {
						duplicateUsage = true
					}
				case "modelContextWindow":
					u.ContextWindow = p.number(duplicate, true)
				default:
					p.skip()
				}
			})
		default:
			p.skip()
		}
	})
	parsed := p.end()
	r.Overflow = r.Overflow || p.overflow
	valid := parsed && usageSeen && !duplicateUsage && validAccountingBreakdown(u.Last) && validAccountingBreakdown(u.Total) && u.ContextWindow.State != "invalid"
	if !valid {
		r.Malformed = true
	}
	if !a.scoped(thread, turn) || !parsed || duplicateUsage {
		return
	}
	// A malformed later update cannot erase a known snapshot. An initial partial
	// snapshot keeps valid members with explicit missingness, without mixing updates.
	if valid || !r.UsagePresent {
		r.Usage = u
		r.UsagePresent = usageSeen
		r.UsageSchemaValid = valid
	}
}
func (a *accountingObservation) observeReroute(raw []byte) {
	r := &a.report
	r.RerouteObserved = true
	var v AccountingReroute
	p := newAccountingReader(raw)
	p.object(func(key string, duplicate bool) {
		switch key {
		case "threadId":
			v.ThreadID = p.text(duplicate, validID)
		case "turnId":
			v.TurnID = p.text(duplicate, validID)
		case "fromModel":
			v.FromModel = p.text(duplicate, accountingModel)
		case "toModel":
			v.ToModel = p.text(duplicate, accountingModel)
		case "reason":
			v.Reason = p.text(duplicate, func(s string) bool { return s == "highRiskCyberActivity" })
		default:
			p.skip()
		}
	})
	parsed := p.end()
	r.Overflow = r.Overflow || p.overflow
	if !parsed || v.FromModel == "" || v.ToModel == "" || v.Reason == "" {
		r.Malformed = true
		return
	}
	if !a.scoped(v.ThreadID, v.TurnID) {
		return
	}
	if int(r.RerouteCount) == len(r.Reroutes) {
		r.Overflow = true
		return
	}
	r.Reroutes[r.RerouteCount] = v
	r.RerouteCount++
}
func (a *accountingObservation) observeTerminal(raw []byte) {
	var thread, turn, status string
	p := newAccountingReader(raw)
	duplicateTurn := false
	p.object(func(key string, duplicate bool) {
		switch key {
		case "threadId":
			thread = p.text(duplicate, validID)
		case "turn":
			duplicateTurn = duplicateTurn || duplicate
			p.object(func(key string, duplicate bool) {
				switch key {
				case "id":
					turn = p.text(duplicate, validID)
				case "status":
					status = p.text(duplicate, func(s string) bool {
						return s == "completed" || s == "failed" || s == "interrupted" || s == "inProgress"
					})
				default:
					p.skip()
				}
			})
		default:
			p.skip()
		}
	})
	parsed := p.end()
	a.report.Overflow = a.report.Overflow || p.overflow
	if !parsed || duplicateTurn || status == "" {
		a.report.Malformed = true
		return
	}
	if a.scoped(thread, turn) {
		a.report.TerminalObserved = true
		a.report.TerminalStatus = status
	}
}
func (a *accountingObservation) observeStart(raw []byte) {
	r := &a.report
	var thread, model, provider string
	effort := AccountingEffort{State: "absent"}
	duplicateThread := false
	p := newAccountingReader(raw)
	p.object(func(key string, duplicate bool) {
		switch key {
		case "thread":
			duplicateThread = duplicateThread || duplicate
			p.object(func(key string, duplicate bool) {
				if key == "id" {
					thread = p.text(duplicate, validID)
				} else {
					p.skip()
				}
			})
		case "model":
			model = p.text(duplicate, accountingModel)
		case "modelProvider":
			provider = p.text(duplicate, func(s string) bool { return s == "openai" })
		case "reasoningEffort":
			token := p.token()
			if token == nil && !p.bad && !p.overflow {
				effort = AccountingEffort{State: "null"}
			} else if value, ok := token.(string); ok {
				effort = accountingEffort(value)
			} else {
				p.skipToken(token)
				effort = AccountingEffort{State: "invalid"}
			}
			if duplicate {
				effort = AccountingEffort{State: "invalid"}
			}
		default:
			p.skip()
		}
	})
	parsed := p.end()
	r.Overflow = r.Overflow || p.overflow
	if !parsed {
		r.Malformed = true
		return
	}
	if !duplicateThread {
		r.ThreadID = thread
	}
	r.AcknowledgedModel = model
	r.AcknowledgedProvider = provider
	r.AcknowledgedEffort = effort
	if thread == "" || duplicateThread || model == "" || provider == "" || effort.State == "invalid" {
		r.Malformed = true
	}
	r.RoutingDisagreement = model != r.RequestedModel || provider != "openai" || (effort.State == "value" && effort.Value != r.SelectedEffort)
}
