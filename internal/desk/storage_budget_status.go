package desk

// This diagnostic is a read projection, never admission authority. Q is a leaf:
// custody and lifetime checks run before/after StatusSnapshot, with no Desk mutex
// held during that call. The cache belongs to the original owner, not a client.
import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

const (
	storageStatusBytes           = 8 << 20
	storageStatusDerivedBytes    = 6160384
	storageStatusRowBytes        = 1024
	storageStatusEnvelopeBytes   = 262144
	storageStatusRefreshInterval = 5 * time.Second
	storageStatusStaleAfter      = 15 * time.Second
)

// These are the eight accepted storage catalogue codes. AI pause/recovery
// integration belongs to that catalogue's owner; this read invents no actions.
const (
	storageStatusQuota     = "storage-quota-exceeded"
	storageStatusHeadroom  = "storage-headroom-low"
	storageStatusDiskFull  = "storage-disk-full"
	storageStatusInodes    = "storage-inodes-low"
	storageStatusUnknown   = "storage-measurement-unknown"
	storageStatusBusy      = "storage-busy"
	storageStatusUncertain = "storage-outcome-uncertain"
	storageStatusUnbounded = "storage-writer-unbounded"
)

func storageStatusReason(err error) string {
	switch {
	case errors.Is(err, storagebudget.ErrLimit):
		return storageStatusQuota
	case errors.Is(err, storagebudget.ErrHeadroom):
		return storageStatusHeadroom
	case errors.Is(err, syscall.ENOSPC):
		return storageStatusDiskFull
	case errors.Is(err, storagebudget.ErrBusy), errors.Is(err, context.DeadlineExceeded):
		return storageStatusBusy
	default:
		return storageStatusUnknown
	}
}
func storageStatusCode(code string) bool {
	switch code {
	case storageStatusQuota, storageStatusHeadroom, storageStatusDiskFull, storageStatusInodes, storageStatusUnknown, storageStatusBusy, storageStatusUncertain, storageStatusUnbounded:
		return true
	}
	return false
}

type storageStatusVector struct {
	Bytes       string `json:"bytes"`
	Files       string `json:"files"`
	Directories string `json:"directories"`
}
type storageStatusCapacityVector struct {
	Bytes  string `json:"bytes"`
	Inodes string `json:"inodes"`
}
type storageStatusScope struct {
	ID         string `json:"id"`
	Generation string `json:"generation"`
	storageStatusAmounts
}
type storageStatusAmounts struct {
	Known                     bool                 `json:"known"`
	ChargedLogical            *storageStatusVector `json:"chargedLogical"`
	PromisedPeak              storageStatusVector  `json:"promisedPeak"`
	PromisedSettlement        storageStatusVector  `json:"promisedSettlement"`
	Limit                     *storageStatusVector `json:"limit"`
	LogicalRemaining          *storageStatusVector `json:"logicalRemaining"`
	EffectiveLogicalRemaining *storageStatusVector `json:"effectiveLogicalRemaining"`
	OverBudget                *bool                `json:"overBudget"`
}
type storageStatusCategory struct {
	Category string `json:"category"`
	storageStatusAmounts
}
type storageStatusFilesystem struct {
	ID                string               `json:"id"`
	Generation        string               `json:"generation"`
	ChargedLogical    *storageStatusVector `json:"chargedLogical"`
	Completeness      string               `json:"completeness"`
	MeasuredLogical   *storageStatusVector `json:"measuredLogical"`
	PhysicalAllocated *storageStatusVector `json:"physicalAllocated"`
	ConstraintIDs     []string             `json:"constraintIds"`
}
type storageStatusConstraint struct {
	ID           string                       `json:"id"`
	Generation   string                       `json:"generation"`
	Resources    []string                     `json:"resources"`
	Headroom     storageStatusCapacityVector  `json:"headroom"`
	PromisedPeak storageStatusCapacityVector  `json:"promisedPeak"`
	Available    *storageStatusCapacityVector `json:"available"`
	Remaining    *storageStatusCapacityVector `json:"remaining"`
	ObservedAt   *string                      `json:"observedAt"`
	Freshness    string                       `json:"freshness"`
	Basis        string                       `json:"basis"`
}
type storageStatusMaybeVector struct {
	Value   *storageStatusVector `json:"value"`
	Unknown string               `json:"unknown"`
}
type storageStatusPressure struct {
	Active       bool     `json:"active"`
	Codes        []string `json:"codes"`
	FirstSeenAt  *string  `json:"firstSeenAt"`
	LastSeenAt   *string  `json:"lastSeenAt"`
	DurableState string   `json:"durableState"`
}
type storageStatusSnapshot struct {
	ObservationSession  string `json:"observationSession"`
	ObservationSequence string `json:"observationSequence"`
	ObservedAt          string `json:"observedAt"`
	PolicyRevision      string `json:"policyRevision"`
	Epoch               string `json:"epoch"`
	Maintenance         bool   `json:"maintenance"`
	Coverage            string `json:"coverage"`
	Measurement         struct {
		At              *string                  `json:"at"`
		Generation      string                   `json:"generation"`
		Freshness       string                   `json:"freshness"`
		Logical         storageStatusMaybeVector `json:"logical"`
		Basis           string                   `json:"basis"`
		AllocationBasis string                   `json:"allocationBasis"`
		SharedBasis     string                   `json:"sharedBasis"`
	} `json:"measurement"`
	Installation               storageStatusScope        `json:"installation"`
	Categories                 []storageStatusCategory   `json:"categories"`
	CurrentWorkspace           *storageStatusScope       `json:"currentWorkspace"`
	CurrentWorkspaceCategories []storageStatusCategory   `json:"currentWorkspaceCategories"`
	Workspaces                 []storageStatusScope      `json:"workspaces"`
	Roots                      []storageStatusScope      `json:"roots"`
	Filesystems                []storageStatusFilesystem `json:"filesystems"`
	Constraints                []storageStatusConstraint `json:"constraints"`
	RetainedLogical            storageStatusMaybeVector  `json:"retainedLogical"`
	PhysicalAllocated          storageStatusMaybeVector  `json:"physicalAllocated"`
	SharedAttribution          struct {
		State string               `json:"state"`
		Value *storageStatusVector `json:"value"`
	} `json:"sharedAttribution"`
	Reclaimable struct {
		State             string               `json:"state"`
		EligibleLogical   *storageStatusVector `json:"eligibleLogical"`
		AllocatedEstimate *storageStatusVector `json:"allocatedEstimate"`
	} `json:"reclaimable"`
	Recovery struct {
		PendingRecoveryCount            *uint64 `json:"pendingRecoveryCount"`
		PendingRecoveryState            string  `json:"pendingRecoveryState"`
		UncertainTicketCount            uint64  `json:"uncertainTicketCount"`
		ActiveTicketCount               uint64  `json:"activeTicketCount"`
		TerminalAwaitingCheckpointCount uint64  `json:"terminalAwaitingCheckpointCount"`
	} `json:"recovery"`
}

