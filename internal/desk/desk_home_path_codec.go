package desk

// These codecs preserve local filesystem bytes, not custody or ownership.
// Unicode paths retain their existing JSON string spelling. Only a new v2
// envelope can carry a non-Unicode path, as one closed base64 value.
import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"
)

const homeTransactionLimit = 4 << 20

var errHomePathCodec = errors.New("Desk home persistence data is invalid.")

type homePath string

func (p homePath) MarshalJSON() ([]byte, error) {
	if utf8.ValidString(string(p)) {
		return json.Marshal(string(p))
	}
	return json.Marshal(struct {
		Bytes []byte `json:"bytes"`
	}{[]byte(p)})
}

func (p *homePath) UnmarshalJSON(raw []byte) error {
	var value string
	if len(raw) != 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil || !utf8.ValidString(value) {
			return errHomePathCodec
		}
		canonical, _ := json.Marshal(value)
		if !bytes.Equal(raw, canonical) {
			return errHomePathCodec
		}
	} else {
		var encoded struct {
			Bytes []byte `json:"bytes"`
		}
		if err := readClosedHomeJSON(raw, &encoded); err != nil || encoded.Bytes == nil || utf8.Valid(encoded.Bytes) {
			return errHomePathCodec
		}
		value = string(encoded.Bytes)
	}
	*p = homePath(value)
	return nil
}

