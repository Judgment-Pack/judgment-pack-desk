package desk

// The model relay: the HTTP sibling of the WebSocket relay in `relay.go`.
//
// # What it is for
//
// The page runs the assistant's loop and the page must never hold the
// credential. Those two sentences are only compatible if something between the
// page and the endpoint carries the key, and that something has to be here:
// the key is on this machine, is never returned by any endpoint, and never
// reaches the browser. So a page-side engine points its provider client at
//
//	baseURL = <this desk's origin>/api/assistant/relay/v1
//
// and sends the model traffic with no credential at all. This route strips
// whatever the page sent, injects the configured key on the configured wire
// protocol, and forwards everything else verbatim.
//
// # Why this is a route in a chassis that has no per-feature endpoints
//
// The README's sentence — "no per-feature endpoints and parses none of the
// traffic it carries" — is amended rather than quietly broken. This is a
// per-feature **route**, and it still parses none of the traffic: no body is
// read, no model name is inspected, no request is rewritten, retried or
// cached. What it adds to a request is one header. What it takes away is every
// header that could be a credential the page had no business holding.
//
// The same-origin arrangement is the second reason it exists. A page that
// called an arbitrary endpoint directly would need that endpoint to answer
// CORS, and an ordinary bring-your-own endpoint answers none — so the
// configuration measured in the bake-off is the only one a browser can
// actually run.
//
// # What is deliberately not here
//
// No retry: a retried model request is a second charge on somebody's account
// for an answer they were never shown. No caching: nothing here understands
// what it carries well enough to know what may be reused. No request
// rewriting: the page's `anthropic-version`, `content-type` and `accept`
// arrive at the endpoint exactly as written, because a relay that improved one
// of them would be a relay that has an opinion about a protocol it claims not
// to read. No model-name inspection, for the same reason `kind` is a protocol
// and never a vendor.
//
// # The destination cannot come from the page
//
// It comes from `configuredEndpoint`, the whole-file decode `deskfile.go`
// shares with the browser, exactly as the probe's destination does. The page
// chooses a **path suffix** and nothing else, that suffix is held to a
// character class with no dot segments and no percent-encoding in it, and it is
// appended to the configured URL's escaped path. Anything holding the session
// token can therefore ask this desk to call one endpoint — the one on this
// machine's `desk.json` — and can neither name a host nor walk out of the path
// space that endpoint documents.

import (
	"context"
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
	// relayPrefix is the mount point, and the `v1` in it belongs to **this**
	// route rather than to any endpoint. A provider client configured with it
	// as its base URL appends whatever its own protocol appends — an
	// OpenAI-compatible one writes `/chat/completions`, an Anthropic one
	// writes `/v1/messages` — and each of those lands, unchanged, after the
	// configured base. That is why one mount point serves both protocols
	// without this file knowing which is which.
	relayPrefix = "/api/assistant/relay/v1/"

	// maxRelayBody bounds one relayed request body.
	//
	// Eight mebibytes, because an authoring turn is not small: a whole schema,
	// several examples and a draft ride in one request, and a bound that
	// refused those would be a bound that refuses the feature. It is a refusal
	// and never a truncation — a request body cut in half is a request the
	// endpoint answers about a document nobody wrote.
	maxRelayBody = 8 << 20

	// maxRelaySuffix bounds the path the page may ask for. Every documented
	// path on either protocol is a few dozen bytes; 256 is far past all of
	// them and short enough that a suffix is never a payload.
	maxRelaySuffix = 256

	// maxRelayInFlight is how many relayed requests this desk will carry at
	// once.
	//
	// **A bound, and deliberately not a queue.** A queue would turn a page
	// that fires a hundred requests into a desk holding a hundred sockets and
	// a hundred deadlines, and would report the wait as latency. Four is more
	// than one authoring session's own parallelism — a loop is a loop, plus a
	// critic — and past it the answer is an immediate, named refusal that a
	// caller can act on.
	maxRelayInFlight = 4
)