// storageRegistrationStatus reports folders this start measures that the
// storage owner record does not register as roots of their own, and project
// folders given an identity after storage registered them under the folder
// (Identities; storage keeps each under the key it registered). It is a
// warning only: their bytes are counted and Desk keeps working.
type storageRegistrationStatus struct {
	Enforcement string   `json:"enforcement"`
	Folders     []string `json:"folders"`
	Identities  []string `json:"identities,omitempty"`
}

// storageRegistrationFolders bounds the folders one status reply names.
const storageRegistrationFolders = 64

// storageStatusRefusal is the entry a read-only start's census refused, in
// words, and its path.
type storageStatusRefusal struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type storageStatusResponse struct {
	ChildObservation *storageChildObservation   `json:"childObservation"`
	Registration     *storageRegistrationStatus `json:"registration"`
	Refusal          *storageStatusRefusal      `json:"refusal,omitempty"`
	Version          int                        `json:"version"`
	Availability     string                     `json:"availability"`
	Reason           *string                    `json:"reason"`
	ReadOnly         bool                       `json:"readOnly"`
	ServedAt         string                     `json:"servedAt"`
	Pressure         storageStatusPressure      `json:"pressure"`
	Snapshot         *storageStatusSnapshot     `json:"snapshot"`
}

// A single fixed-size volatile occurrence slot. Only owners reporting an actual
// refusal update it; reads never manufacture occurrences or clear uncertainty.
// There is no file log, notification, retry queue or per-poll stderr output.
type storageStatusPressureSlot struct {
	mu          sync.Mutex
	codes       [8]string
	count       uint8
	occurrences uint64
	retired     bool
	first, last time.Time
}

func (p *storageStatusPressureSlot) record(code string, now time.Time) {
	if !storageStatusCode(code) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recordLocked(code, now)
}
func (p *storageStatusPressureSlot) recordLocked(code string, now time.Time) {
	if p.retired {
		return
	}
	if p.count == 0 {
		p.first = now
	}
	found := false
	for _, old := range p.codes[:p.count] {
		found = found || old == code
	}
	if !found && p.count < 8 {
		p.codes[p.count] = code
		p.count++
	}
	if p.occurrences != ^uint64(0) {
		p.occurrences++
	}
	p.last = now
}

// Runtime reporters take only their own gate and these volatile slot mutexes.
// Retirement prevents late child reports from repopulating a closed owner.
func (p *storageStatusPressureSlot) retire() {
	p.mu.Lock()
	p.retired = true
	p.mu.Unlock()
}
func (p *storageStatusPressureSlot) recordRuntime(code string, now time.Time, local *storageStatusPressureSlot) {
	if !storageStatusCode(code) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.retired {
		return
	}
	p.recordLocked(code, now)
	local.record(code, now)
}
func (p *storageStatusPressureSlot) view() storageStatusPressure {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := storageStatusPressure{Codes: []string{}, DurableState: "unconfirmed"}
	if p.count != 0 {
		out.Active = true
		out.Codes = append(out.Codes, p.codes[:p.count]...)
		first, last := storageStatusTime(p.first), storageStatusTime(p.last)
		out.FirstSeenAt, out.LastSeenAt = &first, &last
	}
	return out
}
func (s *Server) recordStorageStatusProblem(err error) {
	// Startup owns these calls before serving. A failed activation with no owner
	// has its own bounded slot and cannot attach another installation's counters.
	now := time.Now()
	s.storageStatusPressure.record(storageStatusReason(err), now)
	if c := s.storageConstruction; c != nil && c.owner != nil {
		c.owner.statusPressure.record(storageStatusReason(err), now)
	}
}

// Cache format is private and sanitized. Compact numeric arrays preserve exact
// Go uint64 decoding while keeping the worst-case private encoding below 8MiB.
// No raw owner/locator/path/workflow/ticket/component is retained. Workspace
// category arrays exist only here, not as a public per-workspace category matrix.
type storageStatusNumbers [3]uint64
type storageStatusCategories [storagebudget.Categories]storageStatusNumbers
type storageStatusCachedScope struct {
	ID                      string
	Generation              uint64
	Known                   bool
	Usage, Peak, Settlement storageStatusCategories
	Total                   storageStatusNumbers
	Limits                  storageStatusCategories
}
type storageStatusCachedRoot struct {
	ID, Filesystem          string
	Generation              uint64
	Known                   bool
	Usage, Peak, Settlement storageStatusNumbers
}
type storageStatusCachedFilesystem struct {
	ID          string
	Generation  uint64
	Constraints []string
}
type storageStatusCachedConstraint struct {
	ID             string
	Generation     uint64
	Resources      uint8
	Headroom, Peak [2]uint64
}
type storageStatusProjection struct {
	Installation                string
	Revision, Epoch             uint64
	Maintenance                 bool
	Global                      storageStatusCachedScope
	Workspaces                  []storageStatusCachedScope
	Roots                       []storageStatusCachedRoot
	Filesystems                 []storageStatusCachedFilesystem
	Constraints                 []storageStatusCachedConstraint
	Active, Uncertain, Terminal uint64
}

type storageStatusCache struct {
	mu          sync.Mutex
	session     string
	sequence    uint64
	encoded     []byte
	observedAt  time.Time
	attemptedAt time.Time
	failed      bool
	refreshing  bool
	closed      bool
}

func (c *storageStatusCache) invalidate() {
	c.mu.Lock()
	c.encoded = nil
	c.failed = true
	c.mu.Unlock()
}
func (c *storageStatusCache) retire() {
	c.mu.Lock()
	c.closed = true
	c.encoded = nil
	c.mu.Unlock()
}
func (o *storageBudgetOwner) storageStatusCache() (*storageStatusCache, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closing || o.closed || o.transitionRequired {
		return nil, storagebudget.ErrAuthority
	}
	if o.statusCache == nil {
		session, err := storagebudget.NewID()
		if err != nil {
			return nil, err
		}
		o.statusCache = &storageStatusCache{session: storageHexID(session)}
	}
	return o.statusCache, nil
}

