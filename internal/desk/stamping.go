package desk

// Stamping a desk's trail with a time-stamping authority the owner chooses
// (ADR-0010, section 3, and the maintainer's answer to its question 5;
// delivery row 7).
//
// # What a stamp is
//
// `jpack audit stamp` asks an RFC 3161 authority to stamp the checkpoint of
// the trail's last chained record, and keeps the token in `stamps.jsonl`
// beside the trail, under that file's own lock. The request carries the
// SHA-256 of the checkpoint's canonical form, a nonce and a request for the
// authority's certificate, and nothing else of the trail. The runtime is
// idempotent by the checkpoint's digest: a checkpoint stamped already is not
// asked for again (`already-stamped`), and costs nothing. A stamp shows that
// the checkpoint, and every line before it, existed by the time the authority
// states, as far as that authority is independent of the operator; whether
// the authority is to be trusted is for a verifier with roots to decide,
// `audit verify --tsa-roots`.
//
// # The settings, per desk, kept by Desk
//
// **No authority by default.** Choosing one is a trust decision, and each
// stamp sends that party the checkpoint's digest (runtime ADR-0047,
// "Privacy"). The owner sets one, behind a confirmation in the rotation's
// pattern: `POST /api/audit/stamping/check` holds the proposal to every rule
// below and answers what Desk would keep and a token, a nonce and a MAC under
// the desk's own review key over the proposal and the settings as Desk read
// them; `POST /api/audit/stamping` keeps the proposal only where it gives the
// same MAC against the settings as they are then, once per nonce. The page
// states, before anything runs, that choosing an authority is a trust
// decision, that each stamp sends it the checkpoint's digest, and that
// nothing on the decision path waits for a stamp.
//
// They are kept outside the project, in Desk's configuration folder, under
// `stamping/<name>/`, where `<name>` names the desk as its signing key is
// named (`signingKeyName`): a desk Desk made by its id, the project Desk was
// started on by the hex SHA-256 of its path. Each folder is made 0700 and
// held as the hand-over's are (`openOwnFolder`); each file is 0600, written
// whole and read back (`writePrivateData`):
//
//   - `settings.json`: the authority's address, http or https, passed as
//     `--tsa` and never written into `jpack.json` (question 5); the interval,
//     in minutes, 5 to 1440, 60 unless the owner says otherwise; the policy
//     OIDs; the SHA-256 of the roots and of each revocation list; and when
//     the owner set them, by Desk's clock. It is written last, so it names
//     only files already written.
//   - `roots-<sha256>.pem`: the root certificates, PEM, as the owner gave
//     them, passed as `--tsa-roots` by path.
//   - `crl-<sha256>.crl`: each revocation list, PEM or DER, as given, passed
//     as `--tsa-crls`.
//
// **Read back whole, and held to the record**, before any of it is used
// (`readStampingIn`): `settings.json` to the writer's rules, each file to the
// digest the settings keep and to what the runtime reads (at least one
// certificate, or one list). Anything else is said, and nothing of it is
// used: no stamp, and no root passed. "Could not be read now" is never "not
// set". Removing the authority, on a token over the settings as shown,
// removes these files, stops the stamping and keeps the trail's stamps file
// as it is.
//
// # The scheduler
//
// Desk is the resident process the runtime lacks. Each desk has one loop
// (`stampScheduler`), started with its server and stopped with it. It wakes
// once a minute, and where an authority is set and the interval has passed
// since the last attempt, it makes one attempt: it asks the runtime where the
// trail ends (`audit checkpoint --format json`), and only where the head is
// not the last checkpoint known stamped, by trail, sequence and record digest,
// runs
//
//	jpack audit stamp --config jpack.json --tsa <address> --timeout 15s --format json
//
// through `runRuntime`, in the desk's folder. One run at a time per desk:
// "Stamp now", the owner's request, takes the same turn, and a run that finds
// another in progress starts nothing. A failure, the runtime's
// (JPS-AUDIT-STAMP-NO-AUTHORITY, -UNREACHABLE, -REFUSED, -REJECTED,
// -INVALID) or Desk's, is shown in its words, with no path, and the next
// interval tries again: one attempt per interval, never a retry storm.
//
// **The checkpoint known stamped is replaced, never only advanced** (line
// audit, finding 5): by each stamp run's answer, and by each `audit verify`
// with roots, which says how far the stamps it accepts reach, or that none
// does. A head below it, or at its sequence with another record, is a trail
// restored to an earlier prefix and written or repaired since: it is stamped
// again. A check knows the record digest of the checkpoint it found stamped
// only where that checkpoint is the report's head (`head`); elsewhere it
// knows a trail and a sequence, and no head is taken for that checkpoint
// without its digest (second review of #294). The runtime asks the authority
// nothing for a checkpoint stamped already, so a replacement that lowers what
// is known, or one with no digest, costs at most one `audit stamp` answering
// `already-stamped`, never a stamp.
//
// **Off the decision path.** Nothing a deciding run does waits for it; while
// Desk is not running, nothing is stamped and records stay pending. **A stop
// never kills a stamp mid-write**: the stamp runs without the server's
// cancellation, the runtime's own `--timeout 15s` ends its wait inside
// `runRuntime`'s 20-second bound, and the server's Close waits for it.
//
// # Verification with roots
//
// The decision record's `audit verify` is given `--tsa-roots`, and
// `--tsa-policy` and `--tsa-crls` where set (audit_record.go), so the
// report's `coverage.stamped`, the stamps it accepted and the lag between
// records' `at` and their first stamp are the runtime's. The chained records
// after `stamped.through` are pending a stamp, counted from what the report
// says of them (`chainedAfter`), never by subtracting one line's number from
// another's: a line a repair names as damaged is not a record (line audit,
// finding 7). Where the report does not say, the count is of lines, and is
// said as one. Without roots the runtime says the stamps were not checked.
// The page shows the checkpoint the last stamp run named as the authority's
// answer to Desk's request, not as a stamp checked, wherever the report's
// stamps do not reach it in the same trail: no roots, no stamp, one below it,
// or another trail's (line audit, finding 5).
//
// # What a stamp does not establish
//
// ADR-0010, section 7: when any record was made (a stamp is an upper bound on
// existence); anything against an authority that colludes; revocation, where
// no supplied list speaks for it; anything after the last checkpoint stamped.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// stampingDirName is the folder in Desk's configuration folder that holds
	// each desk's stamping settings, in a folder of its own.
	stampingDirName = "stamping"
	// stampingKept is what Desk keeps there, as a refusal to open it names it.
	stampingKept = "its stamping settings"
	// stampingSettingsName is the settings file in a desk's folder.
	stampingSettingsName = "settings.json"
	// stampingVersion is the settings file's version.
	stampingVersion = "1"
	// stampingDefaultInterval, stampingMinInterval and stampingMaxInterval
	// are the interval, in minutes: an hour unless the owner says otherwise,
	// and from five minutes to a day.
	stampingDefaultInterval = 60
	stampingMinInterval     = 5
	stampingMaxInterval     = 24 * 60
	// stampingAuthorityLimit bounds the authority's address, in bytes.
	stampingAuthorityLimit = 2048
	// stampingFileLimit bounds the roots and each revocation list, in bytes:
	// what the writer accepts and the reader reads. The runtime reads up to
	// 16 MiB of each (runtime 0.27.1, `readTrustFile`).
	stampingFileLimit = 256 << 10
	// stampingMaxPolicies and stampingMaxCRLs bound the policy OIDs and the
	// revocation lists; stampingPolicyLimit bounds one OID, in bytes.
	stampingMaxPolicies = 8
	stampingPolicyLimit = 64
	stampingMaxCRLs     = 4
	// stampingSettingsLimit bounds the settings file. The largest the writer
	// accepts is about 3 KiB (`TestTheLargestStampingSettingsFitTheirBound`).
	stampingSettingsLimit = 8 << 10
	// stampingRequestLimit bounds a check's or a confirmation's body: the
	// roots, and each list in base64.
	stampingRequestLimit = 2 << 20
	// stampingSmallRequestLimit bounds a removal's or a stamp's body.
	stampingSmallRequestLimit = 4 << 10
	// stampWakeEvery is how often a desk's scheduler looks whether a stamp
	// is due.
	stampWakeEvery = time.Minute
	// auditStampCommand is the command's name as its JSON answer gives it.
	auditStampCommand = "audit stamp"
	// The purposes a stamping token names, so that no other token of this
	// desk's confirms either.
	stampingSetPurpose    = "set-stamping-authority"
	stampingRemovePurpose = "remove-stamping-authority"
	// stampingNonceLength is a token's nonce, in hexadecimal characters, and
	// stampingTokenLength a token's: the nonce, then the MAC.
	stampingNonceLength = 32
	stampingTokenLength = stampingNonceLength + 2*sha256.Size
)

