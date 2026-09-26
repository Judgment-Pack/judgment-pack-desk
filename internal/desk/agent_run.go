package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
	"github.com/coder/websocket"
)

type providerRunner interface {
	Run(context.Context, codexbridge.Owner, codexbridge.RunRequest, func(codexbridge.RunEvent) error, codexbridge.ToolHandler) error
}

// One socket owns exactly one run. IDs are Desk-generated, scoped to this
// socket, and single use. There is no raw RPC forwarding or reconnect/replay.
type agentInput struct {
	Type    string                  `json:"type"`
	Request *codexbridge.RunRequest `json:"request,omitempty"`
	RunID   string                  `json:"runId,omitempty"`
	CallID  string                  `json:"callId,omitempty"`
	Answer  *codexbridge.ToolAnswer `json:"answer,omitempty"`
}

func decodeAgentInput(data []byte) (agentInput, error) {
	var input agentInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return input, errors.New("invalid agent message")
	}
	return input, nil
}

func (s *Server) handleAgentRun(w http.ResponseWriter, r *http.Request) {
	id, problem := offeredSessionID(r)
	if problem != "" || r.URL.RawQuery != "" {
		providerReply(w, 400, map[string]string{"error": "invalid-request"})
		return
	}
	held, live := s.sessions.lookup(id)
	if !live || !s.signInSessionAllowed(held) {
		providerReply(w, 401, map[string]string{"error": "browser-session-required"})
		return
	}
	if _, ok := s.signInBrowser(w, r); !ok {
		return
	}
	runner, ok := s.codex.(providerRunner)
	if !ok {
		providerFailure(w, codexbridge.ErrUnavailable)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, Subprotocols: []string{wsProtocol}})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(2 << 20)
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Minute)
	defer cancel()
	stopSession := context.AfterFunc(held.ctx, cancel)
	defer stopSession()
	s.signIn.mu.Lock()
	epoch := s.signIn.epoch
	s.signIn.mu.Unlock()
	stopPolicy := context.AfterFunc(epoch, cancel)
	defer stopPolicy()
	if held.ctx.Err() != nil || epoch.Err() != nil {
		return
	}
	// The shared server registry cancels this socket on shutdown.
	connection := &conn{ws: ws, silent: true, out: make(chan []byte, 1), done: make(chan struct{}), cancel: cancel}
	if !s.register(connection) {
		return
	}
	defer s.unregister(connection)
	stopSocket := context.AfterFunc(ctx, func() { _ = ws.CloseNow() })
	defer stopSocket()
	owner := codexbridge.Owner{ID: s.sessions.handle(id), Session: held.ctx, Policy: epoch}
	firstCtx, firstCancel := context.WithTimeout(ctx, 10*time.Second)
	kind, data, err := ws.Read(firstCtx)
	firstCancel()
	if err != nil || kind != websocket.MessageText {
		return
	}
	start, err := decodeAgentInput(data)
	if err != nil || start.Type != "start" || start.Request == nil || start.RunID != "" || start.CallID != "" || start.Answer != nil {
		return
	}
	runID, err := NewToken()
	if err != nil {
		return
	}
	var writeMu sync.Mutex
	send := func(value any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		data, err := json.Marshal(value)
		if err != nil || len(data) > 2<<20 {
			return codexbridge.ErrLimit
		}
		writeCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		return ws.Write(writeCtx, websocket.MessageText, data)
	}
	var mu sync.Mutex
	var pendingID string
	var answer chan codexbridge.ToolAnswer
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer cancel()
		for {
			kind, data, err := ws.Read(ctx)
			if err != nil || kind != websocket.MessageText {
				return
			}
			input, err := decodeAgentInput(data)
			if err != nil || input.RunID != runID || input.Request != nil {
				return
			}
			if input.Type == "cancel" && input.CallID == "" && input.Answer == nil {
				return
			}
			if input.Type != "tool-result" || input.Answer == nil || len(input.Answer.Text) > 1<<20 {
				return
			}
			mu.Lock()
			if pendingID == "" || input.CallID != pendingID {
				mu.Unlock()
				return
			}
			waiter := answer
			pendingID = ""
			answer = nil
			mu.Unlock()
			waiter <- *input.Answer
		}
	}()
	defer func() { cancel(); <-readerDone }()
	if send(map[string]string{"type": "started", "runId": runID}) != nil {
		return
	}
	err = runner.Run(ctx, owner, *start.Request, func(event codexbridge.RunEvent) error {
		return send(map[string]any{"type": "event", "runId": runID, "event": event})
	}, func(callCtx context.Context, call codexbridge.ToolCall) (codexbridge.ToolAnswer, error) {
		waiter := make(chan codexbridge.ToolAnswer, 1)
		mu.Lock()
		if pendingID != "" {
			mu.Unlock()
			return codexbridge.ToolAnswer{}, codexbridge.ErrBusy
		}
		pendingID, answer = call.ID, waiter
		mu.Unlock()
		if err := send(map[string]any{"type": "tool-call", "runId": runID, "call": call}); err != nil {
			return codexbridge.ToolAnswer{}, err
		}
		select {
		case got := <-waiter:
			return got, nil
		case <-callCtx.Done():
			return codexbridge.ToolAnswer{}, callCtx.Err()
		case <-ctx.Done():
			return codexbridge.ToolAnswer{}, ctx.Err()
		}
	})
	if ctx.Err() != nil {
		return
	}
	code := ""
	if err != nil {
		code = "run-failed"
		switch {
		case errors.Is(err, codexbridge.ErrBusy):
			code = "busy"
		case errors.Is(err, codexbridge.ErrSignIn):
			code = "sign-in-required"
		case errors.Is(err, codexbridge.ErrLimit), errors.Is(err, context.DeadlineExceeded):
			code = "limit"
		case errors.Is(err, codexbridge.ErrUnavailable):
			code = "unavailable"
		}
	}
	_ = send(map[string]string{"type": "end", "runId": runID, "error": code})
}
