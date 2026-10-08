package desk

// The archive of keys: Desk never deletes a signing seed on its own (the
// maintainer's decision of 2026-10-08, after the third pass of the ADR-0010
// line audit, issue #292).
//
// # The rule
//
// Three passes of the line audit found defects in two decision tables, when a
// start's recovery, its sweep or a live rotation may remove a seed, and the
// third showed a window no further check can close (issue #323). So no path
// in Desk removes a seed, a next seed or a list of public keys on its own:
// not a creation that stopped, not the start's sweep, not a rotation's undo
// or its promotion, not an upgrade taken back. Where Desk once removed one,
// it moves it, under the signing folder's lock, into the archive beside it:
//
//	<signing folder>/archive/<identity>/<trail or none>-<sequence or none>-<UTC time>.<kind>
//
// `<signing folder>` is `<configuration>/secrets/signing`, or, for the keys
// Desk keeps for Runner, `<configuration>/secrets/signing/runner`; each
// archive folder is made 0700 and held to the rule its signing folder is
// (`ensureOwnedDirectoryIn`). `<kind>` is the live name's own suffix: `seed`,
// `next.seed`, `keys.jsonl`, and the marker that explains them, `creating` or
// `rotating`. A move is a rename within one folder tree: the bytes are the
// ones the runtime wrote, never read by Desk, and the owner can move a file
// back.
//
// # One journal line beside it
//
// Before each move, Desk appends one line to `archive/<identity>/archive.jsonl`
// (`archiveLine`), synced: the file, the live name it came from, the rule
// that archived it and, in Desk's words, what Desk could not decide. The
// line comes first, so no archived file is without its reason; a move that
// then fails leaves a line naming a file that is not there, which the
// decision record says.
//
// # The owner removes
//
// The decision record lists every archived file (`archiveListing`): its
// identity, the trail and sequence its name records, when it was archived and
// why, and offers Remove on each, confirmed with a token that binds the
// entry's digest (`archiveToken`): its journal line's bytes and the file by
// its device and inode. That is the only removal of a key in Desk
// (`handleRemoveArchived`), and it is made under the signing folder's lock,
// only while the name still holds the file the token names, with a journal
// line of its own.
//
// # What this does not defend against
//
// Custody's residual, as everywhere in the signing folder: a process running
// as this user can read, move or delete what is archived. An archived seed is
// still a key: whoever reads it can sign as it.

import (
	"bufio"
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
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// archiveDirName is the folder, in a signing folder, that holds what Desk
	// archived there.
	archiveDirName = "archive"
	// archiveJournalName is each identity's journal of its archive.
	archiveJournalName = "archive.jsonl"
	// archiveJournalLimit is the most of a journal Desk reads, and
	// archiveLineLimit the longest line it writes or reads: a line is its
	// file's name, a live name, a rule and a sentence.
	archiveJournalLimit = 1 << 20
	archiveLineLimit    = 4 << 10
	// archiveListLimit is the most entries the decision record lists; More
	// counts the rest.
	archiveListLimit = 200
	// archiveRemoveLimit bounds a removal's body: a scope, an identity, a
	// file's name and a token.
	archiveRemoveLimit = 4 << 10
	// archiveTimeLayout is the UTC time in an archived file's name.
	archiveTimeLayout = "20060102T150405.000000000Z"
	// archiveContentLimit is the most of an archived file Desk reads to know
	// its bytes by their digest: a seed is 65 bytes, a list at most
	// keysFileLimit, a marker 512.
	archiveContentLimit = 64 << 10
)

// The kinds of file Desk archives, by the suffix of the live name: a next
// seed's before a seed's, which it ends in.
var archiveKinds = []struct{ suffix, kind string }{
	{nextSeedSuffix, "next.seed"},
	{seedSuffix, "seed"},
	{keysSuffix, "keys.jsonl"},
	{creatingSuffix, "creating"},
	{rotatingSuffix, "rotating"},
}

// archiveFileForm is an archived file's name, as Desk writes it.
var archiveFileForm = regexp.MustCompile(`^(none|[0-9a-f]{32})-(none|[1-9][0-9]{0,15})-([0-9]{8}T[0-9]{6}\.[0-9]{9}Z)\.(seed|next\.seed|keys\.jsonl|creating|rotating)$`)