// Desk's own sentences about stamping. The page lists them
// (STAMPING_REASONS), so that each is shown in the owner's language.
const (
	stampingRequestWords   = "Send the stamping settings as JSON: the authority's address, the interval in minutes, the root certificates, and any policy OIDs and revocation lists."
	stampingAuthorityWords = "Give the authority's address as an http or https URL of at most 2048 characters, with a host, and with no user name, password, fragment, space or control character."
	stampingIntervalWords  = "Give the interval as a whole number of minutes from 5 to 1440."
	stampingRootsWords     = "Give the root certificates you trust for this authority as PEM, at most 262144 bytes of CERTIFICATE blocks and nothing else, at least one, each of which can be read."
	stampingPoliciesWords  = "Give at most 8 policy OIDs, each once, each a dotted object identifier such as 1.2.3.4 of at most 64 characters."
	stampingCRLsWords      = "Give at most 4 revocation lists, each once, each of at most 262144 bytes: X509 CRL blocks in PEM and nothing else, or one list in DER, each of which can be read."
	stampingConfirmWords   = "Confirm the stamping settings with the token Desk gave when it showed them."
	stampingRemoveWords    = "Remove the time-stamping authority with the token the decision record gave."
	stampingStaleWords     = "The stamping settings changed after Desk showed them, so nothing was changed. Check the decision record again."
	stampingUsedWords      = "This confirmation was used already, so nothing was changed. Check the decision record again for a fresh one."
	stampingNoneWords      = "No time-stamping authority is set for this desk, so Desk stamps nothing."
	stampingBusyWords      = "A stamp run for this desk is in progress, so Desk starts no other. The decision record shows its outcome once it ends."
	stampingClosedWords    = "Desk is stopping, so it starts no stamp run."
	stampingUndocumented   = "The runtime's audit stamp did not answer as documented, so Desk cannot say whether it stamped. The decision record, checked again, shows the stamps the runtime accepts."
	stampingStartWords     = "A stamp request needs no settings: send {} as JSON."
	stampingMovedWords     = "The stamping settings changed while the stamp run made its checks, so it asked for no stamp. The next run uses the settings as they are now."
	// With a reason, in Desk's or the custody's words.
	stampingUnreadWords  = "Desk could not read the stamping settings it keeps for this desk, so it uses none of them now: %s."
	stampingKeepWords    = "Desk could not keep these stamping settings, and the decision record shows the settings it reads now: %s."
	stampingCustodyWords = "Desk keeps no stamping settings here: %s."
	stampingNotRunWords  = "The stamp run did not finish: %s."
	stampingHeadWords    = "Desk could not tell where the trail ends now, so it asked for no stamp: %s."
	stampingOlderWords   = "This runtime (jpack %s) has no audit stamp. Stamping needs jpack %s or later."
	stampingHeldWords    = "Desk could not hand the runtime the roots it keeps for this desk, so no stamp was checked: %s."
)

var (
	// stampingFileName is a roots or revocation-list file Desk keeps, named
	// by the SHA-256 of its bytes.
	stampingFileName = regexp.MustCompile(`^(?:roots-[0-9a-f]{64}\.pem|crl-[0-9a-f]{64}\.crl)$`)
	// stampingName is the name a desk's settings are kept under: a desk's id,
	// or the startup project's 64 hexadecimal characters.
	stampingName = runnerKeyName
)

// stampTimeout is the runtime's own wait for the authority, `--timeout`:
// inside runRuntime's bound, so the runtime ends itself rather than being
// killed while it writes `stamps.jsonl` (ADR-0010, section 3). A variable
// only so a test can see an authority that never answers without waiting
// it out.
var stampTimeout = "15s"

// stampClock is Desk's clock for stamping: when a run happened, when the
// settings were set, and whether a stamp is due. A variable only so a test
// can fix it.
var stampClock = time.Now

// newStampWake is what wakes the scheduler of the desk whose settings are
// kept under name, and how to stop it: a ticker every stampWakeEvery. A
// variable only so a test can wake one desk's by hand.
var newStampWake = func(name string) (<-chan time.Time, func()) {
	ticker := time.NewTicker(stampWakeEvery)
	return ticker.C, ticker.Stop
}

// testHookStampWoke runs after a scheduler has acted on each wake, and is nil
// outside tests. testHookStampRun runs inside a stamp run's turn, with the
// settings read again and held, before the runtime is asked to stamp, and is
// nil outside tests: a test holds a run there to see a second one refused,
// or a change of settings wait. testHookStampChecked runs in the turn after
// Desk's checks and the head, before the settings are read again, and is nil
// outside tests: a test changes the settings there. testHookStampingRead runs after the
// decision record has read the settings, before it checks the paths it
// passes, and is nil outside tests: a test puts another file under a name
// there.
var (
	testHookStampWoke    func()
	testHookStampRun     func()
	testHookStampChecked func()
	testHookStampingRead func()
)

/* What Desk keeps ------------------------------------------------------------ */

// stampingFile is `settings.json` as Desk writes it.
type stampingFile struct {
	Version         string   `json:"version"`
	Authority       string   `json:"authority"`
	IntervalMinutes int64    `json:"intervalMinutes"`
	Roots           string   `json:"roots"`
	Policies        []string `json:"policies"`
	CRLs            []string `json:"crls"`
	SetAt           int64    `json:"setAt"`
}

// stampingSettings is a desk's settings as Desk read them back and held them
// to their record: the settings file, its bytes, the roots and lists it names,
// by their bytes, and each of those files as found when read, by name.
type stampingSettings struct {
	file  stampingFile
	raw   []byte
	roots []byte
	crls  [][]byte
	found map[string]os.FileInfo
}

// rootsFileName and crlFileName are the names of the roots and of a
// revocation list whose SHA-256 is digest, in a desk's stamping folder.
func rootsFileName(digest string) string {
	return "roots-" + strings.TrimPrefix(digest, "sha256:") + ".pem"
}

func crlFileName(digest string) string {
	return "crl-" + strings.TrimPrefix(digest, "sha256:") + ".crl"
}

// checkAuthority is whether address is an authority's address Desk keeps: an
// http or https URL with a host, as the runtime asks (runtime 0.27.1, `audit
// stamp`), of at most stampingAuthorityLimit bytes of valid UTF-8, with no
// user name or password (Desk keeps no credential for an authority), no
// fragment, and no space or control character.
func checkAuthority(address string) bool {
	if address == "" || len(address) > stampingAuthorityLimit || !utf8.ValidString(address) ||
		strings.IndexFunc(address, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
		return false
	}
	parsed, err := url.Parse(address)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.Opaque == "" &&
		parsed.User == nil && parsed.Fragment == "" && !strings.Contains(address, "#")
}

// policyForm is a dotted object identifier: at least two arcs, each digits
// with no leading zero.
var policyForm = regexp.MustCompile(`^(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))+$`)