// The two deadlines every relayed request is held to.
//
// Vars rather than consts for the reason `probeTimeout` is one: a test
// shortens them so that the bound can be shown to *apply*, against an upstream
// that never finishes, in a suite that does. Nothing else writes them, and a
// test asserts their defaults.
//
// **Bounded in time and not in bytes.** A model answer is a stream of unknown
// length and cutting it at a byte count would truncate an answer mid-sentence;
// what is actually pathological is a stream that never ends or never moves. So
// there are two: `relayDeadline` bounds one whole relayed request, and
// `relayIdle` bounds the gap between two writes from the endpoint.
var (
	relayDeadline = 10 * time.Minute
	relayIdle     = 2 * time.Minute
)

// relayTransport is the transport every relayed request goes out on.
//
// Nil is `http.DefaultTransport` — TLS verification and all, the same policy
// the probe uses. It is a field rather than an omission so that a test can
// count what actually leaves this process: "no outbound request was made" is a
// claim about the transport, and a stub server nobody was pointed at cannot
// establish it.
//
// **Redirects are not followed**, and there is nothing here that arranges
// that: a `RoundTripper` does not follow them at all, so a 3xx is handed back
// to the page as the answer it is. That is the same decision `probeClient`
// makes with `ErrUseLastResponse`, reached for free — Go strips `Authorization`
// on a cross-host redirect and knows nothing about `x-api-key`, so a followed
// redirect could walk the injected credential to a host nobody configured.
var relayTransport http.RoundTripper

// inboundCredentialHeaders are deleted from every relayed request **by name**,
// before anything is injected.
//
// By name and not by value: a page that holds no key cannot be trusted to have
// sent no header, and matching on what a header contains would be this file
// deciding what a credential looks like. `Api-Key` is Azure's spelling,
// `X-Goog-Api-Key` is Google's, `Proxy-Authorization` is the one a proxy in
// front of the endpoint would read, and `Cookie` is here because a cookie is a
// credential whatever it is called — the page's own session token would
// otherwise travel to somebody else's endpoint.
var inboundCredentialHeaders = []string{
	"Authorization", "X-Api-Key", "Api-Key", "X-Goog-Api-Key", "Cookie", "Proxy-Authorization",
}

// credentialHeader is the one header each wire protocol presents a key in.
//
// **One table, read by both callers.** The probe attaches the credential the
// same way, and a second table here is how the two would come to disagree
// about what an `anthropic` endpoint is sent. `ok` is false for a kind nothing
// defines, which `decodeDeskFile` refuses by name long before either caller
// reaches this.
func credentialHeader(kind, key string) (name, value string, ok bool) {
	switch kind {
	case "openai-compatible":
		return "Authorization", "Bearer " + key, true
	case "anthropic":
		return "x-api-key", key, true
	default:
		return "", "", false
	}
}

// relaySuffixProblem is the whole of what the page may ask for after the mount
// point, and it is a refusal rather than a repair.
//
// The rule is one or more segments of `[A-Za-z0-9._-]`, which is every path
// either protocol documents and nothing else. What it excludes is the point:
//
//   - **No percent sign**, so the escaped and unescaped forms of an accepted
//     suffix are the same string and there is no second reading of it to
//     disagree about. `%2e%2e%2f` is a dot segment written in a costume.
//   - **No dot segment**, so the page cannot climb out of the path space the
//     configured endpoint documents. A relay that forwarded `../../admin`
//     would be a relay that lets whoever holds the session token point the
//     stored credential at a resource nobody configured.
//   - **No empty segment**, because `//` means different things to different
//     servers and the desk should not be the one choosing.
//   - **No backslash**, which some servers read as a separator and this one
//     therefore never sends.
//   - **A bound**, because a suffix is an address and not a payload.
func relaySuffixProblem(suffix string) string {
	if suffix == "" {
		return "a relayed request must name at least one path segment after " +
			strings.TrimSuffix(relayPrefix, "/")
	}
	if len(suffix) > maxRelaySuffix {
		return fmt.Sprintf("a relayed path is at most %d bytes; this one is %d",
			maxRelaySuffix, len(suffix))
	}
	for _, segment := range strings.Split(suffix, "/") {
		if segment == "" {
			return "a relayed path may not contain an empty segment"
		}
		if segment == "." || segment == ".." {
			return `a relayed path may not contain a "." or ".." segment`
		}
		for _, r := range segment {
			if !relayPathRune(r) {
				return "a relayed path may contain only letters, digits, \".\", \"_\" and \"-\" " +
					"in each segment; a backslash, a percent sign or a space is refused rather " +
					"than encoded"
			}
		}
	}
	return ""
}

func relayPathRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.' || r == '_' || r == '-':
		return true
	default:
		return false
	}
}

// withoutSessionToken removes this chassis' own credential from a query before
// it is forwarded.
//
// **The relay's second credential, and the one that is easy to miss.** The
// session token travels as `?token=` on every route this chassis serves, this
// one included — so the query a page sends here *always* carries it, and
// forwarding the query "as-is" would present the desk's own credential to
// somebody else's endpoint on every single relayed request. It is stripped by
// **name**, and every parameter of that name, for the same reason the
// credential headers are: a rule about what a parameter contains is a rule
// that fails the first time a value is spelled differently.
//
// Everything else survives byte for byte and in order. Re-encoding through
// `url.Values` would sort the parameters and normalise their escaping, which is
// a change to somebody else's routing that nobody asked for.
func withoutSessionToken(raw string) string {
	if raw == "" {
		return ""
	}
	parameters := strings.Split(raw, "&")
	kept := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		if name, _, _ := strings.Cut(parameter, "="); name == "token" {
			continue
		}
		kept = append(kept, parameter)
	}
	return strings.Join(kept, "&")
}

// relayTarget is the address one relayed request is sent to.
//
// The suffix goes on the configured URL's **escaped path**, through
// `appendPath`, for the reason that function gives at length: writing the
// decoded path alone re-encodes `%2F` into a separator and turns one
// configured segment into two, which is a different resource with the
// credential attached.
//
// **Two query strings can meet here and both travel.** The configured one is
// the endpoint's own routing — some gateways route on one — and the page's is
// this request's, minus the session token this chassis put there. Dropping
// either would be the desk deciding something about an endpoint it does not
// read: the configured query first, then the page's, joined the way a query is
// joined.
func relayTarget(base, suffix, pageQuery string) (*url.URL, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	appendPath(parsed, "/"+suffix)
	if pageQuery != "" {
		if parsed.RawQuery == "" {
			parsed.RawQuery = pageQuery
		} else {
			parsed.RawQuery += "&" + pageQuery
		}
	}
	return parsed, nil
}

