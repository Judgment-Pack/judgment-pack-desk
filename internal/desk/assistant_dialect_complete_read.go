package desk

// Completed-read evidence is passive saved data. This codec neither grants
// source access nor publishes read receipts. The original invocation owns both.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

const assistantDialectCompleteReadBytes = retainedArtifactByteLimit

var errAssistantDialectCompleteRead = errors.New("complete original Dialect read evidence is unavailable or inconsistent")

type assistantDialectCompleteSource struct {
	RecordID      string  `json:"recordId"`
	RunID         string  `json:"runId"`
	RunRevision   *string `json:"runRevision"`
	RecipeVersion int     `json:"recipeVersion"`
	Availability  string  `json:"availability"`
	Inventory     int     `json:"inventory"`
}

type assistantDialectCompleteInventory struct {
	// FieldJSON is the exact raw object span, not a reserialized decoded field.
	Declarations []runcontext.DialectResearchRevision
	Sources      []assistantDialectCompleteSource
}

type assistantDialectCompleteRead struct {
	SnapshotRaw []byte
	Snapshot    runcontext.DialectSnapshot
	Inventory   assistantDialectCompleteInventory
}

// Only newly issued declarations use this serializer. Existing saved strings
// and their hashes are never changed. The outer carrier appends these raw
// objects directly: json.Marshal(RawMessage) would HTML-escape them again.
func assistantDialectCompleteJSON(value any) ([]byte, error) {
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err := e.Encode(value); err != nil {
		return nil, err
	}
	return bytes.Clone(bytes.TrimSuffix(out.Bytes(), []byte{'\n'})), nil
}

func assistantDialectCompleteObject(raw []byte, required []string, optional ...string) (map[string]json.RawMessage, error) {
	fields, err := assistantTestObject(raw)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(required)+len(optional))
	for _, name := range required {
		if fields[name] == nil {
			return nil, errAssistantDialectCompleteRead
		}
		wanted[name] = true
	}
	for _, name := range optional {
		wanted[name] = true
	}
	for name := range fields {
		if !wanted[name] {
			return nil, errAssistantDialectCompleteRead
		}
	}
	return fields, nil
}

func (v assistantDialectCompleteInventory) validate() error {
	if v.Sources == nil || len(v.Declarations) > inputRecordLimit || len(v.Sources) > inputRecordLimit {
		return errAssistantDialectCompleteRead
	}
	seen := map[string]bool{}
	used := make([]bool, len(v.Declarations))
	recordIDs := map[string]bool{}
	for _, revision := range v.Declarations {
		if !assistantDialectPackageDigest(revision.Revision) || seen[revision.Revision] || len(revision.Fields) > 30 {
			return errAssistantDialectCompleteRead
		}
		seen[revision.Revision] = true
		names := map[string]bool{}
		for _, field := range revision.Fields {
			if _, err := assistantDialectCompleteObject([]byte(field.FieldJSON), []string{"name", "description", "type"}); err != nil {
				return err
			}
			var original inputResearchField
			if len(field.FieldJSON) > retainedDeclarationBytes || json.Unmarshal([]byte(field.FieldJSON), &original) != nil || original.Name != field.Name || names[field.Name] || !researchFieldName.MatchString(field.Name) || !validUTF8([]byte(original.Description)) {
				return errAssistantDialectCompleteRead
			}
			names[field.Name] = true
			// Use the actual journal recipe's field guards, including types.
			if strings.TrimSpace(original.Description) == "" || len(original.Description) > 1000 || !contains(researchFieldTypes, original.Type) || contains([]string{"constructor", "prototype", "__proto__"}, original.Name) {
				return errAssistantDialectCompleteRead
			}
		}
	}
	for _, source := range v.Sources {
		if recordIDs[source.RecordID] {
			return errAssistantDialectCompleteRead
		}
		recordIDs[source.RecordID] = true
		if !validUTF8([]byte(source.RecordID)) || !inputResearchID.MatchString(source.RunID) || !retainedSafeVersion(source.RecipeVersion) || source.Inventory < 0 || source.Inventory >= len(v.Declarations) {
			return errAssistantDialectCompleteRead
		}
		used[source.Inventory] = true
		fields := v.Declarations[source.Inventory].Fields
		switch source.Availability {
		case "known":
			// Original collection revision identity, not an arbitrary label.
			if source.RunRevision == nil || !assistantRunDigest(*source.RunRevision) || fields == nil {
				return errAssistantDialectCompleteRead
			}
		case "unavailable":
			if source.RunRevision != nil || fields != nil {
				return errAssistantDialectCompleteRead
			}
		default:
			return errAssistantDialectCompleteRead
		}
	}
	for _, selected := range used {
		if !selected {
			return errAssistantDialectCompleteRead
		}
	}
	return nil
}