// archiveIdentityForm is a name Desk keeps keys under: a made desk's id, or
// the identity of the project Desk was started on.
var archiveIdentityForm = regexp.MustCompile(`^[0-9a-f]{32}(?:[0-9a-f]{32})?$`)

// The rules that archive a file (`archiveLine.Rule`): which path moved it.
const (
	// archiveCreationStopped: a creation of a key that stopped before its
	// desk, its project's jpack.json or its Runner named the key.
	archiveCreationStopped = "creation-stopped"
	// archiveNeverPublished: the start's sweep, for a creation whose marker,
	// or the project's jpack.json, shows it was never published.
	archiveNeverPublished = "creation-never-published"
	// archiveRotationStopped: a rotation that stopped before the runtime was
	// asked to rotate, or that the runtime refused.
	archiveRotationStopped = "rotation-stopped"
	// archiveRotationNotWritten: a rotation whose trail's signature sidecar
	// does not name its next key.
	archiveRotationNotWritten = "rotation-not-written"
	// archivePromoted: the key in force before a rotation, once the runtime
	// handed signing over to the next one.
	archivePromoted = "rotation-promoted"
	// archiveRotationConflict: a rotation whose trail was moved aside or
	// replaced while its next key was promoted.
	archiveRotationConflict = "rotation-conflict"
	// archiveRotationOrphaned: a rotation's journal under a name no project
	// or desk open here holds, on this project's trail (issue #320).
	archiveRotationOrphaned = "rotation-orphaned"
	// archiveOwnerRemoved: the owner removed the file (`archiveLine.Event`
	// "removed").
	archiveOwnerRemoved = "owner"
)

// archiveLine is one line of an identity's journal of its archive: an event,
// "archived" or "removed", the archived file's name in the folder, the live
// name it was archived from, the rule, Desk's sentence, for a removal the
// generation of the file's entry it consumed (review round 1 of #327,
// finding 2), and the time, UTC. The members are in that order, as
// json.Marshal writes them, and a line in any other spelling is read as no
// line.
type archiveLine struct {
	Version    string `json:"version"`
	Event      string `json:"event"`
	File       string `json:"file"`
	From       string `json:"from,omitempty"`
	Rule       string `json:"rule"`
	Why        string `json:"why,omitempty"`
	Generation int    `json:"generation,omitempty"`
	At         string `json:"at"`
}

// line is the journal line's one spelling.
func (l archiveLine) line() []byte {
	data, _ := json.Marshal(l)
	return append(data, '\n')
}

// archived is what an archive move records of the file it moves: the
// identity it is kept under, the trail and the sequence it is the key of
// where Desk knows them, the rule that moves it and Desk's sentence on what
// it could not decide. The sentence names no path.
type archived struct {
	identity string
	trail    string
	sequence int64
	rule     string
	why      string
}

// archiveKindOf is the kind of the live name, and the identity it is kept
// under; false where name is not one Desk archives.
func archiveKindOf(name string) (kind, identity string, ok bool) {
	for _, each := range archiveKinds {
		if id, found := strings.CutSuffix(name, each.suffix); found {
			return each.kind, id, archiveIdentityForm.MatchString(id)
		}
	}
	return "", "", false
}

// archiveName is the name an archived file takes: trail, sequence, the time
// and its kind, each "none" where Desk does not know it.
func archiveName(trail string, sequence int64, at time.Time, kind string) string {
	if !keyIDForm.MatchString(trail) {
		trail = "none"
	}
	seq := "none"
	if sequence >= 1 {
		seq = strconv.FormatInt(sequence, 10)
	}
	return trail + "-" + seq + "-" + at.UTC().Format(archiveTimeLayout) + "." + kind
}

// archiveClock is the time an archived file is named by. A variable only so
// that a test can make two moves in one instant.
var archiveClock = time.Now

// errNotArchivable is a name that is not a key, a list or a marker of one.
var errNotArchivable = errors.New("it is not a key, a list of public keys or a marker Desk keeps")

// archiveFolder makes, where missing, and holds to custody's rule the
// archive folder of identity in d: `archive/` and `archive/<identity>/`, each
// a real directory, the user's, open to nobody else.
func (d *signingDir) archiveFolder(identity string) (string, error) {
	if !archiveIdentityForm.MatchString(identity) {
		return "", errors.New("its identity is not one Desk keeps keys under")
	}
	if err := ensureOwnedDirectoryIn(d.root, d.path, archiveDirName); err != nil {
		return "", err
	}
	folder := filepath.Join(archiveDirName, identity)
	if err := ensureOwnedDirectoryIn(d.root, d.path, folder); err != nil {
		return "", err
	}
	return folder, nil
}

