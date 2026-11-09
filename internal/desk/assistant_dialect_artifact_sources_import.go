package desk

import (
	"reflect"
)

// Import availability describes the exact historical slot, never a receipt,
// native namespace grant or authority to look up an arbitrary object. Only the
// eventual concrete restore transaction produces it after full archive closure
// validation. Ordinary live journal transactions cannot manufacture it.
type assistantDialectArtifactImport struct {
	Version int                                  `json:"version"`
	Slots   []assistantDialectArtifactImportSlot `json:"slots"`
}

type assistantDialectArtifactImportSlot struct {
	HostOperationID string `json:"hostOperationId"`
	Stage           string `json:"stage"`
	Availability    string `json:"availability"`
}

type assistantDialectArtifactImportSlotKey struct {
	host  string
	stage string
}

func readAssistantDialectArtifactImport(raw []byte) error {
	return readAssistantDialectFactoryShape(raw, reflect.TypeOf(assistantDialectArtifactImport{}), false, 1)
}

// Exact coverage uses the existing admitted custody bounds. Missing-heavy
// histories have no extra count or declared-payload limit here: only actual
// archived bodies count against the eventual archive payload cap.
func validateAssistantDialectArtifactImport(run assistantRunRecord) error {
	value := run.DialectArtifactImport
	if value == nil {
		return nil
	}
	if !run.Historical || !assistantRunTerminal(run.State) || !assistantRunDigest(run.DialectArtifactOwner) ||
		len(run.DialectDrafts) == 0 || value.Version != 1 || value.Slots == nil {
		return errAssistantRunInvalid
	}
	wanted := make(map[assistantDialectArtifactImportSlotKey]bool)
	for _, held := range run.DialectDrafts {
		for _, stage := range []string{"candidate", "checked", "final"} {
			slot := assistantDialectDraftSlotOf(&held, stage)
			if slot.Intent == nil {
				continue
			}
			key := assistantDialectArtifactImportSlotKey{held.HostOperationID, stage}
			if _, duplicate := wanted[key]; duplicate {
				return errAssistantRunInvalid
			}
			wanted[key] = slot.Receipt == nil
		}
	}
	if len(wanted) == 0 || len(value.Slots) != len(wanted) {
		return errAssistantRunInvalid
	}
	for _, slot := range value.Slots {
		key := assistantDialectArtifactImportSlotKey{slot.HostOperationID, slot.Stage}
		unknown, exists := wanted[key]
		if !exists || slot.Availability != "present" && slot.Availability != "missing" || slot.Availability == "missing" && !unknown {
			return errAssistantRunInvalid
		}
		delete(wanted, key)
	}
	if len(wanted) != 0 {
		return errAssistantRunInvalid
	}
	return nil
}

// Returns native (empty) or exact imported availability. Callers must inspect
// missing before entering any object view; another slot or destination may
// legitimately contain the same hash and cannot fill this historical absence.
func assistantDialectArtifactImportedAvailability(run assistantRunRecord, host, stage string) (string, error) {
	if err := validateAssistantDialectArtifactImport(run); err != nil {
		return "", err
	}
	if run.DialectArtifactImport == nil {
		return "", nil
	}
	for _, slot := range run.DialectArtifactImport.Slots {
		if slot.HostOperationID == host && slot.Stage == stage {
			return slot.Availability, nil
		}
	}
	return "", errAssistantRunScope
}

// This is only the ordinary journal transition guard. There is deliberately no
// serialized restore admission or bypass: restore writes a newly prevalidated
// historical target through changeChatStorage under its original custody.
func validateAssistantDialectArtifactImportTransition(prior *assistantRunRecord, run assistantRunRecord) error {
	if prior == nil {
		if run.DialectArtifactImport != nil {
			return errAssistantRunScope
		}
		return nil
	}
	if !reflect.DeepEqual(prior.DialectArtifactImport, run.DialectArtifactImport) {
		return errAssistantRunScope
	}
	if prior.DialectArtifactImport != nil && (!run.Historical || !assistantRunTerminal(run.State) ||
		run.DialectArtifactOwner != prior.DialectArtifactOwner || !reflect.DeepEqual(prior.DialectDrafts, run.DialectDrafts)) {
		return errAssistantRunScope
	}
	return nil
}
