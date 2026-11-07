package desk

// Retention is a bounded selection policy, never permission to delete. This
// dependency provides settings and read-only source observations. Archive,
// spent-source transitions and removal remain unavailable.
import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const storageRetentionName = "storage-retention.json"
const storageRetentionBytes = 512 << 10
const storageRetentionRequestBytes = 16 << 10
const storageRetentionWorkflow = "storage-retention-settings-v1"

// Exactly four positions, in the order below; nil, duplicate and unknown
// classes are invalid. Decimal strings preserve the complete bounded integers.
var storageRetentionClasses = [4]string{"scratch", "cache", "superseded-binaries", "history"}

type storageRetentionRule struct {
	Class      string `json:"class"`
	Mode       string `json:"mode"`
	MaxAgeDays string `json:"maxAgeDays"`
	MaxBytes   string `json:"maxBytes"`
	MaxObjects string `json:"maxObjects"`
}
type storageRetentionRules [4]storageRetentionRule
type storageRetentionWorkspace struct {
	ID         string                `json:"id"`
	Generation string                `json:"generation"`
	Rules      storageRetentionRules `json:"rules"`
}
type storageRetentionPolicy struct {
	Version           int                         `json:"version"`
	Installation      string                      `json:"installation"`
	InstallationRules storageRetentionRules       `json:"installationRules"`
	DefaultRules      storageRetentionRules       `json:"defaultRules"`
	Workspaces        []storageRetentionWorkspace `json:"workspaces"`
}
type storageRetentionComparison struct {
	Installation string `json:"installation"`
	Revision     string `json:"revision"`
	Workspace    string `json:"workspace"`
	Generation   string `json:"generation"`
}
type storageRetentionSnapshot struct {
	Expected           storageRetentionComparison `json:"expected"`
	ObservedAt         string                     `json:"observedAt"`
	InstallationRules  storageRetentionRules      `json:"installationRules"`
	DefaultRules       storageRetentionRules      `json:"defaultRules"`
	WorkspaceRules     storageRetentionRules      `json:"workspaceRules"`
	DestructiveActions bool                       `json:"destructiveActions"`
}
type storageRetentionEdit struct {
	Expected *storageRetentionComparison `json:"expected"`
	Scope    string                      `json:"scope"`
	Rules    *storageRetentionRules      `json:"rules"`
}

func storageRetentionDefaultRules() storageRetentionRules {
	return storageRetentionRules{
		{"scratch", "automatic", "1", "67108864", "256"},
		{"cache", "automatic", "7", "67108864", "256"},
		{"superseded-binaries", "automatic", "30", "268435456", "8"},
		{"history", "manual", "90", "1073741824", "128"},
	}
}
func storageRetentionRulesValid(r storageRetentionRules) bool {
	for i, v := range r {
		age, e := storagePolicyHTTPUint(v.MaxAgeDays, true)
		if e != nil || age > 36500 {
			return false
		}
		size, e := storagePolicyHTTPUint(v.MaxBytes, true)
		if e != nil || size > 1<<60 {
			return false
		}
		count, e := storagePolicyHTTPUint(v.MaxObjects, true)
		if e != nil || count > 1<<40 {
			return false
		}
		if v.Class != storageRetentionClasses[i] || i == 3 && v.Mode != "manual" || i != 3 && v.Mode != "automatic" && v.Mode != "manual" {
			return false
		}
	}
	return true
}
func (p storageRetentionPolicy) validate() error {
	if p.Version != 1 || !storageRetentionInstallationValid(p.Installation) || !storageRetentionRulesValid(p.InstallationRules) || !storageRetentionRulesValid(p.DefaultRules) || p.Workspaces == nil || len(p.Workspaces) > storagebudget.MaxWorkspaces {
		return storagebudget.ErrBounds
	}
	for i, w := range p.Workspaces {
		if _, e := storagePolicyHTTPID(w.ID); e != nil {
			return e
		}
		if _, e := storagePolicyHTTPUint(w.Generation, true); e != nil {
			return e
		}
		if !storageRetentionRulesValid(w.Rules) || i > 0 && p.Workspaces[i-1].ID >= w.ID {
			return storagebudget.ErrBounds
		}
	}
	return nil
}
func decodeStorageRetention(raw []byte) (p storageRetentionPolicy, err error) {
	if len(raw) > storageRetentionBytes || len(raw) == 0 || !uniqueJSON(raw) || checkJSONMembers(raw, reflect.TypeOf(p)) != nil {
		return p, storagebudget.ErrBounds
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&p); err != nil {
		return p, storagebudget.ErrBounds
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return p, storagebudget.ErrBounds
	}
	return p, p.validate()
}
func storageRetentionRevision(raw []byte) string {
	if raw == nil {
		return "absent"
	}
	return digestOf(raw)
}
func storageRetentionInitial(installation storagebudget.ID) storageRetentionPolicy {
	return storageRetentionPolicy{1, hex.EncodeToString(installation[:]), storageRetentionDefaultRules(), storageRetentionDefaultRules(), []storageRetentionWorkspace{}}
}
func (p storageRetentionPolicy) workspace(id string, generation string) (storageRetentionRules, error) {
	for _, w := range p.Workspaces {
		if w.ID == id {
			if w.Generation != generation {
				return storageRetentionRules{}, storagebudget.ErrStale
			}
			return w.Rules, nil
		}
	}
	return p.DefaultRules, nil
}

