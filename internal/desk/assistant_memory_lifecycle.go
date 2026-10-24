package desk

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Optional private v1 metadata retains a canonical proposal without duplicating
// its text or storing acceptance receipts in model-visible content. Only the
// existing authenticated management path can make an entry active.
type memoryCanonical struct {
	ID        string                `json:"id"`
	Kind      string                `json:"kind"`
	Source    contract.MemorySource `json:"source"`
	Digest    string                `json:"digest"`
	ExpiresAt *string               `json:"expiresAt,omitempty"`
}

func memoryExpired(entry memoryEntry) bool {
	if entry.Canonical == nil || entry.Canonical.ExpiresAt == nil {
		return false
	}
	expires, err := time.Parse(time.RFC3339Nano, strings.ToUpper(*entry.Canonical.ExpiresAt))
	return err == nil && !expires.After(time.Now())
}

func assistantMemoryRecord(project string, entry memoryEntry) (contract.Memory, error) {
	var record contract.Memory
	if metadata := entry.Canonical; metadata != nil {
		// Canonical proposal digests cover exact proposed text, without the
		// legacy management topic prefix. The released reader verifies this.
		record = contract.Memory{ID: metadata.ID, Kind: metadata.Kind, Text: entry.Text,
			Source: metadata.Source, Scope: "project", Digest: metadata.Digest,
			CreatedAt: entry.CreatedAt, State: "accepted", ExpiresAt: metadata.ExpiresAt}
	} else {
		// Computed at read time: valid v1 stores never need a migration write.
		entry.Stale = false // A disposable management display is not provenance.
		raw, err := json.Marshal(struct {
			Project string      `json:"project"`
			Entry   memoryEntry `json:"entry"`
		}{project, entry})
		if err != nil {
			return record, err
		}
		identity := runcontext.ContextDigest(string(raw))
		text := entry.Key + ": " + entry.Text
		record = contract.Memory{ID: "desk-memory:" + identity, Kind: "reference", Text: text,
			Source: contract.MemorySource{Run: "desk-memory-import:" + identity, ProposedBy: "host"},
			Scope:  "project", Digest: "sha256:" + runcontext.ContextDigest(text),
			CreatedAt: entry.CreatedAt, State: "accepted"}
	}
	_, err := runcontext.RecallMemory([]contract.Memory{record}, memoryLimit, "")
	return record, err
}

type assistantMemorySnapshot struct {
	project  string
	document memoryDocument
	eligible []memoryEntry
}

func (h *projectPromptHost) readMemorySnapshot(ctx context.Context) (*assistantMemorySnapshot, error) {
	lock, err := h.server.privateDataLockContext(ctx, true)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	store, err := h.server.openChatData()
	if err != nil {
		return nil, err
	}
	defer store.root.Close()
	conversation, err := resolveConversationName(store.root, h.server.conversationIdentity())
	if err != nil {
		return nil, err
	}
	memory, err := readMemory(store.root, strings.Replace(conversation, "conversations-", "ai-memory-", 1))
	if err != nil {
		return nil, err
	}
	// Freeze dependency eligibility for this host assembly as well as stored
	// bytes. Agent owns the resulting immutable recall for the rest of the run.
	for i := range memory.Content.Entries {
		if h.server.validMemoryDependencies(memory.Content.Entries[i].Dependencies) != nil {
			memory.Content.Entries[i].State = "superseded"
		}
		memory.Content.Entries[i].Dependencies = append([]memoryDependency(nil), memory.Content.Entries[i].Dependencies...)
	}
	eligible := []memoryEntry{}
	if memory.Content.Settings.Use {
		for _, entry := range memory.Content.Entries {
			if entry.State == "active" && !memoryExpired(entry) {
				eligible = append(eligible, entry)
			}
		}
	}
	return &assistantMemorySnapshot{project: conversation, document: memory.Content, eligible: eligible}, ctx.Err()
}

// MemoryLifecycle never reconstructs a record from retained metadata. It only
// reports terminal changes to identities Agent already owns. Query relevance
// and recall byte/count selection have no bearing on this comparison.
func (h *projectPromptHost) MemoryLifecycle(ctx context.Context, prior []contract.ProvenanceMemoryRecall) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h.memoryMu.Lock()
	defer h.memoryMu.Unlock()
	snapshot := h.memorySnapshot
	if snapshot == nil {
		var err error
		snapshot, err = h.readMemorySnapshot(ctx)
		if err != nil {
			return nil, err
		}
	}
	current := map[string]contract.Memory{}
	for _, entry := range snapshot.document.Entries {
		if len(prior) == 0 || !snapshot.document.Settings.Use || entry.State != "active" {
			continue
		}
		record, err := assistantMemoryRecord(snapshot.project, entry)
		if err != nil {
			return nil, err
		}
		current[record.ID] = record
	}
	terminal := map[string]bool{}
	updates := []contract.MemoryLifecycleUpdate{}
	for _, retained := range prior {
		if retained.State != "accepted" {
			terminal[retained.ID] = true
			continue // Terminal states cannot switch on a later resume.
		}
		record, exists := current[retained.ID]
		state := ""
		if !exists || record.Digest != retained.Digest || record.Kind != retained.Kind || !snapshot.document.Settings.Use {
			state = "revoked"
		} else if record.ExpiresAt != nil {
			expires, _ := time.Parse(time.RFC3339Nano, strings.ToUpper(*record.ExpiresAt))
			if !expires.After(time.Now()) {
				state = "expired"
			}
		}
		if state != "" {
			terminal[retained.ID] = true
			updates = append(updates, contract.MemoryLifecycleUpdate{ID: retained.ID, Digest: retained.Digest, Kind: retained.Kind, State: state})
		}
	}
	if h.memorySnapshot == nil {
		// Terminal provenance belongs to this execution even if management makes
		// the same private identity eligible again. Filter before recall ranking
		// and budgeting, and freeze this selection with the first lifecycle read.
		eligible := []memoryEntry{}
		for _, entry := range snapshot.eligible {
			record, err := assistantMemoryRecord(snapshot.project, entry)
			if err != nil {
				return nil, err
			}
			if !terminal[record.ID] {
				eligible = append(eligible, entry)
			}
		}
		snapshot.eligible = eligible
	}
	// Delegated children can share this host concurrently. Freeze it once so
	// their callbacks cannot replace a parent's lifecycle/recall snapshot. Each
	// resumed execution constructs a fresh host before assembling its context.
	h.memorySnapshot = snapshot
	return updates, nil
}
