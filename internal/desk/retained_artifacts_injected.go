package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"sync/atomic"
)

const (
	retainedArtifactInjectedIdentityBytes = 159
	retainedArtifactInjectedBindingBytes  = 393
)

// These codecs describe local immutable provenance. They are neither provider
// locators nor custody tokens. Only trusted construction selects a root.
type retainedArtifactInjectedIdentity struct {
	Version               int    `json:"version"`
	Provider              string `json:"provider"`
	StorageFolderIdentity string `json:"storageFolderIdentity"`
}

type retainedArtifactInjectedBinding struct {
	Version               int    `json:"version"`
	Provider              string `json:"provider"`
	Owner                 string `json:"owner"`
	ProjectFolderIdentity string `json:"projectFolderIdentity"`
	WorkDomain            string `json:"workDomain"`
	Namespace             string `json:"namespace"`
}

func (v retainedArtifactInjectedIdentity) valid() bool {
	return v.Version == 1 && assistantRunDigest(v.Provider) && dialectArtifactOwnerFileIdentity(v.StorageFolderIdentity)
}
func (v retainedArtifactInjectedBinding) valid() bool {
	return v.Version == 1 && assistantRunDigest(v.Provider) && assistantRunDigest(v.Owner) &&
		dialectArtifactOwnerFileIdentity(v.ProjectFolderIdentity) && assistantRunDigest(v.WorkDomain) && assistantRunDigest(v.Namespace)
}
func readRetainedArtifactInjectedIdentity(raw []byte) (v retainedArtifactInjectedIdentity, err error) {
	err = readDialectArtifactOwnerJSON(raw, retainedArtifactInjectedIdentityBytes, &v)
	if err == nil && !v.valid() {
		err = errDialectArtifactOwner
	}
	return v, err
}
func readRetainedArtifactInjectedBinding(raw []byte) (v retainedArtifactInjectedBinding, err error) {
	err = readDialectArtifactOwnerJSON(raw, retainedArtifactInjectedBindingBytes, &v)
	if err == nil && !v.valid() {
		err = errDialectArtifactOwner
	}
	return v, err
}

// A finite selection is copied and closed before any disk lookup. It applies
// existing archive request limits; it imposes no lifetime storage quota.
type retainedArtifactInjectedSelection struct {
	refs    []retainedArtifactRef
	restore bool
}

func newRetainedArtifactInjectedSelection(refs []retainedArtifactRef, restore bool) (retainedArtifactInjectedSelection, error) {
	if len(refs) > 4096 {
		return retainedArtifactInjectedSelection{}, errDialectArtifactOwner
	}
	copied := append([]retainedArtifactRef(nil), refs...)
	sort.Slice(copied, func(i, j int) bool { return copied[i].SHA256 < copied[j].SHA256 })
	total := int64(0)
	for i, ref := range copied {
		if !ref.valid() || i > 0 && ref.SHA256 == copied[i-1].SHA256 {
			return retainedArtifactInjectedSelection{}, errDialectArtifactOwner
		}
		total += int64(ref.Bytes)
		if total > 128<<20 {
			return retainedArtifactInjectedSelection{}, errDialectArtifactOwner
		}
	}
	raw, err := json.Marshal(copied)
	if err != nil || len(raw) > 1<<20 {
		return retainedArtifactInjectedSelection{}, errors.Join(errDialectArtifactOwner, err)
	}
	return retainedArtifactInjectedSelection{refs: copied, restore: restore}, nil
}
func (s retainedArtifactInjectedSelection) contains(ref retainedArtifactRef) bool {
	i := sort.Search(len(s.refs), func(i int) bool { return s.refs[i].SHA256 >= ref.SHA256 })
	return i < len(s.refs) && s.refs[i] == ref
}

