package desk

import (
	"encoding/json"
	"errors"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"
)

type chatPlanReference struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision string `json:"revision"`
}
type chatPlan struct {
	ID         string              `json:"id"`
	Revision   int                 `json:"revision"`
	Objective  string              `json:"objective"`
	Steps      []string            `json:"steps"`
	Questions  []string            `json:"questions"`
	References []chatPlanReference `json:"references"`
	Status     string              `json:"status"`
	CreatedAt  string              `json:"createdAt"`
	AcceptedAt string              `json:"acceptedAt,omitempty"`
}

var planDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validPlanText(s string, n int) bool {
	return strings.TrimFunc(s, func(r rune) bool {
		return r >= 9 && r <= 13 || r == 32 || r == 0xA0 || r == 0x1680 || r >= 0x2000 && r <= 0x200A || r == 0x2028 || r == 0x2029 || r == 0x202F || r == 0x205F || r == 0x3000 || r == 0xFEFF
	}) != "" && runcontext.U16Length(s) <= n
}
func validateChatPlan(data json.RawMessage) error {
	var c struct {
		Interaction string     `json:"interaction"`
		Plans       []chatPlan `json:"plans"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return errors.New("Invalid saved plan.")
	}
	fields, fieldErr := assistantTestObject(data)
	if fieldErr != nil {
		return fieldErr
	}
	for key := range fields {
		if (strings.EqualFold(key, "interaction") && key != "interaction") || (strings.EqualFold(key, "plans") && key != "plans") {
			return errAssistantRunInvalid
		}
	}
	if _, err := assistantChatPhase(fields["interaction"]); err != nil {
		return err
	}
	var planRows []json.RawMessage
	if raw, ok := fields["plans"]; ok {
		if jsonscalar.ValidateEscapes(raw) != nil {
			return errAssistantRunInvalid
		}
		if err := readAssistantDialectCustody(raw, reflect.TypeOf([]chatPlan{})); err != nil {
			return err
		}
		planRows, fieldErr = assistantChatArray(raw)
		if fieldErr != nil {
			return fieldErr
		}
	}
	if c.Interaction != "" && c.Interaction != "plan" && c.Interaction != "work" {
		return errors.New("Invalid planning phase.")
	}
	if len(c.Plans) > 24 {
		return errors.New("Too many saved plan revisions.")
	}
	ids := map[string]bool{}
	for i, p := range c.Plans {
		if !validPlanText(p.ID, 80) || ids[p.ID] || p.Revision != i+1 || !validPlanText(p.Objective, 2000) || len(p.Steps) < 1 || len(p.Steps) > 20 || p.Questions == nil || len(p.Questions) > 10 || p.References == nil || len(p.References) > 8 {
			return errors.New("Invalid saved plan.")
		}
		ids[p.ID] = true
		if !contains([]string{"proposed", "accepted", "started", "superseded"}, p.Status) || i < len(c.Plans)-1 && p.Status != "superseded" {
			return errors.New("Invalid plan status.")
		}
		if _, e := time.Parse(time.RFC3339Nano, p.CreatedAt); e != nil {
			return errors.New("Invalid plan timestamp.")
		}
		planFields, err := assistantTestObject(planRows[i])
		if err != nil {
			return err
		}
		if p.Status == "started" || p.Status == "accepted" || planFields["acceptedAt"] != nil {
			if _, e := time.Parse(time.RFC3339Nano, p.AcceptedAt); e != nil {
				return errors.New("Invalid plan acceptance.")
			}
		}
		for _, s := range append(append([]string{}, p.Steps...), p.Questions...) {
			if !validPlanText(s, 1000) {
				return errors.New("Invalid plan step or question.")
			}
		}
		for _, r := range p.References {
			if !contains([]string{"file", "dialect"}, r.Kind) || !validPlanText(r.ID, 1024) || !planDigest.MatchString(r.Revision) {
				return errors.New("Invalid plan reference.")
			}
		}
	}
	return nil
}

// Chat-triggered writes must not consume an allowance while its owner is planning.
// Direct workspace buttons remain separate, explicitly authorized user commands.
func (s *Server) checkChatExecution(chat string) error {
	s.writes.Lock()
	defer s.writes.Unlock()
	lock, e := s.privateDataLock(true)
	if e != nil {
		return e
	}
	defer lock.Close()
	return s.checkChatExecutionLocked(chat)
}

// Caller holds the conversation/private-data lock through its admission commit.
func (s *Server) checkChatExecutionLocked(chat string) error {
	store, e := s.openChatData()
	if e != nil {
		return e
	}
	defer store.root.Close()
	name, e := resolveConversationName(store.root, s.conversationIdentity())
	if e != nil {
		return e
	}
	b, e := readPrivateData(store.root, name, maxConversationBytes)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = validateConversations(b); e != nil {
		return e
	}
	var doc struct {
		Chats []json.RawMessage `json:"chats"`
	}
	if e = json.Unmarshal(b, &doc); e != nil {
		return e
	}
	for _, raw := range doc.Chats {
		var c struct {
			ID          string     `json:"id"`
			Interaction string     `json:"interaction"`
			Plans       []chatPlan `json:"plans"`
		}
		if json.Unmarshal(raw, &c) != nil {
			return errAssistantRunInvalid
		}
		if c.ID != chat {
			continue
		}
		if len(c.Plans) > 0 {
			plan := c.Plans[len(c.Plans)-1]
			if plan.Status == "accepted" || plan.Status == "started" {
				if err := s.checkCurrentAssistantPlan(raw, plan); err != nil {
					return err
				}
			}
		}
		if c.Interaction == "plan" {
			return errors.New("This chat is in Plan mode. Review its plan and choose Start work before requesting an operation.")
		}
	}
	return nil
}
