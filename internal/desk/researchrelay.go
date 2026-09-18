package desk

// The research relay: the second route that carries traffic this chassis does
// not read.
//
// The page runs the assistant's research — a search, a page read — through a
// judgment-pack gateway: an `/acquire` per source call, a `/seal` when the
// run's session closes, and a `/registry` fetch so the page can verify what it
// was handed under the key it pinned (gateway SPEC.md §5a). A gateway binds a
// loopback address and answers no CORS, so a page cannot call it directly, and
// this route exists for the same reason the model relay does: it is the one
// place a request to something outside this desk can be made on the page's
// behalf.
//
// **It carries no provider credential, in either direction.** Selected Drive
// files carry short-lived grants, constrained below to managed local processing.
// The provider's key —
// Tavily's, Jina's — is in the gateway's own credentials file, read by the
// adapter the gateway spawns and by nothing on this machine; the gateway
// itself is reached as the desk-level file names it, with no token, because a
// gateway configured with an identity issuer is not what this route speaks to
// (that is a documented bound, not an omission). What travels is the page's
// JSON body on a fixed path, and what comes back is the gateway's answer with
// its own headers filtered to the two the page reads.
//
// **Three paths and no more.** `acquire` and `seal` by POST, `registry` by
// GET, each the gateway's own spelling under the configured base. The suffix
// is a closed list rather than a character class: the gateway has exactly six
// routes and this desk relays three of them by name, so `/verify` — the
// gateway grading itself, which §5a.3 says is not evidence — and `/act` — a
// write nobody at this desk authorised — cannot be asked for through here at
// all. A query is refused outright; the page has no query to send.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const (
	// researchPrefix is the relay's mount point.
	researchPrefix = "/api/research/gateway/"
	// maxResearchBody bounds a relayed request body: the gateway's own bound
	// on what it reads, so a body past it is refused here rather than there.
	maxResearchBody = 1 << 20
	// maxResearchInFlight bounds how many research requests are in flight at
	// once. Two: an authoring run acquires one source at a time, and a second
	// tab's run is the other.
	maxResearchInFlight = 2
)

var (
	// researchDeadline is the overall bound on one relayed request. A gateway
	// gives its source thirty seconds and then canonicalizes and signs what
	// came back; a reader service's answer to a long PDF is megabytes. Vars,
	// not consts, so a test can show the bounds apply.
	researchDeadline = 90 * time.Second
	// researchIdle is the bound between two bytes of the answer, and on the
	// wait for its first one.
	researchIdle = 60 * time.Second
)

// researchRoutes is the closed list: the suffix the page names, and the one
// method it may name it with.
var researchRoutes = map[string]string{
	"acquire":  http.MethodPost,
	"seal":     http.MethodPost,
	"registry": http.MethodGet,
}

// researchRequestHeaders is the closed set of request headers that travel to
// the gateway. An allow-list, on the model relay's reasoning: `Authorization`,
// `Cookie`, `Origin` and every credential header anybody invents are absent
// because they were never added.
var researchRequestHeaders = map[string]bool{
	"Accept":         true,
	"Content-Type":   true,
	"Content-Length": true,
}

// researchAnswerHeaders is what the page is handed back of the gateway's
// answer: enough to read a JSON body, and nothing that would redirect the
// page, set a cookie against this desk's origin, or wear this chassis' own
// refusal mark.
var researchAnswerHeaders = map[string]bool{
	"Content-Type":   true,
	"Content-Length": true,
}

// configuredResearch decodes the whole desk-level file and answers the gateway
// only where nothing at all in that file was refused — the same one gate
// `configuredEndpoint` applies, and for the same reason: a file the page
// visibly rejected authorises no request.
func (s *Server) configuredResearch() (researchGateway, error) {
	var zero researchGateway
	if !s.assistant.usable() {
		return zero, withCode(CodeAssistantUnusableStore, s.assistant.problem)
	}
	path := s.deskConfigPath()
	present, data, err := s.readDeskFile()
	if err != nil {
		return zero, withCode(CodeResearchUnconfigured, fmt.Errorf(
			"no research gateway could be read: %s could not be read: %v", path, err))
	}
	if !present && s.localGateway != nil {
		return s.localResearch(nil)
	}
	if !present {
		return zero, withCode(CodeResearchUnconfigured, fmt.Errorf(
			"no research gateway is configured: there is no %s", path))
	}
	if !validUTF8(data) {
		return zero, withCode(CodeResearchUnconfigured, fmt.Errorf(
			"no research gateway is configured: %s is not UTF-8 text", path))
	}
	decoded := decodeDeskFile(data)
	if decoded.refused() {
		return zero, withCode(CodeResearchUnconfigured, fmt.Errorf(
			"%s was refused, so no research gateway in it is configured: %s",
			path, describeProblems(decoded.Problems)))
	}
	if decoded.Research == nil {
		return s.localResearch(nil)
	}
	if decoded.Research.gateway == nil {
		documents := decoded.Research.documents
		if documents == nil {
			var raw struct {
				Research struct {
					Documents json.RawMessage `json:"documents"`
				} `json:"research"`
			}
			_ = json.Unmarshal(data, &raw)
			if string(raw.Research.Documents) == "null" {
				documents = &documentSourceConfig{}
			}
		}
		return s.localResearch(documents)
	}
	gateway := *decoded.Research.gateway
	gateway.maxRequestBytes = maxResearchBody
	if decoded.Research.documents != nil && decoded.Research.documents.enabled {
		gateway.maxRequestBytes = decoded.Research.documents.maxRequestBytes
		gateway.maxFileBytes = decoded.Research.documents.maxFileBytes
	}
	return gateway, nil
}

