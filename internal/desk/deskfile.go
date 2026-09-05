package desk

// The desk-level configuration file, decoded here under the same contract the
// browser decodes it under.
//
// # Why this is not "read one member"
//
// It used to be. The chassis pulled `assistant.endpoint` out of the file,
// checked those four members, and formed no opinion about the rest — on the
// argument that the verdict about the file as a whole belongs to the page.
//
// That argument was wrong, and the way it was wrong is worth writing down. The
// page refuses the **whole file** for one bad key: a `desk.json` carrying a
// good endpoint and a stray `apiKey` at the top level, or a missing `tools`,
// or a whitespace model, shows "configuration refused" on Admin and supplies
// nothing to the desk. But the probe read only its own member, found it
// serviceable, and sent the stored key to the endpoint that file named. A
// configuration the desk had visibly rejected was still authorising an
// outbound request with a credential in it — which is exactly the situation
// where the two must not disagree.
//
// So the contract is one contract, and this is the second implementation of
// it. **Two implementations of one rule drift**, so they are held together by
// fixtures that both sides read: `web/src/config/fixtures/desk-config/`
// carries the files and one `expected.json` naming, for each, whether it is
// accepted and which keys are refused. The vitest suite and this package's
// tests walk the same directory. A rule changed on one side and not the other
// fails on both.
//
// # What a refusal means here
//
// The same as it means there: any problem anywhere in the file refuses the
// whole file, and every refusal names its key. For this package that has one
// further consequence, which is the point of the exercise — a refused file
// means **no outbound request is made at all**. Not a request to a different
// place; none.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

// deskProblem is one refusal: the offending key, path-qualified, and why.
type deskProblem struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// deskDecode is the verdict on one file.
type deskDecode struct {
	// Endpoint is what the assistant member decoded to, **whether or not the
	// file as a whole was accepted**. It is usable only where `refused()` is
	// false, and `configuredEndpoint` — the only reader — asks that first.
	// See the note at the end of `decodeDeskFile` for why it is carried out of
	// a refused decode rather than dropped.
	Endpoint *assistantEndpoint
	Problems []deskProblem
}

func (d deskDecode) refused() bool { return len(d.Problems) > 0 }

// keysAreNeverInConfiguration is the sentence a key-shaped member is refused
// with, character for character as `deskConfig.ts` writes it.
//
// Held identical by a test that reads the TypeScript declaration, because a
// refusal a reader meets in one place and not the other is two contracts.
const keysAreNeverInConfiguration = "a key is never stored in configuration — the desk keeps " +
	"the assistant key on this machine, in a file that is in no project and is never sent to " +
	"this page; store it on Admin › Assistant"

// keyLikeWords are the names this decoder treats as credential-shaped.
var keyLikeWords = []string{
	"key", "secret", "token", "password", "credential", "bearer", "authorization",
}

// isKeyLike folds a member name to letters and looks for one of the words.
//
// Deliberately broad and deliberately a substring test: the point is to catch
// a credential somebody reached for a plausible name for, and a member of this
// schema that collided with one of these words would be renamed rather than
// exempted.
func isKeyLike(name string) bool {
	folded := make([]rune, 0, len(name))
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' {
			folded = append(folded, r)
		}
	}
	word := string(folded)
	for _, candidate := range keyLikeWords {
		if strings.Contains(word, candidate) {
			return true
		}
	}
	return false
}

// withoutRedundantReasons drops every other refusal for a key that already
// carries the credential sentence.
//
// **One producer of that sentence** — `scanForKeys` — and the schema walk says
// only "unknown key". A helper that picked the sentence at the schema walk as
// well meant breaking either one left the other saying it, and the mutation
// table reported an unheld safeguard while two things held it. Mirrors
// `withoutRedundantReasons` in `deskConfig.ts`.
func withoutRedundantReasons(problems []deskProblem) []deskProblem {
	credentialed := make(map[string]bool)
	for _, problem := range problems {
		if problem.Reason == keysAreNeverInConfiguration {
			credentialed[problem.Key] = true
		}
	}
	kept := make([]deskProblem, 0, len(problems))
	for _, problem := range problems {
		if problem.Reason != keysAreNeverInConfiguration && credentialed[problem.Key] {
			continue
		}
		kept = append(kept, problem)
	}
	return kept
}

