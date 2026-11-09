package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// Closure atoms are detached comparison data assembled from original selected
// P/J/manifest operands. They do not grant a Reserve, native write or recovery.
// Terminal is populated only by the existing complete original retirement/J
// join; its existence is never inferred from a slot or a returned error.
type storagePolicyOwedTerminal struct {
	Plan           storagebudget.ID
	Action         byte
	Token          storagebudget.Token
	Manifest       storagebudget.Digest
	Request        storagebudget.Request
	Terminal       *storagebudget.Ticket
	Uninstantiated bool
}

type storagePolicyClosureInput struct {
	Historical []storagebudget.Ticket
	Current    [64]storagebudget.HostSlotState
	Owed       []storagePolicyOwedTerminal
	Sets       []storagePolicyEffectiveSet
	Dictionary storageOwnerCatalogV2
}

type storagePolicyClosureSize struct{ Tickets, Symbols, TicketBytes, SetBytes, Bytes uint64 }

func storagePolicyTerminalBytes(request storagebudget.Request) (uint64, error) {
	validText := func(s string) bool {
		return len(s) > 0 && len(s) <= 128 && !strings.ContainsAny(s, "\x00\r\n\ufffd")
	}
	if len(request.Components) == 0 || len(request.Components) > 16 || !validText(request.Workflow) || !validText(request.Operation) || request.Owner == (storagebudget.Digest{}) {
		return 0, storagebudget.ErrBounds
	}
	return uint64(148 + len(request.Workflow) + len(request.Operation) + 490*len(request.Components)), nil
}

func storagePolicyAlignedTicket(n uint64) uint64 { return (36 + n + 7) &^ uint64(7) }

// The arithmetic is monotone through mandatory realization: factual retained
// variants stay additive, and only an exact original application/Q alias can
// share one still-owed terminal atom. No synthetic successful ticket is encoded.
func storagePolicyObligationClosure(in storagePolicyClosureInput) (storagePolicyClosureSize, error) {
	var out storagePolicyClosureSize
	factual := map[string]bool{}
	add := func(t storagebudget.Ticket) error {
		body, err := storageEnrollmentPeakWriteTicket(t)
		if err != nil {
			return err
		}
		if !factual[string(body)] {
			factual[string(body)] = true
			out.Tickets++
			out.TicketBytes += storagePolicyAlignedTicket(uint64(len(body)))
		}
		return nil
	}
	for _, t := range in.Historical {
		if err := add(t); err != nil {
			return out, err
		}
	}
	// Every actual retained variant is supplied in Historical, including any
	// earlier live variant. Current live full-manifest fanout values are not
	// ticket-page operands; their allowed eventual terminal is charged below.
	for _, row := range in.Current {
		if row.Ticket.State == storagebudget.Empty || row.Ticket.State == storagebudget.Terminal {
			if err := add(row.Ticket); err != nil {
				return out, err
			}
		}
	}
	alias := [64]bool{}
	seenActions := map[struct {
		Plan   storagebudget.ID
		Action byte
	}]bool{}
	for _, atom := range in.Owed {
		key := struct {
			Plan   storagebudget.ID
			Action byte
		}{atom.Plan, atom.Action}
		if atom.Plan == (storagebudget.ID{}) || atom.Action == 0 || seenActions[key] {
			return out, storagebudget.ErrAuthority
		}
		seenActions[key] = true
		if atom.Uninstantiated {
			if atom.Token != (storagebudget.Token{}) || atom.Terminal != nil || len(atom.Request.Components) != 0 {
				return out, storagebudget.ErrAuthority
			}
			out.Symbols++
			out.TicketBytes += storagePolicyAlignedTicket(8244)
			continue
		}
		n, err := storagePolicyTerminalBytes(atom.Request)
		if err != nil {
			return out, err
		}
		if atom.Terminal != nil {
			t := *atom.Terminal
			if t.State != storagebudget.Terminal || t.Token != atom.Token || t.Manifest != atom.Manifest || t.Owner != atom.Request.Owner || t.Workflow != atom.Request.Workflow || t.Operation != atom.Request.Operation || !reflect.DeepEqual(t.Components, atom.Request.Components) || (t.Fanout != nil) != (atom.Request.Fanout != nil) {
				return out, storagebudget.ErrAuthority
			}
			if err = add(t); err != nil {
				return out, err
			}
			continue
		}
		if atom.Token != (storagebudget.Token{}) {
			if int(atom.Token.Slot) >= len(in.Current) || alias[atom.Token.Slot] {
				return out, storagebudget.ErrAuthority
			}
			t := in.Current[atom.Token.Slot].Ticket
			if t.Token != atom.Token || t.Manifest != atom.Manifest || t.Owner != atom.Request.Owner || t.Workflow != atom.Request.Workflow || t.Operation != atom.Request.Operation || !reflect.DeepEqual(t.Components, atom.Request.Components) || !reflect.DeepEqual(t.Fanout, atom.Request.Fanout) {
				return out, storagebudget.ErrAuthority
			}
			alias[atom.Token.Slot] = true
			if t.State == storagebudget.Terminal {
				continue
			}
			if t.State == storagebudget.Empty {
				return out, storagebudget.ErrAuthority
			}
		}
		out.Symbols++
		out.TicketBytes += storagePolicyAlignedTicket(n)
	}
	for i, row := range in.Current {
		t := row.Ticket
		if alias[i] || t.State == storagebudget.Empty || t.State == storagebudget.Terminal {
			continue
		}
		n, err := storagePolicyTerminalBytes(storagebudget.Request{Workflow: t.Workflow, Operation: t.Operation, Owner: t.Owner, Components: t.Components, Fanout: t.Fanout})
		if err != nil {
			return out, err
		}
		out.Symbols++
		out.TicketBytes += storagePolicyAlignedTicket(n)
	}
	// Empty ticket payload isolates exact set framing/pool size without charging
	// historical/current tickets twice. This builder performs all strict ID/gen,
	// policy, canonical set and full-value validation under the original dictionary.
	setRaw, _, err := storagePolicyBuildEffectiveTable(nil, in.Sets, in.Dictionary)
	if err != nil {
		return out, err
	}
	out.SetBytes = uint64(len(setRaw))
	out.Bytes = out.TicketBytes + out.SetBytes
	if out.Tickets+out.Symbols > 4224 || out.Bytes > 35143680 {
		return out, storagebudget.ErrBounds
	}
	return out, nil
}

