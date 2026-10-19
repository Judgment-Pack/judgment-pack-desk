package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

// Runner's listing of the installation's profiles (GET /v1/input-profiles)
// holds two shapes since Runner v0.23.0 (runner #156). An installed profile
// is {profile, digest}. After the installed ones, a registered profile that
// Runner set aside when it opened its store, because its current form refuses
// it (such as a scoped page profile whose endpointScope entry RFC 0018 round
// 16 refuses), is {profile, digest, state: "set-aside", reason}. Runner keeps
// it and never binds it: a binding, job or run that names it is refused with
// 422 input_profile_set_aside, naming it. Desk never takes one as a candidate
// either: it is not offered, a pin that names it is refused by its name with
// Runner's reason, and nothing binds to it.

// codeProfileSetAside is Runner's code for a binding to a set-aside profile.
const codeProfileSetAside = "input_profile_set_aside"

// listedInputProfile is one entry of Runner's listing. Profile is a set-aside
// entry's stored bytes, which Runner's current form refuses. State is the
// state member as written, kept raw so that its presence is what counts.
type listedInputProfile struct {
	Profile json.RawMessage `json:"profile"`
	Digest  string          `json:"digest"`
	State   json.RawMessage `json:"state,omitempty"`
	Reason  string          `json:"reason,omitempty"`
	id      string
}

// installed reports an installed profile: an entry with no state member. An
// entry with one, whatever its value (set-aside, the one Runner lists, but
// also an empty string or null), is never a candidate.
func (p listedInputProfile) installed() bool { return p.State == nil }

type inputProfileListing []listedInputProfile

// readInputProfileListing reads Runner's listing, each entry's profile id
// read leniently: a profile whose id cannot be read is named by no pin.
func readInputProfileListing(raw json.RawMessage) (inputProfileListing, error) {
	var listing inputProfileListing
	if json.Unmarshal(raw, &listing) != nil {
		return nil, errors.New("The installation profiles are unavailable.")
	}
	for i := range listing {
		var named struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(listing[i].Profile, &named) == nil {
			listing[i].id = named.ID
		}
	}
	return listing, nil
}

// installedProfile is the installed entry with the id a pin names that
// matches, or nil where none is listed. A pin that names a set-aside profile
// is refused by its name with Runner's reason, whatever else it names, as
// Runner refuses a binding to it.
func (l inputProfileListing) installedProfile(id string, matches func(listedInputProfile) bool) (*listedInputProfile, error) {
	for i := range l {
		if l[i].id != id {
			continue
		}
		if !l[i].installed() {
			return nil, setAsideProfileError{ID: id, Reason: l[i].Reason}
		}
		if matches(l[i]) {
			return &l[i], nil
		}
	}
	return nil, nil
}

// digestIs matches the entry a pin names by its digest.
func digestIs(digest string) func(listedInputProfile) bool {
	return func(p listedInputProfile) bool { return p.Digest == digest }
}

// setAsideProfileError is Desk's refusal of a binding to a profile Runner set
// aside. It names the profile, gives Runner's reason, labelled as Runner's,
// and says what the user can do: choose another profile for the source, and,
// where the binding is a job's (Job), upgrade the job. Nothing else of
// Runner's refusal reaches the user.
type setAsideProfileError struct {
	ID, Reason string
	Job        bool
}

func (e setAsideProfileError) Error() string {
	next := " Choose another profile for this source."
	if e.Job {
		next = " Choose another profile for this source, or upgrade the job to a release that does not name it."
	}
	message := "The input profile " + e.ID + " was set aside by Runner and cannot be bound."
	if e.ID == "" {
		message = "An input profile this binding names was set aside by Runner and cannot be bound."
	}
	if reason := runnerReason(e.ID, e.Reason); reason != "" {
		message += " Runner's reason: " + reason + "."
	}
	return message + next
}

// runnerCode is the code Runner ends a profile refusal with, such as
// " (input-malformed)".
var runnerCode = regexp.MustCompile(`\s*\([a-z][a-z0-9_-]*\)$`)

// runnerReason is Runner's reason as Desk shows it: without the leading "the
// input profile <id>: " where it names the same profile, which the message
// names already, without Runner's code at its end, and without a final stop.
func runnerReason(id, reason string) string {
	reason = strings.TrimSpace(reason)
	if id != "" {
		reason = strings.TrimPrefix(reason, "the input profile "+id+": ")
	}
	reason = runnerCode.ReplaceAllString(reason, "")
	return strings.TrimSuffix(strings.TrimSpace(reason), ".")
}

// setAsideNamed reads the profile a refusal names from Runner's message, in
// Runner v0.23.0's wording, its id in the profile id form. The reason is read
// from Runner's listing, never from the message.
var setAsideNamed = regexp.MustCompile(`^The input profile ([a-zA-Z][a-zA-Z0-9_-]{0,63}) was set aside `)

// setAsideRefusal is Desk's refusal in place of Runner's
// input_profile_set_aside with message: the profile the message names, and
// the reason Runner lists it with. A message in another wording names no
// profile, and a profile the listing does not give as set aside, no reason.
func (s *Server) setAsideRefusal(ctx context.Context, message string, job bool) setAsideProfileError {
	named := setAsideNamed.FindStringSubmatch(message)
	if named == nil {
		return setAsideProfileError{Job: job}
	}
	refusal := setAsideProfileError{ID: named[1], Job: job}
	raw, err := s.inputRunner(ctx, "input-profiles", nil, "")
	if err != nil {
		return refusal
	}
	listing, err := readInputProfileListing(raw)
	if err != nil {
		return refusal
	}
	for _, p := range listing {
		if p.id == refusal.ID && !p.installed() {
			refusal.Reason = p.Reason
		}
	}
	return refusal
}

// setAsideAnswer is the answer Desk forwards in place of Runner's 422
// input_profile_set_aside: Runner's error envelope, carrying Desk's message,
// and the profile and reason the page words its own message from. ok is false
// for any other answer, which is forwarded unchanged.
func (s *Server) setAsideAnswer(ctx context.Context, status int, body []byte) (answer []byte, ok bool) {
	if status != http.StatusUnprocessableEntity {
		return nil, false
	}
	var failure struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &failure) != nil || failure.Error.Code != codeProfileSetAside {
		return nil, false
	}
	refusal := s.setAsideRefusal(ctx, failure.Error.Message, true)
	refused := map[string]any{"code": codeProfileSetAside, "message": refusal.Error(), "retryable": false}
	if refusal.ID != "" {
		refused["profile"] = refusal.ID
	}
	if refusal.Reason != "" {
		refused["reason"] = refusal.Reason
	}
	answer, err := json.Marshal(map[string]any{"error": refused})
	return answer, err == nil
}