// The top-level keys the desk-level file admits. `identity` and `assistant`
// are the two that may appear **only** here.
var deskTopLevelKeys = []string{
	"deskConfigVersion", "organization", "user", "appearance", "panes", "storage",
	"identity", "assistant",
}

// The pane dimensions and their bounds, mirrored from `PANE_BOUNDS`.
var paneBounds = map[string][2]float64{
	"panes.left.width":      {160, 640},
	"panes.inspector.width": {240, 720},
	"panes.console.height":  {80, 720},
}

const maxMarkBytes = 65536

const deskConfigVersion = 1

// decodeDeskFile is the whole contract, in the order the browser applies it:
// is it JSON, is it an object, does it declare the version, is every member
// one this location admits — and only then are the values read.
func decodeDeskFile(text []byte) deskDecode {
	var parsed any
	if err := json.Unmarshal(text, &parsed); err != nil {
		return deskDecode{Problems: []deskProblem{{
			Key: "", Reason: fmt.Sprintf("the file is not JSON: %v", err)}}}
	}
	record, ok := parsed.(map[string]any)
	if !ok {
		return deskDecode{Problems: []deskProblem{{
			Key: "", Reason: fmt.Sprintf("the file must be a JSON object; found %s", describe(parsed))}}}
	}

	var problems []deskProblem
	// **The credential scan runs first, and over everything.** Schema decoding
	// only visits members it knows about, so a key inside an object this
	// schema has never heard of — or inside an array — was refused as an
	// unknown *object* and never named as a key. This walks the parsed
	// document instead, so depth and shape are irrelevant.
	problems = append(problems, scanForKeys("", parsed)...)

	if version, present := record["deskConfigVersion"]; !present {
		problems = append(problems, deskProblem{Key: "deskConfigVersion", Reason: "required"})
	} else if number, ok := version.(float64); !ok || number != deskConfigVersion {
		problems = append(problems, deskProblem{
			Key:    "deskConfigVersion",
			Reason: fmt.Sprintf("must be %d; found %s", deskConfigVersion, describe(version))})
	}

	for _, key := range sortedKeys(record) {
		if contains(deskTopLevelKeys, key) {
			continue
		}
		problems = append(problems, deskProblem{Key: key, Reason: "unknown key"})
	}

	var endpoint *assistantEndpoint
	if section, present := record["organization"]; present {
		problems = append(problems, decodeOrganization(section)...)
	}
	if section, present := record["user"]; present {
		problems = append(problems, decodeUser(section)...)
	}
	if section, present := record["appearance"]; present {
		problems = append(problems, decodeAppearance(section)...)
	}
	if section, present := record["panes"]; present {
		problems = append(problems, decodePanes(section)...)
	}
	if section, present := record["storage"]; present {
		problems = append(problems, decodeStorage(section)...)
	}
	if section, present := record["identity"]; present {
		problems = append(problems, decodeIdentity(section)...)
	}
	if section, present := record["assistant"]; present {
		found, assistantProblems := decodeAssistant(section)
		problems = append(problems, assistantProblems...)
		endpoint = found
	}

	// **The endpoint is carried out even when the file is refused, and the
	// single gate is `refused()`.** Dropping it here as well looked safer and
	// made the safeguard untestable: a mutation that skipped the refusal check
	// in `configuredEndpoint` still met a nil endpoint, refused for that
	// reason instead, and made no request — so the harness row for "a refused
	// configuration still authorises a probe" survived, and the property it
	// claimed to hold was held by an accident of structure rather than by the
	// check. One gate, in one place, that a test can break.
	//
	// Nothing may read `Endpoint` without asking `refused()` first;
	// `configuredEndpoint` is the only caller and does exactly that.
	return deskDecode{Endpoint: endpoint, Problems: dedupeProblems(withoutRedundantReasons(problems))}
}