// Size only: maximum-width fields and zero byte strings below are arithmetic
// operands, never encoded as a factual source or successful native result.
// Exact factual decoding remains mandatory before the first admitted write.
func storageOwnerV6PackedClosureSize(c storageOwnerV6PackedCatalog, source storageOwnerCatalogV2, lineage storageCompositionLineage) (int, error) {
	s, actualLineage, err := storageOwnerDecodePackedSource(c.Source)
	if err != nil {
		return 0, err
	}
	a, err := actualLineage.encode()
	if err != nil {
		return 0, err
	}
	b, err := lineage.encode()
	if err != nil || !bytes.Equal(a, b) {
		return 0, storagebudget.ErrBounds
	}
	room, err := lineage.closureSize()
	if err != nil {
		return 0, err
	}
	plans, from := len(lineage.Rows.LineagePlans), 0
	for _, p := range lineage.Rows.LineagePlans {
		if len(p.IdentityFromCleared) > 0 {
			from++
		}
	}
	// P/C/S/F append-order bytes and every original Initial are retained. Only
	// missing receipt rows are widened by the existing closed source shadows.
	futureSuffix := 8 + 36*plans + 16*(2*plans+2*from)
	futureRows := room - 4 - futureSuffix
	if futureRows < 0 || futureSuffix > 102408 {
		return 0, storagebudget.ErrBounds
	}
	b64 := func(n int) int { return 4 * ((n + 2) / 3) }
	futureSource := len(c.Source) - len(s.LineageRows) - b64(len(s.LineageSuffix)) + futureRows + b64(futureSuffix)
	if len(s.Core) < 8320 {
		futureSource += b64(8320) - b64(len(s.Core))
	}
	shadow := c
	shadow.Sequence = math.MaxUint64
	shadow.Phase = "aborted-unfenced"
	shadow.Transition = storageOwnerV5MaxID()
	shadow.PreviousCatalogDigest = storageOwnerV5MaxDigest()
	shadow.BeforeEffectiveDigest = storageOwnerV5MaxDigest()
	shadow.AfterEffectiveDigest = storageOwnerV5MaxDigest()
	maximum := storagebudget.Limits{Total: storagebudget.Vector{Bytes: 1 << 60, Files: 1 << 40, Directories: 1 << 40}}
	for i := range maximum.Category {
		maximum.Category[i] = maximum.Total
	}
	shadow.EffectivePolicy.Installation = maximum
	shadow.EffectivePolicy.Workspace = maximum
	if len(source.Workspaces) > 1024 {
		return 0, storagebudget.ErrBounds
	}
	shadow.Overrides = make([]byte, 2+264*len(source.Workspaces))
	shadow.PolicyTransition = make([]byte, 2048)
	raw, err := json.Marshal(shadow)
	if err != nil {
		return 0, err
	}
	n := len(raw) - len(c.Source) + futureSource
	if n < 0 || n > storageOwnerCatalogBytes-64 {
		return 0, storagebudget.ErrBounds
	}
	return n, nil
}

func storageOwnerV5MaxDigest() (d storagebudget.Digest) {
	for i := range d {
		d[i] = 255
	}
	return
}
func storageOwnerV5MaxID() (d storagebudget.ID) {
	for i := range d {
		d[i] = 255
	}
	return
}
func storageOwnerV5MaxObject(kind uint8) storageOwnerPersistentObject {
	return storageOwnerPersistentObject{Backend: 2, VolumeUUID: [16]byte(storageOwnerV5MaxID()), Object: math.MaxUint64, Incarnation: math.MaxUint64, IncarnationKind: 2, ObjectKind: kind}
}
func storageOwnerV5MaxReceipt(kind uint8) storageOwnerV5Receipt {
	token := storagebudget.Token{Slot: storagebudget.Slots - 1, ID: storageOwnerV5MaxID(), Sequence: math.MaxUint64 - 1, Generation: math.MaxUint64}
	bytesDigest := storageOwnerV5MaxDigest()
	if kind == 1 {
		bytesDigest = storagebudget.Digest{}
	}
	return storageOwnerV5Receipt{storageOwnerV5MaxID(), token, token, math.MaxUint64, math.MaxUint64, storageOwnerV5MaxDigest(), storageOwnerV5MaxDigest(), storageOwnerV5MaxObject(kind), bytesDigest, storageOwnerV5MaxDigest()}
}
func storageOwnerV5CompletionShadow(p storageOwnerV5LineagePlan) storageOwnerV5LineageCompletion {
	return storageOwnerV5LineageCompletion{p.PlanID, storageOwnerV5MaxDigest(), storageOwnerV5MaxReceipt(2)}
}
func storageOwnerV5ContinuationShadow(p storageOwnerV5LineagePlan) storageOwnerV5FromContinuation {
	// A moved outcome has two maximum-width directory tuples and a full stamp
	// receipt. Both-present has four tuples but no stamp receipt; choose by actual
	// canonical encoded size, avoiding a guessed constant for either branch.
	source, target := storageOwnerV5MaxObject(1), storageOwnerV5MaxObject(1)
	target.Object--
	moved := storageOwnerV5FromContinuation{PlanID: p.PlanID, PlanDigest: storageOwnerV5MaxDigest(), StampDisposition: storageOwnerV5Moved, StampSourceBefore: source, StampTargetAfter: source, StampReceipt: storageOwnerV5MaxReceipt(1), Rewrite: storageOwnerV5MaxReceipt(2)}
	kept := storageOwnerV5FromContinuation{PlanID: p.PlanID, PlanDigest: storageOwnerV5MaxDigest(), StampDisposition: storageOwnerV5BothPresent, StampSourceBefore: source, StampTargetBefore: target, StampSourceAfter: source, StampTargetAfter: target, Rewrite: storageOwnerV5MaxReceipt(2)}
	a, _ := json.Marshal(moved)
	b, _ := json.Marshal(kept)
	if len(b) > len(a) {
		return kept
	}
	return moved
}

