package desk

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
)

//go:embed testdata/dialect_read_recovery_public.json
var recoveryPublicVectors []byte

// Hand-authored field-maximal data, not recorded from a production run.
func recoveryMaximumRecord() assistantDialectReadRecoveryRecord {
	d := strings.Repeat("f", 64)
	return assistantDialectReadRecoveryRecord{
		OperationID: strings.Repeat("<", 128), OriginalIntentSHA256: d, BindingSHA256: d, Owner: d, CustodyIntentSHA256: d,
		Reference: &retainedArtifactRef{SHA256: d, Bytes: 33554432}, FirstClassification: "acknowledged-result-unavailable", OriginalUncertainty: "cancelled-after-intent",
		LatestObservation: assistantDialectReadRecoveryObservation{Ordinal: math.MaxUint64, BaselineRunRevision: math.MaxUint64, BaselineRunSHA256: d, SourceProvenanceSHA256: d, State: "unavailable", Reason: "provider-unavailable", RequestID: strings.Repeat("<", 128)},
		Settlement:        assistantDialectReadRecoverySettlement{State: "accounted-retained", OwnerSHA256: d, EvidenceSHA256: d},
	}
}

func recoveryMaximumFile() (string, assistantDialectReadRecovery) {
	d := strings.Repeat("f", 64)
	value := assistantDialectReadRecovery{Version: 1, Project: d, RunID: "arun_" + strings.Repeat("f", 32), RunBindingSHA256: d, Revision: math.MaxUint64, Records: []assistantDialectReadRecoveryRecord{}}
	for i := 0; i < 512; i++ {
		row := recoveryMaximumRecord()
		// Nine ternary HTML characters retain the maximal six-byte escaping and
		// provide distinct operation IDs without growing the 128-byte field.
		id, n := []byte(strings.Repeat("<", 128)), i
		for j := 119; j < 128; j++ {
			id[j] = "<>&"[n%3]
			n /= 3
		}
		row.OperationID = string(id)
		value.Records = append(value.Records, row)
	}
	return assistantDialectReadRecoveryName(value.Project, value.RunID), value
}

func TestDialectReadRecoveryEncodedMaximum(t *testing.T) {
	row := recoveryMaximumRecord()
	raw, err := json.Marshal(row)
	assistantRunMust(t, err)
	if len(raw) != 2654 || len(raw) > assistantDialectReadRecoveryRecordLimit {
		t.Fatalf("escaped row bytes=%d, want 2654 within 3072", len(raw))
	}
	name, value := recoveryMaximumFile()
	raw, err = json.Marshal(value)
	assistantRunMust(t, err)
	if len(raw) != 1359628 || len(raw) > assistantDialectReadRecoveryFileLimit {
		t.Fatalf("512-row bytes=%d, want 1359628 within 2097152", len(raw))
	}
	decoded, err := decodeAssistantDialectReadRecovery(name, raw)
	assistantRunMust(t, err)
	if decoded.Revision != math.MaxUint64 || decoded.Records[511].LatestObservation.Ordinal != math.MaxUint64 || len(decoded.Records) != 512 {
		t.Fatal("lossless counters/complete rows not retained")
	}
	if storageFileLimit(name) != 2<<20 || maxBackupData != 128<<20 || maxStorageFiles != 4096 || maxBackupManifest != 1<<20 || assistantRunRecordLimit != 16<<20 {
		t.Fatal("new format changed an existing capacity contract")
	}
}