type storageStatusCacheView struct {
	encoded    []byte
	session    string
	sequence   uint64
	observedAt time.Time
	failed     bool
}

func (v storageStatusCacheView) stale(now time.Time, maintenance bool) bool {
	return v.failed || maintenance || now.Sub(v.observedAt) > storageStatusStaleAfter
}

func (c *storageStatusCache) begin(now time.Time) (storageStatusCacheView, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	view := storageStatusCacheView{c.encoded, c.session, c.sequence, c.observedAt, c.failed}
	if c.closed || c.refreshing || !c.attemptedAt.IsZero() && now.Sub(c.attemptedAt) < storageStatusRefreshInterval {
		return view, false
	}
	c.refreshing = true
	c.attemptedAt = now
	return view, true
}
func (c *storageStatusCache) finish(encoded []byte, now time.Time, err error) storageStatusCacheView {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshing = false
	if !c.closed {
		if err == nil && len(encoded) != 0 && len(encoded) <= storageStatusBytes && c.sequence != ^uint64(0) {
			c.encoded = encoded
			c.sequence++
			c.observedAt = now
			c.failed = false
		} else {
			c.failed = true
		}
	}
	return storageStatusCacheView{c.encoded, c.session, c.sequence, c.observedAt, c.failed}
}

func storageStatusTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func storageStatusUInt(n uint64) string    { return strconv.FormatUint(n, 10) }
func storageStatusVectorOf(v storageStatusNumbers) storageStatusVector {
	return storageStatusVector{storageStatusUInt(v[0]), storageStatusUInt(v[1]), storageStatusUInt(v[2])}
}
func storageStatusNums(v storagebudget.Vector) storageStatusNumbers {
	return storageStatusNumbers{v.Bytes, v.Files, v.Directories}
}
func storageStatusVectorValid(v storageStatusNumbers) bool {
	return v[0] <= 1<<60 && v[1] <= 1<<40 && v[2] <= 1<<40
}
func storageStatusAdd(a, b storageStatusNumbers) (storageStatusNumbers, error) {
	if !storageStatusVectorValid(a) || !storageStatusVectorValid(b) {
		return storageStatusNumbers{}, storagebudget.ErrBounds
	}
	var out storageStatusNumbers
	for i, cap := range (storageStatusNumbers{1 << 60, 1 << 40, 1 << 40}) {
		if a[i] > cap-b[i] {
			return storageStatusNumbers{}, storagebudget.ErrBounds
		}
		out[i] = a[i] + b[i]
	}
	return out, nil
}
func storageStatusSum(v storageStatusCategories) (out storageStatusNumbers, err error) {
	for _, row := range v {
		out, err = storageStatusAdd(out, row)
		if err != nil {
			return storageStatusNumbers{}, err
		}
	}
	return out, nil
}
func storageStatusMin(a, b storageStatusNumbers) storageStatusNumbers {
	return storageStatusNumbers{min(a[0], b[0]), min(a[1], b[1]), min(a[2], b[2])}
}
func storageStatusResidual(usage, peak, settlement, limit storageStatusNumbers) (storageStatusNumbers, bool, error) {
	if !storageStatusVectorValid(limit) {
		return storageStatusNumbers{}, false, storagebudget.ErrBounds
	}
	used, err := storageStatusAdd(usage, peak)
	if err != nil {
		return storageStatusNumbers{}, false, err
	}
	used, err = storageStatusAdd(used, settlement)
	if err != nil {
		return storageStatusNumbers{}, false, err
	}
	var out storageStatusNumbers
	over := false
	for i := range out {
		if used[i] > limit[i] {
			over = true
		} else {
			out[i] = limit[i] - used[i]
		}
	}
	return out, over, nil
}
func storageStatusCategoriesOf(v [storagebudget.Categories]storagebudget.Vector) (out storageStatusCategories) {
	for i, row := range v {
		out[i] = storageStatusNums(row)
	}
	return
}
func storageStatusCachedAmounts(id string, generation uint64, known bool, usage [storagebudget.Categories]storagebudget.Vector, promises storagebudget.StatusPromises, limits storagebudget.Limits) storageStatusCachedScope {
	return storageStatusCachedScope{id, generation, known, storageStatusCategoriesOf(usage), storageStatusCategoriesOf(promises.Peak), storageStatusCategoriesOf(promises.Settlement), storageStatusNums(limits.Total), storageStatusCategoriesOf(limits.Category)}
}
func storageStatusID(id string) bool {
	if len(id) != 32 {
		return false
	}
	nonzero := false
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
		nonzero = nonzero || c != '0'
	}
	return nonzero
}

func storageStatusSanitize(v storagebudget.StatusSnapshot, installation storagebudget.ID) (storageStatusProjection, error) {
	if installation == (storagebudget.ID{}) || len(v.Roots) > storagebudget.MaxRoots || len(v.Workspaces) > storagebudget.MaxWorkspaces || len(v.Filesystems) > storagebudget.MaxFilesystems || len(v.CapacityConstraints) > storagebudget.MaxCapacityConstraints || len(v.CapacityPromised) > storagebudget.MaxCapacityConstraints {
		return storageStatusProjection{}, storagebudget.ErrBounds
	}
	p := storageStatusProjection{Installation: storageHexID(installation), Revision: v.Revision, Epoch: v.Epoch, Maintenance: v.Maintenance,
		Workspaces: []storageStatusCachedScope{}, Roots: []storageStatusCachedRoot{}, Filesystems: []storageStatusCachedFilesystem{}, Constraints: []storageStatusCachedConstraint{},
		Active: v.Tickets.Active, Uncertain: v.Tickets.Uncertain, Terminal: v.Tickets.TerminalUnapplied}
	known := true
	for _, row := range v.Roots {
		known = known && row.Known
	}
	for _, row := range v.Workspaces {
		known = known && row.Known
	}
	p.Global = storageStatusCachedAmounts(p.Installation, 0, known, v.Installation, v.Promises, v.Policy.Installation)
	for _, row := range v.Workspaces {
		p.Workspaces = append(p.Workspaces, storageStatusCachedAmounts(storageHexID(row.ID), row.Generation, row.Known, row.Usage, row.Promises, row.Limits))
	}
	for _, row := range v.Roots {
		usage, e := storageStatusSum(storageStatusCategoriesOf(row.Usage))
		if e != nil {
			return storageStatusProjection{}, e
		}
		peak, e := storageStatusSum(storageStatusCategoriesOf(row.Promises.Peak))
		if e != nil {
			return storageStatusProjection{}, e
		}
		settlement, e := storageStatusSum(storageStatusCategoriesOf(row.Promises.Settlement))
		if e != nil {
			return storageStatusProjection{}, e
		}
		p.Roots = append(p.Roots, storageStatusCachedRoot{storageHexID(row.ID), storageHexID(row.Filesystem), row.Generation, row.Known, usage, peak, settlement})
	}
	for _, row := range v.Filesystems {
		refs := []string{storageHexID(row.ByteConstraint)}
		if row.InodeConstraint != (storagebudget.ID{}) && row.InodeConstraint != row.ByteConstraint {
			refs = append(refs, storageHexID(row.InodeConstraint))
		}
		p.Filesystems = append(p.Filesystems, storageStatusCachedFilesystem{storageHexID(row.ID), row.Generation, refs})
	}
	promised := map[storagebudget.ID]storagebudget.CapacityPromise{}
	for _, row := range v.CapacityPromised {
		if _, exists := promised[row.Constraint]; exists {
			return storageStatusProjection{}, storagebudget.ErrBounds
		}
		promised[row.Constraint] = row
	}
	for _, row := range v.CapacityConstraints {
		peak := storagebudget.CapacityVector{}
		if got, exists := promised[row.ID]; exists {
			if got.Generation != row.Generation {
				return storageStatusProjection{}, storagebudget.ErrBounds
			}
			peak = got.Peak
			delete(promised, row.ID)
		}
		p.Constraints = append(p.Constraints, storageStatusCachedConstraint{storageHexID(row.ID), row.Generation, row.Resources, [2]uint64{row.Headroom.Bytes, row.Headroom.Inodes}, [2]uint64{peak.Bytes, peak.Inodes}})
	}
	if len(promised) != 0 {
		return storageStatusProjection{}, storagebudget.ErrBounds
	}
	// Validate every public row and every workspace's private category view before
	// publication, not only the first child's selected view.
	if _, err := p.public("", "", 0, time.Time{}); err != nil {
		return storageStatusProjection{}, err
	}
	return p, nil
}