// New v2 forms have one canonical spelling and exact recursive controls. V1
// deliberately stays on its original decoder, including accepted whitespace.
func readClosedHomeJSON(raw []byte, value any) error {
	if checkJSONMembers(raw, reflect.TypeOf(value)) != nil || decodeDataJSON(raw, value) != nil {
		return errHomePathCodec
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(raw, canonical) {
		return errHomePathCodec
	}
	return nil
}

func homeEnvelopeVersion(raw []byte) (int, error) {
	var header struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(raw, &header) != nil || header.Version != 1 && header.Version != 2 {
		return 0, errHomePathCodec
	}
	return header.Version, nil
}

type homeRenameV2 struct {
	From     homePath `json:"from"`
	To       homePath `json:"to"`
	Identity string   `json:"identity"`
}
type homeWriteV2 struct {
	Path       homePath `json:"path"`
	Before     string   `json:"before"`
	Data       []byte   `json:"data"`
	Executable bool     `json:"executable,omitempty"`
}
type homeRunnerRelocationV2 struct {
	Version           int      `json:"version"`
	ID                string   `json:"id"`
	Dir               homePath `json:"dir"`
	Workspace         string   `json:"workspace"`
	Owner             string   `json:"owner"`
	PreviousInputRoot homePath `json:"previousInputRoot"`
	InputRoot         homePath `json:"inputRoot"`
}
type homeTransactionV2 struct {
	RunnerBinary      homePath                     `json:"runnerBinary,omitempty"`
	RunnerDigest      string                       `json:"runnerDigest,omitempty"`
	RunnerRelocations []homeRunnerRelocationV2     `json:"runnerRelocations,omitempty"`
	Version           int                          `json:"version"`
	Destination       homePath                     `json:"destination"`
	Config            homePath                     `json:"config"`
	Prelude           []homeWriteV2                `json:"prelude,omitempty"`
	Renames           []homeRenameV2               `json:"renames"`
	Writes            []homeWriteV2                `json:"writes"`
	Parent            homePath                     `json:"parent"`
	ParentIdentity    string                       `json:"parentIdentity"`
	Complete          bool                         `json:"complete"`
	Retained          *dialectArtifactHomeMovePlan `json:"retained,omitempty"`
}

func homeTransactionHasRawPath(tx deskHomeTransaction) bool {
	paths := []string{tx.Destination, tx.Config, tx.Parent, tx.RunnerBinary}
	for _, op := range tx.Renames {
		paths = append(paths, op.From, op.To)
	}
	for _, write := range tx.Prelude {
		paths = append(paths, write.Path)
	}
	for _, write := range tx.Writes {
		paths = append(paths, write.Path)
	}
	for _, request := range tx.RunnerRelocations {
		paths = append(paths, request.Dir, request.PreviousInputRoot, request.InputRoot)
	}
	for _, path := range paths {
		if !utf8.ValidString(path) {
			return true
		}
	}
	return false
}

func homeWriteToV2(write homeWrite) homeWriteV2 {
	data := write.Data
	if data == nil {
		data = []byte{}
	}
	return homeWriteV2{homePath(write.Path), write.Before, data, write.Executable}
}
func (tx deskHomeTransaction) MarshalJSON() ([]byte, error) {
	if tx.Version == 1 && tx.Retained == nil && !homeTransactionHasRawPath(tx) {
		type legacy deskHomeTransaction
		return json.Marshal(legacy(tx))
	}
	if tx.Version != 1 && tx.Version != 2 {
		return nil, errHomePathCodec
	}
	wire := homeTransactionV2{Version: 2, Destination: homePath(tx.Destination), Config: homePath(tx.Config), Parent: homePath(tx.Parent), ParentIdentity: tx.ParentIdentity,
		RunnerBinary: homePath(tx.RunnerBinary), RunnerDigest: tx.RunnerDigest, Complete: tx.Complete, Retained: tx.Retained,
		Renames: make([]homeRenameV2, 0, len(tx.Renames)), Writes: make([]homeWriteV2, 0, len(tx.Writes))}
	for _, op := range tx.Renames {
		wire.Renames = append(wire.Renames, homeRenameV2{homePath(op.From), homePath(op.To), op.Identity})
	}
	for _, write := range tx.Writes {
		wire.Writes = append(wire.Writes, homeWriteToV2(write))
	}
	for _, write := range tx.Prelude {
		wire.Prelude = append(wire.Prelude, homeWriteToV2(write))
	}
	for _, request := range tx.RunnerRelocations {
		wire.RunnerRelocations = append(wire.RunnerRelocations, homeRunnerRelocationV2{request.Version, request.ID, homePath(request.Dir), request.Workspace, request.Owner, homePath(request.PreviousInputRoot), homePath(request.InputRoot)})
	}
	return json.Marshal(wire)
}

func homeWriteFromV2(write homeWriteV2) homeWrite {
	return homeWrite{Path: string(write.Path), Before: write.Before, Data: write.Data, Executable: write.Executable}
}
func decodeHomeTransaction(raw []byte) (*deskHomeTransaction, error) {
	if len(raw) > homeTransactionLimit {
		return nil, errHomePathCodec
	}
	version, err := homeEnvelopeVersion(raw)
	if err != nil {
		return nil, err
	}
	var tx deskHomeTransaction
	if version == 1 {
		members, _, e := topLevelMembers(raw)
		if e != nil {
			return nil, e
		}
		for _, member := range members {
			if strings.EqualFold(member.name, "retained") {
				return nil, errHomePathCodec
			}
		}
		// No new canonical, duplicate, presence or path rule applies to old v1.
		if decodeDataJSON(raw, &tx) != nil {
			return nil, errHomePathCodec
		}
	} else {
		var wire homeTransactionV2
		if readClosedHomeJSON(raw, &wire) != nil || wire.Renames == nil || wire.Writes == nil {
			return nil, errHomePathCodec
		}
		tx = deskHomeTransaction{Version: 2, Destination: string(wire.Destination), Config: string(wire.Config), Parent: string(wire.Parent), ParentIdentity: wire.ParentIdentity,
			RunnerBinary: string(wire.RunnerBinary), RunnerDigest: wire.RunnerDigest, Complete: wire.Complete, Retained: wire.Retained, Renames: []homeRename{}, Writes: []homeWrite{}}
		for _, op := range wire.Renames {
			tx.Renames = append(tx.Renames, homeRename{string(op.From), string(op.To), op.Identity})
		}
		for _, write := range wire.Writes {
			if write.Data == nil {
				return nil, errHomePathCodec
			}
			tx.Writes = append(tx.Writes, homeWriteFromV2(write))
		}
		for _, write := range wire.Prelude {
			if write.Data == nil {
				return nil, errHomePathCodec
			}
			tx.Prelude = append(tx.Prelude, homeWriteFromV2(write))
		}
		for _, request := range wire.RunnerRelocations {
			if request.Version != 1 || !validProjectHistoryPath(string(request.Dir)) || !validProjectHistoryPath(string(request.PreviousInputRoot)) || !validProjectHistoryPath(string(request.InputRoot)) {
				return nil, errHomePathCodec
			}
			tx.RunnerRelocations = append(tx.RunnerRelocations, homeRunnerRelocation{request.Version, request.ID, string(request.Dir), request.Workspace, request.Owner, string(request.PreviousInputRoot), string(request.InputRoot)})
		}
		if tx.Parent != "" && !validProjectHistoryPath(tx.Parent) || tx.RunnerBinary != "" && !filepath.IsAbs(tx.RunnerBinary) {
			return nil, errHomePathCodec
		}
	}
	if !validProjectHistoryPath(tx.Config) || !validProjectHistoryPath(tx.Destination) || len(tx.Renames) > 10000 || len(tx.Writes) > 2000 || len(tx.Prelude) > 1 {
		return nil, errHomePathCodec
	}
	for _, op := range tx.Renames {
		if !validProjectHistoryPath(op.From) || !validProjectHistoryPath(op.To) || op.Identity == "" {
			return nil, errHomePathCodec
		}
	}
	for _, writes := range [][]homeWrite{tx.Prelude, tx.Writes} {
		for _, write := range writes {
			if !validProjectHistoryPath(write.Path) || write.Before == "" {
				return nil, errHomePathCodec
			}
		}
	}
	if tx.Retained != nil && !tx.Retained.valid(&tx) {
		return nil, errHomePathCodec
	}
	return &tx, nil
}

// The writer enforces the existing reader's aggregate/count bounds before a
// journal can be claimed. It never drops rows or raises capacity to fit v2.
func marshalHomeTransaction(tx deskHomeTransaction) ([]byte, error) {
	if tx.Retained != nil && !tx.Retained.valid(&tx) {
		return nil, errHomePathCodec
	}
	if len(tx.Renames) > 10000 || len(tx.Writes) > 2000 || len(tx.Prelude) > 1 {
		return nil, errHomePathCodec
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		return nil, err
	}
	if len(raw) > homeTransactionLimit {
		return nil, errHomePathCodec
	}
	return raw, nil
}

type homeEntryV2 struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Folder          string   `json:"folder"`
	Kind            string   `json:"kind"`
	HistoryIdentity homePath `json:"historyIdentity"`
	StorageOwner    string   `json:"storageOwner"`
	RunnerWorkspace string   `json:"runnerWorkspace"`
}
type homeRegistryV2 struct {
	Version int           `json:"version"`
	Owner   string        `json:"owner"`
	Startup string        `json:"startup"`
	Desks   []homeEntryV2 `json:"desks"`
}

