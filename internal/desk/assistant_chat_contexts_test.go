package desk

import (
	"fmt"
	"sync"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

func assistantContextTestBinding() assistantRunBinding {
	return assistantRunBinding{Principal: "principal", Session: "session", Policy: "policy", Project: "project", Owner: assistantRunOwner{Kind: "chat", ID: "chat-a", Revision: "revision"}, RequestID: "request", SourceDigests: []string{"source"}}
}
func assistantContextTestDocument(t *testing.T, c *assistantChatContexts, b assistantRunBinding, id string) assistantContextDocumentView {
	t.Helper()
	d, err := c.openDocument(b, id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func assistantContextTestRetain(t *testing.T, c *assistantChatContexts, b assistantRunBinding, s assistantRunContextSelector, purpose, owner string) *assistantChatContextHandle {
	t.Helper()
	h, err := c.retain(b, s, purpose, owner)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func TestAssistantChatContextsIsolationAndReuse(t *testing.T) {
	c := newAssistantChatContexts()
	defer c.close()
	b := assistantContextTestBinding()
	d := assistantContextTestDocument(t, c, b, "document-a")
	s := assistantRunContextSelector{Document: d.Document}
	first := assistantContextTestRetain(t, c, b, s, "chat", "chat-a")
	defer first.Release()
	changed := b
	changed.RequestID = "later"
	changed.Owner.Revision = "later"
	changed.ConnectionID = "another"
	later := assistantContextTestRetain(t, c, changed, s, "chat", "chat-a")
	defer later.Release()
	if first.Chat() != later.Chat() {
		t.Fatal("later request or connection replaced the document's chat")
	}
	other := assistantContextTestRetain(t, c, b, s, "chat", "chat-b")
	defer other.Release()
	if first.Chat() == other.Chat() {
		t.Fatal("different saved owners share context")
	}
	d2 := assistantContextTestDocument(t, c, b, "document-b")
	elsewhere := assistantContextTestRetain(t, c, b, assistantRunContextSelector{Document: d2.Document}, "chat", "chat-a")
	defer elsewhere.Release()
	if elsewhere.Chat() == first.Chat() {
		t.Fatal("different documents share context")
	}
	for _, field := range []string{"principal", "session", "policy", "project"} {
		foreign := b
		switch field {
		case "principal":
			foreign.Principal = "foreign"
		case "session":
			foreign.Session = "foreign"
		case "policy":
			foreign.Policy = "foreign"
		case "project":
			foreign.Project = "foreign"
		}
		if _, err := c.retain(foreign, s, "chat", "chat-a"); err != errAssistantRunScope {
			t.Errorf("foreign %s: %v", field, err)
		}
	}
	// Pane contexts are mount-owned, rather than capture-owner-owned.
	p, err := c.openPane(b, d.Document, "pane-a", "author-pack")
	if err != nil {
		t.Fatal(err)
	}
	paneSelector := assistantRunContextSelector{Document: p.Document, Pane: p.Pane}
	pane := assistantContextTestRetain(t, c, b, paneSelector, "author-pack", "capture-a")
	defer pane.Release()
	next := assistantContextTestRetain(t, c, b, paneSelector, "author-pack", "capture-b")
	defer next.Release()
	if pane.Chat() != next.Chat() || pane.Chat() == first.Chat() {
		t.Fatal("pane scope does not follow mount lifetime")
	}
	if _, err = c.retain(b, paneSelector, "chat", "chat-a"); err != errAssistantRunInvalid {
		t.Fatal("saved chat accepted pane selector")
	}
	if err = c.validatePane(b, paneSelector, "author-graph"); err != errAssistantRunScope {
		t.Fatal("pane accepted foreign purpose")
	}
}
func TestAssistantChatContextsRetirementKeepsOriginalOwner(t *testing.T) {
	c := newAssistantChatContexts()
	b := assistantContextTestBinding()
	d := assistantContextTestDocument(t, c, b, "document")
	selector := assistantRunContextSelector{Document: d.Document}
	h := assistantContextTestRetain(t, c, b, selector, "chat", "chat-a")
	chat := h.Chat()
	if err := c.bindRun(h, "run-a"); err != nil {
		t.Fatal(err)
	}
	if err := c.bindRun(h, "run-a"); err != nil {
		t.Fatal(err)
	} // Idempotency adds no second run reference.
	h.Release()
	h.Release()
	if err := c.retireDocument(b, d.Document); err != nil {
		t.Fatal(err)
	}
	if chat.IsDisposed() {
		t.Fatal("document close disposed retained owner")
	}
	if _, err := c.openDocument(b, "document"); err != errAssistantRunScope {
		t.Fatalf("retired registration resurrected: %v", err)
	}
	if _, err := c.retain(b, selector, "chat", "chat-a"); err != errAssistantRunScope {
		t.Fatal("retired selector accepted")
	}
	continuation, err := c.retainRun("run-a", b)
	if err != nil {
		t.Fatal(err)
	}
	if continuation.Chat() != chat {
		t.Fatal("continuation lost original context")
	}
	for _, mutate := range []func(*assistantRunBinding){func(b *assistantRunBinding) { b.Session = "foreign" }, func(b *assistantRunBinding) { b.Owner.ID = "chat-b" }, func(b *assistantRunBinding) { b.RequestID = "other" }, func(b *assistantRunBinding) { b.SourceDigests = []string{"changed"} }} {
		changed := b
		mutate(&changed)
		if _, err := c.retainRun("run-a", changed); err != errAssistantRunScope {
			t.Fatal("run rebound to changed binding")
		}
	}
	c.settleRun("run-a")
	c.settleRun("run-a")
	if chat.IsDisposed() {
		t.Fatal("settlement ignored active temporary reference")
	}
	continuation.Release()
	if !chat.IsDisposed() {
		t.Fatal("last reference failed to dispose retired context")
	}
	if len(c.entries) != 0 {
		t.Fatal("retired context still counted")
	}
}
func TestAssistantChatContextsPendingRetirementAndAuthorityEnd(t *testing.T) {
	for _, finish := range []string{"document", "pane", "authority", "shutdown"} {
		t.Run(finish, func(t *testing.T) {
			c := newAssistantChatContexts()
			b := assistantContextTestBinding()
			d := assistantContextTestDocument(t, c, b, "document")
			p, err := c.openPane(b, d.Document, "pane", "author-pack")
			if err != nil {
				t.Fatal(err)
			}
			selector := assistantRunContextSelector{Document: p.Document, Pane: p.Pane}
			h := assistantContextTestRetain(t, c, b, selector, "author-pack", "capture")
			switch finish {
			case "document":
				err = c.retireDocument(b, d.Document)
			case "pane":
				err = c.retirePane(b, selector)
			case "authority":
				c.retireAuthority(b)
			case "shutdown":
				c.close()
			}
			if err != nil {
				t.Fatal(err)
			}
			if h.Chat().IsDisposed() {
				t.Fatal("pending preparation disposed before release")
			}
			if err = c.bindRun(h, "new-run"); err != errAssistantRunScope {
				t.Fatal("retired pending scope admitted")
			}
			h.Release()
			if !h.Chat().IsDisposed() {
				t.Fatal("pending release leaked context")
			}
			if finish == "pane" {
				if _, err = c.openPane(b, d.Document, "pane", "author-pack"); err != errAssistantRunScope {
					t.Fatal("retired pane registration resurrected")
				}
			}
		})
	}
	// Authority end and shutdown retire future reuse; retained owner settlement
	// remains explicit, after the host has cancelled/drained its execution.
	c := newAssistantChatContexts()
	b := assistantContextTestBinding()
	d := assistantContextTestDocument(t, c, b, "document")
	h := assistantContextTestRetain(t, c, b, assistantRunContextSelector{Document: d.Document}, "chat", "chat-a")
	if err := c.bindRun(h, "run"); err != nil {
		t.Fatal(err)
	}
	h.Release()
	c.retireAuthority(b)
	c.close()
	if h.Chat().IsDisposed() {
		t.Fatal("shutdown disposed unsettled owner")
	}
	c.settleRun("run")
	if !h.Chat().IsDisposed() {
		t.Fatal("settled authority-end owner leaked")
	}
}
func TestAssistantChatContextsRegistrationAndCountBounds(t *testing.T) {
	c := newAssistantChatContexts()
	defer c.close()
	b := assistantContextTestBinding()
	var documents []assistantContextDocumentView
	for i := 0; i < assistantContextDocumentLimit; i++ {
		documents = append(documents, assistantContextTestDocument(t, c, b, fmt.Sprintf("document-%d", i)))
	}
	if _, err := c.openDocument(b, "overflow"); err != errAssistantRunUnavailable {
		t.Fatal("document cap did not refuse")
	}
	if d, err := c.openDocument(b, "document-0"); err != nil || d != documents[0] {
		t.Fatal("lost document registration response changed token")
	}
	for i := 0; i < assistantContextPaneLimit; i++ {
		if _, err := c.openPane(b, documents[0].Document, fmt.Sprintf("pane-%d", i), "author-pack"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.openPane(b, documents[0].Document, "overflow", "author-pack"); err != errAssistantRunUnavailable {
		t.Fatal("pane cap did not refuse")
	}
	// Pending reservations count atomically; concurrent callers cannot overshoot.
	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	for i := 0; i < assistantContextCountLimit+40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h, err := c.retain(b, assistantRunContextSelector{Document: documents[1].Document}, "chat", fmt.Sprintf("chat-%d", i))
			if err == nil {
				mu.Lock()
				admitted++
				mu.Unlock()
				h.Release()
			} else if err != errAssistantRunUnavailable {
				t.Errorf("unexpected refusal: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if admitted != assistantContextCountLimit || len(c.entries) != assistantContextCountLimit {
		t.Fatalf("admitted=%d entries=%d", admitted, len(c.entries))
	}
	if err := c.retireDocument(b, documents[1].Document); err != nil {
		t.Fatal(err)
	}
	if len(c.entries) != 0 {
		t.Fatal("retirement failed to reclaim unreferenced capacity")
	}
}
func TestAssistantChatContextsBoundedRegistrationTombstones(t *testing.T) {
	c := newAssistantChatContexts()
	b := assistantContextTestBinding()
	for i := 0; i < assistantContextRegistrationLimit; i++ {
		d := assistantContextTestDocument(t, c, b, fmt.Sprintf("document-%d", i))
		if err := c.retireDocument(b, d.Document); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.openDocument(b, "new"); err != errAssistantRunUnavailable {
		t.Fatal("registration tombstone cap did not refuse")
	}
	if _, err := c.openDocument(b, "document-0"); err != errAssistantRunScope {
		t.Fatal("lost-ack retry resurrected retired document")
	}
	c.retireAuthority(b)
	if len(c.registrations) != 0 {
		t.Fatal("authority end retained registration tombstones")
	}
}
func TestAssistantChatContextsOwnerDeletionAndExistingBudget(t *testing.T) {
	c := newAssistantChatContexts()
	defer c.close()
	b := assistantContextTestBinding()
	d := assistantContextTestDocument(t, c, b, "document")
	h := assistantContextTestRetain(t, c, b, assistantRunContextSelector{Document: d.Document}, "chat", "chat-a")
	turn, err := h.Chat().OpenTurn()
	if err != nil {
		t.Fatal(err)
	}
	policy := runcontext.Defaults
	policy.RetainedBytes = 256
	ledger := runcontext.NewContextLedger(&policy, turn)
	_, err = ledger.Retain(runcontext.Document{ID: "one", Kind: "tool", Label: "one", Value: runcontext.String(string(make([]byte, 1024)))}, false)
	if err == nil {
		t.Fatal("registry bypassed existing retained-context budget")
	}
	turn.Release()
	c.retireOwner("other", b.Project, "chat-a")
	if h.Chat().IsDisposed() {
		t.Fatal("foreign principal retired owner")
	}
	c.retireOwner(b.Principal, b.Project, "chat-a")
	if h.Chat().IsDisposed() {
		t.Fatal("owner deletion ignored pending reference")
	}
	h.Release()
	if !h.Chat().IsDisposed() {
		t.Fatal("owner deletion leaked after release")
	}
}
func TestAssistantChatContextsConcurrentRetentionAndRetirement(t *testing.T) {
	c := newAssistantChatContexts()
	b := assistantContextTestBinding()
	d := assistantContextTestDocument(t, c, b, "document")
	h := assistantContextTestRetain(t, c, b, assistantRunContextSelector{Document: d.Document}, "chat", "chat-a")
	if err := c.bindRun(h, "run"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			held, err := c.retainRun("run", b)
			if err != nil {
				t.Errorf("retained owner unavailable: %v", err)
				return
			}
			if held.Chat() != h.Chat() {
				t.Error("retention rebound owner")
			}
			held.Release()
			held.Release()
		}()
	}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Release()
			if err := c.retireDocument(b, d.Document); err != nil {
				t.Errorf("idempotent retirement: %v", err)
			}
		}()
	}
	wg.Wait()
	if h.Chat().IsDisposed() {
		t.Fatal("concurrent retirement disposed original run")
	}
	c.settleRun("run")
	if !h.Chat().IsDisposed() {
		t.Fatal("original owner settlement did not dispose")
	}
}
func TestAssistantChatContextsBindingCopyAndRunBound(t *testing.T) {
	c := newAssistantChatContexts()
	defer c.close()
	b := assistantContextTestBinding()
	flag := true
	b.MemoryProposals = &flag
	original := assistantContextCopyBinding(b)
	d := assistantContextTestDocument(t, c, b, "document")
	h := assistantContextTestRetain(t, c, b, assistantRunContextSelector{Document: d.Document}, "chat", "chat-a")
	defer h.Release()
	b.SourceDigests[0] = "mutated"
	flag = false
	if err := c.bindRun(h, "first"); err != nil {
		t.Fatal(err)
	}
	held, err := c.retainRun("first", original)
	if err != nil {
		t.Fatal("mutable caller binding changed retained identity")
	}
	held.Release()
	for i := 1; i < assistantRunCountLimit; i++ {
		if err := c.bindRun(h, fmt.Sprintf("run-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.bindRun(h, "overflow"); err != errAssistantRunUnavailable {
		t.Fatal("retained run cap did not refuse")
	}
	c.settleRun("first")
	for i := 1; i < assistantRunCountLimit; i++ {
		c.settleRun(fmt.Sprintf("run-%d", i))
	}
}