var storageStatusCategoryNames = [storagebudget.Categories]string{"history", "retained", "attachments", "memory", "control", "disposable", "binaries", "transient", "settlement"}

func storageStatusScopeOf(s storageStatusCachedScope, intersection *storageStatusNumbers) (storageStatusScope, storageStatusNumbers, error) {
	out := storageStatusScope{ID: s.ID, Generation: storageStatusUInt(s.Generation)}
	if !storageStatusID(s.ID) {
		return out, storageStatusNumbers{}, storagebudget.ErrBounds
	}
	usage, err := storageStatusSum(s.Usage)
	if err != nil {
		return out, storageStatusNumbers{}, err
	}
	peak, err := storageStatusSum(s.Peak)
	if err != nil {
		return out, storageStatusNumbers{}, err
	}
	settlement, err := storageStatusSum(s.Settlement)
	if err != nil {
		return out, storageStatusNumbers{}, err
	}
	// Validate combined obligations even when committed usage is unknown.
	if _, err = storageStatusAdd(peak, settlement); err != nil {
		return out, storageStatusNumbers{}, err
	}
	if !storageStatusVectorValid(s.Total) {
		return out, storageStatusNumbers{}, storagebudget.ErrBounds
	}
	out.Known = s.Known
	out.PromisedPeak, out.PromisedSettlement = storageStatusVectorOf(peak), storageStatusVectorOf(settlement)
	limit := storageStatusVectorOf(s.Total)
	out.Limit = &limit
	var residual storageStatusNumbers
	if s.Known {
		charged := storageStatusVectorOf(usage)
		out.ChargedLogical = &charged
		var over bool
		residual, over, err = storageStatusResidual(usage, peak, settlement, s.Total)
		if err != nil {
			return out, residual, err
		}
		remaining := storageStatusVectorOf(residual)
		out.LogicalRemaining = &remaining
		out.OverBudget = &over
		if intersection != nil {
			effective := storageStatusVectorOf(storageStatusMin(residual, *intersection))
			out.EffectiveLogicalRemaining = &effective
		}
	}
	return out, residual, nil
}
func storageStatusCategoryRows(s storageStatusCachedScope, total storageStatusNumbers, effective *storageStatusNumbers, global *[storagebudget.Categories]storageStatusNumbers) ([]storageStatusCategory, [storagebudget.Categories]storageStatusNumbers, error) {
	rows := make([]storageStatusCategory, storagebudget.Categories)
	_, totalOver, err := storageStatusResidualSum(s)
	if err != nil {
		return nil, [storagebudget.Categories]storageStatusNumbers{}, err
	}
	var residuals [storagebudget.Categories]storageStatusNumbers
	for i := range rows {
		if !storageStatusVectorValid(s.Usage[i]) || !storageStatusVectorValid(s.Peak[i]) || !storageStatusVectorValid(s.Settlement[i]) || !storageStatusVectorValid(s.Limits[i]) {
			return nil, residuals, storagebudget.ErrBounds
		}
		row := storageStatusCategory{Category: storageStatusCategoryNames[i]}
		row.Known = s.Known
		row.PromisedPeak, row.PromisedSettlement = storageStatusVectorOf(s.Peak[i]), storageStatusVectorOf(s.Settlement[i])
		limit := storageStatusVectorOf(s.Limits[i])
		row.Limit = &limit
		if s.Known {
			charged := storageStatusVectorOf(s.Usage[i])
			row.ChargedLogical = &charged
			residual, over, err := storageStatusResidual(s.Usage[i], s.Peak[i], s.Settlement[i], s.Limits[i])
			if err != nil {
				return nil, residuals, err
			}
			// The category is over budget if either its own or the shared total cap is exceeded.
			over = over || totalOver
			residuals[i] = storageStatusMin(residual, total)
			remaining := storageStatusVectorOf(residuals[i])
			row.LogicalRemaining = &remaining
			row.OverBudget = &over
			if effective != nil {
				r := storageStatusMin(residuals[i], *effective)
				if global != nil {
					r = storageStatusMin(r, global[i])
				}
				value := storageStatusVectorOf(r)
				row.EffectiveLogicalRemaining = &value
			}
		}
		rows[i] = row
	}
	return rows, residuals, nil
}
func storageStatusResidualSum(s storageStatusCachedScope) (storageStatusNumbers, bool, error) {
	u, e := storageStatusSum(s.Usage)
	if e != nil {
		return storageStatusNumbers{}, false, e
	}
	p, e := storageStatusSum(s.Peak)
	if e != nil {
		return storageStatusNumbers{}, false, e
	}
	t, e := storageStatusSum(s.Settlement)
	if e != nil {
		return storageStatusNumbers{}, false, e
	}
	return storageStatusResidual(u, p, t, s.Total)
}

