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
// appended to the configured URL's escaped path. Anything holding a session on
// this desk can therefore ask this desk to call one endpoint — the one on this
// machine's `desk.json` — and can neither name a host nor walk out of the path
// space that endpoint documents.

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
	"strconv"
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

	// maxListingBody bounds the one relayed answer this desk reads.
	//
	// A megabyte, which is two orders of magnitude past the largest model
	// listing any of the three protocols serves and small enough that reading
	// it whole costs nothing. A body past it is **refused**, not truncated and
	// not forwarded unread: this desk cannot say whether the part it did not
	// read carries the credential, and an answer it cannot say that about is
	// one it will not put on the page.
	maxListingBody = 1 << 20
)

// relayListingSuffix is the path each protocol's model listing is at, relative
// to the configured base — and the whole of what this desk treats as a listing.
//
// **Mirrored on the page** (`LISTING_SUFFIX` in `web/src/assistant/modelListing.ts`)
// and held equal to it by a test that reads this declaration, exactly as the
// method list and the query pair are. A suffix on one side and not the other is
// either an answer the page renders unscanned or a scan of something nobody
// asked for.
var relayListingSuffix = map[string]string{
	"openai-compatible": "models",
	"anthropic":         "v1/models",
	"gemini":            "v1beta/models",
}

// The three ways a listing is refused, carried out of `ModifyResponse` — which
// is the one place a relayed answer can still be refused before a byte of it
// has reached the page.
var (
	errListingCarriesKey = errors.New("listing carries the key")
	errListingTooLarge   = errors.New("listing past the bound")
	errListingNotJSON    = errors.New("listing is not one JSON value")
)

// listingProblem is the scan, and what it refuses is a **class** rather than a
// match.
//
// **A listing is forwarded only if it is exactly one JSON value, decoded end to
// end, with nothing behind it, and no string in it is the key.** Anything else
// is refused. That is the repair for a rule that read the other way round: a
// decode error used to fall back to comparing the raw bytes, so a body that was
// not JSON at all — `not a listing`, an empty answer, a truncated one, one with
// a second value behind it, malformed JSON with an escaped credential in the
// half the decoder never reached — was **forwarded** whenever the literal key
// bytes happened to be absent. A scan that cannot read a body cannot clear it,
// and clearing it anyway is the whole of what went wrong.
//
// It costs nothing real: the three protocols' listings are JSON documents, and
// an endpoint that answers something else at its own listing path has not
// answered a listing. The page says `NOT_A_LISTING` for the same body today,
// so what changes is where that is decided and whether the bytes travel.
//
// **Every JSON string in the body**, keys and values alike, compared to the
// configured key: equal to it, or containing it where the key is long enough
// to be looked for inside a longer string. Numbers too, by the text they were
// written as — a key of digits is a key, and `UseNumber` keeps that text
// rather than a float somebody would have to render back. That is the
// answer-header rule applied to the one body this desk reads, on the same
// twelve-byte floor and for the same reason — below it a key is a substring of
// ordinary text, and a filter that deletes an answer to protect three
// characters is a worse answer than the three characters.
//
// **`UseNumber` is also what makes the validity rule honest.** Without it the
// decoder converts every number to a `float64` and *fails* on one outside that
// range, so `1e1000` — a syntactically valid JSON document — was refused by a
// rule that says every shape of exactly one valid value is carried. Whether a
// number is representable in Go is not a fact about the endpoint's listing.
//
// **The strings are the decoded ones**, because a key written into JSON with
// escapes is one string to a decoder and different bytes on the wire. What no
// comparison catches is a *derived* representation — base64, hex, half of it —
// and the README says so rather than implying a completeness no comparison has.
func listingProblem(body []byte, key string) error {
	long := key != "" && len(key) >= minFingerprintable
	carries := func(value string) bool {
		return key != "" && (value == key || (long && strings.Contains(value, key)))
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	// One value, and the decoder is what says where it ends. `Token` walks the
	// whole of it; `More` afterwards is what catches a second value behind it,
	// which is the shape every other reader on this desk refuses too.
	depth := 0
	values := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errListingNotJSON
		}
		switch typed := token.(type) {
		case json.Delim:
			if typed == '[' || typed == '{' {
				if depth == 0 {
					values++
				}
				depth++
			} else {
				depth--
			}
		case string:
			if carries(typed) {
				return errListingCarriesKey
			}
			if depth == 0 {
				values++
			}
		case json.Number:
			// The digits as they were written. A `json.Number` is not a
			// `string` to a type switch, so without this arm a key of digits
			// would travel through the one member the scan did not look at.
			if carries(string(typed)) {
				return errListingCarriesKey
			}
			if depth == 0 {
				values++
			}
		default:
			if depth == 0 {
				values++
			}
		}
	}
	if values != 1 || depth != 0 {
		return errListingNotJSON
	}
	return nil
}

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

