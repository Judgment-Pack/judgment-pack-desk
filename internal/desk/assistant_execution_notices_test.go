package desk

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
)

// This isolates the notice ingress contract using the real Gate refusal path.
// It is not a substitute for the full production adapter qualification below.
func TestAssistantRetainedNoticeWaitsForOuterUnwind(t *testing.T) {
	_, _, e := assistantP1ExecutionFixture(t, true)
	x, err := e.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
	assistantRunMust(t, err)
	defer x.Close()
	entered, proceed := make(chan struct{}), make(chan struct{})
	release := gate.GuardNotices(x.Run.Gate, func(event contract.RunEvent) (contract.RunEvent, bool) {
		close(entered)
		<-proceed
		return event, true
	})
	defer release()
	done := make(chan error, 1)
	outer := x.invocation.adapter(adapter.Func(func(context.Context, *adapter.Run) error {
		defer release() // same ordering as an adapter's notice/boundary defers
		x.Run.Gate.AdmitHost("unoffered_fixture_write")
		return nil
	}))
	go func() { done <- outer.Run(t.Context(), x.Run) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Gate did not enter notice guard")
	}
	x.notices.mu.Lock()
	active := x.notices.active
	x.notices.mu.Unlock()
	if active != 0 {
		t.Fatal("notice unexpectedly reached Emit before guard returned")
	}
	retireErr := x.notices.retire(x.invocation)
	installErr := x.notices.install(&assistantExecutionInvocation{owner: e, generation: x.invocation.generation + 1})
	close(proceed)
	assistantRunMust(t, <-done)
	if retireErr == nil || installErr == nil {
		t.Fatal("zero active publications admitted a successor before outer unwind")
	}
	assistantRunMust(t, x.notices.retire(x.invocation))
	if !x.notices.parked(x.invocation.generation) {
		t.Fatal("joined invocation did not retire")
	}
	if err := x.notices.install(&assistantExecutionInvocation{owner: e, generation: x.invocation.generation + 2}); err == nil {
		t.Fatal("notice ingress skipped a generation")
	}
	successor := &assistantExecutionInvocation{owner: e, generation: x.invocation.generation + 1}
	assistantRunMust(t, x.notices.install(successor))
	before := assistantExecutionRecord(t, e)
	// Saved callbacks refuse before reaching the Gate/transport even with a
	// fresh, uncanceled context. A saved emit cannot publish to any successor.
	if _, err := x.Run.Model(t.Context(), "chat/completions", assistantExecutionRequest()); err == nil {
		t.Fatal("retired model callback accepted work")
	}
	if err := outer.Run(t.Context(), x.Run); err == nil {
		t.Fatal("retired adapter was reusable")
	}
	x.Run.Emit(contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "retired"}})
	after := assistantExecutionRecord(t, e)
	if after.Canonical.Head.Revision != before.Canonical.Head.Revision {
		t.Fatal("retired callback changed canonical head")
	}
	if successor.failure != nil || successor.active != 0 {
		t.Fatal("retired callback reached the successor sink")
	}
	if _, err := x.invocation.proof(); err == nil {
		t.Fatal("nil adapter return without any provider evidence qualified parking")
	}
}

// Exercise the actual HTTP handler and response receipt. EOF alone must not
// qualify an invocation; the adapter consumer must acknowledge Body.Close too.
func TestAssistantRetainedHTTPRequiresConsumerClose(t *testing.T) {
	u := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"exact":true}`)
	})
	_, _, e := assistantExecutionFixture(t, u.server.URL, "work")
	call, err := e.assistantModel()
	assistantRunMust(t, err)
	response, err := call(t.Context(), "chat/completions", assistantExecutionRequest())
	assistantRunMust(t, err)
	defer response.Body.Close()
	_, err = io.ReadAll(response.Body)
	assistantRunMust(t, err)
	body := response.Body.(*assistantModelReceipt)
	// Lifecycle flags isolate resource settlement; full adapter End/unwind is
	// exercised by the separate production-construction test.
	scope := &assistantExecutionInvocation{started: true, returned: true, closed: true, ended: true, models: []*assistantModelDispatch{body.dispatch}}
	if _, err := scope.proof(); err == nil {
		t.Fatal("EOF was mistaken for consumer release")
	}
	assistantRunMust(t, response.Body.Close())
	if !body.dispatch.handlerReturned.Load() || !body.dispatch.physicalEOF.Load() || !body.dispatch.physicalClosed.Load() || !body.dispatch.receiptAcknowledged.Load() {
		t.Fatal("actual handler/provider/receipt release was not observed")
	}
	proof, err := scope.proof()
	assistantRunMust(t, err)
	if !proof.safe(nil) {
		t.Fatal("complete real HTTP release was not qualified")
	}
	scope.ended = false
	if _, err := scope.proof(); err == nil {
		t.Fatal("provider release without final boundary drain qualified parking")
	}
}

type assistantP3bCloseBody struct {
	io.Reader
	err   error
	calls int
}

func (b *assistantP3bCloseBody) Close() error { b.calls++; return b.err }

func TestAssistantRetainedHTTPPhysicalCloseFailure(t *testing.T) {
	want := errors.New("provider release failed")
	body := &assistantP3bCloseBody{Reader: strings.NewReader("complete"), err: want}
	dispatch := &assistantModelDispatch{}
	response := &assistantModelResponse{ReadCloser: body, bound: dispatch}
	_, err := io.ReadAll(response)
	assistantRunMust(t, err)
	if dispatch.physicalClosed.Load() {
		t.Fatal("EOF claimed Close")
	}
	for i := 0; i != 2; i++ {
		if !errors.Is(response.Close(), want) {
			t.Fatal("physical close error was lost")
		}
	}
	if body.calls != 1 || !dispatch.physicalClosed.Load() || !dispatch.physicalCloseFailed.Load() {
		t.Fatal("physical release was repeated or its failure forgotten")
	}
	dispatch.admitted.Store(true)
	dispatch.handlerReturned.Store(true)
	dispatch.consumerClosed.Store(true)
	dispatch.receiptAcknowledged.Store(true)
	scope := &assistantExecutionInvocation{started: true, returned: true, closed: true, ended: true, models: []*assistantModelDispatch{dispatch}}
	if _, err := scope.proof(); err == nil {
		t.Fatal("failed physical release qualified parking")
	}
}