// archive moves name, the file found as info in the folder d holds, into the
// archive of its identity, with one journal line first (`archiveLine`), and
// answers the archived file's name. The move is made only while name still
// holds info, and never over anything: an archived name taken already is
// passed over for the next instant. Both folders are synced after. The
// caller holds the signing folder's lock, where one can be taken here.
func (d *signingDir) archive(name string, info os.FileInfo, record archived) (string, error) {
	kind, identity, ok := archiveKindOf(name)
	if !ok || info == nil {
		return "", fmt.Errorf("%s was not archived: %w", name, errNotArchivable)
	}
	if record.identity != "" && record.identity != identity {
		return "", fmt.Errorf("%s was not archived: it is not kept under the identity named", name)
	}
	return d.archiveAs(name, kind, identity, info, record)
}

// archiveAs is archive for a file whose name does not say what it is, a
// list of public keys staged under a name of its own: kind and identity are
// given.
func (d *signingDir) archiveAs(name, kind, identity string, info os.FileInfo, record archived) (string, error) {
	if info == nil {
		return "", fmt.Errorf("%s was not archived: %w", name, errNotArchivable)
	}
	folder, err := d.archiveFolder(identity)
	if err != nil {
		return "", fmt.Errorf("%s was not archived: its archive folder: %w", name, err)
	}
	found, err := d.root.Lstat(name)
	if err != nil || !os.SameFile(found, info) {
		return "", fmt.Errorf("%s was not archived: it is not the file Desk inspected", name)
	}
	at := archiveClock()
	var file string
	for attempt := 0; ; attempt++ {
		file = archiveName(record.trail, record.sequence, at.Add(time.Duration(attempt)), kind)
		if _, err := d.root.Lstat(filepath.Join(folder, file)); errors.Is(err, fs.ErrNotExist) {
			break
		} else if err != nil || attempt >= 64 {
			return "", fmt.Errorf("%s was not archived: no free name in its archive folder", name)
		}
	}
	line := archiveLine{Version: "1", Event: "archived", File: file, From: name, Rule: record.rule, Why: d.words(record.why), At: at.UTC().Format(time.RFC3339Nano)}
	if err := d.appendArchiveLine(folder, line); err != nil {
		return "", fmt.Errorf("%s was not archived: its journal line could not be written: %w", name, err)
	}
	keyBetween("archive: line written")
	if err := d.root.Rename(name, filepath.Join(folder, file)); err != nil {
		return "", fmt.Errorf("%s was not archived: %w", name, err)
	}
	_ = syncPrivateDirectory(d.root)
	if held, err := d.root.OpenRoot(folder); err == nil {
		_ = syncPrivateDirectory(held)
		held.Close()
	}
	d.say("desk: %s was moved to Desk's archive of keys, as %s of %s (%s): %s", name, file, identity, record.rule, record.why)
	return file, nil
}

// words is a sentence for an archive line with no path in it: the signing
// folder and every folder on the way to it replaced whole, as the runtime and
// Go spell them (`pathSpans`), and then every other path from a root
// (`withoutAbsolutePaths`). A caller that holds the server cleans its
// sentence with the server's own spans first (`withoutPaths`).
func (d *signingDir) words(message string) string {
	const held = "\x00"
	message = strings.ReplaceAll(message, held, "")
	return strings.ReplaceAll(withoutAbsolutePaths(replaceSpans(message, pathSpans(d.path, true, held))), held, "…")
}