// Before every default edit, materialize ALL current registrations under the
// original admission exclusion. An equal-valued deliberate override is sticky;
// a workspace is never classified as inherited by comparing its numbers.
func storageRetentionApply(p storageRetentionPolicy, registered []storagebudget.RegisteredWorkspace, current storagebudget.RegisteredWorkspace, revision string, edit storageRetentionEdit) (storageRetentionPolicy, error) {
	if !storageRetentionEditValid(edit) || p.validate() != nil || edit.Expected == nil || edit.Expected.Installation != p.Installation || edit.Expected.Revision != revision || edit.Expected.Workspace != hex.EncodeToString(current.ID[:]) || edit.Expected.Generation != strconv.FormatUint(current.Generation, 10) {
		return p, storagebudget.ErrStale
	}
	if edit.Scope != "installation" && edit.Scope != "default" && edit.Scope != "workspace" && edit.Scope != "copy-default" {
		return p, storagebudget.ErrBounds
	}
	if edit.Scope == "copy-default" {
		if edit.Rules != nil {
			return p, storagebudget.ErrBounds
		}
	} else if edit.Rules == nil || !storageRetentionRulesValid(*edit.Rules) {
		return p, storagebudget.ErrBounds
	}
	next := p
	next.Workspaces = append([]storageRetentionWorkspace{}, p.Workspaces...)
	members := map[string]string{}
	for _, w := range registered {
		if w.ID == (storagebudget.ID{}) || w.Generation == 0 || len(members) >= storagebudget.MaxWorkspaces {
			return p, storagebudget.ErrBounds
		}
		id := hex.EncodeToString(w.ID[:])
		gen := strconv.FormatUint(w.Generation, 10)
		if _, ok := members[id]; ok {
			return p, storagebudget.ErrBounds
		}
		members[id] = gen
	}
	if members[edit.Expected.Workspace] != edit.Expected.Generation {
		return p, storagebudget.ErrStale
	}
	for _, w := range next.Workspaces {
		if members[w.ID] != w.Generation {
			return p, storagebudget.ErrStale
		}
	}
	for id, gen := range members {
		found := false
		for _, w := range next.Workspaces {
			if w.ID == id {
				found = true
				break
			}
		}
		if !found {
			next.Workspaces = append(next.Workspaces, storageRetentionWorkspace{id, gen, p.DefaultRules})
		}
	}
	switch edit.Scope {
	case "installation":
		next.InstallationRules = *edit.Rules
	case "default":
		next.DefaultRules = *edit.Rules
	default:
		rules := p.DefaultRules
		if edit.Scope == "workspace" {
			rules = *edit.Rules
		}
		for i := range next.Workspaces {
			if next.Workspaces[i].ID == edit.Expected.Workspace {
				next.Workspaces[i].Rules = rules
			}
		}
	}
	sort.Slice(next.Workspaces, func(i, j int) bool { return next.Workspaces[i].ID < next.Workspaces[j].ID })
	return next, next.validate()
}

// A source's Completeness concerns references only. It does not certify reader
// drain, byte reclaimability or effect authority. UnknownAny invalidates every
// negative-reference proof in the joined selection, not just its own source.
type storageRetentionReference struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision string `json:"revision"`
}
type storageRetentionSource struct {
	ID         string                      `json:"id"`
	Kind       string                      `json:"kind"`
	Revision   string                      `json:"revision"`
	Complete   bool                        `json:"complete"`
	Reasons    []string                    `json:"reasons"`
	References []storageRetentionReference `json:"references"`
}
type storageRetentionObject struct {
	ID                                    string
	Kind                                  string
	Class                                 string
	Revision                              string
	Bytes                                 uint64
	Completed                             time.Time
	Owned, Current, Private, SingleLink   bool
	Disposable, Released, Pinned, Pending bool
}
type storageRetentionCandidate struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Revision     string `json:"revision"`
	LogicalBytes string `json:"logicalBytes"`
	State        string `json:"state"`
	Reason       string `json:"reason"`
}
type storageRetentionPreview struct {
	Version                 int                         `json:"version"`
	ObservedAt              string                      `json:"observedAt"`
	Revision                string                      `json:"revision"`
	ExportRevision          string                      `json:"exportRevision"`
	Complete                bool                        `json:"complete"`
	Sources                 []storageRetentionSource    `json:"sources"`
	Objects                 []storageRetentionCandidate `json:"objects"`
	ManualCandidateBytes    *string                     `json:"manualCandidateBytes"`
	AutomaticCandidateBytes *string                     `json:"automaticCandidateBytes"`
	AllocatedEstimate       *string                     `json:"allocatedEstimate"`
	DestructiveActions      bool                        `json:"destructiveActions"`
	ExportComplete          bool                        `json:"exportComplete"`
}

