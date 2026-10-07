package desk

// The upgrade offer for an existing project (ADR-0009, section 4).
//
// A desk made before new desks started gated, or the project Desk was started
// on, is offered the gates and never put under them. The offer says what it
// would write, and why:
//
//  1. `jpack.json`: configVersion "5" ("4" where the runtime reads no "5"),
//     with `requireReviewed` and the audit directory `.desk-private/audit`,
//     made owner-only where it is missing. A project that already declares an
//     audit directory keeps it. Every other byte of the file is its own: the
//     other members, their order, their values and the whitespace between
//     them.
//  2. `.gitignore`: the line `.desk-private/`, where the project is in a Git
//     work tree and its `.gitignore` does not already ignore that folder.
//  3. The first "Review and lock" of the project's packs: PR C's review, over
//     the configuration as the upgrade would write it.
//  4. `requireComparableFacts`, which the owner can decline on its own.
//  5. On the project Desk was started on, "Sign this project's decisions":
//     configVersion "6", with `audit.signingKey` naming a key Desk makes for
//     the project (startup_key.go). It is never pre-selected, and it is
//     offered only where the runtime reads "6" and Desk's custody can keep a
//     key; elsewhere the offer says why.
//
// **Nothing is written before the owner confirms, and what is confirmed is
// what was shown.** One reading of the project is the offer: the
// configuration, every declared document and the lock (PR C's snapshot), and
// `.gitignore` and the audit folder's state. The offer hands back a token: a
// MAC, under the desk's own review key, over the desk, that reading's
// digests, the configuration the upgrade would write, `.gitignore` before and
// after, and the audit folder's state. A confirmation is honoured only where
// a fresh reading, under the desk's review lock, gives the same token.
//
// **The configuration and the first lock go together.** Still under the
// review lock, the upgrade makes the audit folder, writes `.gitignore` and
// `jpack.json`, each only over the bytes the reading saw, and runs the
// runtime's `packs lock`. The lock it writes must pin exactly the upgraded
// configuration and the documents shown. Otherwise, whether the runtime
// refused or anything moved, every file is put back as it was and what the
// upgrade made is removed. `requireReviewed` with no lock of its
// configuration refuses every deciding run, so a project is never left
// holding one without the other. Where the owner chose the signing key, the
// key is made first, and taken away with every file where the upgrade does
// not complete.
//
// **One upgrade at a time on a project, in every Desk process** (issue
// #284): the confirmation takes this project's lock (project_lock.go) before
// its fresh reading and holds it until its last write or its last file put
// back. And a file is put back only where it still holds what the upgrade
// wrote: the lock, where it holds what the upgrade's `packs lock` left.
// Anything else was written since, and is left as it is, and said.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	// upgradeConfirmLimit bounds a confirmation's body: a token and a choice.
	upgradeConfirmLimit = 4 << 10
	gitignoreName       = ".gitignore"
	// deskPrivateIgnore is the line the upgrade adds to `.gitignore`.
	deskPrivateIgnore = ".desk-private/"
	// newestKnownConfigVersion is the newest configuration version Desk knows
	// how to upgrade. A newer one is the runtime's to describe, not Desk's.
	newestKnownConfigVersion = 6
)

// configMember is one top-level member of a configuration, as it is written:
// its name, where its value lies in the bytes, the whitespace before its name,
// and the bytes between its name and its value.
type configMember struct {
	name       string
	start, end int
	sep, colon string
}

// configMembers reads a configuration's top-level members, in the file's own
// order, with where each value lies.
//
// **A duplicate name is refused, not collapsed.** The runtime refuses one
// (`JPS-PROJECT-CONFIG-JSON`), and a rewrite of the first spelling would
// leave the second saying something else. Anything after the closing brace
// but whitespace is refused too.
func configMembers(data []byte) ([]configMember, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return nil, errors.New("it is not a JSON object")
	}
	var members []configMember
	seen := map[string]bool{}
	// Taken before `More`, which reads past whitespace to see what is next.
	before := int(decoder.InputOffset())
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, errors.New("a member name is not a string")
		}
		if seen[name] {
			return nil, fmt.Errorf("it names %q twice", name)
		}
		seen[name] = true
		afterName := int(decoder.InputOffset())
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		end := int(decoder.InputOffset())
		start := end - len(value)
		// Between the previous value (or the brace) and the name: whitespace,
		// at most one comma, and whitespace. The name's own opening quote is
		// the first quote there.
		gap := string(data[before:afterName])
		quote := strings.IndexByte(gap, '"')
		sep := gap[:quote]
		if comma := strings.IndexByte(sep, ','); comma >= 0 {
			sep = sep[comma+1:]
		}
		members = append(members, configMember{name: name, start: start, end: end, sep: sep, colon: string(data[afterName:start])})
		before = end
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("the object is not closed: %w", err)
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return nil, errors.New("the object is not closed")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("there is something after the object")
	}
	return members, nil
}

// configEdit replaces data[start:end] with text.
type configEdit struct {
	start, end int
	text       string
}

