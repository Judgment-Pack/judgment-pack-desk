package desk

// Rotating a desk's signing key, on the owner's word (ADR-0010, section 1,
// "Rotating it"), and finishing or undoing a rotation a stop cut short.
//
// # What a rotation is
//
// The runtime's `jpack audit key rotate --next <seed>` appends one
// key-rotation line to the trail's signature sidecar, made with the key in
// force and naming the next key's public key and the trail's last line, `at`.
// The records up to `at` are signed with the old key, and the records after
// it with the next. From that line on the old key signs nothing, and the next
// key signs nothing until the project names it: records written in between
// are unsigned, never refused. A desk's `jpack.json` names its key by
// `<config>/secrets/signing/<desk id>.seed`, so Desk makes the next key the
// one named by renaming it over that name. `jpack.json` never changes, and no
// lock is written. The project Desk was started on, where its upgrade made a
// key, is rotated the same way, under its own name (`signingKeyName`, its
// identity, startup_identity.go), wherever this file says `<desk id>`.
//
// # The steps, under the desk's key lock and the signing folder's lock
//
// Only a confirmed request of the owner's reaches them (`handleRotateKey`):
// the panel's answer carries a token, a MAC under the desk's own review key
// over the list of public keys and the key the panel read, and a rotation is
// made only where a fresh reading gives the same token. From that reading to
// the last step, it holds the signing folder's lock (signing_lock.go), which
// every change to keys there takes, in this Desk process or another; it waits
// a bounded time for it, and then refuses. Each step goes through the signing
// folder Desk holds:
//
//  1. `<desk id>.rotating`, a marker, 0600, written never over anything,
//     journalling the step about to be made and the trail the rotation is
//     made on (`rotationJournal`, issue #285): "generate" here, "rotate" with
//     the next key before step 3, "finish" with the runtime's sequence after
//     it, each written whole and synced before the step;
//  2. `audit key generate <signing>/<desk id>.next.seed --format json`, with
//     the folder's pathname checked to name the folder held before the run,
//     and the seed's after it;
//  3. `audit key rotate --next <that seed> --config jpack.json --format
//     json`, in the desk's folder; its `at` is read;
//  4. the list of public keys written whole with the next key appended, `at`
//     its sequence and the trail it was rotated on recorded in its line
//     (issue #285): staged, synced and renamed over the old list, after a
//     check that the old list is still the file and the bytes read;
//  5. the next seed renamed over the current seed's name, in the same folder,
//     after a check that the list is still the file and the bytes written,
//     and the seed's pathname checked to name the file renamed;
//  6. the marker removed.
//
// # Was the line written? The sidecar says
//
// A runtime's refusal is not taken alone as proof that nothing was written: a
// write that failed after its line was whole would be refused too. Wherever
// Desk must know whether step 3 wrote the line, after a failed step 3 and at
// every start for each marker left, it reads the marker's journal first: a
// rotation journalled at "generate" never reached the runtime, and its next
// seed goes whatever any sidecar says. Otherwise it reads the trail's
// signature sidecar, under the trail's lock, as the trail's download does
// (`readSidecar`), only where it is the trail the journal names, by the
// trail identity its lines name; a trail moved aside or replaced since has
// no say, and both keys and the list are kept, and said. It asks the
// runtime for the next seed's public key (`audit key public`), which must be
// the one the journal names:
//
//   - **Written**, where the sidecar's last key-rotation line hands over to
//     the next key from the current one: steps 4 to 6 are made.
//   - **Not written**, where no line of the sidecar names the next key, as the
//     key a rotation hands over to or from, or as the key of a record's
//     signature: the next seed and the marker are removed. That is the case
//     after the runtime refuses because the trail has no chained record yet,
//     its last line is incomplete, or the current key is not in force.
//   - **Nothing left**, where there is no next seed, and the list, the current
//     seed and the sidecar agree as the panel requires (`checkKeysAgainst`): a
//     stop before step 2, or after step 5. The marker alone is removed.
//   - **Cannot tell**, where anything that decides it could not be read: the
//     sidecar, unreadable, refused or locked; a seed missing, or one the
//     runtime will not read; or the list. Nothing is changed, the marker
//     stays, and the panel says that a rotation did not finish, and why.
//
// A marker an earlier Desk left empty names no trail: it is finished where
// the sidecar's last rotation hands over to its next key, which no other
// trail's can, and otherwise left as it is.
//
// At start the recovery takes the signing folder's lock, asking again once,
// a bounded time, where another Desk process holds it (issue #286); where it
// is still held, or none can be taken here, it changes nothing, and leaves
// every marker for the next start.
//
// # A trail begun after a rotation
//
// The list Desk keeps names every key the desk has had, each key a rotation
// added with the trail identity the rotation was made on (step 4). A trail
// moved aside and begun again is signed from its first record by the key
// Desk kept, which is that trail's first key: the list is held to the new
// trail's sidecar from the last key whose rotation recorded another trail
// (`trailFirst`, `checkKeysAgainst`), and only the new trail's keys are given
// to `audit verify` and shown for a holder (`keysOfTrail`). A rotation that
// recorded this trail, or none, must be in this sidecar.
//
// The current seed is never removed. A seed is never removed because a read
// failed, and never while the sidecar names it. What is removed is removed
// through the folder held, only while its name still holds the file inspected,
// the marker last.
//
// # What a rotation does not do
//
// It revokes nothing: whoever holds the old key can still sign as it, and a
// verifier refuses that only with its own `--revoked`. The rename removes the
// old seed's name, not its bytes from the disk. A lost key cannot be rotated
// away from: rotation needs the key in force.

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
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	// rotatingSuffix follows a desk's id in the name of the marker a
	// rotation keeps until it ends.
	rotatingSuffix = ".rotating"
	// nextSeedSuffix follows a desk's id in the name of the next key while a
	// rotation makes it.
	nextSeedSuffix = ".next.seed"
	// rotateConfirmLimit bounds a confirmation's body: a token.
	rotateConfirmLimit = 4 << 10
	// sidecarLineLimit is the longest sidecar line the runtime reads whole
	// (runtime 0.27.1, `maxSidecarLineBytes`); a longer one is unreadable.
	sidecarLineLimit = 4096
	// sidecarIntegerLimit is the bound every integer of the sidecar is held
	// under (runtime 0.27.1, `maxSafeInteger`, 2^53 - 1).
	sidecarIntegerLimit = 1<<53 - 1
)

var (
	signatureForm = regexp.MustCompile(`^[0-9a-f]{128}$`)
	recordForm    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// errKeysChanged is a list of public keys that is not the file, or not the
// bytes, a rotation read.
var errKeysChanged = errors.New("the list of public keys changed after it was read")

/* The signature sidecar ------------------------------------------------------ */

// sidecarRotation is one key-rotation line of the trail's signature sidecar:
// the trail line it follows, the key that made it, by keyId, and the public
// key it hands over to.
type sidecarRotation struct {
	At    int64
	KeyID string
	Next  string
}

// sidecarReading is what Desk reads of a signature sidecar: each key-rotation
// line, in order; the keys, by keyId, that made a record's signature; and
// whether a record's signature follows the last key rotation. Desk reads which
// key each line names and checks no signature: that is `audit verify`'s.
type sidecarReading struct {
	rotations []sidecarRotation
	signers   map[string]bool
	// signedSinceRotation is whether a record's signature follows the last
	// key-rotation line, which is whether a record was written after it.
	signedSinceRotation bool
	// trail is the trail identity every line it reads names, the runtime's
	// 32 hexadecimal characters; "" where no line names one, or where two
	// lines name two (mixed): no one trail (issue #285).
	trail string
	mixed bool
}

// noteTrail takes the trail identity one line names.
func (r *sidecarReading) noteTrail(trail string) {
	switch {
	case r.mixed:
	case r.trail == "":
		r.trail = trail
	case r.trail != trail:
		r.trail, r.mixed = "", true
	}
}

// oneTrail is the trail identity the sidecar names, where it names one.
func (r sidecarReading) oneTrail() (string, bool) {
	return r.trail, r.trail != "" && !r.mixed
}

// last is the sidecar's last key-rotation line, where it has one.
func (r sidecarReading) last() (sidecarRotation, bool) {
	if len(r.rotations) == 0 {
		return sidecarRotation{}, false
	}
	return r.rotations[len(r.rotations)-1], true
}

// names is whether any line of the sidecar names key: as the key a rotation
// hands over to or was made by, or as the key of a record's signature. A key
// named so has been in force.
func (r sidecarReading) names(key deskPublicKey) bool {
	if r.signers[key.KeyID] {
		return true
	}
	for _, rotation := range r.rotations {
		if rotation.Next == key.PublicKey || rotation.KeyID == key.KeyID {
			return true
		}
	}
	return false
}

// readSidecarLines reads a signature sidecar's lines as the runtime's
// verifier reads them: only lines a newline ends, so a last line a write left
// incomplete is no line; a line longer than sidecarLineLimit unreadable; and
// each line one JSON object of exactly the seven members of its kind, each
// once and of its form (`readSidecarLine`). A line of no shape it reads names
// nothing.
func readSidecarLines(r io.Reader) (sidecarReading, error) {
	reader := bufio.NewReaderSize(r, sidecarLineLimit+1)
	reading := sidecarReading{signers: map[string]bool{}}
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// Longer than any line the runtime reads: passed over to its end.
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = reader.ReadSlice('\n')
			}
			if err == nil {
				continue
			}
		}
		if errors.Is(err, io.EOF) {
			return reading, nil
		}
		if err != nil {
			return sidecarReading{}, err
		}
		reading.read(line[:len(line)-1])
	}
}