func storageRetentionSelect(now time.Time, rules storageRetentionRules, sources []storageRetentionSource, objects []storageRetentionObject) (out storageRetentionPreview, err error) {
	if now.IsZero() || !storageRetentionRulesValid(rules) || len(sources) > maxStorageFiles || len(objects) > maxStorageFiles {
		return out, storagebudget.ErrBounds
	}
	detached := make([]storageRetentionSource, len(sources))
	for i, source := range sources {
		detached[i] = source
		detached[i].Reasons = append([]string{}, source.Reasons...)
		detached[i].References = append([]storageRetentionReference{}, source.References...)
	}
	out = storageRetentionPreview{Version: 1, ObservedAt: now.UTC().Format(time.RFC3339Nano), Complete: true, Sources: detached, Objects: []storageRetentionCandidate{}}
	refs := map[string]bool{}
	seen := map[string]bool{}
	for _, s := range sources {
		if s.ID == "" || !assistantRunDigest(s.Revision) || seen[s.ID] || len(s.References) > storagebudget.MaxEntries {
			return out, storagebudget.ErrBounds
		}
		seen[s.ID] = true
		if !s.Complete {
			out.Complete = false
		}
		for _, r := range s.References {
			if r.ID == "" || r.Kind == "" {
				return out, storagebudget.ErrBounds
			}
			refs[r.Kind+":"+r.ID] = true
		}
	}
	seen = map[string]bool{}
	var manual, automatic uint64
	known := true
	type usage struct{ bytes, count uint64 }
	classUsage := map[string]usage{}
	ordered := append([]storageRetentionObject(nil), objects...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].Completed.Equal(ordered[j].Completed) {
			return ordered[i].Completed.Before(ordered[j].Completed)
		}
		return ordered[i].Kind+ordered[i].ID < ordered[j].Kind+ordered[j].ID
	})
	for _, v := range ordered {
		u := classUsage[v.Class]
		if v.Bytes > 1<<60 || u.bytes > 1<<60-v.Bytes {
			return out, storagebudget.ErrBounds
		}
		u.bytes += v.Bytes
		u.count++
		classUsage[v.Class] = u
	}
	for _, v := range ordered {
		if v.ID == "" || seen[v.Kind+":"+v.ID] || !assistantRunDigest(v.Revision) || v.Bytes > 1<<60 {
			return out, storagebudget.ErrBounds
		}
		seen[v.Kind+":"+v.ID] = true
		row := storageRetentionCandidate{v.ID, v.Kind, v.Revision, strconv.FormatUint(v.Bytes, 10), "protected", "durable-original"}
		switch {
		case !out.Complete:
			row.State = "unknown"
			row.Reason = "incomplete-reference-closure"
		case !v.Owned || !v.Current || !v.Private || !v.SingleLink:
			row.Reason = "ownership-or-linkage"
		case v.Pending:
			row.Reason = "pending-or-uncertain"
		case v.Pinned || refs[v.Kind+":"+v.ID]:
			row.Reason = "pinned-or-referenced"
		case !v.Released:
			row.State = "unknown"
			row.Reason = "reader-process-custody-unproved"
			known = false
		case !v.Disposable:
			row.State = "manual-candidate"
			row.Reason = "separate-owner-confirmation-required"
			if manual > 1<<60-v.Bytes {
				return out, storagebudget.ErrBounds
			}
			manual += v.Bytes
		default:
			idx := -1
			for i, c := range storageRetentionClasses {
				if c == v.Class && i != 3 {
					idx = i
				}
			}
			if idx < 0 {
				row.Reason = "unclassified-original"
				break
			}
			days, _ := storagePolicyHTTPUint(rules[idx].MaxAgeDays, true)
			if v.Completed.IsZero() || v.Completed.After(now) {
				row.State = "unknown"
				row.Reason = "completion-time-unknown"
				known = false
				break
			}
			maxBytes, _ := storagePolicyHTTPUint(rules[idx].MaxBytes, true)
			maxCount, _ := storagePolicyHTTPUint(rules[idx].MaxObjects, true)
			u := classUsage[v.Class]
			if now.Sub(v.Completed) < time.Duration(days)*24*time.Hour && u.bytes <= maxBytes && u.count <= maxCount {
				row.Reason = "within-retention-window"
				break
			}
			if rules[idx].Mode == "manual" {
				row.State = "manual-candidate"
				row.Reason = "separate-owner-confirmation-required"
				if manual > 1<<60-v.Bytes {
					return out, storagebudget.ErrBounds
				}
				manual += v.Bytes
				break
			}
			row.State = "automatic-candidate"
			row.Reason = "original-disposable-owner-required-at-execution"
			u.bytes -= v.Bytes
			u.count--
			classUsage[v.Class] = u
			if automatic > 1<<60-v.Bytes {
				return out, storagebudget.ErrBounds
			}
			automatic += v.Bytes
		}
		out.Objects = append(out.Objects, row)
	}
	out.Complete = out.Complete && known
	if out.Complete {
		m, a := storageStatusUInt(manual), storageStatusUInt(automatic)
		out.ManualCandidateBytes = &m
		out.AutomaticCandidateBytes = &a
	}
	// Physical exclusivity/free space is NOT proved by logical eligibility.
	raw, e := json.Marshal(struct {
		Sources []storageRetentionSource
		Objects []storageRetentionCandidate
	}{out.Sources, out.Objects})
	if e != nil {
		return out, e
	}
	out.Revision = digestOf(raw)
	return out, nil
}

func storageRetentionUnknown(name, kind string, raw []byte, reason string) storageRetentionSource {
	return storageRetentionSource{ID: digestOf([]byte(name)), Kind: kind, Revision: digestOf(raw), Complete: false, Reasons: []string{reason}, References: []storageRetentionReference{}}
}

func storageRetentionInstallationValid(id string) bool {
	_, err := storagePolicyHTTPID(id)
	return err == nil
}
func (r *storageRetentionRules) UnmarshalJSON(raw []byte) error {
	if checkJSONMembers(raw, reflect.TypeOf(r)) != nil {
		return storagebudget.ErrBounds
	}
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) != nil || len(rows) != 4 {
		return storagebudget.ErrBounds
	}
	for i, v := range rows {
		if checkJSONMembers(v, reflect.TypeOf(storageRetentionRule{})) != nil {
			return storagebudget.ErrBounds
		}
		d := json.NewDecoder(bytes.NewReader(v))
		d.DisallowUnknownFields()
		if d.Decode(&r[i]) != nil {
			return storagebudget.ErrBounds
		}
	}
	if !storageRetentionRulesValid(*r) {
		return storagebudget.ErrBounds
	}
	return nil
}
func storageRetentionConfig(o *storageBudgetOwner) (*storageRootBinding, error) {
	var root *storageRootBinding
	for _, r := range o.roots {
		if r.configControl {
			if root != nil {
				return nil, storagebudget.ErrAuthority
			}
			root = r
		}
	}
	if root == nil {
		return nil, storagebudget.ErrAuthority
	}
	return root, nil
}