// checkPolicy is whether text is a policy OID the runtime reads (runtime
// 0.27.1, `parseOID`: at least two arcs, each a decimal integer it can read,
// with no leading zero), written with digits alone, and of at most
// stampingPolicyLimit bytes.
func checkPolicy(text string) bool {
	if len(text) > stampingPolicyLimit || !policyForm.MatchString(text) {
		return false
	}
	for _, part := range strings.Split(text, ".") {
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

// pemOnly is the PEM blocks data holds, where it holds blocks of kind and
// nothing else: no block of another type, no headers, and nothing outside
// the blocks but white space. Desk keeps the file as given, so a block the
// runtime would pass over, a private key among them, is never kept beside
// what the page shows (review round 1).
func pemOnly(data []byte, kind string) ([]*pem.Block, bool) {
	var blocks []*pem.Block
	rest := data
	for {
		rest = bytes.TrimLeft(rest, " \t\r\n")
		if len(rest) == 0 {
			return blocks, true
		}
		if !bytes.HasPrefix(rest, []byte("-----BEGIN ")) {
			return nil, false
		}
		block, after := pem.Decode(rest)
		if block == nil || block.Type != kind || len(block.Headers) > 0 {
			return nil, false
		}
		blocks = append(blocks, block)
		rest = after
	}
}

// readRoots is the root certificates in data, as the runtime reads them
// (runtime 0.27.1, `readStampOptions`): each PEM block of type CERTIFICATE,
// which must be one; at least one; and nothing else (`pemOnly`).
func readRoots(data []byte) ([]*x509.Certificate, bool) {
	if len(data) == 0 || len(data) > stampingFileLimit {
		return nil, false
	}
	blocks, ok := pemOnly(data, "CERTIFICATE")
	if !ok {
		return nil, false
	}
	var roots []*x509.Certificate
	for _, block := range blocks {
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, false
		}
		roots = append(roots, certificate)
	}
	return roots, len(roots) > 0
}

// readCRLs is the revocation lists in data, as the runtime reads them
// (runtime 0.27.1, `parseCRLs`): PEM blocks of type X509 CRL and nothing
// else (`pemOnly`), or else one DER list; at least one.
func readCRLs(data []byte) ([]*x509.RevocationList, bool) {
	if len(data) == 0 || len(data) > stampingFileLimit {
		return nil, false
	}
	if bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("-----BEGIN ")) {
		blocks, ok := pemOnly(data, "X509 CRL")
		if !ok {
			return nil, false
		}
		var lists []*x509.RevocationList
		for _, block := range blocks {
			list, err := x509.ParseRevocationList(block.Bytes)
			if err != nil {
				return nil, false
			}
			lists = append(lists, list)
		}
		return lists, true
	}
	list, err := x509.ParseRevocationList(data)
	if err != nil {
		return nil, false
	}
	return []*x509.RevocationList{list}, true
}

// checkStampingFile is whether file is one Desk writes: its version, an
// authority, an interval and policies by the writer's rules, and digests of
// the roots and of each list, each list once.
func checkStampingFile(file stampingFile) bool {
	if file.Version != stampingVersion || !checkAuthority(file.Authority) ||
		file.IntervalMinutes < stampingMinInterval || file.IntervalMinutes > stampingMaxInterval ||
		!recordForm.MatchString(file.Roots) || file.Policies == nil || file.CRLs == nil ||
		len(file.Policies) > stampingMaxPolicies || len(file.CRLs) > stampingMaxCRLs || file.SetAt < 0 {
		return false
	}
	for i, policy := range file.Policies {
		if !checkPolicy(policy) || slices.Contains(file.Policies[:i], policy) {
			return false
		}
	}
	for i, digest := range file.CRLs {
		if !recordForm.MatchString(digest) || slices.Contains(file.CRLs[:i], digest) {
			return false
		}
	}
	return true
}

// openStamping opens this desk's stamping folder, `stamping/<name>/` in
// Desk's configuration folder; with create, made owner-only where missing.
// A folder that is not there is fs.ErrNotExist.
func (s *Server) openStamping(create bool) (*os.Root, error) {
	if !s.assistant.usable() {
		return nil, fmt.Errorf(stampingCustodyWords, strings.TrimRight(s.custodyWords(s.assistant.problem.Error()), "."))
	}
	name := s.signingKeyName()
	if !stampingName.MatchString(name) {
		return nil, fmt.Errorf(stampingCustodyWords, "this desk has no name its settings can be kept under")
	}
	root, _, err := openOwnFolder(s.assistant.root, []string{stampingDirName, name}, create, stampingKept)
	return root, err
}

// stampingPath is the path the runtime is given a file of this desk's
// stamping folder by.
func (s *Server) stampingPath(name string) string {
	return filepath.Join(s.configDir, stampingDirName, s.signingKeyName(), name)
}

// readStampingIn reads this desk's settings through folder, and holds them
// to their record: found is false where no settings file is there. Anything
// that is not what Desk writes, a file a digest does not match, roots the
// runtime would not read, and a failure to read, are each an error.
func readStampingIn(folder *os.Root) (settings stampingSettings, found bool, err error) {
	raw, err := readPrivateData(folder, stampingSettingsName, stampingSettingsLimit)
	if errors.Is(err, fs.ErrNotExist) {
		return stampingSettings{}, false, nil
	}
	if err != nil {
		return stampingSettings{}, false, err
	}
	var file stampingFile
	if err := decodeDataJSON(raw, &file); err != nil || !checkStampingFile(file) {
		return stampingSettings{}, false, fmt.Errorf("%s is not a settings file Desk wrote", stampingSettingsName)
	}
	settings = stampingSettings{file: file, raw: raw, found: map[string]os.FileInfo{}}
	read := func(name, digest string) ([]byte, error) {
		data, opened, err := readPrivateFile(folder, name, stampingFileLimit)
		if err != nil {
			return nil, err
		}
		if sha256Digest(data) != digest {
			return nil, fmt.Errorf("%s is not the file its settings name", name)
		}
		settings.found[name] = opened
		return data, nil
	}
	if settings.roots, err = read(rootsFileName(file.Roots), file.Roots); err != nil {
		return stampingSettings{}, false, err
	}
	if _, ok := readRoots(settings.roots); !ok {
		return stampingSettings{}, false, errors.New("the roots it keeps hold no certificate the runtime can read")
	}
	for _, digest := range file.CRLs {
		data, err := read(crlFileName(digest), digest)
		if err != nil {
			return stampingSettings{}, false, err
		}
		if _, ok := readCRLs(data); !ok {
			return stampingSettings{}, false, errors.New("a revocation list it keeps holds no list the runtime can read")
		}
		settings.crls = append(settings.crls, data)
	}
	return settings, true, nil
}

// readStamping reads this desk's settings, as readStampingIn does. A folder
// that is not there is no settings. The caller holds stampingMu.
func (s *Server) readStamping() (stampingSettings, bool, error) {
	folder, err := s.openStamping(false)
	if errors.Is(err, fs.ErrNotExist) {
		return stampingSettings{}, false, nil
	}
	if err != nil {
		return stampingSettings{}, false, err
	}
	defer folder.Close()
	return readStampingIn(folder)
}