// upgradedConfig is data with the gates on, and the names of the members it
// added or changed: configVersion to, `requireReviewed` true, the audit
// directory `.desk-private/audit` where none is declared, with facts,
// `requireComparableFacts` true, and with a signingKey, the audit member's
// `signingKey` naming it ("signingKey" among the names).
//
// **Every other byte is data's own.** A member that is already there is
// changed in place, by its value's bytes alone, and an `audit` member is not
// changed at all. A member that is not there is added in the order a new desk
// writes them (`configVersion`, `requireReviewed`, `requireComparableFacts`,
// `audit`): directly after the nearest member before it in that order that the
// file has, spaced as that member is. So a desk made before PR B, written
// `{"configVersion":"3","packs":{}}`, comes out exactly as a new desk is
// written, and a hand-formatted file keeps its own formatting.
//
// **The signing key goes inside the audit member**: in the one it adds, after
// `dir`; in one that is there, after its last member, spaced as that member
// is, and nothing else in it changed.
//
// The caller has checked that `configVersion` is there, and, with a
// signingKey, that an audit member that is there is an object.
func upgradedConfig(data []byte, members []configMember, to string, facts bool, signingKey string) ([]byte, []string) {
	index := map[string]configMember{}
	for _, member := range members {
		index[member.name] = member
	}
	var edits []configEdit
	var changed []string
	anchor, added := index["configVersion"], ""
	// step changes the member that is there to value where want, or adds it
	// after the anchor; a member that is there is the next one's anchor.
	step := func(name string, want bool, value func(sep, colon string) string) {
		member, ok := index[name]
		if !ok {
			if want {
				added += "," + anchor.sep + strconv.Quote(name) + anchor.colon + value(anchor.sep, anchor.colon)
				changed = append(changed, name)
			}
			return
		}
		if want && string(data[member.start:member.end]) != value(member.sep, member.colon) {
			edits = append(edits, configEdit{member.start, member.end, value(member.sep, member.colon)})
			changed = append(changed, name)
		}
		if added != "" {
			edits = append(edits, configEdit{anchor.end, anchor.end, added})
		}
		anchor, added = member, ""
	}
	literal := func(text string) func(string, string) string { return func(string, string) string { return text } }
	step("configVersion", true, literal(strconv.Quote(to)))
	step("requireReviewed", true, literal("true"))
	step("requireComparableFacts", facts, literal("true"))
	audit, declared := index["audit"]
	step("audit", !declared, func(sep, colon string) string { return auditMember(sep, colon, signingKey) })
	if added != "" {
		edits = append(edits, configEdit{anchor.end, anchor.end, added})
	}
	if signingKey != "" {
		if declared {
			edits = append(edits, signingKeyEdit(data, audit, signingKey))
		}
		changed = append(changed, "signingKey")
	}
	// From the end, so each edit's offsets are still the reading's own.
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := bytes.Clone(data)
	for _, edit := range edits {
		out = slices.Concat(out[:edit.start], []byte(edit.text), out[edit.end:])
	}
	return out, changed
}

// auditMember is the value of an added `audit` member, with signingKey
// after `dir` where it is not "", laid out as the configuration's own members
// are: on one line where they are, and otherwise one level deeper than they
// are.
func auditMember(sep, colon, signingKey string) string {
	inside := []string{`"dir"` + colon + strconv.Quote(deskAuditDir)}
	if signingKey != "" {
		inside = append(inside, `"signingKey"`+colon+quotedPath(signingKey))
	}
	cut := strings.LastIndexByte(sep, '\n')
	if cut < 0 {
		return "{" + strings.Join(inside, ","+sep) + "}"
	}
	newline, indent := sep[:cut+1], sep[cut+1:]
	return "{" + newline + indent + indent + strings.Join(inside, ","+newline+indent+indent) + newline + indent + "}"
}

// signingKeyEdit adds `signingKey` naming path to the audit member that is
// there: after its last member, spaced as that member is, or alone in an
// empty one.
func signingKeyEdit(data []byte, audit configMember, path string) configEdit {
	inside, _ := configMembers(data[audit.start:audit.end])
	if len(inside) == 0 {
		return configEdit{audit.start + 1, audit.start + 1, `"signingKey":` + quotedPath(path)}
	}
	last := inside[len(inside)-1]
	at := audit.start + last.end
	return configEdit{at, at, "," + last.sep + `"signingKey"` + last.colon + quotedPath(path)}
}

// ignoresDeskPrivate reports whether a `.gitignore`'s last rule is exactly
// the line Desk writes, `.desk-private/`.
//
// **Desk does not emulate Git's matching.** Git applies the last rule that
// matches a path, and does not look inside a folder it has excluded, so that
// line, last, excludes `.desk-private` and everything in it whatever comes
// before: `!*/`, `!**` or `!.desk-private/` earlier are overridden by it. Any
// other ending gets the line added at the end. A file that already ignores
// the folder some other way gets a redundant line, which does no harm.
//
// Rules are read as Git reads them: blank lines and comments are not rules,
// and trailing spaces and a CRLF file's carriage return are not part of one.
// Desk reads only this one file and runs no Git: a rule elsewhere (a
// parent's `.gitignore`, `.git/info/exclude`, a global excludes file) has
// less weight than this file's own, so it cannot undo the line.
func ignoresDeskPrivate(data []byte) bool {
	last := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(strings.TrimSuffix(line, "\r"), " ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		last = line
	}
	return last == deskPrivateIgnore
}

// withDeskPrivateIgnored is a `.gitignore` with `.desk-private/` added as its
// last line, in the file's own line ending.
func withDeskPrivateIgnored(data []byte) []byte {
	eol := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		eol = "\r\n"
	}
	out := bytes.Clone(data)
	if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, eol...)
	}
	return append(out, deskPrivateIgnore+eol...)
}

// inGitWorkTree reports whether the project is inside a Git work tree, the
// way Git finds one: a `.git` in the project's folder or in a folder above
// it, that is either a folder holding `HEAD` or a file (a linked work tree's
// or a submodule's). Desk runs no Git to ask.
func (s *Server) inGitWorkTree() bool {
	if gitMark(s.root.Lstat, ".git") {
		return true
	}
	for dir := s.projectDir; ; {
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
		if gitMark(os.Lstat, filepath.Join(dir, ".git")) {
			return true
		}
	}
}

// gitMark reports whether the entry at name marks a Git work tree.
func gitMark(lstat func(string) (fs.FileInfo, error), name string) bool {
	info, err := lstat(name)
	switch {
	case err != nil:
		return false
	case info.Mode().IsRegular():
		return true
	case info.IsDir():
		_, err := lstat(filepath.Join(name, "HEAD"))
		return err == nil
	}
	return false
}