// Every read owns its descriptor and joins Close. A successful result binds
// exact raw bytes and both the original entry and held description.
func storageRetentionRead(root *os.Root, name string, limit int) (raw []byte, info os.FileInfo, err error) {
	before, e := root.Lstat(name)
	if e != nil {
		return nil, nil, e
	}
	if !storagePrivate(before, false) || before.Size() > int64(limit) || before.Size() < 0 {
		return nil, nil, storagebudget.ErrBounds
	}
	f, e := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if e != nil {
		return nil, nil, e
	}
	defer func() {
		err = errors.Join(err, f.Close())
		if err != nil {
			raw = nil
			info = nil
		}
	}()
	held, e := f.Stat()
	if e != nil || !os.SameFile(before, held) {
		return nil, nil, errors.Join(errPrivateDataChanged, e)
	}
	raw, e = io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if e != nil || len(raw) > limit {
		return nil, nil, errors.Join(storagebudget.ErrBounds, e)
	}
	after, e := root.Lstat(name)
	last, le := f.Stat()
	if e != nil || le != nil || !os.SameFile(before, after) || !os.SameFile(before, last) || before.Size() != int64(len(raw)) || after.Size() != before.Size() || !before.ModTime().Equal(last.ModTime()) || !before.ModTime().Equal(after.ModTime()) {
		return nil, nil, errors.Join(errPrivateDataChanged, e, le)
	}
	return raw, last, nil
}
func storageRetentionLoad(root *storageRootBinding, installation storagebudget.ID) (p storageRetentionPolicy, raw []byte, err error) {
	raw, _, err = storageRetentionRead(root.root, storageRetentionName, storageRetentionBytes)
	if errors.Is(err, os.ErrNotExist) {
		return storageRetentionInitial(installation), nil, nil
	}
	if err != nil {
		return p, nil, err
	}
	p, err = decodeStorageRetention(raw)
	if err == nil && p.Installation != hex.EncodeToString(installation[:]) {
		err = storagebudget.ErrAuthority
	}
	return p, raw, err
}
func storageRetentionCurrentWorkspace(e *storageServerEnrollment, s *Server, o *storageBudgetOwner) (current storagebudget.RegisteredWorkspace, all []storagebudget.RegisteredWorkspace, err error) {
	id, err := e.construction.storageStatusWorkspace(s)
	if err != nil {
		return current, nil, err
	}
	all = []storagebudget.RegisteredWorkspace{}
	for _, w := range o.workspaces {
		row := storagebudget.RegisteredWorkspace{ID: w.ID, Generation: w.Generation, Limits: w.Limits}
		all = append(all, row)
		if w.ID == id {
			current = row
		}
	}
	if current.ID == (storagebudget.ID{}) {
		return current, nil, storagebudget.ErrAuthority
	}
	return current, all, nil
}
func storageRetentionFormat(p storageRetentionPolicy, raw []byte, w storagebudget.RegisteredWorkspace, now time.Time) (storageRetentionSnapshot, error) {
	id, gen := hex.EncodeToString(w.ID[:]), strconv.FormatUint(w.Generation, 10)
	rules, err := p.workspace(id, gen)
	return storageRetentionSnapshot{storageRetentionComparison{p.Installation, storageRetentionRevision(raw), id, gen}, now.UTC().Format(time.RFC3339Nano), p.InstallationRules, p.DefaultRules, rules, false}, err
}
func (s *Server) readStorageRetention(ctx context.Context) (out storageRetentionSnapshot, err error) {
	e, o, use, err := s.storageStatusLifetime(ctx)
	if err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, use.Close(), o.drop())
		if err != nil {
			out = storageRetentionSnapshot{}
		}
	}()
	// The existing read lifetime pins original currentness; GET admits no mutation.
	root, err := storageRetentionConfig(o)
	if err != nil {
		return out, err
	}
	p, raw, err := storageRetentionLoad(root, use.cell.record.Installation)
	if err != nil {
		return out, err
	}
	w, _, err := storageRetentionCurrentWorkspace(e, s, o)
	if err != nil {
		return out, err
	}
	out, err = storageRetentionFormat(p, raw, w, time.Now())
	if err == nil {
		err = errors.Join(e.statusCurrent(ctx, s, o), root.current(ctx))
	}
	return out, err
}
func (s *Server) updateStorageRetention(ctx context.Context, edit storageRetentionEdit) (out storageRetentionSnapshot, err error) {
	if s.storageReadOnly {
		return out, storagebudget.ErrAuthority
	}
	if !storageRetentionEditValid(edit) {
		return out, storagebudget.ErrBounds
	}
	e, o, use, err := s.storageStatusLifetime(ctx)
	if err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, use.Close(), o.drop())
		if err != nil {
			out = storageRetentionSnapshot{}
		}
	}()
	// Keep original registration exclusion through materialization, inner
	// single-file admission and checked drain. The inner permit owns its own
	// existing authority; it never borrows this one or recursively takes domain EX.
	a, err := o.beginAdmission(ctx)
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, a.Close()) }()
	root, err := storageRetentionConfig(o)
	if err != nil {
		return out, err
	}
	p, raw, err := storageRetentionLoad(root, use.cell.record.Installation)
	if err != nil {
		return out, err
	}
	w, all, err := storageRetentionCurrentWorkspace(e, s, o)
	if err != nil {
		return out, err
	}
	next, err := storageRetentionApply(p, all, w, storageRetentionRevision(raw), edit)
	if err != nil {
		return out, err
	}
	defer s.invalidateStorageStatusCache()
	encoded, err := json.Marshal(next)
	if err != nil || len(encoded) > storageRetentionBytes {
		return out, storagebudget.ErrBounds
	}
	nonce, err := storagebudget.NewID()
	if err != nil {
		return out, err
	}
	if err = errors.Join(a.current(ctx, true), e.statusCurrent(ctx, s, o)); err != nil {
		return out, err
	}
	permit, err := o.admitSingleFile(ctx, storageSingleFilePlan{Root: root, Target: storageRetentionName, Stage: ".retention-stage-" + hex.EncodeToString(nonce[:]), Workflow: storageRetentionWorkflow, Operation: hex.EncodeToString(nonce[:]), ExpectedAbsent: raw == nil, Expected: raw, Bytes: encoded, ObjectLimit: storageRetentionBytes})
	if err != nil {
		return out, err
	}
	err = permit.Write(ctx)
	err = errors.Join(err, permit.Close())
	if err == nil {
		err = errors.Join(a.current(ctx, false), e.statusCurrent(ctx, s, o))
		if err == nil {
			out, err = storageRetentionFormat(next, encoded, w, time.Now())
		}
	}
	return out, err
}