// readStampingBytes is this desk's settings file's bytes as they are now,
// whatever they hold, for a token over them; found is false where there is
// none. The caller holds stampingMu.
func (s *Server) readStampingBytes() ([]byte, bool, error) {
	folder, err := s.openStamping(false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer folder.Close()
	raw, err := readPrivateData(folder, stampingSettingsName, stampingSettingsLimit)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return raw, err == nil, err
}

/* A proposal ------------------------------------------------------------------ */

// stampingProposal is what the owner proposes, as the page sends it: the
// roots as PEM text, and each revocation list's bytes in base64.
type stampingProposal struct {
	Authority       string   `json:"authority"`
	IntervalMinutes *int64   `json:"intervalMinutes"`
	Roots           string   `json:"roots"`
	Policies        []string `json:"policies"`
	CRLs            []string `json:"crls"`
}

// stampingPlan is a proposal held to every rule Desk keeps settings by.
type stampingPlan struct {
	authority string
	interval  int64
	roots     []byte
	policies  []string
	crls      [][]byte
}

// plan is the proposal held to the writer's rules, or the sentence that says
// which it breaks. The interval is 60 minutes where none is given.
func (p stampingProposal) plan() (stampingPlan, string) {
	plan := stampingPlan{authority: p.Authority, interval: stampingDefaultInterval, roots: []byte(p.Roots), policies: []string{}, crls: [][]byte{}}
	if !checkAuthority(p.Authority) {
		return stampingPlan{}, stampingAuthorityWords
	}
	if p.IntervalMinutes != nil {
		plan.interval = *p.IntervalMinutes
	}
	if plan.interval < stampingMinInterval || plan.interval > stampingMaxInterval {
		return stampingPlan{}, stampingIntervalWords
	}
	if _, ok := readRoots(plan.roots); !ok || !utf8.Valid(plan.roots) {
		return stampingPlan{}, stampingRootsWords
	}
	if len(p.Policies) > stampingMaxPolicies {
		return stampingPlan{}, stampingPoliciesWords
	}
	for i, policy := range p.Policies {
		if !checkPolicy(policy) || slices.Contains(p.Policies[:i], policy) {
			return stampingPlan{}, stampingPoliciesWords
		}
		plan.policies = append(plan.policies, policy)
	}
	if len(p.CRLs) > stampingMaxCRLs {
		return stampingPlan{}, stampingCRLsWords
	}
	seen := map[string]bool{}
	for _, encoded := range p.CRLs {
		data, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || seen[sha256Digest(data)] {
			return stampingPlan{}, stampingCRLsWords
		}
		if _, ok := readCRLs(data); !ok {
			return stampingPlan{}, stampingCRLsWords
		}
		seen[sha256Digest(data)] = true
		plan.crls = append(plan.crls, data)
	}
	return plan, ""
}

// file is the settings file the plan is kept as, set at the time given.
func (p stampingPlan) file(setAt int64) stampingFile {
	file := stampingFile{Version: stampingVersion, Authority: p.authority, IntervalMinutes: p.interval, Roots: sha256Digest(p.roots), Policies: slices.Clone(p.policies), CRLs: []string{}, SetAt: setAt}
	for _, crl := range p.crls {
		file.CRLs = append(file.CRLs, sha256Digest(crl))
	}
	return file
}

// digest is the SHA-256 of what the plan keeps, as a token binds it.
func (p stampingPlan) digest() string {
	data, _ := json.Marshal(p.file(0))
	return sha256Digest(data)
}

/* What the page is shown ------------------------------------------------------ */

// stampingCertificate is one root certificate as the page is shown it: its
// subject, with any control or format character as "?", and the SHA-256 of
// its DER bytes.
type stampingCertificate struct {
	Subject string `json:"subject"`
	SHA256  string `json:"sha256"`
}

// stampingList is one revocation-list file as the page is shown it: the
// SHA-256 of its bytes, and how many lists it holds.
type stampingList struct {
	SHA256 string `json:"sha256"`
	Lists  int    `json:"lists"`
}

// stampingShown is settings as the page is shown them: before a
// confirmation, and in the decision record.
type stampingShown struct {
	Authority       string                `json:"authority"`
	IntervalMinutes int64                 `json:"intervalMinutes"`
	Policies        []string              `json:"policies"`
	Roots           []stampingCertificate `json:"roots"`
	CRLs            []stampingList        `json:"crls"`
	SetAt           int64                 `json:"setAt,omitempty"`
}

// shownText is text as the page is shown it: valid UTF-8, any control or
// format character, line or paragraph separator as "?", and at most 200
// characters.
func shownText(text string) string {
	text = displayedPath(strings.ToValidUTF8(text, "?"))
	if runes := []rune(text); len(runes) > 200 {
		return string(runes[:200]) + "…"
	}
	return text
}

// showStamping is what the page is shown of a plan, set at setAt.
func showStamping(plan stampingPlan, setAt int64) stampingShown {
	shown := stampingShown{Authority: plan.authority, IntervalMinutes: plan.interval, Policies: slices.Clone(plan.policies), Roots: []stampingCertificate{}, CRLs: []stampingList{}, SetAt: setAt}
	roots, _ := readRoots(plan.roots)
	for _, root := range roots {
		shown.Roots = append(shown.Roots, stampingCertificate{Subject: shownText(root.Subject.String()), SHA256: sha256Digest(root.Raw)})
	}
	for _, crl := range plan.crls {
		lists, _ := readCRLs(crl)
		shown.CRLs = append(shown.CRLs, stampingList{SHA256: sha256Digest(crl), Lists: len(lists)})
	}
	return shown
}

// plan is the plan settings read back are kept as.
func (settings stampingSettings) plan() stampingPlan {
	return stampingPlan{authority: settings.file.Authority, interval: settings.file.IntervalMinutes, roots: settings.roots, policies: settings.file.Policies, crls: settings.crls}
}

// What the decision record says of stamping (`auditStamping.State`).
const (
	// stampingStateNone: no authority is set.
	stampingStateNone = "none"
	// stampingStateSet: an authority is set, and its settings were read back
	// and held to their record.
	stampingStateSet = "set"
	// stampingStateUnread: the settings could not be read now, or are not
	// what Desk writes; Problem says why, and none of them is used.
	stampingStateUnread = "unread"
	// stampingStateUnavailable: Desk keeps no settings here; Problem says
	// why.
	stampingStateUnavailable = "unavailable"
)

// auditStamping is the decision record's word on stamping, given with a
// report and with the runtime's refusal: the settings, and whether their
// roots were passed to this check; how many records are pending a stamp,
// where the runtime checked the stamps; the last stamp run since Desk
// started, and whether one is running; and the token that confirms a
// removal of the settings as shown.
type auditStamping struct {
	State       string         `json:"state"`
	Settings    *stampingShown `json:"settings,omitempty"`
	Problem     string         `json:"problem,omitempty"`
	RemoveToken string         `json:"removeToken,omitempty"`
	// Passed is whether the roots, and the policies and lists, were given to
	// this check as `--tsa-roots`, `--tsa-policy` and `--tsa-crls`; where
	// they were not, PassProblem says why.
	Passed      bool   `json:"passed,omitempty"`
	PassProblem string `json:"passProblem,omitempty"`
	// Pending is the chained records after the last one a stamp the runtime
	// accepted covers, every one where none does, where the report says how
	// many (`chainedAfter`); PendingLines, in its place where it does not, is
	// the lines after that record through the trail's last chained record.
	Pending      *int64    `json:"pending,omitempty"`
	PendingLines *int64    `json:"pendingLines,omitempty"`
	Last         *stampRun `json:"last,omitempty"`
	Running      bool      `json:"running,omitempty"`
}

/* The tokens ------------------------------------------------------------------ */

// newStampingNonce is a fresh nonce for a token, in hexadecimal.
func newStampingNonce() string {
	var nonce [stampingNonceLength / 2]byte
	// crypto/rand never fails, and never returns short (Go 1.24 and later).
	_, _ = rand.Read(nonce[:])
	return hex.EncodeToString(nonce[:])
}

// stampingToken binds a confirmation to this desk, to one attempt by nonce,
// to its purpose, to what it would keep (proposal: a plan's digest, or none
// for a removal), and to the settings file as Desk read it (current: its
// SHA-256, or none where there is none). It is the nonce, then a MAC under
// the desk's own review key.
func (s *Server) stampingToken(purpose, nonce, proposal, current string) string {
	payload, _ := json.Marshal(struct {
		Purpose  string `json:"purpose"`
		Nonce    string `json:"nonce"`
		Desk     string `json:"desk"`
		Project  string `json:"project"`
		Proposal string `json:"proposal"`
		Current  string `json:"current"`
	}{purpose, nonce, s.cfg.deskID, s.projectDir, proposal, current})
	mac := hmac.New(sha256.New, s.reviewKey[:])
	mac.Write(payload)
	return nonce + hex.EncodeToString(mac.Sum(nil))
}

// currentDigest is the settings file's bytes as a token binds them.
func currentDigest(raw []byte, found bool) string {
	if !found {
		return "none"
	}
	return sha256Digest(raw)
}

/* The scheduler ------------------------------------------------------------- */

// The outcomes of a stamp run (`stampRun.Status`).
const (
	// stampStamped: the authority stamped the checkpoint, and the runtime
	// kept the token.
	stampStamped = "stamped"
	// stampAlready: the runtime found the checkpoint stamped already, and
	// asked nothing.
	stampAlready = "already-stamped"
	// stampRefused: the runtime refused, in its words (Diagnostics).
	stampRefused = "refused"
	// stampProblem: Desk could not run it, or could not read its answer, in
	// Desk's words (Problem).
	stampProblem = "problem"
)

// stampRun is one stamp run's outcome: when, by Desk's clock; whether the
// owner asked for it; and the runtime's answer (the checkpoint it named, by
// trail, sequence and record digest, and for a stamp the time the authority
// states, the time the checkpoint existed by and the policy, each as the
// runtime printed it), its refusal in its words, or Desk's.
type stampRun struct {
	At          int64               `json:"at"`
	Requested   bool                `json:"requested,omitempty"`
	Status      string              `json:"status"`
	Trail       string              `json:"trail,omitempty"`
	Sequence    int64               `json:"sequence,omitempty"`
	Digest      string              `json:"digest,omitempty"`
	StampedAt   string              `json:"stampedAt,omitempty"`
	ExistedBy   string              `json:"existedBy,omitempty"`
	Policy      string              `json:"policy,omitempty"`
	Diagnostics []runtimeDiagnostic `json:"diagnostics,omitempty"`
	Problem     string              `json:"problem,omitempty"`
}

// stampScheduler is one desk's stamping: its loop, one run at a time, and
// what it last did.
type stampScheduler struct {
	s *Server
	// turn is held by the one run in progress, scheduled or asked for.
	turn    sync.Mutex
	running atomic.Bool
	// mu guards the rest.
	mu sync.Mutex
	// lastAttempt is when the loop last made an attempt, or a run was asked
	// for; zero before the first.
	lastAttempt time.Time
	// stamped is the last checkpoint known stamped, nil where none is: a
	// run's answer, by trail, sequence and digest, or a check with roots, by
	// trail and sequence, and by digest where the checkpoint is the report's
	// head, whichever came last (knowStamped).
	stamped *checkpointHead
	last    *stampRun
	// said is the last problem with the settings written to Desk's log.
	said   string
	closed bool
	stop   chan struct{}
	done   chan struct{}
	// cancel ends what an attempt runs before the stamp itself.
	ctx    context.Context
	cancel context.CancelFunc
}

// startStamping starts this desk's scheduler: one loop, until Close.
func (s *Server) startStamping() {
	ctx, cancel := context.WithCancel(context.Background())
	st := &stampScheduler{s: s, stop: make(chan struct{}), done: make(chan struct{}), ctx: ctx, cancel: cancel}
	s.stamping = st
	wake, stop := newStampWake(s.signingKeyName())
	go func() {
		defer close(st.done)
		defer stop()
		for {
			select {
			case <-st.stop:
				return
			case <-wake:
			}
			st.wake()
			if testHookStampWoke != nil {
				testHookStampWoke()
			}
		}
	}()
}

// close stops the loop, and waits for a run in progress, whoever started
// it: a stamp is never killed mid-write. Once closed, nothing starts a run.
func (st *stampScheduler) close() {
	if st == nil {
		return
	}
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		return
	}
	st.closed = true
	st.mu.Unlock()
	close(st.stop)
	st.cancel()
	<-st.done
	st.turn.Lock()
	st.turn.Unlock()
}

