package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const signInFileName = "sign-in.json"
const signInAttemptLifetime = 5 * time.Minute

type signInRecord struct {
	Version  int            `json:"version"`
	Provider signInProvider `json:"provider"`
	Owner    signInOwner    `json:"owner"`
}
type signInAttempt struct {
	id, state, proof, nonce, verifier      string
	origin, returnPath, admin              string
	expires                                time.Time
	generation                             uint64
	provider                               signInProvider
	protocol                               *signInProtocol
	processing, finished, consumed, tested bool
	owner                                  signInOwner
	problem                                string
}
type signInState struct {
	mu          sync.Mutex
	active      *signInRecord
	problem     bool
	generation  uint64
	attempts    map[string]*signInAttempt
	lastTest    map[string]string
	limitStart  time.Time
	limitCount  int
	slots       chan struct{}
	epoch       context.Context
	cancelEpoch context.CancelFunc
}

func (s *Server) openSignIn() {
	s.signIn = &signInState{attempts: make(map[string]*signInAttempt), lastTest: make(map[string]string), slots: make(chan struct{}, 4)}
	s.signIn.epoch, s.signIn.cancelEpoch = context.WithCancel(context.Background())
	record, err := s.assistant.readSignIn()
	if err != nil {
		// A missing store is setup-required; an existing unreadable record must
		// never silently disable enforcement. No provider secret appears in logs.
		_, statErr := os.Lstat(filepath.Join(s.configDir, secretsDirName, signInFileName))
		s.signIn.problem = !errors.Is(statErr, fs.ErrNotExist)
		if s.signIn.problem {
			s.log.Print("desk: sign-in configuration cannot be read; access remains locked")
		}
		return
	}
	s.signIn.active = record
}
func (s *Server) SignInRequired() bool {
	if s.signIn == nil {
		return false
	}
	s.signIn.mu.Lock()
	defer s.signIn.mu.Unlock()
	return s.signIn.active != nil || s.signIn.problem
}
func (s *Server) signInSessionAllowed(held session) bool {
	if s.signIn == nil {
		return true
	}
	s.signIn.mu.Lock()
	defer s.signIn.mu.Unlock()
	if s.signIn.problem {
		return false
	}
	if s.signIn.active == nil {
		return held.issuer == nil
	}
	owner := s.signIn.active.Owner
	return held.issuer != nil && *held.issuer == owner.Issuer && held.subject == owner.Subject
}
func (s *assistantStore) readSignIn() (*signInRecord, error) {
	if !s.usable() {
		return nil, s.problem
	}
	info, err := s.secrets.Lstat(signInFileName)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("sign-in configuration is a link")
	}
	if err := ownerOnlyFile(signInFileName, info.Mode()); err != nil {
		return nil, err
	}
	if err := ownedByUs(signInFileName, info); err != nil {
		return nil, err
	}
	file, err := s.secrets.OpenFile(signInFileName, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("sign-in configuration changed while opening")
	}
	if err := ownerOnlyFile(signInFileName, opened.Mode()); err != nil {
		return nil, err
	}
	if err := ownedByUs(signInFileName, opened); err != nil {
		return nil, err
	}
	raw, err := readBounded(file, 16<<10)
	if err != nil {
		return nil, err
	}
	var record signInRecord
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return nil, errors.New("invalid sign-in configuration")
	}
	if decoder.Decode(new(any)) != io.EOF || record.Version != 1 || record.Provider.validate() != nil || record.Owner.Subject == "" || len(record.Owner.Subject) > 512 || record.Owner.Issuer != record.Provider.Issuer {
		return nil, errors.New("invalid sign-in configuration")
	}
	return &record, nil
}
func (s *assistantStore) writeSignIn(record signInRecord) error {
	if !s.usable() {
		return s.problem
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, name, err := s.stage()
	if err != nil {
		return err
	}
	defer s.secrets.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = s.secrets.Rename(name, signInFileName); err != nil {
		return err
	}
	if dir, err := s.secrets.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// ResetSignIn is deliberately a local operator command, never an HTTP route.
// The running server must be stopped first; no packs or source credentials move.
func ResetSignIn() error {
	store := openAssistantStore(configDirFor(""))
	defer store.Close()
	if !store.usable() {
		return store.problem
	}
	err := store.secrets.Remove(signInFileName)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
