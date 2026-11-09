package codexbridge

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"unsafe"
)

// Handwritten native counters intentionally do not obey arithmetic identities.
const accountingFirst = `{"threadId":"thread","turnId":"turn","tokenUsage":{"last":{"inputTokens":2,"cachedInputTokens":1,"outputTokens":3,"reasoningOutputTokens":2,"totalTokens":5},"total":{"inputTokens":7,"cachedInputTokens":1,"outputTokens":3,"reasoningOutputTokens":2,"totalTokens":10}}}`
const accountingSecond = `{"threadId":"thread","turnId":"turn","tokenUsage":{"last":{"inputTokens":4,"cachedInputTokens":2,"outputTokens":3,"reasoningOutputTokens":1,"totalTokens":7,"cacheWriteInputTokens":0},"total":{"inputTokens":11,"cachedInputTokens":2,"outputTokens":6,"reasoningOutputTokens":3,"totalTokens":17,"cacheWriteInputTokens":2},"modelContextWindow":256000}}`
const accountingRerouteFixture = `{"threadId":"thread","turnId":"turn","fromModel":"model","toModel":"other-model","reason":"highRiskCyberActivity"}`

func accountingFixture() *accountingObservation {
	a := newAccountingObservation(NewAccountingSink(), RunRequest{Model: "model", Effort: "medium"})
	a.report.SelectedEffort = "medium"
	a.report.ThreadID = "thread"
	a.report.TurnID = "turn"
	return a
}
func TestAccountingLatestScopedCountersAndPresence(t *testing.T) {
	a := accountingFixture()
	a.observeUsage([]byte(accountingFirst))
	if a.report.Usage.Total.Total.Value != 10 || a.report.Usage.Last.CacheWriteInput.State != "absent" || a.report.Usage.ContextWindow.State != "absent" {
		t.Fatalf("first observation: %+v", a.report)
	}
	a.observeUsage([]byte(accountingSecond))
	r := a.report
	if r.UsageUpdates != 2 || r.Usage.Observation != 2 || !r.UsageSchemaValid || r.Usage.Total.Total.Value != 17 || r.Usage.Last.Total.Value != 7 || r.Usage.Last.ReasoningOutput.Value != 1 || r.Usage.Last.CacheWriteInput.State != "value" || r.Usage.Last.CacheWriteInput.Value != 0 || r.Usage.ContextWindow.Value != 256000 {
		t.Fatalf("latest observation: %+v", r)
	}
	if r.Usage.LastScope != "provider_last_breakdown" || r.Usage.TotalScope != "provider_thread_total" || r.TerminalUsageFinality != "unproven" {
		t.Fatal("scope/finality changed")
	}
	a.observeUsage([]byte(strings.Replace(accountingSecond, "256000", "null", 1)))
	if a.report.Usage.ContextWindow.State != "null" || a.report.Malformed {
		t.Fatal("nullable context unavailable")
	}
}
func TestAccountingRejectsInvalidRequiredMembersAndPreservesPrior(t *testing.T) {
	for _, member := range []string{"inputTokens", "cachedInputTokens", "outputTokens", "reasoningOutputTokens", "totalTokens"} {
		for _, value := range []string{"null", "-1", "1.5", "9223372036854775808", `"PRIVATE_SENTINEL"`, `{}`, `[]`} {
			t.Run(member+value, func(t *testing.T) {
				// Replace the first (last-breakdown) occurrence, written independently of decoder output.
				old := `"` + member + `":`
				start := strings.Index(accountingFirst, old) + len(old)
				end := strings.IndexAny(accountingFirst[start:], ",}") + start
				raw := accountingFirst[:start] + value + accountingFirst[end:]
				a := accountingFixture()
				a.observeUsage([]byte(raw))
				if !a.report.Malformed || a.report.UsageSchemaValid {
					t.Fatal("invalid required field accepted")
				}
				a = accountingFixture()
				a.observeUsage([]byte(accountingSecond))
				prior := a.report.Usage
				a.observeUsage([]byte(raw))
				if !a.report.Malformed || a.report.Usage != prior || a.report.UsageUpdates != 2 {
					t.Fatal("malformed update erased prior snapshot")
				}
			})
		}
		t.Run(member+"-missing", func(t *testing.T) {
			marker := `"` + member + `":`
			start := strings.Index(accountingFirst, marker)
			end := strings.IndexAny(accountingFirst[start+len(marker):], ",}") + start + len(marker)
			raw := accountingFirst[:start] + accountingFirst[end+1:]
			if accountingFirst[end] == '}' {
				raw = accountingFirst[:start-1] + accountingFirst[end:]
			}
			a := accountingFixture()
			a.observeUsage([]byte(raw))
			if !a.report.Malformed || a.report.UsageSchemaValid {
				t.Fatal("missing required member accepted")
			}
		})
	}
}
func TestAccountingAmbiguityScopeBoundsAndPrivateUnknowns(t *testing.T) {
	cases := []struct {
		name, raw       string
		overflow, scope bool
	}{
		{"duplicate-counter", strings.Replace(accountingFirst, `"inputTokens":2`, `"inputTokens":2,"inputTokens":9`, 1), false, false},
		{"duplicate-usage", strings.TrimSuffix(accountingFirst, "}") + `,"tokenUsage":{}}`, false, false},
		{"duplicate-scope", strings.Replace(accountingFirst, `"threadId":"thread"`, `"threadId":"thread","threadId":"thread"`, 1), false, false},
		{"foreign-thread", strings.Replace(accountingFirst, `"threadId":"thread"`, `"threadId":"other"`, 1), false, true},
		{"foreign-turn", strings.Replace(accountingFirst, `"turnId":"turn"`, `"turnId":"other"`, 1), false, true},
		{"long-id", strings.Replace(accountingFirst, `"threadId":"thread"`, `"threadId":"`+strings.Repeat("a", 257)+`"`, 1), false, false},
		{"depth", strings.TrimSuffix(accountingFirst, "}") + `,"private":` + strings.Repeat("[", 15) + `"PRIVATE_SENTINEL"` + strings.Repeat("]", 15) + `}`, true, false},
		{"members", strings.TrimSuffix(accountingFirst, "}") + `,"private":{` + strings.Repeat(`"x":0,`, 260) + `"last":0}}`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := accountingFixture()
			a.observeUsage([]byte(accountingSecond))
			prior := a.report.Usage
			a.observeUsage([]byte(tc.raw))
			if a.report.Usage != prior || (!a.report.Malformed && !a.report.ScopeMismatch) || a.report.Overflow != tc.overflow || a.report.ScopeMismatch != tc.scope {
				t.Fatalf("unsafe update accepted: %+v", a.report)
			}
		})
	}
	a := accountingFixture()
	raw := strings.TrimSuffix(accountingFirst, "}") + `,"auth":{"accessToken":"AUTH_SENTINEL"},"error":{"message":"ERROR_SENTINEL"},"items":[{"reasoning":"REASON_SENTINEL","text":"TEXT_SENTINEL"}],"unknown":"UNKNOWN_SENTINEL"}`
	a.observeUsage([]byte(raw))
	wire, _ := json.Marshal(a.report)
	if !a.report.UsageSchemaValid || a.report.Malformed || strings.Contains(string(wire), "SENTINEL") {
		t.Fatal("unknown private data retained or changed valid observation")
	}
	// The transient parser never makes the fixed retained representation grow.
	if unsafe.Sizeof(AccountingReport{}) >= 16<<10 || len(wire) >= 16<<10 {
		t.Fatal("report bound exceeded")
	}
}
func TestAccountingRoutingEffortAndTerminalFacts(t *testing.T) {
	for _, effort := range []struct{ raw, state string }{{"", "absent"}, {"null", "null"}, {`"medium"`, "value"}, {`"PRIVATE_SENTINEL"`, "invalid"}} {
		a := accountingFixture()
		raw := `{"thread":{"id":"thread"},"model":"model","modelProvider":"openai"}`
		if effort.raw != "" {
			raw = strings.TrimSuffix(raw, "}") + `,"reasoningEffort":` + effort.raw + `}`
		}
		a.observeStart([]byte(raw))
		if a.report.AcknowledgedEffort.State != effort.state || a.report.AcknowledgedEffort.Value == "PRIVATE_SENTINEL" {
			t.Fatal("effort missingness lost")
		}
	}
	a := accountingFixture()
	a.observeStart([]byte(`{"thread":{"id":"thread"},"model":"other","modelProvider":"openai"}`))
	if !a.report.RoutingDisagreement {
		t.Fatal("routing disagreement lost")
	}
	a = accountingFixture()
	for i := 0; i < 9; i++ {
		a.observeReroute([]byte(accountingRerouteFixture))
	}
	if a.report.RerouteCount != 8 || !a.report.RerouteObserved || !a.report.Overflow {
		t.Fatal("reroute bound lost")
	}
	for _, raw := range []string{strings.Replace(accountingRerouteFixture, "highRiskCyberActivity", "PRIVATE_SENTINEL", 1), strings.Replace(accountingRerouteFixture, `"threadId":"thread"`, `"threadId":"foreign"`, 1), strings.Replace(accountingRerouteFixture, `"fromModel":"model"`, `"fromModel":"`+strings.Repeat("m", 129)+`"`, 1)} {
		a := accountingFixture()
		a.observeReroute([]byte(raw))
		if a.report.RerouteCount != 0 || !a.report.RerouteObserved || (!a.report.Malformed && !a.report.ScopeMismatch) {
			t.Fatal("invalid reroute accepted")
		}
	}
	for _, status := range []string{"completed", "failed", "interrupted"} {
		a := accountingFixture()
		a.observeTerminal([]byte(`{"threadId":"thread","turn":{"id":"turn","status":"` + status + `","items":["PRIVATE_SENTINEL"],"error":"PRIVATE_SENTINEL"}}`))
		if !a.report.TerminalObserved || a.report.TerminalStatus != status || a.report.TerminalUsageFinality != "unproven" {
			t.Fatal("terminal observation incorrect")
		}
	}
	a = accountingFixture()
	a.observeTerminal([]byte(`{"threadId":"other","turn":{"id":"turn","status":"completed"}}`))
	if a.report.TerminalObserved || !a.report.ScopeMismatch {
		t.Fatal("foreign terminal accepted")
	}
}
func TestAccountingSinkFixedCapacityAndSequenceExhaustion(t *testing.T) {
	s := NewAccountingSink()
	for i := 0; i < accountingCapacity+3; i++ {
		a := newAccountingObservation(s, RunRequest{Model: "model"})
		a.finish(s, nil)
	}
	if s.Dropped() != 3 || len(s.reports) != 16 {
		t.Fatal("sink did not drop without blocking")
	}
	for i := 1; i <= 16; i++ {
		r := <-s.Reports()
		if r.Invocation != uint64(i) || r.OwnedClientReaped || !r.Incomplete {
			t.Fatal("sequence or pre-admission release claim")
		}
	}
	s.sequence.Store(math.MaxUint64)
	a := newAccountingObservation(s, RunRequest{Model: "model"})
	a.finish(s, nil)
	r := <-s.Reports()
	if r.Invocation != 0 || !r.SequenceExhausted || !r.Incomplete || s.sequence.Load() != math.MaxUint64 {
		t.Fatal("exhausted sequence reused identity")
	}
	var zero AccountingSink
	zero.tryPublish(AccountingReport{})
	if zero.Dropped() != 1 {
		t.Fatal("zero sink blocked")
	}
}