// scanForKeys walks the parsed document and names every credential-shaped
// member, wherever it is.
//
// Arrays are walked too, with an index in the path, because `[{"apiKey": …}]`
// is a key in a configuration file however unlikely the shape.
func scanForKeys(path string, value any) []deskProblem {
	var problems []deskProblem
	switch typed := value.(type) {
	case map[string]any:
		for _, name := range sortedKeys(typed) {
			child := join(path, name)
			if isKeyLike(name) {
				problems = append(problems, deskProblem{
					Key: child, Reason: keysAreNeverInConfiguration})
			}
			problems = append(problems, scanForKeys(child, typed[name])...)
		}
	case []any:
		for index, element := range typed {
			problems = append(problems, scanForKeys(fmt.Sprintf("%s[%d]", path, index), element)...)
		}
	}
	return problems
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// object is one nested section, with its unknown members refused by path.
func object(value any, key string, allowed []string) (map[string]any, []deskProblem) {
	record, ok := value.(map[string]any)
	if !ok {
		return nil, []deskProblem{{
			Key: key, Reason: fmt.Sprintf("must be an object; found %s", describe(value))}}
	}
	var problems []deskProblem
	for _, member := range sortedKeys(record) {
		if !contains(allowed, member) {
			problems = append(problems, deskProblem{
				Key: join(key, member), Reason: "unknown key"})
		}
	}
	return record, problems
}

func decodeOrganization(value any) []deskProblem {
	record, problems := object(value, "organization", []string{"name", "mark"})
	if record == nil {
		return problems
	}
	if name, present := record["name"]; present && name != nil {
		text, ok := name.(string)
		if !ok || strings.TrimSpace(text) == "" {
			problems = append(problems, deskProblem{Key: "organization.name",
				Reason: fmt.Sprintf("must be a non-empty string or null; found %s", describe(name))})
		}
	}
	if mark, present := record["mark"]; present && mark != nil {
		text, ok := mark.(string)
		if !ok {
			problems = append(problems, deskProblem{Key: "organization.mark",
				Reason: fmt.Sprintf(
					"must be an inline SVG string, a data: URI, or null; found %s", describe(mark))})
		} else {
			trimmed := strings.TrimSpace(text)
			if !strings.HasPrefix(trimmed, "<svg") && !strings.HasPrefix(trimmed, "data:image/") {
				problems = append(problems, deskProblem{Key: "organization.mark",
					Reason: `must begin with "<svg" or "data:image/" — a file path is not accepted`})
			} else if len(text) > maxMarkBytes {
				problems = append(problems, deskProblem{Key: "organization.mark",
					Reason: fmt.Sprintf("must be at most %d bytes of UTF-8; found %d",
						maxMarkBytes, len(text))})
			}
		}
	}
	return problems
}

func decodeUser(value any) []deskProblem {
	record, problems := object(value, "user", []string{"displayName"})
	if record == nil {
		return problems
	}
	if name, present := record["displayName"]; present {
		text, ok := name.(string)
		if !ok || strings.TrimSpace(text) == "" {
			problems = append(problems, deskProblem{Key: "user.displayName",
				Reason: fmt.Sprintf("must be a non-empty string; found %s", describe(name))})
		}
	}
	return problems
}

func decodeAppearance(value any) []deskProblem {
	record, problems := object(value, "appearance", []string{"theme", "density"})
	if record == nil {
		return problems
	}
	problems = append(problems, oneOf(record, "appearance", "theme",
		[]string{"system", "light", "dark"})...)
	problems = append(problems, oneOf(record, "appearance", "density",
		[]string{"comfortable", "compact"})...)
	return problems
}

func decodePanes(value any) []deskProblem {
	record, problems := object(value, "panes", []string{"left", "inspector", "console"})
	if record == nil {
		return problems
	}
	if left, present := record["left"]; present {
		inner, innerProblems := object(left, "panes.left", []string{"mode", "width"})
		problems = append(problems, innerProblems...)
		if inner != nil {
			problems = append(problems, oneOf(inner, "panes.left", "mode",
				[]string{"expanded", "icons"})...)
			problems = append(problems, dimension(inner, "panes.left", "width")...)
		}
	}
	if inspector, present := record["inspector"]; present {
		inner, innerProblems := object(inspector, "panes.inspector", []string{"open", "width"})
		problems = append(problems, innerProblems...)
		if inner != nil {
			problems = append(problems, boolean(inner, "panes.inspector", "open")...)
			problems = append(problems, dimension(inner, "panes.inspector", "width")...)
		}
	}
	if console, present := record["console"]; present {
		inner, innerProblems := object(console, "panes.console", []string{"open", "height"})
		problems = append(problems, innerProblems...)
		if inner != nil {
			problems = append(problems, boolean(inner, "panes.console", "open")...)
			problems = append(problems, dimension(inner, "panes.console", "height")...)
		}
	}
	return problems
}

func decodeStorage(value any) []deskProblem {
	record, problems := object(value, "storage", []string{"packs"})
	if record == nil {
		return problems
	}
	packs, present := record["packs"]
	if !present {
		return problems
	}
	inner, innerProblems := object(packs, "storage.packs", []string{"kind", "dir", "idBase"})
	problems = append(problems, innerProblems...)
	if inner == nil {
		return problems
	}
	if kind, present := inner["kind"]; present {
		if text, ok := kind.(string); !ok || text != "filesystem" {
			problems = append(problems, deskProblem{Key: "storage.packs.kind",
				Reason: fmt.Sprintf(
					`must be "filesystem"; "database" and "cloud storage" are not available yet, found %s`,
					describe(kind))})
		}
	}
	if dir, present := inner["dir"]; present {
		problems = append(problems, packDir(dir)...)
	}
	if base, present := inner["idBase"]; present {
		text, ok := base.(string)
		if !ok || strings.TrimSpace(text) == "" {
			problems = append(problems, deskProblem{Key: "storage.packs.idBase",
				Reason: fmt.Sprintf("must be a non-empty string; found %s", describe(base))})
		} else if _, err := url.Parse(strings.TrimSpace(text)); err != nil ||
			!strings.Contains(strings.TrimSpace(text), ":") {
			problems = append(problems, deskProblem{Key: "storage.packs.idBase",
				Reason: fmt.Sprintf(
					"must be a URI, because a pack's id member is one; found %s", describe(base))})
		}
	}
	return problems
}

// excludedDirectories and stagingPrefixName mirror `EXCLUDED_DIRECTORIES` and
// `STAGING_PREFIX`, which themselves mirror `watch.go` and `files.go`.
var excludedDirectories = []string{".git", "node_modules", "dist", ".venv", "vendor"}

func packDir(value any) []deskProblem {
	bad := func(reason string) []deskProblem {
		return []deskProblem{{Key: "storage.packs.dir", Reason: reason}}
	}
	text, ok := value.(string)
	if !ok {
		return bad(fmt.Sprintf("must be a string; found %s", describe(value)))
	}
	trimmed := strings.TrimRight(strings.TrimSpace(text), "/")
	if trimmed == "" {
		return bad("must name a directory inside the project")
	}
	if strings.HasPrefix(trimmed, "/") {
		return bad("must be relative to the project, not absolute")
	}
	if strings.ContainsAny(trimmed, `\:`) {
		return bad("must be slash-separated and carry no backslash or colon")
	}
	parts := strings.Split(trimmed, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return bad(`must not contain an empty, "." or ".." path segment`)
		}
	}
	for _, part := range parts {
		lower := strings.ToLower(part)
		if strings.HasPrefix(lower, stagingPrefix) {
			return bad(fmt.Sprintf("must not name %s, which the desk never reads or writes", part))
		}
		for _, excluded := range excludedDirectories {
			if lower == excluded {
				return bad(fmt.Sprintf(
					"must not name %s, which the desk never reads or writes", part))
			}
		}
	}
	if len(parts) > maxWalkDepth {
		return bad(fmt.Sprintf(
			"must be at most %d directories deep; the file listing gives up there", maxWalkDepth))
	}
	return nil
}