// say writes one line to the server's log, or to Go's where the folder was
// held without one: every archive move is said.
func (d *signingDir) say(format string, args ...any) {
	if d.logf != nil {
		d.logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// appendArchiveLine appends line to the journal of the archive folder, whole,
// in one write, synced: a regular file, the user's, open to nobody else, made
// 0600 where it is not there yet. A line longer than Desk reads back is never
// written.
func (d *signingDir) appendArchiveLine(folder string, line archiveLine) error {
	data := line.line()
	if len(data) > archiveLineLimit {
		return errors.New("the line is longer than Desk reads back")
	}
	name := filepath.Join(folder, archiveJournalName)
	file, err := d.root.OpenFile(name, os.O_WRONLY|os.O_APPEND|os.O_CREATE|openNoFollow, custodyFileMode)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("the journal is not a regular file")
	}
	if err := ownerOnlyFile(name, info.Mode()); err != nil {
		return err
	}
	if err := ownedByUs(name, info); err != nil {
		return err
	}
	if info.Size()+int64(len(data)) > archiveJournalLimit {
		return errors.New("the journal holds as much as Desk reads")
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

// archiveMade archives what a creation made of its key, the list, the seed
// and the marker, in that order, each only while its name still holds the
// file made, through the folder held; it stops at the first that cannot be
// moved, so the marker stays wherever anything else does, for the next
// start's sweep. A marker left alone, with no list and no seed, says nothing
// Desk could not decide, and is removed. It answers the files archived. A nil
// key made nothing.
func (k *madeKey) archiveMade(record archived) ([]string, error) {
	if k == nil {
		return nil, nil
	}
	if k.keys == nil && k.seed == nil && !k.dir.holds(k.seedName) {
		return nil, k.dir.removeMade(k.markerName, k.marker)
	}
	var moved []string
	for _, made := range []struct {
		name string
		info os.FileInfo
	}{{k.keysName, k.keys}, {k.seedName, k.seed}, {k.markerName, k.marker}} {
		info := made.info
		if info == nil && made.name == k.seedName {
			// A seed the runtime wrote before it failed is the creation's
			// own: its name was free under the marker before the run.
			found, err := k.dir.root.Lstat(made.name)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return moved, fmt.Errorf("%s could not be inspected: %w", made.name, err)
			}
			info = found
		}
		if info == nil {
			continue
		}
		file, err := k.dir.archive(made.name, info, record)
		if err != nil {
			return moved, err
		}
		moved = append(moved, file)
	}
	return moved, nil
}

// holds is whether name, in the folder held, holds anything, or could not be
// inspected: what is not shown absent is taken as there.
func (d *signingDir) holds(name string) bool {
	_, err := d.root.Lstat(name)
	return !errors.Is(err, fs.ErrNotExist)
}

/* The decision record's list, and the owner's Remove ------------------------ */

// The scopes an archived file is listed under: the signing folder's own
// archive, or the archive of the keys Desk keeps for Runner.
const (
	archiveScopeDesk   = "desk"
	archiveScopeRunner = "runner"
)

// archivedKey is one archived file, as the decision record lists it: the
// scope and identity it is kept under, its name, its kind, the trail and
// sequence its name records, when it was archived, the rule and Desk's
// sentence, whether it is kept under this desk's own name, and the token
// that confirms its removal. Missing is a journal line whose file is not in
// the archive; it has no token.
type archivedKey struct {
	Scope    string `json:"scope"`
	Identity string `json:"identity"`
	File     string `json:"file"`
	Kind     string `json:"kind"`
	Trail    string `json:"trail,omitempty"`
	Sequence int64  `json:"sequence,omitempty"`
	At       string `json:"at"`
	Rule     string `json:"rule,omitempty"`
	Why      string `json:"why"`
	Own      bool   `json:"own,omitempty"`
	Missing  bool   `json:"missing,omitempty"`
	Token    string `json:"token,omitempty"`
	// line is the journal line that archived it, object the file by its
	// device and inode, and digest the SHA-256 of its bytes now (review
	// round 1 of #327, finding 1): what the token binds. A file whose bytes
	// could not be read now has no digest, and is offered no Remove.
	// generation is how many lines of the journal name the file (finding 2):
	// a removal adds one, so a token is spent once it is used, and a file put
	// back after it asks for a new confirmation.
	line       []byte
	object     string
	digest     string
	generation int
}

// auditArchive is the decision record's list of what Desk archived: given
// with every answer of the panel's route, a refusal among them, since what is
// archived is the owner's to see whatever the trail's state. Problem says
// why a part of it could not be read now.
type auditArchive struct {
	Entries []archivedKey `json:"entries"`
	More    int           `json:"more,omitempty"`
	Problem string        `json:"problem,omitempty"`
}

// What the decision record says of an archived file Desk's journal does not
// explain, and of a line whose file is not there.
const (
	archiveNoLineWords  = "Desk's journal of its archive holds no line for this file, so Desk cannot say why it is here."
	archiveMissingWords = "Desk's journal names this file, and it is not in the archive now: its move did not happen, or it was removed outside Desk."
	archiveUnreadWords  = "Desk could not read its archive of keys now: %s."
	archiveNoBytesWords = "Desk could not read this file's bytes now, so it offers no Remove for it."
	archiveBackWords    = "Desk's journal says this file was removed on your word, and it is here: the removal did not finish, or the file was put back since."
)

// archiveListing is the decision record's list of every file Desk archived,
// in the signing folder's archive and in Runner's, newest first; nil where
// there is no archive at all. A part that cannot be read now is said in
// Problem, never taken for an empty archive.
func (s *Server) archiveListing() *auditArchive {
	if s.assistant == nil || !s.assistant.usable() {
		return nil
	}
	dir, err := s.assistant.openSigning(false)
	if errors.Is(err, errNoSigningDir) {
		return nil
	}
	if err != nil {
		return &auditArchive{Entries: []archivedKey{}, Problem: fmt.Sprintf(archiveUnreadWords, strings.TrimRight(s.custodyWords(err.Error()), "."))}
	}
	defer dir.Close()
	listing := &auditArchive{Entries: []archivedKey{}}
	var problems []string
	for _, scope := range []string{archiveScopeDesk, archiveScopeRunner} {
		held := dir
		if scope == archiveScopeRunner {
			runner, err := dir.openRunnerDir()
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				problems = append(problems, s.custodyWords(err.Error()))
				continue
			}
			defer runner.Close()
			held = runner
		}
		entries, err := held.archivedIn(scope, s.signingKeyName())
		if err != nil {
			problems = append(problems, s.custodyWords(err.Error()))
		}
		listing.Entries = append(listing.Entries, entries...)
	}
	slices.SortStableFunc(listing.Entries, func(a, b archivedKey) int { return strings.Compare(b.At, a.At) })
	if len(listing.Entries) > archiveListLimit {
		listing.More = len(listing.Entries) - archiveListLimit
		listing.Entries = listing.Entries[:archiveListLimit]
	}
	for i := range listing.Entries {
		if !listing.Entries[i].Missing && listing.Entries[i].digest != "" {
			listing.Entries[i].Token = s.archiveToken(listing.Entries[i])
		}
	}
	if len(problems) > 0 {
		listing.Problem = fmt.Sprintf(archiveUnreadWords, strings.TrimRight(strings.Join(problems, "; "), "."))
	}
	if len(listing.Entries) == 0 && listing.Problem == "" {
		return nil
	}
	return listing
}

// openRunnerDir holds `runner/` in the signing folder, for reading, where it
// is there: the same rule the signing folder is held to. fs.ErrNotExist where
// there is none.
func (d *signingDir) openRunnerDir() (*signingDir, error) {
	info, err := d.root.Lstat(runnerSigningDirName)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(d.path, runnerSigningDirName)
	if err := safeDirectory(path, info, true); err != nil {
		return nil, err
	}
	root, err := d.root.OpenRoot(runnerSigningDirName)
	if err != nil {
		return nil, err
	}
	if opened, err := root.Stat("."); err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("%s changed between being checked and being opened, and was not used", path)
	}
	return &signingDir{root: root, path: path, logf: d.logf}, nil
}