func storageOwnerV5StampShadow(p storageOwnerV5LineagePlan) storageOwnerV5StampCompletionRow {
	return storageOwnerV5ContinuationShadow(p).stamp()
}

// Rooms use final declaration order and maximal-width token/native/digest
// annotations before PlanDigest is computed; the fixed widths break the cycle.
// Return order is C,S,F. No-From plans pledge only C.
func storageOwnerV5Rooms(p storageOwnerV5LineagePlan) (uint32, uint32, uint32, error) {
	c, e := json.Marshal(storageOwnerV5CompletionShadow(p))
	if e != nil || len(c) > storageOwnerV5CompletionBytes {
		return 0, 0, 0, storagebudget.ErrBounds
	}
	sr, fr := 0, 0
	if len(p.IdentityFromCleared) > 0 {
		s, e := json.Marshal(storageOwnerV5StampShadow(p))
		if e != nil || len(s) > storageOwnerV5StampBytes {
			return 0, 0, 0, storagebudget.ErrBounds
		}
		sr = len(s)
		f, e := json.Marshal(storageOwnerV5ContinuationShadow(p))
		if e != nil || len(f) > storageOwnerV5ContinuationBytes {
			return 0, 0, 0, storagebudget.ErrBounds
		}
		fr = len(f)
	}
	return uint32(len(c)), uint32(sr), uint32(fr), nil
}

// ClosureSize reserves all missing C/S/F rows at once, with the full retained old
// projection and the widest future envelope. An unrelated transition cannot
// spend pledged room. Existing factual rows are retained exactly in this copy.
func storageOwnerV5ClosureTree(c storageOwnerV5Catalog) (storageOwnerV5Catalog, error) {
	if !c.old().bounded() || !storageOwnerV5RowChain(c) {
		return storageOwnerV5Catalog{}, storagebudget.ErrBounds
	}
	shadow := c
	shadow.LineageCompletions = append([]storageOwnerV5LineageCompletion{}, c.LineageCompletions...)
	shadow.LineageStampCompletions = append([]storageOwnerV5StampCompletionRow{}, c.LineageStampCompletions...)
	shadow.LineageContinuations = append([]storageOwnerV5FromContinuation{}, c.LineageContinuations...)
	completed, stamped, continued := map[storagebudget.ID]bool{}, map[storagebudget.ID]bool{}, map[storagebudget.ID]bool{}
	for _, row := range c.LineageCompletions {
		completed[row.PlanID] = true
	}
	for _, row := range c.LineageStampCompletions {
		stamped[row.PlanID] = true
	}
	for _, row := range c.LineageContinuations {
		continued[row.PlanID] = true
	}
	for _, p := range c.LineagePlans {
		cr, sr, fr, err := storageOwnerV5Rooms(p)
		if err != nil || p.CompletionRoom != cr || p.ContinuationRoom != fr || p.StampRoom != sr {
			return storageOwnerV5Catalog{}, storagebudget.ErrBounds
		}
		if !completed[p.PlanID] {
			shadow.LineageCompletions = append(shadow.LineageCompletions, storageOwnerV5CompletionShadow(p))
		}
		if len(p.IdentityFromCleared) > 0 && !stamped[p.PlanID] {
			shadow.LineageStampCompletions = append(shadow.LineageStampCompletions, storageOwnerV5StampShadow(p))
		}
		if len(p.IdentityFromCleared) > 0 && !continued[p.PlanID] {
			shadow.LineageContinuations = append(shadow.LineageContinuations, storageOwnerV5ContinuationShadow(p))
		}
	}
	sort.Slice(shadow.LineageCompletions, func(i, j int) bool {
		return bytes.Compare(shadow.LineageCompletions[i].PlanID[:], shadow.LineageCompletions[j].PlanID[:]) < 0
	})
	sort.Slice(shadow.LineageStampCompletions, func(i, j int) bool {
		return bytes.Compare(shadow.LineageStampCompletions[i].PlanID[:], shadow.LineageStampCompletions[j].PlanID[:]) < 0
	})
	sort.Slice(shadow.LineageContinuations, func(i, j int) bool {
		return bytes.Compare(shadow.LineageContinuations[i].PlanID[:], shadow.LineageContinuations[j].PlanID[:]) < 0
	})
	// Future receipt publication can follow another enrollment. Its Before IDs
	// then name every currently retained definition, including rows not in the
	// current enrollment's older Before projection.
	shadow.BeforeDefinitionIDs = storageOwnerDefinitionIDs{[]storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}}
	for _, row := range c.Roots {
		shadow.BeforeDefinitionIDs.Roots = append(shadow.BeforeDefinitionIDs.Roots, row.Definition.ID)
	}
	for _, row := range c.Workspaces {
		shadow.BeforeDefinitionIDs.Workspaces = append(shadow.BeforeDefinitionIDs.Workspaces, row.Definition.ID)
	}
	for _, row := range c.Filesystems {
		shadow.BeforeDefinitionIDs.Filesystems = append(shadow.BeforeDefinitionIDs.Filesystems, row.Definition.ID)
	}
	for _, row := range c.Constraints {
		shadow.BeforeDefinitionIDs.Constraints = append(shadow.BeforeDefinitionIDs.Constraints, row.Definition.ID)
	}
	shadow.Sequence = math.MaxUint64
	shadow.Phase = "prepared"
	shadow.Transition = storageOwnerV5MaxID()
	shadow.TransitionPlan = storageOwnerV5MaxID()
	shadow.TransitionKind = storageOwnerV5StampCompletion
	shadow.PreviousCatalogDigest = storageOwnerV5MaxDigest()
	shadow.BeforeDefinitionsDigest = storageOwnerV5MaxDigest()
	shadow.AfterDefinitionsDigest = storageOwnerV5MaxDigest()
	shadow.ExpectedQRevision = math.MaxUint64
	if !storageOwnerV5RowBounds(shadow) {
		return storageOwnerV5Catalog{}, storagebudget.ErrBounds
	}
	return shadow, nil
}

