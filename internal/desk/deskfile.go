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

// deskNotice is something this decoder did with a member it **accepted**.
//
// **Not a problem, and deliberately a different type.** A problem refuses the
// whole file; a notice is a file that was accepted and decoded to something
// other than what it literally says — today, exactly one thing: the withdrawn
// engine. Sharing `deskProblem` would let a notice reach anything that renders
// a refusal, which is a desk reporting an accepted file as a refused one.
type deskNotice struct {
	Key  string `json:"key"`
	Says string `json:"says"`
}

// deskDecode is the verdict on one file.
type deskDecode struct {
	// Endpoint is what the assistant member decoded to, **whether or not the
	// file as a whole was accepted**. It is usable only where `refused()` is
	// false, and `configuredEndpoint` — the only reader — asks that first.
	// See the note at the end of `decodeDeskFile` for why it is carried out of
	// a refused decode rather than dropped.
	Endpoint *assistantEndpoint
	// Engine and Thinking are the two settings beside the endpoint, with the
	// defaults applied where the file names neither.
	//
	// **Carried rather than validated and dropped**, and the reason is what
	// the shared corpus is for. Nothing in the chassis acts on either — the
	// browser is what shows them — but a decoder that checks a member and
	// throws the value away can only be held to *acceptance* parity: this side
	// and the page could agree that `{"engine":"builtin"}` is legal and
	// disagree about what it decoded to, and the fixtures would pass. Carrying
	// them makes the corpus a statement about the answer and not only about
	// the verdict.
	Engine   string
	Thinking string
	// Notices is what this decoder did with a member it accepted, and it is
	// empty on a refused file: a refused file contributes nothing and shows
	// nothing, so reporting what its engine migrated to would be describing a
	// decode that never took effect.
	//
	// Carried for the same reason Engine and Thinking are. The shared corpus
	// states the notices on both sides, so a migration written here and not in
	// the browser — or written in different words — fails on both.
	Notices []deskNotice
	// ProjectFile is `project.file`: the absolute path of the `jpack-desk.json`
	// this desk opens when it is launched with no directory argument. The
	// empty string is "the file names none", which is also what an absent
	// `project` member means.
	//
	// **Carried, and acted on at launch rather than at request time.** It is
	// the one member of this file that decides something before there is a
	// server: `resolveProjectDir` reads it, and everything after that is
	// pinned to the directory it produced.
	ProjectFile string
	Problems    []deskProblem
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
	"identity", "assistant", "project",
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

	slot := assistantSlot{engine: defaultAssistantEngine, thinking: defaultAssistantThinking}
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
		slot = found
	}
	// Dropped where anything at all was refused, which is the rule the browser
	// holds too: nothing was decoded, so there is nothing this decoder did.
	notices := slot.notices
	if len(problems) > 0 {
		notices = nil
	}
	projectFile := ""
	if section, present := record["project"]; present {
		found, projectProblems := decodeProject(section)
		problems = append(problems, projectProblems...)
		projectFile = found
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
	return deskDecode{
		Endpoint:    slot.endpoint,
		Engine:      slot.engine,
		Thinking:    slot.thinking,
		Notices:     notices,
		ProjectFile: projectFile,
		Problems:    dedupeProblems(withoutRedundantReasons(problems)),
	}
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

// projectConfigName is the file a `project.file` must name.
//
// The same constant the page reads a project's configuration from, because it
// is the same file: what `project.file` says is "open the project this file is
// in", and a path to anything else would be a directory chosen by a name that
// is not the one the desk reads.
const projectConfigName = "jpack-desk.json"

// decodeProject reads `project`, whose one member decides which project this
// desk opens when it is launched without a directory.
//
// **Three rules and no more, and each of them is about the path being one this
// desk can act on before it has a server.**
//
//   - **Absolute.** A relative path would be resolved against whatever
//     directory the desk happened to be started in, which is the very thing the
//     member exists to stop mattering: the point of a default project is that
//     `jpack-desk` opens the same one from anywhere.
//   - **Named `jpack-desk.json`.** The member names the configuration file and
//     the desk opens the directory it is in, so a path to anything else would
//     pick a project by a name this desk never reads.
//   - **Refused by `project.file`**, whichever rule it broke, because that is
//     the member somebody has to repair.
//
// A key-shaped member anywhere under `project` is refused by the credential
// scan that runs over the whole document, so there is nothing to add here.
//
// Mirrored by `projectValue` in `deskConfig.ts` and held to it by the shared
// fixtures.
func decodeProject(value any) (string, []deskProblem) {
	record, problems := object(value, "project", []string{"file"})
	if record == nil {
		return "", problems
	}
	file, present := record["file"]
	if !present || file == nil {
		return "", problems
	}
	text, ok := file.(string)
	if !ok {
		return "", append(problems, deskProblem{Key: "project.file", Reason: fmt.Sprintf(
			"must be an absolute path to a %s, or null; found %s",
			projectConfigName, describe(file))})
	}
	trimmed := strings.TrimSpace(text)
	if !absolutePath(trimmed) {
		return "", append(problems, deskProblem{Key: "project.file", Reason: fmt.Sprintf(
			"must be an absolute path, because it is read before this desk has a working "+
				"directory to resolve one against; found %s", describe(file))})
	}
	if lastSegment(trimmed) != projectConfigName {
		return "", append(problems, deskProblem{Key: "project.file", Reason: fmt.Sprintf(
			"must name a %s — the desk opens the directory that file is in; found %s",
			projectConfigName, describe(file))})
	}
	return trimmed, problems
}

// absolutePath is the lexical absoluteness test **both decoders apply**.
//
// `filepath.IsAbs` is not it, and cannot be: it answers differently on
// different platforms, and the browser has no such function at all — so a
// corpus walked by both sides would be walked under two rules. A leading
// separator, or a drive letter with one, is the whole of it.
func absolutePath(path string) bool {
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) {
		return true
	}
	if len(path) >= 3 && path[1] == ':' && (path[2] == '/' || path[2] == '\\') {
		letter := path[0]
		return (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')
	}
	return false
}

// lastSegment is the file name at the end of a path, on either separator.
func lastSegment(path string) string {
	at := strings.LastIndexAny(path, `/\`)
	if at < 0 {
		return path
	}
	return path[at+1:]
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
//
// **Three members, and two of them say how rather than whether.** `engine` and
// `thinking` are allowed beside a null endpoint on purpose: they describe how
// an assistant would run, and a desk that has not configured one yet may still
// have an opinion about that. Both are optional, both default, and both are
// refused by name for an unknown value — an engine nobody certified, or a tier
// nothing implements, is a setting that reads as a grant to whoever wrote it.
//
// **`engine` carries one migration**, the same one on both sides of the shared
// decoder: `"builtin"` was withdrawn, so it decodes to the engine that runs and
// this decoder says so rather than substituting in silence. The withdrawn value
// is checked before `oneOf`, so the refusal a reader meets for anything else
// never lists an id this build cannot run.
//
// Neither setting is acted on here, because nothing in the chassis reads them:
// the browser decodes the same file under the same contract and is what shows
// them. What this side is for is that a file the browser refuses refuses here
// too, so an unknown engine authorises no outbound request either.
func decodeAssistant(value any) (assistantSlot, []deskProblem) {
	slot := assistantSlot{engine: defaultAssistantEngine, thinking: defaultAssistantThinking}
	record, problems := object(value, "assistant", []string{"endpoint", "engine", "thinking"})
	if record == nil {
		return slot, problems
	}
	if named, ok := record["engine"].(string); ok && named == withdrawnAssistantEngine {
		slot.notices = append(slot.notices,
			deskNotice{Key: "assistant.engine", Says: assistantEngineWithdrawn})
		slot.engine = defaultAssistantEngine
	} else {
		problems = append(problems, oneOf(record, "assistant", "engine", AssistantEngines)...)
		// Read back only where the file said something this decoder accepts; a
		// refused value leaves the default standing, and the file is refused
		// whole anyway.
		if named, ok := record["engine"].(string); ok && contains(AssistantEngines, named) {
			slot.engine = named
		}
	}
	problems = append(problems, oneOf(record, "assistant", "thinking", AssistantThinkingTiers)...)
	if named, ok := record["thinking"].(string); ok && contains(AssistantThinkingTiers, named) {
		slot.thinking = named
	}
	endpoint, present := record["endpoint"]
	if !present || endpoint == nil {
		return slot, problems
	}
	inner, innerProblems := object(endpoint, "assistant.endpoint",
		[]string{"url", "kind", "model", "tools"})
	problems = append(problems, innerProblems...)
	if inner == nil {
		return slot, problems
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
		return slot, problems
	}
	slot.endpoint = &assistantEndpoint{
		url:   normalizedEndpointURL(trimmedURL),
		kind:  kind,
		model: trimmedModel,
		tools: tools,
	}
	return slot, problems
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
// to live, in the one file this desk insists holds none.
//
// **A query string is allowed and is now held to a rule of its own**, because
// the file is no longer only something a person types: `PUT /api/desk-config`
// lets page code write it, and round 1 found the configured query the one part
// of a relayed request the page could then fill with anything. It is checked
// here, at decode, so a hand-edited file meets exactly the same rule.
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
	if reason := endpointQueryProblem(parsed.RawQuery); reason != "" {
		return reason
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

// reservedQueryNames are the query names a configured URL may not use.
//
// **The names the relay itself may add, plus the one the listing pages with.**
// A configured `alt` would be a second copy of the pair the relay admits from
// the page — a query two parsers could count differently, which is the whole
// class `relayQueryProblem` exists to keep off the wire — and a configured
// `pageToken` would page a listing this desk documents as first-page-only,
// from a value nobody re-reads.
//
// **Reserved on every kind, not only the one that admits `alt`.** A per-kind
// rule would make a URL legal until somebody changed `kind` beside it, and the
// two members are edited together by the same form. One list, one answer.
var reservedQueryNames = []string{"alt", "pageToken"}

// endpointQueryProblem is the rule the configured URL's query is held to.
//
// # Why this exists at all
//
// The configured query is the one part of a relayed request that comes off
// this machine rather than out of the page, and `relayTarget` carries it
// upstream byte for byte. That was safe while the file was something a person
// typed. It stopped being safe the moment `PUT /api/desk-config` let page code
// write the file: round 1 found `?key=secret`, `?alt=sse`, `?pageToken=x`, a
// semicolon and every encoded alias reaching the endpoint on every later call,
// through a member the per-kind query rule never looked at.
//
// So three things are refused, and each one is a rule this desk already
// applies somewhere else:
//
//   - **A credential-shaped name**, by the same reading `isKeyLike` gives a
//     member name — because "a key is never written into configuration" cannot
//     be a rule about members only while a URL sits beside them.
//   - **A name the relay reserves**, so a configured pair can never duplicate
//     or pre-empt one the relay adds.
//   - **A semicolon anywhere in it**, which is `relayQueryProblem`'s rule
//     verbatim: a separator to some servers and a value to others, and this
//     desk will not send one it cannot read the same way twice.
//
// Mirrored by `endpointQueryProblem` in `deskConfig.ts` and held to it by the
// shared fixtures.
func endpointQueryProblem(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	if strings.ContainsRune(rawQuery, ';') {
		return "must not carry a semicolon in its query: it is a separator to some servers " +
			"and a value to others, and this desk will not send one it cannot read the same " +
			"way twice"
	}
	for _, parameter := range strings.Split(rawQuery, "&") {
		name, value, _ := strings.Cut(parameter, "=")
		// **Both halves, and both held to UTF-8.** Round 2 found the two
		// decoders disagreeing about exactly this: `url.QueryUnescape("%FF")`
		// answers one byte and no error, while the browser's
		// `decodeURIComponent` throws — so `?%FF=x` was accepted here, could
		// be written through `PUT /api/desk-config`, and could authorise an
		// outbound request, while the page refused the same file. That is the
		// "a configuration the browser refuses still sends the key" class,
		// reopened by a percent escape. Requiring valid UTF-8 is what makes
		// the two answers one answer, and the *value* is read for the same
		// reason the name is: it travels upstream too.
		for _, half := range [2]string{name, value} {
			decoded, err := url.QueryUnescape(half)
			if err != nil || !utf8.ValidString(decoded) {
				return fmt.Sprintf(
					"has a query parameter this desk cannot read the same way a browser "+
						"does (%q): a query it cannot read identically twice is one it will "+
						"not forward", half)
			}
		}
		decoded, _ := url.QueryUnescape(name)
		// **The reserved names are read first**, because `pageToken` folds to
		// a word the credential rule also catches and the sentence a reader
		// repairs the file by should be the true one: it is reserved, not
		// mistaken for a secret. Both refuse either way.
		//
		// **Compared without regard to case**, because that is how the servers
		// this rule exists for read a query name: `?ALT=sse` on a gemini base
		// was accepted, and the relay then added its own pair for an upstream
		// that sees two copies of one name — which is precisely the
		// disagreement the query rules were written to keep off the wire.
		if containsFold(reservedQueryNames, decoded) {
			return fmt.Sprintf(
				"must not carry %q in its query: it is a name the relay itself may add, and "+
					"a query with two of one name is one two parsers count differently",
				decoded)
		}
		if isCredentialQueryName(decoded) {
			return fmt.Sprintf(
				"must not carry %q in its query — %s", decoded, keysAreNeverInConfiguration)
		}
	}
	return ""
}

// containsFold is `contains` for names two readers may spell in different
// cases.
//
// Its own function rather than a lower-cased comparison written inline,
// because the browser's mirror of this rule folds the same way and one
// spelling of "the same name" is what the shared fixtures hold.
func containsFold(haystack []string, needle string) bool {
	for _, candidate := range haystack {
		if strings.EqualFold(candidate, needle) {
			return true
		}
	}
	return false
}

// isCredentialQueryName is `isKeyLike` for a query parameter's name.
//
// The member rule plus `auth`, which that rule does not catch: it folds to
// "auth", and no word on the list is a substring of it. It is a credential
// parameter name in the wild, so it is named here rather than left to a reader
// to notice.
func isCredentialQueryName(name string) bool {
	return isKeyLike(name) || strings.EqualFold(strings.TrimSpace(name), "auth")
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