// Constructor-produced instances cannot be copied into a current registration.
// active pins keep owned roots alive; revocation is visible without host reentry.
type persistentInjectedArtifactProvider struct {
	self      *persistentInjectedArtifactProvider
	id        string
	root      *os.Root
	info      os.FileInfo
	identity  []byte
	mu        sync.Mutex
	active    sync.WaitGroup
	revoked   atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

type retainedArtifactProviderRegistration struct {
	provider *persistentInjectedArtifactProvider
	root     *os.Root
	info     os.FileInfo
	identity []byte
	registry *retainedArtifactProviderRegistry
}

type retainedArtifactProviderRegistry struct {
	byID      map[string]*retainedArtifactProviderRegistration
	closeOnce sync.Once
	closeErr  error
}

func newRetainedArtifactProviderRegistry(providers ...*persistentInjectedArtifactProvider) (*retainedArtifactProviderRegistry, error) {
	r := &retainedArtifactProviderRegistry{byID: make(map[string]*retainedArtifactProviderRegistration, len(providers))}
	seen := make(map[*persistentInjectedArtifactProvider]bool, len(providers))
	for _, p := range providers {
		if p == nil || p.self != p || !assistantRunDigest(p.id) || p.revoked.Load() || seen[p] || r.byID[p.id] != nil {
			return nil, errRetainedArtifactAuthorityUnavailable
		}
		op, err := p.pin()
		if err != nil {
			return nil, err
		}
		checkErr := p.check(context.Background())
		if err = errors.Join(checkErr, op.finish()); err != nil {
			return nil, err
		}
		seen[p] = true
		r.byID[p.id] = &retainedArtifactProviderRegistration{provider: p, root: p.root, info: p.info, identity: bytes.Clone(p.identity), registry: r}
	}
	return r, nil
}
func (r *retainedArtifactProviderRegistry) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		for _, registration := range r.byID {
			r.closeErr = errors.Join(r.closeErr, registration.provider.Close())
		}
	})
	return r.closeErr
}
func (r *retainedArtifactProviderRegistration) current() error {
	if r == nil || r.registry == nil || r.provider == nil {
		return errRetainedArtifactAuthorityUnavailable
	}
	p := r.provider
	if p.self != p || p.revoked.Load() || r.registry.byID[p.id] != r || p.root != r.root || !bytes.Equal(p.identity, r.identity) {
		return errRetainedArtifactAuthorityUnavailable
	}
	info, err := p.root.Stat(".")
	if err != nil || !os.SameFile(info, r.info) || !info.IsDir() || info.Mode().Perm()&0077 != 0 || ownedByUs("injected provider", info) != nil {
		return errors.Join(errRetainedArtifactAuthorityUnavailable, err)
	}
	return nil
}

// A pin is obtained before host admission, and finishes only after every owned
// handle closes. TryLock gives acknowledgment and Close a definite order without
// introducing a lifecycle wait while host custody is held.
type retainedArtifactInjectedOperation struct {
	provider *persistentInjectedArtifactProvider
	finished atomic.Bool
	once     sync.Once
	err      error
}

func (p *persistentInjectedArtifactProvider) pin() (*retainedArtifactInjectedOperation, error) {
	if p == nil || p.self != p || !p.mu.TryLock() {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	defer p.mu.Unlock()
	if p.revoked.Load() {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	p.active.Add(1)
	return &retainedArtifactInjectedOperation{provider: p}, nil
}
func (o *retainedArtifactInjectedOperation) finish() error {
	if o == nil {
		return errRetainedArtifactAuthorityUnavailable
	}
	o.once.Do(func() {
		o.finished.Store(true)
		p := o.provider
		if !p.mu.TryLock() {
			o.err = errRetainedArtifactAuthorityUnavailable
		} else {
			if p.revoked.Load() {
				o.err = errRetainedArtifactAuthorityUnavailable
			}
			p.mu.Unlock()
		}
		p.active.Done()
	})
	return o.err
}
func (p *persistentInjectedArtifactProvider) Close() error {
	if p == nil || p.self != p {
		return errRetainedArtifactAuthorityUnavailable
	}
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.revoked.Store(true)
		p.mu.Unlock()
		p.closeErr = dialectArtifactOwnerStep(context.Background(), "injected-provider-revoked")
		p.active.Wait()
		p.closeErr = errors.Join(p.closeErr, closeDialectArtifactOwner(context.Background(), "injected-provider-root", p.root.Close))
	})
	return p.closeErr
}

// Ordinary non-D5 features have a separate object domain. Neither generic
// method can guess a D5 scope or create a publication/transfer capability.
func (p *persistentInjectedArtifactProvider) Get(ctx context.Context, ref retainedArtifactRef) (raw []byte, err error) {
	if !ref.valid() {
		return nil, errRetainedArtifactChanged
	}
	op, err := p.pin()
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, op.finish())
		if err != nil {
			raw = nil
		}
	}()
	lock, err := p.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-compat-queue", lock.Close)) }()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return nil, err
	}
	if err = p.check(ctx); err != nil {
		return nil, err
	}
	objects, err := openDialectArtifactOwnerFolder(ctx, p.root, false, "injected-compat-folder", "compatibility-objects")
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-compat-folder", objects.Close))
	}()
	raw, err = readDialectArtifactOwnerFile(objects, ref.SHA256, ref.Bytes)
	if err == nil {
		err = verifyRetainedArtifact(ref, raw)
	}
	if err == nil {
		err = p.check(ctx)
	}
	return raw, err
}
func (p *persistentInjectedArtifactProvider) Put(ctx context.Context, raw []byte) (ref retainedArtifactRef, err error) {
	if len(raw) > retainedArtifactByteLimit {
		return ref, errRetainedArtifactChanged
	}
	raw = bytes.Clone(raw)
	op, err := p.pin()
	if err != nil {
		return ref, err
	}
	defer func() {
		err = errors.Join(err, op.finish())
		if err != nil {
			ref = retainedArtifactRef{}
		}
	}()
	lock, err := p.root.Open(".")
	if err != nil {
		return ref, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-compat-queue", lock.Close)) }()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return ref, err
	}
	check := func() error { return p.check(ctx) }
	if err = check(); err != nil {
		return ref, err
	}
	held, err := heldInjectedArtifactCreator(p.root, lock)
	if err != nil {
		return ref, err
	}
	objects, err := openDialectArtifactOwnerFolderHeld(ctx, p.root, true, "injected-compat-folder", held, "compatibility-objects")
	if err != nil {
		return ref, err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-compat-folder", objects.Close))
	}()
	expected := artifactReference(raw)
	if err = claimDialectArtifactOwner(ctx, objects, expected.SHA256, raw, "injected-compat-object", check); err != nil {
		return ref, err
	}
	return expected, nil
}
func (p *persistentInjectedArtifactProvider) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.self != p || p.revoked.Load() {
		return errRetainedArtifactAuthorityUnavailable
	}
	info, err := p.root.Stat(".")
	if err != nil || !os.SameFile(info, p.info) || !info.IsDir() || info.Mode().Perm()&0077 != 0 || ownedByUs("injected provider", info) != nil {
		return errors.Join(errRetainedArtifactAuthorityUnavailable, err)
	}
	raw, err := readDialectArtifactOwnerFile(p.root, "d5-provider.json", retainedArtifactInjectedIdentityBytes)
	if err != nil || !bytes.Equal(raw, p.identity) {
		return errors.Join(errRetainedArtifactAuthorityUnavailable, err)
	}
	return nil
}