func homeRegistryHasRawHistory(value deskHomeRegistry) bool {
	for _, entry := range value.Desks {
		if !utf8.ValidString(entry.HistoryIdentity) {
			return true
		}
	}
	return false
}
func (value deskHomeRegistry) MarshalJSON() ([]byte, error) {
	if value.Version == 1 && !homeRegistryHasRawHistory(value) {
		type legacy deskHomeRegistry
		return json.Marshal(legacy(value))
	}
	if value.Version != 1 && value.Version != 2 || validateHomeRegistry(value) != nil {
		return nil, errHomePathCodec
	}
	wire := homeRegistryV2{Version: 2, Owner: value.Owner, Startup: value.Startup, Desks: make([]homeEntryV2, 0, len(value.Desks))}
	for _, entry := range value.Desks {
		wire.Desks = append(wire.Desks, homeEntryV2{entry.ID, entry.Name, entry.Folder, entry.Kind, homePath(entry.HistoryIdentity), entry.StorageOwner, entry.RunnerWorkspace})
	}
	return json.Marshal(wire)
}

// Preserve the existing v1 transaction spelling, including its newline. A v2
// registry is canonical raw metadata; addJSON's appended newline is not its
// spelling and would make the new shared reader refuse the emitted record.
func (tx *deskHomeTransaction) addHomeRegistry(path string, before []byte, registry deskHomeRegistry) error {
	raw, err := json.Marshal(registry)
	if err != nil {
		return err
	}
	if registry.Version == 1 && !homeRegistryHasRawHistory(registry) {
		raw = append(raw, '\n')
	}
	revision := "absent"
	if before != nil {
		revision = digestOf(before)
	}
	tx.Writes = append(tx.Writes, homeWrite{Path: path, Before: revision, Data: raw})
	return nil
}