func (p storageStatusProjection) public(workspace, session string, sequence uint64, observed time.Time) (*storageStatusSnapshot, error) {
	if !storageStatusID(p.Installation) || p.Global.ID != p.Installation || len(p.Roots) > 4096 || len(p.Workspaces) > 1024 || len(p.Filesystems) > 128 || len(p.Constraints) > 512 || p.Active > 64 || p.Uncertain > 64 || p.Terminal > 64 || p.Active+p.Uncertain+p.Terminal > 64 {
		return nil, storagebudget.ErrBounds
	}
	out := &storageStatusSnapshot{ObservationSession: session, ObservationSequence: storageStatusUInt(sequence), ObservedAt: storageStatusTime(observed), PolicyRevision: storageStatusUInt(p.Revision), Epoch: storageStatusUInt(p.Epoch), Maintenance: p.Maintenance, Coverage: "registered-scope-only", Workspaces: []storageStatusScope{}, Roots: []storageStatusScope{}, Filesystems: []storageStatusFilesystem{}, Constraints: []storageStatusConstraint{}}
	out.Measurement.Generation = "0"
	out.Measurement.Logical.Unknown = "not-measured"
	out.Measurement.Basis = "registered-metadata"
	out.Measurement.AllocationBasis = "reported-blocks"
	out.Measurement.SharedBasis = "workspace-reference-overlap"
	out.Measurement.Freshness = "unknown"
	if p.Maintenance {
		out.Measurement.Freshness = "maintenance"
	}
	out.RetainedLogical.Unknown, out.PhysicalAllocated.Unknown = "not-measured", "not-measured"
	out.SharedAttribution.State, out.Reclaimable.State = "not-measured", "not-measured"
	out.Recovery.PendingRecoveryState = "not-measured"
	out.Recovery.UncertainTicketCount, out.Recovery.ActiveTicketCount, out.Recovery.TerminalAwaitingCheckpointCount = p.Uncertain, p.Active, p.Terminal
	installation, residual, err := storageStatusScopeOf(p.Global, nil)
	if err != nil {
		return nil, err
	}
	// Installation has no separate durable generation; epoch is the observation's
	// maintenance/census generation, not an installation identity replacement.
	installation.Generation = storageStatusUInt(p.Epoch)
	var globalRemaining *storageStatusNumbers
	if p.Global.Known {
		globalRemaining = &residual
		value := storageStatusVectorOf(residual)
		installation.EffectiveLogicalRemaining = &value
	}
	out.Installation = installation
	cats, globalCats, err := storageStatusCategoryRows(p.Global, residual, globalRemaining, nil)
	if err != nil {
		return nil, err
	}
	out.Categories = cats
	seen := map[string]bool{}
	for _, row := range p.Workspaces {
		if row.Generation == 0 || p.Global.Known && !row.Known || seen[row.ID] {
			return nil, storagebudget.ErrBounds
		}
		seen[row.ID] = true
		scope, total, e := storageStatusScopeOf(row, globalRemaining)
		if e != nil {
			return nil, e
		}
		rows, _, e := storageStatusCategoryRows(row, total, globalRemaining, &globalCats)
		if e != nil {
			return nil, e
		}
		out.Workspaces = append(out.Workspaces, scope)
		if row.ID == workspace {
			copy := scope
			out.CurrentWorkspace = &copy
			out.CurrentWorkspaceCategories = rows
		}
	}
	if workspace != "" && out.CurrentWorkspace == nil {
		return nil, storagebudget.ErrAuthority
	}
	seen = map[string]bool{}
	// Unknown registered roots make the installation unknown even if an internal
	// malformed cache claimed otherwise. Validate rather than masking that error.
	for _, row := range p.Roots {
		if !storageStatusID(row.ID) || !storageStatusID(row.Filesystem) || row.Generation == 0 || seen[row.ID] || p.Global.Known && !row.Known {
			return nil, storagebudget.ErrBounds
		}
		seen[row.ID] = true
		if !storageStatusVectorValid(row.Usage) || !storageStatusVectorValid(row.Peak) || !storageStatusVectorValid(row.Settlement) {
			return nil, storagebudget.ErrBounds
		}
		if _, e := storageStatusAdd(row.Peak, row.Settlement); e != nil {
			return nil, e
		}
		scope := storageStatusScope{ID: row.ID, Generation: storageStatusUInt(row.Generation)}
		scope.Known = row.Known
		scope.PromisedPeak = storageStatusVectorOf(row.Peak)
		scope.PromisedSettlement = storageStatusVectorOf(row.Settlement)
		if row.Known {
			value := storageStatusVectorOf(row.Usage)
			scope.ChargedLogical = &value
		}
		out.Roots = append(out.Roots, scope)
	}
	constraintIDs := map[string]bool{}
	for _, row := range p.Constraints {
		if !storageStatusID(row.ID) || row.Generation == 0 || constraintIDs[row.ID] || row.Resources == 0 || row.Resources&^uint8(3) != 0 || row.Headroom[0] > 1<<60 || row.Headroom[1] > 1<<40 || row.Peak[0] > 1<<60 || row.Peak[1] > 1<<40 {
			return nil, storagebudget.ErrBounds
		}
		constraintIDs[row.ID] = true
		if row.Resources&storagebudget.CapacityBytes == 0 && (row.Headroom[0] != 0 || row.Peak[0] != 0) || row.Resources&storagebudget.CapacityInodes == 0 && (row.Headroom[1] != 0 || row.Peak[1] != 0) {
			return nil, storagebudget.ErrBounds
		}
		resources := []string{}
		if row.Resources&storagebudget.CapacityBytes != 0 {
			resources = append(resources, "bytes")
		}
		if row.Resources&storagebudget.CapacityInodes != 0 {
			resources = append(resources, "inodes")
		}
		out.Constraints = append(out.Constraints, storageStatusConstraint{ID: row.ID, Generation: storageStatusUInt(row.Generation), Resources: resources, Headroom: storageStatusCapacityVector{storageStatusUInt(row.Headroom[0]), storageStatusUInt(row.Headroom[1])}, PromisedPeak: storageStatusCapacityVector{storageStatusUInt(row.Peak[0]), storageStatusUInt(row.Peak[1])}, Freshness: "unknown", Basis: "declared-policy-demand"})
	}
	seen = map[string]bool{}
	var filesystemTotal storageStatusNumbers
	allRootsKnown := true
	for _, row := range p.Roots {
		if !row.Known {
			allRootsKnown = false
			continue
		}
		filesystemTotal, err = storageStatusAdd(filesystemTotal, row.Usage)
		if err != nil {
			return nil, err
		}
	}
	globalUsage, err := storageStatusSum(p.Global.Usage)
	if err != nil {
		return nil, err
	}
	// Protected control accounting can be outside registered roots. Such unmatched
	// installation charges prevent calling a filesystem subtotal complete.
	complete := allRootsKnown && filesystemTotal == globalUsage
	for _, row := range p.Filesystems {
		if !storageStatusID(row.ID) || row.Generation == 0 || seen[row.ID] || len(row.Constraints) == 0 || len(row.Constraints) > 2 {
			return nil, storagebudget.ErrBounds
		}
		seen[row.ID] = true
		used := storageStatusNumbers{}
		knownCount, unknownCount := 0, 0
		for _, root := range p.Roots {
			if root.Filesystem != row.ID {
				continue
			}
			if !root.Known {
				unknownCount++
				continue
			}
			knownCount++
			used, err = storageStatusAdd(used, root.Usage)
			if err != nil {
				return nil, err
			}
		}
		refs := map[string]bool{}
		for _, id := range row.Constraints {
			if !constraintIDs[id] || refs[id] {
				return nil, storagebudget.ErrBounds
			}
			refs[id] = true
		}
		scope := storageStatusFilesystem{ID: row.ID, Generation: storageStatusUInt(row.Generation), Completeness: "unknown", ConstraintIDs: append([]string(nil), row.Constraints...)}
		if knownCount > 0 || unknownCount == 0 && complete {
			value := storageStatusVectorOf(used)
			scope.ChargedLogical = &value
			scope.Completeness = "partial"
			if unknownCount == 0 && complete {
				scope.Completeness = "complete"
			}
		}
		out.Filesystems = append(out.Filesystems, scope)
	}
	for _, root := range p.Roots {
		if !seen[root.Filesystem] {
			return nil, storagebudget.ErrBounds
		}
	}
	if err := storageStatusCheckRows(out); err != nil {
		return nil, err
	}
	return out, nil
}

