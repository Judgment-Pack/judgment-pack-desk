package codexbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrRun    = errors.New("Codex could not complete this run")
	ErrLimit  = errors.New("the Codex run reached its limit")
	ErrSignIn = errors.New("connect a ChatGPT account before running Codex")
)

// Only these presentation fields and host-tool requests can leave the bridge.
type RunEvent struct {
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Text  string `json:"text"`
	Phase string `json:"phase,omitempty"`
}
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}
type ToolAnswer struct {
	Text    string `json:"text"`
	IsError bool   `json:"isError"`
}
type ToolHandler func(context.Context, ToolCall) (ToolAnswer, error)

type activeRun struct {
	client *client
	events chan frame
	done   chan struct{}
	cancel context.CancelFunc
}

func opaqueID() (string, error) {
	var token [24]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", ErrUnavailable
	}
	return hex.EncodeToString(token[:]), nil
}
func validID(id string) bool {
	return id != "" && len(id) <= 256 && !strings.ContainsAny(id, "\r\n\x00")
}

// Run admits one owner, re-verifies subscription auth, and uses a new ephemeral
// thread. There is no automatic replay, API-key fallback or native tool handler.
func (m *Manager) Run(ctx context.Context, owner Owner, request RunRequest, emit func(RunEvent) error, execute ToolHandler) error {
	if !owner.alive() || emit == nil || execute == nil {
		return ErrRun
	}
	params, err := threadParams(request, m.profile.work)
	if err != nil {
		return err
	}
	if !m.mu.TryLock() {
		return ErrBusy
	}
	if m.closed || m.disconnecting || m.active != nil || m.pending != nil {
		m.mu.Unlock()
		return ErrBusy
	}
	ctx, release := m.operation(ctx)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	stopSession := context.AfterFunc(owner.Session, cancel)
	stopPolicy := context.AfterFunc(owner.Policy, cancel)
	defer func() { stopSession(); stopPolicy(); cancel(); release() }()
	if !owner.alive() {
		cancel()
	}
	if err = m.ready(ctx); err != nil {
		m.mu.Unlock()
		return err
	}
	account, err := m.readAccount(ctx, true)
	if err != nil || !account.Authenticated {
		m.mu.Unlock()
		if err != nil {
			return err
		}
		return ErrSignIn
	}
	// Recheck discovery at admission: a stale UI selection cannot silently select
	// a replacement model or inherit an unsupported native effort default.
	models, err := m.models(ctx)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	found := false
	for _, model := range models {
		if model.ID != request.Model {
			continue
		}
		effort := request.Effort
		if effort == "" {
			effort = model.DefaultEffort
		}
		for _, allowed := range model.Efforts {
			if effort == allowed {
				found = true
				request.Effort = effort
			}
		}
	}
	if !found {
		m.mu.Unlock()
		return ErrRun
	}
	run := &activeRun{client: m.client, events: make(chan frame, 64), done: make(chan struct{}), cancel: cancel}
	m.active = run
	m.mu.Unlock()
	c := run.client
	stopOnCancel := context.AfterFunc(ctx, c.stop)
	defer stopOnCancel()
	var threadID, turnID string
	defer func() {
		// The process group is the cancellation fallback, including a blocked
		// stdin write or host callback. Reap before admitting another run.
		c.close()
		m.mu.Lock()
		if m.client == c {
			m.client = nil
		}
		if m.active == run {
			m.active = nil
		}
		m.mu.Unlock()
		close(run.done)
	}()
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model    string `json:"model"`
		Provider string `json:"modelProvider"`
		Approval string `json:"approvalPolicy"`
		Cwd      string `json:"cwd"`
	}
	if err = c.call(ctx, "thread/start", params, &started); err != nil {
		return err
	}
	if !validID(started.Thread.ID) || started.Model != request.Model || started.Provider != "openai" || started.Approval != "never" || started.Cwd != m.profile.work {
		return ErrRun
	}
	threadID = started.Thread.ID
	var turn struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err = c.call(ctx, "turn/start", turnParams(threadID, request), &turn); err != nil {
		return err
	}
	if !validID(turn.Turn.ID) {
		return ErrRun
	}
	turnID = turn.Turn.ID
	offered := map[string]bool{}
	for _, tool := range request.Tools {
		offered[tool.Name] = true
	}
	seenRPC, seenCall := map[string]bool{}, map[string]bool{}
	items := map[string]string{}
	texts := map[string]string{}
	completed := map[string]bool{}
	calls, frames, bytes, reasoning := 0, 0, 0, 0
	limit := 20
	if request.Phase == "critic" {
		limit = 4
	}
	itemID := func(native string) (string, error) {
		if !validID(native) {
			return "", ErrRun
		}
		if id, ok := items[native]; ok {
			return id, nil
		}
		if len(items) >= 256 {
			return "", ErrLimit
		}
		id, err := opaqueID()
		if err == nil {
			items[native] = id
		}
		return id, err
	}
	deliver := func(event RunEvent) error {
		bytes += len(event.Text)
		if bytes > 2<<20 {
			return ErrLimit
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return emit(event)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return ErrUnavailable
		case f := <-run.events:
			frames++
			if frames > 10000 {
				return ErrLimit
			}
			if len(f.ID) > 0 {
				if f.Method != "item/tool/call" || len(f.ID) > 512 || seenRPC[string(f.ID)] {
					return ErrRun
				}
				var call struct {
					Thread    string          `json:"threadId"`
					Turn      string          `json:"turnId"`
					ID        string          `json:"callId"`
					Name      string          `json:"tool"`
					Namespace *string         `json:"namespace"`
					Arguments json.RawMessage `json:"arguments"`
				}
				if json.Unmarshal(f.Params, &call) != nil || call.Thread != threadID || call.Turn != turnID ||
					!validID(call.ID) || seenCall[call.ID] || call.Namespace != nil || !offered[call.Name] || len(call.Arguments) > 256<<10 {
					return ErrRun
				}
				var args map[string]json.RawMessage
				if json.Unmarshal(call.Arguments, &args) != nil || args == nil {
					return ErrRun
				}
				calls++
				if calls > limit {
					return ErrLimit
				}
				seenRPC[string(f.ID)], seenCall[call.ID] = true, true
				id, err := opaqueID()
				if err != nil {
					return err
				}
				// The handler receives no native session/thread/RPC identifiers.
				type answer struct {
					value ToolAnswer
					err   error
				}
				answered := make(chan answer, 1)
				callCtx, stop := context.WithTimeout(ctx, 60*time.Second)
				go func() {
					value, err := execute(callCtx, ToolCall{ID: id, Name: call.Name, Arguments: call.Arguments})
					answered <- answer{value, err}
				}()
				var result answer
				select {
				case result = <-answered:
				case <-callCtx.Done():
					stop()
					return callCtx.Err()
				}
				stop()
				if result.err != nil {
					return ErrRun
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if len(result.value.Text) > 1<<20 {
					return ErrLimit
				}
				if err = c.send(map[string]any{"id": f.ID, "result": map[string]any{
					"success": !result.value.IsError, "contentItems": []any{map[string]string{"type": "inputText", "text": result.value.Text}},
				}}); err != nil {
					return err
				}
				continue
			}
			switch f.Method {
			case "item/agentMessage/delta":
				var d struct {
					Thread string `json:"threadId"`
					Turn   string `json:"turnId"`
					Item   string `json:"itemId"`
					Delta  string `json:"delta"`
				}
				if json.Unmarshal(f.Params, &d) != nil || d.Thread != threadID || d.Turn != turnID || completed[d.Item] {
					return ErrRun
				}
				id, err := itemID(d.Item)
				if err != nil {
					return err
				}
				texts[d.Item] += d.Delta
				if err = deliver(RunEvent{Type: "text", ID: id, Text: d.Delta}); err != nil {
					return err
				}
			case "item/started", "item/completed":
				var d struct {
					Thread string `json:"threadId"`
					Turn   string `json:"turnId"`
					Item   struct {
						ID    string `json:"id"`
						Type  string `json:"type"`
						Text  string `json:"text"`
						Phase string `json:"phase"`
					} `json:"item"`
				}
				if json.Unmarshal(f.Params, &d) != nil || d.Thread != threadID || d.Turn != turnID {
					return ErrRun
				}
				id, err := itemID(d.Item.ID)
				if err != nil {
					return err
				}
				if f.Method == "item/started" {
					if d.Item.Type == "reasoning" {
						reasoning++
						if reasoning > limit {
							return ErrLimit
						}
					}
					continue
				}
				if completed[d.Item.ID] {
					return ErrRun
				}
				completed[d.Item.ID] = true
				if d.Item.Type == "agentMessage" {
					if prior := texts[d.Item.ID]; prior != "" && prior != d.Item.Text {
						return ErrRun
					}
					delete(texts, d.Item.ID)
					phase := "commentary"
					if d.Item.Phase == "final_answer" || d.Item.Phase == "" {
						phase = "final"
					}
					if err = deliver(RunEvent{Type: "message", ID: id, Text: d.Item.Text, Phase: phase}); err != nil {
						return err
					}
				}
			case "turn/completed":
				var d struct {
					Thread string `json:"threadId"`
					Turn   struct {
						ID     string `json:"id"`
						Status string `json:"status"`
					} `json:"turn"`
				}
				if json.Unmarshal(f.Params, &d) != nil || d.Thread != threadID || d.Turn.ID != turnID || d.Turn.Status != "completed" || len(texts) > 0 {
					return ErrRun
				}
				return nil
			case "error":
				// Native errors may contain prompts, account data or provider bodies.
				return ErrRun
			default:
				// Lifecycle, token-accounting and fixed native-utility notifications
				// are counted but never forwarded as raw payloads.
			}
		}
	}
}