// No unbound PutGuarded or WithAuthority capability is inferred from Get/Put.
// The future effect caller must bring a real D5 leaf guard to a bound owner.

// CREATE and OPEN are distinct trusted construction decisions. OPEN never
// recreates a missing identity; neither constructor scans stored scopes.
func newPersistentInjectedArtifactProvider(ctx context.Context, providerID string, root *os.Root) (*persistentInjectedArtifactProvider, error) {
	return constructPersistentInjectedArtifactProvider(ctx, providerID, root, true)
}
func openPersistentInjectedArtifactProvider(ctx context.Context, providerID string, root *os.Root) (*persistentInjectedArtifactProvider, error) {
	return constructPersistentInjectedArtifactProvider(ctx, providerID, root, false)
}
func constructPersistentInjectedArtifactProvider(ctx context.Context, providerID string, root *os.Root, create bool) (out *persistentInjectedArtifactProvider, err error) {
	if ctx == nil || root == nil || !assistantRunDigest(providerID) {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	owned, err := root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, owned.Close())
			out = nil
		}
	}()
	info, err := owned.Stat(".")
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || ownedByUs("injected provider", info) != nil {
		return nil, errors.Join(errRetainedArtifactAuthorityUnavailable, err)
	}
	identity := retainedArtifactInjectedIdentity{Version: 1, Provider: providerID, StorageFolderIdentity: identityKey(info)}
	if !identity.valid() {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	raw, err := json.Marshal(identity)
	if err != nil || len(raw) > retainedArtifactInjectedIdentityBytes {
		return nil, errors.Join(errRetainedArtifactAuthorityUnavailable, err)
	}
	lock, err := owned.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-constructor-queue", lock.Close))
		if err != nil {
			out = nil
		}
	}()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return nil, err
	}
	prior, readErr := readDialectArtifactOwnerFile(owned, "d5-provider.json", retainedArtifactInjectedIdentityBytes)
	if create {
		if !injectedArtifactOnlyNotExist(readErr) {
			return nil, errors.Join(errRetainedArtifactAuthorityUnavailable, readErr)
		}
	} else {
		decoded, decodeErr := readRetainedArtifactInjectedIdentity(prior)
		if readErr != nil || decodeErr != nil || decoded != identity || !bytes.Equal(prior, raw) {
			return nil, errors.Join(errRetainedArtifactAuthorityUnavailable, readErr, decodeErr)
		}
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := owned.Stat(".")
		borrowed, borrowedErr := root.Stat(".")
		if err != nil || borrowedErr != nil || !os.SameFile(current, info) || !os.SameFile(borrowed, info) {
			return errors.Join(errRetainedArtifactAuthorityUnavailable, err, borrowedErr)
		}
		return nil
	}
	if err = claimDialectArtifactOwner(ctx, owned, "d5-provider.json", raw, "injected-identity", check); err != nil {
		return nil, err
	}
	p := &persistentInjectedArtifactProvider{id: providerID, root: owned, info: info, identity: bytes.Clone(raw)}
	p.self = p
	if err = p.check(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func (r *retainedArtifactProviderRegistration) drain(ctx context.Context) (err error) {
	if err = r.current(); err != nil {
		return err
	}
	op, err := r.provider.pin()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, op.finish()) }()
	return r.drainPinned(ctx, op)
}
func (r *retainedArtifactProviderRegistration) drainPinned(ctx context.Context, op *retainedArtifactInjectedOperation) (err error) {
	if r == nil || op == nil || op.provider != r.provider {
		return errRetainedArtifactAuthorityUnavailable
	}
	if err = r.current(); err != nil {
		return err
	}
	// The native helper waits on precisely this provider directory flock and
	// closes its directory handle before returning to host admission.
	root, err := r.root.OpenRoot(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-drain-root", root.Close)) }()
	if err = dialectArtifactOwnerStep(ctx, "injected-before-drain"); err != nil {
		return err
	}
	if err = drainRetainedArtifactQueue(ctx, root); err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(ctx, "injected-after-drain"); err != nil {
		return err
	}
	return r.current()
}

func readInjectedArtifactRow(ctx context.Context, root *os.Root, expected []byte, name string, parts ...string) (err error) {
	folder, err := openDialectArtifactOwnerFolder(ctx, root, false, "injected-selected-folder", parts...)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-selected-folder", folder.Close))
	}()
	raw, err := readInjectedArtifactExactFile(ctx, folder, name, len(expected), "injected-selected-file")
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, expected) {
		return errDialectArtifactOwner
	}
	return nil
}
func preflightInjectedArtifactRow(ctx context.Context, root *os.Root, expected []byte, name string, parts ...string) error {
	err := readInjectedArtifactRow(ctx, root, expected, name, parts...)
	if injectedArtifactOnlyNotExist(err) {
		return nil
	}
	return err
}
func claimInjectedArtifactRow(ctx context.Context, root *os.Root, expected []byte, name, label string, check func() error, parts ...string) (err error) {
	return claimInjectedArtifactRowPinned(ctx, root, expected, name, label, check, nil, nil, parts...)
}
func claimInjectedArtifactRowPinned(ctx context.Context, root *os.Root, expected []byte, name, label string, check func() error, pins *[]retainedArtifactInjectedEdge, held *storageCreatorLease, parts ...string) (err error) {
	folder, err := openDialectArtifactOwnerFolderHeld(ctx, root, true, label+"-folder", held, parts...)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, closeDialectArtifactOwner(ctx, label+"-folder", folder.Close))
		}
	}()
	edges, err := pinInjectedArtifactHeldEdges(ctx, root, folder, parts...)
	if err != nil {
		return err
	}
	if pins != nil {
		if err = mergeInjectedArtifactEdges(pins, edges); err != nil {
			return err
		}
	}
	if err = claimDialectArtifactOwner(ctx, folder, name, expected, label, check); err != nil {
		return err
	}
	closed = true
	if err = closeDialectArtifactOwner(ctx, label+"-folder", folder.Close); err != nil {
		return err
	}
	if err = checkInjectedArtifactEdges(ctx, edges); err != nil {
		return err
	}
	if err = readInjectedArtifactRow(ctx, root, expected, name, parts...); err != nil {
		return err
	}
	return check()
}

