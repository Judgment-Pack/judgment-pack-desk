package desk

// Document processing (OCR) is the local gateway's (gateway v0.10.0, #218):
// its connections companion keeps the settings, a processor and the cloud
// credential it needs, in its own store under this desk's connections
// directory, and the gateway reads them when it is asked for its plan. This
// route carries the page's three operations, `status`, `configure` and
// `test`, to this desk's own companion and to nothing else.
//
// A cloud credential passes through here once, on its way from the page to
// the companion, in a `configure` request's body. Desk keeps none of it: it
// is never logged, never written to Desk's own storage or configuration,
// never in a URL or an error, and every answer Desk gives the page is
// rebuilt from the members it names, none of which is a credential.
//
// The plan is read when the local gateway starts, so a `configure` that turns
// a processor on or off is followed by a restart of the local gateway, which
// cuts the reads it is carrying; the page says so before the owner saves.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// processingControlBytes bounds a status or configure request, the
	// companion's control line; processingTestBytes a test's, which carries a
	// PDF of at most 4 MiB, encoded. The line Desk writes is held to the
	// companion's own bound as well (companionRequestBound).
	processingControlBytes = 64 << 10
	processingTestBytes    = 6 << 20
	// The companion gives a test 140 seconds and every other request 50.
	processingTestTimeout    = 145 * time.Second
	processingControlTimeout = 55 * time.Second
)

// processingErrors are the companion's refusals the page may be given, each
// by its one word. Any other is given as processing-failed.
var processingErrors = map[string]bool{
	"invalid-request":             true,
	"private-storage-unavailable": true,
	"blocked-by-policy":           true,
	"processing-changed":          true,
	"processing-unavailable":      true,
	"program-not-allowed":         true,
	"processor-not-installed":     true,
	"processing-failed":           true,
	"response-too-large":          true,
}

var (
	processingDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	processingWord   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
)

// processingConnection is one processor as the page is shown it. It has no
// credential member: the companion blanks the credential and says only
// whether one is held, and Desk passes on nothing it does not name here.
type processingConnection struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Kind                 string `json:"kind"`
	Program              string `json:"program,omitempty"`
	Enabled              bool   `json:"enabled"`
	Ready                bool   `json:"ready,omitempty"`
	Project              string `json:"project,omitempty"`
	Location             string `json:"location,omitempty"`
	Processor            string `json:"processor,omitempty"`
	Endpoint             string `json:"endpoint,omitempty"`
	Region               string `json:"region,omitempty"`
	CredentialConfigured bool   `json:"credentialConfigured,omitempty"`
}

// processingStatus is the companion's answer to status and configure.
type processingStatus struct {
	Version        int                    `json:"version"`
	Mode           string                 `json:"mode"`
	Connection     string                 `json:"connection"`
	Connections    []processingConnection `json:"connections"`
	TimeoutSeconds int                    `json:"timeoutSeconds,omitempty"`
	SHA256         string                 `json:"sha256"`
	State          string                 `json:"state"`
}

// processingTest is the companion's answer to test: a preview of at most
// eight pages of 400 characters, and the record's status and error codes.
// The records' error messages are left out: the page says each code in its
// own words.
type processingTest struct {
	Processing struct {
		Status string                `json:"status"`
		Errors []processingTestError `json:"errors"`
	} `json:"processing"`
	Extraction string               `json:"extraction"`
	PageCount  int                  `json:"pageCount"`
	Pages      []processingTestPage `json:"pages"`
}
type processingTestError struct {
	Code string `json:"code"`
	Page *int   `json:"page"`
}
type processingTestPage struct {
	Number     int    `json:"number"`
	Status     string `json:"status"`
	Extraction string `json:"extraction"`
	Text       string `json:"text"`
}

// processingGateway is what Desk says of its local gateway beside each
// answer: whether it runs, whether the plan it runs gives the document
// sources the processing envelope, and whether this request restarted it.
type processingGateway struct {
	Status             string `json:"status"`
	DocumentProcessing *bool  `json:"documentProcessing,omitempty"`
	Restarted          bool   `json:"restarted,omitempty"`
	AppliesWhenStarted bool   `json:"appliesWhenStarted,omitempty"`
	Problem            string `json:"problem,omitempty"`
}

type processingAnswer struct {
	Result       any               `json:"result,omitempty"`
	Error        string            `json:"error,omitempty"`
	LocalGateway processingGateway `json:"localGateway"`
}