// upgradeUnavailable is why Desk offers no upgrade here, as the page says it.
type upgradeUnavailable struct{ reason string }

func (u *upgradeUnavailable) Error() string { return u.reason }

// gitignorePlan is what the upgrade does to `.gitignore`.
type gitignorePlan struct {
	// state is "add" (a line is added), "create" (the file is made with the
	// line), "ignored" (it already ignores the folder) or "outside" (the
	// project is in no Git work tree).
	state   string
	before  []byte
	present bool
	// after is what is written, or nil where nothing is.
	after []byte
}

// auditPlan is the audit folder the upgraded configuration declares.
type auditPlan struct {
	// State is "create" (Desk makes `.desk-private/audit`, owner-only),
	// "exists" (it is already a folder) or "kept" (the project declares its
	// own, and keeps it).
	State string `json:"state"`
	// Dir is the folder the configuration declares.
	Dir string `json:"dir,omitempty"`
}

// upgradePlan is one reading of the project, and what the upgrade would
// write over it.
type upgradePlan struct {
	schema runtimeSchema
	snap   *reviewSnapshot
	// upgraded is the reading with the upgraded configuration, or nil where
	// the upgrade writes nothing.
	upgraded *reviewSnapshot
	from, to string
	changed  []string
	// gated is whether the configuration already holds deciding runs to a
	// reviewed set and records them: `requireReviewed`, an audit directory,
	// and configVersion 4 or later.
	gated bool
	// facts is "on", "off" (the runtime reads "5", and the configuration does
	// not set it) or "unavailable" (the runtime reads no "5").
	facts string
	// choice is whether this plan sets `requireComparableFacts`.
	choice    bool
	gitignore gitignorePlan
	audit     auditPlan
	// signing is the item "Sign this project's decisions", on the project
	// Desk was started on only (startup_key.go); sign is whether this plan
	// makes the key, at seed, and names it.
	signing *upgradeSigning
	sign    bool
	seed    string
}

// planUpgrade reads the project once and plans the upgrade for the owner's
// choices about `requireComparableFacts` and the signing key. An
// *upgradeUnavailable says why nothing can be offered.
func (s *Server) planUpgrade(schema runtimeSchema, facts, sign bool) (*upgradePlan, error) {
	unavailable := func(format string, args ...any) (*upgradePlan, error) {
		return nil, &upgradeUnavailable{fmt.Sprintf(format, args...)}
	}
	reads := strings.Join(schema.supported, ", ")
	if !slices.Contains(schema.supported, reviewedFromVersion) {
		return unavailable("The runtime this Desk runs (jpack %s) reads configuration versions %s. Turning the gates on needs configuration version 4, which runtime 0.24.0 and later read, so Desk offers no upgrade until the runtime is updated.", schema.version, reads)
	}
	snap, err := s.readSnapshot()
	if err != nil {
		return unavailable("Desk offers no upgrade here, because it could not read every file the first lock would cover: %s.", strings.TrimRight(err.Error(), "."))
	}
	members, err := configMembers(snap.config)
	if err != nil {
		return unavailable("%s is not a configuration Desk can upgrade: %s.", runtimeConfigName, err)
	}
	index := map[string]configMember{}
	for _, member := range members {
		index[member.name] = member
	}
	raw := func(name string) string {
		member, ok := index[name]
		if !ok {
			return ""
		}
		return string(snap.config[member.start:member.end])
	}
	var from string
	if json.Unmarshal([]byte(raw("configVersion")), &from) != nil {
		return unavailable("%s declares no configVersion, so Desk does not upgrade it.", runtimeConfigName)
	}
	number, err := strconv.Atoi(from)
	if err != nil || number < 1 || number > newestKnownConfigVersion || strconv.Itoa(number) != from {
		return unavailable("%s declares configVersion %q, which Desk does not upgrade.", runtimeConfigName, from)
	}
	if !slices.Contains(schema.supported, from) {
		return unavailable("The runtime this Desk runs (jpack %s) reads configuration versions %s, and not %s, which %s declares, so Desk offers no upgrade.", schema.version, reads, from, runtimeConfigName)
	}
	plan := &upgradePlan{schema: schema, snap: snap, from: from, to: from}
	plan.gated = raw("requireReviewed") == "true" && raw("audit") != "" && number >= 4
	switch {
	case raw("requireComparableFacts") == "true":
		plan.facts = "on"
	case slices.Contains(schema.supported, comparableFactsFromVersion):
		plan.facts = "off"
	default:
		plan.facts = "unavailable"
	}
	plan.choice = facts && plan.facts == "off"
	plan.signing, plan.seed = s.planSigning(schema, raw("audit"))
	plan.sign = sign && plan.signing != nil && plan.signing.State == signingOffered
	if plan.gated && !plan.choice && !plan.sign {
		return plan, nil
	}
	if raw("audit") != "" {
		var declared struct {
			Dir string `json:"dir"`
		}
		_ = json.Unmarshal([]byte(raw("audit")), &declared)
		plan.audit = auditPlan{State: "kept", Dir: declared.Dir}
	} else {
		plan.audit = auditPlan{State: "exists", Dir: deskAuditDir}
		for _, part := range []string{".desk-private", deskAuditDir} {
			info, err := s.root.Lstat(part)
			switch {
			case errors.Is(err, fs.ErrNotExist):
				plan.audit.State = "create"
			case err != nil:
				return unavailable("%s could not be read: %s.", part, err)
			case !info.IsDir() || info.Mode()&fs.ModeSymlink != 0:
				return unavailable("%s is not a folder Desk can keep the audit trail in, so it offers no upgrade here.", part)
			}
			if plan.audit.State == "create" {
				break
			}
		}
	}
	plan.to = reviewedFromVersion
	if slices.Contains(schema.supported, comparableFactsFromVersion) {
		plan.to = comparableFactsFromVersion
	}
	// "6" where the key is named, and never a version below the file's own.
	if plan.sign {
		plan.to = signedFromVersion
	}
	if to, _ := strconv.Atoi(plan.to); number > to {
		plan.to = from
	}
	seed := ""
	if plan.sign {
		seed = plan.seed
	}
	config, changed := upgradedConfig(snap.config, members, plan.to, plan.choice, seed)
	// **No file larger than Desk reads back** (`reviewTextLimit`): the review,
	// the decision record and a start's sweep each read jpack.json within it.
	if len(config) > reviewTextLimit {
		return unavailable("%s as the upgrade would write it is larger than the %d bytes Desk reads, so Desk does not upgrade it.", runtimeConfigName, reviewTextLimit)
	}
	plan.upgraded, plan.changed = snap.withConfig(config), changed
	if plan.gitignore, err = s.planGitignore(); err != nil {
		return nil, err
	}
	return plan, nil
}