// archivedIn is every file in the archive of the signing folder d holds,
// listed under scope, each with its journal's word; own names this desk's own
// identity. An archive that is not there lists nothing.
func (d *signingDir) archivedIn(scope, own string) ([]archivedKey, error) {
	info, err := d.root.Lstat(archiveDirName)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := safeDirectory(filepath.Join(d.path, archiveDirName), info, true); err != nil {
		return nil, err
	}
	folders, err := readDirNames(d.root, archiveDirName)
	if err != nil {
		return nil, err
	}
	var entries []archivedKey
	var problems []error
	for _, identity := range folders {
		if !archiveIdentityForm.MatchString(identity) {
			continue
		}
		found, err := d.archivedOf(scope, identity)
		if err != nil {
			problems = append(problems, err)
		}
		for i := range found {
			found[i].Own = scope == archiveScopeDesk && identity == own
		}
		entries = append(entries, found...)
	}
	return entries, errors.Join(problems...)
}

// readDirNames is the names in folder, through root, at most
// registryReadLimit of them.
func readDirNames(root *os.Root, folder string) ([]string, error) {
	listing, err := root.Open(folder)
	if err != nil {
		return nil, err
	}
	defer listing.Close()
	entries, err := listing.ReadDir(registryReadLimit)
	if err != nil && !errors.Is(err, io.EOF) && len(entries) == 0 {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names, nil
}

// archivedOf is every file in the archive of identity, and every line of its
// journal whose file is not there and that no removal settled: each file held
// to its name's form and to custody's rule for a key (a regular file, the
// user's, open to nobody else), with the last journal line that archived it.
func (d *signingDir) archivedOf(scope, identity string) ([]archivedKey, error) {
	folder := filepath.Join(archiveDirName, identity)
	info, err := d.root.Lstat(folder)
	if err != nil {
		return nil, err
	}
	if err := safeDirectory(filepath.Join(d.path, folder), info, true); err != nil {
		return nil, err
	}
	lines, journalErr := d.readArchiveJournal(folder)
	names, err := readDirNames(d.root, folder)
	if err != nil {
		return nil, err
	}
	var entries []archivedKey
	present := map[string]bool{}
	for _, name := range names {
		if name == archiveJournalName {
			continue
		}
		parts := archiveFileForm.FindStringSubmatch(name)
		if parts == nil {
			continue
		}
		file, err := d.root.Lstat(filepath.Join(folder, name))
		if err != nil || !file.Mode().IsRegular() || ownerOnlyFile(name, file.Mode()) != nil || ownedByUs(name, file) != nil {
			continue
		}
		present[name] = true
		entry := archivedKey{Scope: scope, Identity: identity, File: name, Kind: parts[4], object: identityKey(file)}
		if parts[1] != "none" {
			entry.Trail = parts[1]
		}
		if parts[2] != "none" {
			entry.Sequence, _ = strconv.ParseInt(parts[2], 10, 64)
		}
		if at, err := time.Parse(archiveTimeLayout, parts[3]); err == nil {
			entry.At = at.UTC().Format(time.RFC3339)
		}
		if found, ok := lines.archived[name]; ok {
			entry.Rule, entry.Why, entry.line = found.record.Rule, found.record.Why, found.raw
		} else {
			entry.Why = archiveNoLineWords
		}
		entry.generation = lines.events[name]
		if lines.removed[name] {
			entry.Why = archiveBackWords + " " + entry.Why
		}
		if digest, err := d.contentDigest(filepath.Join(folder, name), file); err == nil {
			entry.digest = digest
		} else {
			entry.Why += " " + archiveNoBytesWords
		}
		entries = append(entries, entry)
	}
	for _, name := range lines.order {
		if present[name] || lines.removed[name] {
			continue
		}
		found := lines.archived[name]
		parts := archiveFileForm.FindStringSubmatch(name)
		entry := archivedKey{Scope: scope, Identity: identity, File: name, Kind: parts[4], Rule: found.record.Rule, Why: archiveMissingWords + " " + found.record.Why, Missing: true}
		if at, err := time.Parse(time.RFC3339Nano, found.record.At); err == nil {
			entry.At = at.UTC().Format(time.RFC3339)
		}
		entries = append(entries, entry)
	}
	if journalErr != nil {
		return entries, fmt.Errorf("the journal of the archive of %s could not be read whole: %w", identity, journalErr)
	}
	return entries, nil
}

// archiveJournal is a journal as read: for each archived file, its last line
// that archived it; whether a later line removed it; how many lines name it;
// and the files in the order their first line names them.
type archiveJournal struct {
	archived map[string]archivedLine
	removed  map[string]bool
	events   map[string]int
	order    []string
}

// archivedLine is one line that archived a file: its record and its bytes.
type archivedLine struct {
	record archiveLine
	raw    []byte
}

// readArchiveJournal reads the journal of folder whole, under the rule Desk
// keeps a private file by: a line in its one spelling counts, a line that is
// not, or that no newline ends, does not. A journal that is not there has no
// line.
func (d *signingDir) readArchiveJournal(folder string) (archiveJournal, error) {
	journal := archiveJournal{archived: map[string]archivedLine{}, removed: map[string]bool{}, events: map[string]int{}}
	data, _, err := readPrivateFile(d.root, filepath.Join(folder, archiveJournalName), archiveJournalLimit)
	if errors.Is(err, fs.ErrNotExist) {
		return journal, nil
	}
	if err != nil {
		return journal, err
	}
	reader := bufio.NewReaderSize(bytes.NewReader(data), archiveLineLimit+1)
	for {
		raw, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = reader.ReadSlice('\n')
			}
			continue
		}
		if err != nil {
			return journal, nil
		}
		var line archiveLine
		if json.Unmarshal(raw, &line) != nil || !bytes.Equal(line.line(), raw) || line.Version != "1" || !archiveFileForm.MatchString(line.File) {
			continue
		}
		journal.events[line.File]++
		switch line.Event {
		case "archived":
			if _, seen := journal.archived[line.File]; !seen {
				journal.order = append(journal.order, line.File)
			}
			journal.archived[line.File] = archivedLine{record: line, raw: slices.Clone(raw)}
			delete(journal.removed, line.File)
		case "removed":
			journal.removed[line.File] = true
		}
	}
}

