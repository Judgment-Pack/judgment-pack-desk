package desk

// Saved acquisition packets are data, never authority. This consumer uses the
// installed, integrity-checked Gateway's canonicalizer and verifier offline.
import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

var errAssistantDocument = errors.New("The saved document could not be verified for this request. Review its pages before sending.")

type assistantDocumentReference struct {
	ID           string `json:"id"`
	Digest       string `json:"digest"`
	Pages        []int  `json:"pages"`
	AllowPartial bool   `json:"allowPartial"`
	NeedsReview  bool   `json:"needsReview,omitempty"`
}
type assistantDocumentPage struct {
	Page     int    `json:"page"`
	Citation string `json:"citation"`
	Text     string `json:"text"`
}

// Private fields bind this snapshot to the exact authenticated request and
// storage bytes. Callers recheck immediately before dispatch, alongside their
// existing run admission. Neither this object nor a saved ref grants a write.
type assistantVerifiedDocument struct {
	Name             string
	record           assistantDocumentRecord
	Pages            []assistantDocumentPage
	Partial          bool
	owner            assistantDocumentOwner
	reference        assistantDocumentReference
	attachmentDigest string
	gateway          researchGateway
}

type assistantDocumentOwner struct {
	start                     assistantRunStart
	binding                   assistantRunBinding
	chatDigest, requestDigest string
}

func assistantDocumentOwnerOf(source assistantRunSource) assistantDocumentOwner {
	binding := source.Binding
	binding.SourceDigests = append([]string(nil), binding.SourceDigests...)
	return assistantDocumentOwner{start: source.Start, binding: binding, chatDigest: digestOf(source.Original), requestDigest: digestOf(source.Request)}
}

// Canonical ledger projection matches chat/attachmentContext.ts. Only selected
// readable pages enter the value. The caller retains it as attachment material.
func (held assistantVerifiedDocument) contextDocument() runcontext.Document {
	pages := make([]*runcontext.Value, 0, len(held.Pages))
	for _, page := range held.Pages {
		pages = append(pages, runcontext.String(fmt.Sprintf("[page %d; cite it as %s]\n%s", page.Page, page.Citation, page.Text)))
	}
	return runcontext.Document{ID: "attachment-" + held.reference.ID, Kind: "artifact", Label: "Attached document: " + held.Name, Revision: held.reference.Digest, Value: runcontext.ObjectOf("pages", runcontext.Array(pages...))}
}

func assistantDocumentReferenceValid(ref assistantDocumentReference) bool {
	if !attachmentID.MatchString(ref.ID) || !strings.HasPrefix(ref.Digest, "sha256:") || !assistantRunDigest(strings.TrimPrefix(ref.Digest, "sha256:")) || len(ref.Pages) == 0 || len(ref.Pages) > 500 {
		return false
	}
	seen := map[int]bool{}
	for _, n := range ref.Pages {
		if n < 1 || n > 500 || seen[n] {
			return false
		}
		seen[n] = true
	}
	return true
}

// Read exact, existing reference fields only; arbitrary nested material in a
// saved chat cannot nominate an attachment or substitute a different selection.
func assistantDocumentReferenced(raw []byte, ref assistantDocumentReference) bool {
	var chat struct {
		Documents   []json.RawMessage `json:"documents"`
		Attachments []json.RawMessage `json:"attachments"`
		Checkpoint  struct {
			State struct {
				Turns []struct {
					Attachments []json.RawMessage `json:"attachments"`
				} `json:"turns"`
				Responses []struct {
					Documents []json.RawMessage `json:"documents"`
				} `json:"responses"`
			} `json:"state"`
		} `json:"checkpoint"`
	}
	if json.Unmarshal(raw, &chat) != nil {
		return false
	}
	files := append(chat.Documents, chat.Attachments...)
	for _, turn := range chat.Checkpoint.State.Turns {
		files = append(files, turn.Attachments...)
	}
	for _, response := range chat.Checkpoint.State.Responses {
		files = append(files, response.Documents...)
	}
	for _, file := range files {
		var item map[string]json.RawMessage
		if json.Unmarshal(file, &item) != nil || item["document"] == nil {
			continue
		}
		var held assistantDocumentReference
		if json.Unmarshal(item["document"], &held) == nil && reflect.DeepEqual(held, ref) {
			return true
		}
	}
	return false
}