// D5 requires a real lexical host hold that separately exposes physical checks.
// A generic retainedArtifactGuard (including the existing factory guard) does
// not satisfy this private capability and cannot be called under this queue.
type retainedArtifactInjectedLeafGuard interface {
	retainedArtifactGuard
	injectedArtifactPhysicalGuard()
}

type retainedArtifactInjectedView struct {
	registration         *retainedArtifactProviderRegistration
	binding              retainedArtifactInjectedBinding
	ownerRaw, bindingRaw []byte
	scope                dialectArtifactOwnerScope
	scopeRaw             []byte
	selection            retainedArtifactInjectedSelection
	edges                []retainedArtifactInjectedEdge
	guard                *dialectArtifactInjectedAuthorityHold
	queue                *os.File
	ctx                  context.Context
	closed               bool
	held                 bool
	closeErr             error
}

func (r *retainedArtifactProviderRegistration) beginD5ViewLocked(ctx context.Context, binding retainedArtifactInjectedBinding, selection retainedArtifactInjectedSelection, guard retainedArtifactGuard) (out *retainedArtifactInjectedView, err error) {
	g, ok := guard.(*dialectArtifactInjectedAuthorityHold)
	if !ok || g.registration != r || g.operation == nil || g.operation.provider != r.provider || !binding.valid() {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	if g.selected != nil && g.selected.view != nil {
		return nil, errRetainedArtifactBusy
	}
	selected, err := newRetainedArtifactInjectedSelection(selection.refs, selection.restore)
	if err != nil {
		return nil, err
	}
	ownerRaw, err := json.Marshal(binding.ownerRecord())
	if err != nil {
		return nil, err
	}
	bindingRaw, err := json.Marshal(binding)
	if err != nil {
		return nil, err
	}
	scope := dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: binding.Owner, Namespace: binding.Namespace}
	if g.selected != nil {
		scope = g.selectedScope
		if !scope.valid() || scope.Owner != binding.Owner || scope.Namespace != binding.Namespace {
			return nil, errRetainedArtifactAuthorityUnavailable
		}
	}
	scopeRaw, err := json.Marshal(scope)
	if err != nil {
		return nil, err
	}
	lock, err := r.root.Open(".")
	if err != nil {
		return nil, err
	}
	v := &retainedArtifactInjectedView{registration: r, binding: binding, ownerRaw: ownerRaw, bindingRaw: bindingRaw, scope: scope, scopeRaw: scopeRaw, selection: selected, guard: g, queue: lock, ctx: ctx}
	if g.selected != nil {
		g.selected.view = v
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, v.Close())
			out = nil
		}
	}()
	if err = lockPrivateData(lock, true); err != nil {
		if lockHeld(err) {
			err = errors.Join(errRetainedArtifactBusy, err)
		}
		return nil, err
	}
	v.held = true
	if err = v.check(ctx); err != nil {
		return nil, err
	}
	return v, nil
}
func (b retainedArtifactInjectedBinding) ownerRecord() dialectArtifactInjectedOwnerRecord {
	return dialectArtifactInjectedOwnerRecord{Version: 2, Owner: b.Owner, Kind: "injected", Provider: b.Provider, ProjectFolderIdentity: b.ProjectFolderIdentity, WorkDomain: b.WorkDomain}
}
func (v *retainedArtifactInjectedView) check(ctx context.Context) error {
	if v == nil || v.closed || !v.held || ctx == nil {
		return errRetainedArtifactAuthorityUnavailable
	}
	if v.guard.selected != nil && (v.guard.selected.view != v || v.scope != v.guard.selectedScope) {
		return errRetainedArtifactAuthorityUnavailable
	}
	if err := v.guard.Check(ctx); err != nil {
		return err
	}
	if err := checkInjectedArtifactEdges(ctx, v.edges); err != nil {
		return err
	}
	if err := v.registration.current(); err != nil {
		return err
	}
	if err := v.registration.provider.check(ctx); err != nil {
		return err
	}
	b := v.binding
	if b.Provider != v.registration.provider.id || b.Owner != v.guard.binding.Owner || b != v.guard.binding {
		return errDialectArtifactOwner
	}
	if err := readInjectedArtifactRow(ctx, v.registration.root, v.ownerRaw, b.Owner[2:]+".json", "d5-owners", b.Owner[:2]); err != nil {
		return err
	}
	if v.scope.Kind == "native" {
		return readInjectedArtifactRow(ctx, v.registration.root, v.bindingRaw, b.Owner+".json", "d5-native", b.Namespace)
	}
	if v.guard.selected == nil || v.scope != v.guard.selectedScope || v.scope.Kind != "import" {
		return errRetainedArtifactAuthorityUnavailable
	}
	return readInjectedArtifactRow(ctx, v.registration.root, v.scopeRaw, b.Owner+".json", "d5-imports", b.Namespace, v.scope.RunID)
}
func (v *retainedArtifactInjectedView) GetExact(ctx context.Context, ref retainedArtifactRef) (raw []byte, err error) {
	defer func() {
		if err != nil {
			raw = nil
		}
	}()
	if v == nil || !v.selection.contains(ref) {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	if err = v.check(ctx); err != nil {
		return nil, err
	}
	b := v.binding
	objects, err := openDialectArtifactOwnerFolder(ctx, v.registration.root, false, "injected-object-folder", "d5-objects", b.Owner, b.Namespace)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-object-folder", objects.Close), v.check(ctx))
	}()
	pinned, err := pinInjectedArtifactHeldEdges(ctx, v.registration.root, objects, "d5-objects", b.Owner, b.Namespace)
	if err != nil {
		return nil, err
	}
	if err = mergeInjectedArtifactEdges(&v.edges, pinned); err != nil {
		return nil, err
	}
	raw, err = readInjectedArtifactExactFile(ctx, objects, ref.SHA256, ref.Bytes, "injected-object-read")
	if err == nil {
		err = verifyRetainedArtifact(ref, raw)
	}
	if err == nil {
		err = readInjectedArtifactRow(ctx, v.registration.root, raw, ref.SHA256, "d5-objects", b.Owner, b.Namespace)
	}
	return raw, err
}
func (v *retainedArtifactInjectedView) PublishExact(ctx context.Context, ref retainedArtifactRef, raw []byte) error {
	if v == nil || !v.selection.restore || !v.selection.contains(ref) {
		return errRetainedArtifactAuthorityUnavailable
	}
	if len(raw) != ref.Bytes || len(raw) > retainedArtifactByteLimit {
		return errRetainedArtifactChanged
	}
	raw = bytes.Clone(raw)
	if err := verifyRetainedArtifact(ref, raw); err != nil {
		return err
	}
	if err := v.check(ctx); err != nil {
		return err
	}
	b := v.binding
	// The view's queue is the exclusive lock on the registration folder; the
	// object folders are made under it.
	held, err := heldInjectedArtifactCreator(v.registration.root, v.queue)
	if err != nil {
		return err
	}
	return claimInjectedArtifactRowPinned(ctx, v.registration.root, raw, ref.SHA256, "injected-object", func() error { return v.check(ctx) }, &v.edges, held, "d5-objects", b.Owner, b.Namespace)
}
func (v *retainedArtifactInjectedView) Close() error {
	if v == nil {
		return nil
	}
	if v.closed {
		return v.closeErr
	}
	if v.guard.selected != nil && v.guard.selected.view != v {
		return errRetainedArtifactAuthorityUnavailable
	}
	if v.held {
		v.closeErr = v.check(v.ctx)
	}
	v.closed = true
	v.held = false
	v.closeErr = errors.Join(v.closeErr, closeDialectArtifactOwner(v.ctx, "injected-view-queue", v.queue.Close))
	if v.guard.selected != nil {
		v.guard.selected.view = nil
	}
	// The guard and operation are borrowed from the lexical caller. Exclusion
	// ends before authority locks or the original host descriptors can end.
	return v.closeErr
}