func (s *Server) handleDocumentProcessing(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	method := r.PathValue("method")
	if method != "status" && method != "configure" && method != "test" {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "unknown document processing operation")
		return
	}
	// The body is the one place a credential travels; a query would put it
	// where a URL is logged, so a request with one is refused unread.
	if r.URL.RawQuery != "" {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "document processing operations carry no query; nothing was sent")
		return
	}
	limit := int64(processingControlBytes)
	if method == "test" {
		limit = processingTestBytes
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "document processing request too large; nothing was sent")
		return
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	var obj map[string]json.RawMessage
	if decodeDataJSON(body, &obj) != nil || obj == nil {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "invalid document processing request; nothing was sent")
		return
	}
	// Refuse ambiguous member names before interpreting or relaying them.
	_, duplicate, memberErr := topLevelMembers(body)
	if memberErr != nil || duplicate != "" {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "invalid document processing request; nothing was sent")
		return
	}
	seen := map[string]bool{}
	for name := range obj {
		folded := strings.ToLower(name)
		caseVariant := false
		for _, own := range []string{"restartWarned", "config", "ifMatch"} {
			if strings.EqualFold(name, own) && name != own {
				caseVariant = true
			}
		}
		if caseVariant || seen[folded] {
			writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "invalid document processing request; nothing was sent")
			return
		}
		seen[folded] = true
	}
	// Only this desk's own local gateway: never a gateway the desk file names,
	// and never a fallback to the local one while the desk file names another.
	_, raw, err := s.readDeskFile()
	if err != nil {
		writeJSONCoded(w, http.StatusConflict, CodeResearchUnconfigured, "document processing settings could not be read; nothing was sent")
		return
	}
	status := s.localGatewayStatus(raw)
	if s.localGateway == nil || status == nil || (status.Status != "ready" && status.Status != "unavailable") || (method == "test" && status.Status != "ready") {
		writeJSONCoded(w, http.StatusConflict, CodeResearchUnconfigured, "document processing is set on this desk's local gateway, which is not in use or not running; nothing was sent")
		return
	}
	available := status.Status == "ready" && status.Gateway != nil
	running := available && status.Gateway.documentProcessing
	restart := false
	if method == "configure" {
		var request struct {
			RestartWarned *bool `json:"restartWarned"`
			Config        struct {
				Mode string `json:"mode"`
			} `json:"config"`
		}
		if json.Unmarshal(obj["restartWarned"], &request.RestartWarned) != nil || request.RestartWarned == nil ||
			json.Unmarshal(obj["config"], &request.Config) != nil || (request.Config.Mode != "off" && request.Config.Mode != "auto") {
			writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "invalid document processing request; nothing was sent")
			return
		}
		restart = available && (request.Config.Mode == "auto") != running
		if restart != *request.RestartWarned {
			writeJSONCoded(w, http.StatusConflict, "processing-restart-changed", "The local gateway state changed. Review the settings and restart warning before saving again.")
			return
		}
		// This member belongs to Desk, not the companion's settings contract.
		params := map[string]json.RawMessage{"ifMatch": obj["ifMatch"], "config": obj["config"]}
		body, _ = json.Marshal(params)
	}
	companion := s.connectionCompanion("document-processing", true)
	if companion == nil {
		writeJSONCoded(w, http.StatusServiceUnavailable, CodeBadRequest, "document processing service unavailable; nothing was sent")
		return
	}
	timeout := processingControlTimeout
	if method == "test" {
		timeout = processingTestTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	out, err := companion.call(ctx, s.localGateway.bundle, filepath.Join(s.assistant.dir, "gateway-connections"), method, body, "document-processing", false)
	if errors.Is(err, errCompanionRequestTooLarge) {
		writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "document processing request too large; nothing was sent")
		return
	}
	if err != nil {
		writeJSONCoded(w, http.StatusServiceUnavailable, CodeBadRequest, "document processing service unavailable")
		return
	}
	answer, err := processingRelayed(method, out, processingSecrets(method, body))
	if err != nil {
		s.log.Printf("desk: document processing %s answer refused", method)
		if method == "test" {
			writeJSONCoded(w, http.StatusBadGateway, "processing-preview-unavailable", "The test preview could not be shown.")
			return
		}
		writeJSONCoded(w, http.StatusBadGateway, CodeBadRequest, "the gateway's answer could not be shown; if this was a save, reload the settings to see what was saved")
		return
	}
	restarted := false
	if _, ok := answer.Result.(processingStatus); ok && method == "configure" {
		// The plan follows the settings only when the gateway starts, so a
		// save that turns processing on or off restarts it.
		if restart {
			s.localGateway.restart()
			restarted = true
		}
	}
	if restarted {
		status = s.localGatewayStatus(raw)
	}
	answer.LocalGateway = processingGatewayNow(status, restarted)
	answer.LocalGateway.Problem = s.redactionFor().text(answer.LocalGateway.Problem)
	if _, ok := answer.Result.(processingStatus); ok && method == "configure" && !available {
		answer.LocalGateway.AppliesWhenStarted = true
	}
	s.log.Printf("desk: document processing %s answered", method)
	writeJSON(w, http.StatusOK, answer)
}