// Takes a host-resolved saved source, never a path, URL, browser claim or callback.
// It owns its short storage lock: do not call while holding that lock.
func (s *Server) readAssistantDocument(r *http.Request, source assistantRunSource, ref assistantDocumentReference) (assistantVerifiedDocument, error) {
	var zero assistantVerifiedDocument
	if !assistantDocumentReferenceValid(ref) {
		return zero, errAssistantDocument
	}
	owner := assistantDocumentOwnerOf(source)
	raw, pin, err := s.assistantDocumentSnapshot(r, owner, ref)
	if err != nil {
		return zero, err
	}
	program, err := s.researchVerifier()
	if err != nil {
		return zero, err
	}
	held, err := verifyAssistantDocument(r.Context(), program, raw, inputResearchPin{Authority: pin.authority, Public: pin.signerPublic}, ref)
	if err != nil {
		return zero, err
	}
	held.owner = owner
	held.reference = ref
	held.reference.Pages = append([]int(nil), ref.Pages...)
	held.attachmentDigest = digestOf(raw)
	held.gateway = pin
	if err = s.recheckAssistantDocument(r, held); err != nil {
		return zero, err
	}
	return held, nil
}

func (s *Server) assistantDocumentSnapshot(r *http.Request, owner assistantDocumentOwner, ref assistantDocumentReference) ([]byte, researchGateway, error) {
	var zero researchGateway
	if err := r.Context().Err(); err != nil {
		return nil, zero, err
	}
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		return nil, zero, err
	}
	defer lock.Close()
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil || !assistantRunIdentityMatches(identity, owner.binding) {
		return nil, zero, errAssistantRunScope
	}
	// This resolver does not reacquire the storage lock. It reauthorizes the
	// selected Policy owner, the saved request and exact chat revision.
	store, err := s.openChatData()
	if err != nil {
		return nil, zero, err
	}
	defer store.root.Close()
	project, err := assistantRunNamespace(store.root, s.conversationIdentity())
	if err != nil {
		return nil, zero, err
	}
	current, err := s.resolveAssistantRun(r, owner.start, identity, project)
	if err != nil {
		return nil, zero, err
	}
	// The durable run assigns these two fields after Prepare; the saved
	// source resolver does not. Its admission fence owns their authorization.
	current.Binding.Unit = owner.binding.Unit
	current.Binding.ExecutionKey = owner.binding.ExecutionKey
	if !reflect.DeepEqual(current.Binding, owner.binding) || digestOf(current.Original) != owner.chatDigest || digestOf(current.Request) != owner.requestDigest || !assistantDocumentReferenced(current.Original, ref) {
		return nil, zero, errAssistantRunScope
	}
	pin, err := s.configuredResearch()
	if err != nil {
		return nil, zero, err
	}
	conversation, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		return nil, zero, err
	}
	raw, err := readPrivateData(store.root, attachmentProjectPrefix(conversation)+ref.ID+".json", maxAttachmentObjectBytes)
	if err != nil {
		return nil, zero, errAssistantDocument
	}
	return raw, pin, r.Context().Err()
}
func (s *Server) recheckAssistantDocument(r *http.Request, held assistantVerifiedDocument) error {
	raw, pin, err := s.assistantDocumentSnapshot(r, held.owner, held.reference)
	if err != nil {
		return err
	}
	if digestOf(raw) != held.attachmentDigest || !reflect.DeepEqual(pin, held.gateway) {
		return errAssistantDocument
	}
	return nil
}

// Bound subprocess output as well as input. A cancellation never yields pages.
func assistantDocumentCommand(ctx context.Context, program string, input []byte, limit int, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errAssistantDocument
	}
	if err = cmd.Start(); err != nil {
		return nil, errAssistantDocument
	}
	raw, readErr := readBounded(out, limit)
	if readErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if readErr != nil || waitErr != nil {
		return nil, errAssistantDocument
	}
	return raw, nil
}
func assistantDocumentCanon(ctx context.Context, program string, raw []byte, limit int) ([]byte, error) {
	if len(raw) == 0 || len(raw) > limit || !validUTF8(raw) || !uniqueJSON(raw) {
		return nil, errAssistantDocument
	}
	return assistantDocumentCommand(ctx, program, raw, limit, "canon")
}

func verifyAssistantDocument(ctx context.Context, program string, raw []byte, pin inputResearchPin, ref assistantDocumentReference) (assistantVerifiedDocument, error) {
	return verifyAssistantDocumentPacket(ctx, program, raw, pin, ref, false)
}