// read takes one sidecar line, without its newline.
func (r *sidecarReading) read(line []byte) {
	if len(line) == 0 || len(line) > sidecarLineLimit || !json.Valid(line) {
		return
	}
	members, ok := exactMembers(line)
	if !ok || len(members) != 7 {
		return
	}
	var version, kind, keyID, trail, signature string
	if !sidecarString(members["sidecarVersion"], &version) || version != "1" ||
		!sidecarString(members["kind"], &kind) ||
		!sidecarString(members["keyId"], &keyID) || !keyIDForm.MatchString(keyID) ||
		!sidecarString(members["trail"], &trail) || !keyIDForm.MatchString(trail) ||
		!sidecarString(members["signature"], &signature) || !signatureForm.MatchString(signature) {
		return
	}
	switch kind {
	case "record-signature":
		var sequence int64
		var record string
		if !sidecarInteger(members["sequence"], &sequence) || !sidecarString(members["record"], &record) || !recordForm.MatchString(record) {
			return
		}
		r.signers[keyID] = true
		r.signedSinceRotation = true
		r.noteTrail(trail)
	case "key-rotation":
		var at int64
		var next string
		if !sidecarInteger(members["at"], &at) || !sidecarString(members["next"], &next) || !publicKeyForm.MatchString(next) {
			return
		}
		r.rotations = append(r.rotations, sidecarRotation{At: at, KeyID: keyID, Next: next})
		r.signedSinceRotation = false
		r.noteTrail(trail)
	}
}

// exactMembers is a JSON object's members, where it is one object with no
// member named twice.
func exactMembers(element []byte) (map[string]json.RawMessage, bool) {
	decoder := json.NewDecoder(bytes.NewReader(element))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, false
	}
	members := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok {
			return nil, false
		}
		if _, twice := members[name]; twice {
			return nil, false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, false
		}
		members[name] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, false
	}
	return members, true
}

// sidecarString reads a member that must be a JSON string.
func sidecarString(raw json.RawMessage, into *string) bool {
	return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, into) == nil
}