// report is the last run, and whether one is running.
func (st *stampScheduler) report() (*stampRun, bool) {
	if st == nil {
		return nil, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.last == nil {
		return nil, st.running.Load()
	}
	last := *st.last
	last.Diagnostics = slices.Clone(last.Diagnostics)
	return &last, st.running.Load()
}

// settingsChanged makes the next wake attempt a stamp: the owner set or
// removed the authority.
func (st *stampScheduler) settingsChanged() {
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.lastAttempt = time.Time{}
}

// knowStamped replaces the last checkpoint known stamped with checkpoint, the
// latest word on it, whether it is past what was known or not; nil, or one
// with no trail or sequence, is that none is known (line audit, finding 5).
func (st *stampScheduler) knowStamped(checkpoint *checkpointHead) {
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if checkpoint == nil || checkpoint.Identity == "" || checkpoint.Sequence < 1 {
		st.stamped = nil
		return
	}
	known := *checkpoint
	st.stamped = &known
}

// wake is one wake of the loop: where an authority is set and its interval
// has passed since the last attempt, one attempt, unless a run is in
// progress. A settings file that cannot be read stamps nothing, and is said
// in Desk's log once.
func (st *stampScheduler) wake() {
	s := st.s
	s.stampingMu.RLock()
	settings, found, err := s.readStamping()
	s.stampingMu.RUnlock()
	st.mu.Lock()
	if err != nil && err.Error() != st.said {
		st.said = err.Error()
		s.log.Printf("desk: the stamping settings of desk %s could not be read, so nothing is stamped: %v", s.signingKeyName(), err)
	} else if err == nil {
		st.said = ""
	}
	now := stampClock()
	due := err == nil && found && !st.closed &&
		(st.lastAttempt.IsZero() || !now.Before(st.lastAttempt.Add(time.Duration(settings.file.IntervalMinutes)*time.Minute)))
	st.mu.Unlock()
	if !due || !st.turn.TryLock() {
		return
	}
	defer st.turn.Unlock()
	st.mu.Lock()
	st.lastAttempt = now
	st.mu.Unlock()
	st.running.Store(true)
	defer st.running.Store(false)
	// Desk's checks first: the folder, the trail, and a runtime with the
	// audit commands; each refusal is this attempt's outcome.
	dir, failure := s.stampingRuntime(st.ctx)
	if failure != nil {
		st.record(stampRun{At: now.Unix(), Status: stampProblem, Problem: failure.message})
		return
	}
	st.attempt(dir, settings, false)
}

// attempt is one attempt in dir, after Desk's checks, in the turn the caller
// holds, with the settings the caller read: the head for a scheduled attempt,
// and the stamp, recorded as the last run. A scheduled attempt stamps only
// where the head is not the last checkpoint known stamped: of another trail,
// at another sequence, below it as well as past it, or at its sequence with
// another record digest, or with none known. One the owner asked for always
// asks the runtime, whose answer for a checkpoint stamped already costs
// nothing.
//
// **The settings stamped with are the settings in force** (review round 1).
// Immediately before the stamp, the settings are read again under
// stampingMu, held for reading until the runtime has answered, and must be
// the very bytes read first: a removal or a change confirmed meanwhile is
// never followed by a stamp to the authority it replaced, and a removal or a
// change confirmed after that waits for the stamp, at most runRuntime's
// bound, and is answered only once it is over.
func (st *stampScheduler) attempt(dir heldDir, settings stampingSettings, requested bool) stampRun {
	s := st.s
	st.running.Store(true)
	defer st.running.Store(false)
	at := stampClock().Unix()
	problem := func(words string) stampRun {
		return st.record(stampRun{At: at, Requested: requested, Status: stampProblem, Problem: words})
	}
	if !requested {
		head, said, err := s.readCheckpointHead(st.ctx, dir, deskTrail)
		switch {
		case err != nil:
			return problem(fmt.Sprintf(stampingHeadWords, strings.TrimRight(err.Error(), ".")))
		case said != nil:
			return st.record(stampRun{At: at, Status: stampRefused, Diagnostics: said})
		case head == nil:
			// No chained record: nothing to stamp, and nothing said.
			return stampRun{}
		}
		st.mu.Lock()
		known := st.stamped
		st.mu.Unlock()
		if known != nil && known.Identity == head.Identity && head.Sequence == known.Sequence && known.Digest != "" && known.Digest == head.Digest {
			// The head is the last checkpoint known stamped, by its record:
			// a checkpoint known without its digest is never taken for it.
			return stampRun{}
		}
	}
	if testHookStampChecked != nil {
		testHookStampChecked()
	}
	s.stampingMu.RLock()
	defer s.stampingMu.RUnlock()
	now, found, err := s.readStamping()
	if err != nil || !found || !bytes.Equal(now.raw, settings.raw) {
		return problem(stampingMovedWords)
	}
	if testHookStampRun != nil {
		testHookStampRun()
	}
	// **Never killed mid-write**: not by the server's stop, nor by a request
	// that goes away. The runtime's own --timeout ends its wait inside
	// runRuntime's bound.
	out, runErr := runRuntime(context.WithoutCancel(st.ctx), s.cfg.JpackBin, dir,
		"audit", "stamp", "--config", runtimeConfigName, "--tsa", now.file.Authority, "--timeout", stampTimeout, "--format", "json")
	run := readStamped(out, runErr)
	run.At, run.Requested = at, requested
	if run.Status == stampStamped || run.Status == stampAlready {
		st.knowStamped(&checkpointHead{Identity: run.Trail, Sequence: run.Sequence, Digest: run.Digest})
	}
	return st.record(run)
}

// record keeps run as the last, and says it in Desk's log, never with the
// authority's address, which may name more than the owner meant to show.
func (st *stampScheduler) record(run stampRun) stampRun {
	s := st.s
	st.mu.Lock()
	kept := run
	st.last = &kept
	st.mu.Unlock()
	by := "the scheduler"
	if run.Requested {
		by = "the owner's request"
	}
	switch run.Status {
	case stampStamped:
		s.log.Printf("desk: a stamp run of desk %s, on %s: the authority stamped the checkpoint at record %d, existing by %s as it states", s.signingKeyName(), by, run.Sequence, run.ExistedBy)
	case stampAlready:
		s.log.Printf("desk: a stamp run of desk %s, on %s: the checkpoint at record %d was stamped already, and nothing was asked", s.signingKeyName(), by, run.Sequence)
	case stampRefused:
		s.log.Printf("desk: a stamp run of desk %s, on %s, was refused: %s", s.signingKeyName(), by, mustJSON(run.Diagnostics))
	default:
		s.log.Printf("desk: a stamp run of desk %s, on %s, did not stamp: %s", s.signingKeyName(), by, run.Problem)
	}
	return run
}

// stampWire is `audit stamp --format json` as it arrives.
type stampWire struct {
	OutputVersion string              `json:"outputVersion"`
	Command       string              `json:"command"`
	Status        string              `json:"status"`
	Checkpoint    json.RawMessage     `json:"checkpoint"`
	StampedAt     *string             `json:"stampedAt"`
	ExistedBy     *string             `json:"existedBy"`
	Policy        *string             `json:"policy"`
	Diagnostics   []runtimeDiagnostic `json:"diagnostics"`
}

// readStamped reads what `audit stamp` printed, whatever the exit, under
// outputVersion "2" (runtime 0.27.1, measured).
//
//   - Exit 0, "audit stamp", "stamped", a checkpoint of the runtime's shape,
//     and the authority's time, the time the checkpoint existed by and the
//     policy: a stamp.
//   - Exit 0, "audit stamp", "already-stamped", a checkpoint, and none of
//     those three: a checkpoint stamped already, for which nothing was
//     asked.
//   - Any other non-zero exit with "error" or "unsupported" and the
//     runtime's own diagnostics: its refusal, in its words
//     (JPS-AUDIT-STAMP-NO-AUTHORITY exit 3, -UNREACHABLE exit 4, -REFUSED
//     exit 1, among others).
//
// A run that did not finish within runRuntime's bound, or could not start,
// is said in Desk's words; anything else is not an answer the runtime
// documents.
func readStamped(out []byte, runErr error) stampRun {
	code := 0
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return stampRun{Status: stampProblem, Problem: fmt.Sprintf(stampingNotRunWords, strings.TrimRight(runErr.Error(), "."))}
		}
		code = exit.ExitCode()
	}
	undocumented := stampRun{Status: stampProblem, Problem: stampingUndocumented}
	var got stampWire
	if out == nil || json.Unmarshal(out, &got) != nil || got.OutputVersion != "2" {
		return undocumented
	}
	switch {
	case code == 0 && got.Command == auditStampCommand && (got.Status == stampStamped || got.Status == stampAlready):
		checkpoint, ok := readCheckpointLine(got.Checkpoint)
		stamped := got.StampedAt != nil && got.ExistedBy != nil && got.Policy != nil
		none := got.StampedAt == nil && got.ExistedBy == nil && got.Policy == nil
		if !ok || got.Status == stampStamped && !stamped || got.Status == stampAlready && !none {
			return undocumented
		}
		run := stampRun{Status: got.Status, Trail: checkpoint.trail, Sequence: checkpoint.sequence, Digest: checkpoint.digest}
		if stamped {
			for _, at := range []string{*got.StampedAt, *got.ExistedBy} {
				if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
					return undocumented
				}
			}
			if !checkPolicy(*got.Policy) {
				return undocumented
			}
			run.StampedAt, run.ExistedBy, run.Policy = *got.StampedAt, *got.ExistedBy, *got.Policy
		}
		return run
	case code > 0 && (got.Status == "error" || got.Status == "unsupported") && (auditVerification{Diagnostics: got.Diagnostics}).said():
		return stampRun{Status: stampRefused, Diagnostics: got.Diagnostics}
	}
	return undocumented
}