// planGitignore is what the upgrade does to `.gitignore`, read by the file
// API's rules: one that is a link, or too large to show, is not written.
func (s *Server) planGitignore() (gitignorePlan, error) {
	if !s.inGitWorkTree() {
		return gitignorePlan{state: "outside"}, nil
	}
	data, err := s.readReviewFileWithin(gitignoreName, reviewTextLimit)
	switch {
	case codeOf(err) == CodeNotFound:
		return gitignorePlan{state: "create", after: []byte(deskPrivateIgnore + "\n")}, nil
	case err != nil:
		return gitignorePlan{}, &upgradeUnavailable{fmt.Sprintf("%s could not be read, so Desk offers no upgrade here: %s.", gitignoreName, strings.TrimRight(err.Error(), "."))}
	case ignoresDeskPrivate(data):
		return gitignorePlan{state: "ignored", before: data, present: true}, nil
	}
	return gitignorePlan{state: "add", before: data, present: true, after: withDeskPrivateIgnored(data)}, nil
}

// upgradeToken binds a confirmation to this desk and to one plan: the
// reading, the lock, the configuration the upgrade writes, what it does to
// `.gitignore` (its state, and the bytes it writes there, which are the
// bytes it read with one line added), and the audit folder's state. It is a
// MAC under the desk's own review key, and names its purpose, so a review's
// token never confirms an upgrade.
func (s *Server) upgradeToken(plan *upgradePlan) string {
	lock := "absent"
	if plan.snap.hasLock {
		lock = sha256Digest(plan.snap.lock)
	}
	ignore := [2]string{plan.gitignore.state, "unchanged"}
	if plan.gitignore.after != nil {
		ignore[1] = sha256Digest(plan.gitignore.after)
	}
	payload, _ := json.Marshal(struct {
		Purpose   string    `json:"purpose"`
		Desk      string    `json:"desk"`
		Project   string    `json:"project"`
		Set       reviewSet `json:"set"`
		Lock      string    `json:"lock"`
		Config    string    `json:"config"`
		Gitignore [2]string `json:"gitignore"`
		Audit     auditPlan `json:"audit"`
	}{"upgrade", s.cfg.deskID, s.projectDir, plan.snap.set, lock, plan.upgraded.set.Config, ignore, plan.audit})
	mac := hmac.New(sha256.New, s.reviewKey[:])
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// upgradeAnswer is what the offer shows.
type upgradeAnswer struct {
	// State is "offer" (the upgrade writes something), "unchanged" (nothing
	// to write for this choice) or "unavailable" (Reason says why).
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	// Runtime is the runtime's version, and Reads the configuration versions
	// it reads.
	Runtime string   `json:"runtime,omitempty"`
	Reads   []string `json:"reads,omitempty"`
	Gated   bool     `json:"gated"`
	// ComparableFacts is "on", "off" or "unavailable"; RequireComparableFacts
	// is whether this offer sets it.
	ComparableFacts        string `json:"comparableFacts,omitempty"`
	RequireComparableFacts bool   `json:"requireComparableFacts"`
	From                   string `json:"from,omitempty"`
	To                     string `json:"to,omitempty"`
	// Changes names the members the upgrade adds to jpack.json or changes in
	// it.
	Changes      []string `json:"changes"`
	ConfigBefore string   `json:"configBefore,omitempty"`
	ConfigAfter  string   `json:"configAfter,omitempty"`
	// Gitignore is the plan's state for .gitignore.
	Gitignore string     `json:"gitignore,omitempty"`
	Audit     *auditPlan `json:"audit,omitempty"`
	// Locked is whether the project already keeps a lock, which the new
	// configuration drifts from.
	Locked bool `json:"locked"`
	// Review is the first lock's review, over the upgraded configuration.
	Review *reviewAnswer `json:"review,omitempty"`
	Token  string        `json:"token,omitempty"`
	// SigningKey is the item "Sign this project's decisions", on the project
	// Desk was started on; Sign is whether this offer makes the key and names
	// it.
	SigningKey *upgradeSigning `json:"signingKey,omitempty"`
	Sign       bool            `json:"sign"`
}

// answer describes a plan.
func (plan *upgradePlan) answer() upgradeAnswer {
	answer := upgradeAnswer{State: "unchanged", Runtime: plan.schema.version, Reads: plan.schema.supported, Gated: plan.gated,
		ComparableFacts: plan.facts, RequireComparableFacts: plan.choice, From: plan.from, To: plan.from, Changes: []string{},
		Locked: plan.snap.hasLock, ConfigBefore: string(plan.snap.config), SigningKey: plan.signing, Sign: plan.sign}
	if plan.upgraded != nil {
		answer.State, answer.To, answer.Changes, answer.ConfigAfter = "offer", plan.to, plan.changed, string(plan.upgraded.config)
		answer.Gitignore, answer.Audit = plan.gitignore.state, &plan.audit
	}
	return answer
}

// handleUpgrade answers `GET /api/upgrade`: what turning the gates on would
// write in this project, for the owner's choices about
// `requireComparableFacts` (`?requireComparableFacts=false` declines it) and
// the signing key (`?signingKey=true` chooses it), and a token that confirms
// exactly that. It writes nothing.
func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	facts := r.URL.Query().Get("requireComparableFacts") != "false"
	sign := r.URL.Query().Get("signingKey") == "true"
	dir, refusal := s.reviewRuntime()
	if refusal != "" {
		writeJSON(w, http.StatusOK, upgradeAnswer{State: "unavailable", Reason: refusal, Changes: []string{}})
		return
	}
	answer, err := s.upgradeOffer(r.Context(), dir, facts, sign)
	if err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal, "The upgrade could not be prepared: "+strings.TrimRight(err.Error(), ".")+".")
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