func storageRetentionHTTPDecode(raw []byte, v any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > storageRetentionRequestBytes || len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' || !uniqueJSON(raw) {
		return storagebudget.ErrBounds
	}
	if checkJSONMembers(raw, reflect.TypeOf(v)) != nil {
		return storagebudget.ErrBounds
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return storagebudget.ErrBounds
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return storagebudget.ErrBounds
	}
	if _, edit := v.(*storageRetentionEdit); edit && storageRetentionNullMember(raw, "rules") {
		return storagebudget.ErrBounds
	}
	return nil
}
func (s *Server) handleStorageRetention(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	path := r.URL.Path
	if path != "/api/storage/retention" && path != "/api/storage/retention/preview" && path != "/api/storage/retention/export" || path != r.URL.EscapedPath() || r.URL.RawQuery != "" || r.URL.ForceQuery {
		writeJSONCoded(w, 400, CodeBadRequest, "Use an exact retention route without query inputs.")
		return
	}
	if r.Method == http.MethodGet && path == "/api/storage/retention" {
		if r.ContentLength != 0 || r.Body != nil && r.Body != http.NoBody || len(r.TransferEncoding) != 0 {
			writeJSONCoded(w, 400, CodeBadRequest, "Retention reads do not accept a body.")
			return
		}
		value, err := s.readStorageRetention(r.Context())
		if err != nil {
			writeJSONCoded(w, 503, storageStatusUnknown, "Retention settings are unavailable; original storage preparation may be required.")
			return
		}
		writeJSON(w, 200, value)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		if path == "/api/storage/retention" {
			w.Header().Set("Allow", "GET, POST")
		}
		writeJSONCoded(w, 405, CodeBadRequest, "Unsupported retention method.")
		return
	}
	raw, err := storageRetentionHTTPRead(r)
	if err != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Use one bounded JSON retention request.")
		return
	}
	if path == "/api/storage/retention" {
		var edit storageRetentionEdit
		if storageRetentionHTTPDecode(raw, &edit) != nil || !storageRetentionEditValid(edit) {
			writeJSONCoded(w, 400, CodeBadRequest, "Invalid finite retention policy.")
			return
		}
		value, err := s.updateStorageRetention(r.Context(), edit)
		if err != nil {
			writeJSONCoded(w, 503, storageStatusUncertain, "Retention settings outcome is unconfirmed. Keep the draft and review original storage state before another edit.")
			return
		}
		writeJSON(w, 200, value)
		return
	}
	var request struct {
		Revision *string `json:"revision,omitempty"`
	}
	if storageRetentionHTTPDecode(raw, &request) != nil || storageRetentionNullMember(raw, "revision") || request.Revision != nil && !assistantRunDigest(*request.Revision) {
		writeJSONCoded(w, 400, CodeBadRequest, "Supply a canonical original source revision.")
		return
	}
	if path == "/api/storage/retention/export" {
		if request.Revision == nil {
			writeJSONCoded(w, 400, CodeBadRequest, "Export requires the exact preview source revision.")
			return
		}
		started, err := s.withOriginalRetentionExport(w, r, *request.Revision)
		if err != nil && !started {
			writeJSONCoded(w, 503, storageStatusUnknown, "Complete retention export is unavailable for this source closure. No successful export is claimed.")
		}
		return
	}
	expected := ""
	if request.Revision != nil {
		expected = *request.Revision
	}
	value, err := s.previewStorageRetention(r, expected)
	if err != nil {
		writeJSONCoded(w, 503, storageStatusUnknown, "Retention preview is unavailable or changed; no eligibility is established.")
		return
	}
	writeJSON(w, 200, value)
}

func storageRetentionHTTPRead(r *http.Request) ([]byte, error) {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || len(params) == 1 && params["charset"] != "utf-8" || r.Body == nil || r.ContentLength > storageRetentionRequestBytes || r.Header.Get("Content-Encoding") != "" {
		return nil, storagebudget.ErrBounds
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, storageRetentionRequestBytes+1))
	err = errors.Join(err, r.Body.Close())
	if err != nil || len(raw) > storageRetentionRequestBytes {
		return nil, errors.Join(storagebudget.ErrBounds, err)
	}
	return raw, nil
}
func storageRetentionMembers(raw []byte, names ...string) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return false
	}
	for name := range object {
		if !contains(names, name) {
			return false
		}
	}
	return true
}
func (s *storageRetentionSource) add(ref storageRetentionReference) {
	if len(s.References) >= storagebudget.MaxEntries {
		s.Complete = false
		if !contains(s.Reasons, "reference-bound") {
			s.Reasons = append(s.Reasons, "reference-bound")
		}
		return
	}
	s.References = append(s.References, ref)
}

func storageRetentionEditValid(v storageRetentionEdit) bool {
	if v.Expected == nil || !storageRetentionInstallationValid(v.Expected.Installation) || !storageRetentionInstallationValid(v.Expected.Workspace) || v.Expected.Revision != "absent" && !assistantRunDigest(v.Expected.Revision) {
		return false
	}
	if _, e := storagePolicyHTTPUint(v.Expected.Generation, true); e != nil {
		return false
	}
	if v.Scope == "copy-default" {
		return v.Rules == nil
	}
	return (v.Scope == "installation" || v.Scope == "default" || v.Scope == "workspace") && v.Rules != nil && storageRetentionRulesValid(*v.Rules)
}

type storageRetentionCapture struct {
	Sources   []storageRetentionSource
	Objects   []storageRetentionObject
	ChatNames map[string]string
	Retained  map[retainedArtifactRef][]byte
	Total     uint64
}

func (c *storageRetentionCapture) charge(raw []byte) error {
	if c.Total > maxBackupData || uint64(len(raw)) > maxBackupData-c.Total {
		return storagebudget.ErrBounds
	}
	c.Total += uint64(len(raw))
	return nil
}
func (c storageRetentionCapture) revision() string {
	raw, _ := json.Marshal(c.Sources)
	return digestOf(raw)
}

// Retained bytes are exact operands, not references to missing external data.
// This computes the bounded closure before any archive output. The active chat
// exporter cannot yet admit the project entries; that is an explicit refusal.
func (c storageRetentionCapture) exportable() bool {
	for _, source := range c.Sources {
		if !source.Complete {
			return false
		}
		for _, ref := range source.References {
			switch ref.Kind {
			case "attachment":
				if c.ChatNames[ref.ID] != ref.Revision {
					return false
				}
			case "chat":
				return false // source-memory verification needs its exact turn closure
			case "retained":
				return false // exact bytes captured; original export plan is chat-only
			default:
				return false
			}
		}
	}
	return len(c.Retained) == 0
}
func storageRetentionAttachmentKey(name string) string {
	if !attachmentFileName.MatchString(name) {
		return ""
	}
	return name[11:75] + ":" + strings.TrimSuffix(name[76:], ".json")
}