/* What the decision record passes, and says ---------------------------------- */

// stampingInputs is what the decision record passes `audit verify` of this
// desk's stamping settings, and what it says of them. done releases the
// settings, which are held from the reading until the runtime has read them.
type stampingInputs struct {
	args []string
	view auditStamping
	done func()
}

// stampingForVerify reads this desk's settings, held to their record, and
// gives `--tsa-roots`, and `--tsa-policy` and `--tsa-crls` where set, each
// file by the path the runtime opens, after a check that the path names the
// file read. Settings that cannot be read pass nothing, and say why; so does
// a file whose path names another. It holds the settings for reading until
// done.
func (s *Server) stampingForVerify() stampingInputs {
	s.stampingMu.RLock()
	inputs := stampingInputs{done: s.stampingMu.RUnlock}
	if !s.assistant.usable() {
		inputs.view = auditStamping{State: stampingStateUnavailable, Problem: fmt.Sprintf(stampingCustodyWords, strings.TrimRight(s.custodyWords(s.assistant.problem.Error()), "."))}
		return inputs
	}
	folder, err := s.openStamping(false)
	if errors.Is(err, fs.ErrNotExist) {
		inputs.view = auditStamping{State: stampingStateNone}
		return inputs
	}
	if err != nil {
		inputs.view = auditStamping{State: stampingStateUnread, Problem: fmt.Sprintf(stampingUnreadWords, strings.TrimRight(s.custodyWords(err.Error()), "."))}
		return inputs
	}
	defer folder.Close()
	settings, found, err := readStampingIn(folder)
	switch {
	case err != nil:
		// **Unread, never taken for none**: nothing of it is passed. A
		// settings file that can be read at all can still be removed, by a
		// token over its bytes as they are.
		inputs.view = auditStamping{State: stampingStateUnread, Problem: fmt.Sprintf(stampingUnreadWords, strings.TrimRight(s.custodyWords(err.Error()), "."))}
		if raw, err := readPrivateData(folder, stampingSettingsName, stampingSettingsLimit); err == nil {
			inputs.view.RemoveToken = s.stampingToken(stampingRemovePurpose, newStampingNonce(), "", currentDigest(raw, true))
		}
		return inputs
	case !found:
		inputs.view = auditStamping{State: stampingStateNone}
		return inputs
	}
	if testHookStampingRead != nil {
		testHookStampingRead()
	}
	shown := showStamping(settings.plan(), settings.file.SetAt)
	inputs.view = auditStamping{State: stampingStateSet, Settings: &shown,
		RemoveToken: s.stampingToken(stampingRemovePurpose, newStampingNonce(), "", currentDigest(settings.raw, true))}
	names := []string{rootsFileName(settings.file.Roots)}
	for _, digest := range settings.file.CRLs {
		names = append(names, crlFileName(digest))
	}
	for _, name := range names {
		if err := stampingNames(settings.found[name], s.stampingPath(name)); err != nil {
			s.log.Printf("desk: the stamping roots of desk %s were not passed to audit verify: %v", s.signingKeyName(), err)
			inputs.view.PassProblem = fmt.Sprintf(stampingHeldWords, "the path of a file Desk keeps does not name the file it read")
			return inputs
		}
	}
	inputs.args = []string{"--tsa-roots", s.stampingPath(names[0])}
	for _, policy := range settings.file.Policies {
		inputs.args = append(inputs.args, "--tsa-policy", policy)
	}
	for _, name := range names[1:] {
		inputs.args = append(inputs.args, "--tsa-crls", s.stampingPath(name))
	}
	inputs.view.Passed = true
	return inputs
}

// stampingNames is whether path, not followed, names read, the file Desk
// read through its folder: what the runtime is given to open.
func stampingNames(read os.FileInfo, path string) error {
	named, err := os.Lstat(path)
	if read == nil || err != nil || named.Mode()&fs.ModeSymlink != 0 || !os.SameFile(named, read) {
		return errNotNamed
	}
	return nil
}

