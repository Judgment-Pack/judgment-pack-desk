package desk

// HTTP carries detached logical comparisons only. Existing host methods retain
// original entry/use/source exclusion, native qualification and all five writes.
import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const storagePolicyHTTPBytes = 16 * 1024

type storagePolicyHTTPLimits struct {
	Total      storageStatusVector   `json:"total"`
	Categories []storageStatusVector `json:"categories"`
}
type storagePolicyHTTPWorkspace struct {
	ID         string                  `json:"id"`
	Generation string                  `json:"generation"`
	Limits     storagePolicyHTTPLimits `json:"limits"`
}
type storagePolicyHTTPSnapshot struct {
	Version                int                        `json:"version"`
	Installation           string                     `json:"installation"`
	Sequence               string                     `json:"sequence"`
	Revision               string                     `json:"revision"`
	Epoch                  string                     `json:"epoch"`
	Workspace              storagePolicyHTTPWorkspace `json:"workspace"`
	InstallationLimits     storagePolicyHTTPLimits    `json:"installationLimits"`
	DefaultLimits          storagePolicyHTTPLimits    `json:"defaultLimits"`
	ObservationMaxAgeNanos string                     `json:"observationMaxAgeNanos"`
}
type storagePolicyHTTPRequest struct {
	Expected *storagePolicyHTTPSnapshot `json:"expected"`
	Scope    string                     `json:"scope"`
	Limits   *storagePolicyHTTPLimits   `json:"limits"`
}

func storagePolicyHTTPUint(s string, positive bool) (uint64, error) {
	if len(s) == 0 || len(s) > 20 {
		return 0, storagebudget.ErrBounds
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || strconv.FormatUint(n, 10) != s || positive && n == 0 {
		return 0, storagebudget.ErrBounds
	}
	return n, nil
}
func storagePolicyHTTPID(s string) (out storagebudget.ID, err error) {
	if len(s) != 32 {
		return out, storagebudget.ErrBounds
	}
	raw, e := hex.DecodeString(s)
	if e != nil || hex.EncodeToString(raw) != s {
		return out, storagebudget.ErrBounds
	}
	copy(out[:], raw)
	if out == (storagebudget.ID{}) {
		return out, storagebudget.ErrBounds
	}
	return out, nil
}
func storagePolicyHTTPLimitsValue(v storagePolicyHTTPLimits) (out storagebudget.Limits, err error) {
	if len(v.Categories) != storagebudget.Categories {
		return out, storagebudget.ErrBounds
	}
	read := func(v storageStatusVector) (storagebudget.Vector, error) {
		a, e := storagePolicyHTTPUint(v.Bytes, true)
		if e != nil {
			return storagebudget.Vector{}, e
		}
		b, e := storagePolicyHTTPUint(v.Files, true)
		if e != nil {
			return storagebudget.Vector{}, e
		}
		c, e := storagePolicyHTTPUint(v.Directories, true)
		if e != nil {
			return storagebudget.Vector{}, e
		}
		return storagebudget.Vector{Bytes: a, Files: b, Directories: c}, nil
	}
	out.Total, err = read(v.Total)
	if err != nil {
		return out, err
	}
	for i, c := range v.Categories {
		out.Category[i], err = read(c)
		if err != nil {
			return out, err
		}
	}
	if !storageOwnerLimitsValid(out) {
		return out, storagebudget.ErrBounds
	}
	return out, nil
}
func storagePolicyHTTPFormatLimits(v storagebudget.Limits) storagePolicyHTTPLimits {
	vector := func(v storagebudget.Vector) storageStatusVector {
		return storageStatusVector{strconv.FormatUint(v.Bytes, 10), strconv.FormatUint(v.Files, 10), strconv.FormatUint(v.Directories, 10)}
	}
	out := storagePolicyHTTPLimits{Total: vector(v.Total), Categories: make([]storageStatusVector, storagebudget.Categories)}
	for i, c := range v.Category {
		out.Categories[i] = vector(c)
	}
	return out
}
func storagePolicyHTTPFormat(v storagePolicySnapshot) storagePolicyHTTPSnapshot {
	return storagePolicyHTTPSnapshot{Version: 1, Installation: hex.EncodeToString(v.Installation[:]), Sequence: strconv.FormatUint(v.Sequence, 10), Revision: strconv.FormatUint(v.Revision, 10), Epoch: strconv.FormatUint(v.Epoch, 10), Workspace: storagePolicyHTTPWorkspace{ID: hex.EncodeToString(v.Workspace.ID[:]), Generation: strconv.FormatUint(v.Workspace.Generation, 10), Limits: storagePolicyHTTPFormatLimits(v.Workspace.Limits)}, InstallationLimits: storagePolicyHTTPFormatLimits(v.Policy.Installation), DefaultLimits: storagePolicyHTTPFormatLimits(v.Policy.Workspace), ObservationMaxAgeNanos: strconv.FormatInt(int64(v.Policy.ObservationMaxAge), 10)}
}
func (v storagePolicyHTTPSnapshot) value() (out storagePolicySnapshot, err error) {
	if v.Version != 1 {
		return out, storagebudget.ErrBounds
	}
	out.Installation, err = storagePolicyHTTPID(v.Installation)
	if err != nil {
		return out, err
	}
	out.Workspace.ID, err = storagePolicyHTTPID(v.Workspace.ID)
	if err != nil {
		return out, err
	}
	for _, p := range []struct {
		s string
		n *uint64
	}{{v.Sequence, &out.Sequence}, {v.Revision, &out.Revision}, {v.Epoch, &out.Epoch}, {v.Workspace.Generation, &out.Workspace.Generation}} {
		*p.n, err = storagePolicyHTTPUint(p.s, true)
		if err != nil {
			return out, err
		}
	}
	out.Workspace.Limits, err = storagePolicyHTTPLimitsValue(v.Workspace.Limits)
	if err != nil {
		return out, err
	}
	out.Policy.Installation, err = storagePolicyHTTPLimitsValue(v.InstallationLimits)
	if err != nil {
		return out, err
	}
	out.Policy.Workspace, err = storagePolicyHTTPLimitsValue(v.DefaultLimits)
	if err != nil {
		return out, err
	}
	age, err := storagePolicyHTTPUint(v.ObservationMaxAgeNanos, true)
	if err != nil || age > math.MaxInt64 {
		return out, storagebudget.ErrBounds
	}
	out.Policy.ObservationMaxAge = time.Duration(age)
	return out, nil
}
func storagePolicyHTTPDecode(raw []byte, into any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > storagePolicyHTTPBytes || len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' || checkJSONMembers(raw, reflect.TypeOf(into)) != nil {
		return storagebudget.ErrBounds
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(into) != nil {
		return storagebudget.ErrBounds
	}
	return nil
}
func storagePolicyHTTPRead(w http.ResponseWriter, r *http.Request, into any) bool {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || len(params) == 1 && params["charset"] != "utf-8" || r.Body == nil || r.ContentLength > storagePolicyHTTPBytes || r.Header.Get("Content-Encoding") != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "Storage policy requires a bounded JSON object.")
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, storagePolicyHTTPBytes+1))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || storagePolicyHTTPDecode(raw, into) != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid storage policy request.")
		return false
	}
	return true
}