// Caller holds the existing private namespace exclusion and original source.
// No lookup here creates a directory, recovers a run or takes service.mu.
func (s *Server) captureRetentionChat(r *http.Request, store *chatDataStore, o *storageBudgetOwner) (out storageRetentionCapture, err error) {
	out = storageRetentionCapture{Sources: []storageRetentionSource{}, Objects: []storageRetentionObject{}, ChatNames: map[string]string{}, Retained: map[retainedArtifactRef][]byte{}}
	if store == nil || store.root == nil {
		return out, storagebudget.ErrAuthority
	}
	entries, err := storageRetentionEntries(store.root, o, "chat-entries-close")
	if err != nil {
		return out, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err = r.Context().Err(); err != nil {
			return out, err
		}
		name := entry.Name()
		if !chatStorageFile(name) {
			if name == dataMarkerName || strings.HasPrefix(name, ".data-") {
				continue
			}
			if store.legacy {
				continue
			}
			out.Sources = append(out.Sources, storageRetentionUnknown(name, "unknown-chat-entry", nil, "unrecognized-store-item"))
			continue
		}
		raw, info, e := storageRetentionRead(store.root, name, min(storageFileLimit(name), int(maxBackupData-out.Total)))
		if e != nil {
			return out, e
		}
		if e = out.charge(raw); e != nil {
			return out, e
		}
		if e = validateStorageFile(name, raw); e != nil {
			return out, e
		}
		if e = s.authorizeOriginalTransferExport(r, name, raw); e != nil {
			return out, e
		}
		var source storageRetentionSource
		switch {
		case conversationFileName.MatchString(name):
			source = retentionConversationReferences(name, raw)
		case assistantRunFileName.MatchString(name):
			source = retentionRunReferences(name, raw)
		case memoryFileName.MatchString(name):
			source = retentionMemoryReferences(name, raw)
		case attachmentFileName.MatchString(name):
			// Original attachment-object is closed; arbitrary acquired packet content
			// is retained as data and cannot create a Desk storage reference.
			source = storageRetentionSource{ID: digestOf([]byte(name)), Kind: "attachment", Revision: digestOf(raw), Complete: true, Reasons: []string{}, References: []storageRetentionReference{}}
		default:
			source = storageRetentionUnknown(name, "unsupported-chat-owner", raw, "source-owner-not-yet-covered")
		}
		out.Sources = append(out.Sources, source)
		id, kind := source.ID, source.Kind
		if key := storageRetentionAttachmentKey(name); key != "" {
			id, kind = key, "attachment"
			out.ChatNames[key] = retentionAttachmentResultDigest(raw)
		}
		links, known := linkCount(info)
		out.Objects = append(out.Objects, storageRetentionObject{ID: id, Kind: kind, Class: "history", Revision: digestOf(raw), Bytes: uint64(len(raw)), Owned: true, Current: true, Private: storagePrivate(info, false), SingleLink: known && links == 1, Released: false})
	}
	if store.legacy || store.location.Previous != "" {
		out.Sources = append(out.Sources, storageRetentionUnknown("historical-source", "history-location", []byte(store.revision), "legacy-or-previous-location-not-joined"))
	}
	return out, nil
}
func (s *Server) captureRetentionProject(ctx context.Context, c *storageRetentionCapture, o *storageBudgetOwner) (err error) {
	if s.root == nil || s.project == nil || s.project.own == nil || s.root != s.project.own.root {
		c.Sources = append(c.Sources, storageRetentionUnknown("project", "project", nil, "original-project-unavailable"))
		return nil
	}
	lease, err := s.lockProjectOwned(ctx, 0)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	if s.retainedArtifacts != nil || s.dialectDrafts != nil {
		c.Sources = append(c.Sources, storageRetentionUnknown("provider", "injected-provider", nil, "guarded-provider-enumeration-not-available"))
		return nil
	}
	root, absent, e := storageRetentionOwnFolder(s.root, strings.Split(publicationDirectory, "/"), "artifact versions", o, "publication-ancestor-close")
	if e != nil {
		return e
	}
	if absent {
		c.Sources = append(c.Sources, storageRetentionSource{ID: digestOf([]byte("publication-history")), Kind: "publications", Revision: digestOf(nil), Complete: true, Reasons: []string{}, References: []storageRetentionReference{}})
	} else {
		e = func() (err error) {
			defer func() { err = errors.Join(err, root.Close()) }()
			lock, e := root.Open(".")
			if e != nil {
				return e
			}
			defer func() { err = errors.Join(err, lock.Close()) }()
			if e = lockPrivateData(lock, false); e != nil {
				return e
			}
			raw, _, e := storageRetentionRead(root, "history.json", min(publicationLedgerLimit, int(maxBackupData-c.Total)))
			if errors.Is(e, os.ErrNotExist) {
				raw = []byte(`{"version":1,"items":[]}`)
			} else if e != nil {
				return e
			}
			if e = c.charge(raw); e != nil {
				return e
			}
			ledger, e := decodePublicationLedger(raw)
			if e != nil {
				return e
			}
			source := retentionPublicationReferences(raw, ledger)
			c.Sources = append(c.Sources, source)
			for _, ref := range source.References {
				n, e := strconv.Atoi(ref.Revision)
				if e != nil {
					return e
				}
				key := retainedArtifactRef{ref.ID, n}
				if _, seen := c.Retained[key]; seen {
					continue
				}
				if key.Bytes < 0 || uint64(key.Bytes) > maxBackupData-c.Total {
					return storagebudget.ErrBounds
				}
				bytes, e := s.readRetainedArtifactForRetention(ctx, key, o)
				if e != nil {
					return e
				}
				if e = verifyRetainedArtifact(key, bytes); e != nil {
					return e
				}
				if e = c.charge(bytes); e != nil {
					return e
				}
				c.Retained[key] = bytes
			}
			return nil
		}()
		if e != nil {
			return e
		}
	}
	return s.captureRetentionDrafts(ctx, c, o)
}