// sidecarInteger reads a member that must be an integer written in digits
// alone, no sign, fraction, exponent or leading zero, from 1 to under 2^53.
func sidecarInteger(raw json.RawMessage, into *int64) bool {
	text := string(raw)
	if text == "" || text[0] == '0' || strings.Trim(text, "0123456789") != "" {
		return false
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n >= sidecarIntegerLimit {
		return false
	}
	*into = n
	return true
}

// readSidecar reads this desk's signature sidecar as the trail's download
// reads it (`snapshotAuditFile`): through the project's root, refusing links
// and a second name, and between two writes, under the trail's shared lock,
// up to the size read under it.
//
// **Not there is read as empty, and nothing else is.** A sidecar that is not
// there, in an audit directory that is there or not, has no line, and a
// configuration that declares no audit directory keeps no trail. Every other
// failure, a sidecar that could not be inspected, opened, locked or read now,
// is an error, and is never taken for one with no line.
func (s *Server) readSidecar(ctx context.Context) (sidecarReading, error) {
	dir, declared, err := s.projectAuditDir()
	if err != nil {
		return sidecarReading{}, err
	}
	if !declared {
		return sidecarReading{}, nil
	}
	parts, err := auditDirParts(dir)
	if err != nil {
		return sidecarReading{}, err
	}
	root, err := s.openAuditDir(parts)
	if errors.Is(err, fs.ErrNotExist) {
		return sidecarReading{}, nil
	}
	if err != nil {
		return sidecarReading{}, err
	}
	defer root.Close()
	snapshot, err := snapshotAuditFile(ctx, root, "signatures")
	if errors.Is(err, fs.ErrNotExist) {
		return sidecarReading{}, nil
	}
	if err != nil {
		return sidecarReading{}, err
	}
	defer snapshot.file.Close()
	return readSidecarLines(io.NewSectionReader(snapshot.file, 0, snapshot.size))
}

// sidecarProblem is why the sidecar could not be read, in words with no path.
func sidecarProblem(err error) string {
	switch {
	case errors.Is(err, errAuditOutside):
		return "the audit directory jpack.json declares is not a folder inside the project that Desk reads"
	case errors.Is(err, errAuditLinked):
		return "the way to it passes through a symbolic link"
	case errors.Is(err, errAuditNotAFile):
		return "the way to it is not a folder and a regular file"
	case errors.Is(err, errAuditHardLinked):
		return "it, or the trail it is read beside, has another name as well, a hard link"
	case errors.Is(err, errAuditLinksUnknown):
		return "Desk cannot tell here whether it has another name, a hard link"
	case errors.Is(err, errAuditUnchecked):
		return "the way to it changed while Desk was opening it"
	case errors.Is(err, errAuditNoTrail):
		return "it is read under the trail's lock, and there is no trail beside it"
	case errors.Is(err, errAuditNoLock):
		return "Desk can take no lock on the trail here, so it cannot read the sidecar between two writes"
	case errors.Is(err, errAuditLockBusy):
		return "a runtime held the trail's lock for too long"
	}
	return "it could not be read"
}

/* The list of keys, against the seed and the sidecar ------------------------- */

// checkKeysAgainst is the rule a list of public keys is held to before Desk
// passes it or rotates from it, beside parseDeskKeys's own: its last key is
// current, the key the runtime reads from the seed Desk keeps, with that
// keyId; and each key of the trail the sidecar is, after that trail's first
// (`trailFirst`), is the one the sidecar's key rotations hand over to, in
// order, each made by the key before it, at the sequence the list gives, and
// the sidecar records no other rotation. The sidecar is consulted only where
// it was read. Its error is a plain reason, with no path.
//
// **A trail begun after a rotation** (issue #285; ADR-0010 section 1, "moved
// aside"): a rotation records in the list the trail it was made on, so the
// rotations of an earlier trail are told from this trail's by what they
// recorded, never inferred from what this sidecar lacks. A rotation Desk
// recorded on this trail, or recorded nothing for, as a Desk before this one,
// must be in this sidecar: one missing is the same trail with a rotation
// missing, and is refused.
func checkKeysAgainst(keys []deskPublicKey, current deskPublicKey, sidecar *sidecarReading) error {
	if len(keys) == 0 {
		return errors.New("it lists no key")
	}
	if last := keys[len(keys)-1]; last.PublicKey != current.PublicKey || last.KeyID != current.KeyID {
		return errNotSeedsKey
	}
	if sidecar == nil {
		return nil
	}
	if sidecar.mixed {
		return errors.New("the trail's signature sidecar names more than one trail")
	}
	first := trailFirst(keys, sidecar)
	if len(sidecar.rotations) != len(keys)-1-first {
		return fmt.Errorf("the trail's signature sidecar records %d key rotations, and the list names %d keys after this trail's first", len(sidecar.rotations), len(keys)-1-first)
	}
	for i, rotation := range sidecar.rotations {
		key, before := keys[first+i+1], keys[first+i]
		switch {
		case rotation.Next != key.PublicKey:
			return fmt.Errorf("key %d of the list is not the key the sidecar's key rotation %d hands over to", first+i+2, i+1)
		case rotation.KeyID != before.KeyID:
			return fmt.Errorf("the sidecar's key rotation %d was made by a key other than key %d of the list", i+1, first+i+1)
		case rotation.At != key.At:
			return fmt.Errorf("key %d of the list takes over from sequence %d, and the sidecar's key rotation to it from %d", first+i+2, key.At, rotation.At)
		}
	}
	return nil
}

// keysOfTrail is the keys of the trail sidecar is, as `audit verify` is
// given them and the decision record shows them for a holder: keys from
// trailFirst on, the first taking over from 0 where it began a trail after
// an earlier one's keys ("the retained current key as the first key of the
// new trail", issue #285). The list Desk keeps is not changed.
func keysOfTrail(keys []deskPublicKey, sidecar *sidecarReading) []deskPublicKey {
	first := trailFirst(keys, sidecar)
	if first == 0 {
		return keys
	}
	trail := slices.Clone(keys[first:])
	trail[0].At = 0
	return trail
}

// trailFirst is the index in keys of the first key of the trail sidecar is:
// the last key whose rotation recorded another trail than the one this
// sidecar names, which was in force when this trail began; 0 where there is
// none. A sidecar with no line names no trail: it is a trail begun with the
// current key where the list's last rotation recorded its trail (that
// trail's sidecar holds the line), and otherwise the list's whole. The keys
// from it on are the ones `audit verify` is given for this trail: given an
// earlier trail's keys too, the runtime takes the first for the key the new
// trail began with, and reports the trail invalid (runtime 0.27.1,
// measured).
func trailFirst(keys []deskPublicKey, sidecar *sidecarReading) int {
	if sidecar == nil || len(keys) < 2 {
		return 0
	}
	trail, ok := sidecar.oneTrail()
	if !ok {
		if !sidecar.mixed && keys[len(keys)-1].Trail != "" {
			return len(keys) - 1
		}
		return 0
	}
	first := 0
	for i := 1; i < len(keys); i++ {
		if keys[i].Trail != "" && keys[i].Trail != trail {
			first = i
		}
	}
	return first
}

// errNotSeedsKey is a list whose last key is not the seed's.
var errNotSeedsKey = errors.New("its last key is not the key Desk keeps for this desk")

// keysLines is a list of public keys, as Desk writes it.
func keysLines(keys []deskPublicKey) []byte {
	var data []byte
	for _, key := range keys {
		data = append(data, key.line()...)
	}
	return data
}

// replaceKeys writes keys whole under name, in place of the list read there:
// staged under a name of its own, made 0600 on its descriptor, synced, and
// renamed over name, only while name still holds the file read with the bytes
// read (errKeysChanged otherwise). The folder is synced after. A stop at any
// moment leaves the old list or the new one at its name, never a part of one;
// a stage it leaves is never read as a list. The moment between the check and
// the rename is in a folder only this user can change, under the desk's key
// lock. It answers the list as written: the file renamed into place, and its
// bytes.
func (d *signingDir) replaceKeys(name string, read keysFile, keys []deskPublicKey) (keysFile, error) {
	data := keysLines(keys)
	if _, err := parseDeskKeys(data); err != nil {
		return keysFile{}, fmt.Errorf("the list with the next key is not one Desk keeps: %w", err)
	}
	staged, stagedName, err := d.stage()
	if err != nil {
		return keysFile{}, err
	}
	defer d.root.Remove(stagedName)
	if _, err := staged.Write(data); err != nil {
		staged.Close()
		return keysFile{}, err
	}
	if err := staged.Chmod(custodyFileMode); err != nil {
		staged.Close()
		return keysFile{}, err
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		return keysFile{}, err
	}
	written, err := staged.Stat()
	if err != nil {
		staged.Close()
		return keysFile{}, err
	}
	if err := staged.Close(); err != nil {
		return keysFile{}, err
	}
	_, now, found, err := d.readKeysFile(name)
	if err != nil || !found || !os.SameFile(now.info, read.info) || !bytes.Equal(now.data, read.data) {
		return keysFile{}, errKeysChanged
	}
	if err := d.root.Rename(stagedName, name); err != nil {
		return keysFile{}, err
	}
	if dir, err := d.root.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return keysFile{info: written, data: data}, nil
}

// promoteNext renames the next seed over the current seed's name, in the
// folder held, while each name still holds the file found there and the list
// of public keys is still list, the file and the bytes the rotation wrote or
// read; and checks that the seed's name, and its pathname, then name the file
// that was the next seed: the file the desk's configuration now names.
//
// **The list, again, immediately before the rename** (issue #239). A list put
// back in its place after it was written, or after the start inspected it as
// written, would otherwise leave the desk naming a key its list does not end
// in, and no marker to say so: the panel would pass no key, and offer nothing
// to finish. On any difference the marker stays, for the next start.
func (d *signingDir) promoteNext(nextName, seedName, keysName string, list keysFile, next, seed os.FileInfo) error {
	if found, err := d.root.Lstat(nextName); err != nil || !os.SameFile(found, next) {
		return errors.New("the next key is not the file the rotation made")
	}
	if found, err := d.root.Lstat(seedName); err != nil || !os.SameFile(found, seed) {
		return errors.New("the current key is not the file the rotation read")
	}
	if _, now, found, err := d.readKeysFile(keysName); err != nil || !found || !os.SameFile(now.info, list.info) || !bytes.Equal(now.data, list.data) {
		return errors.New("the list of public keys is not the file, or not the bytes, the rotation wrote or read")
	}
	if err := d.root.Rename(nextName, seedName); err != nil {
		return fmt.Errorf("the next key could not be renamed over the current one: %w", err)
	}
	if found, err := d.root.Lstat(seedName); err != nil || !os.SameFile(found, next) {
		return errors.New("the key's name does not hold the next key after the rename")
	}
	if d.namesFile(seedName, next) != nil {
		return errors.New("the key's path does not name the next key after the rename")
	}
	if dir, err := d.root.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

/* The rotation's journal ---------------------------------------------------- */

// What a rotation's marker journals: the step the rotation is about to make
// (issue #285). It is written before each step, synced, and read back by a
// start's recovery, which decides from it and from the sidecar of the trail
// it names, never from whichever sidecar is there now alone.
const (
	// journalGenerate: the next key is about to be made. The runtime has not
	// been asked to rotate.
	journalGenerate = "generate"
	// journalRotate: the next key, Next, is made, and the runtime is about to
	// be asked to rotate to it, on Trail.
	journalRotate = "rotate"
	// journalFinish: the runtime answered that it rotated to Next, on Trail,
	// at At: the list and the rename are left.
	journalFinish = "finish"
	// rotationJournalLimit bounds the marker Desk reads: a journal is under
	// 200 bytes.
	rotationJournalLimit = 512
)

// rotationJournal is a rotation's marker, as Desk writes it: one line, in
// the spelling json.Marshal gives it, then a newline.
//
//	{"version":"1","phase":"rotate","trail":"<32 hex>","next":"<64 hex>","at":0}
//
// Trail is the trail identity the rotation is made on, named before anything
// is made (`sidecarReading.oneTrail`, or the runtime's checkpoint where the
// sidecar has no line yet); Next the next key's public key, from "rotate" on;
// At the sequence the runtime answered, at "finish".
type rotationJournal struct {
	Version string `json:"version"`
	Phase   string `json:"phase"`
	Trail   string `json:"trail"`
	Next    string `json:"next"`
	At      int64  `json:"at"`
}

// line is the journal's one spelling.
func (j rotationJournal) line() []byte {
	data, _ := json.Marshal(j)
	return append(data, '\n')
}

// errJournal is a marker that is not one Desk writes.
var errJournal = errors.New("its marker is not a journal Desk writes")

// parseJournal holds a marker's bytes to the journal's record. An empty
// marker is one a Desk before the journal left (legacy): it names no step
// and no trail.
func parseJournal(data []byte) (journal rotationJournal, legacy bool, err error) {
	if len(data) == 0 {
		return rotationJournal{}, true, nil
	}
	if json.Unmarshal(data, &journal) != nil || !bytes.Equal(journal.line(), data) || journal.Version != "1" || !keyIDForm.MatchString(journal.Trail) {
		return rotationJournal{}, false, errJournal
	}
	switch journal.Phase {
	case journalGenerate:
		if journal.Next == "" && journal.At == 0 {
			return journal, false, nil
		}
	case journalRotate:
		if publicKeyForm.MatchString(journal.Next) && journal.At == 0 {
			return journal, false, nil
		}
	case journalFinish:
		if publicKeyForm.MatchString(journal.Next) && journal.At >= 1 && journal.At < sidecarIntegerLimit {
			return journal, false, nil
		}
	}
	return rotationJournal{}, false, errJournal
}

// writeJournal writes a rotation's marker holding journal, 0600, never over
// anything, through the folder this holds, synced with its folder, and
// answers it as written.
func (d *signingDir) writeJournal(name string, journal rotationJournal) (os.FileInfo, error) {
	file, err := d.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|openNoFollow, custodyFileMode)
	if err != nil {
		return nil, err
	}
	_, err = file.Write(journal.line())
	if err == nil {
		err = file.Sync()
	}
	info, statErr := file.Stat()
	if closed := file.Close(); err == nil {
		err = closed
	}
	if err == nil {
		err = statErr
	}
	if err != nil {
		_ = d.root.Remove(name)
		return nil, err
	}
	_ = syncPrivateDirectory(d.root)
	return info, nil
}

// rewriteJournal puts journal in the place of the marker found as held:
// staged under a name of its own, 0600, synced, and renamed over the marker
// only while its name still holds held; the folder is synced after. It
// answers the marker as written.
func (d *signingDir) rewriteJournal(name string, held os.FileInfo, journal rotationJournal) (os.FileInfo, error) {
	staged, stagedName, err := d.stage()
	if err != nil {
		return nil, err
	}
	defer d.root.Remove(stagedName)
	_, err = staged.Write(journal.line())
	if err == nil {
		err = staged.Chmod(custodyFileMode)
	}
	if err == nil {
		err = staged.Sync()
	}
	written, statErr := staged.Stat()
	if closed := staged.Close(); err == nil {
		err = closed
	}
	if err == nil {
		err = statErr
	}
	if err != nil {
		return nil, err
	}
	if found, err := d.root.Lstat(name); err != nil || !os.SameFile(found, held) {
		return nil, errors.New("the rotation's marker is not the file the rotation wrote")
	}
	if err := d.root.Rename(stagedName, name); err != nil {
		return nil, err
	}
	_ = syncPrivateDirectory(d.root)
	return written, nil
}

// readJournal reads the marker found as held, through the folder this
// holds, under the rule Desk keeps a private file by (`readPrivateFile`),
// and holds it to the journal's record.
func (d *signingDir) readJournal(name string, held os.FileInfo) (rotationJournal, bool, error) {
	data, opened, err := readPrivateFile(d.root, name, rotationJournalLimit)
	if err != nil {
		return rotationJournal{}, false, err
	}
	if !os.SameFile(opened, held) {
		return rotationJournal{}, false, errors.New("the rotation's marker changed while it was read")
	}
	return parseJournal(data)
}

/* What a stopped rotation left ----------------------------------------------- */

// rotationOutcome is what a rotation's marker says is left to do.
type rotationOutcome int

const (
	// rotationNone: there is no marker.
	rotationNone rotationOutcome = iota
	// rotationSettled: nothing but the marker is left, to remove.
	rotationSettled
	// rotationWritten: the runtime wrote the rotation; steps 4 to 6 finish it.
	rotationWritten
	// rotationUnwritten: it did not; the next seed and the marker go.
	rotationUnwritten
	// rotationUnknown: Desk cannot tell; nothing changes.
	rotationUnknown
)

// rotationState is what Desk found of a rotation that left its marker, and
// what it would do with it.
type rotationState struct {
	outcome rotationOutcome
	// why says, for rotationUnknown, what could not be told, in words with no
	// path.
	why string
	// marker is the marker, as inspected.
	marker os.FileInfo
	// next and seed are the next and current seeds as found; nextKey and
	// current the keys the runtime read from them.
	next, seed       os.FileInfo
	nextKey, current deskPublicKey
	// list is the list of public keys as read, and finished the list the
	// finish writes in its place, where it is not written already.
	list     keysFile
	finished []deskPublicKey
	// at is, for rotationWritten, the sequence the next key takes over from.
	at int64
	// journal is what the marker journals; legacy is set where the marker is
	// empty, as a Desk before the journal left it (issue #285).
	journal rotationJournal
	legacy  bool
}

// What a start says of a rotation the journal or the sidecar cannot decide
// (issue #285), and keeps everything for.
const (
	// rotationTrailGone: the sidecar of the trail the rotation was made on is
	// gone, or is another trail's.
	rotationTrailGone = "the trail it was made on was moved aside or replaced since, so no signature sidecar Desk can read says whether the runtime wrote it: Desk keeps the current key, the next key and the list of public keys as they are"
	// rotationLegacy: an empty marker, of a Desk before the journal.
	rotationLegacy = "it was begun by an earlier Desk, which did not record the trail it was made on, and the trail's signature sidecar now does not name the next key, as a trail moved aside since would not: Desk keeps the current key, the next key and the list of public keys as they are"
)

// rotationNames are the names a rotation of desk id keeps in the signing
// folder.
func rotationNames(id string) (marker, next, seed, keys string) {
	return id + rotatingSuffix, id + nextSeedSuffix, id + seedSuffix, id + keysSuffix
}

// inspectRotation tells what the marker of a rotation of this desk left to
// do, reading only: the marker, inspected where marker is nil (a rotation
// that is running knows its own); the current seed and the key the runtime
// reads from it; the list of public keys; the trail's signature sidecar; and
// the next seed and its key, where there is one. Anything that could not be
// read makes it rotationUnknown, with why.
func (s *Server) inspectRotation(ctx context.Context, project heldDir, dir *signingDir, marker os.FileInfo) rotationState {
	markerName, nextName, seedName, keysName := rotationNames(s.signingKeyName())
	state := rotationState{marker: marker}
	unknown := func(why string) rotationState {
		state.outcome, state.why = rotationUnknown, why
		return state
	}
	if marker == nil {
		found, err := dir.root.Lstat(markerName)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return rotationState{outcome: rotationNone}
		case err != nil:
			return unknown("its marker could not be inspected")
		case !found.Mode().IsRegular():
			return unknown("its marker is not a regular file")
		}
		state.marker = found
	}
	journal, legacy, err := dir.readJournal(markerName, state.marker)
	if err != nil {
		return unknown("its marker could not be read as the journal Desk writes")
	}
	state.journal, state.legacy = journal, legacy
	seed, err := dir.root.Lstat(seedName)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return unknown("Desk holds no current key for this desk")
	case err != nil:
		return unknown("the current key could not be inspected")
	}
	if err := checkSeed(seedName, seed); err != nil {
		return unknown("the current key is not one Desk uses: " + strings.TrimRight(err.Error(), "."))
	}
	if dir.namesFile(seedName, seed) != nil {
		return unknown("the current key is not at the path the runtime reads it from")
	}
	state.seed = seed
	current, err := s.publicKeyOf(ctx, project, dir, seedName)
	if err != nil {
		return unknown("the runtime could not read the current key: " + strings.TrimRight(err.Error(), "."))
	}
	if current.check() != nil {
		return unknown("the runtime's audit key public did not answer as documented for the current key")
	}
	state.current = current
	keys, list, found, err := dir.readKeysFile(keysName)
	switch {
	case err != nil:
		return unknown("Desk could not read the list of this desk's public keys: " + err.Error())
	case !found:
		return unknown("Desk keeps no list of this desk's public keys")
	}
	state.list = list
	sidecar, err := s.readSidecar(ctx)
	if err != nil {
		s.log.Printf("desk: the signature sidecar of desk %s could not be read to finish a rotation: %v", s.signingKeyName(), err)
		return unknown("the trail's signature sidecar could not be read: " + sidecarProblem(err))
	}
	next, err := dir.root.Lstat(nextName)
	if errors.Is(err, fs.ErrNotExist) {
		if err := checkKeysAgainst(keys, current, &sidecar); err != nil {
			return unknown("there is no next key, and the list of this desk's public keys does not agree with its current key and the sidecar: " + err.Error())
		}
		state.outcome = rotationSettled
		return state
	}
	if err != nil {
		return unknown("the next key could not be inspected")
	}
	if err := checkSeed(nextName, next); err != nil {
		return unknown("the next key is not one Desk uses: " + strings.TrimRight(err.Error(), "."))
	}
	if dir.namesFile(nextName, next) != nil {
		return unknown("the next key is not at the path the runtime reads it from")
	}
	state.next = next
	nextKey, err := s.publicKeyOf(ctx, project, dir, nextName)
	if err != nil {
		return unknown("the runtime could not read the next key: " + strings.TrimRight(err.Error(), "."))
	}
	if nextKey.check() != nil || nextKey.PublicKey == current.PublicKey {
		return unknown("the runtime's audit key public did not answer as documented for the next key")
	}
	state.nextKey = nextKey
	// **The sidecar of the trail the journal names, and no other** (issue
	// #285): a trail moved aside, or replaced, since the rotation began has
	// no say in whether the runtime wrote it there. A rotation journalled
	// before its next key was made never reached the runtime, on any trail:
	// whichever sidecar is there decides it, by whether a line names the next
	// key, as before the journal.
	last, rotated := sidecar.last()
	handsOver := rotated && last.Next == nextKey.PublicKey
	if !state.legacy && state.journal.Phase != journalGenerate {
		if state.journal.Next != nextKey.PublicKey {
			return unknown("the next key is not the key the rotation's journal names")
		}
		// The trail the journal names, which every journal names.
		if trail, ok := sidecar.oneTrail(); !ok || trail != state.journal.Trail {
			return unknown(rotationTrailGone)
		}
	}
	if !handsOver {
		switch {
		case sidecar.names(nextKey):
			return unknown("the trail's signature sidecar names the next key, and its last key rotation does not hand over to it")
		case state.legacy:
			return unknown(rotationLegacy)
		case state.journal.Phase == journalFinish:
			return unknown("the rotation's journal says the runtime rotated to the next key on this trail, and the trail's signature sidecar does not hand over to it")
		}
		state.outcome = rotationUnwritten
		return state
	}
	if last.KeyID != current.KeyID {
		return unknown("the sidecar's last key rotation hands over to the next key from a key other than the current one")
	}
	// The next key's line records the trail its rotation was made on: the
	// one this sidecar names, which handed over to it (issue #285).
	onTrail, _ := sidecar.oneTrail()
	took := deskPublicKey{PublicKey: nextKey.PublicKey, KeyID: nextKey.KeyID, At: last.At, Trail: onTrail}
	state.at = last.At
	switch n := len(keys); {
	case sameKey(keys[n-1], took) && n >= 2 && keys[n-2].PublicKey == current.PublicKey:
		// The list was written already (step 4); the rename is left.
		if err := checkKeysAgainst(keys, took, &sidecar); err != nil {
			return unknown("the list of this desk's public keys does not agree with the sidecar: " + err.Error())
		}
	case keys[n-1].PublicKey == current.PublicKey:
		finished := append(slices.Clone(keys), took)
		if _, err := parseDeskKeys(keysLines(finished)); err != nil {
			return unknown("the list with the next key would not be one Desk keeps: " + err.Error())
		}
		if err := checkKeysAgainst(finished, took, &sidecar); err != nil {
			return unknown("the list with the next key would not agree with the sidecar: " + err.Error())
		}
		state.finished = finished
	default:
		return unknown("the list of this desk's public keys ends in neither the current key nor the next one")
	}
	state.outcome = rotationWritten
	return state
}

