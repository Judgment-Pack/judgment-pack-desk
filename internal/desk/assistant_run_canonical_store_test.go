package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

type assistantCanonicalFixture struct {
	store   assistantRunStore
	binding assistantRunBinding
	record  assistantRunRecord
	lease   *assistantRunLease
	writer  *assistantRunWriter
	port    *assistantCanonicalRunStore
}

func assistantCanonicalStoreFixture(t *testing.T, id string) assistantCanonicalFixture {
	t.Helper()
	store, binding, _, record, lease, writer := assistantRunAdmitted(t)
	port, err := writer.canonicalStore(t.Context(), id)
	assistantRunMust(t, err)
	return assistantCanonicalFixture{store, binding, record, lease, writer, port}
}
func (f assistantCanonicalFixture) saved(t *testing.T) assistantRunRecord {
	t.Helper()
	run, err := f.store.Read(t.Context(), f.binding, f.record.ID)
	assistantRunMust(t, err)
	return run
}
func canonicalOpen(id string) runcontext.RunCommit {
	return runcontext.RunCommit{RunID: id, CommitID: "open", Checkpoint: []byte(`{ "payload":9007199254740993,"decimal":1e0,"zero":-0 }`), Events: [][]byte{[]byte(`{"version":1,"scope":{"execution":"fixture","role":"author"},"event":{"type":"fixture","unknown":null}}`)}}
}
func TestAssistantCanonicalStorePublishedConformance(t *testing.T) {
	assistantRunMust(t, runcontext.CheckRunStoreConformance(t.Context(), func() runcontext.RunStore {
		return assistantCanonicalStoreFixture(t, "run-alpha").port
	}))
}
func TestAssistantCanonicalStorePublishedFaultConformance(t *testing.T) {
	defer func() { testHookPrivateRead = nil }()
	assistantRunMust(t, runcontext.CheckRunStoreFaultConformance(t.Context(), func(phase string) runcontext.RunStore {
		testHookPrivateRead = nil
		f := assistantCanonicalStoreFixture(t, "fault-run")
		reads, fired := 0, false
		testHookPrivateRead = func(name string) error {
			if name == f.store.name && !fired {
				reads++
				if phase == "before" && reads == 1 || phase == "after" && reads == 2 {
					fired = true
					return errors.New("physical private-store fixture fault")
				}
			}
			return nil
		}
		return f.port
	}))
}
func TestAssistantCanonicalStoreObservedHeadDoesNotInitializeOrMutateCallbacks(t *testing.T) {
	f := assistantCanonicalStoreFixture(t, "canonical-one")
	before := f.saved(t)
	beforeBytes, _ := json.Marshal(before)
	head, err := f.port.Load(t.Context(), "canonical-one")
	assistantRunMust(t, err)
	page, err := f.port.ReadEvents(t.Context(), "canonical-one", "", 1)
	assistantRunMust(t, err)
	if head != nil || len(page.Items) != 0 || page.NextCursor != "" || page.HasMore {
		t.Fatal("read initialized a head")
	}
	after := f.saved(t)
	afterBytes, _ := json.Marshal(after)
	if !bytes.Equal(beforeBytes, afterBytes) || after.Canonical != nil {
		t.Fatal("read changed callbacks, revision or journal")
	}
	input := canonicalOpen("canonical-one")
	receipt, err := f.port.Commit(t.Context(), input)
	assistantRunMust(t, err)
	after = f.saved(t)
	if !sameAssistantRunCheckpoint(before.Checkpoint, after.Checkpoint) || len(after.Events) != len(before.Events) || len(after.Operations) != len(before.Operations) || after.State != before.State || receipt.Revision == "" {
		t.Fatal("inactive canonical persistence changed callback execution")
	}
	if after.Revision != before.Revision+1 {
		t.Fatal("outer revision did not advance exactly once")
	}
}
func TestAssistantCanonicalStoreExactDetachedBytesAndIndependentRevision(t *testing.T) {
	f := assistantCanonicalStoreFixture(t, "canonical-one")
	input := canonicalOpen("canonical-one")
	checkpoint, event := bytes.Clone(input.Checkpoint), bytes.Clone(input.Events[0])
	first, err := f.port.Commit(t.Context(), input)
	assistantRunMust(t, err)
	input.Checkpoint[0] = 'x'
	input.Events[0][0] = 'x'
	head, err := f.port.Load(t.Context(), "canonical-one")
	assistantRunMust(t, err)
	page, err := f.port.ReadEvents(t.Context(), "canonical-one", "", 100)
	assistantRunMust(t, err)
	if !bytes.Equal(head.Checkpoint, checkpoint) || !bytes.Equal(page.Items[0].Bytes, event) {
		t.Fatal("input bytes aliased or normalized")
	}
	head.Checkpoint[0] = 'x'
	page.Items[0].Bytes[0] = 'x'
	head, err = f.port.Load(t.Context(), "canonical-one")
	assistantRunMust(t, err)
	if !bytes.Equal(head.Checkpoint, checkpoint) {
		t.Fatal("returned bytes alias the head")
	}
	assistantRunMust(t, f.writer.Intent(t.Context(), assistantRunOp("interleaved")))
	assistantRunMust(t, f.writer.Dispatch(t.Context(), "interleaved"))
	assistantRunMust(t, f.writer.Result(t.Context(), "interleaved", "completed", []byte(`{"exact":false}`)))
	after, err := f.port.Load(t.Context(), "canonical-one")
	assistantRunMust(t, err)
	if after.Revision != first.Revision {
		t.Fatal("physical host receipts changed canonical CAS")
	}
	second, err := f.port.Commit(t.Context(), runcontext.RunCommit{RunID: "canonical-one", ExpectedRevision: first.Revision, CommitID: "event-only", Events: [][]byte{[]byte(`{"type":"next"}`)}})
	assistantRunMust(t, err)
	assistantRunMust(t, f.writer.Event(t.Context(), "callback-event", contract.RunEvent{Message: &contract.RunEventMessage{Type: "message", Text: "Unchanged callback event"}}))
	head, err = f.port.Load(t.Context(), "canonical-one")
	assistantRunMust(t, err)
	if head.Revision != second.Revision || !bytes.Equal(head.Checkpoint, checkpoint) || first.Revision == second.Revision || f.writer.Err() != nil {
		t.Fatal("interleaving lost canonical head or poisoned host writer")
	}
}
func TestAssistantCanonicalStoreRefusalsDoNotPoisonAndDuplicateIsExact(t *testing.T) {
	f := assistantCanonicalStoreFixture(t, "canonical-one")
	input := canonicalOpen("canonical-one")
	first, err := f.port.Commit(t.Context(), input)
	assistantRunMust(t, err)
	changed := canonicalOpen("canonical-one")
	changed.Checkpoint = []byte(`{"payload":9007199254740993,"decimal":1e0,"zero":-0}`)
	var mismatch *runcontext.StorageCommitMismatch
	if _, err = f.port.Commit(t.Context(), changed); !errors.As(err, &mismatch) {
		t.Fatalf("lexical change reused commit: %v", err)
	}
	changed = canonicalOpen("canonical-one")
	changed.ExpectedRevision = first.Revision
	if _, err = f.port.Commit(t.Context(), changed); !errors.As(err, &mismatch) {
		t.Fatalf("different expected CAS reused commit: %v", err)
	}
	var conflict *runcontext.StorageConflict
	changed = canonicalOpen("canonical-one")
	changed.CommitID = "other-create"
	if _, err = f.port.Commit(t.Context(), changed); !errors.As(err, &conflict) {
		t.Fatalf("create overwrote head: %v", err)
	}
	again, err := f.port.Commit(t.Context(), input)
	assistantRunMust(t, err)
	if again != first || f.writer.Err() != nil {
		t.Fatal("refusal poisoned data port or changed receipt")
	}
	next, err := f.port.Commit(t.Context(), runcontext.RunCommit{RunID: "canonical-one", CommitID: "next", ExpectedRevision: first.Revision, Checkpoint: []byte(`{"next":true}`)})
	assistantRunMust(t, err)
	if next.Revision == first.Revision || next.Cursor != first.Cursor {
		t.Fatal("checkpoint-only receipt failed")
	}
	nilHead, err := f.port.Commit(t.Context(), runcontext.RunCommit{RunID: "canonical-one", CommitID: "nil-checkpoint", ExpectedRevision: next.Revision, Events: [][]byte{[]byte(" ")}})
	assistantRunMust(t, err)
	changed = runcontext.RunCommit{RunID: "canonical-one", CommitID: "nil-checkpoint", ExpectedRevision: next.Revision, Checkpoint: []byte(`{"next":true}`), Events: [][]byte{[]byte(" ")}}
	if _, err = f.port.Commit(t.Context(), changed); !errors.As(err, &mismatch) {
		t.Fatal("nil checkpoint presence lost")
	}
	head, err := f.port.Load(t.Context(), "canonical-one")
	assistantRunMust(t, err)
	if head.Revision != nilHead.Revision {
		t.Fatal("changed duplicate mutated head")
	}
}
func TestAssistantCanonicalStoreEventOnlyCreateAndCursors(t *testing.T) {
	f := assistantCanonicalStoreFixture(t, "canonical-one")
	first, err := f.port.Commit(t.Context(), runcontext.RunCommit{RunID: "canonical-one", CommitID: "event-open", Events: [][]byte{[]byte(`{"n":1}`), []byte(`{"n":2}`)}})
	assistantRunMust(t, err)
	head, err := f.port.Load(t.Context(), "canonical-one")
	assistantRunMust(t, err)
	if head.Checkpoint != nil || head.Cursor != first.Cursor {
		t.Fatal("event-only create invented checkpoint")
	}
	page, err := f.port.ReadEvents(t.Context(), "canonical-one", "", 1)
	assistantRunMust(t, err)
	if len(page.Items) != 1 || !page.HasMore {
		t.Fatal("bounded page missing continuation")
	}
	next, err := f.port.ReadEvents(t.Context(), "canonical-one", page.NextCursor, 1)
	assistantRunMust(t, err)
	if next.HasMore || len(next.Items) != 1 || next.NextCursor != first.Cursor {
		t.Fatal("cursor order failed")
	}
	for _, id := range []string{"other-run", "canonical-one"} {
		cursor := first.Cursor
		if id == "canonical-one" {
			cursor = "foreign-cursor"
		}
		var invalid *runcontext.StorageCursorInvalid
		if _, err = f.port.ReadEvents(t.Context(), id, cursor, 1); !errors.As(err, &invalid) {
			t.Fatalf("foreign cursor accepted: %v", err)
		}
	}
	other := assistantCanonicalStoreFixture(t, "canonical-one")
	var invalid *runcontext.StorageCursorInvalid
	if _, err = other.port.ReadEvents(t.Context(), "canonical-one", first.Cursor, 1); !errors.As(err, &invalid) {
		t.Fatal("cursor crossed private store")
	}
}
func TestAssistantCanonicalStoreConcurrentCAS(t *testing.T) {
	f := assistantCanonicalStoreFixture(t, "canonical-one")
	first, err := f.port.Commit(t.Context(), canonicalOpen("canonical-one"))
	assistantRunMust(t, err)
	var group sync.WaitGroup
	errorsOut := make(chan error, 2)
	for _, id := range []string{"left", "right"} {
		group.Add(1)
		go func() {
			defer group.Done()
			_, e := f.port.Commit(t.Context(), runcontext.RunCommit{RunID: "canonical-one", ExpectedRevision: first.Revision, CommitID: id, Checkpoint: []byte(id), Events: [][]byte{[]byte(id)}})
			errorsOut <- e
		}()
	}
	group.Wait()
	close(errorsOut)
	success, conflicts := 0, 0
	for err := range errorsOut {
		var conflict *runcontext.StorageConflict
		if err == nil {
			success++
		} else if errors.As(err, &conflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 || f.writer.Err() != nil {
		t.Fatal("CAS race poisoned port or had multiple winners")
	}
	head, err := f.port.Load(t.Context(), "canonical-one")
	assistantRunMust(t, err)
	page, err := f.port.ReadEvents(t.Context(), "canonical-one", "", 100)
	assistantRunMust(t, err)
	if len(page.Items) != 2 || !bytes.Equal(head.Checkpoint, page.Items[1].Bytes) {
		t.Fatal("head and winning event split")
	}
}
func TestAssistantCanonicalStoreAdmittedScopeAndLease(t *testing.T) {
	f := assistantCanonicalStoreFixture(t, "canonical-one")
	if _, err := f.port.Commit(t.Context(), canonicalOpen("not-admitted")); !errors.Is(err, errAssistantRunScope) {
		t.Fatal("unadmitted canonical ID accepted")
	}
	first, err := f.port.Commit(t.Context(), canonicalOpen("canonical-one"))
	assistantRunMust(t, err)
	wrong := f.binding
	wrong.Session = digestOf([]byte("other-session"))
	foreign := &assistantCanonicalRunStore{writer: &assistantRunWriter{lease: f.lease, binding: wrong, id: f.record.ID}, id: "canonical-one"}
	if _, err = foreign.Load(t.Context(), "canonical-one"); !errors.Is(err, errAssistantRunScope) {
		t.Fatal("session mismatch read accepted")
	}
	for _, field := range []string{"project", "principal", "policy", "owner"} {
		t.Run(field, func(t *testing.T) {
			binding := f.binding
			switch field {
			case "project":
				binding.Project = digestOf([]byte(field))
			case "principal":
				binding.Principal = digestOf([]byte("other-principal"))
				if binding.Principal == f.binding.Principal {
					t.Fatal("principal fixture mutation did not change binding")
				}
			case "policy":
				binding.Policy = digestOf([]byte("other-policy"))
				if binding.Policy == f.binding.Policy {
					t.Fatal("policy fixture mutation did not change binding")
				}
			case "owner":
				binding.Owner.ID = "other-chat"
			}
			port := &assistantCanonicalRunStore{writer: &assistantRunWriter{lease: f.lease, binding: binding, id: f.record.ID}, id: "canonical-one"}
			if _, err := port.Load(t.Context(), "canonical-one"); err == nil {
				t.Fatal("immutable binding mismatch read accepted")
			}
		})
	}
	assistantRunMust(t, f.lease.Close())
	head, err := f.port.Load(t.Context(), "canonical-one")
	assistantRunMust(t, err)
	if head.Revision != first.Revision {
		t.Fatal("lease loss hid observed receipt")
	}
	if _, err = f.port.Commit(t.Context(), runcontext.RunCommit{RunID: "canonical-one", ExpectedRevision: first.Revision, CommitID: "after-loss", Checkpoint: []byte(`{}`)}); !errors.Is(err, errAssistantRunScope) {
		t.Fatal("lease loss permitted commit")
	}
}
func TestAssistantCanonicalStoreBoundedInputsAndCapacity(t *testing.T) {
	f := assistantCanonicalStoreFixture(t, "canonical-one")
	oversized := bytes.Repeat([]byte("x"), runcontext.StorageRecordBytes+1)
	cases := []runcontext.RunCommit{
		{RunID: "canonical-one", CommitID: "empty"},
		{RunID: "canonical-one", CommitID: "empty-checkpoint", Checkpoint: []byte{}},
		{RunID: "canonical-one", CommitID: "invalid-utf8", Checkpoint: []byte{0xff}},
		{RunID: "canonical-one", CommitID: "oversized", Checkpoint: oversized},
		{RunID: "canonical-one", CommitID: "control\u0085", Checkpoint: []byte(`{}`)},
		{RunID: "canonical-one", CommitID: "many-events", Events: make([][]byte, runcontext.StorageEventLimit+1)},
		{RunID: "canonical-one", CommitID: "large-transaction", Checkpoint: oversized[:runcontext.StorageRecordBytes], Events: [][]byte{oversized[:runcontext.StorageRecordBytes], []byte("x")}},
	}
	for _, input := range cases {
		var invalid *runcontext.StorageInputError
		if _, err := f.port.Commit(t.Context(), input); !errors.As(err, &invalid) {
			t.Fatalf("invalid input accepted: %s %v", input.CommitID, err)
		}
	}
	for _, limit := range []int{0, 101} {
		if _, err := f.port.ReadEvents(t.Context(), "canonical-one", "", limit); err == nil {
			t.Fatal("invalid page limit accepted")
		}
	}
	if head, err := f.port.Load(t.Context(), "canonical-one"); err != nil || head != nil {
		t.Fatal("refused input created a head")
	}
	input := canonicalOpen("canonical-one")
	input.Events = make([][]byte, 256)
	for i := range input.Events {
		input.Events[i] = []byte("e")
	}
	receipt, err := f.port.Commit(t.Context(), input)
	assistantRunMust(t, err)
	for _, id := range []string{"page-two", "page-three", "page-four"} {
		receipt, err = f.port.Commit(t.Context(), runcontext.RunCommit{RunID: "canonical-one", ExpectedRevision: receipt.Revision, CommitID: id, Events: input.Events})
		assistantRunMust(t, err)
	}
	before := f.saved(t)
	_, err = f.port.Commit(t.Context(), runcontext.RunCommit{RunID: "canonical-one", ExpectedRevision: receipt.Revision, CommitID: "overflow-events", Events: [][]byte{[]byte("e")}})
	if err == nil || f.saved(t).Revision != before.Revision {
		t.Fatal("event retention silently overflowed")
	}
}
func TestAssistantCanonicalStoreAggregateFileBoundAndCancellation(t *testing.T) {
	f := assistantCanonicalStoreFixture(t, "canonical-one")
	// Three individually valid 4 MiB records exceed the actual 16 MiB private
	// JSON file after base64 custody encoding. No partial head/events may publish.
	chunk := bytes.Repeat([]byte("x"), runcontext.StorageRecordBytes)
	receipt, err := f.port.Commit(t.Context(), runcontext.RunCommit{RunID: "canonical-one", CommitID: "first-large", Checkpoint: chunk, Events: [][]byte{chunk}})
	assistantRunMust(t, err)
	before := f.saved(t)
	_, err = f.port.Commit(t.Context(), runcontext.RunCommit{RunID: "canonical-one", ExpectedRevision: receipt.Revision, CommitID: "aggregate-overflow", Events: [][]byte{chunk}})
	if err == nil {
		t.Fatal("aggregate file limit bypassed")
	}
	after := f.saved(t)
	if after.Revision != before.Revision || len(after.Canonical.Events) != 1 || after.Canonical.Head.Revision != receipt.Revision {
		t.Fatal("aggregate refusal partially published")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = f.port.Commit(ctx, runcontext.RunCommit{RunID: "canonical-one", ExpectedRevision: receipt.Revision, CommitID: "cancelled", Checkpoint: []byte(`{}`)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled commit accepted: %v", err)
	}
	if f.writer.Err() != nil {
		t.Fatal("data refusal poisoned callback writer")
	}
}
func TestAssistantCanonicalStoreClosedStorageAndReopen(t *testing.T) {
	f := assistantCanonicalStoreFixture(t, "canonical-one")
	first, err := f.port.Commit(t.Context(), canonicalOpen("canonical-one"))
	assistantRunMust(t, err)
	private, err := f.store.server.openChatData()
	assistantRunMust(t, err)
	defer private.root.Close()
	raw, err := readPrivateData(private.root, f.store.name, assistantRunRecordLimit)
	assistantRunMust(t, err)
	malformed := []string{
		strings.Replace(string(raw), `"canonical":{`, `"canonical":{"unknown":true,`, 1),
		strings.Replace(string(raw), `"canonical":{`, `"canonical":{"Version":1,`, 1),
		strings.Replace(string(raw), `"version":1,"head":`, `"version":1,"version":1,"head":`, 1),
		strings.Replace(string(raw), `"fingerprint":`, `"Fingerprint":`, 1),
	}
	for i, value := range malformed {
		if value == string(raw) {
			t.Fatalf("mutation %d did not match handwritten member", i)
		}
		if _, err := decodeAssistantRunStorage(f.store.name, []byte(value)); err == nil {
			t.Fatalf("structural mutation %d accepted", i)
		}
	}
	// A fresh adapter shares only the durable filesystem state, not process-local
	// revision/cursor/commit maps. Constructing it performs no recovery.
	writer, err := f.lease.Writer(t.Context(), f.binding, f.record.ID)
	assistantRunMust(t, err)
	reopened, err := writer.canonicalStore(t.Context(), "canonical-one")
	assistantRunMust(t, err)
	again, err := reopened.Commit(t.Context(), canonicalOpen("canonical-one"))
	assistantRunMust(t, err)
	if again != first {
		t.Fatal("reopen lost original exact receipt")
	}
	// Namespace checks remain owned by the actual private store. A stale store
	// cannot resolve its old journal against another project directory.
	other := t.TempDir()
	f.store.server.projectDir = other
	if _, err = reopened.Load(t.Context(), "canonical-one"); err == nil {
		t.Fatal("stale project namespace read accepted")
	}
}
func TestAssistantCanonicalStoreFaultKeepsCallbackRevisionAndExactReceipt(t *testing.T) {
	f := assistantCanonicalStoreFixture(t, "canonical-one")
	input := canonicalOpen("canonical-one")
	reads := 0
	injected := errors.New("post-rename readback fixture")
	testHookPrivateRead = func(name string) error {
		if name == f.store.name {
			reads++
			if reads == 2 {
				return injected
			}
		}
		return nil
	}
	_, err := f.port.Commit(t.Context(), input)
	testHookPrivateRead = nil
	if !errors.Is(err, injected) || f.writer.Err() != nil {
		t.Fatal("post-publication uncertainty poisoned data port")
	}
	head, err := f.port.Load(t.Context(), "canonical-one")
	assistantRunMust(t, err)
	if head == nil {
		t.Fatal("published head was lost")
	}
	receipt, err := f.port.Commit(t.Context(), input)
	assistantRunMust(t, err)
	if receipt.Revision != head.Revision {
		t.Fatal("duplicate receipt changed revision")
	}
	assistantRunMust(t, f.writer.Intent(t.Context(), assistantRunOp("after-reconcile")))
	// The actual file is private and remains in the existing storage directory.
	private, err := f.store.server.openChatData()
	assistantRunMust(t, err)
	defer private.root.Close()
	info, err := os.Stat(filepath.Join(private.location.Path, f.store.name))
	assistantRunMust(t, err)
	if info.Mode().Perm() != custodyFileMode {
		t.Fatal("canonical bytes escaped private file custody")
	}
}