func storageOwnerV5ClosureSize(c storageOwnerV5Catalog) (int, error) {
	shadow, err := storageOwnerV5ClosureTree(c)
	if err != nil {
		return 0, err
	}
	b, err := json.Marshal(shadow)
	if err != nil || len(b) > storageOwnerCatalogBytes-64 {
		return 0, storagebudget.ErrBounds
	}
	// Exercise the exact scanner on the complete maximum tree too. It preserves
	// all existing source/list bounds; no size estimate bypasses preallocation.
	if !storageOwnerV5Preflight(b) {
		return 0, storagebudget.ErrBounds
	}
	return len(b), nil
}

// V6ClosureSize sizes the actual closed future tree, not an allowance guessed
// from current sparse rows. Every registered workspace has room for a full
// maximum-width current override, in addition to all missing pledged receipts.
// No shadow is a factual receipt or an authority, and no source Limit is edited.
func storageOwnerV6ClosureTree(c storageOwnerV6Catalog) (storageOwnerV6Catalog, error) {
	shadow := c
	var source storageOwnerCatalogV2
	switch {
	case bytes.HasPrefix(c.Source, []byte(`{"Version":3,`)):
		if !storageOwnerCatalogPreflight(c.Source) || json.Unmarshal(c.Source, &source) != nil || !source.bounded() {
			return storageOwnerV6Catalog{}, storagebudget.ErrBounds
		}
		future := source
		future.BeforeDefinitionIDs = storageOwnerDefinitionIDs{[]storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}}
		for _, v := range source.Roots {
			future.BeforeDefinitionIDs.Roots = append(future.BeforeDefinitionIDs.Roots, v.Definition.ID)
		}
		for _, v := range source.Workspaces {
			future.BeforeDefinitionIDs.Workspaces = append(future.BeforeDefinitionIDs.Workspaces, v.Definition.ID)
		}
		for _, v := range source.Filesystems {
			future.BeforeDefinitionIDs.Filesystems = append(future.BeforeDefinitionIDs.Filesystems, v.Definition.ID)
		}
		for _, v := range source.Constraints {
			future.BeforeDefinitionIDs.Constraints = append(future.BeforeDefinitionIDs.Constraints, v.Definition.ID)
		}
		future.Sequence = math.MaxUint64
		future.Phase = "prepared"
		future.Transition = storageOwnerV5MaxID()
		future.PreviousCatalogDigest = storageOwnerV5MaxDigest()
		future.BeforeDefinitionsDigest = storageOwnerV5MaxDigest()
		future.AfterDefinitionsDigest = storageOwnerV5MaxDigest()
		future.ExpectedQRevision = math.MaxUint64
		b, err := json.Marshal(future)
		if err != nil || !storageOwnerCatalogPreflight(b) {
			return storageOwnerV6Catalog{}, storagebudget.ErrBounds
		}
		shadow.Source = b
	case bytes.HasPrefix(c.Source, []byte(`{"Version":4,`)):
		var old storageOwnerV4Catalog
		if !storageOwnerV4Preflight(c.Source) || json.Unmarshal(c.Source, &old) != nil {
			return storageOwnerV6Catalog{}, storagebudget.ErrBounds
		}
		source = old.old()
		future, err := storageOwnerV6V4ClosureTree(old)
		if err != nil {
			return storageOwnerV6Catalog{}, err
		}
		b, err := json.Marshal(future)
		if err != nil || !storageOwnerV4Preflight(b) {
			return storageOwnerV6Catalog{}, storagebudget.ErrBounds
		}
		shadow.Source = b
	case bytes.HasPrefix(c.Source, []byte(`{"Version":5,`)):
		var old storageOwnerV5Catalog
		if !storageOwnerV5Preflight(c.Source) || json.Unmarshal(c.Source, &old) != nil {
			return storageOwnerV6Catalog{}, storagebudget.ErrBounds
		}
		source = old.old()
		future, err := storageOwnerV5ClosureTree(old)
		if err != nil {
			return storageOwnerV6Catalog{}, err
		}
		b, err := json.Marshal(future)
		if err != nil || !storageOwnerV5Preflight(b) {
			return storageOwnerV6Catalog{}, storagebudget.ErrBounds
		}
		shadow.Source = b
	default:
		return storageOwnerV6Catalog{}, storagebudget.ErrBounds
	}
	maxLimits := storagebudget.Limits{Total: storagebudget.Vector{Bytes: 1 << 60, Files: 1 << 40, Directories: 1 << 40}}
	for i := range maxLimits.Category {
		maxLimits.Category[i] = maxLimits.Total
	}
	shadow.EffectivePolicy.Installation = maxLimits
	shadow.EffectivePolicy.Workspace = maxLimits
	shadow.Overrides = make([]storageOwnerV6Override, 0, len(source.Workspaces))
	for _, w := range source.Workspaces {
		shadow.Overrides = append(shadow.Overrides, storageOwnerV6Override{w.Definition.ID, w.Definition.Generation, maxLimits})
	}
	sort.Slice(shadow.Overrides, func(i, j int) bool { return bytes.Compare(shadow.Overrides[i].ID[:], shadow.Overrides[j].ID[:]) < 0 })
	shadow.TransitionKind = storageOwnerV6PolicyTransition
	shadow.Version = 6
	shadow.Sequence = math.MaxUint64
	shadow.Phase = "aborted-unfenced"
	shadow.Transition = storageOwnerV5MaxID()
	shadow.PreviousCatalogDigest = storageOwnerV5MaxDigest()
	shadow.BeforeEffectiveDigest = storageOwnerV5MaxDigest()
	shadow.AfterEffectiveDigest = storageOwnerV5MaxDigest()
	// The inverse operand is always exactly 2048 bytes, regardless of values.
	shadow.PolicyTransition = make([]byte, storagebudget.PolicyTransitionBytes)
	return shadow, nil
}

