package desk

// Originals and their acquisition packets live beside private conversations.
// A reference contains no filesystem path. The project-history binding determines
// the namespace, so explicit project relinking also relinks its document objects.
import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const maxAttachmentObjectBytes = 64 << 20

var attachmentID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var attachmentSession = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
var attachmentFileName = regexp.MustCompile(`^attachment-[a-f0-9]{64}-[a-f0-9-]{36}\.json$`)

type attachmentOriginal struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Bytes     string `json:"bytes"`
	SHA256    string `json:"sha256"`
}
type attachmentProof struct {
	Web *struct {
		URL  string `json:"url"`
		Site string `json:"site,omitempty"`
	} `json:"web,omitempty"`
	Session   string `json:"session"`
	Source    string `json:"source"`
	Authority string `json:"authority"`
	PublicKey string `json:"publicKey"`
	Response  string `json:"response"`
	Registry  string `json:"registry"`
	Drive     *struct {
		Grant  string `json:"grant"`
		FileID string `json:"fileId"`
	} `json:"drive,omitempty"`
	Gmail *struct {
		Grant     string `json:"grant"`
		MessageID string `json:"messageId"`
	} `json:"gmail,omitempty"`
	Connected *struct {
		Grant      string `json:"grant"`
		ResourceID string `json:"resourceId"`
	} `json:"connected,omitempty"`
	Resource *struct {
		Grant      string `json:"grant"`
		ResourceID string `json:"resourceId"`
	} `json:"resource,omitempty"`
}
type attachmentObject struct {
	Version  int                `json:"version"`
	Original attachmentOriginal `json:"original"`
	Proof    *attachmentProof   `json:"proof,omitempty"`
}

func validateAttachmentObject(data []byte) error {
	var doc attachmentObject
	if err := decodeDataJSON(data, &doc); err != nil {
		return err
	}
	if doc.Version != 1 || len(doc.Original.Name) == 0 || len(doc.Original.Name) > 255 || strings.ContainsAny(doc.Original.Name, "\x7f") {
		return errors.New("invalid attachment name or version")
	}
	for _, c := range doc.Original.Name {
		if c < 32 {
			return errors.New("invalid attachment name")
		}
	}
	switch doc.Original.MediaType {
	case "application/pdf", "text/plain", "text/markdown", "text/csv", "application/json":
	default:
		return errors.New("unsupported attachment media type")
	}
	if len(doc.Original.Bytes) > base64.StdEncoding.EncodedLen(16<<20) {
		return errors.New("attachment exceeds the original byte limit")
	}
	original, err := base64.StdEncoding.Strict().DecodeString(doc.Original.Bytes)
	if err != nil || len(original) == 0 || len(original) > 16<<20 || base64.StdEncoding.EncodeToString(original) != doc.Original.Bytes || doc.Original.SHA256 != "sha256:"+digestOf(original) {
		return errors.New("attachment original checksum or encoding is invalid")
	}
	if p := doc.Proof; p != nil {
		if !researchSourceName.MatchString(p.Source) || !publicKeyHex.MatchString(p.PublicKey) || !authorityLabel.MatchString(p.Authority) || !attachmentSession.MatchString(p.Session) || p.Session == "." || p.Session == ".." || len(p.Response) > 16<<20 || len(p.Registry) > 4<<20 || !json.Valid([]byte(p.Response)) {
			return errors.New("invalid attachment acquisition packet")
		}
	}
	return nil // Cryptographic and page-contract verification is the consumer's job.
}

func storageFileLimit(name string) int {
	if memoryFileName.MatchString(name) {
		return memoryLimit
	}
	if attachmentFileName.MatchString(name) {
		return maxAttachmentObjectBytes
	}
	return maxConversationBytes
}

func attachmentProjectPrefix(conversation string) string {
	return "attachment-" + strings.TrimSuffix(strings.TrimPrefix(conversation, "conversations-"), ".json") + "-"
}

func (s *Server) handleAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	id := r.PathValue("id")
	if !attachmentID.MatchString(id) || r.URL.RawQuery != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "invalid attachment reference")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// Read bounded request bytes before holding storage locks. A slow sender must
	// not block unrelated chat saves or storage relocation.
	var body []byte
	if r.Method == http.MethodPut {
		select {
		case s.researchSlots <- struct{}{}:
			defer func() { <-s.researchSlots }()
		default:
			writeJSONCoded(w, 503, CodeResearchRelayBusy, "document storage is busy; retry after current uploads finish")
			return
		}
		if r.ContentLength > maxAttachmentObjectBytes {
			writeJSONCoded(w, 413, CodeTooLarge, "attachment object exceeds its storage limit")
			return
		}
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(researchDeadline))
		defer controller.SetReadDeadline(time.Time{})
		var err error
		body, err = readBounded(r.Body, maxAttachmentObjectBytes)
		if err != nil {
			writeJSONCoded(w, 413, CodeTooLarge, "attachment object exceeds its storage limit")
			return
		}
		receipt, err := s.attachmentStorage().write(r.Context(), attachmentWriteRequest{
			id: id, ifMatch: r.Header.Get("If-Match"), data: body,
		})
		if err != nil {
			var storage *attachmentStorageError
			if errors.As(err, &storage) {
				storageFailure(w, storage.err)
			} else {
				writeJSONError(w, statusForRefusal(err), err)
			}
			return
		}
		writeJSON(w, http.StatusOK, receipt)
		return
	}

	s.writes.Lock()
	defer s.writes.Unlock()
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer lock.Close()
	store, err := s.openChatData()
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer store.root.Close()
	conversation, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		storageFailure(w, err)
		return
	}
	name := attachmentProjectPrefix(conversation) + id + ".json"
	current, err := readPrivateData(store.root, name, maxAttachmentObjectBytes)
	exists := !errors.Is(err, os.ErrNotExist)
	if err != nil && exists {
		storageFailure(w, err)
		return
	}
	if !exists {
		writeJSONCoded(w, 404, CodeNotFound, "attachment is unavailable in this project's chat storage")
		return
	}
	w.Header().Set("ETag", digestOf(current))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(current)
}
