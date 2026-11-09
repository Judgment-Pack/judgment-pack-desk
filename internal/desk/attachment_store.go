package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// attachmentStore owns attachment validation and the complete private-storage
// transaction. Its server supplies the store owner and project-history identity;
// callers cannot choose a directory, filename, or another project's namespace.
type attachmentStore struct{ server *Server }

func (s *Server) attachmentStorage() attachmentStore { return attachmentStore{server: s} }

type attachmentWriteRequest struct {
	id      string
	ifMatch string
	data    []byte
	// checkCurrent is an optional, invocation-local admission recheck for an
	// authenticated host. It must only inspect local state, never perform network
	// or subprocess work while storage locks are held. It cannot replace any of
	// the owner's validation, configuration, CAS, custody, or quota checks.
	checkCurrent func() error
}

type attachmentReceipt struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

// Keep the manual endpoint's storage-error presentation separate from authored
// refusals, without making the storage owner depend on HTTP.
type attachmentStorageError struct{ err error }

func (e *attachmentStorageError) Error() string { return e.err.Error() }
func (e *attachmentStorageError) Unwrap() error { return e.err }

func (owner attachmentStore) write(ctx context.Context, request attachmentWriteRequest) (attachmentReceipt, error) {
	var zero attachmentReceipt
	refuse := func(code, message string) (attachmentReceipt, error) {
		return zero, withCode(code, errors.New(message))
	}
	fail := func(err error) (attachmentReceipt, error) {
		return zero, &attachmentStorageError{err: err}
	}
	if !attachmentID.MatchString(request.id) {
		return refuse(CodeBadRequest, "invalid attachment reference")
	}
	if len(request.data) > maxAttachmentObjectBytes {
		return refuse(CodeTooLarge, "attachment object exceeds its storage limit")
	}
	request.data = bytes.Clone(request.data)
	// Validate bounded input before either lock. Keep the exact caller bytes:
	// both CAS and the receipt refer to stored bytes, not re-encoded JSON.
	if err := validateAttachmentObject(request.data); err != nil {
		return refuse(CodeBadRequest, err.Error())
	}
	s := owner.server
	var after attachmentObject
	_ = json.Unmarshal(request.data, &after)
	if request.ifMatch == "absent" {
		gateway, err := s.configuredResearch()
		if err != nil {
			return zero, err
		}
		n := len(after.Original.Bytes)/4*3 - (len(after.Original.Bytes) - len(strings.TrimRight(after.Original.Bytes, "=")))
		if gateway.maxFileBytes == 0 {
			return refuse(CodeResearchUnconfigured, "document uploads are not enabled")
		}
		if int64(n) > gateway.maxFileBytes {
			return refuse(CodeTooLarge, "original exceeds the configured document upload limit")
		}
		if after.Proof != nil {
			return refuse(CodeBadRequest, "retain the original before adding an acquisition packet")
		}
	}

	s.writes.Lock()
	defer s.writes.Unlock()
	lock, err := s.privateDataLockContext(ctx, true)
	if err != nil {
		return fail(err)
	}
	defer lock.Close()
	if request.checkCurrent != nil {
		if err := request.checkCurrent(); err != nil {
			return zero, err
		}
	}
	store, err := s.openChatData()
	if err != nil {
		return fail(err)
	}
	defer store.root.Close()
	conversation, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		return fail(err)
	}
	name := attachmentProjectPrefix(conversation) + request.id + ".json"
	current, err := readPrivateData(store.root, name, maxAttachmentObjectBytes)
	exists := !errors.Is(err, os.ErrNotExist)
	if err != nil && exists {
		return fail(err)
	}
	expected := "absent"
	if exists {
		expected = digestOf(current)
	}
	if request.ifMatch != expected {
		return refuse(CodeStale, "attachment changed; reload before saving")
	}
	if exists {
		var before attachmentObject
		if err := validateAttachmentObject(current); err != nil {
			return fail(err)
		}
		_ = json.Unmarshal(current, &before)
		if before.Original != after.Original || before.Proof != nil {
			return refuse(CodeStale, "an original or completed acquisition cannot be replaced")
		}
	}
	// A hard store-wide quota also bounds abandoned/unsent uploads. They are
	// retained intentionally, and included in backups, rather than silently erased.
	entries, err := storageEntries(store.root)
	if err != nil {
		return fail(err)
	}
	// Reserve the first conversation and storage bookkeeping, including backups.
	if !exists && len(entries) >= maxStorageFiles-4 {
		return refuse(CodeTooLarge, "private chat storage has reached its upload file limit")
	}
	total := int64(len(request.data) - len(current))
	for _, entry := range entries {
		if chatStorageFile(entry.Name()) {
			info, err := store.root.Lstat(entry.Name())
			if err != nil {
				return fail(err)
			}
			total += info.Size()
		}
	}
	if total > maxMoveBytes {
		return refuse(CodeTooLarge, "private chat storage has reached its 1 GiB upload limit")
	}
	if request.checkCurrent != nil {
		if err := request.checkCurrent(); err != nil {
			return zero, err
		}
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := writePrivateData(store.root, name, request.data); err != nil {
		// A readback/durability failure may follow rename. Return no receipt and
		// never retry: the caller must reconcile the retained exact object.
		return fail(err)
	}
	return attachmentReceipt{ID: request.id, SHA256: digestOf(request.data)}, nil
}
