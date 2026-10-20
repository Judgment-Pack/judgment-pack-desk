package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

// Only an authorized, server-held execution constructs this marker. No header
// or request field can suppress the legacy context preparation path.
type assistantAssembledPromptKey struct{}
type assistantModelDispatch struct {
	execution *assistantRunExecution
	operation assistantRunOperation
	admitted  atomic.Bool
	uncertain atomic.Bool
}

func (d *assistantModelDispatch) dispatch(ctx context.Context) error {
	if err := d.execution.recheckAssistantInput(ctx); err != nil {
		return err
	}
	if err := d.prepare(ctx); err != nil {
		return err
	}
	if err := d.execution.Dispatch(ctx, d.operation.ID); err != nil {
		return err
	}
	d.admitted.Store(true)
	return nil
}

func (d *assistantModelDispatch) prepare(ctx context.Context) error {
	if d.operation.ID != "" {
		return nil
	}
	op, err := d.execution.boundIntent(ctx, d.operation.Kind, d.operation.Name, func(string) ([]byte, error) { return d.operation.Intent, nil })
	if err == nil {
		d.operation = op
	}
	return err
}

// Authorization happens again after the real relay's capacity wait, immediately
// before its transport. Failed receipts prevent the outbound request entirely.
type assistantModelTransport struct {
	inner http.RoundTripper
	bound *assistantModelDispatch
}

func (t assistantModelTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := t.bound.dispatch(r.Context()); err != nil {
		return nil, err
	}
	response, err := t.inner.RoundTrip(r)
	if err != nil {
		// An admitted transport failure is uncertain, even if ReverseProxy
		// would otherwise render it as a retryable HTTP gateway refusal.
		t.bound.uncertain.Store(true)
		t.bound.execution.cancel()
	}
	if response != nil && response.Body != nil {
		response.Body = &assistantModelResponse{ReadCloser: response.Body, bound: t.bound}
	}
	return response, err
}

// ReverseProxy may render a copy failure as a clean handler return. Observe
// the real provider body before that can erase an interrupted transport.
type assistantModelResponse struct {
	io.ReadCloser
	bound *assistantModelDispatch
}

func (b *assistantModelResponse) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		b.bound.uncertain.Store(true)
		b.bound.execution.cancel()
	}
	return n, err
}

// model gives the published client the actual Desk handler. Session, project,
// connection and revision belong to the retained execution, never the adapter.
func (e *assistantRunExecution) assistantModel() (relay.Call, error) {
	if err := e.Authorize(e.ctx); err != nil {
		return nil, err
	}
	if e.source.Connection.Assistant.Engine != "vercel" || e.source.Connection.Assistant.Endpoint == nil {
		return nil, errAssistantRunUnavailable
	}
	family := e.source.Connection.Assistant.Endpoint.Kind
	client := &relay.Client{Family: family, Bearer: bearerOf(e.request)}
	client.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = e.request.Host
		r.URL.Host = e.request.Host
		r.Header.Set(aiConnectionHeader, e.record.Binding.ConnectionID)
		r.Header.Set(aiRevisionHeader, e.record.Binding.ConnectionRevision)
		// serveProject supplies the normal project-move fence around the real
		// handler, without exposing another HTTP route.
		e.service.server.serveProject(w, r)
	})
	models, err := e.assistantExecutionModels()
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, suffix string, request relay.Request) (*http.Response, error) {
		path, _, _ := strings.Cut(suffix, "?")
		if request.Method != "" && request.Method != http.MethodPost || !profileAPIModelAllowed(family, path, request.Body, []string{e.record.Binding.Model, models.Critic, models.Child}) {
			return nil, errAssistantRunScope
		}
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(e.ctx, cancel)
		if err := e.Authorize(ctx); err != nil {
			stop()
			cancel()
			return nil, err
		}
		intent, err := json.Marshal(struct {
			Suffix  string        `json:"suffix"`
			Request relay.Request `json:"request"`
		}{suffix, request})
		if err != nil {
			stop()
			cancel()
			return nil, err
		}
		dispatch := &assistantModelDispatch{execution: e, operation: assistantRunOperation{Kind: "model", Name: e.record.Binding.Model, Intent: intent}}
		ctx = context.WithValue(ctx, assistantAssembledPromptKey{}, dispatch)
		answer, err := client.Call()(ctx, suffix, request)
		if err != nil {
			if dispatch.admitted.Load() {
				e.cancel()
			}
			stop()
			cancel()
			return nil, err
		}
		answer.Body = &assistantModelReceipt{ReadCloser: answer.Body, execution: e, dispatch: dispatch, status: answer.StatusCode, headers: answer.Header.Clone(), ctx: ctx, stop: stop, cancel: cancel}
		return answer, nil
	}, nil
}

// A response completes only after the exact bytes have been retained. A lost,
// oversized, partially read or canceled response keeps the dispatch uncertain.
type assistantModelReceipt struct {
	io.ReadCloser
	execution *assistantRunExecution
	dispatch  *assistantModelDispatch
	status    int
	headers   http.Header
	ctx       context.Context
	stop      func() bool
	cancel    context.CancelFunc
	bytes     bytes.Buffer
	once      sync.Once
	err       error
	retained  atomic.Bool
}

func (b *assistantModelReceipt) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if b.dispatch.admitted.Load() && b.ctx.Err() != nil && !b.retained.Load() {
		b.dispatch.uncertain.Store(true)
		b.execution.cancel()
		return n, b.ctx.Err()
	}
	if b.dispatch.uncertain.Load() {
		return n, io.ErrUnexpectedEOF
	}
	if b.bytes.Len()+n > assistantRunRecordLimit {
		b.execution.cancel()
		b.cancel()
		return 0, errors.New("the assistant model response exceeds the retained run limit")
	}
	b.bytes.Write(p[:n])
	if err != nil && !errors.Is(err, io.EOF) {
		b.execution.cancel()
	}
	if errors.Is(err, io.EOF) {
		b.once.Do(func() {
			defer b.stop()
			defer b.cancel()
			result, encodeErr := json.Marshal(struct {
				Status  int         `json:"status"`
				Headers http.Header `json:"headers"`
				Body    []byte      `json:"body"`
			}{b.status, b.headers, b.bytes.Bytes()})
			if encodeErr != nil {
				b.err = encodeErr
				return
			}
			state := "completed"
			if !b.dispatch.admitted.Load() {
				// A refusal written before the real outbound boundary is a
				// not-started receipt, not a dispatched provider request.
				if b.err = b.dispatch.prepare(b.ctx); b.err != nil {
					return
				}
				state = "refused"
			}
			b.err = b.execution.Result(b.ctx, b.dispatch.operation.ID, state, result)
			if b.err == nil {
				b.retained.Store(true)
			}
		})
		if b.err != nil {
			return n, b.err
		}
	}
	return n, err
}

func (b *assistantModelReceipt) Close() error {
	// A protocol terminator may precede HTTP EOF: drain while the request is
	// live, but only a successfully persisted receipt makes a close known.
	var err error
	if !b.retained.Load() && b.ctx.Err() == nil {
		_, err = io.Copy(io.Discard, b)
	}
	if b.dispatch.admitted.Load() && !b.retained.Load() {
		b.dispatch.uncertain.Store(true)
		b.execution.cancel()
		if err == nil {
			err = b.ctx.Err()
		}
	}
	b.stop()
	b.cancel()
	return errors.Join(err, b.ReadCloser.Close())
}