func (s *Server) previewStorageRetention(r *http.Request, expected string) (out storageRetentionPreview, err error) {
	e, o, use, err := s.storageStatusLifetime(r.Context())
	if err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, use.Close(), o.drop())
		if err != nil {
			out = storageRetentionPreview{}
		} else {
			out.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
	}()
	// All files already exist. Never use the O_CREATE lock helpers or run Acquire.
	settings, err := s.storageSettingsRoot()
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, settings.Close(), o.fail("retention-read", "settings-close")) }()
	var held []storageNamedFile
	defer func() {
		for i := len(held) - 1; i >= 0; i-- {
			err = errors.Join(err, held[i].current(), held[i].file.Close())
		}
	}()
	for _, name := range []string{".data-transfer.lock", ".data-work.lock"} {
		n, e := storageRetentionReadLock(settings, name, false)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return out, e
		}
		held = append(held, n)
	}
	if !s.writes.TryLock() {
		return out, storagebudget.ErrBusy
	}
	defer s.writes.Unlock()
	release, turn, err := s.privateDataTurns.tryAcquireOriginal(r.Context())
	if err != nil {
		return out, err
	}
	named, err := storageRetentionReadLock(settings, ".data.lock", true)
	if err != nil {
		release()
		return out, err
	}
	private := newPrivateDataShortLock(named.file, release, turn, true)
	defer func() { err = errors.Join(err, named.current(), private.Close()) }()
	// Borrow this already-owned settings root. The checked path cannot create
	// a pointer, claim a data directory or hide a second settings Close.
	store, err := s.openRetentionChatData(settings)
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, store.root.Close()) }()
	capture, err := s.captureRetentionChat(r, store, o)
	if err != nil {
		return out, err
	}
	if err = s.captureRetentionProject(r.Context(), &capture, o); err != nil {
		return out, err
	}
	sort.Slice(capture.Sources, func(i, j int) bool { return capture.Sources[i].ID < capture.Sources[j].ID })
	revision := capture.revision()
	if expected != "" && expected != revision {
		return out, storagebudget.ErrStale
	}
	root, err := storageRetentionConfig(o)
	if err != nil {
		return out, err
	}
	policy, _, err := storageRetentionLoad(root, use.cell.record.Installation)
	if err != nil {
		return out, err
	}
	current, _, err := storageRetentionCurrentWorkspace(e, s, o)
	if err != nil {
		return out, err
	}
	rules, err := policy.workspace(hex.EncodeToString(current.ID[:]), strconv.FormatUint(current.Generation, 10))
	if err != nil {
		return out, err
	}
	out, err = storageRetentionSelect(time.Now(), rules, capture.Sources, capture.Objects)
	out.ExportRevision = revision
	out.ExportComplete = capture.exportable()
	if err == nil {
		err = errors.Join(e.statusCurrent(r.Context(), s, o), use.Current())
	}
	return out, err
}
func (s *Server) withOriginalRetentionExport(w http.ResponseWriter, r *http.Request, revision string) (bool, error) {
	if !assistantRunDigest(revision) || s.storageEnrollment == nil {
		return false, storagebudget.ErrAuthority
	}
	// Existing transfer owns qualification/admission/archive/serving/Close. Only
	// the same original lexical source can validate this extra strict closure.
	return s.withOriginalChatExportCompared(w, r, &revision)
}
func (s *Server) checkOriginalRetentionExport(r *http.Request, store *chatDataStore, revision string, o *storageBudgetOwner) error {
	c, err := s.captureRetentionChat(r, store, o)
	if err != nil {
		return err
	}
	if err = s.captureRetentionProject(r.Context(), &c, o); err != nil {
		return err
	}
	sort.Slice(c.Sources, func(i, j int) bool { return c.Sources[i].ID < c.Sources[j].ID })
	if c.revision() != revision {
		return storagebudget.ErrStale
	}
	if !c.exportable() {
		return storagebudget.ErrUnsupported
	}
	return nil
}

func storageRetentionNullMember(raw []byte, name string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return true
	}
	v, ok := fields[name]
	return ok && bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}

// Existing original named-file identity checks, using a noncreating read-only
// description. The caller owns every returned Close and original queue release.
func storageRetentionReadLock(root *os.Root, name string, exclusive bool) (n storageNamedFile, err error) {
	f, err := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return n, err
	}
	info, err := f.Stat()
	if err != nil {
		return n, errors.Join(err, f.Close())
	}
	n = storageNamedFile{root: root, name: name, file: f, identity: info, size: 0}
	if err = n.current(); err == nil {
		err = lockPrivateData(f, exclusive)
	}
	if err == nil {
		err = n.current()
	}
	if err != nil {
		return storageNamedFile{}, errors.Join(err, f.Close())
	}
	return n, nil
}

// Retention owns this description and joins its Close before accepting rows.
// Fault labels exercise the actual read-only outer path; nil faults are inert.
func storageRetentionEntries(root *os.Root, o *storageBudgetOwner, phase string) (entries []os.DirEntry, err error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, dir.Close())
		if o != nil {
			err = errors.Join(err, o.fail("retention-read", phase))
		}
		if err != nil {
			entries = nil
		}
	}()
	entries, err = dir.ReadDir(maxStorageFiles + 1)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if len(entries) > maxStorageFiles {
		return nil, withCode(CodeTooLarge, errors.New("chat storage contains too many entries to inspect safely"))
	}
	return entries, nil
}