// finishRotation makes steps 4 to 6 of a rotation the runtime wrote: the list
// written with the next key, where it is not already; the next seed renamed
// over the current one; and the marker removed. renamed is whether the next
// key is the one the desk names, its list written and its seed renamed and
// checked, which is the rotation's effect: nothing may say the key was
// rotated, or that the next key signs, before it.
func (s *Server) finishRotation(dir *signingDir, state rotationState) (renamed bool, err error) {
	markerName, nextName, seedName, keysName := rotationNames(s.signingKeyName())
	// The list the next key is renamed against: the one written here, or,
	// where it was written already, the one inspected.
	list := state.list
	if state.finished != nil {
		written, err := dir.replaceKeys(keysName, state.list, state.finished)
		if err != nil {
			return false, fmt.Errorf("the list of public keys could not be written with the next key: %w", err)
		}
		list = written
	}
	keyBetween("rotation: list written")
	if err := dir.promoteNext(nextName, seedName, keysName, list, state.next, state.seed); err != nil {
		return false, err
	}
	keyBetween("rotation: seed renamed")
	if err := dir.removeMade(markerName, state.marker); err != nil {
		return true, fmt.Errorf("its marker could not be removed: %w", err)
	}
	return true, nil
}

// unfinishedWords is what a rotation the runtime wrote, and Desk could not
// finish, says: never that the key was rotated before its effect, and, once
// the next key is the one the desk names, that it is.
func unfinishedWords(renamed bool, at int64, err error) string {
	why := strings.TrimRight(err.Error(), ".")
	if renamed {
		return fmt.Sprintf("The rotation of this desk's key did not finish: the next key now signs the records after record %d, and %s. Desk removes the marker when it next starts, and starts no other rotation until then.", at, why)
	}
	return "The rotation of this desk's key did not finish: the runtime wrote it, and Desk could not finish it: " + why + ". Records written until it is finished may be unsigned. Desk finishes it when it next starts, and the decision record says a rotation did not finish."
}