// stampingAfterVerify completes what the decision record says of stamping
// once the runtime has answered: the records pending a stamp, where it
// checked the stamps, or the lines where it does not say how many records;
// what the scheduler last did; and, from a check with roots, the last
// checkpoint known stamped, in place of the one known before, or none where
// no stamp covers a record.
func (s *Server) stampingAfterVerify(view auditStamping, report *auditReport) *auditStamping {
	if report != nil && view.Passed {
		switch stamped := report.Coverage.Stamped; stamped.Status {
		case "through":
			view.Pending, view.PendingLines = countedAs(chainedAfter(report, stamped.Through))
			// The record digest is known only where the checkpoint stamped
			// through is the report's head; elsewhere none is kept, and the
			// next wake asks the runtime (second review of #294).
			known := checkpointHead{Identity: report.Trail, Sequence: stamped.Through}
			if report.head.trail == report.Trail && report.head.sequence == stamped.Through {
				known.Digest = report.head.digest
			}
			s.stamping.knowStamped(&known)
		case "none":
			view.Pending, view.PendingLines = countedAs(chainedAfter(report, 0))
			s.stamping.knowStamped(nil)
		}
	}
	view.Last, view.Running = s.stamping.report()
	return &view
}

// countedAs is a count as the page is given it: records, or lines.
func countedAs(count int64, records bool) (*int64, *int64) {
	if records {
		return &count, nil
	}
	return nil, &count
}

/* The routes ------------------------------------------------------------------ */

// stampingRuntime is the folder the stamping's commands run in, or why they
// do not run: the decision record's own refusal; a project that keeps no
// trail; a runtime with no audit commands; and a Desk that keeps no
// settings.
func (s *Server) stampingRuntime(ctx context.Context) (heldDir, *lockFailure) {
	dir, refusal := s.auditRuntime()
	if refusal != "" {
		return heldDir{}, &lockFailure{http.StatusConflict, CodeBadRequest, refusal}
	}
	if !s.assistant.usable() {
		return heldDir{}, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(stampingCustodyWords, strings.TrimRight(s.custodyWords(s.assistant.problem.Error()), "."))}
	}
	if _, declared, err := s.projectAuditDir(); err != nil {
		return heldDir{}, &lockFailure{http.StatusInternalServerError, CodeInternal, fmt.Sprintf(stampingNotRunWords, strings.TrimRight(err.Error(), "."))}
	} else if !declared {
		return heldDir{}, &lockFailure{http.StatusConflict, CodeBadRequest, noTrailWords}
	}
	schema, err := readRuntimeSchema(ctx, s.cfg.JpackBin, dir)
	if err != nil {
		return heldDir{}, &lockFailure{http.StatusInternalServerError, CodeInternal, fmt.Sprintf(stampingNotRunWords, strings.TrimRight(err.Error(), "."))}
	}
	if !slices.Contains(schema.supported, auditConfigVersion) {
		return heldDir{}, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(stampingOlderWords, schema.version, auditRuntimeFloor)}
	}
	return dir, nil
}

// stampingRequest reads a request's JSON body into into, under the rules a
// rotation's confirmation has: from the same site, as JSON, bounded, and of
// exactly the members asked for.
func stampingRequest(w http.ResponseWriter, r *http.Request, limit int, into any, refused string) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden, "A cross-site request cannot change this desk's stamping.")
		return false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeJSONCoded(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Send it as JSON.")
		return false
	}
	data, err := readBounded(r.Body, limit)
	if err != nil || decodeDataJSON(data, into) != nil {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, refused)
		return false
	}
	return true
}

// refuseStamping writes failure, its message passed through withoutPaths:
// the one way a message reaches the page from these routes. The log keeps it
// whole.
func (s *Server) refuseStamping(w http.ResponseWriter, failure *lockFailure) {
	message := s.withoutPaths(failure.message)
	if message != failure.message || failure.status == http.StatusInternalServerError {
		s.log.Printf("desk: the stamping of desk %s, as said: %s", s.signingKeyName(), failure.message)
	}
	writeJSONCoded(w, failure.status, failure.code, message)
}

// stampingChecked is what a check answers: what Desk would keep, and the
// token that confirms it.
type stampingChecked struct {
	Token string        `json:"token"`
	Shown stampingShown `json:"shown"`
}

// handleStampingCheck answers `POST /api/audit/stamping/check`: the owner's
// proposal held to every rule Desk keeps settings by, and, where it holds,
// what Desk would keep and a token over it and over the settings as they
// are now. It changes nothing.
func (s *Server) handleStampingCheck(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var proposal stampingProposal
	if !stampingRequest(w, r, stampingRequestLimit, &proposal, stampingRequestWords) {
		return
	}
	plan, refused := proposal.plan()
	if refused != "" {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, refused)
		return
	}
	if _, failure := s.stampingRuntime(r.Context()); failure != nil {
		s.refuseStamping(w, failure)
		return
	}
	s.stampingMu.RLock()
	raw, found, err := s.readStampingBytes()
	s.stampingMu.RUnlock()
	if err != nil {
		s.refuseStamping(w, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(stampingUnreadWords, strings.TrimRight(s.custodyWords(err.Error()), "."))})
		return
	}
	writeJSON(w, http.StatusOK, stampingChecked{Token: s.stampingToken(stampingSetPurpose, newStampingNonce(), plan.digest(), currentDigest(raw, found)), Shown: showStamping(plan, 0)})
}

// handleStampingSet answers `POST /api/audit/stamping`: keep the proposal
// the token confirms, or change nothing. The token must be the one a check
// gave for the same proposal, against the settings as they are now, and is
// spent before anything is written, whatever follows.
func (s *Server) handleStampingSet(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		stampingProposal
		Token string `json:"token"`
	}
	if !stampingRequest(w, r, stampingRequestLimit, &request, stampingRequestWords) {
		return
	}
	if len(request.Token) != stampingTokenLength {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, stampingConfirmWords)
		return
	}
	plan, refused := request.plan()
	if refused != "" {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, refused)
		return
	}
	if _, failure := s.stampingRuntime(r.Context()); failure != nil {
		s.refuseStamping(w, failure)
		return
	}
	shown, failure := func() (*stampingShown, *lockFailure) {
		s.stampingMu.Lock()
		defer s.stampingMu.Unlock()
		return s.setStamping(plan, request.Token)
	}()
	if failure != nil {
		s.refuseStamping(w, failure)
		return
	}
	s.stamping.settingsChanged()
	s.log.Printf("desk: a time-stamping authority was set for desk %s on the owner's confirmation", s.signingKeyName())
	writeJSON(w, http.StatusOK, auditStamping{State: stampingStateSet, Settings: shown})
}

// spendStampingNonce is whether token's nonce was not spent, and spends it.
// The caller holds stampingMu for writing.
func (s *Server) spendStampingNonce(token string) bool {
	nonce := token[:stampingNonceLength]
	if s.stampingNonces[nonce] {
		return false
	}
	if s.stampingNonces == nil {
		s.stampingNonces = map[string]bool{}
	}
	s.stampingNonces[nonce] = true
	return true
}