type retainedArtifactInjectedEdge struct {
	root  *os.Root
	parts []string
	info  os.FileInfo
}

func pinInjectedArtifactHeldEdges(ctx context.Context, root, held *os.Root, parts ...string) (edges []retainedArtifactInjectedEdge, err error) {
	for i := range parts {
		prefix := append([]string(nil), parts[:i+1]...)
		fresh, openErr := openDialectArtifactOwnerFolder(ctx, root, false, "injected-edge-folder", prefix...)
		if openErr != nil {
			return nil, openErr
		}
		info, statErr := fresh.Stat(".")
		closeErr := closeDialectArtifactOwner(ctx, "injected-edge-folder", fresh.Close)
		if statErr != nil || closeErr != nil {
			return nil, errors.Join(statErr, closeErr)
		}
		edges = append(edges, retainedArtifactInjectedEdge{root: root, parts: prefix, info: info})
	}
	if len(edges) != 0 {
		info, statErr := held.Stat(".")
		if statErr != nil || !os.SameFile(info, edges[len(edges)-1].info) {
			return nil, errors.Join(errDialectArtifactOwner, statErr)
		}
	}
	return edges, nil
}
func prepareInjectedArtifactEdges(ctx context.Context, root *os.Root, held *storageCreatorLease, parts ...string) (edges []retainedArtifactInjectedEdge, err error) {
	folder, err := openDialectArtifactOwnerFolderHeld(ctx, root, true, "injected-edge-setup", held, parts...)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-edge-setup", folder.Close))
		if err != nil {
			edges = nil
		}
	}()
	return pinInjectedArtifactHeldEdges(ctx, root, folder, parts...)
}