// Joined cleanup/persistence errors never become a clean conflict merely
// because errors.Is also finds a stale/busy comparison somewhere inside.
func storagePolicyHTTPConflict(err error) bool {
	if err == storagebudget.ErrStale || err == storagebudget.ErrBusy || err == storagebudget.ErrAuthority {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, e := range children {
			if !storagePolicyHTTPConflict(e) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return storagePolicyHTTPConflict(wrapped.Unwrap())
	}
	return false
}
func storagePolicyHTTPError(w http.ResponseWriter, err error, mutation bool, noEffect storagePolicyNoEffect) {
	if mutation && bool(noEffect) && storagePolicyHTTPConflict(err) {
		writeJSONCoded(w, 409, "storage-policy-conflict", "Storage policy comparison is no longer usable. Review current settings before another edit.")
		return
	}
	if mutation {
		writeJSONCoded(w, 503, storageStatusUncertain, "Storage policy outcome is not confirmed. Keep the draft and review storage recovery before another edit.")
		return
	}
	writeJSONCoded(w, 503, storageStatusUnknown, "Storage policy is unavailable. Preparation or original storage recovery may be required.")
}
func (s *Server) handleStoragePolicy(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	prepare := r.URL.Path == "/api/storage/policy/prepare"
	path := "/api/storage/policy"
	allow := "GET, POST"
	if prepare {
		path += "/prepare"
		allow = "POST"
	}
	if r.URL.Path != path || r.URL.EscapedPath() != path || r.Method != http.MethodGet && r.Method != http.MethodPost || prepare && r.Method != http.MethodPost {
		w.Header().Set("Allow", allow)
		writeJSONCoded(w, 405, CodeBadRequest, "Storage policy requires an exact supported method and path.")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		writeJSONCoded(w, 400, CodeBadRequest, "Storage policy does not accept query inputs.")
		return
	}
	if r.Method == http.MethodGet {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.Body != nil && r.Body != http.NoBody {
			writeJSONCoded(w, 400, CodeBadRequest, "Storage policy reads do not accept a body.")
			return
		}
		snapshot, err := s.readStoragePolicySnapshot(r.Context())
		if err != nil {
			storagePolicyHTTPError(w, err, false, false)
			return
		}
		writeJSON(w, 200, storagePolicyHTTPFormat(snapshot))
		return
	}
	if s.storageReadOnly {
		writeJSONCoded(w, 503, storageStatusUnknown, "Storage is read only; policy changes are unavailable.")
		return
	}
	if prepare {
		var request struct{}
		if !storagePolicyHTTPRead(w, r, &request) {
			return
		}
		if err := s.prepareStoragePolicy(r.Context()); err != nil {
			storagePolicyHTTPError(w, err, true, false)
			return
		}
		s.invalidateStorageStatusCache()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var request storagePolicyHTTPRequest
	if !storagePolicyHTTPRead(w, r, &request) {
		return
	}
	if request.Expected == nil || request.Limits == nil {
		writeJSONCoded(w, 400, CodeBadRequest, "An exact policy snapshot and limits are required.")
		return
	}
	expected, err := request.Expected.value()
	if err != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid storage policy comparison.")
		return
	}
	limits, err := storagePolicyHTTPLimitsValue(*request.Limits)
	if err != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid storage limits.")
		return
	}
	edit := storagePolicyEdit{Expected: expected}
	switch request.Scope {
	case "installation":
		edit.Installation = &limits
	case "default":
		edit.Default = &limits
	case "workspace":
		edit.Workspace = &limits
	default:
		writeJSONCoded(w, 400, CodeBadRequest, "Choose installation, default or current workspace limits.")
		return
	}
	snapshot, noEffect, err := s.updateStoragePolicyInvocation(r.Context(), edit)
	if err != nil {
		storagePolicyHTTPError(w, err, true, noEffect)
		return
	}
	s.invalidateStorageStatusCache()
	writeJSON(w, 200, storagePolicyHTTPFormat(snapshot))
}
