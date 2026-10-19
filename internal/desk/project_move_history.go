package desk

// A move keeps the existing history namespace. Only the binding changes; saved
// evidence, quotations, reviews and decisions are never rewritten.
import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

const historyMoveConflict = "This location already has saved history. Desk has not combined the histories. Recover them separately in Admin → Storage & backups before continuing."

func historyMember(name, conversation string) bool {
	if name == conversation || name == draftPackName(conversation) {
		return true
	}
	for _, prefix := range []string{"ai-memory-", "briefs-", "pack-tests-", "source-reviews-"} {
		if name == strings.Replace(conversation, "conversations-", prefix, 1) {
			return true
		}
	}
	return attachmentFileName.MatchString(name) && strings.HasPrefix(name, attachmentProjectPrefix(conversation))
}

// Caller holds the data lock. Also inspect sibling records when there are no
// chats yet: creating a fresh namespace would hide drafts and attachments.
func movedHistoryBinding(root *os.Root, previous, next string) (projectBindings, string, bool, error) {
	bindings, _, err := readProjectBindings(root)
	if err != nil {
		return bindings, "", false, err
	}
	source, err := resolveConversationName(root, previous)
	if err != nil {
		return bindings, "", false, err
	}
	target := "conversations-" + digestOf([]byte(next)) + ".json"
	if linked, ok := bindings.Projects[next]; ok {
		if linked != source {
			return bindings, "", false, errors.New(historyMoveConflict)
		}
		// Repeating after an interrupted identity write is safe; do not rebind.
		if _, err := resolveConversationName(root, next); err != nil {
			return bindings, "", false, err
		}
	}
	entries, err := storageEntries(root)
	if err != nil {
		return bindings, "", false, err
	}
	found := false
	for _, entry := range entries {
		name := entry.Name()
		if target != source && historyMember(name, target) {
			return bindings, "", false, errors.New(historyMoveConflict)
		}
		if historyMember(name, source) {
			info, err := root.Lstat(name)
			if err == nil {
				err = ownerOnlyFile(name, info.Mode())
			}
			if err == nil {
				err = ownedByUs(name, info)
			}
			if err != nil {
				return bindings, "", false, err
			}
			found = true
		}
	}
	return bindings, source, found, nil
}

func linkMovedHistory(root *os.Root, previous, next string) error {
	if previous == next {
		return nil
	}
	bindings, source, found, err := movedHistoryBinding(root, previous, next)
	if err != nil || !found {
		return err
	}
	if bindings.Projects[next] == source {
		return nil
	}
	// Bindings require a conversations anchor. With only sibling history, make
	// an empty anchor at the OLD namespace, never a replacement of existing data.
	if _, err = root.Lstat(source); errors.Is(err, os.ErrNotExist) {
		err = claimPrivateData(root, source, []byte(`{"version":1,"chats":[]}`))
	}
	if err != nil {
		return err
	}
	bindings.Projects[next] = source
	data, err := json.Marshal(bindings)
	if err == nil {
		_, err = decodeProjectBindings(data)
	}
	if err != nil {
		return err
	}
	return writePrivateData(root, projectBindingsName, data)
}

// Called with the project lock held, BEFORE overwriting the old identity path.
func (s *Server) retainMovedHistory(previous, next string) error {
	if previous == next {
		return nil
	}
	work, err := s.privateDataFileLock(".data-work.lock", true)
	if err != nil {
		return err
	}
	defer work.Close()
	s.writes.Lock()
	defer s.writes.Unlock()
	lock, err := s.privateDataLock(true)
	if err != nil {
		return err
	}
	defer lock.Close()
	store, err := s.openChatData()
	if err != nil {
		return err
	}
	defer store.root.Close()
	return linkMovedHistory(store.root, previous, next)
}