// heldInjectedArtifactCreator names the caller's exclusive queue lock on the
// provider folder as the creator lease for folders made directly under it.
// The lease is borrowed, never closed: the queue's own close releases it.
func heldInjectedArtifactCreator(root *os.Root, queue *os.File) (*storageCreatorLease, error) {
	if root == nil || queue == nil {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	return storageCreatorLockedLease(root, queue)
}
func checkInjectedArtifactEdges(ctx context.Context, edges []retainedArtifactInjectedEdge) error {
	for _, edge := range edges {
		folder, err := openDialectArtifactOwnerFolder(ctx, edge.root, false, "injected-edge-recheck", edge.parts...)
		if err != nil {
			return err
		}
		info, statErr := folder.Stat(".")
		closeErr := closeDialectArtifactOwner(ctx, "injected-edge-recheck", folder.Close)
		if statErr != nil || closeErr != nil || !os.SameFile(info, edge.info) {
			return errors.Join(errDialectArtifactOwner, statErr, closeErr)
		}
	}
	return nil
}

// Minted only while a real current native view is valid. The saved binding
// restricts a later fresh owning caller; it does not carry that caller's grant.
// Existing generic/factory guards are unavailable here until their owning
// implementation supplies the explicit physical-only D5 guard capability.
type boundInjectedArtifactRepository struct {
	factory      *assistantDialectDraftPutOwner
	completeRead *assistantDialectCompleteReadOwner
	registration *retainedArtifactProviderRegistration
	binding      retainedArtifactInjectedBinding
	acquire      func(context.Context) (retainedArtifactGuard, error)
}

func (v *retainedArtifactInjectedView) boundRepository(ctx context.Context, acquire func(context.Context) (retainedArtifactGuard, error)) (*boundInjectedArtifactRepository, error) {
	if acquire == nil {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	if err := v.check(ctx); err != nil {
		return nil, err
	}
	return &boundInjectedArtifactRepository{registration: v.registration, binding: v.binding, acquire: acquire}, nil
}

// Construction-only capture from the real lexical enrollment. Every later
// effect must bring its own original factory guard and current operation pin.
func (v *retainedArtifactInjectedView) boundDialectFactoryRepository(ctx context.Context, owner *assistantDialectDraftPutOwner) (*boundInjectedArtifactRepository, error) {
	if owner == nil || owner.self != owner || owner.factory != nil || owner.execution == nil || owner.writer != owner.execution.writer || owner.lease != owner.writer.lease || owner.binding.Project != v.binding.Namespace {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	if err := v.check(ctx); err != nil {
		return nil, err
	}
	return &boundInjectedArtifactRepository{factory: owner, registration: v.registration, binding: v.binding}, nil
}
func (b *boundInjectedArtifactRepository) Get(ctx context.Context, ref retainedArtifactRef) (raw []byte, err error) {
	defer func() {
		if err != nil {
			raw = nil
		}
	}()
	err = b.withExact(ctx, ref, false, b.acquire, func(v *retainedArtifactInjectedView) error {
		raw, err = v.GetExact(ctx, ref)
		return err
	})
	return raw, err
}
func (*boundInjectedArtifactRepository) Put(context.Context, []byte) (retainedArtifactRef, error) {
	return retainedArtifactRef{}, errRetainedArtifactAuthorityUnavailable
}
func (b *boundInjectedArtifactRepository) PutGuarded(ctx context.Context, raw []byte, acquire func(context.Context) (retainedArtifactGuard, error)) (ref retainedArtifactRef, err error) {
	if len(raw) > retainedArtifactByteLimit {
		return ref, errRetainedArtifactChanged
	}
	raw = bytes.Clone(raw)
	expected := artifactReference(raw)
	err = b.withExact(ctx, expected, true, acquire, func(v *retainedArtifactInjectedView) error { return v.PublishExact(ctx, expected, raw) })
	if err != nil {
		return ref, err
	}
	return expected, nil
}
func (b *boundInjectedArtifactRepository) withExact(ctx context.Context, ref retainedArtifactRef, publish bool, acquire func(context.Context) (retainedArtifactGuard, error), use func(*retainedArtifactInjectedView) error) (err error) {
	if b == nil || b.registration == nil || acquire == nil && b.factory == nil && b.completeRead == nil || use == nil || !ref.valid() {
		return errRetainedArtifactAuthorityUnavailable
	}
	if err = b.registration.current(); err != nil {
		return err
	}
	op, err := b.registration.provider.pin()
	if err != nil {
		return err
	}
	defer func() {
		if b.factory != nil {
			err = errors.Join(err, dialectArtifactOwnerStep(ctx, "factory-before-operation-finish"))
		}
		err = errors.Join(err, op.finish())
	}()
	if err = b.registration.drainPinned(ctx, op); err != nil {
		return err
	}
	if b.completeRead != nil {
		return b.withCompleteReadExact(ctx, ref, publish, acquire, use, op)
	}
	if b.factory != nil {
		return b.withFactoryExact(ctx, ref, publish, acquire, use, op)
	}
	guard, err := acquireRetainedArtifactGuard(ctx, acquire)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, guard.Close()) }()
	leaf, ok := guard.(*dialectArtifactInjectedAuthorityHold)
	if !ok || leaf.registration != b.registration || leaf.binding != b.binding {
		return errRetainedArtifactAuthorityUnavailable
	}
	selection, err := newRetainedArtifactInjectedSelection([]retainedArtifactRef{ref}, publish)
	if err != nil {
		return err
	}
	v, err := b.registration.beginD5ViewLocked(ctx, b.binding, selection, leaf)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, v.Close()) }()
	return use(v)
}