// handleModelRelay carries one model request to the configured endpoint.
//
// The refusals come first and each names which state it is: a request this
// desk will not forward, a desk with nowhere safe to keep a key, no endpoint
// configured — a refused file included — no key stored, and a desk already
// carrying as many requests as it will. **In every one of them no outbound
// request is made at all.**
func (s *Server) handleModelRelay(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	// The suffix is a property of the request alone, so it is decided before
	// anything is read off this machine. A request that was never going to be
	// forwarded should not cause a key to be opened.
	suffix := strings.TrimPrefix(r.URL.EscapedPath(), relayPrefix)
	if reason := relaySuffixProblem(suffix); reason != "" {
		writeJSONCoded(w, http.StatusBadRequest, CodeAssistantRelayPath, reason)
		return
	}
	// A declared length past the bound is refused before a byte is read. A
	// body with no declared length is bounded below, at the reader.
	if r.ContentLength > maxRelayBody {
		writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge,
			fmt.Sprintf("a relayed request body is at most %d bytes; nothing was sent",
				maxRelayBody))
		return
	}
	if s.refuseUnusableStore(w) {
		return
	}
	endpoint, err := s.configuredEndpoint()
	if err != nil {
		writeJSONError(w, statusForRefusal(err), err)
		return
	}
	key, err := s.assistant.readKey()
	if err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal,
			fmt.Sprintf("the assistant key could not be read: %v", err))
		return
	}
	if key == "" {
		// The same state the probe names, and the same repair: store a key on
		// Admin. A second code for one state would be two answers to one
		// question.
		writeJSONCoded(w, http.StatusConflict, CodeAssistantNoKey,
			"no key is stored on this machine, so there is nothing to present to the endpoint")
		return
	}
	name, value, ok := credentialHeader(endpoint.kind, key)
	if !ok {
		// Unreachable: `decodeDeskFile` refuses every other kind by name.
		writeJSONCoded(w, http.StatusConflict, CodeAssistantUnconfigured,
			"no relay is defined for that endpoint's wire protocol")
		return
	}
	target, err := relayTarget(endpoint.url, suffix, withoutSessionToken(r.URL.RawQuery))
	if err != nil {
		writeJSONCoded(w, http.StatusConflict, CodeAssistantUnconfigured,
			"the configured endpoint is not an address a request can be sent to")
		return
	}

	// A bound, not a queue: taken without waiting, and refused where there is
	// nothing to take.
	select {
	case s.relaySlots <- struct{}{}:
		defer func() { <-s.relaySlots }()
	default:
		writeJSONCoded(w, http.StatusServiceUnavailable, CodeAssistantRelayBusy,
			fmt.Sprintf("this desk is already carrying %d requests to the endpoint; "+
				"nothing was sent", maxRelayInFlight))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), relayDeadline)
	defer cancel()
	r = r.WithContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, maxRelayBody)

	status := 0
	proxy := &httputil.ReverseProxy{
		// `Rewrite` rather than `Director`, and the difference is a header
		// nobody asked for: the `Director` path appends `X-Forwarded-For`,
		// which would tell somebody else's endpoint about the loopback address
		// of a desk that promised to forward the page's headers and add one
		// credential. `Rewrite` adds nothing and strips the forwarding headers
		// a page might have set of its own.
		Rewrite: func(pr *httputil.ProxyRequest) {
			out := pr.Out
			out.URL = target
			// Emptied so the Host header is taken from the URL. The inbound
			// Host is this desk's loopback address and is none of the
			// endpoint's business.
			out.Host = ""
			for _, header := range inboundCredentialHeaders {
				out.Header.Del(header)
			}
			// Where the request was made from is the page's business and not
			// the endpoint's, and an Origin from a loopback desk would tell it
			// nothing true anyway.
			out.Header.Del("Origin")
			out.Header.Del("Referer")
			// The one thing this relay adds. Everything else the page wrote is
			// on its way out untouched.
			out.Header.Set(name, value)
		},
		Transport:     relayTransport,
		FlushInterval: -1,
		ModifyResponse: func(response *http.Response) error {
			status = response.StatusCode
			// **A relayed answer may not set a cookie on this desk's origin.**
			// The page and this chassis share an origin, so a `Set-Cookie` from
			// the endpoint would be stored against the desk and sent back to
			// the desk's own endpoints — an endpoint writing into the browser
			// state of the desk that called it.
			response.Header.Del("Set-Cookie")
			response.Body = boundedByIdle(response.Body, cancel)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge,
					fmt.Sprintf("a relayed request body is at most %d bytes", maxRelayBody))
				return
			}
			// **One word, from the probe's closed vocabulary, and never
			// anything the endpoint wrote.** This is a transport failure, so
			// there is no body to quote — and the rule that there is nothing to
			// quote is the rule worth keeping rather than one that holds
			// because today's error happens to be empty.
			writeJSONCoded(w, http.StatusBadGateway, CodeAssistantRelayUpstream,
				"the configured endpoint could not be reached: "+transportDiagnostic(err))
		},
	}
	proxy.ServeHTTP(w, r)
	// **Scheme and host only, and no path at all.** The same rule the probe's
	// line follows and for the same reason: a configured URL may carry a query
	// string, and a query string is a place people put credentials. The suffix
	// is not written down either — it is the page's, and a log is not the place
	// to reconstruct a session from.
	s.log.Printf("desk: assistant relay %s answered %d", loggableOrigin(endpoint.url), status)
}

// idleBody cancels a relayed request that has stopped moving.
//
// The overall deadline cannot do this on its own: a stream that writes one byte
// an hour is inside a ten-minute-per-request bound only until it is not, and
// what the page actually experiences is an answer that never arrives. So the
// gap between two writes is bounded as well, and the timer is reset by the only
// thing that means the endpoint is still talking — a read that returned.
type idleBody struct {
	inner io.ReadCloser
	timer *time.Timer
}

func boundedByIdle(inner io.ReadCloser, cancel context.CancelFunc) io.ReadCloser {
	return &idleBody{inner: inner, timer: time.AfterFunc(relayIdle, cancel)}
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	if err == nil {
		b.timer.Reset(relayIdle)
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	return b.inner.Close()
}
