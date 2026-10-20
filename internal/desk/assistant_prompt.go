package desk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
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
func (s *Server) assistantPromptHost(ctx context.Context) (runcontext.Host, error) {
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

func (h *projectPromptHost) MemoryFor(ctx context.Context, query string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// A private-data transaction reads the same project binding and memory
	// document as management and legacy recall. No wire/message parsing belongs here.
	lock, err := h.server.privateDataLockContext(ctx, true)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	store, err := h.server.openChatData()
	if err != nil {
		return "", err
	}
	defer store.root.Close()
	conversation, err := resolveConversationName(store.root, h.server.conversationIdentity())
	if err != nil {
		return "", err
	}
	memory, err := readMemory(store.root, strings.Replace(conversation, "conversations-", "ai-memory-", 1))
	if err != nil {
		return "", err
	}
	entries := h.server.recallMemory(memory.Content, query)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	if len(data) > runcontext.Defaults.MemoryBytes {
		return "", errors.New("Recalled project memory exceeds its size limit.")
	}
	return string(data), nil
}
