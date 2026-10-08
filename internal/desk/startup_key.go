package desk

// The signing key of the project Desk was started on (ADR-0010, section 1,
// and the maintainer's answer to its question 2; delivery row 4).
//
// # What it is
//
// A desk Desk makes is written signed (desks.go). The project Desk was
// started on is the owner's own, and is never changed without the owner's
// word: it is offered the key through the upgrade step (upgrade.go), as an
// item of its own, "Sign this project's decisions", never pre-selected, with
// its costs listed before the confirmation:
//
//   - the home path in a committed file: `audit.signingKey` names the seed by
//     its absolute path;
//   - `packs validate` failing in CI, in any checkout where the key is not;
//   - runtimes before the floor (0.26.0) refusing the project, which is
//     written at configVersion "6".
//
// # Where it is kept
//
// As a desk's key is (signing.go), at `<config>/secrets/signing/<name>.seed`,
// with its list of public keys `<name>.keys.jsonl` and, while it is made, its
// creation marker `<name>.creating`. `<name>` is the project's identity
// (startup_identity.go, `signingKeyName`): 64 hexadecimal characters kept in
// its `.desk-private/project.json`, which move with the project; a project
// that has none is named by the hex SHA-256 of its resolved path, for what
// Desk kept under that name before. No desk id is 64 characters long, so the
// two never meet.
//
// # When it is offered
//
// Only on the project Desk was started on, and only where the same two
// conditions a new desk's creation asks hold: the runtime reads "6", and
// Desk's custody can keep a key. Besides those, Desk offers no key where the
// runtime would not sign with it: where `JPACK_SIGNING_KEY` is inherited,
// which the runtime takes over the configuration's key; where the audit
// member turns its chain off; where the signing folder is inside the project,
// whose own directory the runtime refuses on a key's path. Where jpack.json
// already names a key, the item says so and offers nothing. Each refusal is
// one sentence, in the creation's words.
//
// # How it is made
//
// On the owner's confirmation, under the upgrade's token, under this
// project's lock (project_lock.go), and under the signing folder's lock
// (signing_lock.go) from before the marker to its removal: the project's
// identity first, where it has none, with the name the offer showed; then the
// key (`generateDeskKey`, the same checks and list as a made desk's, and a
// marker that binds the creation to this project and this transaction,
// `startupCreation`); then the upgrade's own writes, `jpack.json` at "6" with
// `audit.signingKey` naming the seed, and its lock. The seed's pathname must
// still name the seed found immediately before `jpack.json` is written. A
// failure at any step puts every file back and removes the key; where the
// files could not be put back, the key and its marker are left, for the next
// start's sweep to decide, by whether `jpack.json` names the key.
//
// # The next start
//
// A stopped upgrade leaves the marker. The start's sweep
// (`sweepUnfinishedKeys`) reads the project's `jpack.json`: where it names
// the seed, the upgrade wrote it, and only the marker is removed; where it
// names no key, or another, the list, the seed and the marker are removed,
// only where the marker binds the creation to this project and this
// transaction (`creationBound`: its identity, held by no other folder, its
// folder, and the jpack.json it set out to replace, all as found), and
// otherwise left, and said; where it cannot be read now, nothing is. A marker of another project's name is left for a
// start on that project, wherever that project is now; and a marker under the
// path's hash, which a project that was moved away left as well as one that
// was not, loses only itself, where jpack.json names its seed (issue #283).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// What the item says where it offers nothing, in the creation's words
// (`unsignedByRuntime`, `unsignedByCustody`).
const (
	// keyNotOfferedByRuntime: the runtime's version, and the first that reads
	// "6".
	keyNotOfferedByRuntime = "This project is not offered a signing key: a project names its signing key at configVersion 6, and the runtime this Desk runs (jpack %s) does not read it. A runtime of %s or later does."
	// keyNotOfferedByCustody: why custody keeps no key, in words with no path.
	keyNotOfferedByCustody = "This project is not offered a signing key, because Desk could not keep one for it: %s."
	// keyNotOfferedInherited: the runtime signs with an inherited key.
	keyNotOfferedInherited = "This project is not offered a signing key: JPACK_SIGNING_KEY is set where Desk was started, and the runtime signs this project's records with the key it names, not with one jpack.json names."
	// keyNotOfferedUnchained: the audit member turns its chain off.
	keyNotOfferedUnchained = "This project is not offered a signing key: its audit member turns the chain off, and the runtime signs only a chained trail."
	// keyNotOfferedAuditShape: an audit member that is not an object.
	keyNotOfferedAuditShape = "This project is not offered a signing key: its audit member is not an object Desk can add a key to."
)