func decodeIdentity(value any) []deskProblem {
	record, problems := object(value, "identity", []string{"provider"})
	if record == nil {
		return problems
	}
	provider, present := record["provider"]
	if !present || provider == nil {
		return problems
	}
	inner, innerProblems := object(provider, "identity.provider", []string{
		"label", "issuer", "clientId", "scopes", "audience", "claims",
		"showRemoteAvatar", "signOut"})
	problems = append(problems, innerProblems...)
	if inner == nil {
		return problems
	}
	issuer, ok := inner["issuer"].(string)
	if !ok {
		problems = append(problems, deskProblem{Key: "identity.provider.issuer",
			Reason: fmt.Sprintf("must be a string; found %s", describe(inner["issuer"]))})
	} else if !acceptableIssuer(issuer) {
		problems = append(problems, deskProblem{Key: "identity.provider.issuer",
			Reason: "must be an https: URL, or an http: URL on localhost or 127.0.0.1"})
	}
	clientID, ok := inner["clientId"].(string)
	if !ok || clientID == "" {
		problems = append(problems, deskProblem{Key: "identity.provider.clientId",
			Reason: fmt.Sprintf("must be a non-empty string; found %s", describe(inner["clientId"]))})
	}
	if scopes, present := inner["scopes"]; present {
		list, ok := scopes.([]any)
		if !ok {
			problems = append(problems, deskProblem{Key: "identity.provider.scopes",
				Reason: fmt.Sprintf("must be an array of strings; found %s", describe(scopes))})
		} else {
			for _, scope := range list {
				if _, ok := scope.(string); !ok {
					problems = append(problems, deskProblem{Key: "identity.provider.scopes",
						Reason: fmt.Sprintf("must be an array of strings; found %s", describe(scopes))})
					break
				}
			}
		}
	}
	if claims, present := inner["claims"]; present {
		declared, claimProblems := object(claims, "identity.provider.claims",
			[]string{"name", "picture", "subject"})
		problems = append(problems, claimProblems...)
		if declared != nil {
			for _, member := range []string{"name", "picture", "subject"} {
				spelled, present := declared[member]
				if !present {
					continue
				}
				if text, ok := spelled.(string); !ok || text == "" {
					problems = append(problems, deskProblem{
						Key:    "identity.provider.claims." + member,
						Reason: fmt.Sprintf("must be a non-empty string; found %s", describe(spelled))})
				}
			}
		}
	}
	// **`label` and `audience` were declared and never read**, which is how
	// the two decoders came apart a second time: an object-valued `label`
	// beside a perfectly good endpoint was refused by the browser and accepted
	// here, and "accepted here" is what authorises an outbound request with a
	// credential in it. Anything that is validated on one side is validated on
	// both; the fixtures below are what keeps that true rather than hoped.
	problems = append(problems, optionalString(inner, "identity.provider", "label")...)
	problems = append(problems, optionalString(inner, "identity.provider", "audience")...)
	problems = append(problems, boolean(inner, "identity.provider", "showRemoteAvatar")...)
	problems = append(problems, oneOf(inner, "identity.provider", "signOut",
		[]string{"local", "provider"})...)
	return problems
}