func storageOwnerV6ClosureSize(c storageOwnerV6Catalog) (int, error) {
	shadow, err := storageOwnerV6ClosureTree(c)
	if err != nil {
		return 0, err
	}
	b, err := json.Marshal(shadow)
	if err != nil || len(b) > storageOwnerCatalogBytes-64 || !storageOwnerV6Preflight(b) {
		return 0, storagebudget.ErrBounds
	}
	// Check fixed envelope widths too. The exact factual Prepared/final values
	// must each pass the full codec before the host dispatches its first write.
	// The complete maximum tree also covers the inverse policy/override widths.
	phases := []string{"prepared", "active"}
	if c.TransitionKind == storageOwnerV6PolicyTransition {
		phases = append(phases, "aborted", "aborted-unfenced")
	}
	for _, phase := range phases {
		actual := c
		actual.Phase = phase
		raw, err := json.Marshal(actual)
		if err != nil || len(raw) > storageOwnerCatalogBytes-64 || !storageOwnerV6Preflight(raw) {
			return 0, storagebudget.ErrBounds
		}
		if len(raw) > len(b) {
			return 0, storagebudget.ErrBounds
		}
	}
	return len(b), nil
}

// V6 keeps the unmodified, unpinned V4 codec implementation outside this new
// pinned implementation path. This sizing copy uses its original
// exact C/F shadows, room checks, order and widest source envelope.
func storageOwnerV6V4ClosureTree(c storageOwnerV4Catalog) (storageOwnerV4Catalog, error) {
	if !c.old().bounded() || !storageOwnerV4RowBounds(c) {
		return storageOwnerV4Catalog{}, storagebudget.ErrBounds
	}
	shadow := c
	shadow.LineageCompletions = append([]storageOwnerV4LineageCompletion{}, c.LineageCompletions...)
	shadow.LineageContinuations = append([]storageOwnerV4FromContinuation{}, c.LineageContinuations...)
	completed, continued := map[storagebudget.ID]bool{}, map[storagebudget.ID]bool{}
	for _, row := range c.LineageCompletions {
		completed[row.PlanID] = true
	}
	for _, row := range c.LineageContinuations {
		continued[row.PlanID] = true
	}
	for _, p := range c.LineagePlans {
		cr, fr, err := storageOwnerV4Rooms(p)
		if err != nil || p.CompletionRoom != cr || p.ContinuationRoom != fr {
			return storageOwnerV4Catalog{}, storagebudget.ErrBounds
		}
		if !completed[p.PlanID] {
			shadow.LineageCompletions = append(shadow.LineageCompletions, storageOwnerV4CompletionShadow(p))
		}
		if len(p.IdentityFromCleared) > 0 && !continued[p.PlanID] {
			shadow.LineageContinuations = append(shadow.LineageContinuations, storageOwnerV4ContinuationShadow(p))
		}
	}
	sort.Slice(shadow.LineageCompletions, func(i, j int) bool {
		return bytes.Compare(shadow.LineageCompletions[i].PlanID[:], shadow.LineageCompletions[j].PlanID[:]) < 0
	})
	sort.Slice(shadow.LineageContinuations, func(i, j int) bool {
		return bytes.Compare(shadow.LineageContinuations[i].PlanID[:], shadow.LineageContinuations[j].PlanID[:]) < 0
	})
	// Future receipt publication can follow another enrollment. Its Before IDs
	// then name every currently retained definition, including rows not in the
	// current enrollment's older Before projection.
	shadow.BeforeDefinitionIDs = storageOwnerDefinitionIDs{[]storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}, []storagebudget.ID{}}
	for _, row := range c.Roots {
		shadow.BeforeDefinitionIDs.Roots = append(shadow.BeforeDefinitionIDs.Roots, row.Definition.ID)
	}
	for _, row := range c.Workspaces {
		shadow.BeforeDefinitionIDs.Workspaces = append(shadow.BeforeDefinitionIDs.Workspaces, row.Definition.ID)
	}
	for _, row := range c.Filesystems {
		shadow.BeforeDefinitionIDs.Filesystems = append(shadow.BeforeDefinitionIDs.Filesystems, row.Definition.ID)
	}
	for _, row := range c.Constraints {
		shadow.BeforeDefinitionIDs.Constraints = append(shadow.BeforeDefinitionIDs.Constraints, row.Definition.ID)
	}
	shadow.Sequence = math.MaxUint64
	shadow.Phase = "prepared"
	shadow.Transition = storageOwnerV4MaxID()
	shadow.TransitionPlan = storageOwnerV4MaxID()
	shadow.TransitionKind = storageOwnerV4BridgePlan
	shadow.PreviousCatalogDigest = storageOwnerV4MaxDigest()
	shadow.BeforeDefinitionsDigest = storageOwnerV4MaxDigest()
	shadow.AfterDefinitionsDigest = storageOwnerV4MaxDigest()
	shadow.ExpectedQRevision = math.MaxUint64
	if !storageOwnerV4RowBounds(shadow) {
		return storageOwnerV4Catalog{}, storagebudget.ErrBounds
	}
	return shadow, nil
}