// What the item is (`upgradeSigning.State`).
const (
	// signingOffered: the owner can choose it.
	signingOffered = "offered"
	// signingNamed: jpack.json already names a signing key.
	signingNamed = "named"
	// signingNotOffered: Reason says why not.
	signingNotOffered = "unavailable"
)

// upgradeSigning is the item "Sign this project's decisions", as the offer
// shows it. It is given on the project Desk was started on only.
type upgradeSigning struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// signingKeyName is the name Desk keeps this desk's signing key, and its
// stamping settings, under: a desk Desk made by its id, and the project Desk
// was started on by its identity (`startupName`, startup_identity.go); ""
// where that identity could not be read now, which names nothing kept.
func (s *Server) signingKeyName() string {
	if s.cfg.deskID != "" {
		return s.cfg.deskID
	}
	return s.startupName()
}

// startupKey is whether name is the name the project Desk was started on
// keeps its key under, on that project's own server: what the start's sweep
// and its recovery of rotations act on besides the desks' ids.
func (s *Server) startupKey(name string) bool {
	return s.cfg.deskID == "" && name != "" && name == s.signingKeyName()
}

// planSigning is the item for this project, from what the runtime reads and
// the audit member as jpack.json spells it ("" where it has none), and the
// absolute path of the seed the configuration would name where it is
// offered. It changes nothing. It is nil on a desk Desk made.
func (s *Server) planSigning(schema runtimeSchema, audit string) (*upgradeSigning, string) {
	if s.cfg.deskID != "" {
		return nil, ""
	}
	notOffered := func(reason string) (*upgradeSigning, string) {
		return &upgradeSigning{State: signingNotOffered, Reason: reason}, ""
	}
	if audit != "" {
		members, err := configMembers([]byte(audit))
		if err != nil {
			return notOffered(keyNotOfferedAuditShape)
		}
		for _, member := range members {
			switch {
			case member.name == "signingKey":
				return &upgradeSigning{State: signingNamed}, ""
			case member.name == "chain" && audit[member.start:member.end] == "false":
				return notOffered(keyNotOfferedUnchained)
			}
		}
	}
	if !slices.Contains(schema.supported, signedFromVersion) {
		return notOffered(fmt.Sprintf(keyNotOfferedByRuntime, schema.version, auditRuntimeFloor))
	}
	if s.inheritsSigningKey() {
		return notOffered(keyNotOfferedInherited)
	}
	if s.startupShared() {
		return notOffered("This project is not offered a signing key: " + sharedWords + ".")
	}
	if s.startupUnresolved() {
		return notOffered("This project is not offered a signing key: " + unresolvedWords + ".")
	}
	seed, err := s.startupSeedPath()
	if err != nil {
		return notOffered(fmt.Sprintf(keyNotOfferedByCustody, strings.TrimRight(s.custodyWords(err.Error()), ".")))
	}
	return &upgradeSigning{State: signingOffered}, seed
}