func acceptableIssuer(issuer string) bool {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	return parsed.Scheme == "http" &&
		(parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1")
}

// decodeAssistant reads the slot, and is the only section that yields a value.
func decodeAssistant(value any) (*assistantEndpoint, []deskProblem) {
	record, problems := object(value, "assistant", []string{"endpoint"})
	if record == nil {
		return nil, problems
	}
	endpoint, present := record["endpoint"]
	if !present || endpoint == nil {
		return nil, problems
	}
	inner, innerProblems := object(endpoint, "assistant.endpoint",
		[]string{"url", "kind", "model", "tools"})
	problems = append(problems, innerProblems...)
	if inner == nil {
		return nil, problems
	}

	raw, ok := inner["url"].(string)
	trimmedURL := strings.TrimSpace(raw)
	if !ok || trimmedURL == "" {
		problems = append(problems, deskProblem{Key: "assistant.endpoint.url",
			Reason: fmt.Sprintf("must be a non-empty string; found %s", describe(inner["url"]))})
	} else if reason := endpointURLProblem(trimmedURL); reason != "" {
		problems = append(problems, deskProblem{Key: "assistant.endpoint.url", Reason: reason})
	}

	kind, _ := inner["kind"].(string)
	if _, present := inner["kind"]; !present {
		problems = append(problems, deskProblem{Key: "assistant.endpoint.kind", Reason: "required"})
	} else if !contains(AssistantKinds, kind) {
		problems = append(problems, deskProblem{Key: "assistant.endpoint.kind",
			Reason: fmt.Sprintf("must be one of %s; found %s",
				quotedList(AssistantKinds), describe(inner["kind"]))})
	}

	model, ok := inner["model"].(string)
	trimmedModel := strings.TrimSpace(model)
	if !ok || trimmedModel == "" {
		problems = append(problems, deskProblem{Key: "assistant.endpoint.model",
			Reason: fmt.Sprintf("must be a non-empty string; found %s", describe(inner["model"]))})
	}

	var tools []string
	rawTools, present := inner["tools"]
	if !present {
		problems = append(problems, deskProblem{Key: "assistant.endpoint.tools",
			Reason: "required — an absent tool list would be a capability granted by a file that " +
				"never mentioned it; write [] for an assistant that may call nothing"})
	} else {
		list, ok := rawTools.([]any)
		if !ok {
			problems = append(problems, deskProblem{Key: "assistant.endpoint.tools",
				Reason: fmt.Sprintf("must be an array of strings; found %s", describe(rawTools))})
		} else {
			for _, entry := range list {
				name, ok := entry.(string)
				if !ok {
					problems = append(problems, deskProblem{Key: "assistant.endpoint.tools",
						Reason: fmt.Sprintf("must be an array of strings; found %s", describe(rawTools))})
					break
				}
				if !contains(AssistantTools, name) {
					problems = append(problems, deskProblem{Key: "assistant.endpoint.tools",
						Reason: fmt.Sprintf(
							"%q is not a tool the assistant may call; it accepts %s",
							name, strings.Join(AssistantTools, ", "))})
					continue
				}
				tools = append(tools, name)
			}
		}
	}

	// Same rule one level down: the object is handed back whenever it decoded
	// into something, and whether it may be *used* is `refused()`'s answer.
	if len(problems) > 0 {
		return nil, problems
	}
	return &assistantEndpoint{
		url:   normalizedEndpointURL(trimmedURL),
		kind:  kind,
		model: trimmedModel,
		tools: tools,
	}, problems
}

// endpointURLProblem is the transport rule, and the credential rule beside it.
//
// **`https:`, or `http:` on `localhost` or `127.0.0.1`** — a rule about
// transport, because a bearer credential in clear text over a network is a
// credential given away, and one about transport only: nothing reads the host,
// compares it to a list, or behaves differently for one endpoint than another.
// `::1` is deliberately **not** admitted, because the browser's decoder does
// not admit it either, and one of the two accepting a URL the other refuses is
// the disagreement this whole file exists to end.
//
// **Userinfo and a fragment are refused by name.** A URL is written into a
// configuration file, shown on Admin and named in a diagnostic; a credential
// smuggled into its userinfo would be a second, unmanaged place for a secret
// to live, in the one file this desk insists holds none. A query string is
// *allowed* — some gateways route on one — and is never logged.
func endpointURLProblem(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Sprintf("must be an absolute URL; found %q", raw)
	}
	if parsed.User != nil {
		return "must not carry a user or password in the URL — a key is never written into " +
			"configuration, and that includes into a URL"
	}
	if parsed.Fragment != "" || strings.Contains(raw, "#") {
		return "must not carry a fragment; an endpoint is a location a request is sent to, " +
			"and a fragment is never sent"
	}
	if parsed.Scheme == "https" {
		return ""
	}
	if parsed.Scheme == "http" &&
		(parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1") {
		return ""
	}
	return fmt.Sprintf(
		"must be an https: URL, or an http: URL on localhost or 127.0.0.1; found %q — "+
			"a key sent in clear text over a network is a key given away", raw)
}