// contentDigest is the SHA-256 of the bytes of name, the file found as info
// in the folder d holds, read whole within archiveContentLimit through a
// descriptor that is that file (review round 1 of #327, finding 1). Desk
// reads a seed's bytes here only to know them by their digest: the digest is
// bound into a token and compared, and is never shown, logged or sent.
func (d *signingDir) contentDigest(name string, info os.FileInfo) (string, error) {
	file, err := d.root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || info != nil && !os.SameFile(opened, info) {
		return "", fmt.Errorf("%s is not the file Desk inspected", name)
	}
	data, err := readBounded(file, archiveContentLimit)
	if err != nil {
		return "", fmt.Errorf("%s could not be read whole within %d bytes", name, archiveContentLimit)
	}
	return sha256Digest(data), nil
}

// archiveToken binds the owner's Remove to one archived file as the decision
// record listed it: its scope, identity and name, the digest of the journal
// line that archived it, the file by its device and inode, and its bytes, by
// their digest (review round 1 of #327, finding 1: a file written again in
// place keeps its inode), and the generation of its entry (finding 2: a
// removal adds a line, so a token is spent once used). It is a MAC under the
// desk's own review key, and names its purpose, so no other token confirms
// it.
func (s *Server) archiveToken(entry archivedKey) string {
	payload, _ := json.Marshal(struct {
		Purpose    string `json:"purpose"`
		Desk       string `json:"desk"`
		Project    string `json:"project"`
		Scope      string `json:"scope"`
		Identity   string `json:"identity"`
		File       string `json:"file"`
		Line       string `json:"line"`
		Object     string `json:"object"`
		Content    string `json:"content"`
		Generation int    `json:"generation"`
	}{"remove-archived-key", s.cfg.deskID, s.projectDir, entry.Scope, entry.Identity, entry.File, sha256Digest(entry.line), entry.object, entry.digest, entry.generation})
	mac := hmac.New(sha256.New, s.reviewKey[:])
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// handleRemoveArchived answers `POST /api/audit/key/archive/remove`: the
// owner's word to remove one archived file, confirmed with the token the
// decision record gave for it, or nothing changed. It is the one removal of a
// key in Desk.
func (s *Server) handleRemoveArchived(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden, "A cross-site request cannot remove an archived key.")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeJSONCoded(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Send the confirmation as JSON.")
		return
	}
	var request struct {
		Scope    string `json:"scope"`
		Identity string `json:"identity"`
		File     string `json:"file"`
		Token    string `json:"token"`
	}
	data, err := readBounded(r.Body, archiveRemoveLimit)
	if err != nil || decodeDataJSON(data, &request) != nil || request.Scope != archiveScopeDesk && request.Scope != archiveScopeRunner ||
		!archiveIdentityForm.MatchString(request.Identity) || !archiveFileForm.MatchString(request.File) || len(request.Token) != 64 {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Confirm the removal with the token the decision record gave.")
		return
	}
	if failure := s.removeArchived(r.Context(), request.Scope, request.Identity, request.File, request.Token); failure != nil {
		writeJSONCoded(w, failure.status, failure.code, failure.message)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		State string `json:"state"`
	}{"removed"})
}