func (s *Server) upgradeOffer(ctx context.Context, dir heldDir, facts, sign bool) (upgradeAnswer, error) {
	schema, err := readRuntimeSchema(ctx, s.cfg.JpackBin, dir)
	if err != nil {
		return upgradeAnswer{State: "unavailable", Reason: "Desk offers no upgrade here, because " + strings.TrimRight(err.Error(), ".") + ".", Changes: []string{}}, nil
	}
	plan, err := s.planUpgrade(schema, facts, sign)
	if unavailable := (*upgradeUnavailable)(nil); errors.As(err, &unavailable) {
		return upgradeAnswer{State: "unavailable", Reason: unavailable.reason, Runtime: schema.version, Reads: schema.supported, Changes: []string{}}, nil
	}
	if err != nil {
		return upgradeAnswer{}, err
	}
	answer := plan.answer()
	if plan.upgraded == nil {
		return answer, nil
	}
	// The first lock's review, over a private copy of the reading with the
	// upgraded configuration in it: the runtime's findings are about what
	// the lock would cover.
	review, err := s.reviewOf(ctx, dir, plan.upgraded, nil)
	if err != nil {
		return upgradeAnswer{}, err
	}
	answer.Review, answer.Token = &review, s.upgradeToken(plan)
	return answer, nil
}