// normalizedEndpointURL trims a trailing separator from the URL's **path**.
//
// The path, not the string. `strings.TrimRight(raw, "/")` was the first
// version and it is wrong the moment a query string is allowed: it trims
// nothing at all from `https://gw/v1/?route=eu`, and worse, it invited the
// protocol path to be appended to the whole string — which put `/models`
// after the query and sent `GET /v1?route=eu/models`. The live drive is what
// caught that.
func normalizedEndpointURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	// Through the escaped form, for the reason `appendPath` gives: writing the
	// decoded path alone re-encodes `%2F` into a separator and turns one
	// configured segment into two.
	appendPath(parsed, "")
	return parsed.String()
}

/* Small shared readers ----------------------------------------------------- */

func oneOf(record map[string]any, section, member string, choices []string) []deskProblem {
	value, present := record[member]
	if !present {
		return nil
	}
	text, ok := value.(string)
	if ok && contains(choices, text) {
		return nil
	}
	return []deskProblem{{Key: join(section, member),
		Reason: fmt.Sprintf("must be one of %s; found %s", quotedList(choices), describe(value))}}
}

// optionalString mirrors the browser's `optionalString`: a string, or null,
// or absent. Anything else is refused by name, in the same words.
func optionalString(record map[string]any, section, member string) []deskProblem {
	value, present := record[member]
	if !present || value == nil {
		return nil
	}
	if _, ok := value.(string); ok {
		return nil
	}
	return []deskProblem{{Key: join(section, member),
		Reason: fmt.Sprintf("must be a string or null; found %s", describe(value))}}
}