// Page adapters serve only already captured passive evidence. Binding every
// request prevents using an index from one saved read with another snapshot.
func (v assistantDialectCompleteRead) fieldIndex(ctx context.Context, request runcontext.DialectFieldIndexRequest) (runcontext.DialectFieldIndexPage, error) {
	var page runcontext.DialectFieldIndexPage
	if ctx.Err() != nil || v.Snapshot.ResearchIndex == nil || request.ID != v.Snapshot.ID || request.PackKey != v.Snapshot.PackKey || request.Revision != v.Snapshot.Revision || request.InventoryDigest != v.Snapshot.ResearchIndex.Digest || request.Entry < 0 || request.Entry >= len(v.Inventory.Declarations) {
		return page, errAssistantDialectCompleteRead
	}
	index := runcontext.DialectFieldIndexOf(v.Inventory.Declarations[request.Entry])
	if index == nil {
		return page, errAssistantDialectCompleteRead
	}
	page = runcontext.DialectFieldIndexPage{ID: request.ID, PackKey: request.PackKey, Revision: request.Revision, InventoryDigest: request.InventoryDigest, Entry: request.Entry, FieldIndex: *index}
	return page, nil
}

func (v assistantDialectCompleteRead) declarations(ctx context.Context, request runcontext.DialectDeclarationPageRequest) (runcontext.DialectDeclarationPage, error) {
	page := runcontext.DialectDeclarationPage{ID: request.ID, PackKey: request.PackKey, Revision: request.Revision, InventoryDigest: request.InventoryDigest, Cursor: request.Cursor, Declarations: []runcontext.DialectDeclaration{}}
	if ctx.Err() != nil || v.Snapshot.ResearchIndex == nil || request.ID != v.Snapshot.ID || request.PackKey != v.Snapshot.PackKey || request.Revision != v.Snapshot.Revision || request.InventoryDigest != v.Snapshot.ResearchIndex.Digest || request.Cursor < 0 || request.Limit < 1 || request.Limit > runcontext.DialectPageDeclarations {
		return page, errAssistantDialectCompleteRead
	}
	base, err := assistantDialectCompleteCanonicalBytes(page)
	if err != nil {
		return page, err
	}
	pageBytes := base
	at := 0
	for _, revision := range v.Inventory.Declarations {
		for _, field := range revision.Fields {
			if at >= request.Cursor && len(page.Declarations) < request.Limit {
				row := runcontext.DialectDeclaration{Revision: revision.Revision, Name: field.Name, FieldJSON: field.FieldJSON}
				rowBytes, err := assistantDialectCompleteCanonicalBytes(row)
				if err != nil {
					return page, err
				}
				separator := 0
				if len(page.Declarations) != 0 {
					separator = 1
				}
				if pageBytes+separator+rowBytes > runcontext.DialectPageBytes {
					if len(page.Declarations) == 0 {
						return page, errAssistantDialectCompleteRead
					}
					return page, nil
				}
				pageBytes += separator + rowBytes
				page.Declarations = append(page.Declarations, row)
			}
			at++
			if len(page.Declarations) == request.Limit {
				return page, nil
			}
		}
	}
	if len(page.Declarations) == 0 {
		return page, errAssistantDialectCompleteRead
	}
	return page, nil
}