// Review custody verifies the entire packet but never releases unconsented text.
// Saved-reference readers always use the stricter wrapper above.
func verifyAssistantDocumentPacket(ctx context.Context, program string, raw []byte, pin inputResearchPin, ref assistantDocumentReference, review bool) (assistantVerifiedDocument, error) {
	var zero assistantVerifiedDocument
	if !assistantDocumentReferenceValid(ref) && !(review && attachmentID.MatchString(ref.ID) && adDigest(ref.Digest) && len(ref.Pages) == 0) {
		return zero, errAssistantDocument
	}
	// Canonicalize only for validation; never replace the retained original bytes.
	if _, err := assistantDocumentCanon(ctx, program, raw, maxAttachmentObjectBytes); err != nil {
		return zero, err
	}
	if !assistantDocumentPacketFields(raw) || validateAttachmentObject(raw) != nil {
		return zero, errAssistantDocument
	}
	var object attachmentObject
	_ = json.Unmarshal(raw, &object)
	p := object.Proof
	if p == nil || p.Authority != pin.Authority || p.PublicKey != pin.Public {
		return zero, errAssistantDocument
	}
	// Decode arguments from their original JSON members. Struct round trips would
	// discard future signed members and change the exact argument commitment.
	var wire struct {
		Proof map[string]json.RawMessage `json:"proof"`
	}
	_ = json.Unmarshal(raw, &wire)
	var args json.RawMessage
	shape := "command"
	selected := 0
	for _, kind := range []string{"drive", "gmail", "connected", "resource", "web"} {
		if value, ok := wire.Proof[kind]; ok && string(value) != "null" {
			selected++
			args = value
			if kind == "drive" || kind == "gmail" || kind == "web" {
				shape = "http"
			}
			if kind == "connected" && p.Source == "notion" {
				shape = "mcp"
			}
			if kind == "resource" {
				shape = ""
			}
		}
	}
	if selected > 1 {
		return zero, errAssistantDocument
	}
	if selected == 0 {
		args, _ = json.Marshal(map[string]any{"document": object.Original, "options": map[string]string{"ocr": "auto"}})
	}
	response, err := assistantDocumentCanon(ctx, program, []byte(p.Response), 16<<20)
	if err != nil {
		return zero, err
	}
	var env struct {
		Result  json.RawMessage   `json:"result"`
		Receipt json.RawMessage   `json:"receipt"`
		Salts   map[string]string `json:"salts"`
	}
	if json.Unmarshal(response, &env) != nil {
		return zero, errAssistantDocument
	}
	var receipt struct {
		Version     string `json:"receiptVersion"`
		Kind        string `json:"kind"`
		Source      string `json:"source"`
		Session     string `json:"sessionId"`
		Index       int    `json:"callIndex"`
		Digest      string `json:"resultDigest"`
		Commitment  string `json:"argumentsCommitment"`
		Acquisition struct {
			Shape string `json:"shape"`
		} `json:"acquisition"`
	}
	if json.Unmarshal(env.Receipt, &receipt) != nil || receipt.Version != "3" || receipt.Kind != "acquisition" || receipt.Source != p.Source || receipt.Session != p.Session || receipt.Index != 0 || receipt.Digest != ref.Digest || (shape != "" && receipt.Acquisition.Shape != shape) || (shape == "" && !contains([]string{"command", "http", "mcp"}, receipt.Acquisition.Shape)) {
		return zero, errAssistantDocument
	}
	canonicalArgs, err := assistantDocumentCanon(ctx, program, args, maxAttachmentObjectBytes)
	if err != nil {
		return zero, err
	}
	salt, err := hex.DecodeString(env.Salts["args"])
	if err != nil || len(salt) != 32 || env.Salts["args"] != hex.EncodeToString(salt) || receipt.Commitment != "sha256:"+digestOf(append(append(salt, []byte("args:")...), canonicalArgs...)) {
		return zero, errAssistantDocument
	}
	result, err := assistantDocumentCanon(ctx, program, env.Result, 16<<20)
	if err != nil {
		return zero, err
	}
	if receipt.Digest != "sha256:"+digestOf(result) {
		return zero, errAssistantDocument
	}
	// The packet holds exactly one receipt. Isolate its registry session rather
	// than claiming a verdict about other Gateway sessions we do not hold.
	var seals bytes.Buffer
	for _, line := range strings.Split(p.Registry, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) > 64<<10 {
			return zero, errAssistantDocument
		}
		if !validUTF8([]byte(line)) || !uniqueJSON([]byte(line)) {
			return zero, errAssistantDocument
		}
		var seal struct {
			Session string `json:"sessionId"`
		}
		if json.Unmarshal([]byte(line), &seal) != nil {
			return zero, errAssistantDocument
		}
		if seal.Session == p.Session {
			seals.WriteString(line)
			seals.WriteByte('\n')
		}

	}
	if seals.Len() == 0 {
		return zero, errAssistantDocument
	}
	dir, err := os.MkdirTemp("", "desk-document-proof-")
	if err != nil {
		return zero, errAssistantDocument
	}
	defer os.RemoveAll(dir)
	for _, sub := range []string{"artifacts", filepath.Join("receipts", p.Session)} {
		if os.MkdirAll(filepath.Join(dir, sub), 0700) != nil {
			return zero, errAssistantDocument
		}
	}
	for name, data := range map[string][]byte{filepath.Join("artifacts", strings.TrimPrefix(receipt.Digest, "sha256:")): result, filepath.Join("receipts", p.Session, "0.json"): env.Receipt, "registry.jsonl": seals.Bytes()} {
		if os.WriteFile(filepath.Join(dir, name), data, 0600) != nil {
			return zero, errAssistantDocument
		}
	}
	verdict, err := assistantDocumentCommand(ctx, program, []byte(pin.Public), 64<<10, "verify", dir, filepath.Join(dir, "registry.jsonl"), pin.Authority)
	var verified struct {
		OK bool `json:"ok"`
	}
	if err != nil {
		return zero, err
	}
	if json.Unmarshal(verdict, &verified) != nil || !verified.OK {
		return zero, errAssistantDocument
	}
	record, err := readAssistantDocumentRecord(result)
	if err != nil {
		return zero, err
	}
	if !assistantDocumentOriginalBinding(record, object) {
		return zero, errAssistantDocument
	}
	if !review {
		if record.partial && !ref.AllowPartial {
			return zero, errAssistantDocument
		}
	}
	held := assistantVerifiedDocument{Name: record.name, record: record, Partial: record.partial, reference: ref}
	held.reference.Pages = append([]int(nil), ref.Pages...)
	for _, number := range ref.Pages {
		if number > len(record.pages) {
			return zero, errAssistantDocument
		}
		page := record.pages[number-1]
		if !page.usable {
			return zero, errAssistantDocument
		}
		if review && record.partial && !ref.AllowPartial {
			continue
		}
		held.Pages = append(held.Pages, assistantDocumentPage{Page: number, Citation: fmt.Sprintf("attachment:%s/%s/page/%d", ref.ID, ref.Digest, number), Text: page.text})
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return held, nil
}