// Encoding uses a capped writer before headers. No path can send partial JSON
// or silently truncate rows. Row and envelope caps enforce the stricter public
// derivation as well as the final emergency cap.
type storageStatusWriter struct {
	bytes.Buffer
	limit int
}

func (w *storageStatusWriter) Write(b []byte) (int, error) {
	if len(b) > w.limit-w.Len() {
		return 0, storagebudget.ErrBounds
	}
	return w.Buffer.Write(b)
}
func storageStatusEncode(v any, limit int) ([]byte, error) {
	w := &storageStatusWriter{limit: limit}
	e := json.NewEncoder(w).Encode(v)
	if e != nil {
		return nil, e
	}
	return w.Bytes(), nil
}
func storageStatusCheckRows(s *storageStatusSnapshot) error {
	check := func(v any) error { _, e := storageStatusEncode(v, storageStatusRowBytes); return e }
	for _, row := range s.Roots {
		if e := check(row); e != nil {
			return e
		}
	}
	for _, row := range s.Workspaces {
		if e := check(row); e != nil {
			return e
		}
	}
	for _, row := range s.Filesystems {
		if e := check(row); e != nil {
			return e
		}
	}
	for _, row := range s.Constraints {
		if e := check(row); e != nil {
			return e
		}
	}
	envelope := *s
	envelope.Roots = nil
	envelope.Workspaces = nil
	envelope.Filesystems = nil
	envelope.Constraints = nil
	_, e := storageStatusEncode(envelope, storageStatusEnvelopeBytes)
	return e
}

func (e *storageServerEnrollment) statusCurrent(ctx context.Context, s *Server, o *storageBudgetOwner) error {
	e.mu.Lock()
	valid := e.self == e && !e.closed && e.server == s && s.storageEnrollment == e && e.construction != nil && e.ref.valid() && e.construction.ref != nil && e.ref.cell == e.construction.ref.cell && e.construction.owner == o
	e.mu.Unlock()
	if !valid {
		return storagebudget.ErrAuthority
	}
	o.mu.Lock()
	live := o.self == o && !o.closing && !o.closed && !o.transitionRequired
	o.mu.Unlock()
	if !live {
		return storagebudget.ErrAuthority
	}
	c := e.construction
	if s.deskOwner() != c.server || s.configDir != c.server.configDir || s.assistant == nil || c.sources == nil {
		return storagebudget.ErrAuthority
	}
	if s != c.server {
		if s.cfg.parent != c.server || s.project == nil || s.project.own.root != s.root || c.server.home == nil || s.retainedArtifactProviders != c.providers {
			return storagebudget.ErrAuthority
		}
		if p, ok := s.retainedArtifacts.(*persistentInjectedArtifactProvider); ok {
			if c.providers == nil || c.providers.byID[p.id] == nil || c.providers.byID[p.id].provider != p || len(c.sources.selectedProviders[p.id]) == 0 {
				return storagebudget.ErrAuthority
			}
		}
		entry := c.server.home.entry(s.projectDir)
		if entry == nil {
			return storagebudget.ErrAuthority
		}
		original, ok := c.sources.projects[entry.ID]
		if !ok || original.entry != *entry {
			return storagebudget.ErrAuthority
		}
		if err := original.named.current(); err != nil {
			return err
		}
		a, err := original.named.root.Stat(".")
		b, be := s.root.Stat(".")
		if err != nil || be != nil || !os.SameFile(a, b) || !os.SameFile(b, s.project.info) {
			return errStorageCustody
		}
	}
	return c.current(ctx, false)
}
func (s *Server) storageStatusLifetime(ctx context.Context) (*storageServerEnrollment, *storageBudgetOwner, *storageInstallationUse, error) {
	e := s.storageEnrollment
	if e == nil {
		return nil, nil, nil, storagebudget.ErrAuthority
	}
	e.mu.Lock()
	if e.self != e || e.closed || e.server != s || e.construction == nil || e.construction.owner == nil || !e.ref.valid() || e.construction.ref == nil || e.ref.cell != e.construction.ref.cell {
		e.mu.Unlock()
		return nil, nil, nil, storagebudget.ErrAuthority
	}
	o := e.construction.owner
	err := o.retain()
	e.mu.Unlock()
	if err != nil {
		return nil, nil, nil, err
	}
	refuse := func(err error) (*storageServerEnrollment, *storageBudgetOwner, *storageInstallationUse, error) {
		_ = o.drop()
		return nil, nil, nil, err
	}
	if err = e.statusCurrent(ctx, s, o); err != nil {
		return refuse(err)
	}
	use, err := e.ref.acquire()
	if err != nil {
		return refuse(err)
	}
	if err = use.Current(); err == nil {
		err = e.statusCurrent(ctx, s, o)
	}
	if err != nil {
		_ = use.Close()
		return refuse(err)
	}
	return e, o, use, nil
}