// processingGatewayNow is what Desk says of its local gateway after a request.
func processingGatewayNow(status *LocalGatewayStatus, restarted bool) processingGateway {
	if status == nil || status.Status != "ready" || status.Gateway == nil {
		said := processingGateway{Status: "unavailable", Restarted: restarted}
		if status != nil {
			said.Problem = status.Problem
		}
		return said
	}
	processing := status.Gateway.documentProcessing
	return processingGateway{Status: "ready", DocumentProcessing: &processing, Restarted: restarted}
}

// processingRelayed rebuilds the companion's answer from the members Desk
// names, so that nothing else it holds reaches the page: a refusal by its
// word from a closed list, a status without credentials, a test's preview
// without the records' messages. secrets are the credentials the request
// carried; an answer that holds one is refused whole.
func processingRelayed(method string, out json.RawMessage, secrets [][]byte) (processingAnswer, error) {
	invalid := errors.New("invalid document processing answer")
	var refusal struct {
		Error *string `json:"error"`
	}
	if json.Unmarshal(out, &refusal) == nil && refusal.Error != nil {
		word := *refusal.Error
		if !processingErrors[word] {
			word = "processing-failed"
		}
		return processingAnswer{Error: word}, nil
	}
	var result any
	switch method {
	case "status", "configure":
		var settings processingStatus
		if json.Unmarshal(out, &settings) != nil || settings.Version != 1 || (settings.Mode != "off" && settings.Mode != "auto") ||
			!processingDigest.MatchString(settings.SHA256) || settings.State != "ready" || len(settings.Connections) > 16 ||
			settings.TimeoutSeconds < 0 || settings.TimeoutSeconds > 120 {
			return processingAnswer{}, invalid
		}
		if settings.Connections == nil {
			settings.Connections = []processingConnection{}
		}
		result = settings
	case "test":
		var test processingTest
		if json.Unmarshal(out, &test) != nil || !processingWord.MatchString(test.Processing.Status) || !processingWord.MatchString(test.Extraction) ||
			test.PageCount < 0 || len(test.Pages) > 8 || len(test.Processing.Errors) > 64 {
			return processingAnswer{}, invalid
		}
		for _, e := range test.Processing.Errors {
			if !processingWord.MatchString(e.Code) {
				return processingAnswer{}, invalid
			}
		}
		for _, page := range test.Pages {
			if !processingWord.MatchString(page.Status) || !processingWord.MatchString(page.Extraction) || utf8.RuneCountInString(page.Text) > 400 {
				return processingAnswer{}, invalid
			}
		}
		if test.Processing.Errors == nil {
			test.Processing.Errors = []processingTestError{}
		}
		if test.Pages == nil {
			test.Pages = []processingTestPage{}
		}
		result = test
	default:
		return processingAnswer{}, invalid
	}
	shown, err := json.Marshal(result)
	if err != nil {
		return processingAnswer{}, invalid
	}
	for _, secret := range secrets {
		if bytes.Contains(shown, secret) {
			return processingAnswer{}, errors.New("a document processing answer held a credential")
		}
	}
	return processingAnswer{Result: result}, nil
}

// processingSecrets are the credentials a configure request carries, each as
// it is written in an answer Desk encodes, and as typed: the whole credential,
// and of one written as a JSON object (an AWS key pair, a Google service
// account) the members that are secret. Shorter than eight bytes, a value is
// left out, as one an answer may hold by chance.
func processingSecrets(method string, body []byte) [][]byte {
	if method != "configure" {
		return nil
	}
	var request struct {
		Config struct {
			Connections []struct {
				Credential string `json:"credential"`
			} `json:"connections"`
		} `json:"config"`
	}
	if json.Unmarshal(body, &request) != nil {
		return nil
	}
	var secrets [][]byte
	add := func(value string) {
		if len(value) < 8 {
			return
		}
		secrets = append(secrets, []byte(value))
		if encoded, err := json.Marshal(value); err == nil {
			secrets = append(secrets, encoded[1:len(encoded)-1])
		}
	}
	for _, connection := range request.Config.Connections {
		add(connection.Credential)
		var parts map[string]any
		if json.Unmarshal([]byte(connection.Credential), &parts) != nil {
			continue
		}
		for _, name := range []string{"accessKeyId", "secretAccessKey", "sessionToken", "private_key", "private_key_id"} {
			if value, ok := parts[name].(string); ok {
				add(value)
			}
		}
	}
	return secrets
}