// setStamping keeps plan where token confirms it against the settings as
// they are now: the files it names first, then the settings file, each
// written whole and read back; then the whole read back and held to what was
// written; then the files no longer named removed. The caller holds
// stampingMu for writing.
func (s *Server) setStamping(plan stampingPlan, token string) (*stampingShown, *lockFailure) {
	nonce := token[:stampingNonceLength]
	if s.stampingNonces[nonce] {
		return nil, &lockFailure{http.StatusConflict, CodeStale, stampingUsedWords}
	}
	raw, found, err := s.readStampingBytes()
	if err != nil {
		return nil, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(stampingUnreadWords, strings.TrimRight(s.custodyWords(err.Error()), "."))}
	}
	if !hmac.Equal([]byte(s.stampingToken(stampingSetPurpose, nonce, plan.digest(), currentDigest(raw, found))), []byte(token)) {
		return nil, &lockFailure{http.StatusConflict, CodeStale, stampingStaleWords}
	}
	s.spendStampingNonce(token)
	keepFailed := func(err error) (*stampingShown, *lockFailure) {
		return nil, &lockFailure{http.StatusInternalServerError, CodeInternal, fmt.Sprintf(stampingKeepWords, strings.TrimRight(s.custodyWords(err.Error()), "."))}
	}
	file := plan.file(stampClock().Unix())
	data, err := json.Marshal(file)
	if err != nil {
		return keepFailed(err)
	}
	// **The writer holds the settings to the reader's bound**, newline and
	// all: settings written past it could not be read again.
	if len(data)+1 > stampingSettingsLimit {
		return keepFailed(fmt.Errorf("these settings would be larger than the %d bytes Desk reads of them", stampingSettingsLimit))
	}
	folder, err := s.openStamping(true)
	if err != nil {
		return keepFailed(err)
	}
	defer folder.Close()
	if err := writePrivateData(folder, rootsFileName(file.Roots), plan.roots); err != nil {
		return keepFailed(err)
	}
	for i, crl := range plan.crls {
		if err := writePrivateData(folder, crlFileName(file.CRLs[i]), crl); err != nil {
			return keepFailed(err)
		}
	}
	if err := writePrivateData(folder, stampingSettingsName, append(data, '\n')); err != nil {
		return keepFailed(err)
	}
	// **Held to what was written**: the whole read back as any use reads it.
	read, found, err := readStampingIn(folder)
	if err == nil && (!found || !bytes.Equal(read.raw, append(data, '\n')) || !bytes.Equal(read.roots, plan.roots) || len(read.crls) != len(plan.crls)) {
		err = errors.New("the settings read back are not the ones written")
	}
	if err != nil {
		return keepFailed(err)
	}
	s.removeUnnamed(folder, &file)
	shown := showStamping(plan, file.SetAt)
	return &shown, nil
}

// removeUnnamed removes the roots and lists in folder that file does not
// name: only files of the names Desk gives them, each through the folder
// held. A file that cannot be removed now is said in Desk's log, and left:
// no settings name it.
func (s *Server) removeUnnamed(folder *os.Root, file *stampingFile) {
	kept := map[string]bool{}
	if file != nil {
		kept[rootsFileName(file.Roots)] = true
		for _, digest := range file.CRLs {
			kept[crlFileName(digest)] = true
		}
	}
	dir, err := folder.Open(".")
	if err != nil {
		s.log.Printf("desk: the stamping folder of desk %s could not be listed to remove what no settings name: %v", s.signingKeyName(), err)
		return
	}
	entries, err := dir.ReadDir(64)
	dir.Close()
	if err != nil && len(entries) == 0 {
		s.log.Printf("desk: the stamping folder of desk %s could not be listed to remove what no settings name: %v", s.signingKeyName(), err)
		return
	}
	for _, entry := range entries {
		if name := entry.Name(); stampingFileName.MatchString(name) && !kept[name] && entry.Type().IsRegular() {
			if err := folder.Remove(name); err != nil {
				s.log.Printf("desk: %s, which no stamping settings of desk %s name, could not be removed: %v", name, s.signingKeyName(), err)
			}
		}
	}
	if err := syncPrivateDirectory(folder); err != nil {
		s.log.Printf("desk: the stamping folder of desk %s could not be synced: %v", s.signingKeyName(), err)
	}
}

// handleStampingRemove answers `POST /api/audit/stamping/remove`: remove
// this desk's authority, as the decision record the token names showed its
// settings, or change nothing. The trail's stamps file is left as it is.
func (s *Server) handleStampingRemove(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		Token string `json:"token"`
	}
	if !stampingRequest(w, r, stampingSmallRequestLimit, &request, stampingRemoveWords) {
		return
	}
	if len(request.Token) != stampingTokenLength {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, stampingRemoveWords)
		return
	}
	if _, refusal := s.auditRuntime(); refusal != "" {
		s.refuseStamping(w, &lockFailure{http.StatusConflict, CodeBadRequest, refusal})
		return
	}
	failure := func() *lockFailure {
		s.stampingMu.Lock()
		defer s.stampingMu.Unlock()
		return s.removeStamping(request.Token)
	}()
	if failure != nil {
		s.refuseStamping(w, failure)
		return
	}
	s.stamping.settingsChanged()
	s.log.Printf("desk: the time-stamping authority of desk %s was removed on the owner's confirmation; its stamps are kept", s.signingKeyName())
	writeJSON(w, http.StatusOK, auditStamping{State: stampingStateNone})
}

// removeStamping removes the settings token confirms, as they are now: the
// settings file first, so that no settings are in force from then on, then
// the files it named. The caller holds stampingMu for writing.
func (s *Server) removeStamping(token string) *lockFailure {
	nonce := token[:stampingNonceLength]
	if s.stampingNonces[nonce] {
		return &lockFailure{http.StatusConflict, CodeStale, stampingUsedWords}
	}
	folder, err := s.openStamping(false)
	if errors.Is(err, fs.ErrNotExist) {
		return &lockFailure{http.StatusConflict, CodeStale, stampingStaleWords}
	}
	if err != nil {
		return &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(stampingUnreadWords, strings.TrimRight(s.custodyWords(err.Error()), "."))}
	}
	defer folder.Close()
	raw, err := readPrivateData(folder, stampingSettingsName, stampingSettingsLimit)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &lockFailure{http.StatusConflict, CodeStale, stampingStaleWords}
	case err != nil:
		return &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(stampingUnreadWords, strings.TrimRight(s.custodyWords(err.Error()), "."))}
	case !hmac.Equal([]byte(s.stampingToken(stampingRemovePurpose, nonce, "", currentDigest(raw, true))), []byte(token)):
		return &lockFailure{http.StatusConflict, CodeStale, stampingStaleWords}
	}
	s.spendStampingNonce(token)
	if err := folder.Remove(stampingSettingsName); err != nil {
		return &lockFailure{http.StatusInternalServerError, CodeInternal, fmt.Sprintf(stampingKeepWords, strings.TrimRight(s.custodyWords(err.Error()), "."))}
	}
	s.removeUnnamed(folder, nil)
	return nil
}

// handleStampNow answers `POST /api/audit/stamping/stamp`: one stamp run on
// the owner's request, in the desk's one turn, with the authority set; or
// why none ran. The page then checks the decision record again.
func (s *Server) handleStampNow(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct{}
	if !stampingRequest(w, r, stampingSmallRequestLimit, &request, stampingStartWords) {
		return
	}
	dir, failure := s.stampingRuntime(r.Context())
	if failure != nil {
		s.refuseStamping(w, failure)
		return
	}
	s.stampingMu.RLock()
	settings, found, err := s.readStamping()
	s.stampingMu.RUnlock()
	switch {
	case err != nil:
		s.refuseStamping(w, &lockFailure{http.StatusConflict, CodeBadRequest, fmt.Sprintf(stampingUnreadWords, strings.TrimRight(s.custodyWords(err.Error()), "."))})
		return
	case !found:
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, stampingNoneWords)
		return
	}
	st := s.stamping
	if !st.turn.TryLock() {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, stampingBusyWords)
		return
	}
	// **Closed is read in the turn**: Close marks the scheduler closed before
	// it waits for the turn, so a run that takes the turn after it starts
	// nothing.
	run, closed := func() (stampRun, bool) {
		defer st.turn.Unlock()
		st.mu.Lock()
		closed := st.closed
		if !closed {
			st.lastAttempt = stampClock()
		}
		st.mu.Unlock()
		if closed {
			return stampRun{}, true
		}
		return st.attempt(dir, settings, true), false
	}()
	if closed {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, stampingClosedWords)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Run stampRun `json:"run"`
	}{s.withoutPathsInRun(run)})
}

// withoutPathsInRun is run with every sentence in it passed through
// withoutPaths: Desk's, and the runtime's.
func (s *Server) withoutPathsInRun(run stampRun) stampRun {
	run.Problem = s.withoutPaths(run.Problem)
	if run.Diagnostics != nil {
		said := make([]runtimeDiagnostic, len(run.Diagnostics))
		for i, diagnostic := range run.Diagnostics {
			said[i] = runtimeDiagnostic{Code: diagnostic.Code, Message: s.withoutPaths(diagnostic.Message)}
		}
		run.Diagnostics = said
	}
	return run
}