func TestDialectReadRecoveryStrictShapeAndBounds(t *testing.T) {
	name, value := recoveryMaximumFile()
	value.Records = value.Records[:1]
	raw, err := json.Marshal(value)
	assistantRunMust(t, err)
	for label, bad := range map[string][]byte{
		"empty": {}, "null": []byte("null"), "missing": bytes.Replace(raw, []byte(`"version":1,`), nil, 1),
		"alias":         bytes.Replace(raw, []byte(`"version":`), []byte(`"Version":`), 1),
		"duplicate":     bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		"unknown":       bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"grant":true`), 1),
		"file-overflow": append(bytes.Repeat([]byte(" "), assistantDialectReadRecoveryFileLimit), raw...),
		"row-overflow":  bytes.Replace(raw, []byte(`"operationId":`), append(bytes.Repeat([]byte(" "), assistantDialectReadRecoveryRecordLimit), []byte(`"operationId":`)...), 1),
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := decodeAssistantDialectReadRecovery(name, bad); err == nil {
				t.Fatal("malformed saved data accepted")
			}
		})
	}
	for label, change := range map[string]func(*assistantDialectReadRecovery){
		"foreign-project":     func(v *assistantDialectReadRecovery) { v.Project = strings.Repeat("a", 64) },
		"foreign-run":         func(v *assistantDialectReadRecovery) { v.RunID = "arun_" + strings.Repeat("a", 32) },
		"duplicate-operation": func(v *assistantDialectReadRecovery) { v.Records = append(v.Records, v.Records[0]) },
		"too-many": func(v *assistantDialectReadRecovery) {
			for len(v.Records) < 513 {
				row := v.Records[0]
				row.OperationID = string(rune(1000 + len(v.Records)))
				v.Records = append(v.Records, row)
			}
		},
		"result-as-authority": func(v *assistantDialectReadRecovery) { v.Records[0].FirstClassification = "completed" },
		"unknown-state":       func(v *assistantDialectReadRecovery) { v.Records[0].LatestObservation.State = "success" },
		"raw-error-text":      func(v *assistantDialectReadRecovery) { v.Records[0].LatestObservation.Reason = "caught provider text" },
		"unbound-settlement":  func(v *assistantDialectReadRecovery) { v.Records[0].Settlement.OwnerSHA256 = "" },
	} {
		t.Run(label, func(t *testing.T) {
			var bad assistantDialectReadRecovery
			assistantRunMust(t, json.Unmarshal(raw, &bad))
			change(&bad)
			if validateAssistantDialectReadRecovery(name, bad) == nil {
				t.Fatal("invalid bound accepted")
			}
		})
	}
}

func TestDialectReadRecoveryFiniteStates(t *testing.T) {
	for _, state := range []string{"present-valid", "absent", "corrupt", "unavailable"} {
		row := recoveryMaximumRecord()
		row.LatestObservation.State = state
		row.LatestObservation.Reason = ""
		if state == "unavailable" {
			row.LatestObservation.Reason = "storage-io"
		}
		if validateAssistantDialectReadRecoveryRecord(row) != nil {
			t.Fatalf("state %s refused", state)
		}
	}
	for _, reason := range []string{"owner-unavailable", "provider-unavailable", "ownership-mismatch", "storage-io", "close-uncertain", "cancelled", "budget-unresolved"} {
		row := recoveryMaximumRecord()
		row.LatestObservation.Reason = reason
		if validateAssistantDialectReadRecoveryRecord(row) != nil {
			t.Fatalf("reason %s refused", reason)
		}
	}
	for _, class := range []string{"not-reserved", "reserved-unacknowledged", "acknowledged-result-unavailable", "completed-backing-unavailable"} {
		row := recoveryMaximumRecord()
		row.FirstClassification = class
		if class == "not-reserved" {
			row.Reference = nil
			row.CustodyIntentSHA256 = ""
			row.OriginalUncertainty = ""
			row.LatestObservation.State = "absent"
			row.LatestObservation.Reason = ""
		}
		if validateAssistantDialectReadRecoveryRecord(row) != nil {
			t.Fatalf("classification %s refused", class)
		}
	}
	for _, uncertainty := range []string{"", "storage-unacknowledged", "ack-mismatch", "cancelled-after-intent"} {
		row := recoveryMaximumRecord()
		row.OriginalUncertainty = uncertainty
		if validateAssistantDialectReadRecoveryRecord(row) != nil {
			t.Fatalf("uncertainty %s refused", uncertainty)
		}
	}
	for _, state := range []string{"unresolved", "accounted-retained", "accounted-absent"} {
		row := recoveryMaximumRecord()
		row.Settlement.State = state
		if state == "unresolved" {
			row.Settlement.OwnerSHA256 = ""
			row.Settlement.EvidenceSHA256 = ""
		}
		if validateAssistantDialectReadRecoveryRecord(row) != nil {
			t.Fatalf("storage state %s refused", state)
		}
	}
}

func TestDialectReadRecoveryLosslessPublicIdentity(t *testing.T) {
	for _, value := range []string{"9007199254740993", "18446744073709551615"} {
		got, err := assistantDialectReadRecoveryDecimal(value, false)
		assistantRunMust(t, err)
		if got <= 1<<53 {
			t.Fatal("uint64 identity lost")
		}
	}
	for _, value := range []string{"", "00", "01", "-1", "1.0", "1e3", "18446744073709551616"} {
		if _, err := assistantDialectReadRecoveryDecimal(value, true); err == nil {
			t.Fatalf("invalid decimal %q accepted", value)
		}
	}
}

func TestDialectReadRecoveryPublicProjectionBound(t *testing.T) {
	name, maximum := recoveryMaximumFile()
	_ = name
	view := assistantDialectReadRecoveryView{RunID: maximum.RunID, RunRevision: "18446744073709551615", RecoveryRevision: "18446744073709551615", Reads: []assistantDialectReadRecoveryPublic{}}
	for _, record := range maximum.Records {
		view.Reads = append(view.Reads, assistantDialectReadRecoveryPublic{OperationID: record.OperationID, OriginalOutcome: "interrupted", BackingState: "unavailable", Reason: "provider-unavailable", Ordinal: "18446744073709551615", StorageState: "accounted-retained", CanInspect: false})
	}
	raw, err := json.Marshal(view)
	assistantRunMust(t, err)
	if len(raw) != 495754 || len(raw) > assistantDialectReadRecoveryPublicLimit {
		t.Fatalf("public bytes %d, want 495754 below 1MiB", len(raw))
	}
	decoded, err := decodeAssistantDialectReadRecoveryPublic(raw, view.RunID)
	assistantRunMust(t, err)
	if len(decoded.Reads) != 512 {
		t.Fatal("public projection omitted rows")
	}
}

func TestDialectReadRecoveryPublicSharedVectors(t *testing.T) {
	var vectors struct {
		Version   int             `json:"version"`
		Valid     json.RawMessage `json:"valid"`
		Mutations []struct {
			Name   string          `json:"name"`
			Path   []string        `json:"path"`
			Remove bool            `json:"remove"`
			Value  json.RawMessage `json:"value"`
		} `json:"mutations"`
	}
	assistantRunMust(t, json.Unmarshal(recoveryPublicVectors, &vectors))
	if vectors.Version != 1 || len(vectors.Mutations) != 20 {
		t.Fatal("shared vector coverage changed")
	}
	expected := "arun_" + strings.Repeat("f", 32)
	got, err := decodeAssistantDialectReadRecoveryPublic(vectors.Valid, expected)
	assistantRunMust(t, err)
	if got.RunRevision != "9007199254740993" || got.RecoveryRevision != "18446744073709551615" || len(got.Reads) != 11 {
		t.Fatal("shared lossless state changed")
	}
	for _, mutation := range vectors.Mutations {
		t.Run(mutation.Name, func(t *testing.T) {
			var value any
			assistantRunMust(t, json.Unmarshal(vectors.Valid, &value))
			at := value
			for _, key := range mutation.Path[:len(mutation.Path)-1] {
				switch v := at.(type) {
				case map[string]any:
					at = v[key]
				case []any:
					n, e := strconv.Atoi(key)
					assistantRunMust(t, e)
					at = v[n]
				default:
					t.Fatal("invalid handwritten path")
				}
			}
			object, ok := at.(map[string]any)
			if !ok {
				t.Fatal("invalid handwritten target")
			}
			key := mutation.Path[len(mutation.Path)-1]
			if mutation.Remove {
				delete(object, key)
			} else {
				var replacement any
				assistantRunMust(t, json.Unmarshal(mutation.Value, &replacement))
				object[key] = replacement
			}
			raw, err := json.Marshal(value)
			assistantRunMust(t, err)
			if _, err = decodeAssistantDialectReadRecoveryPublic(raw, expected); err == nil {
				t.Fatal("shared refusal vector accepted")
			}
		})
	}
}
