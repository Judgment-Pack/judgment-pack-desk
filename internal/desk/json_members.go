package desk

// Persistence distinguishes control envelopes from opaque JSON values. Unknown
// envelope branches retain the older conservative case-folded check; only an
// explicitly declared RawMessage/map payload has case-sensitive member names.
import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

var opaqueJSONMembers = reflect.TypeOf(json.RawMessage{})

// checkJSONMembers checks names without decoding or rewriting retained values.
// RawMessage is opaque data, maps have data keys, and structs declare reserved
// names. A nil schema conservatively retains case-folded duplicate rejection.
func checkJSONMembers(raw []byte, shape reflect.Type) error {
	if !utf8.Valid(raw) || jsonscalar.ValidateEscapes(raw) != nil {
		return errors.New("invalid JSON Unicode")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var read func(int, reflect.Type) error
	read = func(depth int, shape reflect.Type) error {
		if depth > 64 {
			return errors.New("JSON nesting exceeds limit")
		}
		for shape != nil && shape.Kind() == reflect.Pointer {
			shape = shape.Elem()
		}
		opaque := shape == opaqueJSONMembers
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok {
					return errors.New("invalid JSON member")
				}
				identity := strings.ToLower(key)
				var next reflect.Type
				switch {
				case opaque:
					identity = key
					next = opaqueJSONMembers
				case shape != nil && shape.Kind() == reflect.Map:
					identity = key
					next = shape.Elem()
				case shape != nil && shape.Kind() == reflect.Struct:
					next, err = jsonControlField(shape, key)
					if next == opaqueJSONMembers {
						identity = key
					}
					if err != nil {
						return err
					}
				}
				if seen[identity] {
					return errors.New("duplicate JSON member")
				}
				seen[identity] = true
				if err = read(depth+1, next); err != nil {
					return err
				}
			}
		case '[':
			var next reflect.Type
			if opaque {
				next = opaqueJSONMembers
			} else if shape != nil && (shape.Kind() == reflect.Slice || shape.Kind() == reflect.Array) {
				next = shape.Elem()
			}
			for decoder.More() {
				if err = read(depth+1, next); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := read(0, shape); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("one JSON value required")
	}
	return nil
}

func jsonControlField(shape reflect.Type, key string) (reflect.Type, error) {
	fields := []reflect.StructField{}
	seen := map[reflect.Type]bool{}
	var collect func(reflect.Type)
	collect = func(current reflect.Type) {
		if seen[current] {
			return
		}
		seen[current] = true
		for i := 0; i < current.NumField(); i++ {
			field := current.Field(i)
			nested := field.Type
			for nested.Kind() == reflect.Pointer {
				nested = nested.Elem()
			}
			if field.Tag.Get("json") == "" && nested.Kind() == reflect.Struct && (field.Anonymous || current.PkgPath() == reflect.TypeOf(contract.RunEvent{}).PkgPath()) {
				collect(nested)
			} else {
				fields = append(fields, field)
			}
		}
	}
	collect(shape)
	for _, field := range fields {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" && key == name {
			return field.Type, nil
		}
	}
	for _, field := range fields {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" && strings.EqualFold(key, name) {
			return nil, errors.New("use exact JSON control field names")
		}
	}
	// Published open objects retain unknown members as data. Known aliases were
	// refused above, so these keys cannot replace a declared control member.
	for _, field := range fields {
		if field.Name == "AdditionalMembers" && field.Tag.Get("json") == "-" {
			return opaqueJSONMembers, nil
		}
	}
	return nil, nil
}

// These are validation shapes, not persisted copies. Existing readers still own
// type/closed-schema checks. Opaque payloads cannot declare a second control key.
type workspaceJSONHistory struct {
	Version int                  `json:"version"`
	Chats   []workspaceJSONEntry `json:"chats"`
	Drafts  []workspaceJSONEntry `json:"drafts"`
	Deleted []string             `json:"deleted"`
}
type workspaceJSONEntry struct {
	policyWorkspaceEntry
	PolicyRequest         *assistantPolicyBasis          `json:"policyRequest"`
	PolicyProposals       []assistantPolicyProposal      `json:"policyProposals"`
	PolicyInterpretations []policyInterpretationProposal `json:"policyInterpretations"`
	Interaction           string                         `json:"interaction"`
	AIConnection          string                         `json:"aiConnection"`
	Model                 string                         `json:"model"`
	Plans                 []chatPlan                     `json:"plans"`
	Pack                  *struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		Digest string `json:"digest"`
	} `json:"pack"`
	Graph *struct {
		ID        string `json:"id"`
		Path      string `json:"path"`
		Workspace string `json:"workspace"`
	} `json:"graph"`
	Input *struct {
		ID string `json:"id"`
	} `json:"input"`
	DraftID    string `json:"draftId"`
	Checkpoint *struct {
		State *struct {
			Execution         *contract.Checkpoint            `json:"execution"`
			Cases             []workspaceJSONCase             `json:"cases"`
			ExpectationIssues []workspaceJSONExpectationIssue `json:"expectationIssues"`
		} `json:"state"`
	} `json:"checkpoint"`
}

// All persisted case positions share the same control/data boundary. Other
// expectation fields keep conservative traversal; only facts are opaque data.
type workspaceJSONCase struct {
	ID                    string          `json:"id"`
	Facts                 json.RawMessage `json:"facts"`
	ExpectationSource     string          `json:"expectationSource"`
	Rationale             string          `json:"rationale"`
	ExpectedDisposition   any             `json:"expectedDisposition"`
	EvidenceAvailability  any             `json:"evidenceAvailability"`
	ExpectedHandoffTarget any             `json:"expectedHandoffTarget"`
}

type workspaceJSONExpectationIssue struct {
	ID       string                              `json:"id"`
	Message  string                              `json:"message"`
	Original workspaceJSONCase                   `json:"original"`
	Resolved *workspaceJSONExpectationResolution `json:"resolved"`
}

type workspaceJSONExpectationResolution struct {
	ApprovedAt  string            `json:"approvedAt"`
	Rationale   string            `json:"rationale"`
	Replacement workspaceJSONCase `json:"replacement"`
}
