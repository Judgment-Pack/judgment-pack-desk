package desk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// The browser reads the released Agent budget from the server, rather than
// keeping a second instruction limit alongside its older execution package.
type assistantPromptPolicy struct {
	ProjectInstructionsBytes int `json:"projectInstructionsBytes"`
}

func currentAssistantPromptPolicy() assistantPromptPolicy {
	return assistantPromptPolicy{ProjectInstructionsBytes: runcontext.Defaults.ProjectInstructionsBytes}
}

// assistantPromptHost snapshots the selected project's explicit configuration.
// Agent asks its parts once per run and owns all framing and layer ordering.
func (s *Server) assistantPromptHost(ctx context.Context) (*projectPromptHost, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	profile, err := s.readAssistantProfile()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	brief := ""
	if profile.Context != nil {
		brief = profile.Context.ProjectBrief
	}
	return &projectPromptHost{server: s, brief: brief, instructions: profile.ProjectInstructions}, nil
}

type projectPromptHost struct {
	server       *Server
	brief        string
	instructions string
}

var _ runcontext.Host = (*projectPromptHost)(nil)
var _ runcontext.MemoryHost = (*projectPromptHost)(nil)

func (h *projectPromptHost) Brief(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if h.brief == "" {
		return "", nil
	}
	if err := h.server.refuseSymlinkedPath(h.brief); err != nil {
		return "", errors.New("The configured AI project brief cannot be read safely.")
	}
	data, _, err := h.server.readThroughRootWithin(h.brief, runcontext.Defaults.BriefBytes)
	if err != nil || !validUTF8(data) {
		return "", errors.New("The configured AI project brief is missing, unreadable, or exceeds its size limit. Fix the file or its context setting.")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(data), nil
}

func (h *projectPromptHost) ProjectInstructions(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return h.instructions, nil
}

func (h *projectPromptHost) MemoryFor(ctx context.Context, query string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A private-data transaction reads the same project binding and memory
	// document as management and legacy recall. No wire/message parsing belongs here.
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
	entries := h.server.memoryCandidates(memory.Content, query)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return assistantMemoryRecords(conversation, entries)
}

// Manual memory remains in Desk's private schema. This projection identifies an
// exact stored version, without treating a historical chat/turn as an Agent run.
// The opaque host import identity binds the project and all retained entry fields.
func assistantMemoryRecords(project string, entries []memoryEntry) ([]contract.Memory, error) {
	records := []contract.Memory{}
	bytes := 0
	for _, entry := range entries {
		raw, err := json.Marshal(struct {
			Project string      `json:"project"`
			Entry   memoryEntry `json:"entry"`
		}{project, entry})
		if err != nil {
			return nil, err
		}
		identity := runcontext.ContextDigest(string(raw))
		text := entry.Key + ": " + entry.Text
		record := contract.Memory{
			ID: "desk-memory:" + identity, Kind: "reference", Text: text,
			Source: contract.MemorySource{Run: "desk-memory-import:" + identity, ProposedBy: "host"},
			Scope:  "project", Digest: "sha256:" + runcontext.ContextDigest(text),
			CreatedAt: entry.CreatedAt, State: "accepted",
		}
		// Agent validates the closed record and measures its canonical serialized
		// bytes. The private store bound permits measuring a single valid row before
		// applying the smaller recall bound, so an oversized row can be skipped.
		// Keep the same relevance ordering, skip-to-fit behavior and five rows.
		recalled, err := runcontext.RecallMemory([]contract.Memory{record}, memoryLimit, "")
		if err != nil {
			return nil, err
		}
		if bytes+recalled.Bytes > runcontext.Defaults.MemoryBytes {
			continue
		}
		records = append(records, record)
		bytes += recalled.Bytes
		if len(records) == 5 {
			break
		}
	}
	if _, err := runcontext.RecallMemory(records, runcontext.Defaults.MemoryBytes, ""); err != nil {
		return nil, err
	}
	return records, nil
}