// undoRotation removes the next seed a rotation the runtime did not write
// made, by identity, and then its marker. The current seed is never touched.
// Where the next seed cannot be removed, the marker stays.
func (s *Server) undoRotation(dir *signingDir, next, marker os.FileInfo) error {
	markerName, nextName, _, _ := rotationNames(s.signingKeyName())
	if err := dir.removeMade(nextName, next); err != nil {
		return fmt.Errorf("the next key could not be removed: %w", err)
	}
	if err := dir.removeMade(markerName, marker); err != nil {
		return fmt.Errorf("its marker could not be removed: %w", err)
	}
	return nil
}

// recoverRotations finishes or undoes each rotation a stopped Desk left, where
// it can tell which (`inspectRotation`), and leaves the rest for the panel to
// say. It runs once, at start, under the registry's lock, after the desks are
// opened and before any request is served.
//
// **Under the signing folder's lock** (signing_lock.go). Where another Desk
// process holds it, a key creation or a rotation is under way there: the
// recovery asks again once, waiting for it a bounded time
// (`signingLockWait`), and acts once it is let go; where it is still held
// then, or none can be taken here, nothing may be removed, and it changes
// nothing and leaves every marker for the next start (issue #286). It acts
// only on markers named `<desk id>.rotating` directly in the signing folder,
// of a desk the registry opened or of this server's own signing identity
// (`signingKeyName`): the project Desk was started on, or the desk Desk made
// that it was opened on directly, which the registry leaves out; and on
// nothing in a folder below it.
func (s *Server) recoverRotations() {
	dir, err := s.assistant.openSigning(false)
	if errors.Is(err, errNoSigningDir) {
		return
	}
	if err != nil {
		s.log.Printf("desk: the signing folder could not be opened to look for unfinished rotations: %v", err)
		return
	}
	defer dir.Close()
	listing, err := dir.root.Open(".")
	if err != nil {
		return
	}
	entries, err := listing.ReadDir(registryReadLimit)
	listing.Close()
	if err != nil && len(entries) == 0 {
		return
	}
	var markers []string
	for _, entry := range entries {
		id, isMarker := strings.CutSuffix(entry.Name(), rotatingSuffix)
		if isMarker && (deskIDPattern.MatchString(id) || s.startupKey(id)) && entry.Type().IsRegular() {
			markers = append(markers, id)
		}
	}
	if len(markers) == 0 {
		return
	}
	keyBetween("rotations: before the lock")
	unlock, err := lockSigning(dir)
	if errors.Is(err, errSigningBusy) {
		// **One bounded retry, not a loop**: a live creation or rotation in
		// another process ends, and the recovery goes on after it.
		s.log.Printf("desk: the signing folder's lock is held, so unfinished rotations wait for it, at most %v", signingLockWait)
		unlock, err = lockSigningWithin(context.Background(), dir, signingLockWait)
	}
	if err != nil {
		s.log.Printf("desk: unfinished rotations were left for the next start, because the signing folder's lock was not taken: %v", err)
		return
	}
	defer unlock()
	keyBetween("rotations: locked")
	own := s.signingKeyName()
	for _, id := range markers {
		child := s.desks[id]
		// **By the signing identity** (issue #286): this server's own
		// markers are its own, a made desk's opened directly among them.
		if own != "" && id == own {
			// **No recovery under an identity another folder holds**
			// (review round 1 of #296): its trail may be a copy's.
			if s.startupShared() {
				s.log.Printf("desk: an unfinished rotation of this project's key was left as it is, because %s", sharedWords)
				continue
			}
			child = s
		}
		if child == nil {
			s.log.Printf("desk: an unfinished rotation of desk %s's key was left as it is, because that desk is not open", id)
			continue
		}
		child.recoverRotation(dir)
	}
}

// recoverRotation finishes or undoes this desk's rotation where its marker
// says which, through dir, the signing folder the caller holds and holds the
// lock of, and under the desk's key lock: every name inspected first, then
// acted on.
func (s *Server) recoverRotation(dir *signingDir) {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	project, refusal := s.auditRuntime()
	if refusal != "" {
		s.log.Printf("desk: an unfinished rotation of desk %s's key was left as it is: %s", s.signingKeyName(), refusal)
		return
	}
	state := s.inspectRotation(context.Background(), project, dir, nil)
	keyBetween("rotation: inspected")
	switch state.outcome {
	case rotationSettled:
		markerName, _, _, _ := rotationNames(s.signingKeyName())
		if err := dir.removeMade(markerName, state.marker); err != nil {
			s.log.Printf("desk: the marker of a finished rotation of desk %s's key could not be removed: %v", s.signingKeyName(), err)
			return
		}
		s.log.Printf("desk: a rotation of desk %s's key had nothing left to do; its marker was removed", s.signingKeyName())
	case rotationWritten:
		if renamed, err := s.finishRotation(dir, state); err != nil {
			s.log.Printf("desk: a rotation of desk %s's key that the runtime wrote could not be finished (the next key named: %v): %v", s.signingKeyName(), renamed, err)
			return
		}
		s.log.Printf("desk: a rotation of desk %s's key that a stop cut short was finished: records after %d are signed with key %s", s.signingKeyName(), state.at, state.nextKey.KeyID)
	case rotationUnwritten:
		if err := s.undoRotation(dir, state.next, state.marker); err != nil {
			s.log.Printf("desk: a rotation of desk %s's key that the runtime did not write could not be undone: %v", s.signingKeyName(), err)
			return
		}
		s.log.Printf("desk: a rotation of desk %s's key that the runtime did not write was undone; Desk kept key %s", s.signingKeyName(), state.current.KeyID)
	case rotationUnknown:
		s.log.Printf("desk: an unfinished rotation of desk %s's key was left as it is: %s", s.signingKeyName(), state.why)
	}
}

/* The offer, and the owner's confirmation ------------------------------------ */

// creationLeftWords is what the panel says where a creation of the startup
// project's key did not finish, and the start left it.
const creationLeftWords = "A signing key's creation for this project did not finish, and Desk could not establish that it was this project's own, from this folder, over the jpack.json found now: it keeps that key, its list and its creation's marker as they are, and rotates no key until they are resolved."

// What the panel says of rotating the desk's key.
const (
	// rotationAvailable: the owner can rotate it; Token confirms it.
	rotationAvailable = "available"
	// rotationUnavailable: Reason says why not.
	rotationUnavailable = "unavailable"
	// rotationUnfinished: a rotation did not finish; Reason says what is
	// left, or why Desk cannot tell.
	rotationUnfinished = "unfinished"
)

// auditRotation is the panel's word on rotating the desk's signing key.
type auditRotation struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	Token  string `json:"token,omitempty"`
}

