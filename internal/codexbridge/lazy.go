package codexbridge

import (
	"context"
	"sync"
)

// LazyManager lets an API-only Desk remain independent of the subscription
// profile. Catalog reads do not acquire its lease or launch a native process.
// The first account or run operation acquires one manager for this Desk.
type LazyManager struct {
	mu      sync.Mutex
	options Options
	manager *Manager
	closed  bool
}

func NewLazyManager(options Options) *LazyManager { return &LazyManager{options: options} }
func (l *LazyManager) get() (*Manager, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, ErrUnavailable
	}
	if l.manager == nil {
		manager, err := NewManager(l.options)
		if err != nil {
			return nil, err
		}
		l.manager = manager
	}
	return l.manager, nil
}
func (l *LazyManager) Status(ctx context.Context, owner string, refresh bool) (Status, error) {
	m, err := l.get()
	if err != nil {
		return Status{}, err
	}
	return m.Status(ctx, owner, refresh)
}
func (l *LazyManager) StartLogin(ctx context.Context, owner Owner, method string) (Challenge, error) {
	m, err := l.get()
	if err != nil {
		return Challenge{}, err
	}
	return m.StartLogin(ctx, owner, method)
}
func (l *LazyManager) CancelLogin(ctx context.Context, owner, id string) error {
	m, err := l.get()
	if err != nil {
		return err
	}
	return m.CancelLogin(ctx, owner, id)
}
func (l *LazyManager) Logout(ctx context.Context) error {
	m, err := l.get()
	if err != nil {
		return err
	}
	return m.Logout(ctx)
}
func (l *LazyManager) Models(ctx context.Context) ([]Model, error) {
	m, err := l.get()
	if err != nil {
		return nil, err
	}
	return m.Models(ctx)
}
func (l *LazyManager) Run(ctx context.Context, owner Owner, request RunRequest, emit func(RunEvent) error, execute ToolHandler) error {
	m, err := l.get()
	if err != nil {
		return err
	}
	return m.Run(ctx, owner, request, emit, execute)
}
func (l *LazyManager) Close() {
	l.mu.Lock()
	l.closed = true
	m := l.manager
	l.mu.Unlock()
	if m != nil {
		m.Close()
	}
}
