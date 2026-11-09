package desk

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

const assistantDialectReadRecoveryRecordLimit = 3 << 10
const assistantDialectReadRecoveryFileLimit = 2 << 20

var assistantDialectReadRecoveryFileName = regexp.MustCompile(`^assistant-dialect-read-recovery-([a-f0-9]{64})-(arun_[a-f0-9]{32})\.json$`)
var assistantDialectReadRecoveryRunID = regexp.MustCompile(`^arun_[a-f0-9]{32}$`)

// Recovery is separate observed history. Nothing in this file is an admission,
// original receipt, completed tool result, source selector or accounting grant.
type assistantDialectReadRecovery struct {
	Version          int                                  `json:"version"`
	Project          string                               `json:"project"`
	RunID            string                               `json:"runId"`
	RunBindingSHA256 string                               `json:"runBindingSha256"`
	Revision         uint64                               `json:"revision"`
	Records          []assistantDialectReadRecoveryRecord `json:"records"`
}

type assistantDialectReadRecoveryRecord struct {
	OperationID          string                                  `json:"operationId"`
	OriginalIntentSHA256 string                                  `json:"originalIntentSha256"`
	BindingSHA256        string                                  `json:"bindingSha256"`
	Owner                string                                  `json:"owner,omitempty"`
	CustodyIntentSHA256  string                                  `json:"custodyIntentSha256,omitempty"`
	Reference            *retainedArtifactRef                    `json:"reference,omitempty"`
	FirstClassification  string                                  `json:"firstClassification"`
	OriginalUncertainty  string                                  `json:"originalUncertainty,omitempty"`
	LatestObservation    assistantDialectReadRecoveryObservation `json:"latestObservation"`
	Settlement           assistantDialectReadRecoverySettlement  `json:"settlement"`
}

type assistantDialectReadRecoveryObservation struct {
	Ordinal                uint64 `json:"ordinal"`
	BaselineRunRevision    uint64 `json:"baselineRunRevision"`
	BaselineRunSHA256      string `json:"baselineRunSha256"`
	SourceProvenanceSHA256 string `json:"sourceProvenanceSha256,omitempty"`
	State                  string `json:"state"`
	Reason                 string `json:"reason,omitempty"`
	RequestID              string `json:"requestId"`
}

type assistantDialectReadRecoverySettlement struct {
	// The original B2 owner journal retains the coordinator token. Recovery
	// stores its evidence identity only; it does not invent a second token format.
	State          string `json:"state"`
	OwnerSHA256    string `json:"ownerSha256,omitempty"`
	EvidenceSHA256 string `json:"evidenceSha256,omitempty"`
}

func assistantDialectReadRecoveryName(project, run string) string {
	return "assistant-dialect-read-recovery-" + project + "-" + run + ".json"
}

func assistantDialectReadRecoveryHistoryMember(name, conversation string) bool {
	parts := assistantDialectReadRecoveryFileName.FindStringSubmatch(name)
	return parts != nil && conversation == "conversations-"+parts[1]+".json" && !strings.Contains(name, "/")
}