// startupSeedPath is the absolute path the project's seed would be made at,
// under its identity or the name the offer shows until it has one
// (`newStartupKeyName`), where Desk's custody can keep it there now: the
// signing folder is one custody accepts, or is not there yet and would be
// made; its path can be named in jpack.json and is not inside the project;
// and nothing is kept under that name. It reads, and makes nothing.
func (s *Server) startupSeedPath() (string, error) {
	name, err := s.newStartupKeyName()
	if err != nil {
		return "", err
	}
	if !s.startupKept() {
		if err := s.identityFolderUsable(); err != nil {
			return "", err
		}
	}
	dir, err := s.assistant.openSigning(false)
	var folder string
	switch {
	case errors.Is(err, errNoSigningDir):
		folder = filepath.Join(s.assistant.dir, secretsDirName, signingDirName)
	case err != nil:
		return "", err
	default:
		defer dir.Close()
		folder = dir.path
		for _, kept := range []string{name + creatingSuffix, name + seedSuffix, name + keysSuffix} {
			if _, err := dir.root.Lstat(kept); !errors.Is(err, fs.ErrNotExist) {
				if kept == name+creatingSuffix {
					return "", errors.New("a signing key's creation for this project did not finish, and Desk keeps that key, its list and its marker until it can tell that it was this project's own; a key is never written over anything")
				}
				return "", errors.New("Desk's signing folder already holds a key, or an unfinished one, under this project's name, and a key is never written over anything")
			}
		}
	}
	switch {
	case !utf8.ValidString(folder):
		return "", errors.New("the path of Desk's signing folder is not valid UTF-8, which jpack.json cannot name")
	case pathContains(s.projectDir, folder):
		return "", errors.New("Desk's signing folder is inside this project's folder, and the runtime refuses a key there")
	}
	return filepath.Join(folder, name+seedSuffix), nil
}

// makeStartupKey makes the project's key at seed, the path the confirmed
// offer's configuration names: through `generateDeskKey`, in the signing
// folder custody holds, under the signing folder's lock, which the key holds
// until it is closed. It answers why where it made none, in words the
// confirmation passes through its redaction (`handleUpgradeConfirm`);
// nothing in the project has been written then.
//
// left is whether a creation that failed left some of what it made of the
// key, which the upgrade keeps the project's identity beside.
func (s *Server) makeStartupKey(ctx context.Context, project heldDir, seed string, replaces []byte) (key *madeKey, failure *lockFailure, left bool) {
	name := s.signingKeyName()
	if !s.startupBound() || filepath.Base(seed) != name+seedSuffix {
		return nil, &lockFailure{http.StatusConflict, CodeStale, "This project's identity is not the one the offer named its key by, so nothing was written. Review the upgrade again."}, false
	}
	dir, err := s.assistant.openSigning(true)
	if err != nil {
		s.log.Printf("desk: no signing key was made for this project: %v", err)
		return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, fmt.Sprintf("Nothing was written, because Desk could not keep a signing key for this project: %s.", strings.TrimRight(s.custodyWords(err.Error()), "."))}, false
	}
	if filepath.Join(dir.path, name+seedSuffix) != seed {
		dir.Close()
		return nil, &lockFailure{http.StatusConflict, CodeStale, "Desk's signing folder is not where the offer named it, so nothing was written. Review the upgrade again."}, false
	}
	// The signing folder's lock, from before the marker until the marker is
	// removed (`madeKey.close`), so that no other Desk process's sweep removes
	// the key meanwhile (issue #230), as a new desk's creation holds it.
	unlock, err := lockSigningWithin(ctx, dir, signingLockWait)
	if errors.Is(err, errSigningBusy) {
		dir.Close()
		return nil, &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was written: " + signingBusyWords}, false
	}
	if err != nil {
		// **No key without the lock** (review round 1 of #327, finding 4).
		dir.Close()
		s.log.Printf("desk: no signing key was made for this project, because the signing folder's lock was not taken: %v", err)
		return nil, &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was written: " + noSigningLockWords}, false
	}
	// Released however the creation ends before the key holds the lock, a
	// panic included, as a stopped process releases it.
	handedOver := false
	defer func() {
		if !handedOver {
			unlock()
			dir.Close()
		}
	}()
	key, err = generateKeyMarked(ctx, s.cfg.JpackBin, project, dir, name, startupCreation{ID: name, Project: s.projectDir, Config: sha256Digest(replaces)}.line())
	if err != nil {
		s.log.Printf("desk: this project's signing key was not generated: %v", err)
		// Its words pass the confirmation's redaction, which takes the key's
		// path, its folders' and the project's whole, before any other path.
		return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was written: " + strings.TrimRight(err.Error(), ".") + "."}, errors.As(err, new(keyMadeLeft))
	}
	key.unlock, handedOver = unlock, true
	return key, nil, false
}