// Native closure is evaluated BEFORE a new policy/source/P/Q admission, while
// the outer original exclusion and atomic Q comparison still hold. Existing
// mandatory C/S/F and R1 groups consume this room; their encoders perform only
// factual framing, never a new future-capacity admission after an effect.
//
// The core maximum is the complete fixed envelope + Snapshot + exact compact
// completed E1 + the largest accepted successor-intent source. It includes the
// first independently decodable Fence; no future evidence bytes are assumed.
// The immediate recipe has no lineage/core/effective-set copy. Current sparse
// values are binary fixed-width; new baseline rows add no override by inference.
type storageOwnerV6NativeClosure struct {
	Current, Future, Lineage, Core, Recipe, Sparse, MoveOperands int
}

func storageOwnerV6NativeClosureSize(v storageOwnerV6NativeBank) (storageOwnerV6NativeClosure, error) {
	var out storageOwnerV6NativeClosure
	var err error
	v, err = storageMoveNativeOperands(v)
	if err != nil {
		return out, err
	}
	_, _, moveBytes, err := storageMoveBankOperands(v.Stage)
	if err != nil {
		return out, err
	}
	out.MoveOperands = len(moveBytes)
	core, err := storageCompositionCompactCore(v.Stage.Core)
	if err != nil {
		return out, err
	}
	lineage, err := v.Stage.Lineage.encode()
	if err != nil {
		return out, err
	}
	var recipe []byte
	if v.Before != nil {
		recipe, err = v.Before.encode()
		if err != nil {
			return out, err
		}
	}
	out.Current = 64 + storageOwnerV6NativePrefix + len(core) + len(lineage) + len(v.Overrides) + len(recipe) + out.MoveOperands
	out.Lineage, err = v.Stage.Lineage.closureSize()
	if err != nil {
		return out, err
	}
	out.Core = 64 + 4160 + 256 + 8320 + 96 + storageEnrollmentPeakSourceMaximum
	out.Recipe = storageOwnerV6BeforeMax
	out.Sparse = len(v.Overrides)
	// Missing receipt rows and chronological Order/Initial are widened by the
	// existing exact P/C/S/F closure. Effective values cannot change as a tail
	// outcome: a different current policy/member set needs separate admission.
	out.Future = 64 + storageOwnerV6NativePrefix + out.Core + out.Lineage + out.Sparse + out.Recipe + out.MoveOperands
	if out.Current > storageOwnerCatalogBytes || out.Future > storageOwnerCatalogBytes {
		return out, storagebudget.ErrBounds
	}
	return out, nil
}

// This read-only comparison stays inside the caller's original source/use
// exclusion and the existing host-control gate. It is never a Q callback.
// The returned DTO is consumed by exactly one ReserveCompared; it grants no
// source, journal, native or retry permission.
func (o *storageBudgetOwner) captureOriginalReserve(ctx context.Context, request storagebudget.Request, table *storageIntentTable, operation storagebudget.ID, identity *storageIdentityIntent) (storagebudget.ReserveComparison, error) {
	return o.captureOriginalReserveOriginal(ctx, request, table, operation, identity, nil)
}

// original is invocation-local under the registered move and bootstrap EX;
// its two physical images bracket this complete J/Q/closure comparison.
func (o *storageBudgetOwner) captureOriginalReserveOriginal(ctx context.Context, request storagebudget.Request, table *storageIntentTable, operation storagebudget.ID, identity *storageIdentityIntent, original *storagePolicyOriginal) (storagebudget.ReserveComparison, error) {
	var comparison storagebudget.ReserveComparison
	if !o.valid() || operation == (storagebudget.ID{}) {
		return comparison, storagebudget.ErrAuthority
	}
	if table == nil {
		if original != nil {
			return comparison, storagebudget.ErrAuthority
		}
		err := o.withControl(ctx, func(ctx context.Context, original *storageIntentTable) error {
			var err error
			comparison, err = o.captureOriginalReserve(ctx, request, original, operation, identity)
			return err
		})
		return comparison, err
	}
	if table.owner != o {
		return comparison, storagebudget.ErrAuthority
	}
	comparison, inspected, err := o.ref.cell.q.CaptureReserve(ctx, request)
	if err != nil {
		return storagebudget.ReserveComparison{}, err
	}
	active, err := o.policyReserveClosureActive(ctx)
	if err != nil {
		return storagebudget.ReserveComparison{}, err
	}
	// Old-format promises remain under their original admitted closure. The
	// first policy activation admits the complete new shadow before any write;
	// only thereafter may an unrelated Reserve spend its unpledged capacity.
	if !active {
		return comparison, ctx.Err()
	}
	var state storagePolicyOriginal
	if original == nil {
		state, err = o.readOriginalPolicy(ctx)
		if err != nil {
			return comparison, err
		}
	} else {
		state = *original
	}
	if err = storagePolicyCompletedQ(state, inspected); err != nil {
		return comparison, err
	}
	input, err := o.originalPolicyClosureInputOriginal(ctx, inspected, table, operation, request, identity, original)
	if err != nil {
		return storagebudget.ReserveComparison{}, err
	}
	if _, err = storagePolicyObligationClosure(input); err != nil {
		return storagebudget.ReserveComparison{}, err
	}
	return comparison, ctx.Err()
}

func (o *storageBudgetOwner) policyReserveClosureActive(ctx context.Context) (bool, error) {
	if !o.valid() {
		return false, storagebudget.ErrAuthority
	}
	if o.ref.cell.ownerRecord == nil {
		return false, ctx.Err()
	}
	active := false
	for _, name := range []string{"storage-owner.a", "storage-owner.b"} {
		n, err := storageOpenOriginalControl(o.ref.cell, name)
		if err != nil {
			return false, err
		}
		var header [8]byte
		count, err := n.file.ReadAt(header[:], 0)
		err = errors.Join(err, n.current(), n.file.Close())
		if err != nil || count != len(header) {
			return false, errors.Join(errStorageCustody, err)
		}
		active = active || header == storageOwnerV6Magic
	}
	return active, ctx.Err()
}