// All persisted registry readers use this decoder. An initial raw project
// retains its original history bytes; a later publication retains version 2.
func decodeHomeRegistry(raw []byte) (deskHomeRegistry, error) {
	var value deskHomeRegistry
	if len(raw) > 1<<20 {
		return value, errHomePathCodec
	}
	version, err := homeEnvelopeVersion(raw)
	if err != nil {
		return value, err
	}
	if version == 1 {
		if decodeDataJSON(raw, &value) != nil {
			return value, errHomePathCodec
		}
	} else {
		var wire homeRegistryV2
		if readClosedHomeJSON(raw, &wire) != nil || wire.Desks == nil {
			return value, errHomePathCodec
		}
		value = deskHomeRegistry{Version: 2, Owner: wire.Owner, Startup: wire.Startup, Desks: []deskHomeEntry{}}
		for _, entry := range wire.Desks {
			value.Desks = append(value.Desks, deskHomeEntry{entry.ID, entry.Name, entry.Folder, entry.Kind, string(entry.HistoryIdentity), entry.StorageOwner, entry.RunnerWorkspace})
		}
	}
	if err := validateHomeRegistry(value); err != nil {
		return value, err
	}
	return value, nil
}

// This is a token input, not a persisted path or an ownership grant. Bind the
// original bytes of every selected path, including external chat storage and
// the retained namespace, before any destination directory is created.
func homePlanSnapshot(plan deskHomePlan) ([]byte, error) {
	type entrySnapshot struct {
		ID              string
		Name            []byte
		Folder          homePath
		Kind            string
		HistoryIdentity homePath
		StorageOwner    string
		RunnerWorkspace string
	}
	type registrySnapshot struct {
		Version int
		Owner   string
		Startup string
		Desks   []entrySnapshot
	}
	type moveSnapshot struct {
		Label    []byte
		From     homePath
		To       homePath
		Identity string
	}
	registry := registrySnapshot{Version: plan.Registry.Version, Owner: plan.Registry.Owner, Startup: plan.Registry.Startup, Desks: []entrySnapshot{}}
	for _, entry := range plan.Registry.Desks {
		registry.Desks = append(registry.Desks, entrySnapshot{entry.ID, []byte(entry.Name), homePath(entry.Folder), entry.Kind, homePath(entry.HistoryIdentity), entry.StorageOwner, entry.RunnerWorkspace})
	}
	moves := make([]moveSnapshot, 0, len(plan.Moves))
	for _, move := range plan.Moves {
		moves = append(moves, moveSnapshot{[]byte(move.Label), homePath(move.From), homePath(move.To), move.Identity})
	}
	return json.Marshal(struct {
		Destination      homePath
		Config           homePath
		Parent           homePath
		ParentID         string
		Revision         string
		Registry         registrySnapshot
		Moves            []moveSnapshot
		Files            []string
		ChatPath         homePath
		ChatConversation string
		RetainedSnapshot string
	}{homePath(plan.Destination), homePath(plan.Config), homePath(plan.Parent), plan.ParentIdentity, plan.SourceRevision, registry, moves, plan.ChatFiles, homePath(plan.ChatPath), plan.ChatConversation, homeMoveSnapshotDigest(plan.Retained)})
}