// researchTarget is the gateway's own route under the configured base.
func researchTarget(base, suffix string) (*url.URL, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	appendPath(parsed, "/"+suffix)
	parsed.RawQuery, parsed.ForceQuery, parsed.Fragment = "", false, ""
	return parsed, nil
}

// handleResearchRelay carries one request to the configured gateway.
//
// The refusals come first and each names its state; **in every one of them no
// outbound request is made at all.**
func (s *Server) handleResearchRelay(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	suffix := strings.TrimPrefix(r.URL.EscapedPath(), researchPrefix)
	method, known := researchRoutes[suffix]
	if !known {
		writeJSONCoded(w, http.StatusBadRequest, CodeResearchRelayPath,
			"a research request names one of acquire, seal and registry, and nothing else; nothing was sent")
		return
	}
	if r.Method != method {
		writeJSONCoded(w, http.StatusBadRequest, CodeResearchRelayPath,
			fmt.Sprintf("%s is a %s route on the gateway; nothing was sent", suffix, method))
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		writeJSONCoded(w, http.StatusBadRequest, CodeResearchRelayPath,
			"a research request carries no query; nothing was sent")
		return
	}
	if s.refuseUnusableStore(w) {
		return
	}
	gateway, err := s.configuredResearch()
	if err != nil {
		writeJSONError(w, statusForRefusal(err), err)
		return
	}
	// A Drive grant must stay at the managed local gateway, even if the page
	// has stale settings. This marker constrains the resolved target; it cannot
	// select a destination and is removed by the outbound header allow-list.
	if values, present := r.Header[http.CanonicalHeaderKey("X-JPack-Local-Documents")]; present {
		if len(values) != 1 || values[0] != "1" || !gateway.managedLocal || gateway.maxFileBytes == 0 {
			writeJSONCoded(w, http.StatusConflict, CodeResearchUnconfigured,
				"local document processing is no longer available; nothing was sent")
			return
		}
	}
	// Larger uploads are opt-in at the personal configuration boundary. Seal
	// and registry requests retain the original small envelope limit.
	bodyLimit := int64(maxResearchBody)
	if suffix == "acquire" {
		bodyLimit = gateway.maxRequestBytes
	}
	if r.ContentLength > bodyLimit {
		writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge,
			fmt.Sprintf("a research request body is at most %d bytes; nothing was sent", bodyLimit))
		return
	}
	target, err := researchTarget(gateway.url, suffix)
	if err != nil {
		writeJSONCoded(w, http.StatusConflict, CodeResearchUnconfigured,
			"the configured gateway is not an address a request can be sent to")
		return
	}
	work, err := s.privateDataFileLock(".data-work.lock", false)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer work.Close()

	select {
	case s.researchSlots <- struct{}{}:
		defer func() { <-s.researchSlots }()
	default:
		writeJSONCoded(w, http.StatusServiceUnavailable, CodeResearchRelayBusy,
			fmt.Sprintf("this desk is already carrying %d requests to the gateway; nothing was sent",
				maxResearchInFlight))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), researchDeadline)
	defer cancel()
	r = r.WithContext(ctx)
	deadline := time.Now().Add(researchDeadline)
	controller := http.NewResponseController(w)

	// The whole body first, bounded, so a request this desk refuses is one the
	// gateway never saw any of — the model relay's rule, for its reason.
	_ = controller.SetReadDeadline(deadline)
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bodyLimit))
	_ = controller.SetReadDeadline(time.Time{})
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge,
				fmt.Sprintf("a research request body is at most %d bytes; nothing was sent", bodyLimit))
			return
		}
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest,
			"the request body could not be read, and nothing was sent")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.TransferEncoding = nil

	status := 0
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			out := pr.Out
			out.URL = target
			out.Host = ""
			carried := make(http.Header, len(researchRequestHeaders))
			for header, values := range out.Header {
				if researchRequestHeaders[http.CanonicalHeaderKey(header)] {
					carried[header] = values
				}
			}
			out.Header = carried
		},
		Transport:     beforeTheFirstByte{inner: relayTransport, cancel: cancel, idle: researchIdle},
		FlushInterval: -1,
		ModifyResponse: func(response *http.Response) error {
			status = response.StatusCode
			// **A new header set for the answer too.** The page reads the body
			// and its type; a gateway's `Set-Cookie` would be stored against
			// this desk's origin, a `Location` would make the page's fetch
			// repeat the request elsewhere, and the refusal mark is this
			// chassis' alone.
			kept := make(http.Header, len(researchAnswerHeaders))
			for header, values := range response.Header {
				if researchAnswerHeaders[http.CanonicalHeaderKey(header)] {
					kept[header] = values
				}
			}
			response.Header = kept
			response.Trailer = nil
			response.Body = boundedByIdleFor(response.Body, cancel, researchIdle)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge,
					fmt.Sprintf("a research request body is at most %d bytes", bodyLimit))
				return
			}
			// One word from the probe's closed vocabulary, never anything the
			// gateway wrote: a transport failure has no body to quote.
			writeJSONCoded(w, http.StatusBadGateway, CodeResearchRelayUpstream,
				"the configured gateway could not be reached: "+transportDiagnostic(err))
		},
	}
	proxy.ServeHTTP(&deadlineWriter{
		ResponseWriter: w,
		controller:     controller,
		until:          deadline,
		idle:           researchIdle,
	}, r)
	withoutTrailers(w.Header())
	s.log.Printf("desk: research relay %s %s answered %d", suffix, loggableOrigin(gateway.url), status)
}