func (s *Server) handleStorageStatus(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != "/api/storage/status" || r.URL.EscapedPath() != "/api/storage/status" || r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSONCoded(w, http.StatusMethodNotAllowed, CodeBadRequest, "Storage status requires an exact GET request.")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.Body != nil && r.Body != http.NoBody {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Storage status does not accept query or body inputs.")
		return
	}
	if s.storageLineOffStatus(w) {
		return
	}
	s.writeStorageStatus(w, s.readStorageStatus(r.Context()))
}

// Complete the original-current read before delivering its detached display
// response. A slow response writer must not retain owner/installation use and
// block first-use enrollment after the diagnostic read has finished.
func (s *Server) readStorageStatus(parent context.Context) storageStatusResponse {
	now := time.Now()
	out := storageStatusResponse{Version: 1, Availability: "unavailable", ReadOnly: s.storageReadOnly, ServedAt: storageStatusTime(now), Pressure: s.storageStatusPressure.view()}
	reason := storageStatusUnknown
	out.Reason = &reason
	// The request owns the synchronous refresh lifetime. Cancellation discards its
	// result; it cannot cancel another request, spawn a replacement, or drop these
	// retained descriptors before an actual blocking filesystem call returns.
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	out.ChildObservation = s.storageChildStatus(ctx, now)
	out.Registration = s.storageRegistrationStatus()
	var refused *storageCensusRefusal
	if s.storageReadOnly && errors.As(s.storageProblem, &refused) {
		out.Refusal = &storageStatusRefusal{Path: refused.Path, Reason: refused.Error()}
	}
	e, o, use, err := s.storageStatusLifetime(ctx)
	if err != nil {
		reason = storageStatusReason(err)
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, storagebudget.ErrBusy) {
			s.invalidateStorageStatusCache()
		}
		return out
	}
	defer o.drop()
	defer use.Close()
	out.Pressure = o.statusPressure.view()
	cache, err := o.storageStatusCache()
	if err != nil {
		return out
	}
	view, refresh := cache.begin(now)
	measuredBefore, failedBefore := o.ownedMeasurement()
	activityBefore := o.ownedMeasurementActivity()
	genuineRefresh := false
	if refresh {
		value, readErr := use.cell.q.StatusSnapshot(ctx)
		var encoded []byte
		if readErr == nil {
			var projection storageStatusProjection
			projection, readErr = storageStatusSanitize(value, use.cell.record.Installation)
			if readErr == nil {
				encoded, readErr = storageStatusEncode(projection, storageStatusBytes)
			}
		}
		// Q has returned; owner and cell mutexes are now safe to acquire.
		authorityErr := use.Current()
		if authorityErr == nil {
			authorityErr = e.statusCurrent(context.WithoutCancel(ctx), s, o)
		}
		if authorityErr != nil {
			cache.invalidate()
			cache.finish(nil, time.Now(), authorityErr)
			return out
		}
		if readErr == nil {
			o.mu.Lock()
			epoch := o.censusEpoch
			o.mu.Unlock()
			if epoch != 0 && epoch != value.Epoch {
				readErr = storagebudget.ErrStale
				cache.invalidate()
			}
		}
		if readErr == nil {
			readErr = ctx.Err()
		}
		genuineRefresh = readErr == nil
		view = cache.finish(encoded, time.Now(), readErr)
		if readErr != nil {
			reason = storageStatusReason(readErr)
		}
	}
	if len(view.encoded) != 0 {
		var projection storageStatusProjection
		if len(view.encoded) > storageStatusBytes {
			err = storagebudget.ErrBounds
		} else {
			err = json.Unmarshal(view.encoded, &projection)
		}
		if err == nil {
			o.mu.Lock()
			epoch := o.censusEpoch
			o.mu.Unlock()
			if epoch != 0 && epoch != projection.Epoch {
				cache.invalidate()
				err = storagebudget.ErrStale
			}
		}
		if err == nil {
			out.Snapshot, err = projection.public(storageHexID(e.workspace), view.session, view.sequence, view.observedAt)
			if err == nil {
				measured, failed := o.ownedMeasurement()
				projection.applyOwnedMeasurement(out.Snapshot, measured, failed, time.Now(), genuineRefresh && !view.failed && !failedBefore && measured == measuredBefore, measured.activityContinuous(activityBefore, o.ownedMeasurementActivity()))
			}
		}
		if err == nil {
			out.Availability = "available"
			out.Reason = nil
			if view.stale(time.Now(), out.Snapshot.Maintenance) {
				out.Availability = "stale"
				out.Reason = &reason
			}
		} else {
			out.Snapshot = nil
		}
	}
	// Every cache response is still an original-current read. A failed post-check
	// invalidates counters rather than lending a detached cache new authority.
	if use.Current() != nil || e.statusCurrent(context.WithoutCancel(ctx), s, o) != nil {
		cache.invalidate()
		out.Snapshot = nil
		out.Availability = "unavailable"
		out.Reason = &reason
	}
	if out.Snapshot != nil && out.Snapshot.Measurement.Freshness == "fresh" {
		measured, failed := o.ownedMeasurement()
		if failed || measured != measuredBefore || !measured.activityContinuous(activityBefore, o.ownedMeasurementActivity()) {
			out.Snapshot.Measurement.Freshness = "stale"
		}
	}
	out.ServedAt = storageStatusTime(time.Now())
	return out
}
func (s *Server) writeStorageStatus(w http.ResponseWriter, out storageStatusResponse) {
	out.ServedAt = storageStatusTime(time.Now())
	encoded, err := storageStatusEncode(out, min(storageStatusBytes, storageStatusDerivedBytes))
	if err != nil {
		reason := storageStatusUnknown
		out = storageStatusResponse{Version: 1, Availability: "unavailable", Reason: &reason, ReadOnly: out.ReadOnly, ServedAt: storageStatusTime(time.Now()), ChildObservation: out.ChildObservation, Registration: out.Registration, Pressure: storageStatusPressure{Codes: []string{}, DurableState: "unconfirmed"}}
		encoded, _ = storageStatusEncode(out, storageStatusEnvelopeBytes)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, bytes.NewReader(encoded))
}