// Pure shadow data is assembled only while the caller owns source/bootstrap
// exclusion. The inspected Q and every J record are actual original operands;
// this function never writes, admits, checkpoints or synthesizes a terminal.
func (o *storageBudgetOwner) originalPolicyClosureInput(ctx context.Context, inspected storagebudget.EnrollmentInspection, table *storageIntentTable, operation storagebudget.ID, request storagebudget.Request, identity *storageIdentityIntent) (storagePolicyClosureInput, error) {
	return o.originalPolicyClosureInputOriginal(ctx, inspected, table, operation, request, identity, nil)
}

func (o *storageBudgetOwner) originalPolicyClosureInputOriginal(ctx context.Context, inspected storagebudget.EnrollmentInspection, table *storageIntentTable, operation storagebudget.ID, request storagebudget.Request, identity *storageIdentityIntent, original *storagePolicyOriginal) (storagePolicyClosureInput, error) {
	var err error
	status := storageHostState(inspected.View)
	if err = o.validateIntentMappingsOriginal(ctx, table, status, original); err != nil {
		return storagePolicyClosureInput{}, err
	}
	var lineage storageCompositionLineage
	if original == nil {
		lineage, err = o.originalPolicyLineage(ctx)
		if err != nil {
			return storagePolicyClosureInput{}, err
		}
	} else {
		if original.RecoveryOnly {
			return storagePolicyClosureInput{}, errStorageEnrollmentUnresolved
		}
		lineage = original.Lineage
	}
	input := storagePolicyClosureInput{Current: inspected.View.Slots, Dictionary: o.ref.cell.ownerCatalog}
	if original := o.ref.cell.enrollmentEndpoint; original != nil {
		input.Historical = append([]storagebudget.Ticket{}, original.Tickets...)
		if original.Version == 3 {
			input.Sets = append([]storagePolicyEffectiveSet{}, original.EffectiveSets...)
		} else {
			needed := map[uint16]bool{original.Checkpoint.View: true}
			for _, origin := range original.Board.Origins {
				needed[origin.View] = true
			}
			for i, view := range original.Views {
				if !needed[uint16(i)] {
					continue
				}
				q, err := storageEnrollmentPeakViewCheckpoint(view, original.Inner.Catalog)
				if err != nil {
					return storagePolicyClosureInput{}, err
				}
				set := storagePolicyEffectiveSet{Policy: view.Policy, Workspaces: []storagebudget.RegisteredWorkspace{}}
				for _, row := range q.Workspaces {
					if row.ID == (storageEnrollmentPeakQID{}) {
						continue
					}
					var limits storagebudget.Limits
					storageEnrollmentPeakCopy(reflect.ValueOf(&limits).Elem(), reflect.ValueOf(row.Limits))
					set.Workspaces = append(set.Workspaces, storagebudget.RegisteredWorkspace{ID: storagebudget.ID(row.ID), Generation: row.Generation, Limits: limits})
				}
				sort.Slice(set.Workspaces, func(i, j int) bool { return bytes.Compare(set.Workspaces[i].ID[:], set.Workspaces[j].ID[:]) < 0 })
				input.Sets = append(input.Sets, set)
			}
		}
	}
	current := storageOwnerDefinitionSnapshot(inspected.View)
	input.Sets = append(input.Sets, storagePolicyEffectiveSet{Policy: current.Policy, Workspaces: current.Workspaces})
	// Exact plan/action pairs name only missing, already-pledged applications.
	// A live J/token can replace its own widest atom, never a different one.
	type actionKey struct {
		plan   storagebudget.ID
		action byte
	}
	owed := map[actionKey]storagePolicyOwedTerminal{}
	for _, plan := range lineage.Rows.LineagePlans {
		var initial storageCompositionInitial
		for i, id := range lineage.Order[0] {
			if id == plan.PlanID {
				initial = lineage.Initial[i]
			}
		}
		actions, err := storageIdentityRequiredActions(plan, initial)
		if err != nil {
			return storagePolicyClosureInput{}, err
		}
		for _, action := range actions {
			complete := false
			switch action {
			case storageIdentityIntentCreate, storageIdentityIntentInitialCAS:
				for _, row := range lineage.Rows.LineageCompletions {
					complete = complete || row.PlanID == plan.PlanID
				}
			case storageIdentityIntentStamp:
				for _, row := range lineage.Rows.LineageStampCompletions {
					complete = complete || row.PlanID == plan.PlanID
				}
			case storageIdentityIntentClear:
				for _, row := range lineage.Rows.LineageContinuations {
					complete = complete || row.PlanID == plan.PlanID
				}
			}
			if !complete {
				owed[actionKey{plan.PlanID, action}] = storagePolicyOwedTerminal{Plan: plan.PlanID, Action: action, Uninstantiated: true}
			}
		}
	}
	identityKey := func(r storageIdentityIntent) (actionKey, error) {
		var out actionKey
		for _, plan := range lineage.Rows.LineagePlans {
			digest, err := storageOwnerV5PlanDigest(plan)
			if err != nil {
				return out, err
			}
			if digest == r.PlanDigest {
				if out.plan != (storagebudget.ID{}) {
					return out, storagebudget.ErrAuthority
				}
				out = actionKey{plan.PlanID, r.Action}
			}
		}
		if out.plan == (storagebudget.ID{}) {
			return out, storagebudget.ErrAuthority
		}
		return out, nil
	}
	for i, selected := range table.slots {
		r := selected.record
		if r.reusable() {
			continue
		}
		key := actionKey{r.OperationID, 1}
		var originalRequest storagebudget.Request
		if table.identity[i] != nil {
			key, err = identityKey(*table.identity[i])
			if err != nil {
				return storagePolicyClosureInput{}, err
			}
			originalRequest, err = storageIdentityRequest(*table.identity[i], o.ref.cell.binding)
		} else {
			originalRequest, err = o.originalIntentRequest(r)
		}
		if err != nil {
			return storagePolicyClosureInput{}, err
		}
		atom := storagePolicyOwedTerminal{Plan: key.plan, Action: key.action, Request: originalRequest}
		if r.Token == (storagebudget.Token{}) {
			// A Prepared with no positively recorded ticket remains possible debt;
			// it must not be mistaken for a reusable/absent application.
			atom.Uninstantiated, atom.Request = true, storagebudget.Request{}
		} else {
			t := inspected.View.Slots[r.Token.Slot].Ticket
			if !storageIntentMatchingTicket(r, t, o.ref.cell.binding) {
				return storagePolicyClosureInput{}, errStorageOwnerRecovery
			}
			atom.Token, atom.Manifest = t.Token, t.Manifest
			if t.State == storagebudget.Terminal {
				atom.Terminal = &t
			}
		}
		owed[key] = atom
	}
	if operation != (storagebudget.ID{}) {
		key := actionKey{operation, 1}
		if identity != nil {
			key, err = identityKey(*identity)
			if err != nil {
				return storagePolicyClosureInput{}, err
			}
			original, err := storageIdentityRequest(*identity, o.ref.cell.binding)
			if err != nil || !reflect.DeepEqual(original, request) || identity.Core.OperationID != operation {
				return storagePolicyClosureInput{}, storagebudget.ErrAuthority
			}
			if _, promised := owed[key]; !promised {
				// An original move is a new admission, not a missing founding
				// P/C/S/F promise. Only its exact registered epoch/request may
				// add this one atom before Prepared and atomic ReserveCompared.
				if identity.Action != storageIdentityIntentMove || o.originalMoveAdmissionCurrent(ctx, *identity, request) != nil {
					return storagePolicyClosureInput{}, storagebudget.ErrAuthority
				}
			}
		} else if _, duplicate := owed[key]; duplicate {
			return storagePolicyClosureInput{}, storagebudget.ErrAuthority
		}
		owed[key] = storagePolicyOwedTerminal{Plan: key.plan, Action: key.action, Request: request}
	}
	keys := make([]actionKey, 0, len(owed))
	for key := range owed {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].plan != keys[j].plan {
			return bytes.Compare(keys[i].plan[:], keys[j].plan[:]) < 0
		}
		return keys[i].action < keys[j].action
	})
	for _, key := range keys {
		input.Owed = append(input.Owed, owed[key])
	}
	return input, ctx.Err()
}