func TestAccountingStartRejectsDuplicateMetadataAndEffortDisagreement(t *testing.T) {
	for _, raw := range []string{
		`{"thread":{"id":"thread","id":"other"},"model":"model","modelProvider":"openai"}`,
		`{"thread":{"id":"thread"},"thread":{"id":"thread"},"model":"model","modelProvider":"openai"}`,
		`{"thread":{"id":"thread"},"model":"model","model":"model","modelProvider":"openai"}`,
		`{"thread":{"id":"thread"},"model":"model","modelProvider":"openai","reasoningEffort":"medium","reasoningEffort":"medium"}`,
	} {
		a := accountingFixture()
		a.observeStart([]byte(raw))
		if !a.report.Malformed {
			t.Fatal("ambiguous start accepted")
		}
	}
	a := accountingFixture()
	a.observeStart([]byte(`{"thread":{"id":"thread"},"model":"model","modelProvider":"openai","reasoningEffort":"high","error":"PRIVATE_SENTINEL","auth":{"accessToken":"PRIVATE_SENTINEL"}}`))
	if !a.report.RoutingDisagreement || a.report.Malformed || a.report.AcknowledgedEffort.Value != "high" {
		t.Fatal("acknowledgement mismatch lost")
	}
	wire, _ := json.Marshal(a.report)
	if strings.Contains(string(wire), "SENTINEL") {
		t.Fatal("private start fields retained")
	}
}

func TestAccountingRetainedStorageBound(t *testing.T) {
	// Native IDs: two root plus two per reroute, at 256 bytes each. Models:
	// request/ack plus from/to per reroute, at 128 ASCII bytes each. Efforts
	// are closed names of at most seven bytes; all other strings are constants.
	// Cloning retained identifiers prevents a short substring pinning a large input.
	const maximumIdentifierBytes = 18*256 + 18*128 + 3*7
	if unsafe.Sizeof(AccountingReport{})+maximumIdentifierBytes >= 16<<10 {
		t.Fatal("retained report exceeds fixed memory bound")
	}
}
