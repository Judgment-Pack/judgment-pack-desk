package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
)

// One immutable exact-byte parsing fact per Server, under private-data custody.
// It retains neither a decoded graph nor permissions, currentness or a live root.
// Temporary read/decode allocations are separate from this bounded retained copy.
type assistantRunStorageValidation struct {
	root                          os.FileInfo
	path, revision, project, name string
	raw                           []byte
}

func (v *assistantRunStorageValidation) matches(root os.FileInfo, data *chatDataStore, project, name string, raw []byte) bool {
	return v != nil && os.SameFile(v.root, root) && v.path == data.location.Path && v.revision == data.revision && v.project == project && v.name == name && bytes.Equal(v.raw, raw)
}

// Call only under custody after the full decoder or composed output proof, and
// for output only after writePrivateData's sync/rename/readback has succeeded.
func (s *Server) rememberAssistantRunStorage(root os.FileInfo, data *chatDataStore, project, name string, raw []byte) {
	retained := make([]byte, len(raw))
	copy(retained, raw)
	s.assistantRunValidation = &assistantRunStorageValidation{root: root, path: data.location.Path, revision: data.revision, project: project, name: name, raw: retained}
}

// Observation only. These hooks cannot replace validators or their results.
var testHookAssistantRunStorageValidation func()
var testHookAssistantRunOutputValidation func(associated bool)

// readValidatedLocked freshly captures the same guarded private file before any
// lookup. A hit decodes an independent callback graph. A miss runs the full
// original-byte decoder. Unlocked readSnapshot deliberately does not use facts.
func (s assistantRunStore) readValidatedLocked(ctx context.Context, data *chatDataStore) (assistantRunLedger, assistantRunCapturedRead, os.FileInfo, error) {
	var empty assistantRunLedger
	captured, err := s.captureLocked(data)
	if err != nil {
		return empty, captured, nil, err
	}
	root, err := data.root.Stat(".")
	if err != nil {
		return empty, captured, nil, err
	}
	if err = ctx.Err(); err != nil {
		return empty, captured, nil, err
	}
	if !captured.missing && s.server.assistantRunValidation.matches(root, data, s.project, s.name, captured.raw) {
		ledger, err := captured.decodeValidated(ctx)
		return ledger, captured, root, err
	}
	ledger, err := captured.decode(ctx)
	if err != nil {
		return empty, captured, nil, err
	}
	if !captured.missing {
		s.server.rememberAssistantRunStorage(root, data, s.project, s.name, captured.raw)
	}
	return ledger, captured, root, nil
}

// Only readValidatedLocked and transactionLocked call this, after these exact
// independently captured bytes passed the full decoder or matched its retained
// proof under private-data custody. UTF-8, members, unknown fields and the single
// document are already proven; Unmarshal constructs another typed graph without
// repeating decodeDataJSON's whole-ledger top-level scan. Keep this graph separate
// from the callback and immutable transition baseline, including on false returns.
// A missing file still uses the validated empty baseline.
func (c assistantRunCapturedRead) decodeValidated(ctx context.Context) (assistantRunLedger, error) {
	var empty assistantRunLedger
	ledger := assistantRunLedger{Version: 1, Project: c.project, Runs: []assistantRunRecord{}}
	if !c.missing {
		if err := json.Unmarshal(c.raw, &ledger); err != nil {
			return empty, err
		}
		if ledger.Project != c.project {
			return empty, errAssistantRunInvalid
		}
	} else if err := validateAssistantRunLedger(ledger, c.project); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return ledger, nil
}

// Latch before validation: the dynamic marshaler can run inside validation as
// well as json.Marshal, mutate other fields, and replace itself. Named strings
// are intentionally ineligible; their methods need the independently validated
// decoded fallback. The type-graph regression test guards this fixed inventory.
func assistantRunMarshalEligible(ledger assistantRunLedger) bool {
	for _, run := range ledger.Runs {
		if run.Creation == nil {
			continue
		}
		for _, citation := range run.Creation.Citations {
			switch citation.ExcerptID.(type) {
			case nil, string:
			default:
				return false
			}
		}
	}
	return true
}

// Own the whole proof: V(callback), marshal, every original raw guard and an
// independent decoded graph. Only audited marshal purity AND structural equality
// associate V(callback) with V(decoded). Otherwise validate the decoded graph.
// The synchronous callback must relinquish mutation while this proof runs.
func prepareAssistantRunStorage(name string, ledger assistantRunLedger) ([]byte, assistantRunLedger, error) {
	var empty assistantRunLedger
	if !assistantRunFileName.MatchString(name) {
		return nil, empty, errAssistantRunInvalid
	}
	project := strings.TrimSuffix(strings.TrimPrefix(name, "assistant-runs-"), ".json")
	eligible := assistantRunMarshalEligible(ledger)
	if err := validateAssistantRunLedger(ledger, project); err != nil {
		return nil, empty, err
	}
	raw, err := json.Marshal(ledger)
	if err != nil {
		return nil, empty, err
	}
	if len(raw) > assistantRunRecordLimit {
		return nil, empty, withCode(CodeTooLarge, errors.New("Assistant run storage limit reached; retained work was not discarded."))
	}
	saved, err := decodeAssistantRunStorageRaw(name, raw)
	if err != nil {
		return nil, empty, err
	}
	associated := eligible && reflect.DeepEqual(ledger, saved)
	if testHookAssistantRunOutputValidation != nil {
		testHookAssistantRunOutputValidation(associated)
	}
	if !associated {
		if err = validateAssistantRunLedger(saved, project); err != nil {
			return nil, empty, err
		}
	}
	return raw, saved, nil
}