// relayFinalWrite bounds the one write allowed after a request's overall
// deadline has passed: the refusal the handler is about to make.
//
// Five seconds, and a constant rather than a third dial. It is short enough
// that a slot cannot be held past the overall bound in any way that matters,
// and long enough that a page on a loopback socket will always have taken a
// two-hundred-byte envelope.
const relayFinalWrite = 5 * time.Second

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

// beforeTheFirstByte bounds the wait for the **upstream's response headers**,
// which is the half of the answer no other bound here reached.
//
// `boundedByIdle` is installed in `ModifyResponse`, and `ModifyResponse` runs
// only once the transport has a response — so an endpoint that accepted the
// request and then sent nothing at all, not a header and not a byte, was held
// by the ten-minute *overall* deadline rather than the two-minute idle one.
// Four of those exhausted every relay slot for ten minutes, against a README
// promising two.
//
// **A wrapper rather than `ResponseHeaderTimeout` on the shared transport**,
// for one reason: the bound is `relayIdle`, which is a var a test shortens so
// the bound can be shown to *apply*, and a field read off one long-lived
// transport cannot follow it without cloning the connection pool per request.
// The timer is armed before the round trip and stopped when it returns —
// which is the instant the headers are in hand, since the body streams
// afterwards — and firing it cancels the request's own context, so the
// refusal, the slot release and the log line are the ones every other
// transport failure takes.
type beforeTheFirstByte struct {
	inner  http.RoundTripper
	cancel context.CancelFunc
}

func (b beforeTheFirstByte) RoundTrip(r *http.Request) (*http.Response, error) {
	timer := time.AfterFunc(relayIdle, b.cancel)
	defer timer.Stop()
	inner := b.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	return inner.RoundTrip(r)
}

// relayedRequestHeaders is the closed set of request headers that travel to
// the endpoint. **Everything else is dropped.**
//
// **This was a denylist and the denylist was the defect.** "Every inbound
// credential is stripped" cannot be held by a list of names somebody thought
// of: `X-Auth-Token`, `X-Access-Token`, `X-Amz-Security-Token`,
// `Ocp-Apim-Subscription-Key` and whatever a gateway invents next all walked
// straight through it, and a test that populated its inputs *from* that same
// list could never have said so. An allow-list is the only shape in which the
// claim is structural: a credential header nobody has heard of does not travel
// because it is not on this list, and `Cookie` falls out without being named.
//
// What is on it is what the two protocols need to be spoken: the content and
// negotiation headers, the two `anthropic-*` headers that protocol requires,
// the beta headers both vendors document, and — through `relayedRequestPrefixes`
// — the `X-Stainless-*` telemetry the generated SDKs attach to every request.
// A page that needs a header this list does not carry is a change to this list,
// reviewed, rather than a header that arrives because nobody forbade it.
//
// `Origin` and `Referer` are absent rather than deleted, which is the point of
// the shape: there is no second rule to keep in step with this one.
var relayedRequestHeaders = []string{
	"Accept", "Accept-Encoding", "Accept-Language", "Content-Type", "Content-Length",
	"User-Agent", "Anthropic-Version", "Anthropic-Beta", "OpenAI-Beta", "OpenAI-Organization",
	"OpenAI-Project",
}

// relayedRequestPrefixes are the header families carried whole.
//
// One entry: the OpenAI and Anthropic SDKs are Stainless-generated and attach
// `X-Stainless-Lang`, `-Package-Version`, `-Runtime`, `-Retry-Count` and more
// to every request. Naming the family rather than the members is what keeps an
// SDK upgrade from being an outage; it carries no credential, and an endpoint
// that reads it reads what the client is.
var relayedRequestPrefixes = []string{"X-Stainless-"}