// handleUpgradeConfirm answers `POST /api/upgrade`: write exactly what the
// offer the token names showed, lock it, or write nothing.
//
// It is a `POST` with a JSON body and the desk's bearer, which a cross-site
// page cannot send: the guard checks the session and the Origin, and a
// request a browser marks cross-site is refused besides.
func (s *Server) handleUpgradeConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden, "A cross-site request cannot change this project.")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeJSONCoded(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Send the confirmation as JSON.")
		return
	}
	var request struct {
		Token                  string `json:"token"`
		RequireComparableFacts bool   `json:"requireComparableFacts"`
		SigningKey             bool   `json:"signingKey"`
	}
	data, err := readBounded(r.Body, upgradeConfirmLimit)
	if err != nil || decodeDataJSON(data, &request) != nil || len(request.Token) != 64 {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Confirm the upgrade with the token its offer gave.")
		return
	}
	dir, refusal := s.reviewRuntime()
	if refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	// The desk's review lock around the upgrade alone, released by a defer,
	// so that a panic, which net/http recovers, does not leave it held.
	answer, failure := func() (any, *lockFailure) {
		s.reviewMu.Lock()
		defer s.reviewMu.Unlock()
		return s.upgradeConfirmed(r.Context(), dir, request.Token, request.RequireComparableFacts, request.SigningKey)
	}()
	if failure != nil {
		// **No path reaches the page** (ADR-0010, section 1): the key's, its
		// folders', the project's or any other. The log keeps the message whole.
		message := s.withoutPaths(failure.message)
		if message != failure.message {
			s.log.Printf("desk: the upgrade, as said: %s", failure.message)
		}
		writeJSONCoded(w, failure.status, failure.code, message)
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

// upgradeConfirmed writes what the offer the token names showed, and locks
// it, or puts every file back. The caller holds the desk's review lock.
//
// **Where the offer named a signing key, the key first, then the
// configuration that names it, then the lock that pins it**, as a new desk is
// made: the key is made before anything in the project is written, its
// pathname checked to name it immediately before jpack.json is, and its
// marker removed only once the lock is checked. A failure at any step puts
// every file back and removes the key; where the files could not be put back,
// the key and its marker stay for the next start, which keeps the key only
// where jpack.json names it (startup_key.go).
func (s *Server) upgradeConfirmed(ctx context.Context, dir heldDir, token string, facts, sign bool) (any, *lockFailure) {
	stale := &lockFailure{http.StatusConflict, CodeStale, "The project changed after you reviewed the upgrade, so nothing was written. Review it again."}
	// **This project's lock, from before the fresh reading to the last file
	// put back** (issue #284), released last, after the key's and the held
	// folders'.
	unlock, failure := s.lockProjectFor(ctx, "an upgrade", "written")
	if failure != nil {
		return nil, failure
	}
	defer unlock()
	schema, err := readRuntimeSchema(ctx, s.cfg.JpackBin, dir)
	if err != nil {
		return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was written: " + strings.TrimRight(err.Error(), ".") + "."}
	}
	plan, err := s.planUpgrade(schema, facts, sign)
	if err != nil || plan.upgraded == nil || !hmac.Equal([]byte(s.upgradeToken(plan)), []byte(token)) {
		return nil, stale
	}
	var key *madeKey
	undo := &upgradeUndo{s: s, plan: plan}
	defer undo.close()
	fail := func(failure *lockFailure) (any, *lockFailure) {
		undone := undo.run()
		if undone.problems != nil {
			s.log.Printf("desk: an upgrade that did not complete could not put the project back: %v", undone.problems)
			message := "The upgrade did not complete, and the project could not be put back as it was: " + undone.problems.Error() + ". Check jpack.json, jpack.lock.json and .gitignore before relying on them."
			if undone.keyLeft {
				message += " " + keyLeftWords
			}
			return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, message}
		}
		if undone.keyErr != nil {
			s.log.Printf("desk: an upgrade that did not complete could not remove the signing key it made: %v", undone.keyErr)
			failure.message += " The signing key made for this project could not be removed, and was left with its creation marker in Desk's signing folder, with the project's identity; jpack.json does not name it, and Desk removes it when it next starts."
		}
		return nil, failure
	}
	// **The project's identity before its key** (issue #283): the key is made
	// under the name the project keeps, so that the next start finds it, and
	// recovers a stopped creation, wherever the project is then.
	if plan.sign {
		if err := undo.establishIdentity(); err != nil {
			if errors.Is(err, errIdentityChanged) {
				return fail(&lockFailure{http.StatusConflict, CodeStale, "This project's identity changed after you reviewed the upgrade, so nothing was written. Review it again."})
			}
			s.log.Printf("desk: this project's identity was not written: %v", err)
			return fail(&lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was written, because Desk could not keep this project's identity in its private folder: " + strings.TrimRight(err.Error(), ".") + "."})
		}
		var failure *lockFailure
		if key, failure, undo.creationLeft = s.makeStartupKey(ctx, dir, plan.seed, plan.snap.config); failure != nil {
			return fail(failure)
		}
		undo.key = key
		defer key.close()
	}
	if plan.audit.State == "create" {
		if err := undo.makeAuditFolder(); err != nil {
			return fail(&lockFailure{http.StatusInternalServerError, CodeInternal, "The audit folder could not be made (" + err.Error() + "), so nothing was written."})
		}
	}
	if plan.gitignore.after != nil {
		if err := undo.write(gitignoreName, plan.gitignore.before, plan.gitignore.present, plan.gitignore.after); err != nil {
			return fail(writeFailure(err))
		}
	}
	// **The key is still where the configuration names it**, or nothing names
	// it: the seed's pathname must name the seed found through the folder
	// held, asked at the last moment before jpack.json is published, once its
	// bytes are staged and synced (`upgradeUndo.write`), and again before the
	// marker goes.
	if key != nil {
		keyBetween("upgrade: before naming")
	}
	if err := undo.write(runtimeConfigName, plan.snap.config, true, plan.upgraded.config); err != nil {
		return fail(writeFailure(err))
	}
	undo.locking = true
	lockErr := lockRuntimeProjectAt(ctx, s.cfg.JpackBin, dir)
	locked := false
	if lockErr == nil {
		if after := s.readProjectFile(runtimeLockName); after.err == nil && after.present {
			if pinned, _, err := lockedSet(after.data); err == nil && pinned.equal(plan.upgraded.set) {
				locked = true
			}
		}
	}
	if !locked {
		if lockErr != nil {
			return fail(&lockFailure{http.StatusInternalServerError, CodeInternal, "The runtime did not lock the project (" + strings.TrimRight(lockErr.Error(), ".") + "), so every file was put back as it was."})
		}
		return fail(&lockFailure{http.StatusConflict, CodeStale, "A file changed while the project was being locked, so every file was put back as it was. Review the upgrade again."})
	}
	// The key's pathname, checked again now jpack.json names it and the lock
	// pins it: a folder replaced since the configuration was published leaves
	// it naming a seed that is not there, so every file goes back, and the
	// key, found through the folder held, with them (review round 1 of #261).
	if key != nil {
		keyBetween("upgrade: named")
		if key.stillNamed() != nil {
			return fail(&lockFailure{http.StatusConflict, CodeStale, "Desk's signing folder was replaced while the project was being locked, so every file was put back as it was. Review the upgrade again."})
		}
	}
	result := struct {
		Files                  int    `json:"files"`
		ConfigVersion          string `json:"configVersion"`
		RequireComparableFacts bool   `json:"requireComparableFacts"`
		Gitignore              string `json:"gitignore"`
		Audit                  string `json:"audit"`
		Copies                 string `json:"copies"`
		CopiesProblem          string `json:"copiesProblem,omitempty"`
		// SigningKey is the public half of the key jpack.json now names,
		// where the upgrade made one.
		SigningKey *deskPublicKey `json:"signingKey,omitempty"`
	}{1 + len(plan.upgraded.set.Entries), plan.to, plan.choice, plan.gitignore.state, plan.audit.State, "stored", "", nil}
	// The key is named and pinned: its marker goes. Where it cannot, the next
	// start removes the marker alone, since jpack.json names the key.
	if key != nil {
		if err := key.settle(); err != nil {
			s.log.Printf("desk: this project's new signing key keeps its creation marker; the next start removes it: %v", err)
		}
		result.SigningKey = &key.public
	}
	if made := undo.identity; made != nil && made.record.From != "" {
		s.finishStartupMove(made.root, made.record, made.info)
	}
	if err := s.storeReviewedCopies(plan.upgraded); err != nil {
		s.log.Printf("desk: the reviewed copies were not stored: %v", err)
		result.Copies, result.CopiesProblem = "not-stored", err.Error()
	}
	return result, nil
}

// errUpgradeMoved is a file that no longer holds the bytes the offer read.
var errUpgradeMoved = errors.New("a file changed after you reviewed the upgrade")

// writeFailure is a write the upgrade could not make.
func writeFailure(err error) *lockFailure {
	if errors.Is(err, errMovedBeforePublish) {
		return &lockFailure{http.StatusConflict, CodeStale, "Desk's signing folder was replaced before jpack.json named the key, so every file was put back as it was. Review the upgrade again."}
	}
	if errors.Is(err, errUpgradeMoved) {
		return &lockFailure{http.StatusConflict, CodeStale, "The project changed after you reviewed the upgrade, so every file was put back as it was. Review it again."}
	}
	return &lockFailure{http.StatusInternalServerError, CodeInternal, "The upgrade could not write the project (" + strings.TrimRight(err.Error(), ".") + "), so every file was put back as it was."}
}

// upgradeUndo is what an upgrade has written so far, and how to take it back.
type upgradeUndo struct {
	s    *Server
	plan *upgradePlan
	// made is each folder the upgrade made, outermost first, with the held
	// folder it was made in.
	made []upgradeMade
	// held is each folder the upgrade opened, closed when it is done.
	held []*os.Root
	// written is each file the upgrade replaced, in order.
	written []upgradeWritten
	// locking is set once the runtime may have written a lock.
	locking bool
	// key is the signing key the configuration this upgrade writes names,
	// where it names one: jpack.json is published only while the key's
	// pathname names the key made (`write`).
	key *madeKey
	// identity is the project's identity file, where this upgrade wrote it.
	identity *madeIdentity
	// creationLeft is set where the key's creation failed and left some of
	// what it made: the identity stays beside it.
	creationLeft bool
}

// madeIdentity is an identity file an upgrade wrote: the folder held it is
// in, the file as written, and what it says.
type madeIdentity struct {
	root   *os.Root
	info   os.FileInfo
	record identityRecord
}

// upgradeMade is a folder the upgrade made: its name in the held folder it
// was made in, and what it was when it was made.
type upgradeMade struct {
	in   *os.Root
	name string
	info fs.FileInfo
}

type upgradeWritten struct {
	name    string
	before  []byte
	present bool
	// after is what the upgrade wrote: the bytes it is put back over.
	after []byte
}

// testHookAuditFolderChecked runs after each part of the audit folder's path
// is checked, or made, and before it is opened; testHookBeforeUpgradePublish
// runs after a file the upgrade writes is staged and immediately before it is
// compared again and published. Both are nil outside tests. They are where a
// test is the writer the desk's mutex knows nothing about.
var (
	testHookAuditFolderChecked   func(part string)
	testHookBeforeUpgradePublish func(name, staged string)
)

// makeAuditFolder makes `.desk-private` and `.desk-private/audit`
// owner-only where they are missing, one held folder at a time.
//
// **Through held folders, never by a path.** Each part is looked at, or made,
// in the folder held open before it, then opened, and what was opened must be
// what was looked at: a part replaced by a link meanwhile, even one that stays
// inside the project, is refused, and nothing is made through it. A part that
// is there and is not a folder, or is a link, is refused too.
func (u *upgradeUndo) makeAuditFolder() error {
	current := u.s.root
	for _, part := range strings.Split(deskAuditDir, "/") {
		info, err := current.Lstat(part)
		if errors.Is(err, fs.ErrNotExist) {
			if err = current.Mkdir(part, custodyDirMode); err != nil {
				return err
			}
			if info, err = current.Lstat(part); err != nil {
				return err
			}
			u.made = append(u.made, upgradeMade{current, part, info})
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is not a folder", part)
		}
		if testHookAuditFolderChecked != nil {
			testHookAuditFolderChecked(part)
		}
		next, err := current.OpenRoot(part)
		if err != nil {
			return err
		}
		u.held = append(u.held, next)
		if opened, err := next.Stat("."); err != nil || !os.SameFile(info, opened) {
			return fmt.Errorf("%s changed while it was being opened", part)
		}
		current = next
	}
	return nil
}

// close lets go of the folders the upgrade held.
func (u *upgradeUndo) close() {
	for _, root := range u.held {
		root.Close()
	}
}

// write replaces name with after, under the desk's write lock and by the file
// API's rules for the path, only where it still holds before (or, where it was
// absent, is still absent).
//
// **That is checked after the bytes are staged, immediately before they are
// published**, and not before: an editor the desk's write mutex knows nothing
// about can change the file while the bytes are being staged, and checked
// earlier, that edit was overwritten unseen. A file that no longer holds
// before is left as it is, and nothing is published.
func (u *upgradeUndo) write(name string, before []byte, present bool, after []byte) error {
	u.s.writes.Lock()
	defer u.s.writes.Unlock()
	holds := func() error {
		// The reading read this path through no link, so a link on it now,
		// or anything else the walk refuses, is the project changing after
		// the offer: stale, like changed bytes.
		if err := u.s.refuseSymlinkedPath(name); err != nil {
			return errUpgradeMoved
		}
		current, _, err := u.s.readThroughRootWithin(name, reviewTextLimit)
		switch {
		case present && (err != nil || !bytes.Equal(current, before)):
			return errUpgradeMoved
		case !present && codeOf(err) != CodeNotFound:
			return errUpgradeMoved
		}
		// **The configuration names the key only while the key is where it
		// was made**, asked here, at the last moment before jpack.json is
		// published: a signing folder replaced while its bytes were staged
		// would otherwise leave it naming a seed that is not there (review
		// round 1 of #261).
		if name == runtimeConfigName && u.key.stillNamed() != nil {
			return errMovedBeforePublish
		}
		return nil
	}
	err := u.s.atomicWriteChecked(name, after, func(staged string) error {
		if testHookBeforeUpgradePublish != nil {
			testHookBeforeUpgradePublish(name, staged)
		}
		return holds()
	})
	if err != nil {
		return err
	}
	u.written = append(u.written, upgradeWritten{name, before, present, after})
	return nil
}

// establishIdentity writes the project's identity, where it has none, with
// the name the offer showed its key under (`newStartupKeyName`): in
// `.desk-private/`, made owner-only through the folder held where it is
// missing, never over anything. Where it is written, the stamping settings
// kept under the path's hash move to it once the upgrade is done
// (`finishStartupMove`), and, where the upgrade is put back whole, it is
// removed with what else the upgrade made (`run`). errIdentityChanged where
// the project took another identity since the offer.
func (u *upgradeUndo) establishIdentity() error {
	s := u.s
	if s.startupKept() {
		return nil
	}
	name, err := s.newStartupKeyName()
	if err != nil {
		return err
	}
	if filepath.Base(u.plan.seed) != name+seedSuffix {
		return errIdentityChanged
	}
	info, err := s.root.Lstat(startupIdentityDir)
	if errors.Is(err, fs.ErrNotExist) {
		if err = s.root.Mkdir(startupIdentityDir, custodyDirMode); err != nil {
			return err
		}
		if info, err = s.root.Lstat(startupIdentityDir); err != nil {
			return err
		}
		u.made = append(u.made, upgradeMade{s.root, startupIdentityDir, info})
	}
	if err != nil {
		return err
	}
	private, err := s.openIdentityFolder()
	if err != nil {
		return err
	}
	u.held = append(u.held, private)
	if opened, err := private.Stat("."); err != nil || !os.SameFile(info, opened) {
		return fmt.Errorf("%s changed while it was being opened", startupIdentityDir)
	}
	signing, err := s.assistant.openSigning(false)
	switch {
	case errors.Is(err, errNoSigningDir):
		signing = nil
	case err != nil:
		return err
	default:
		defer signing.Close()
	}
	record := identityRecord{ID: name, Path: s.projectDir}
	if record.From, err = s.legacyStampingToMove(signing); err != nil {
		return err
	}
	written, err := writeIdentity(private, record, nil)
	if errors.Is(err, errIdentityChanged) {
		// Another writer gave the project its identity first: the next offer
		// names the key by that one.
		if found, _, ok, readErr := readIdentity(private); readErr == nil && ok {
			s.setStartup(identityKept, found.ID, found.From, "")
		}
	}
	if err != nil {
		return err
	}
	u.identity = &madeIdentity{root: private, info: written, record: record}
	s.setStartup(identityKept, record.ID, record.From, "")
	return nil
}

// run takes the upgrade back: the previous lock, then each file it wrote,
// last first, then each folder it made, innermost first.
//
// A folder is removed through the held folder it was made in, and only while
// it is still the folder the upgrade made: one that was moved, or replaced by
// a link, is left, and so is one that now holds something the upgrade did not
// put there, which is not empty. Each is said.
func (u *upgradeUndo) run() undoResult {
	var problems []string
	u.s.writes.Lock()
	if u.locking {
		// **Only a lock of the configuration this upgrade wrote** (review
		// round 1 of #296): a lock read after the runtime ran is not taken
		// for the one it wrote.
		if err := u.s.restoreLock(u.plan.snap.lock, u.plan.snap.hasLock, u.plan.upgraded.set.Config); err != nil {
			problems = append(problems, runtimeLockName+": "+err.Error())
		}
	}
	for i := len(u.written) - 1; i >= 0; i-- {
		written := u.written[i]
		// **Only over what this upgrade wrote** (issue #284): a file another
		// writer changed since, another Desk process's upgrade among them,
		// is left as it is.
		err := u.s.putBack(written.name, wroteBytes(written.after), projectFile{data: written.before, present: written.present})
		if err != nil {
			problems = append(problems, written.name+": "+err.Error())
		}
	}
	u.s.writes.Unlock()
	var result undoResult
	// **The key, and only then the identity it is kept under** (review
	// round 1 of #296). The key goes where every file was put back; where
	// anything was not, it is left with its marker, for the next start to
	// decide by its marker and jpack.json. The identity goes only once
	// nothing is kept under its name in Desk's signing folder: a key and a
	// marker are never left without the identity their recovery is bound to,
	// whatever fails, and wherever a stop comes.
	if u.key != nil {
		if len(problems) > 0 {
			result.keyLeft = true
		} else {
			keyBetween("upgrade: files put back")
			if err := u.key.unmake(); err != nil {
				result.keyLeft, result.keyErr = true, err
			}
		}
	}
	keptIdentity := false
	if u.identity != nil && (result.keyLeft || u.creationLeft) {
		keptIdentity = true
		u.s.log.Printf("desk: this project's identity is kept beside the signing key, or what was made of it, that this upgrade left")
	}
	if made := u.identity; made != nil && !keptIdentity {
		keyBetween("upgrade: key taken back")
		found, err := made.root.Lstat(startupIdentityName)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil || !os.SameFile(found, made.info):
			problems = append(problems, startupIdentityDir+"/"+startupIdentityName+": it is no longer the file the upgrade wrote, so it was left")
			keptIdentity = true
		default:
			if err := made.root.Remove(startupIdentityName); err != nil {
				problems = append(problems, startupIdentityDir+"/"+startupIdentityName+": "+err.Error())
				keptIdentity = true
			} else {
				u.s.setStartup(identityNone, "", "", "")
			}
		}
	}
	for i := len(u.made) - 1; i >= 0; i-- {
		made := u.made[i]
		if keptIdentity && made.in == u.s.root && made.name == startupIdentityDir {
			continue
		}
		info, err := made.in.Lstat(made.name)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err == nil && !os.SameFile(info, made.info):
			err = fmt.Errorf("it is no longer the folder the upgrade made, so it was left")
		case err == nil:
			err = made.in.Remove(made.name)
		}
		if err != nil {
			problems = append(problems, made.name+": "+err.Error())
		}
	}
	if len(problems) > 0 {
		result.problems = errors.New(strings.Join(problems, "; "))
	}
	return result
}

// undoResult is what taking an upgrade back did: each file, lock or folder
// left, and why (problems); whether the signing key it made, or anything
// kept under the project's name, is left in Desk's signing folder (keyLeft);
// and why the key could not be removed, where every file was put back
// (keyErr).
type undoResult struct {
	problems error
	keyLeft  bool
	keyErr   error
}

// keyLeftWords is what an upgrade that left its key says of it.
const keyLeftWords = "The signing key Desk made for this project was left with its creation marker, and with the project's identity: when Desk next starts, it removes the marker alone where jpack.json names the key, the key with it where jpack.json is as it was before this upgrade, and otherwise leaves both and says so."