func boolean(record map[string]any, section, member string) []deskProblem {
	value, present := record[member]
	if !present {
		return nil
	}
	if _, ok := value.(bool); ok {
		return nil
	}
	return []deskProblem{{Key: join(section, member),
		Reason: fmt.Sprintf("must be a boolean; found %s", describe(value))}}
}

func dimension(record map[string]any, section, member string) []deskProblem {
	value, present := record[member]
	if !present {
		return nil
	}
	key := join(section, member)
	number, ok := value.(float64)
	if !ok || number != float64(int64(number)) {
		return []deskProblem{{Key: key,
			Reason: fmt.Sprintf("must be a whole number of pixels; found %s", describe(value))}}
	}
	bounds, known := paneBounds[key]
	if known && (number < bounds[0] || number > bounds[1]) {
		return []deskProblem{{Key: key, Reason: fmt.Sprintf(
			"must be between %d and %d pixels inclusive; found %d",
			int(bounds[0]), int(bounds[1]), int(number))}}
	}
	return nil
}

// describe names a value's kind the way the browser's decoder does, so a
// fixture's expected sentence reads the same on both sides.
func describe(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case []any:
		return "an array"
	case string:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	case bool:
		return fmt.Sprintf("boolean %t", typed)
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("number %d", int64(typed))
		}
		return fmt.Sprintf("number %v", typed)
	case map[string]any:
		return "object [object Object]"
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func quotedList(choices []string) string {
	quoted := make([]string, 0, len(choices))
	for _, choice := range choices {
		quoted = append(quoted, fmt.Sprintf("%q", choice))
	}
	return strings.Join(quoted, ", ")
}

func sortedKeys(record map[string]any) []string {
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// dedupeProblems collapses the one overlap the two passes can produce: the
// credential scan and the schema walk both name a key-shaped member.
func dedupeProblems(problems []deskProblem) []deskProblem {
	seen := make(map[deskProblem]bool, len(problems))
	unique := make([]deskProblem, 0, len(problems))
	for _, problem := range problems {
		if seen[problem] {
			continue
		}
		seen[problem] = true
		unique = append(unique, problem)
	}
	return unique
}

// validUTF8 is used before anything is decoded: the file API refuses non-UTF-8
// and so does this, for the same reason — a decoder handed replacement
// characters is deciding about a document nobody wrote.
func validUTF8(data []byte) bool { return utf8.Valid(data) }