// removeArchived is the owner's removal, the transaction itself: under the
// desk's key lock and the signing folder's, each released by a deferred call
// when it returns and never held while an answer is written. The file is
// listed afresh, its token made again from what is found now, and removed
// only while its name holds the file the token names, after a journal line
// that says the owner removed it.
func (s *Server) removeArchived(ctx context.Context, scope, identity, file, token string) *lockFailure {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	dir, err := s.assistant.openSigning(false)
	if err != nil {
		s.log.Printf("desk: an archived key could not be removed, because the signing folder could not be opened: %v", err)
		return &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was removed: Desk could not open the folder it keeps signing keys in."}
	}
	defer dir.Close()
	unlock, err := lockSigningWithin(ctx, dir, signingLockWait)
	switch {
	case errors.Is(err, errSigningBusy):
		return &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was removed: " + signingBusyWords}
	case err != nil:
		s.log.Printf("desk: an archived key could not be removed, because the signing folder's lock was not taken: %v", err)
		return &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was removed: Desk removes an archived key only under the lock of its signing folder, and none can be taken here."}
	}
	defer unlock()
	held := dir
	if scope == archiveScopeRunner {
		runner, err := dir.openRunnerDir()
		if err != nil {
			return &lockFailure{http.StatusConflict, CodeStale, "That archived file is not in Desk's archive now, so nothing was removed. Check the decision record again."}
		}
		defer runner.Close()
		held = runner
	}
	entries, _ := held.archivedIn(scope, s.signingKeyName())
	index := slices.IndexFunc(entries, func(entry archivedKey) bool {
		return entry.Identity == identity && entry.File == file && !entry.Missing
	})
	if index < 0 {
		return &lockFailure{http.StatusConflict, CodeStale, "That archived file is not in Desk's archive now, so nothing was removed. Check the decision record again."}
	}
	entry := entries[index]
	if entry.digest == "" || !hmac.Equal([]byte(s.archiveToken(entry)), []byte(token)) {
		return &lockFailure{http.StatusConflict, CodeStale, "That archived file changed after the decision record showed it, so nothing was removed. Check the decision record again."}
	}
	folder := filepath.Join(archiveDirName, identity)
	name := filepath.Join(folder, file)
	keyBetween("archive: removal confirmed")
	found, err := held.root.Lstat(name)
	if err != nil || identityKey(found) != entry.object {
		return &lockFailure{http.StatusConflict, CodeStale, "That archived file changed after the decision record showed it, so nothing was removed. Check the decision record again."}
	}
	// **Its bytes, again, immediately before the removal** (review round 1
	// of #327, finding 1), under the signing folder's lock.
	if now, err := held.contentDigest(name, found); err != nil || now != entry.digest {
		return &lockFailure{http.StatusConflict, CodeStale, "That archived file changed after the decision record showed it, so nothing was removed. Check the decision record again."}
	}
	// **The confirmation is spent here, under the lock** (review round 1 of
	// #327, finding 2): the line names the generation it consumed, and adds
	// one, so the same token never confirms another removal, of this file
	// put back or of any other.
	removed := archiveLine{Version: "1", Event: "removed", File: file, Rule: archiveOwnerRemoved, Generation: entry.generation, At: archiveClock().UTC().Format(time.RFC3339Nano)}
	if err := held.appendArchiveLine(folder, removed); err != nil {
		s.log.Printf("desk: the archived file %s of %s was not removed, because its journal line could not be written: %v", file, identity, err)
		return &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was removed: Desk could not record the removal in its archive's journal."}
	}
	if err := held.removeArchivedFile(name, found); err != nil {
		s.log.Printf("desk: the archived file %s of %s could not be removed: %v", file, identity, err)
		return &lockFailure{http.StatusInternalServerError, CodeInternal, "The archived file could not be removed now. Check the decision record again."}
	}
	_ = syncPrivateDirectory(held.root)
	s.log.Printf("desk: on the owner's word, the archived %s file %s of %s was removed", entry.Kind, file, identity)
	return nil
}

// removeArchivedFile removes name, an archived file in the folder held, only
// while it is info: the owner's removal (`removeArchived`), the one place a
// key leaves Desk's custody.
func (d *signingDir) removeArchivedFile(name string, info os.FileInfo) error {
	found, err := d.root.Lstat(name)
	if err != nil || !os.SameFile(found, info) {
		return fmt.Errorf("%s is not the file the token names", name)
	}
	return d.root.Remove(name)
}

// withoutPathsInArchive is the archive's list, its sentences passed through
// clean.
func withoutPathsInArchive(listing *auditArchive, clean func(string) string) *auditArchive {
	if listing == nil {
		return nil
	}
	shown := *listing
	shown.Problem = clean(shown.Problem)
	shown.Entries = slices.Clone(shown.Entries)
	for i := range shown.Entries {
		shown.Entries[i].Why = clean(shown.Entries[i].Why)
	}
	return &shown
}
