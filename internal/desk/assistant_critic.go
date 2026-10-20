package desk

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/turn"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// This is input custody, never a tool grant, saved check or approval. Once
// installed it cannot be refreshed to a different reviewed target in this run.
type assistantCriticSnapshot struct {
	path, packDigest, suiteDigest string
	cases                         []json.RawMessage
	fingerprint                   string
}

const assistantCriticCheckpointMember = "deskCriticInputV1"

var assistantCriticMembers = []string{"id", "facts", "evidenceAvailability", "supportedExtensions", "expectedDisposition", "expectedErrorClass", "expectedErrorPhase", "expectedHandoffTarget", "origin", "focus", "specSection", "cites"}

// Match criticCases.ts: validate each row with the published reader, skip an
// uncomparable legacy row, then keep saved rows first, one id, at most 200.
// A skipped row is never repaired or given a different expected outcome.
func assistantCriticCases(declared, suite *runcontext.Value) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	seen := map[string]bool{}
	keep := func(row *runcontext.Value, source string) {
		value := runcontext.ObjectOf("source", runcontext.String(source))
		for _, key := range assistantCriticMembers {
			if row.Has(key) {
				value.Set(key, row.Get(key).Clone())
			}
		}
		parsed, err := turn.ReadCriticCases(runcontext.Array(value).JSON())
		if err != nil || len(parsed) != 1 || seen[parsed[0].ID] || len(out) >= 200 {
			return
		}
		seen[parsed[0].ID] = true
		out = append(out, value.JSON())
	}
	if suite != nil {
		if !suite.IsObject() || !suite.Get("cases").IsArray() || len(suite.Get("cases").Items()) > 256 || suite.Has("deleted") && !suite.Get("deleted").IsArray() {
			return nil, errAssistantRunInvalid
		}
		deleted := map[string]bool{}
		for _, id := range suite.Get("deleted").Items() {
			if !id.IsString() {
				return nil, errAssistantRunInvalid
			}
			deleted[id.Str()] = true
		}
		ids := map[string]bool{}
		for _, item := range suite.Get("cases").Items() {
			id, row := item.Get("id"), item.Get("row")
			if !item.IsObject() || !id.IsString() || ids[id.Str()] || !row.IsObject() || !row.Get("id").IsString() || row.Get("id").Str() != id.Str() || !item.Get("rationale").IsString() || !item.Get("origin").IsString() {
				return nil, errAssistantRunInvalid
			}
			ids[id.Str()] = true
			errorClass := row.Get("expectedErrorClass")
			if deleted[id.Str()] || !row.Has("expectedDisposition") && (!errorClass.IsString() || errorClass.Str() == "") {
				continue
			}
			row = row.Clone()
			row.Set("id", id.Clone())
			if rationale := item.Get("rationale"); rationale.Str() != "" {
				row.Set("focus", rationale.Clone())
			}
			if origin := item.Get("origin"); origin.Str() == "ai" || !row.Has("origin") || row.Get("origin").Kind() == runcontext.KindNull {
				row.Set("origin", origin.Clone())
			}
			keep(row, "test")
		}
	}
	for _, item := range declared.Items() {
		// AuthoringCase's declaration shape deliberately excludes historical
		// expectationSource and any execution/check fields.
		row := runcontext.Object()
		for _, key := range []string{"id", "facts", "evidenceAvailability", "expectedDisposition", "expectedHandoffTarget"} {
			if item.Has(key) {
				row.Set(key, item.Get(key).Clone())
			}
		}
		if item.Has("rationale") {
			row.Set("focus", item.Get("rationale").Clone())
		}
		keep(row, "declared")
	}
	return out, nil
}

func assistantCriticEnabled(chat *assistantChatInput, phase string) bool {
	return phase != "plan" && turn.CriticRuns(chat.Review, runcontext.Defaults.AdversarialReview)
}

func (e *assistantRunExecution) readAssistantCriticSnapshot(ctx context.Context) (*assistantCriticSnapshot, error) {
	if err := e.Authorize(ctx); err != nil {
		return nil, err
	}
	chat, err := assistantChatValue(e.source.Original)
	if err != nil {
		return nil, err
	}
	held := &assistantCriticSnapshot{}
	var suite *runcontext.Value
	if chat.Has("pack") {
		link := chat.Get("pack")
		if !link.IsObject() || !link.Get("id").IsString() || !assistantRunText(link.Get("id").Str(), 256) || !link.Get("path").IsString() || !link.Get("digest").IsString() {
			return nil, errAssistantRunInvalid
		}
		path, err := wireRelativePath(link.Get("path").Str())
		if err != nil || path != link.Get("path").Str() {
			return nil, errAssistantRunScope
		}
		s := e.service.server
		// Take the same short private-data lock as admission; no model or
		// Runtime work occurs under it, or under the input mutex.
		lock, err := s.privateDataLockContext(ctx, true)
		if err != nil {
			return nil, err
		}
		defer lock.Close()
		current, tests, err := s.approvalChat(e.source.Start.OwnerID)
		if err != nil || current.Pack == nil || current.Pack.ID != link.Get("id").Str() || current.Pack.Path != path {
			return nil, errAssistantRunScope
		}
		if err := s.refuseSymlinkedPath(path); err != nil {
			return nil, err
		}
		pack, _, err := s.readThroughRoot(path)
		if err != nil {
			return nil, err
		}
		// Only identity is consumed here. Keep all other target members opaque:
		// a lossless saved-chat parser would unnecessarily reject an otherwise
		// valid target containing high-precision numbers that never enter context.
		var packFields map[string]json.RawMessage
		var packID string
		if !validUTF8(pack) || !uniqueJSON(pack) || json.Unmarshal(pack, &packFields) != nil || json.Unmarshal(packFields["id"], &packID) != nil || packID != current.Pack.ID {
			return nil, errAssistantRunScope
		}
		store, err := assistantChatValue(tests)
		if err != nil || !store.IsObject() || store.Get("version").Text() != "1" || !store.Get("suites").IsObject() {
			return nil, errAssistantRunInvalid
		}
		suite = store.Get("suites").Get(current.Pack.ID)
		held.path, held.packDigest = path, digestOf(pack)
		// Absence and an empty suite remain distinct custody snapshots.
		if suite == nil {
			held.suiteDigest = "absent"
		} else {
			held.suiteDigest = digestOf(suite.JSON())
		}
	}
	held.cases, err = assistantCriticCases(chat.Get("checkpoint").Get("state").Get("cases"), suite)
	if err == nil {
		cases, _ := json.Marshal(held.cases)
		binding, _ := json.Marshal([]string{e.record.ID, e.source.Binding.ExecutionKey, e.source.Binding.RequestDigest, e.source.Binding.SourceDigests[0], held.path, held.packDigest, held.suiteDigest, digestOf(cases)})
		held.fingerprint = digestOf(binding)
	}
	return held, err
}