// Read only the already-persisted selection. Ordinary openChatData and its
// legacy first-use behavior remain unchanged. The caller owns settings.
func (s *Server) openRetentionChatData(settings *os.Root) (store *chatDataStore, err error) {
	data, _, err := storageRetentionRead(settings, dataLocationName, 16<<10)
	if err != nil {
		return nil, err
	}
	var location dataLocation
	if decodeDataJSON(data, &location) != nil || location.Version != 1 {
		return nil, errors.New("chat storage settings are invalid")
	}
	if s.containedDeskData() {
		if filepath.IsAbs(location.Path) || !filepath.IsLocal(location.Path) {
			return nil, errors.New("desk storage must stay inside its folder")
		}
		location.Path = filepath.Join(s.projectDir, ".desk-private", location.Path)
		if location.Previous != "" {
			if !filepath.IsLocal(location.Previous) {
				return nil, errors.New("invalid recovery location")
			}
			location.Previous = filepath.Join(s.projectDir, ".desk-private", location.Previous)
		}
	}
	if !filepath.IsAbs(location.Path) || filepath.Clean(location.Path) != location.Path {
		return nil, errors.New("chat storage settings are invalid; the existing files have not been changed")
	}
	for _, path := range []string{location.Path, location.Previous} {
		if path != "" {
			if err = s.policyMachineTarget(path); err != nil {
				return nil, err
			}
		}
	}
	if s.containedDeskData() && !pathContains(filepath.Join(s.projectDir, ".desk-private"), location.Path) {
		return nil, errors.New("desk data must stay inside its folder")
	}
	if !s.containedDeskData() && location.Path != s.configDir && pathContains(s.projectDir, location.Path) {
		return nil, errors.New("private chat storage must be outside the project")
	}
	var root *os.Root
	if location.Path == s.configDir {
		root, err = s.assistant.root.OpenRoot(".")
	} else {
		root, err = openRetentionExistingRoot(location.Path)
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, root.Close())
			store = nil
		}
	}()
	store = &chatDataStore{root: root, location: location, revision: digestOf(data), legacy: location.Path == s.configDir}
	if store.legacy {
		return store, nil
	}
	raw, _, err := storageRetentionRead(root, dataMarkerName, 4096)
	if err != nil {
		return nil, err
	}
	var marker dataMarker
	if err = decodeDataJSON(raw, &marker); err != nil {
		return nil, err
	}
	if marker.Version != 1 || marker.Owner != s.storageOwner() {
		return nil, errors.New("this folder belongs to a different Desk data store")
	}
	return store, nil
}

// The noncreating private-data walk, with every departed ancestor checked.
// Uses the same ownership, no-symlink, entry/description and permission rules
// as openPrivateDataRoot; it cannot mkdir, chmod, claim, recover or migrate.
func openRetentionExistingRoot(dir string) (*os.Root, error) {
	if !custodyChecked || !filepath.IsAbs(dir) || strings.ContainsAny(dir, "\x00\r\n") {
		return nil, errors.New("chat storage requires an absolute path on a platform with file ownership checks")
	}
	parts := splitPath(dir)
	root, err := os.OpenRoot(parts[0])
	if err != nil {
		return nil, err
	}
	label := parts[0]
	for index, part := range parts[1:] {
		label = filepath.Join(label, part)
		info, err := root.Lstat(part)
		last := index == len(parts)-2
		if err == nil {
			err = safeDirectory(label, info, last)
		}
		if err == nil && last && needsNarrowing(info.Mode()) {
			err = errors.New("chat storage must be private (directory permissions 0700)")
		}
		if err != nil {
			return nil, errors.Join(err, root.Close())
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			return nil, errors.Join(err, root.Close())
		}
		opened, err := next.Stat(".")
		if err == nil && !os.SameFile(info, opened) {
			err = errors.New("chat storage changed while being opened")
		}
		if err == nil {
			err = safeDirectory(label, opened, last)
		}
		if err == nil && last && needsNarrowing(opened.Mode()) {
			err = errors.New("chat storage permissions changed while being opened")
		}
		err = errors.Join(err, root.Close())
		if err != nil {
			return nil, errors.Join(err, next.Close())
		}
		root = next
	}
	if len(parts) < 2 {
		return nil, errors.Join(errors.New("choose a private data folder, not the filesystem root"), root.Close())
	}
	return root, nil
}

// Retention borrows the original project root and opens only existing own
// folders. As in openOwnFolder(false), every component must be a private,
// user-owned real directory with matching looked-at/opened identity. Unlike
// that ordinary helper, every departed ancestor Close participates in success.
// absent is proof of lookup absence AND successful cleanup, never errors.Is
// applied to a joined lookup/cleanup error. The caller owns the returned leaf.
func storageRetentionOwnFolder(start *os.Root, parts []string, keeps string, o *storageBudgetOwner, phase string) (*os.Root, bool, error) {
	if start == nil || len(parts) == 0 {
		return nil, false, storagebudget.ErrBounds
	}
	current := start
	closeOwned := func() error {
		if current == start {
			return nil
		}
		err := current.Close()
		current = start
		if o != nil {
			err = errors.Join(err, o.fail("retention-read", phase))
		}
		return err
	}
	for _, part := range parts {
		info, lookErr := current.Lstat(part)
		if lookErr != nil {
			closeErr := closeOwned()
			if errors.Is(lookErr, os.ErrNotExist) && closeErr == nil {
				return nil, true, nil
			}
			return nil, false, errors.Join(lookErr, closeErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, false, errors.Join(fmt.Errorf("%s is not a folder Desk keeps %s in", part, keeps), closeOwned())
		}
		if info.Mode().Perm()&0o077 != 0 {
			return nil, false, errors.Join(fmt.Errorf("%s is open to other users (%v), so Desk does not keep %s in it", part, info.Mode().Perm(), keeps), closeOwned())
		}
		if err := ownedByUs(part, info); err != nil {
			return nil, false, errors.Join(err, closeOwned())
		}
		next, err := current.OpenRoot(part)
		if err != nil {
			closeErr := closeOwned()
			if errors.Is(err, os.ErrNotExist) && closeErr == nil {
				return nil, true, nil
			}
			return nil, false, errors.Join(err, closeErr)
		}
		if held, statErr := next.Stat("."); statErr != nil || !os.SameFile(info, held) {
			return nil, false, errors.Join(fmt.Errorf("%s changed while it was being opened", part), statErr, next.Close(), closeOwned())
		}
		if err = closeOwned(); err != nil {
			return nil, false, errors.Join(err, next.Close())
		}
		current = next
	}
	return current, false, nil
}