// storageRegistrationStatus is the start's registration difference: the
// folders its source inspection measured inside another tree because the
// saved catalog does not register them (storageOriginalSources.unregistered).
// It is fixed for the construction's lifetime and reads no filesystem.
func (s *Server) storageRegistrationStatus() *storageRegistrationStatus {
	c := s.storageConstruction
	if c == nil && s.storageEnrollment != nil {
		c = s.storageEnrollment.construction
	}
	if c == nil || c.sources == nil || len(c.sources.unregistered) == 0 && len(c.sources.identityLater) == 0 {
		return nil
	}
	folders, identities := c.sources.unregistered, c.sources.identityLater
	if len(folders) > storageRegistrationFolders {
		folders = folders[:storageRegistrationFolders]
	}
	if len(identities) > storageRegistrationFolders {
		identities = identities[:storageRegistrationFolders]
	}
	out := &storageRegistrationStatus{Enforcement: "warning-only", Folders: append([]string{}, folders...)}
	if len(identities) != 0 {
		out.Identities = append([]string{}, identities...)
	}
	return out
}

// Resolve scope once while enrolling against original construction sources.
// No status caller may supply a path, identity, or workspace selector.
func (c *storageBudgetConstruction) storageStatusWorkspace(s *Server) (storagebudget.ID, error) {
	if c.sources == nil {
		return storagebudget.ID{}, storagebudget.ErrAuthority
	}
	if c.server.home != nil {
		entry := c.server.home.entry(s.projectDir)
		if entry == nil {
			return storagebudget.ID{}, storagebudget.ErrAuthority
		}
		original, ok := c.sources.projects[entry.ID]
		if !ok || original.entry != *entry {
			return storagebudget.ID{}, storagebudget.ErrAuthority
		}
		id := storageDerivedID("desk-storage-workspace-v2", c.ref.cell.record.Installation, []byte(entry.RunnerWorkspace))
		for _, row := range c.sources.workspaces {
			if row.Definition.ID == id {
				return id, nil
			}
		}
		return storagebudget.ID{}, storagebudget.ErrAuthority
	}
	if s != c.server || len(c.sources.workspaces) != 1 {
		return storagebudget.ID{}, storagebudget.ErrAuthority
	}
	return c.sources.workspaces[0].Definition.ID, nil
}

func (s *Server) invalidateStorageStatusCache() {
	e := s.storageEnrollment
	if e == nil {
		return
	}
	e.mu.Lock()
	var owner *storageBudgetOwner
	if e.self == e && e.server == s && e.construction != nil {
		owner = e.construction.owner
	}
	e.mu.Unlock()
	if owner == nil {
		return
	}
	owner.mu.Lock()
	cache := owner.statusCache
	owner.mu.Unlock()
	if cache != nil {
		cache.invalidate()
	}
}

// This display join has no effect authority. A real Q refresh is necessary for
// freshness; a cached checkpoint plus an idle owner cannot prove no write ran.
func (p storageStatusProjection) applyOwnedMeasurement(out *storageStatusSnapshot, v *storageOwnedMeasurement, failed bool, now time.Time, refreshed, continuous bool) {
	if out == nil || v == nil || v.Completed.IsZero() || len(v.Filesystems) != len(p.Filesystems) || len(v.Roots) != len(p.Roots) || len(v.WorkspaceGenerations) != len(p.Workspaces) {
		return
	}
	for _, row := range p.Roots {
		id, e := storageStatusParseID(row.ID)
		if e != nil {
			return
		}
		fs, e := storageStatusParseID(row.Filesystem)
		if e != nil {
			return
		}
		old, ok := v.Roots[id]
		if !ok || old.Generation != row.Generation || old.Filesystem != fs {
			return
		}
	}
	for _, row := range p.Workspaces {
		id, e := storageStatusParseID(row.ID)
		if e != nil {
			return
		}
		generation, ok := v.WorkspaceGenerations[id]
		if !ok || generation != row.Generation {
			return
		}
	}
	for _, row := range p.Filesystems {
		id, e := storageStatusParseID(row.ID)
		if e != nil {
			return
		}
		if _, ok := v.Filesystems[id]; !ok || v.FilesystemGenerations[id] != row.Generation {
			return
		}
	}
	at := storageStatusTime(v.Completed)
	out.Measurement.At = &at
	out.Measurement.Generation = storageStatusUInt(v.Epoch)
	out.Measurement.Freshness = "stale"
	if p.Maintenance {
		out.Measurement.Freshness = "maintenance"
	} else if refreshed && continuous && !failed && v.LogicalKnown && v.BoundaryKnown && p.Epoch == v.Epoch && p.Revision == v.Revision && p.Active+p.Uncertain+p.Terminal == 0 && !now.Before(v.Completed) && now.Sub(v.Completed) <= storageStatusStaleAfter {
		out.Measurement.Freshness = "fresh"
	}
	out.Measurement.Logical.Unknown = "unavailable"
	if v.LogicalKnown {
		value := storageStatusVectorOf(storageStatusNums(v.Logical))
		out.Measurement.Logical.Value = &value
		out.Measurement.Logical.Unknown = ""
	}
	out.PhysicalAllocated.Unknown = "unavailable"
	if v.AllocationKnown {
		value := storageStatusVectorOf(storageStatusNums(v.Allocation))
		out.PhysicalAllocated.Value = &value
		out.PhysicalAllocated.Unknown = ""
	}
	out.SharedAttribution.State = "unknown"
	if v.SharedKnown {
		value := storageStatusVectorOf(storageStatusNums(v.Shared))
		out.SharedAttribution.Value = &value
		out.SharedAttribution.State = "measured"
	}
	for i, row := range out.Filesystems {
		id, e := storageStatusParseID(row.ID)
		if e != nil {
			continue
		}
		if measurement, ok := v.Filesystems[id]; ok {
			if measurement.AllocationKnown {
				value := storageStatusVectorOf(storageStatusNums(measurement.Allocation))
				out.Filesystems[i].PhysicalAllocated = &value
			}
			if measurement.LogicalKnown {
				value := storageStatusVectorOf(storageStatusNums(measurement.Logical))
				out.Filesystems[i].MeasuredLogical = &value
			}
		}
	}
}

func storageStatusParseID(value string) (storagebudget.ID, error) {
	var id storagebudget.ID
	if !storageStatusID(value) {
		return id, storagebudget.ErrBounds
	}
	raw, e := hex.DecodeString(value)
	if e != nil {
		return id, storagebudget.ErrBounds
	}
	copy(id[:], raw)
	return id, nil
}