// encoding/json accepts case aliases; the browser and signature contracts do
// not. Keep the storage wrapper's declared field spellings exact as well.
func assistantDocumentPacketFields(raw []byte) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return false
	}
	allowed := func(m map[string]json.RawMessage, names ...string) bool {
		for key := range m {
			if !contains(names, key) {
				return false
			}
		}
		return true
	}
	if !allowed(object, "version", "original", "proof") || object["version"] == nil || object["original"] == nil || object["proof"] == nil {
		return false
	}
	var original, proof map[string]json.RawMessage
	if json.Unmarshal(object["original"], &original) != nil || json.Unmarshal(object["proof"], &proof) != nil || len(original) != 4 || !allowed(original, "name", "mediaType", "bytes", "sha256") || !allowed(proof, "session", "source", "authority", "publicKey", "response", "registry", "drive", "gmail", "connected", "resource", "web") {
		return false
	}
	for _, key := range []string{"session", "source", "authority", "publicKey", "response", "registry"} {
		if proof[key] == nil {
			return false
		}
	}
	for _, key := range []string{"drive", "gmail", "connected", "resource", "web"} {
		raw, ok := proof[key]
		if !ok || string(raw) == "null" {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return false
		}
		switch key {
		case "drive":
			if len(fields) != 2 || !allowed(fields, "grant", "fileId") {
				return false
			}
		case "gmail":
			if len(fields) != 2 || !allowed(fields, "grant", "messageId") {
				return false
			}
		case "web":
			if !allowed(fields, "url", "site") || fields["url"] == nil {
				return false
			}
		default:
			if len(fields) != 2 || !allowed(fields, "grant", "resourceId") {
				return false
			}
		}
	}
	return true
}