// startupSeedNamed is nil where the project's jpack.json names, by its audit
// member's signingKey, the seed Desk keeps for it: Desk's own path to the
// seed, and that path naming seed, the file found through dir, the signing
// folder Desk holds. Otherwise it says why, in words with no path: a rotation
// renames the next key over that seed, which signs for the project only where
// jpack.json names that very file (review round 1 of #261).
func (s *Server) startupSeedNamed(dir *signingDir, seed os.FileInfo) error {
	name := s.signingKeyName() + seedSuffix
	named, err := s.startupKeyNamed(filepath.Join(dir.path, name))
	switch {
	case err != nil:
		return errors.New("its jpack.json could not be read now")
	case !named:
		return errors.New("its jpack.json names another file, or none")
	case dir.namesFile(name, seed) != nil:
		return errors.New("the path its jpack.json names is not the key Desk found in its signing folder")
	}
	return nil
}

// tokenSeed binds a rotation's confirmation, on the project Desk was started
// on, to the very seed file the panel read: one replaced since, even by a
// copy holding the same key, makes the confirmation stale. "" on a desk Desk
// made.
func (s *Server) tokenSeed(reading *keyReading) string {
	if s.cfg.deskID != "" {
		return ""
	}
	return identityKey(reading.seed)
}

// startupKeyNamed is whether the project's jpack.json names seed as its
// signing key, read under the file API's rules: false where there is no
// jpack.json, or it names no key or another; an error where it could not be
// read now, or is not a configuration Desk can read, and so cannot say.
func (s *Server) startupKeyNamed(seed string) (bool, error) {
	data, err := s.readReviewFileWithin(runtimeConfigName, reviewTextLimit)
	if codeOf(err) == CodeNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var config struct {
		Audit *struct {
			SigningKey *string `json:"signingKey"`
		} `json:"audit"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return false, fmt.Errorf("%s is not a configuration Desk can read: %w", runtimeConfigName, err)
	}
	return config.Audit != nil && config.Audit.SigningKey != nil && *config.Audit.SigningKey == seed, nil
}

// startupCreation is the marker of the startup project's key while it is
// made (review round 1 of #296): one line binding the creation to the
// project it was made for, and to the transaction, by the identity, the
// project's resolved path and the digest of the jpack.json the upgrade set
// out to replace.
//
//	{"id":"<64 hex>","project":"<resolved path>","config":"sha256:<64 hex>"}
type startupCreation struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Config  string `json:"config"`
}

// line is the marker's one spelling.
func (c startupCreation) line() []byte {
	data, _ := json.Marshal(c)
	return append(data, '\n')
}

// startupCreationLimit is the most of a creation's marker Desk reads.
const startupCreationLimit = 8 << 10

// creationBound is "" where the unfinished creation of the key kept as id,
// whose marker was inspected as marker, is this project's and this
// transaction's: the identity is this project's own and held by no other
// folder (`startupBound`), and the marker, read whole, in its one spelling,
// names that identity, this project's folder and the digest of the
// jpack.json found now, which is the one the upgrade set out to replace, so
// its key was never named. Otherwise it says why, in words with no path, and
// the creation's key is not removed.
func (s *Server) creationBound(dir *signingDir, id string, marker os.FileInfo) string {
	if !s.startupBound() {
		if s.startupShared() {
			return sharedWords
		}
		if s.startupUnresolved() {
			return unresolvedWords
		}
		return "a name made from a path, or one Desk cannot read now, is not bound to one project, so the key may be one a project moved away from here still names"
	}
	data, opened, err := readPrivateFile(dir.root, id+creatingSuffix, startupCreationLimit)
	if err != nil || !os.SameFile(opened, marker) {
		return "its marker could not be read now"
	}
	var record startupCreation
	if json.Unmarshal(data, &record) != nil || !bytes.Equal(record.line(), data) {
		return "its marker does not say which project and which jpack.json the creation was for"
	}
	config, err := s.readReviewFileWithin(runtimeConfigName, reviewTextLimit)
	switch {
	case record.ID != id:
		return "its marker names another identity"
	case record.Project != s.projectDir:
		return "its marker names another folder: the creation was for a project there, or this project before it was moved"
	case err != nil:
		return "jpack.json could not be read now, or is not there"
	case record.Config != sha256Digest(config):
		return "jpack.json is not the file the upgrade set out to replace, and does not name the key"
	}
	return ""
}