func (v assistantDialectCompleteRead) validate(ctx context.Context) error {
	if err := v.Inventory.validate(); err != nil {
		return err
	}
	if !inputPreparationID.MatchString(v.Snapshot.ID) || !assistantRunDigest(v.Snapshot.Revision) || v.Snapshot.RecordCount < int64(len(v.Inventory.Sources)) || v.Snapshot.RecordCount > inputRecordLimit {
		return errAssistantDialectCompleteRead
	}
	for _, source := range v.Inventory.Sources {
		if v.Inventory.Declarations[source.Inventory].Revision != assistantDialectCompleteRevision(v.Snapshot.ID, source.RecipeVersion) {
			return errAssistantDialectCompleteRead
		}
	}
	index, err := runcontext.DialectResearchIndexOf(v.Inventory.Declarations)
	if err != nil || !reflect.DeepEqual(index, v.Snapshot.ResearchIndex) {
		return errAssistantDialectCompleteRead
	}
	// Execute only the released package's pure read validator. All ports are
	// passive captured bytes; no Runtime, preview, save or model port exists.
	tools := runcontext.NewDialectTools(runcontext.DialectAuthoringPorts{
		Read: func(context.Context, runcontext.DialectReadRequest) (runcontext.DialectSnapshot, error) {
			return v.Snapshot, nil
		},
		ReadFieldIndex: v.fieldIndex, ReadDeclarations: v.declarations,
	})
	request, _ := json.Marshal(runcontext.DialectReadRequest{ID: v.Snapshot.ID, PackKey: v.Snapshot.PackKey})
	result, err := tools[0].Execute(ctx, request)
	if err != nil || result.Err() {
		return errors.Join(errAssistantDialectCompleteRead, err)
	}
	return nil
}

func encodeAssistantDialectCompleteRead(ctx context.Context, snapshot runcontext.DialectSnapshot, inventory assistantDialectCompleteInventory) ([]byte, error) {
	snapshotRaw, err := json.Marshal(snapshot) // exact selected Go serializer
	if err != nil || len(snapshotRaw) > runcontext.DialectSnapshotBytes {
		return nil, errAssistantDialectCompleteRead
	}
	v := assistantDialectCompleteRead{SnapshotRaw: snapshotRaw, Snapshot: snapshot, Inventory: inventory}
	if err = v.validate(ctx); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString(`{"version":1,"kind":"dialect-complete-read","packageVersion":"0.12.0","conversionContract":`)
	contract, _ := json.Marshal(runcontext.DialectRetainedColumnDraft())
	out.Write(contract)
	out.WriteString(`,"snapshot":`)
	out.Write(snapshotRaw)
	out.WriteString(`,"researchDeclarations":`)
	if inventory.Declarations == nil {
		out.WriteString(`null`)
	} else {
		out.WriteByte('[')
	}
	for i, revision := range inventory.Declarations {
		if i != 0 {
			out.WriteByte(',')
		}
		label, _ := assistantDialectCompleteJSON(revision.Revision)
		out.WriteString(`{"revision":`)
		out.Write(label)
		out.WriteString(`,"fields":`)
		if revision.Fields == nil {
			out.WriteString(`null`)
		} else {
			out.WriteByte('[')
			for j, field := range revision.Fields {
				if j != 0 {
					out.WriteByte(',')
				}
				out.WriteString(field.FieldJSON)
			}
			out.WriteByte(']')
		}
		out.WriteByte('}')
	}
	if inventory.Declarations != nil {
		out.WriteByte(']')
	}
	out.WriteString(`,"originalSources":`)
	sources, err := assistantDialectCompleteJSON(inventory.Sources)
	if err != nil {
		return nil, err
	}
	out.Write(sources)
	out.WriteByte('}')
	if out.Len() > assistantDialectCompleteReadBytes {
		return nil, errAssistantDialectCompleteRead
	}
	return out.Bytes(), nil
}