func (b *boundInjectedArtifactRepository) withFactoryExact(ctx context.Context, ref retainedArtifactRef, publish bool, acquire func(context.Context) (retainedArtifactGuard, error), use func(*retainedArtifactInjectedView) error, op *retainedArtifactInjectedOperation) (err error) {
	o := b.factory
	if !o.original() || o.injected != b || o.registration != b.registration || b.binding.Owner != o.enrollment.Owner || b.binding.Namespace != o.enrollment.Namespace || op == nil || op.provider != b.registration.provider || op.finished.Load() {
		return errRetainedArtifactAuthorityUnavailable
	}
	if !publish {
		acquire = func(ctx context.Context) (retainedArtifactGuard, error) { return o.acquireInspectionGuard(ctx, ref) }
	}
	if acquire == nil {
		return errRetainedArtifactAuthorityUnavailable
	}
	original, err := acquireRetainedArtifactGuard(ctx, acquire)
	if err != nil {
		return err
	}
	var authority *dialectArtifactInjectedAuthorityHold
	var view *retainedArtifactInjectedView
	// The view borrows authority, which borrows the original concrete host
	// hold. Release in this order on every exit, including partial admission.
	defer func() {
		if view != nil {
			err = errors.Join(err, view.Close())
		}
		if authority != nil {
			err = errors.Join(err, authority.Close())
		}
		err = errors.Join(err, original.Check(ctx), original.Close())
	}()
	if publish {
		g, ok := original.(*assistantDialectDraftGuard)
		if !ok || g.self != g || g.owner != o || g.publication.ref != ref || !g.reservation.consumed {
			return errRetainedArtifactAuthorityUnavailable
		}
		if err = dialectArtifactOwnerStep(ctx, "factory-before-authority"); err != nil {
			return err
		}
		authority, err = g.holdInjectedAuthority(ctx, b.registration, b.binding, op)
	} else {
		g, ok := original.(*assistantDialectDraftInspectionGuard)
		if !ok || g.self != g || g.owner != o || g.ref != ref {
			return errRetainedArtifactAuthorityUnavailable
		}
		if err = dialectArtifactOwnerStep(ctx, "factory-before-authority"); err != nil {
			return err
		}
		authority, err = g.holdInjectedAuthority(ctx, b.registration, b.binding, op)
	}
	if err != nil {
		return err
	}
	if authority.operation != op || authority.registration != b.registration || authority.binding != b.binding {
		return errRetainedArtifactAuthorityUnavailable
	}
	selection, err := newRetainedArtifactInjectedSelection([]retainedArtifactRef{ref}, publish)
	if err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(ctx, "factory-before-provider-queue"); err != nil {
		return err
	}
	view, err = b.registration.beginD5ViewLocked(ctx, b.binding, selection, authority)
	if err != nil {
		return err
	}
	if err = dialectArtifactOwnerStep(ctx, "factory-after-provider-queue"); err != nil {
		return err
	}
	return use(view)
}