// relayedRequestHeader reports whether one request header travels.
func relayedRequestHeader(name string) bool {
	for _, allowed := range relayedRequestHeaders {
		if strings.EqualFold(name, allowed) {
			return true
		}
	}
	for _, prefix := range relayedRequestPrefixes {
		if len(name) > len(prefix) && strings.EqualFold(name[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

// reflectedCredentialHeaders are deleted from every relayed **answer**.
//
// **An endpoint can hand the key back.** The one this desk sends is the
// endpoint's own credential, and an endpoint that echoes what it was sent —
// a debug gateway, a misconfigured proxy, a hostile one — would otherwise put
// the machine-held key into the page, which is the single thing this route
// exists to prevent. So the answer's headers are filtered as well as the
// request's: these names, and any header whose value **is** the key.
//
// The limit is stated rather than glossed, and it is the limit chunk 1 already
// ruled on for the probe: a *derived* representation — base64, hex, half of it
// — is not detectable, and a body is not read at all. See the README.
var reflectedCredentialHeaders = []string{
	"Authorization", "Proxy-Authorization", "WWW-Authenticate", "Proxy-Authenticate",
	"X-Api-Key", "Api-Key", "X-Goog-Api-Key", "Set-Cookie",
}

// credentialHeader is the one header each wire protocol presents a key in.
//
// **One table, read by both callers.** The probe attaches the credential the
// same way, and a second table here is how the two would come to disagree
// about what an `anthropic` endpoint is sent. `ok` is false for a kind nothing
// defines, which `decodeDeskFile` refuses by name long before either caller
// reaches this.
// **Every entry is a header, and that is the rule rather than a coincidence.**
// Google's Gemini API documents `?key=` as an alternative to `x-goog-api-key`,
// and this desk does not take it: a credential in a URL is a credential in a
// log, in a `Referer`, in a proxy's access record and in the `loggableOrigin`
// line this route writes — and the configuration decoder already refuses a URL
// carrying userinfo for the same reason. The header is the only place a key
// goes.
func credentialHeader(kind, key string) (name, value string, ok bool) {
	switch kind {
	case "openai-compatible":
		return "Authorization", "Bearer " + key, true
	case "anthropic":
		return "x-api-key", key, true
	case "gemini":
		return "x-goog-api-key", key, true
	default:
		return "", "", false
	}
}

// appendQueryPair puts one raw query pair after whatever query is already
// there.
//
// **The configured query keeps its place and the added pair goes after it.**
// Shared by `relayTarget` and the probe's address builder so that the order
// has one implementation: a desk that put its own pair first in one place and
// last in the other would be sending two different requests to an endpoint
// that routes on the first parameter it reads.
func appendQueryPair(raw, pair string) string {
	if raw == "" {
		return pair
	}
	return raw + "&" + pair
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
//     would be a relay that lets whoever holds a session point the stored
//     credential at a resource nobody configured.
//   - **No empty segment**, because `//` means different things to different
//     servers and the desk should not be the one choosing.
//   - **No backslash**, which some servers read as a separator and this one
//     therefore never sends.
//   - **A bound**, because a suffix is an address and not a payload.
//
// **One closed exception, and it is a shape rather than a character.** The
// native Gemini wire addresses a method with a colon in the last segment —
// `models/gemini-2.5-pro:streamGenerateContent` — so a segment may be
// `<name>:<method>` where the method is one of `relayPathMethods` and the name
// either side of it is the same class as before. A colon anywhere else, a
// second colon, a method outside the list, or an empty name is refused with
// `assistant-relay-path` exactly as it was. The rule is about the **path** and
// is not gated on the configured kind: the kind decides the credential, and a
// relay that read one to decide the other would be two rules where there is
// one. In practice only the gemini wire writes such a path.
func relaySuffixProblem(suffix string) string {
	if suffix == "" {
		return "a relayed request must name at least one path segment after " +
			strings.TrimSuffix(relayPrefix, "/")
	}
	if len(suffix) > maxRelaySuffix {
		return fmt.Sprintf("a relayed path is at most %d bytes; this one is %d",
			maxRelaySuffix, len(suffix))
	}
	segments := strings.Split(suffix, "/")
	for index, segment := range segments {
		if segment == "" {
			return "a relayed path may not contain an empty segment"
		}
		if segment == "." || segment == ".." {
			return `a relayed path may not contain a "." or ".." segment`
		}
		// The one exception, taken at the **first** colon: `a:b:generateContent`
		// leaves `b:generateContent` as the method, which is on no list, and
		// `a:generateContent:x` leaves `generateContent:x`. So a second colon
		// refuses itself and there is no arithmetic to get wrong.
		//
		// **And only in the last segment.** Round 1 found `v1beta/a:countTokens/b`
		// accepted and forwarded with the credential: the rule was written per
		// segment and never asked where the segment was, which is wider than
		// the shape the wire actually uses. A method is a verb applied to the
		// resource the path names, so there is nothing after it — and a rule
		// that admits one in the middle admits a resource nobody documented
		// under a verb this desk agreed to.
		if name, method, found := strings.Cut(segment, ":"); found {
			if index != len(segments)-1 || name == "" ||
				!contains(relayPathMethods, method) {
				return "a colon in a relayed path may only introduce one of " +
					strings.Join(relayPathMethods, ", ") +
					", after a non-empty final segment"
			}
			segment = name
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

// relayPathMethods is the closed list of methods a colon may introduce.
//
// The three the native Gemini wire documents on a model resource, and no more.
// **Closed rather than "anything after a colon"**, because the segment before
// the colon is a resource and the part after it is a *verb*: an open list would
// let whoever holds a session ask the configured endpoint to do something
// nobody wrote down, with the stored credential attached. Adding one
// is a change to this list, reviewed, exactly as adding a request header is.
var relayPathMethods = []string{"generateContent", "streamGenerateContent", "countTokens"}

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

// relayQueryProblem is the whole of what a page may put in a relayed request's
// query, and the answer is **nothing of its own**.
//
// # Why this is a refusal and not a filter
//
// The page's query used to be forwarded with this chassis' own session token
// taken out of it, and that arrangement leaked the token three times, three
// different ways, to three reviewers:
//
//   - `?%74oken=…` — the guard read names with `url.Query`, which
//     percent-decodes; a strip comparing raw text did not.
//   - `?x=1;token=…&token=…` — Go rejects a pair containing `;`, so the guard
//     saw one `token` parameter; a server that still treats `;` as a separator
//     sees two.
//   - `?Token=…&token=…` — the guard's comparison was case-sensitive, and
//     ASP.NET Core's query parser folds case, so an upstream read `Token` as
//     `token`.
//
// Each fix was a better comparison, and each time the next parser disagreed
// somewhere else. **The class existed because the query carried a secret at
// all**: no comparison this desk can write is the comparison every parser
// downstream makes, and a rule that has to be right about all of them is a rule
// that will be wrong again.
//
// **That class is gone at its root, and this refusal is what keeps it gone.**
// Since the launch exchange, no secret rides on any query anywhere in this
// chassis: a browser presents the session id it holds and a script the launch
// secret, both on `Authorization: Bearer`, so there is no `token` parameter for
// a downstream parser to read differently. A `token=…` pair on a relayed
// request is therefore not a credential being carefully handled — it is a page
// sending something no part of this desk asks for, and it is refused by the
// same rule as any other pair.
//
// So the query is not filtered and nothing of the page's own travels: every
// raw pair is refused — any name, any case, any encoding, an empty name
// included. Refusing is the one rule every parser agrees on, because nothing is
// sent for them to disagree about.
//
// What reaches the endpoint is the configured URL's own query, which
// `appendPath` carries: the endpoint's routing, out of the file on this
// machine, exactly as before. The page chooses a **path suffix** and nothing
// else, and that sentence is now literally true.
//
// A literal `;` is refused by name as well, because a query two parsers read
// differently is one this desk would have an argument to make about. It has
// none to make now.
//
// # The one closed exception, and why it is a pair and not a filter
//
// The native Gemini wire asks for a server-sent-event stream with a **query**
// parameter — `?alt=sse` — and there is nowhere else to put it: it is not a
// header, and the configured URL cannot carry it because the same endpoint
// serves the unary call too. So exactly one pair is admitted, **byte for byte
// and at most once**: the literal seven bytes `alt=sse`. `alt=json`, `ALT=sse`,
// `%61lt=sse`, a second copy, anything with a value of its own — each is
// refused with `assistant-relay-path` and nothing is sent.
//
// That is a closed exception rather than a loosening, and it is the *only*
// thing this function admits. Byte equality against one fixed literal is the
// one comparison that has no second reading — there is nothing to decode, fold
// or split — and the pair carries no secret, so the three leaks above have no
// analogue here.
//
// **Whether the configured endpoint may carry it is decided elsewhere**, once
// the kind is known and before the key is opened: see `relayExtraQueryPair`
// and `handleModelRelay`. This function answers only what the request said,
// which is a property of the request alone and is settled before anything is
// read off this machine.
func relayQueryProblem(raw string) (extra, problem string) {
	if raw == "" {
		return "", ""
	}
	if strings.ContainsRune(raw, ';') {
		return "", "a relayed query may not contain a semicolon: it is a separator to some " +
			"servers and a value to others, and this desk will not send one it cannot " +
			"read the same way twice"
	}
	for _, parameter := range strings.Split(raw, "&") {
		name, _, _ := strings.Cut(parameter, "=")
		decoded, err := url.QueryUnescape(name)
		// The one exception, and every word of this condition is load-bearing:
		// the name must *decode* to the one this desk knows, the raw pair must
		// be that literal byte for byte (so an encoded spelling is not a second
		// reading of it), and there must not already be one (so
		// `alt=sse&alt=sse` is a query two parsers could count differently).
		if err == nil && decoded == relayStreamParameter &&
			parameter == relayStreamPair && extra == "" {
			extra = parameter
			continue
		}
		return "", "a relayed request carries no query of the page's own: nothing of it is " +
			"forwarded, because no comparison this desk can write is the one every server " +
			"downstream makes"
	}
	return extra, ""
}

// The one query pair a page may send, and the name inside it.
//
// A literal rather than a builder: what is admitted is these bytes, and a
// `name + "=" + value` would be an invitation to admit a second value later
// without noticing that the rule had changed shape.
const (
	relayStreamParameter = "alt"
	relayStreamPair      = "alt=sse"
)

// relayExtraQueryPair is the query pair each wire protocol admits from the
// page, or the empty string for one that admits none.
//
// **A table beside `credentialHeader`, and closed the same way.** The page's
// query is refused entirely for `openai-compatible` and `anthropic`: both
// protocols carry streaming in the request body, so a pair admitted for them
// would be a capability nothing asked for. Only the gemini wire needs one, and
// it needs precisely one.
func relayExtraQueryPair(kind string) string {
	switch kind {
	case "gemini":
		return relayStreamPair
	default:
		return ""
	}
}

// relayTarget is the address one relayed request is sent to.
//
// The suffix goes on the configured URL's **escaped path**, through
// `appendPath`, for the reason that function gives at length: writing the
// decoded path alone re-encodes `%2F` into a separator and turns one
// configured segment into two, which is a different resource with the
// credential attached.
//
// **The configured query travels, and after it at most one pair of the page's.**
// The configured one is the endpoint's own routing, out of the file on this
// machine — some gateways route on one — and `appendPath` carries it across
// unchanged, first and byte for byte. `extra` is the single literal
// `relayQueryProblem` admitted and `handleModelRelay` checked against the
// configured kind; everything else of the page's query was refused, and the
// request never reached here.
//
// `ForceQuery` is cleared with the pair appended, because a configured base
// written as `https://gw/v1?` would otherwise emit `?` and then `&alt=sse`,
// which is a query with an empty first pair in it — the kind of thing two
// parsers read differently, and the one thing this route will not send.
func relayTarget(base, suffix, extra string) (*url.URL, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	appendPath(parsed, "/"+suffix)
	if extra != "" {
		parsed.RawQuery = appendQueryPair(parsed.RawQuery, extra)
		parsed.ForceQuery = false
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
	// The query, on the same footing and decided in the same breath: a
	// property of the request alone, and the page's half of it is refused
	// rather than filtered. `extra` is the one closed exception it may have
	// admitted — a pair whose *shape* is settled here and whose *permission*
	// is settled below, once the configured kind is known. See
	// `relayQueryProblem`.
	extra, reason := relayQueryProblem(r.URL.RawQuery)
	if reason != "" {
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
	// **The other half of the query rule, and it is here because this is the
	// first line at which the kind is known.** The shape was settled before
	// anything was read off this machine; whether *this* endpoint's protocol
	// admits that pair is a per-kind allow-list, and it is checked before the
	// key is opened so that a request nobody was ever going to forward does
	// not cause a credential to be read. Nothing outbound happens either way.
	if extra != "" && extra != relayExtraQueryPair(endpoint.kind) {
		writeJSONCoded(w, http.StatusBadRequest, CodeAssistantRelayPath,
			fmt.Sprintf("a relayed request to a %q endpoint carries no query parameter at "+
				"all; nothing was sent", endpoint.kind))
		return
	}
	stored, err := s.assistant.readKey()
	if err != nil {
		s.refuseKeyRead(w, err)
		return
	}
	if !stored.present {
		// The same state the probe names, and the same repair: store a key on
		// Admin. A second code for one state would be two answers to one
		// question.
		writeJSONCoded(w, http.StatusConflict, CodeAssistantNoKey,
			"no key is stored on this machine, so there is nothing to present to the endpoint")
		return
	}
	// **The binding, and it is what keeps "the destination cannot come from
	// the page" true now that the page can write the configuration.** The
	// endpoint may be anything the file says; the credential goes only to the
	// destination it was entered for. A configuration naming another one is
	// refused here, before a socket is opened, and the repair is a person
	// entering the key again — which page code cannot do, because it has never
	// held it.
	if reason := bindingProblem(stored, endpoint); reason != "" {
		writeJSONCoded(w, http.StatusConflict, CodeAssistantKeyUnbound, reason)
		return
	}
	key := stored.key
	name, value, ok := credentialHeader(endpoint.kind, key)
	if !ok {
		// Unreachable: `decodeDeskFile` refuses every other kind by name.
		writeJSONCoded(w, http.StatusConflict, CodeAssistantUnconfigured,
			"no relay is defined for that endpoint's wire protocol")
		return
	}
	// **Listing-shaped, decided here and once.** A `GET` at the suffix this
	// protocol's model listing is at is the only relayed answer this desk
	// reads: everything else is model traffic an engine consumes in code,
	// while a listing is a set of strings the page renders, stores and lets a
	// person copy. Read off the configured kind rather than off anything the
	// page said, so a page cannot ask for the scan to be skipped — or asked
	// for.
	listing := r.Method == http.MethodGet && suffix == relayListingSuffix[endpoint.kind]

	target, err := relayTarget(endpoint.url, suffix, extra)
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
	deadline := time.Now().Add(relayDeadline)
	controller := http.NewResponseController(w)

	// **The whole body is read before a byte of it is dispatched**, and that
	// is what makes "refused, never truncated" true rather than nearly true.
	// A body of undeclared length was previously bounded at the reader while
	// the proxy was already streaming it upstream: the endpoint received —
	// and could act on — the first eight mebibytes of a request this desk then
	// refused. All or nothing means buffering first.
	//
	// The cost is bounded twice: eight mebibytes per request, and four
	// requests in flight, so at most 32 MiB of request bodies are held by this
	// process at once. It is read **after** the slot is taken for exactly that
	// reason — a desk that buffered before the bound could hold as many as the
	// page cared to send.
	//
	// Read under the overall deadline, because a client that dribbles a body
	// holds a slot for as long as it dribbles.
	_ = controller.SetReadDeadline(deadline)
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRelayBody))
	_ = controller.SetReadDeadline(time.Time{})
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge,
				fmt.Sprintf("a relayed request body is at most %d bytes; nothing was sent",
					maxRelayBody))
			return
		}
		// The body did not arrive. Nothing of the request's own words is
		// repeated: what a reader needs is that it was not forwarded.
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest,
			"the request body could not be read, and nothing was sent")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	// Declared, so the endpoint is sent a length rather than a chunked stream
	// this desk has already measured.
	r.ContentLength = int64(len(body))
	r.TransferEncoding = nil

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
			// **A new header set, built from the allow-list.** Filtering in
			// place would leave the rule to be read as "these are removed";
			// building the set says what it is — only these travel. `Origin`,
			// `Referer`, `Cookie` and every credential header anybody invents
			// are absent because they were never added.
			carried := make(http.Header, len(relayedRequestHeaders)+1)
			for header, values := range out.Header {
				if !relayedRequestHeader(header) {
					continue
				}
				carried[header] = values
			}
			// The one thing this relay adds.
			carried.Set(name, value)
			// **A listing is asked for uncompressed**, because a scan of
			// compressed bytes is not a scan. Deleting the page's own
			// `Accept-Encoding` lets Go's transport add and transparently undo
			// its own gzip, so what reaches the scan below is the listing's
			// text. Nothing else is touched: model traffic keeps whatever
			// encoding the page negotiated.
			if listing {
				carried.Del("Accept-Encoding")
			}
			out.Header = carried
		},
		Transport:     beforeTheFirstByte{inner: relayTransport, cancel: cancel},
		FlushInterval: -1,
		ModifyResponse: func(response *http.Response) error {
			status = response.StatusCode
			// **The answer's headers are filtered too**, and the reason is the
			// key: an endpoint that echoes what it was sent would otherwise
			// hand the machine-held credential to the page. `Set-Cookie` is on
			// that list for a second reason — the page and this chassis share
			// an origin, so a cookie from the endpoint would be stored against
			// the desk and sent back to the desk's own endpoints.
			withoutReflectedCredentials(response.Header, key)
			// **No trailer is forwarded**, and this is where that is decided:
			// the proxy copies `res.Trailer` to the page after the body, past
			// every filter here, so an announced `Trailer: X-Echo` was a second
			// way to hand the key over. Emptied rather than filtered, because
			// nothing either protocol needs arrives in one and a trailer this
			// desk carried would be a header nobody had checked.
			response.Trailer = nil
			response.Header.Del("Trailer")
			// **The one body this desk reads, and it is read whole before any
			// of it is forwarded.** Every status, not only a success: a 401's
			// body can carry the credential it rejected as easily as a 200's
			// can carry it as a model id, and a rule with a status in it is a
			// rule with a hole in it. Refusing here is what makes "nothing of
			// it travels" true — `ModifyResponse` runs before the headers are
			// copied, so the error below reaches `ErrorHandler` with the page
			// still holding nothing.
			if listing {
				// **Read through the same idle bound everything else is**, and
				// not with a bare `io.ReadAll`. This branch buffers rather than
				// streams, so the wrapper that was applied *after* it never
				// reached it: an endpoint that sent one byte at its listing
				// path and then stalled held a relay slot until the overall
				// ten-minute deadline, and four of them denied every slot for
				// that long — against a README that promises two minutes
				// between two writes, unqualified. The bound cancels the
				// request's own context, which ends this read.
				bounded := boundedByIdle(response.Body, cancel)
				read, err := io.ReadAll(io.LimitReader(bounded, maxListingBody+1))
				// Stopped whether the read ended or was cut: the timer holds a
				// reference to the cancel function for as long as it is armed.
				_ = bounded.Close()
				if err != nil {
					return err
				}
				if len(read) > maxListingBody {
					return errListingTooLarge
				}
				if problem := listingProblem(read, key); problem != nil {
					return problem
				}
				// Re-declared from what was actually read: Go removes the
				// length and the encoding when it undoes a transparent gzip,
				// and a listing forwarded with neither is one the page reads
				// to EOF rather than to a length.
				response.Body = io.NopCloser(bytes.NewReader(read))
				response.ContentLength = int64(len(read))
				response.Header.Set("Content-Length", strconv.Itoa(len(read)))
				response.Header.Del("Content-Encoding")
				return nil
			}
			response.Body = boundedByIdle(response.Body, cancel)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			// **The listing refusals, and nothing of the body with them.** The
			// sentence is this desk's own and is the whole of what is said:
			// what the endpoint wrote is exactly the thing being withheld.
			if errors.Is(err, errListingCarriesKey) {
				status = http.StatusBadGateway
				writeJSONCoded(w, http.StatusBadGateway, CodeAssistantListingRefused,
					"the endpoint put the credential in its model listing; "+
						"this desk will not list it")
				return
			}
			if errors.Is(err, errListingTooLarge) {
				status = http.StatusBadGateway
				writeJSONCoded(w, http.StatusBadGateway, CodeAssistantListingRefused,
					fmt.Sprintf("the endpoint's model listing is past the %d bytes this desk "+
						"reads, so none of it was listed", maxListingBody))
				return
			}
			if errors.Is(err, errListingNotJSON) {
				status = http.StatusBadGateway
				writeJSONCoded(w, http.StatusBadGateway, CodeAssistantListingRefused,
					"the endpoint's model listing is not one JSON document, so this desk "+
						"could not read it and did not list it")
				return
			}
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
	// **Every write to the page is bounded too.** The two deadlines above
	// cancel the *upstream* context; neither of them ends a write to a client
	// that has stopped reading, and the outer server has no `WriteTimeout` by
	// design — `/ws` is a socket it holds open for a session. So four clients
	// that authenticate and then stop reading could hold all four slots for
	// ever, and every later request would answer `assistant-relay-busy` about
	// a desk that was not carrying anything. The connection's write deadline is
	// extended by the idle bound before each write and never past the overall
	// one, so a page that stops reading loses its answer and gives back the
	// slot.
	proxy.ServeHTTP(&deadlineWriter{
		ResponseWriter: w,
		controller:     controller,
		until:          deadline,
	}, r)
	// Anything the proxy staged for a trailer after the body, dropped before
	// this handler returns — which is when net/http would send it.
	withoutTrailers(w.Header())
	// **Scheme and host only, and no path at all.** The same rule the probe's
	// line follows and for the same reason: a configured URL may carry a query
	// string, and a query string is a place people put credentials. The suffix
	// is not written down either — it is the page's, and a log is not the place
	// to reconstruct a session from.
	s.log.Printf("desk: assistant relay %s answered %d", loggableOrigin(endpoint.url), status)
}

// withoutReflectedCredentials takes the key back out of an answer's headers.
//
// Two rules, and the second is why the first is not enough. **By name**, for
// the headers a credential is conventionally echoed in — a 401 that repeats
// the `Authorization` it rejected, a gateway that mirrors `x-api-key`, a
// `WWW-Authenticate` challenge quoting what was presented. And **by value**,
// exactly: any header at all whose value *is* the configured key, whatever it
// is called, because an endpoint that wants to hand the key back will not use
// a name on anybody's list.
//
// **The value rule has a length in it, and the length is the honest part.**
// A key of twelve bytes or more is looked for *anywhere* in a value, so
// `X-Echo: Bearer <key>` under a name nobody listed is caught as well as a bare
// echo. Below twelve it is exact equality only, because a short key is a
// substring of ordinary text: a three-character key would match a date, a
// status word and half the header set, and a filter that deletes the answer to
// protect a credential is a worse answer than the credential. Twelve is
// `minFingerprintable`, the same length below which this desk will not show a
// fingerprint either, and for the same reason — below it there is not enough
// value to reason about.
//
// **What no comparison catches is a derived form** — base64, percent-encoded,
// hex, half of it — which is the ruling chunk 1 already took for the probe. And
// bodies are not read at all. The bound on both is in the README and it is not a
// filter: the key is the endpoint's own credential, presented only to the
// endpoint the desk-level file names, and good only at the endpoint that
// already holds it.
func withoutReflectedCredentials(header http.Header, key string) {
	for _, name := range reflectedCredentialHeaders {
		header.Del(name)
	}
	if key == "" {
		return
	}
	long := len(key) >= minFingerprintable
	for name, values := range header {
		for _, value := range values {
			if value == key || (long && strings.Contains(value, key)) {
				header.Del(name)
				break
			}
		}
	}
}

// withoutTrailers takes the trailer keys back out of a header map.
//
// **`ModifyResponse` never sees a trailer.** `httputil.ReverseProxy` copies
// `res.Trailer` into the client's header map *after* the body has been
// forwarded, under `http.TrailerPrefix` — so an endpoint that announces
// `Trailer: X-Echo` and sends the key in it had a second, unfiltered way to
// hand the credential to the page. The answer is that this relay forwards no
// trailer at all: `response.Trailer` is emptied before the copy can happen, and
// this is what clears anything staged for one afterwards.
func withoutTrailers(header http.Header) {
	header.Del("Trailer")
	for name := range header {
		if strings.HasPrefix(name, http.TrailerPrefix) {
			header.Del(name)
		}
	}
}

// deadlineWriter bounds how long one write to the page may take.
//
// `http.ResponseController` rather than a hijacked connection, because the
// answer is still an ordinary HTTP response and this is the supported way to
// reach the deadline behind it. Every write extends it by the idle bound and
// never past the request's own overall deadline; a write that misses it fails,
// the proxy's copy ends, the handler returns and the slot is released.
//
// `SetWriteDeadline` is best effort: a `ResponseWriter` that cannot support one
// answers `http.ErrNotSupported`, and the write proceeds unbounded rather than
// the request being refused for the shape of a writer nobody chose. Under the
// desk's own server it is supported, and the test that exercises this runs
// against that server.
type deadlineWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
	until      time.Time
	// finalUntil is the instant the one answer allowed past the overall
	// deadline must be finished by, set the first time a write is attempted
	// past it. Zero until then. Written and read on the proxy's own goroutine,
	// which is the only one that writes a response.
	finalUntil time.Time
}

// extend gives the next write the idle bound, capped at the request's overall
// deadline — **while that deadline is still ahead.** Past it there is one write
// left, the refusal this handler is about to make, and a deadline already in
// the past would fail it before it was attempted: the page would be dropped
// mid-connection instead of told what happened. So the whole bound is the
// overall deadline plus, at most, one idle bound for the final write, and that
// is what the README says.
func (d *deadlineWriter) extend() {
	now := time.Now()
	next := now.Add(relayIdle)
	if next.After(d.until) {
		next = d.until
	}
	if next.After(now) {
		_ = d.controller.SetWriteDeadline(next)
		return
	}
	// Past the overall deadline. There is one answer left — the refusal this
	// handler is about to make — and it gets **one** short bound rather than
	// another idle one: a deadline already in the past would fail it before it
	// was attempted and drop the page mid-connection, and a fresh idle bound
	// would let a late write hold a slot for two more minutes, which is the
	// whole-request bound not being one.
	//
	// One *answer* and not one `Write`: an envelope is a `WriteHeader` and a
	// `Write` at least, so the instant is fixed the first time it is asked for
	// and reused after — the tail is five seconds however many calls it takes,
	// and a write that arrives past it fails, as it should.
	if d.finalUntil.IsZero() {
		d.finalUntil = now.Add(relayFinalWrite)
	}
	_ = d.controller.SetWriteDeadline(d.finalUntil)
}

// WriteHeader swallows an informational response.
//
// **1xx reaches the page through a path no filter sees.** `ReverseProxy`
// forwards an endpoint's `103 Early Hints` through a client trace that copies
// its headers into the page's header map and writes the status — all of it
// before `ModifyResponse` runs — so an endpoint could put the key in a 103
// header and have it delivered. Nothing either protocol needs is carried in
// one, so none is forwarded: the write is dropped, and the proxy clears the
// headers it staged for it immediately afterwards.
func (d *deadlineWriter) WriteHeader(status int) {
	if status < 100 || status >= 200 {
		d.extend()
		d.ResponseWriter.WriteHeader(status)
	}
}

func (d *deadlineWriter) Write(p []byte) (int, error) {
	d.extend()
	return d.ResponseWriter.Write(p)
}

// Flush is what `httputil.ReverseProxy` looks for to stream at all: the
// wrapper must offer it, or a wrapped writer turns every relayed answer into a
// buffered one.
func (d *deadlineWriter) Flush() {
	d.extend()
	_ = d.controller.Flush()
}

// Unwrap lets anything else that wants the real writer find it.
func (d *deadlineWriter) Unwrap() http.ResponseWriter { return d.ResponseWriter }

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