// Re-read the actual source pair under the caller's existing exclusion. Old
// portable/comparison cores never substitute for the held V6 physical banks.
func (o *storageBudgetOwner) originalPolicyLineage(ctx context.Context) (storageCompositionLineage, error) {
	lineage := storageCompositionEmptyLineage()
	if o.valid() && o.ref.cell.enrollmentEndpoint != nil {
		state, err := o.readOriginalPolicy(ctx)
		if err != nil {
			return lineage, err
		}
		if state.Packed != nil && state.Packed.Phase == "prepared" {
			return lineage, errStorageEnrollmentUnresolved
		}
		return state.Lineage, ctx.Err()
	}
	if !o.valid() || o.ref.cell.ownerRecord == nil {
		return lineage, storagebudget.ErrAuthority
	}
	var actual [2][]byte
	for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
		raw, named, err := storageOwnerReadFixed(o.ref.cell.original.config, name, storageOwnerCatalogBytes)
		if err != nil {
			return lineage, err
		}
		err = errors.Join(named.current(), named.file.Close())
		if err != nil {
			return lineage, err
		}
		actual[i] = raw
	}
	record, claim := *o.ref.cell.ownerRecord, o.ref.cell.ownerClaim
	if string(actual[0][:8]) == "DSKONL1\x00" && string(actual[1][:8]) == "DSKONL1\x00" {
		pair, err := storageCompositionReadPair(actual[0], actual[1], record, claim)
		if err != nil || pair.RecoveryOnly {
			return lineage, errors.Join(errStorageEnrollmentUnresolved, err)
		}
		lineage = pair.Banks[pair.Selected].Lineage
	} else if bytes.Equal(actual[0][:8], storageOwnerV6Magic[:]) || bytes.Equal(actual[1][:8], storageOwnerV6Magic[:]) {
		original := o.ref.cell.enrollmentEndpoint
		if original == nil {
			return lineage, errStorageEnrollmentUnresolved
		}
		selected := byte(0)
		if storageEnrollmentPeakU64(actual[1][16:24]) > storageEnrollmentPeakU64(actual[0][16:24]) {
			selected = 1
		}
		_, _, current, _, _, err := storageOwnerV6CaptureBefore(actual, selected, original.Raw, o.ref.cell.ownerCatalog, record, claim)
		if err != nil {
			return lineage, err
		}
		lineage = current
	} else if string(actual[0][:8]) == "DSKOWN3\x00" && string(actual[1][:8]) == "DSKOWN3\x00" {
		pair, err := storageOwnerDecodeCatalogPairV3(actual[0], actual[1], record, claim)
		if err != nil || pair.High.Phase != "active" {
			return lineage, errors.Join(errStorageOwnerRecovery, err)
		}
	} else {
		pair, _, err := storageEnrollmentPeakDecodePair(actual[0], actual[1], record, claim)
		original := o.ref.cell.enrollmentEndpoint
		if err != nil || original == nil || storageEnrollmentPeakJoinMarker(pair, *original, original.Ready) != nil || pair.Marker.Phase != 3 {
			return lineage, errors.Join(errStorageEnrollmentUnresolved, err)
		}
	}
	if err := lineage.validateGraph(o.ref.cell.ownerCatalog, record); err != nil {
		return lineage, err
	}
	return lineage, ctx.Err()
}