// Only the private host writes this bounded consistency record. It is not
// restored from saved chat, exposed as a tool, or interpreted as a permission.
// The open published checkpoint preserves it through the normal reader/store.
func assistantCriticCheckpoint(cp contract.Checkpoint, held *assistantCriticSnapshot) contract.Checkpoint {
	if held == nil {
		return cp
	}
	members := make(map[string]json.RawMessage, len(cp.AdditionalMembers)+1)
	for key, value := range cp.AdditionalMembers {
		members[key] = append(json.RawMessage(nil), value...)
	}
	members[assistantCriticCheckpointMember], _ = json.Marshal(held.fingerprint)
	cp.AdditionalMembers = members
	return cp
}

func (e *assistantRunExecution) bindAssistantCriticCheckpoint(ctx context.Context, held *assistantCriticSnapshot) error {
	// The comparison and first capture share one existing private transaction;
	// concurrent constructors cannot replace each other's held target.
	err := e.writer.change(ctx, false, func(run *assistantRunRecord) (bool, error) {
		if run.Checkpoint == nil || assistantRunTerminal(run.State) {
			return false, errAssistantRunScope
		}
		if raw, present := run.Checkpoint.AdditionalMembers[assistantCriticCheckpointMember]; present {
			var stored string
			if json.Unmarshal(raw, &stored) != nil || stored != held.fingerprint {
				return false, errAssistantRunScope
			}
			return false, nil
		}
		// Missing evidence after any work or explicit continuation is a
		// refusal, never an opportunity to capture a new reviewed target.
		if len(run.Operations) != 0 || len(run.Actions) != 0 || len(run.Checkpoint.Prompts) != 0 || run.Checkpoint.Used != 0 {
			return false, errAssistantRunScope
		}
		cp, err := readAssistantRunCheckpoint(assistantCriticCheckpoint(*run.Checkpoint, held), run.Binding)
		if err != nil {
			return false, err
		}
		run.Checkpoint = cp
		return true, nil
	})
	e.changed(err)
	return err
}

func (e *assistantRunExecution) holdAssistantCritic(ctx context.Context) (*assistantCriticSnapshot, error) {
	if e.input == nil {
		return nil, errAssistantRunUnavailable
	}
	e.input.mu.Lock()
	held := e.input.critic
	e.input.mu.Unlock()
	if held != nil {
		return held, nil
	}
	current, err := e.readAssistantCriticSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if err := e.bindAssistantCriticCheckpoint(ctx, current); err != nil {
		return nil, err
	}
	e.input.mu.Lock()
	defer e.input.mu.Unlock()
	if e.input.critic == nil {
		e.input.critic = current
	} else if !assistantCriticSame(e.input.critic, current) {
		return nil, errAssistantRunScope
	}
	return e.input.critic, nil
}

func assistantCriticSame(a, b *assistantCriticSnapshot) bool {
	return a.path == b.path && a.packDigest == b.packDigest && a.suiteDigest == b.suiteDigest
}

func (e *assistantRunExecution) recheckAssistantCritic(ctx context.Context) error {
	if e.input == nil {
		return errAssistantRunUnavailable
	}
	e.input.mu.Lock()
	held := e.input.critic
	e.input.mu.Unlock()
	if held == nil {
		return nil
	}
	current, err := e.readAssistantCriticSnapshot(ctx)
	if err == nil && !assistantCriticSame(held, current) {
		err = errAssistantRunScope
	}
	if err != nil {
		e.cancel()
	}
	return err
}

// The testing prompt is served guidance with no request-bearing arguments.
// Missing discovery support stays empty so Agent emits its canonical no-prompt
// critique on a proposal; a malformed or refused get is an explicit failure.
func (r *assistantRuntime) criticPrompt(ctx context.Context) (string, error) {
	text, err := r.Prompt(ctx, assistantExecutionPrompt{Name: "test_pack"})
	if errors.Is(err, errAssistantRunUnavailable) {
		return "", nil
	}
	return text, err
}