// rotationKeyLimit is the most keys a desk's list may hold before Desk
// offers no further rotation: the list's own bound. A variable only so a test
// can reach it without that many rotations.
var rotationKeyLimit = maxDeskKeys

// rotationOffer says whether the owner can rotate this desk's key now, with
// the token that confirms it, from what the panel read of its keys. It runs
// under the desk's key lock.
func (s *Server) rotationOffer(ctx context.Context, project heldDir, keys auditKeys, reading *keyReading) auditRotation {
	// **No key action where the runtime would act on another key** (ADR-0010,
	// section 1): on the project Desk was started on, an inherited
	// JPACK_SIGNING_KEY is the key the runtime signs and rotates with.
	if s.inheritsSigningKey() {
		return auditRotation{State: rotationUnavailable, Reason: "JPACK_SIGNING_KEY is set where Desk was started, and the runtime signs this project's records with the key it names, not with the key Desk keeps, so Desk rotates no key here."}
	}
	if s.startupShared() {
		return auditRotation{State: rotationUnavailable, Reason: "Desk rotates no key here: " + sharedWords + "."}
	}
	if keys.State == keysStartup {
		return auditRotation{State: rotationUnavailable, Reason: "Desk keeps no signing key for the project it was started on, so it has none to rotate."}
	}
	if keys.State == keysNone {
		return auditRotation{State: rotationUnavailable, Reason: "Desk keeps no signing key for this desk, so it has none to rotate."}
	}
	dir := (*signingDir)(nil)
	if reading != nil {
		dir = reading.dir
	} else {
		opened, err := s.assistant.openSigning(false)
		if err != nil {
			return auditRotation{State: rotationUnavailable, Reason: "Desk rotates only a key it can read, with a list of public keys that agrees with it."}
		}
		defer opened.Close()
		dir = opened
	}
	markerName, _, _, _ := rotationNames(s.signingKeyName())
	if _, err := dir.root.Lstat(markerName); !errors.Is(err, fs.ErrNotExist) {
		return s.unfinishedRotation(ctx, project, dir)
	}
	// **A creation left unfinished, and left by the start** (review round 1
	// of #296): its key, list and marker stay where the start could not bind
	// them to this project, and the panel says so.
	if s.cfg.deskID == "" {
		if _, err := dir.root.Lstat(s.signingKeyName() + creatingSuffix); !errors.Is(err, fs.ErrNotExist) {
			return auditRotation{State: rotationUnavailable, Reason: creationLeftWords}
		}
	}
	// **On the project Desk was started on, only the key its jpack.json names,
	// by Desk's own path to it, is rotated** (review round 1 of #261). A
	// rotation renames the next key over Desk's seed: where jpack.json names
	// another file, even one holding the same key, the trail would hand
	// signing over while jpack.json named a key that signs nothing more.
	var unnamed error
	if s.cfg.deskID == "" && reading != nil {
		unnamed = s.startupSeedNamed(reading.dir, reading.seed)
	}
	switch {
	case keys.State != keysKept || reading == nil:
		return auditRotation{State: rotationUnavailable, Reason: "Desk rotates only a key it can read, with a list of public keys that agrees with it."}
	case unnamed != nil:
		return auditRotation{State: rotationUnavailable, Reason: "Desk rotates the signing key of the project it was started on only where its jpack.json names the key Desk keeps: " + unnamed.Error() + ". A rotation now would hand signing over in the trail while jpack.json named a key that signs nothing more."}
	case reading.sidecarErr != nil:
		return auditRotation{State: rotationUnavailable, Reason: "Desk reads the trail's signature sidecar to tell whether a rotation was written, and it could not be read: " + sidecarProblem(reading.sidecarErr) + ". So Desk does not rotate the key now."}
	case len(reading.keys) >= rotationKeyLimit:
		// **No rotation the list cannot record.** The runtime would hand
		// signing to the next key, and the list, held to its bound, could not
		// name it: the desk would keep a key no longer in force, and the
		// rotation could never finish (review round 1).
		return auditRotation{State: rotationUnavailable, Reason: fmt.Sprintf("This desk already has %d signing keys, the most Desk keeps for one desk, so it rotates no further key.", rotationKeyLimit)}
	case len(reading.sidecar.rotations) > 0 && !reading.sidecar.signedSinceRotation:
		return auditRotation{State: rotationUnavailable, Reason: fmt.Sprintf("This desk's key took over after record %d, and no record has been signed since: a rotation now would take over after the same record. Make a deciding run first.", reading.keys[len(reading.keys)-1].At)}
	}
	return auditRotation{State: rotationAvailable, Token: s.rotationToken(reading)}
}

// unfinishedRotation is the panel's word on a rotation that left its marker:
// what Desk does with it at its next start, or why it cannot tell. It reads,
// and changes nothing.
func (s *Server) unfinishedRotation(ctx context.Context, project heldDir, dir *signingDir) auditRotation {
	state := s.inspectRotation(ctx, project, dir, nil)
	reason := map[rotationOutcome]string{
		rotationNone:      "Its marker was removed while Desk looked.",
		rotationSettled:   "Nothing of it is left to do but remove its marker, which Desk does when it next starts.",
		rotationWritten:   "The runtime wrote the rotation, and Desk did not finish it: Desk finishes it when it next starts. Until then, records are written unsigned.",
		rotationUnwritten: "The runtime did not write the rotation: Desk removes the next key when it next starts, and keeps the current key.",
	}[state.outcome]
	if state.outcome == rotationUnknown {
		reason = "Desk cannot tell whether the runtime wrote the rotation, so it changes nothing: " + strings.TrimRight(state.why, ".") + "."
	}
	return auditRotation{State: rotationUnfinished, Reason: reason}
}

