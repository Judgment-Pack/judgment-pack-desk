package desk

import (
	"crypto/rand"
	"encoding/hex"
	"reflect"
	"sync"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// These bound multiplicity, including pending preparations and retained owners.
// Context contents keep the Agent package's existing byte/task/result budgets.
const (
	assistantContextDocumentLimit     = 32
	assistantContextPaneLimit         = 16
	assistantContextCountLimit        = 256
	assistantContextRegistrationLimit = 512
)

type assistantRunContextSelector struct {
	Document string `json:"document"`
	Pane     string `json:"pane,omitempty"`
}
type assistantContextDocumentView struct {
	Document string `json:"document"`
}
type assistantContextPaneView struct {
	Document string `json:"document"`
	Pane     string `json:"pane"`
}
type assistantContextAuthority struct{ principal, session, policy, project string }

func assistantContextAuthorityOf(b assistantRunBinding) assistantContextAuthority {
	return assistantContextAuthority{b.Principal, b.Session, b.Policy, b.Project}
}
func (a assistantContextAuthority) valid() bool {
	return a.principal != "" && a.session != "" && a.policy != "" && a.project != ""
}

type assistantContextRegistration struct {
	authority                       assistantContextAuthority
	document, registration, purpose string
}
type assistantContextDocument struct {
	authority assistantContextAuthority
	panes     map[string]*assistantContextPane
	chats     map[string]*assistantContextEntry
}
type assistantContextPane struct {
	purpose string
	entry   *assistantContextEntry
}
type assistantContextEntry struct {
	authority assistantContextAuthority
	chat      *runcontext.ChatContext
	refs      int
	retired   bool
	blocked   bool // an invocation with unknown settlement may never grant future reuse
}
type assistantContextRun struct {
	entry   *assistantContextEntry
	binding assistantRunBinding
}

// Private to one server service. The mutex covers only membership/refcounts;
// disposal and all source/Agent/provider operations happen outside it.
type assistantChatContexts struct {
	mu            sync.Mutex
	closed        bool
	documents     map[string]*assistantContextDocument
	registrations map[assistantContextRegistration]string // Retired tokens stay as bounded registration tombstones.
	entries       map[*assistantContextEntry]struct{}
	runs          map[string]assistantContextRun
	reserved      int // pending preparations count against the retained-run capacity
}

func newAssistantChatContexts() *assistantChatContexts {
	return &assistantChatContexts{documents: map[string]*assistantContextDocument{}, registrations: map[assistantContextRegistration]string{}, entries: map[*assistantContextEntry]struct{}{}, runs: map[string]assistantContextRun{}}
}
func assistantContextToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
func (c *assistantChatContexts) openDocument(binding assistantRunBinding, registrationID string) (assistantContextDocumentView, error) {
	authority := assistantContextAuthorityOf(binding)
	if !authority.valid() || !assistantRunText(registrationID, 128) {
		return assistantContextDocumentView{}, errAssistantRunInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := assistantContextRegistration{authority: authority, registration: registrationID}
	if c.closed {
		return assistantContextDocumentView{}, errAssistantRunUnavailable
	}
	if token, ok := c.registrations[key]; ok {
		if c.documents[token] == nil {
			return assistantContextDocumentView{}, errAssistantRunScope
		}
		return assistantContextDocumentView{token}, nil
	}
	if len(c.documents) >= assistantContextDocumentLimit || len(c.registrations) >= assistantContextRegistrationLimit {
		return assistantContextDocumentView{}, errAssistantRunUnavailable
	}
	token, err := assistantContextToken()
	if err != nil {
		return assistantContextDocumentView{}, errAssistantRunUnavailable
	}
	c.documents[token] = &assistantContextDocument{authority: authority, panes: map[string]*assistantContextPane{}, chats: map[string]*assistantContextEntry{}}
	c.registrations[key] = token
	return assistantContextDocumentView{token}, nil
}
func (c *assistantChatContexts) documentLocked(binding assistantRunBinding, document string) (*assistantContextDocument, error) {
	d := c.documents[document]
	if c.closed || d == nil || d.authority != assistantContextAuthorityOf(binding) {
		return nil, errAssistantRunScope
	}
	return d, nil
}
func (c *assistantChatContexts) openPane(binding assistantRunBinding, document, registrationID, purpose string) (assistantContextPaneView, error) {
	if !assistantRunText(registrationID, 128) || !assistantAuthoringPurpose(purpose) {
		return assistantContextPaneView{}, errAssistantRunInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	d, err := c.documentLocked(binding, document)
	if err != nil {
		return assistantContextPaneView{}, err
	}
	key := assistantContextRegistration{authority: d.authority, document: document, registration: registrationID, purpose: purpose}
	// One registration may not change purpose during a retry.
	for held := range c.registrations {
		if held.authority == key.authority && held.document == document && held.registration == registrationID && held.purpose != purpose {
			return assistantContextPaneView{}, errAssistantRunConflict
		}
	}
	if token, ok := c.registrations[key]; ok {
		if d.panes[token] == nil {
			return assistantContextPaneView{}, errAssistantRunScope
		}
		return assistantContextPaneView{document, token}, nil
	}
	if len(d.panes) >= assistantContextPaneLimit || len(c.registrations) >= assistantContextRegistrationLimit {
		return assistantContextPaneView{}, errAssistantRunUnavailable
	}
	token, err := assistantContextToken()
	if err != nil {
		return assistantContextPaneView{}, errAssistantRunUnavailable
	}
	d.panes[token] = &assistantContextPane{purpose: purpose}
	c.registrations[key] = token
	return assistantContextPaneView{document, token}, nil
}
func (c *assistantChatContexts) validatePane(binding assistantRunBinding, selector assistantRunContextSelector, purpose string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, err := c.documentLocked(binding, selector.Document)
	if err != nil {
		return err
	}
	p := d.panes[selector.Pane]
	if p == nil || p.purpose != purpose {
		return errAssistantRunScope
	}
	return nil
}

type assistantChatContextHandle struct {
	registry *assistantChatContexts
	entry    *assistantContextEntry
	binding  assistantRunBinding
	reserved bool // guarded by registry.mu
	released bool // guarded by registry.mu
}

func (h *assistantChatContextHandle) Chat() *runcontext.ChatContext {
	if h == nil {
		return nil
	}
	return h.entry.chat
}
func (c *assistantChatContexts) retain(binding assistantRunBinding, selector assistantRunContextSelector, purpose, ownerID string) (*assistantChatContextHandle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, err := c.documentLocked(binding, selector.Document)
	if err != nil {
		return nil, err
	}
	var entry *assistantContextEntry
	var pane *assistantContextPane
	if purpose == "chat" {
		if selector.Pane != "" || !researchChatID.MatchString(ownerID) {
			return nil, errAssistantRunInvalid
		}
		entry = d.chats[ownerID]
	} else if assistantAuthoringPurpose(purpose) {
		pane = d.panes[selector.Pane]
		if pane == nil || pane.purpose != purpose {
			return nil, errAssistantRunScope
		}
		entry = pane.entry
	} else {
		return nil, errAssistantRunInvalid
	}
	if entry == nil {
		if len(c.entries) >= assistantContextCountLimit {
			return nil, errAssistantRunUnavailable
		}
		token, err := assistantContextToken()
		if err != nil {
			return nil, errAssistantRunUnavailable
		}
		entry = &assistantContextEntry{authority: d.authority, chat: runcontext.NewChatContext(token)}
		c.entries[entry] = struct{}{}
		if pane == nil {
			d.chats[ownerID] = entry
		} else {
			pane.entry = entry
		}
	}
	if entry.blocked {
		return nil, errAssistantRunUnavailable
	}
	entry.refs++
	return &assistantChatContextHandle{registry: c, entry: entry, binding: assistantContextCopyBinding(binding)}, nil
}
func assistantContextCopyBinding(b assistantRunBinding) assistantRunBinding {
	if b.SourceDigests != nil {
		b.SourceDigests = append([]string{}, b.SourceDigests...)
	}
	if b.MemoryProposals != nil {
		v := *b.MemoryProposals
		b.MemoryProposals = &v
	}
	return b
}
func (h *assistantChatContextHandle) Release() {
	if h == nil {
		return
	}
	c := h.registry
	c.mu.Lock()
	if h.released {
		c.mu.Unlock()
		return
	}
	h.released = true
	if h.reserved {
		h.reserved = false
		c.reserved--
	}
	h.entry.refs--
	disposal := c.collectLocked(h.entry)
	c.mu.Unlock()
	assistantContextDispose(disposal)
}
func (c *assistantChatContexts) bindRun(h *assistantChatContextHandle, runID string) error {
	if h == nil || h.registry != c || !assistantRunText(runID, 128) {
		return errAssistantRunInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if held, ok := c.runs[runID]; ok {
		if held.entry != h.entry || !reflect.DeepEqual(held.binding, h.binding) {
			return errAssistantRunConflict
		}
		return nil
	}
	if c.closed || h.released || h.entry.retired || h.entry.blocked || h.reserved {
		return errAssistantRunScope
	}
	if len(c.runs)+c.reserved >= assistantRunCountLimit {
		return errAssistantRunUnavailable
	}
	h.entry.refs++
	c.runs[runID] = assistantContextRun{h.entry, assistantContextCopyBinding(h.binding)}
	return nil
}
func (c *assistantChatContexts) retainRun(runID string, binding assistantRunBinding) (*assistantChatContextHandle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	held, ok := c.runs[runID]
	if c.closed || !ok || held.entry.blocked || !reflect.DeepEqual(held.binding, binding) {
		return nil, errAssistantRunScope
	}
	held.entry.refs++
	return &assistantChatContextHandle{registry: c, entry: held.entry, binding: assistantContextCopyBinding(held.binding)}, nil
}
func (c *assistantChatContexts) settleRun(runID string) {
	c.mu.Lock()
	held, ok := c.runs[runID]
	var disposal *runcontext.ChatContext
	if ok {
		delete(c.runs, runID)
		held.entry.refs--
		disposal = c.collectLocked(held.entry)
	}
	c.mu.Unlock()
	assistantContextDispose(disposal)
}
func (c *assistantChatContexts) collectLocked(entry *assistantContextEntry) *runcontext.ChatContext {
	if entry.retired && entry.refs == 0 {
		delete(c.entries, entry)
		return entry.chat
	}
	return nil
}
func assistantContextDispose(chat *runcontext.ChatContext) {
	if chat != nil {
		chat.Dispose()
	}
}
func assistantContextsDispose(chats []*runcontext.ChatContext) {
	for _, chat := range chats {
		chat.Dispose()
	}
}
func (c *assistantChatContexts) retireEntryLocked(entry *assistantContextEntry, chats *[]*runcontext.ChatContext) {
	if entry == nil || entry.retired {
		return
	}
	entry.retired = true
	if chat := c.collectLocked(entry); chat != nil {
		*chats = append(*chats, chat)
	}
}
func (c *assistantChatContexts) retirePaneLocked(d *assistantContextDocument, document, pane string, chats *[]*runcontext.ChatContext) {
	p := d.panes[pane]
	if p == nil {
		return
	}
	delete(d.panes, pane)
	c.retireEntryLocked(p.entry, chats)

}
func (c *assistantChatContexts) retireDocumentLocked(document string, chats *[]*runcontext.ChatContext) {
	d := c.documents[document]
	if d == nil {
		return
	}
	delete(c.documents, document)
	for _, entry := range d.chats {
		c.retireEntryLocked(entry, chats)
	}
	for pane := range d.panes {
		c.retirePaneLocked(d, document, pane, chats)
	}

}
func (c *assistantChatContexts) retireDocument(binding assistantRunBinding, document string) error {
	c.mu.Lock()
	_, err := c.documentLocked(binding, document)
	if err != nil {
		for key, token := range c.registrations {
			if key.document == "" && token == document && key.authority == assistantContextAuthorityOf(binding) {
				c.mu.Unlock()
				return nil
			}
		}
		c.mu.Unlock()
		return err
	}
	var chats []*runcontext.ChatContext
	c.retireDocumentLocked(document, &chats)
	c.mu.Unlock()
	assistantContextsDispose(chats)
	return nil
}
func (c *assistantChatContexts) retirePane(binding assistantRunBinding, selector assistantRunContextSelector) error {
	c.mu.Lock()
	d, err := c.documentLocked(binding, selector.Document)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	if d.panes[selector.Pane] == nil {
		for key, token := range c.registrations {
			if key.document == selector.Document && token == selector.Pane && key.authority == d.authority {
				c.mu.Unlock()
				return nil
			}
		}
		c.mu.Unlock()
		return errAssistantRunScope
	}
	var chats []*runcontext.ChatContext
	c.retirePaneLocked(d, selector.Document, selector.Pane, &chats)
	c.mu.Unlock()
	assistantContextsDispose(chats)
	return nil
}
func (c *assistantChatContexts) retireOwner(principal, project, ownerID string) {
	c.mu.Lock()
	var chats []*runcontext.ChatContext
	for _, d := range c.documents {
		if d.authority.principal == principal && d.authority.project == project {
			entry := d.chats[ownerID]
			delete(d.chats, ownerID)
			c.retireEntryLocked(entry, &chats)
		}
	}
	c.mu.Unlock()
	assistantContextsDispose(chats)
}

// The host cancels authority-owned work first. References held by in-flight
// preparation/continuation remain until their original owners release them.
func (c *assistantChatContexts) retireAuthority(binding assistantRunBinding) {
	c.mu.Lock()
	authority := assistantContextAuthorityOf(binding)
	var chats []*runcontext.ChatContext
	for token, d := range c.documents {
		if d.authority == authority {
			c.retireDocumentLocked(token, &chats)
		}
	}
	for key := range c.registrations {
		if key.authority == authority {
			delete(c.registrations, key)
		}
	}
	c.mu.Unlock()
	assistantContextsDispose(chats)
}
func (c *assistantChatContexts) close() {
	c.mu.Lock()
	c.closed = true
	var chats []*runcontext.ChatContext
	for token := range c.documents {
		c.retireDocumentLocked(token, &chats)
	}
	c.registrations = map[assistantContextRegistration]string{}
	c.mu.Unlock()
	assistantContextsDispose(chats)
}

// reserveRun counts a pending preparation before it can produce any Chat state.
// The pending handle remains the lifetime owner even after document retirement.
func (c *assistantChatContexts) reserveRun(h *assistantChatContextHandle) error {
	if h == nil || h.registry != c {
		return errAssistantRunInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || h.released || h.entry.retired || h.entry.blocked {
		return errAssistantRunScope
	}
	if h.reserved {
		return nil
	}
	if len(c.runs)+c.reserved >= assistantRunCountLimit {
		return errAssistantRunUnavailable
	}
	h.reserved = true
	c.reserved++
	return nil
}

// promoteRun consumes only a preexisting reservation. The host has admitted the
// original source and derived ExecutionKey/Unit; every other binding fact stays
// exact. Document/pane retirement cannot revoke that pending lifetime. Authority
// and source are checked by durable Admit/launch, not recreated from this handle.
func (c *assistantChatContexts) promoteRun(h *assistantChatContextHandle, runID string, binding assistantRunBinding) error {
	if h == nil || h.registry != c || !assistantRunText(runID, 128) || binding.ExecutionKey == "" || binding.Unit == "" {
		return errAssistantRunInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	expected := assistantContextCopyBinding(h.binding)
	expected.ExecutionKey, expected.Unit = binding.ExecutionKey, binding.Unit
	if c.closed || h.released || h.entry.blocked || !h.reserved || !reflect.DeepEqual(expected, binding) {
		return errAssistantRunScope
	}
	if _, exists := c.runs[runID]; exists {
		return errAssistantRunConflict
	}
	h.reserved = false
	c.reserved--
	h.binding = assistantContextCopyBinding(binding)
	h.entry.refs++
	c.runs[runID] = assistantContextRun{h.entry, assistantContextCopyBinding(binding)}
	return nil
}

// An unknown invocation is not evidence that its Chat is no longer in use.
// Keep its bounded run reference and poison the existing mapping: deleting it
// would silently permit the next request to allocate a replacement context.
func (c *assistantChatContexts) quarantineRun(runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if held, ok := c.runs[runID]; ok {
		held.entry.blocked = true
	}
}