func decodeAssistantDialectReadRecovery(name string, raw []byte) (assistantDialectReadRecovery, error) {
	var out assistantDialectReadRecovery
	if len(raw) == 0 || len(raw) > assistantDialectReadRecoveryFileLimit || !validUTF8(raw) || jsonscalar.ValidateEscapes(raw) != nil || !uniqueJSON(raw) {
		return out, errAssistantRunInvalid
	}
	if readAssistantDialectFactoryShape(raw, reflect.TypeOf(out), false, 1) != nil || json.Unmarshal(raw, &out) != nil || validateAssistantDialectReadRecovery(name, out) != nil {
		return assistantDialectReadRecovery{}, errAssistantRunInvalid
	}
	// Enforce the original encoded row bound too, before canonical JSON can shrink
	// escaped spellings or insignificant whitespace supplied by an import.
	var wire struct {
		Records []json.RawMessage `json:"records"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return assistantDialectReadRecovery{}, errAssistantRunInvalid
	}
	for _, row := range wire.Records {
		if len(row) > assistantDialectReadRecoveryRecordLimit {
			return assistantDialectReadRecovery{}, errAssistantRunInvalid
		}
	}
	return out, nil
}

func validateAssistantDialectReadRecovery(name string, value assistantDialectReadRecovery) error {
	parts := assistantDialectReadRecoveryFileName.FindStringSubmatch(name)
	if parts == nil || value.Version != 1 || value.Project != parts[1] || value.RunID != parts[2] || !assistantRunDigest(value.RunBindingSHA256) || value.Revision == 0 || value.Records == nil || len(value.Records) > assistantRunOperationLimit {
		return errAssistantRunInvalid
	}
	seen := map[string]bool{}
	for _, row := range value.Records {
		if seen[row.OperationID] || validateAssistantDialectReadRecoveryRecord(row) != nil {
			return errAssistantRunInvalid
		}
		seen[row.OperationID] = true
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > assistantDialectReadRecoveryFileLimit {
		return errAssistantRunInvalid
	}
	return nil
}

func validateAssistantDialectReadRecoveryRecord(row assistantDialectReadRecoveryRecord) error {
	o, s := row.LatestObservation, row.Settlement
	if !assistantRunText(row.OperationID, 128) || !assistantRunDigest(row.OriginalIntentSHA256) || !assistantRunDigest(row.BindingSHA256) || row.Owner != "" && !assistantRunDigest(row.Owner) || !contains([]string{"not-reserved", "reserved-unacknowledged", "acknowledged-result-unavailable", "completed-backing-unavailable"}, row.FirstClassification) || row.OriginalUncertainty != "" && !contains([]string{"storage-unacknowledged", "ack-mismatch", "cancelled-after-intent"}, row.OriginalUncertainty) {
		return errAssistantRunInvalid
	}
	if row.Reference == nil {
		if row.CustodyIntentSHA256 != "" || row.FirstClassification != "not-reserved" || row.OriginalUncertainty != "" {
			return errAssistantRunInvalid
		}
	} else if !row.Reference.valid() || row.Reference.Bytes < 1 || !assistantRunDigest(row.CustodyIntentSHA256) || !assistantRunDigest(row.Owner) || row.FirstClassification == "not-reserved" {
		return errAssistantRunInvalid
	}
	if o.Ordinal == 0 || o.BaselineRunRevision == 0 || !assistantRunDigest(o.BaselineRunSHA256) || o.SourceProvenanceSHA256 != "" && !assistantRunDigest(o.SourceProvenanceSHA256) || !assistantRunText(o.RequestID, 128) || !contains([]string{"present-valid", "absent", "corrupt", "unavailable"}, o.State) {
		return errAssistantRunInvalid
	}
	if o.State == "unavailable" {
		if !contains([]string{"owner-unavailable", "provider-unavailable", "ownership-mismatch", "storage-io", "close-uncertain", "cancelled", "budget-unresolved"}, o.Reason) {
			return errAssistantRunInvalid
		}
	} else if o.Reason != "" || row.Reference == nil && o.State != "absent" || o.State != "absent" && o.SourceProvenanceSHA256 == "" {
		return errAssistantRunInvalid
	}
	if !contains([]string{"unresolved", "accounted-retained", "accounted-absent"}, s.State) {
		return errAssistantRunInvalid
	}
	if s.State == "unresolved" && (s.OwnerSHA256 != "" || s.EvidenceSHA256 != "") || s.State != "unresolved" && (!assistantRunDigest(s.OwnerSHA256) || !assistantRunDigest(s.EvidenceSHA256)) {
		return errAssistantRunInvalid
	}
	raw, err := json.Marshal(row)
	if err != nil || len(raw) > assistantDialectReadRecoveryRecordLimit {
		return errAssistantRunInvalid
	}
	return nil
}

// Saved-data validation only: no source/provider/raw storage or live admission.
func validateAssistantDialectReadRecoveryBound(value assistantDialectReadRecovery, run assistantRunRecord) error {
	if value.Project != run.Binding.Project || value.RunID != run.ID || value.RunBindingSHA256 != digestOf(assistantChatJSON(run.Binding)) {
		return errAssistantRunScope
	}
	for _, row := range value.Records {
		op, _, err := assistantDialectCompleteReadRoot(run, row.OperationID)
		if err != nil || row.BindingSHA256 != value.RunBindingSHA256 || row.OriginalIntentSHA256 != digestOf(op.Intent) || row.Owner != run.DialectArtifactOwner {
			return errAssistantRunScope
		}
		held := assistantDialectCompleteReadOf(&run, row.OperationID)
		if held == nil {
			if row.Reference != nil || row.FirstClassification != "not-reserved" || row.OriginalUncertainty != "" {
				return errAssistantRunScope
			}
		} else if row.Reference == nil || *row.Reference != held.Intent.Reference || row.CustodyIntentSHA256 != digestOf(assistantChatJSON(held.Intent)) || row.OriginalUncertainty != held.Uncertainty {
			return errAssistantRunScope
		}
		if held != nil {
			classification := "reserved-unacknowledged"
			if held.Receipt != nil {
				classification = "acknowledged-result-unavailable"
			}
			if op.State == "completed" {
				classification = "completed-backing-unavailable"
			}
			if row.FirstClassification != classification {
				return errAssistantRunScope
			}
		}
	}
	return nil
}

func sameAssistantDialectReadRecoveryOriginal(a, b assistantDialectReadRecoveryRecord) bool {
	a.LatestObservation, b.LatestObservation = assistantDialectReadRecoveryObservation{}, assistantDialectReadRecoveryObservation{}
	a.Settlement, b.Settlement = assistantDialectReadRecoverySettlement{}, assistantDialectReadRecoverySettlement{}
	return bytes.Equal(assistantChatJSON(a), assistantChatJSON(b))
}