// rotationToken binds a confirmation to this desk and to the reading the
// panel showed: the list of public keys, by its bytes, and the key the
// runtime read from the seed. It is a MAC under the desk's own review key,
// and names its purpose, so no other token confirms a rotation.
func (s *Server) rotationToken(reading *keyReading) string {
	payload, _ := json.Marshal(struct {
		Purpose   string `json:"purpose"`
		Desk      string `json:"desk"`
		Project   string `json:"project"`
		Keys      string `json:"keys"`
		Current   string `json:"current"`
		Rotations int    `json:"rotations"`
		Seed      string `json:"seed"`
	}{"rotate-signing-key", s.cfg.deskID, s.projectDir, sha256Digest(reading.list.data), reading.current.PublicKey, len(reading.sidecar.rotations), s.tokenSeed(reading)})
	mac := hmac.New(sha256.New, s.reviewKey[:])
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// rotationAnswer is what a rotation answers.
type rotationAnswer struct {
	// State is "rotated".
	State string `json:"state"`
	// At is the sequence the next key takes over from: it signs the records
	// after it.
	At int64 `json:"at"`
	// From is the key that signed until then, and Next the key that signs
	// after it.
	From deskPublicKey `json:"from"`
	Next deskPublicKey `json:"next"`
}

// handleRotateKey answers `POST /api/audit/key/rotate`: rotate this desk's
// signing key, as the panel the token names showed it, or change nothing.
//
// It is a `POST` with a JSON body and the desk's bearer, which a cross-site
// page cannot send: the guard checks the session and the Origin, and a
// request a browser marks cross-site is refused besides.
func (s *Server) handleRotateKey(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden, "A cross-site request cannot rotate this desk's key.")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeJSONCoded(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Send the confirmation as JSON.")
		return
	}
	var request struct {
		Token string `json:"token"`
	}
	data, err := readBounded(r.Body, rotateConfirmLimit)
	if err != nil || decodeDataJSON(data, &request) != nil || len(request.Token) != 64 {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Confirm the rotation with the token the decision record gave.")
		return
	}
	project, refusal := s.auditRuntime()
	if refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	// **The desk's key lock around the rotation alone, released by a defer**
	// (issue #239, review round 1). A panic in a rotation, which net/http
	// recovers, would otherwise leave the lock held, and a client that stops
	// reading the answer would hold it while the answer is written: either
	// way every later reading of this desk's keys, and every rotation, waits.
	answer, failure := func() (*rotationAnswer, *lockFailure) {
		s.keyMu.Lock()
		defer s.keyMu.Unlock()
		return s.rotateKey(r.Context(), project, request.Token)
	}()
	if failure != nil {
		message := s.withoutPaths(failure.message)
		if message != failure.message {
			s.log.Printf("desk: the rotation of desk %s's key, as said: %s", s.signingKeyName(), failure.message)
		}
		writeJSONCoded(w, failure.status, failure.code, message)
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

// rotateKey makes the rotation the token confirms, or changes nothing, in two
// phases: checkRotation reads, and changes nothing; makeRotation acts on what
// it read. The caller holds the desk's key lock across both, and this holds
// the signing folder's lock across both (signing_lock.go): it waits for it a
// bounded time, and then refuses in plain words. Where no lock can be taken
// here, it rotates as it would with one.
func (s *Server) rotateKey(ctx context.Context, project heldDir, token string) (*rotationAnswer, *lockFailure) {
	dir, openErr := s.assistant.openSigning(false)
	if openErr == nil {
		defer dir.Close()
		unlock, err := lockSigningWithin(ctx, dir, signingLockWait)
		switch {
		case errors.Is(err, errSigningBusy):
			return nil, &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was rotated: " + signingBusyWords}
		case err != nil:
			s.log.Printf("desk: desk %s's key is rotated without the signing folder's lock: %v", s.signingKeyName(), err)
		default:
			defer unlock()
		}
	}
	reading, failure := s.checkRotation(ctx, project, token, dir, openErr)
	if failure != nil {
		return nil, failure
	}
	return s.makeRotation(ctx, project, reading)
}

// checkRotation is a rotation's first phase: whether the runtime has `audit
// key rotate`, the desk's keys read afresh through dir, the signing folder
// rotateKey holds (`keysIn`), whether a rotation can be made now
// (`rotationOffer`), and whether the token is the one the panel gave for
// those keys. It changes nothing, and answers the reading, through dir, for
// makeRotation, or why no rotation is made.
func (s *Server) checkRotation(ctx context.Context, project heldDir, token string, dir *signingDir, openErr error) (*keyReading, *lockFailure) {
	schema, err := readRuntimeSchema(ctx, s.cfg.JpackBin, project)
	if err != nil {
		return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was rotated: " + strings.TrimRight(err.Error(), ".") + "."}
	}
	if !slices.Contains(schema.supported, signedFromVersion) {
		return nil, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf("The runtime this Desk runs (jpack %s) does not read configVersion 6 and has no audit key rotate, so Desk rotates no key with it. A runtime of %s or later has it.", schema.version, auditRuntimeFloor)}
	}
	keys, reading := s.keysIn(ctx, project, dir, openErr)
	offer := s.rotationOffer(ctx, project, keys, reading)
	refuse := func(failure *lockFailure) (*keyReading, *lockFailure) {
		return nil, failure
	}
	switch {
	case offer.State == rotationUnfinished:
		return refuse(&lockFailure{http.StatusConflict, CodeBadRequest, "A rotation of this desk's key did not finish, so Desk starts no other. " + offer.Reason})
	case offer.State != rotationAvailable:
		reason := offer.Reason
		if keys.State == keysUnread {
			reason += " " + keys.Problem
		}
		return refuse(&lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was rotated. " + reason})
	case !hmac.Equal([]byte(offer.Token), []byte(token)):
		return refuse(&lockFailure{http.StatusConflict, CodeStale, "This desk's keys changed after the decision record showed them, so nothing was rotated. Check the decision record again."})
	}
	return reading, nil
}

// makeRotation is a rotation's second phase, over what checkRotation read:
// the six steps, and the finish or undo of what they made.
//
// From the marker on, every way out that changes nothing removes what it made
// by name, in the order made, and never through a deferred call, so that a
// stop at any moment leaves exactly what a crash would, for the next start to
// read. It answers that the key was rotated only once the next key is the one
// the desk names (`finishRotation`); a rotation the runtime wrote and Desk
// could not finish is said as one that did not finish.
func (s *Server) makeRotation(ctx context.Context, project heldDir, reading *keyReading) (*rotationAnswer, *lockFailure) {
	dir, id := reading.dir, s.signingKeyName()
	markerName, nextName, _, _ := rotationNames(id)
	nextPath := filepath.Join(dir.path, nextName)
	// **The trail the rotation is made on, named before anything is made**
	// (review round 1 of #302): as the sidecar the panel read names it, or,
	// where it names none yet, as the runtime names the trail's last chained
	// record (`audit checkpoint`). A trail with no chained record has nothing
	// to rotate on, and nothing is made; a journal never names no trail,
	// which no start could tell a trail moved aside from.
	trail, ok := reading.sidecar.oneTrail()
	if !ok {
		named, refusal := s.rotationTrail(ctx, project)
		if refusal != nil {
			return nil, refusal
		}
		trail = named
	}
	journal := rotationJournal{Version: "1", Phase: journalGenerate, Trail: trail}

	// 1. The marker, journalling the first step, before anything else.
	marker, err := dir.writeJournal(markerName, journal)
	if err != nil {
		s.log.Printf("desk: the rotation of desk %s's key could not write its marker: %v", id, err)
		return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was rotated: Desk could not write the rotation's marker in its signing folder."}
	}
	// **From here, the request going away stops nothing.** A page closed in
	// the middle would otherwise cancel a run whose effect Desk must then
	// read back; each run keeps runRuntime's own bound.
	ctx = context.WithoutCancel(ctx)
	keyBetween("rotation: marker written")
	undo := func(next os.FileInfo, failure *lockFailure) (*rotationAnswer, *lockFailure) {
		if next != nil {
			if err := dir.removeMade(nextName, next); err != nil {
				s.log.Printf("desk: a rotation of desk %s's key that changed nothing could not remove its next key: %v", id, err)
				failure.message += " The next key it made could not be removed, and its marker stays: Desk removes them when it next starts, where it can tell the runtime wrote no rotation."
				return nil, failure
			}
		}
		if err := dir.removeMade(markerName, marker); err != nil {
			s.log.Printf("desk: a rotation of desk %s's key that changed nothing could not remove its marker: %v", id, err)
			failure.message += " Its marker could not be removed: Desk removes it when it next starts."
		}
		return nil, failure
	}
	if _, err := dir.root.Lstat(nextName); !errors.Is(err, fs.ErrNotExist) {
		return undo(nil, &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was rotated: something is already kept under the next key's name, and a key is never written over anything."})
	}

	// 2. The next key.
	if dir.namesHeld() != nil {
		return undo(nil, &lockFailure{http.StatusConflict, CodeStale, "Nothing was rotated: Desk's signing folder was replaced before the runtime made the next key, so no key was made."})
	}
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, project, "audit", "key", "generate", nextPath, "--format", "json")
	keyBetween("rotation: next generated")
	nextKey, err := readGenerated(out, runErr)
	if err != nil {
		// Whatever the next key's name holds was made by that run: it was
		// free before it, under the marker this rotation holds.
		removed := dir.root.Remove(nextName)
		failure := &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was rotated: " + strings.TrimRight(err.Error(), ".") + "."}
		if removed != nil && !errors.Is(removed, fs.ErrNotExist) {
			s.log.Printf("desk: what a failed generation left as %s could not be removed: %v", nextName, removed)
			failure.message += " What it left under the next key's name could not be removed, and the rotation's marker stays."
			return nil, failure
		}
		return undo(nil, failure)
	}
	next, err := dir.root.Lstat(nextName)
	if errors.Is(err, fs.ErrNotExist) {
		return undo(nil, &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was rotated: the runtime reported the next key, but there is none in the signing folder Desk holds."})
	}
	if err != nil {
		// Could not be inspected now is not "not there": what the runtime
		// wrote, if anything, stays with the marker for the next start.
		s.log.Printf("desk: the next key of a rotation of desk %s could not be inspected: %v", id, err)
		return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was rotated: the next key the runtime reported could not be inspected, so the rotation's marker stays, and the decision record says a rotation did not finish."}
	}
	if dir.namesFile(nextName, next) != nil {
		return undo(next, &lockFailure{http.StatusConflict, CodeStale, "Nothing was rotated: Desk's signing folder was replaced while the runtime made the next key."})
	}
	if err := checkSeed(nextName, next); err != nil {
		return undo(next, &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was rotated: " + strings.TrimRight(err.Error(), ".") + "."})
	}
	if nextKey.PublicKey == reading.current.PublicKey {
		return undo(next, &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was rotated: the runtime made a next key that is the current key."})
	}

	// 3. The rotation: journalled first, with the next key, on the trail.
	journal.Phase, journal.Next = journalRotate, nextKey.PublicKey
	rewritten, err := dir.rewriteJournal(markerName, marker, journal)
	if err != nil {
		s.log.Printf("desk: the rotation of desk %s's key could not journal its next step: %v", id, err)
		return undo(next, &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was rotated: Desk could not record the rotation's next step in its marker."})
	}
	marker = rewritten
	keyBetween("rotation: rotate journalled")
	if dir.namesHeld() != nil || dir.namesFile(nextName, next) != nil {
		return undo(next, &lockFailure{http.StatusConflict, CodeStale, "Nothing was rotated: Desk's signing folder, or the next key in it, was replaced before the runtime rotated to it."})
	}
	out, runErr = runRuntime(ctx, s.cfg.JpackBin, project, "audit", "key", "rotate", "--next", nextPath, "--config", runtimeConfigName, "--format", "json")
	keyBetween("rotation: line written")
	at, rotatedOn, said, rotated := readRotated(out, runErr, reading.current, nextKey, trail)
	if rotated {
		// The runtime's answer, journalled: a stop from here finishes the
		// rotation on this trail, and on no other. A journal that cannot be
		// written now leaves "rotate", which a start decides from the same
		// trail's sidecar.
		journal.Phase, journal.Trail, journal.At = journalFinish, rotatedOn, at
		if rewritten, err := dir.rewriteJournal(markerName, marker, journal); err != nil {
			s.log.Printf("desk: the rotation of desk %s's key was written, and its marker could not record it: %v", id, err)
		} else {
			marker = rewritten
		}
	}
	state := rotationState{marker: marker, next: next, seed: reading.seed, current: reading.current, nextKey: nextKey, list: reading.list}
	if rotated {
		took := deskPublicKey{PublicKey: nextKey.PublicKey, KeyID: nextKey.KeyID, At: at, Trail: rotatedOn}
		state.outcome, state.at, state.finished = rotationWritten, at, append(slices.Clone(reading.keys), took)
	} else {
		// The runtime refused, or did not answer as documented. Whether it
		// wrote the line is the sidecar's to say, not its answer's.
		state = s.inspectRotation(ctx, project, dir, marker)
		s.log.Printf("desk: the runtime did not rotate desk %s's key as asked: %s", id, said)
	}
	switch state.outcome {
	case rotationUnwritten:
		// **Desk kept its key; whether that key signs, it did not check**
		// (issue #239). A refusal can be the runtime's word that another key
		// is in force: another runtime rotated the trail after Desk read it.
		return undo(state.next, &lockFailure{http.StatusConflict, CodeBadRequest, "The runtime did not rotate the key, and nothing was changed: Desk kept the current key. It said: " + strings.TrimRight(said, ".") + "."})
	case rotationWritten:
		// 4 to 6.
		if renamed, err := s.finishRotation(dir, state); err != nil {
			s.log.Printf("desk: the rotation of desk %s's key was written and could not be finished (the next key named: %v): %v", id, renamed, err)
			return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, unfinishedWords(renamed, state.at, err)}
		}
		// **Reported made only where jpack.json still names the key**, on the
		// project Desk was started on, whose jpack.json is the owner's to edit
		// meanwhile (review round 1 of #261).
		if s.cfg.deskID == "" {
			seed, err := dir.root.Lstat(id + seedSuffix)
			if err == nil {
				err = s.startupSeedNamed(dir, seed)
			}
			if err != nil {
				s.log.Printf("desk: the rotation of this project's key was made, and its jpack.json no longer names the key Desk keeps: %v", err)
				return nil, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf("The runtime handed signing over after record %d and Desk now keeps the next key, but this project's jpack.json no longer names the key Desk keeps: %s. Records are not signed by Desk's key until jpack.json names it again.", state.at, strings.TrimRight(err.Error(), "."))}
			}
		}
		from := reading.keys[len(reading.keys)-1]
		next := deskPublicKey{PublicKey: state.nextKey.PublicKey, KeyID: state.nextKey.KeyID, At: state.at}
		return &rotationAnswer{State: "rotated", At: state.at, From: from, Next: next}, nil
	}
	why := state.why
	if state.outcome != rotationUnknown {
		why = "what it left could not be read as a rotation the runtime did or did not write"
	}
	return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, "The runtime did not answer as asked (" + strings.TrimRight(said, ".") + "), and Desk cannot tell whether it wrote the rotation: " + strings.TrimRight(why, ".") + ". Desk changed nothing, and the decision record says a rotation did not finish."}
}

// readGenerated is the key `audit key generate --format json` made, or why it
// made none, in the runtime's words where it gave any.
func readGenerated(out []byte, runErr error) (deskPublicKey, error) {
	var answer struct {
		Command     string              `json:"command"`
		Status      string              `json:"status"`
		PublicKey   string              `json:"publicKey"`
		KeyID       string              `json:"keyId"`
		Diagnostics []runtimeDiagnostic `json:"diagnostics"`
	}
	decoded := out != nil && json.Unmarshal(out, &answer) == nil
	if runErr != nil || !decoded || answer.Command != "audit key generate" || answer.Status != "generated" {
		return deskPublicKey{}, generationFailure(answer.Diagnostics, runErr)
	}
	key := deskPublicKey{PublicKey: answer.PublicKey, KeyID: answer.KeyID}
	if err := key.check(); err != nil {
		return deskPublicKey{}, fmt.Errorf("the runtime's audit key generate did not answer as documented: %w", err)
	}
	return key, nil
}

// rotationTrail is the identity of the trail a rotation would be made on,
// where its sidecar has no line yet: the trail of its last chained record, as
// the runtime's `audit checkpoint` names it. Where the trail has no chained
// record, there is nothing to rotate on; that, a refusal in the runtime's
// words, or an answer Desk cannot read is the refusal, with nothing made.
func (s *Server) rotationTrail(ctx context.Context, project heldDir) (string, *lockFailure) {
	head, said, err := s.readCheckpointHead(ctx, project, handoverChain{})
	switch {
	case err != nil:
		s.log.Printf("desk: the trail of desk %s could not be named for a rotation: %v", s.signingKeyName(), err)
		return "", &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was rotated: Desk could not learn which trail the rotation would be made on: " + strings.TrimRight(err.Error(), ".") + "."}
	case len(said) > 0:
		var words []string
		for _, diagnostic := range said {
			if message := strings.TrimSpace(diagnostic.Message); message != "" {
				words = append(words, message)
			}
		}
		return "", &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was rotated: " + strings.TrimRight(strings.Join(words, " "), ".") + "."}
	case head == nil:
		return "", &lockFailure{http.StatusConflict, CodeBadRequest, "Nothing was rotated: the trail has no chained record yet, so there is no trail to rotate the key of; the first record is signed with whichever key the project names."}
	case !keyIDForm.MatchString(head.Identity):
		return "", &lockFailure{http.StatusInternalServerError, CodeInternal, "Nothing was rotated: the runtime named the trail in a form Desk does not read."}
	}
	return head.Identity, nil
}

// readRotated reads what `audit key rotate --format json` printed: the
// sequence the next key takes over from, and the trail identity it rotated
// on, where it rotated from current to next as asked, exit 0 and "rotated",
// on the trail the rotation's journal names; and otherwise what it said, in
// its own words where it gave any.
func readRotated(out []byte, runErr error, current, next deskPublicKey, trail string) (int64, string, string, bool) {
	var answer struct {
		Command       string              `json:"command"`
		Status        string              `json:"status"`
		Trail         string              `json:"trail"`
		At            int64               `json:"at"`
		From          string              `json:"from"`
		Next          string              `json:"next"`
		NextPublicKey string              `json:"nextPublicKey"`
		Diagnostics   []runtimeDiagnostic `json:"diagnostics"`
	}
	decoded := out != nil && json.Unmarshal(out, &answer) == nil
	if runErr == nil && decoded && answer.Command == "audit key rotate" && answer.Status == "rotated" &&
		answer.At >= 1 && answer.At < sidecarIntegerLimit && answer.From == current.KeyID && answer.Next == next.KeyID && answer.NextPublicKey == next.PublicKey &&
		answer.Trail == trail {
		return answer.At, answer.Trail, "", true
	}
	var said []string
	for _, diagnostic := range answer.Diagnostics {
		if message := strings.TrimSpace(diagnostic.Message); message != "" {
			said = append(said, message)
		}
	}
	switch {
	case len(said) > 0:
		return 0, "", strings.Join(said, " "), false
	case runErr != nil:
		var exit *exec.ExitError
		if errors.As(runErr, &exit) {
			return 0, "", fmt.Sprintf("its audit key rotate exited %d and gave no reason", exit.ExitCode()), false
		}
		return 0, "", runErr.Error(), false
	}
	return 0, "", "its audit key rotate did not answer as documented", false
}

// publicKeyOf is the public key the runtime reads from the seed kept as name
// in dir (`jpack audit key public <path> --format json`, run in project).
// Desk never reads a seed's bytes itself. It is the runtime's answer, as it
// gave it; its error is the runtime's words where it gave any.
func (s *Server) publicKeyOf(ctx context.Context, project heldDir, dir *signingDir, name string) (deskPublicKey, error) {
	out, runErr := runRuntime(ctx, s.cfg.JpackBin, project, "audit", "key", "public", filepath.Join(dir.path, name), "--format", "json")
	var answer struct {
		Command     string              `json:"command"`
		Status      string              `json:"status"`
		PublicKey   string              `json:"publicKey"`
		KeyID       string              `json:"keyId"`
		Diagnostics []runtimeDiagnostic `json:"diagnostics"`
	}
	decoded := out != nil && json.Unmarshal(out, &answer) == nil
	if runErr != nil || !decoded || answer.Command != "audit key public" || answer.Status != "read" {
		s.log.Printf("desk: the runtime did not read the key Desk keeps as %s: %v %s", name, runErr, out)
		var said []string
		for _, diagnostic := range answer.Diagnostics {
			if message := strings.TrimSpace(diagnostic.Message); message != "" {
				said = append(said, message)
			}
		}
		if len(said) > 0 {
			return deskPublicKey{}, errors.New(strings.Join(said, " "))
		}
		return deskPublicKey{}, errors.New("its audit key public did not answer as documented")
	}
	return deskPublicKey{PublicKey: answer.PublicKey, KeyID: answer.KeyID}, nil
}