func readAssistantDialectCompleteRead(ctx context.Context, raw []byte) (v assistantDialectCompleteRead, err error) {
	if len(raw) == 0 || len(raw) > assistantDialectCompleteReadBytes || !validUTF8(raw) || jsonscalar.ValidateEscapes(raw) != nil {
		return v, errAssistantDialectCompleteRead
	}
	// Own one immutable backing slice; every declaration points at its exact
	// selected span. A caller changing its input cannot change saved evidence.
	raw = bytes.Clone(raw)
	root, err := parseRetainedJSON(raw)
	if err != nil {
		return v, err
	}
	fields, err := assistantDialectCompleteObject(raw, []string{"version", "kind", "packageVersion", "conversionContract", "snapshot", "researchDeclarations", "originalSources"})
	if err != nil || string(fields["version"]) != "1" || string(fields["kind"]) != `"dialect-complete-read"` || string(fields["packageVersion"]) != `"0.12.0"` {
		return v, errAssistantDialectCompleteRead
	}
	var contract runcontext.DialectConversionContract
	if err = assistantDialectCompleteShape(fields["conversionContract"], reflect.TypeOf(contract), false); err != nil || json.Unmarshal(fields["conversionContract"], &contract) != nil || contract != runcontext.DialectRetainedColumnDraft() {
		return v, errAssistantDialectCompleteRead
	}
	v.SnapshotRaw = fields["snapshot"]
	if len(v.SnapshotRaw) > runcontext.DialectSnapshotBytes || assistantDialectCompleteShape(v.SnapshotRaw, reflect.TypeOf(v.Snapshot), false) != nil || json.Unmarshal(v.SnapshotRaw, &v.Snapshot) != nil {
		return v, errAssistantDialectCompleteRead
	}
	rows := root.member("researchDeclarations")
	if rows == nil || rows.kind != '[' && string(rows.raw) != "null" {
		return v, errAssistantDialectCompleteRead
	}
	if rows.kind == '[' {
		v.Inventory.Declarations = []runcontext.DialectResearchRevision{}
	}
	for _, row := range rows.elements {
		f, e := assistantDialectCompleteObject(row.raw, []string{"revision", "fields"})
		if e != nil {
			return v, e
		}
		var revision runcontext.DialectResearchRevision
		if json.Unmarshal(f["revision"], &revision.Revision) != nil {
			return v, errAssistantDialectCompleteRead
		}
		items := row.member("fields")
		if items.kind == '[' {
			revision.Fields = []runcontext.DialectResearchField{}
			for _, item := range items.elements {
				var field inputResearchField
				if json.Unmarshal(item.raw, &field) != nil {
					return v, errAssistantDialectCompleteRead
				}
				revision.Fields = append(revision.Fields, runcontext.DialectResearchField{Name: field.Name, FieldJSON: string(item.raw)})
			}
		} else if string(items.raw) != "null" {
			return v, errAssistantDialectCompleteRead
		}
		v.Inventory.Declarations = append(v.Inventory.Declarations, revision)
	}
	if assistantDialectCompleteShape(fields["originalSources"], reflect.TypeOf(v.Inventory.Sources), false) != nil || json.Unmarshal(fields["originalSources"], &v.Inventory.Sources) != nil {
		return v, errAssistantDialectCompleteRead
	}
	if err = v.validate(ctx); err != nil {
		return assistantDialectCompleteRead{}, err
	}
	return v, nil
}

// Closed final-012 controls, including its explicit nullable top-index triple.
// This is independent of the legacy factory codecs' v1/v2 migration work.
func assistantDialectCompleteShape(raw []byte, shape reflect.Type, nullable bool) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if nullable {
			return nil
		}
		return errAssistantDialectCompleteRead
	}
	for shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	switch shape.Kind() {
	case reflect.Struct:
		fields, err := assistantTestObject(raw)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for i := 0; i < shape.NumField(); i++ {
			f := shape.Field(i)
			name, options, _ := strings.Cut(f.Tag.Get("json"), ",")
			known[name] = true
			value, found := fields[name]
			if !found {
				if strings.Contains(options, "omitempty") {
					continue
				}
				return errAssistantDialectCompleteRead
			}
			null := shape == reflect.TypeOf(runcontext.DialectSnapshot{}) && name == "researchIndex" || shape == reflect.TypeOf(runcontext.DialectIndexRevision{}) && contains([]string{"fields", "digest", "bytes"}, name) || shape == reflect.TypeOf(assistantDialectCompleteSource{}) && name == "runRevision"
			if err = assistantDialectCompleteShape(value, f.Type, null); err != nil {
				return err
			}
		}
		for name := range fields {
			if !known[name] {
				return errAssistantDialectCompleteRead
			}
		}
	case reflect.Slice:
		rows, err := assistantChatArray(raw)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err = assistantDialectCompleteShape(row, shape.Elem(), false); err != nil {
				return err
			}
		}
	case reflect.Map:
		fields, err := assistantTestObject(raw)
		if err != nil {
			return err
		}
		for _, value := range fields {
			if err = assistantDialectCompleteShape(value, shape.Elem(), false); err != nil {
				return err
			}
		}
	default:
		if json.Unmarshal(raw, reflect.New(shape).Interface()) != nil {
			return errAssistantDialectCompleteRead
		}
	}
	return nil
}

func assistantDialectCompleteCanonicalBytes(value any) (int, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	parsed, err := runcontext.ParseJSON(raw)
	if err != nil {
		return 0, err
	}
	return len(runcontext.CanonicalJSON(parsed)), nil
}

func assistantDialectCompleteRevision(id string, version int) string {
	raw, _ := json.Marshal([]any{"desk-complete-original-recipe-v1", id, strconv.Itoa(version)})
	return "sha256:" + digestOf(raw)
}