// A read root has its own closed original owner, never a draft owner or a
// serialized preheld assertion. This capture occurs inside real enrollment.
func (v *retainedArtifactInjectedView) boundDialectCompleteReadRepository(ctx context.Context, o *assistantDialectCompleteReadOwner) (*boundInjectedArtifactRepository, error) {
	if o == nil || o.self != o || o.execution == nil || o.writer != o.execution.writer || o.lease != o.writer.lease || o.binding.Project != v.binding.Namespace {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	if err := v.check(ctx); err != nil {
		return nil, err
	}
	return &boundInjectedArtifactRepository{completeRead: o, registration: v.registration, binding: v.binding}, nil
}
func (b *boundInjectedArtifactRepository) withCompleteReadExact(ctx context.Context, ref retainedArtifactRef, publish bool, acquire func(context.Context) (retainedArtifactGuard, error), use func(*retainedArtifactInjectedView) error, op *retainedArtifactInjectedOperation) (err error) {
	o := b.completeRead
	if !o.original() || o.injected != b || o.registration != b.registration || b.binding.Owner != o.enrollment.Owner || b.binding.Namespace != o.enrollment.Namespace || ref != o.held.Intent.Reference || op == nil || op.provider != b.registration.provider || op.finished.Load() {
		return errRetainedArtifactAuthorityUnavailable
	}
	if !publish {
		if !o.published {
			return errRetainedArtifactAuthorityUnavailable
		}
		acquire = func(ctx context.Context) (retainedArtifactGuard, error) {
			return o.acquire(ctx, assistantDialectCompleteInspect)
		}
	}
	original, err := acquireRetainedArtifactGuard(ctx, acquire)
	if err != nil {
		return err
	}
	g, ok := original.(*assistantDialectCompleteReadGuard)
	var authority *dialectArtifactInjectedAuthorityHold
	var view *retainedArtifactInjectedView
	defer func() {
		if view != nil {
			err = errors.Join(err, view.Close())
		}
		if authority != nil {
			err = errors.Join(err, authority.Close())
		}
		err = errors.Join(err, original.Check(ctx), original.Close())
	}()
	expectedKind := assistantDialectCompleteInspect
	if publish {
		expectedKind = assistantDialectCompletePublish
	}
	if !ok || g.self != g || g.owner != o || g.kind != expectedKind {
		return errRetainedArtifactAuthorityUnavailable
	}
	physical := func() error {
		if op.finished.Load() {
			return errRetainedArtifactAuthorityUnavailable
		}
		if err := g.checkPhysical(ctx); err != nil {
			return err
		}
		if err := g.checkRun(); err != nil {
			return err
		}
		if err := g.source.Check(ctx); err != nil {
			return err
		}
		return g.project.Check(ctx)
	}
	authority, err = holdDialectArtifactInjectedAuthority(o.execution.service.server, o.execution.request.WithContext(ctx), o.binding, b.registration, op, physical)
	if err != nil {
		return err
	}
	authority.binding = b.binding
	selection, err := newRetainedArtifactInjectedSelection([]retainedArtifactRef{ref}, publish)
	if err != nil {
		return err
	}
	view, err = b.registration.beginD5ViewLocked(ctx, b.binding, selection, authority)
	if err != nil {
		return err
	}
	return use(view)
}

func mergeInjectedArtifactEdges(into *[]retainedArtifactInjectedEdge, incoming []retainedArtifactInjectedEdge) error {
	for _, edge := range incoming {
		found := false
		for _, prior := range *into {
			if prior.root != edge.root || len(prior.parts) != len(edge.parts) {
				continue
			}
			same := true
			for i := range prior.parts {
				if prior.parts[i] != edge.parts[i] {
					same = false
					break
				}
			}
			if !same {
				continue
			}
			if !os.SameFile(prior.info, edge.info) {
				return errDialectArtifactOwner
			}
			found = true
			break
		}
		if !found {
			*into = append(*into, edge)
		}
	}
	return nil
}
func readInjectedArtifactExactFile(ctx context.Context, root *os.Root, name string, limit int, label string) (raw []byte, err error) {
	raw, err = readDialectArtifactOwnerFile(root, name, limit)
	if err == nil {
		err = dialectArtifactOwnerStep(ctx, label+".file-close")
	}
	if err != nil {
		raw = nil
	}
	return raw, err
}
