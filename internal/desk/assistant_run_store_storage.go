package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"io"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"
)

var assistantRunFileName = regexp.MustCompile(`^assistant-runs-[a-f0-9]{64}\.json$`)

func assistantRunNamespace(root *os.Root, identity string) (string, error) {
	name, err := resolveConversationName(root, identity)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimPrefix(name, "conversations-"), ".json"), nil
}
func decodeAssistantRunStorage(name string, data []byte) (assistantRunLedger, error) {
	var ledger assistantRunLedger
	if !assistantRunFileName.MatchString(name) || len(data) > assistantRunRecordLimit {
		return ledger, errAssistantRunInvalid
	}
	if err := assistantRunJSONMembers(data); err != nil {
		return ledger, err
	}
	if err := decodeDataJSON(data, &ledger); err != nil {
		return ledger, err
	}
	// Validate original checkpoint bytes before generated Go structs can fill in
	// missing fields or apply their JSON decoding behavior.
	var wire struct {
		Runs []struct {
			Binding    assistantRunBinding `json:"binding"`
			Checkpoint json.RawMessage     `json:"checkpoint"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return ledger, err
	}
	for _, run := range wire.Runs {
		if len(run.Checkpoint) > 0 && !bytes.Equal(bytes.TrimSpace(run.Checkpoint), []byte("null")) {
			value, err := runcontext.ParseJSON(run.Checkpoint)
			if err != nil {
				return ledger, err
			}
			cp, err := runcontext.ReadExecution(value)
			if err != nil {
				return ledger, err
			}
			if cp == nil || cp.RequestKey != run.Binding.ExecutionKey || cp.Phase != run.Binding.Phase || cp.Unit != run.Binding.Unit {
				return ledger, errAssistantRunScope
			}
		}
	}
	project := strings.TrimSuffix(strings.TrimPrefix(name, "assistant-runs-"), ".json")
	if err := validateAssistantRunLedger(ledger, project); err != nil {
		return ledger, err
	}
	return ledger, nil
}

// Match declared members exactly while leaving arbitrary document/fact maps
// case sensitive. encoding/json's struct aliases must not silently collapse
// project/Project, state/State or canonical checkpoint allowance members.
func assistantRunJSONField(shape reflect.Type, key string) (reflect.Type, error) {
	for shape != nil && shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	if shape == nil || shape == reflect.TypeOf(json.RawMessage{}) {
		return nil, nil
	}
	if shape.Kind() == reflect.Map {
		return shape.Elem(), nil
	}
	if shape.Kind() != reflect.Struct {
		return nil, nil
	}
	fields := []reflect.StructField{}
	for i := 0; i < shape.NumField(); i++ {
		field := shape.Field(i)
		if shape == reflect.TypeOf(contract.RunEvent{}) {
			leg := field.Type.Elem()
			for j := 0; j < leg.NumField(); j++ {
				fields = append(fields, leg.Field(j))
			}
		} else {
			fields = append(fields, field)
		}
	}
	for _, field := range fields {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if key == name {
			return field.Type, nil
		}
	}
	for _, field := range fields {
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" && strings.EqualFold(key, name) {
			return nil, errAssistantRunInvalid
		}
	}
	if shape.PkgPath() == reflect.TypeOf(assistantRunLedger{}).PkgPath() {
		return nil, errAssistantRunInvalid
	}
	return nil, nil // Canonical open objects retain unknown members as data.
}
func assistantRunJSONMembers(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var read func(int, reflect.Type) error
	read = func(depth int, shape reflect.Type) error {
		if depth > 64 {
			return errAssistantRunInvalid
		}
		for shape != nil && shape.Kind() == reflect.Pointer {
			shape = shape.Elem()
		}
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
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errAssistantRunInvalid
				}
				seen[name] = true
				next, err := assistantRunJSONField(shape, name)
				if err != nil {
					return err
				}
				if err = read(depth+1, next); err != nil {
					return err
				}
			}
		case '[':
			var next reflect.Type
			if shape != nil && (shape.Kind() == reflect.Slice || shape.Kind() == reflect.Array) {
				next = shape.Elem()
			}
			for decoder.More() {
				if err := read(depth+1, next); err != nil {
					return err
				}
			}
		default:
			return errAssistantRunInvalid
		}
		_, err = decoder.Token()
		return err
	}
	if err := read(0, reflect.TypeOf(assistantRunLedger{})); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errAssistantRunInvalid
	}
	return nil
}

// Imported state is historical, never an executable approval/checkpoint. Keep
// bindings, exact receipts and canonical grants as evidence, not authority.
func interruptAssistantRuns(ledger *assistantRunLedger, historical bool) bool {
	changed := false
	for i := range ledger.Runs {
		run := &ledger.Runs[i]
		modified := false
		if historical && !run.Historical {
			run.Historical = true
			modified = true
		}
		if run.State == "admitted" || run.State == "running" || historical && !assistantRunTerminal(run.State) {
			run.State = "interrupted"
			modified = true
		}
		for j := range run.Operations {
			if run.Operations[j].State == "dispatched" {
				run.Operations[j].State = "uncertain"
				modified = true
			}
		}
		if run.Checkpoint != nil {
			if run.Checkpoint.Status == "running" || historical && run.Checkpoint.Status == "paused" {
				run.Checkpoint.Status = "stopped"
				modified = true
			}
			for j := range run.Checkpoint.Operations {
				if run.Checkpoint.Operations[j].Status == "running" {
					run.Checkpoint.Operations[j].Status = "interrupted"
					modified = true
				}
			}
		}
		if modified {
			run.Revision++
			changed = true
		}
	}
	return changed
}
func historicalAssistantRunStorage(name string, data []byte) ([]byte, error) {
	ledger, err := decodeAssistantRunStorage(name, data)
	if err != nil {
		return nil, err
	}
	if !interruptAssistantRuns(&ledger, true) {
		return data, nil
	}
	updated, err := json.Marshal(ledger)
	if err != nil {
		return nil, err
	}
	if _, err = decodeAssistantRunStorage(name, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Server) authorizeAssistantRunStorage(r *http.Request) error {
	if err := s.authorizePrivateWorkspace(r); err != nil {
		return err
	}
	// Records can outlive a Policy-linked chat and carry excerpts in exact
	// results. Opaque binding hashes cannot classify or confer Policy rights.
	return s.authorizePolicyHistory(r)
}
func (s *Server) assistantRunRecoveryFailure() error {
	s.assistantRunRecoveryMu.Lock()
	defer s.assistantRunRecoveryMu.Unlock()
	return s.assistantRunRecoveryErr
}
func (s *Server) setAssistantRunRecoveryFailure(err error) {
	s.assistantRunRecoveryMu.Lock()
	defer s.assistantRunRecoveryMu.Unlock()
	s.assistantRunRecoveryErr = err
}

// Startup is bounded and releases both leases even when there is no run file.
// Reacquisition repeats recovery before returning a dispatch-capable lease.
// No process is launched, and a second server cannot recover a live owner.
func (s *Server) recoverAssistantRuns() {
	if !s.assistant.usable() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Do not turn a blank or chat-only legacy startup into a storage migration.
	present, err := func() (bool, error) {
		lock, err := s.privateDataLockContext(ctx, true)
		if err != nil {
			return false, err
		}
		defer lock.Close()
		root, err := s.storageSettingsRoot()
		if err != nil {
			return false, err
		}
		defer root.Close()
		if _, err = readPrivateData(root, dataLocationName, 16<<10); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		entries, err := storageEntries(root)
		if err != nil {
			return false, err
		}
		for _, entry := range entries {
			if assistantRunFileName.MatchString(entry.Name()) {
				return true, nil
			}
		}
		return false, nil
	}()
	if err == nil && !present {
		return
	}
	if err == nil {
		var lease *assistantRunLease
		lease, err = s.assistantRuns().Acquire(ctx)
		if err == nil {
			err = lease.Close()
		}
	}
	if err != nil {
		s.setAssistantRunRecoveryFailure(err)
		s.log.Print("desk: assistant run recovery unavailable; finish active work or check private storage before starting another run")
	}
}

func (s assistantRunStore) checkScope(root *os.Root) error {
	if s.problem != nil {
		return s.problem
	}
	if s.server == nil || !assistantRunFileName.MatchString(s.name) || s.name != "assistant-runs-"+s.project+".json" {
		return errAssistantRunScope
	}
	project, err := assistantRunNamespace(root, s.server.conversationIdentity())
	if err != nil {
		return err
	}
	if project != s.project {
		return errAssistantRunScope
	}
	return nil
}

var errAssistantRunRecovery = errors.New("Assistant run recovery is incomplete; finish active work or check private storage before starting another run.")
