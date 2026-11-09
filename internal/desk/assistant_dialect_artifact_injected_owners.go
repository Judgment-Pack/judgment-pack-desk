package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"
)

const dialectArtifactInjectedOwnerRecordBytes = 332

type dialectArtifactInjectedOwnerRecord struct {
	Version               int    `json:"version"`
	Owner                 string `json:"owner"`
	Kind                  string `json:"kind"`
	Provider              string `json:"provider"`
	ProjectFolderIdentity string `json:"projectFolderIdentity"`
	WorkDomain            string `json:"workDomain"`
}

func (v dialectArtifactInjectedOwnerRecord) valid() bool {
	return v.Version == 2 && v.Kind == "injected" && assistantRunDigest(v.Owner) && assistantRunDigest(v.Provider) &&
		dialectArtifactOwnerFileIdentity(v.ProjectFolderIdentity) && assistantRunDigest(v.WorkDomain)
}
func readDialectArtifactInjectedOwnerRecord(raw []byte) (v dialectArtifactInjectedOwnerRecord, err error) {
	err = readDialectArtifactOwnerJSON(raw, dialectArtifactInjectedOwnerRecordBytes, &v)
	if err == nil && !v.valid() {
		err = errDialectArtifactOwner
	}
	return v, err
}

// Only an unadorned absence can permit a new immutable claim. In particular a
// joined Close failure must not disappear behind errors.Is(err, ErrNotExist).
func injectedArtifactOnlyNotExist(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		all := joined.Unwrap()
		if len(all) == 0 {
			return false
		}
		for _, child := range all {
			if !injectedArtifactOnlyNotExist(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return injectedArtifactOnlyNotExist(wrapped.Unwrap())
	}
	return errors.Is(err, os.ErrNotExist)
}

type dialectArtifactInjectedUse struct {
	selection retainedArtifactInjectedSelection
	use       func(*retainedArtifactInjectedView) error
}

type dialectArtifactInjectedOwnerRows struct {
	projectPrivate, metadata, rows, locks                *os.Root
	markerLock, ownerLock                                *os.File
	marker                                               dialectArtifactOwnerMarkerRecord
	markerRaw, ownerRaw                                  []byte
	markerNew                                            bool
	projectIdentity, workIdentity, workDomain, namespace string
	workFileInfo, workInfo                               os.FileInfo
}

// The actual session map and authentication policy are pinned with try-only
// host locks before provider exclusion. This is distinct from checking e.ctx:
// session removal, expiry, actor/password mode and policy generation cannot
// change while the immutable provider publication is acknowledged. Contexts
// remain directly revocable. No authorization callback runs under the queue.
type dialectArtifactInjectedAuthorityHold struct {
	s             *Server
	request       *http.Request
	sessions      *sessionStore
	signIn        *signInState
	handle        string
	held          session
	issuer        string
	active        *signInRecord
	problem       bool
	generation    uint64
	epoch         context.Context
	expected      assistantRunBinding
	registration  *retainedArtifactProviderRegistration
	operation     *retainedArtifactInjectedOperation
	binding       retainedArtifactInjectedBinding
	physical      func() error
	selected      *dialectArtifactSelectedSources
	selectedScope dialectArtifactOwnerScope
	edges         []retainedArtifactInjectedEdge
	closed        bool
	closeErr      error
}

func holdDialectArtifactInjectedAuthority(s *Server, request *http.Request, expected assistantRunBinding, registration *retainedArtifactProviderRegistration, operation *retainedArtifactInjectedOperation, physical func() error) (_ *dialectArtifactInjectedAuthorityHold, err error) {
	if s == nil || request == nil || s.sessions == nil || registration == nil || operation == nil || physical == nil {
		return nil, errRetainedArtifactAuthorityUnavailable
	}
	g := &dialectArtifactInjectedAuthorityHold{s: s, request: request, sessions: s.sessions, signIn: s.signIn, expected: expected, registration: registration, operation: operation, physical: physical}
	if err = pinDialectArtifactInjectedAuthentication(g); err != nil {
		return nil, err
	}
	return g, nil
}

// Pin exactly the original session/sign-in maps. This helper does not select a
// provider or supply physical custody; its caller must first construct the real
// owning hold. Keeping this implementation shared prevents selected historical
// readers from inventing a weaker authentication token or copying auth state.
func pinDialectArtifactInjectedAuthentication(g *dialectArtifactInjectedAuthorityHold) (err error) {
	if g == nil || g.s == nil || g.request == nil || g.sessions == nil ||
		g.registration == nil || g.operation == nil || g.physical == nil || g.closed {
		return errRetainedArtifactAuthorityUnavailable
	}
	if !g.sessions.mu.TryLock() {
		return errRetainedArtifactBusy
	}
	defer func() {
		if err != nil {
			g.sessions.mu.Unlock()
		}
	}()
	if g.signIn != nil {
		if !g.signIn.mu.TryLock() {
			return errRetainedArtifactBusy
		}
		defer func() {
			if err != nil {
				g.signIn.mu.Unlock()
			}
		}()
		g.active, g.problem, g.generation, g.epoch = g.signIn.active, g.signIn.problem, g.signIn.generation, g.signIn.epoch
	}
	g.handle = g.sessions.handle(bearerOf(g.request))
	var ok bool
	g.held, ok = g.sessions.live[g.handle]
	if !ok || !validPolicyActor(policyActorOf(g.held)) {
		return errAssistantRunScope
	}
	if g.held.issuer != nil {
		g.issuer = *g.held.issuer
	}
	policyRaw, marshalErr := json.Marshal(struct {
		Generation uint64
		Actor      string
	}{g.generation, g.issuer + "\x00" + g.held.subject})
	current := assistantRunBinding{Principal: digestOf([]byte(g.issuer + "\x00" + g.held.subject)), Session: digestOf([]byte(g.handle)), Policy: digestOf(policyRaw)}
	if marshalErr != nil || !assistantRunIdentityMatches(current, g.expected) {
		return errors.Join(errAssistantRunScope, marshalErr)
	}
	// Same policy/session mode rules as signInSessionAllowed, read under the
	// already-held original mutex rather than recursively entering that method.
	if g.signIn != nil {
		if g.problem || g.epoch == nil || g.active == nil && g.held.issuer != nil ||
			g.active != nil && g.active.Password != nil && (!g.held.password || g.held.issuer != nil) ||
			g.active != nil && g.active.Password == nil && (g.held.issuer == nil || g.issuer != g.active.Owner.Issuer || g.held.subject != g.active.Owner.Subject) {
			return errAssistantRunScope
		}
	}
	if err = g.Check(g.request.Context()); err != nil {
		return err
	}
	return nil
}
func (*dialectArtifactInjectedAuthorityHold) injectedArtifactPhysicalGuard() {}
func (g *dialectArtifactInjectedAuthorityHold) Check(ctx context.Context) error {
	if g == nil || g.closed || ctx == nil || g.s.sessions != g.sessions || g.s.signIn != g.signIn || g.sessions.handle(bearerOf(g.request)) != g.handle {
		return errAssistantRunScope
	}
	if err := errors.Join(ctx.Err(), g.request.Context().Err(), g.held.ctx.Err()); err != nil {
		return err
	}
	if g.signIn != nil && (g.signIn.active != g.active || g.signIn.problem != g.problem || g.signIn.generation != g.generation || g.signIn.epoch != g.epoch || g.epoch.Err() != nil) {
		return errAssistantRunScope
	}
	held, ok := g.sessions.live[g.handle]
	if !ok || held.ctx != g.held.ctx || held.subject != g.held.subject || held.issuer != g.held.issuer || held.password != g.held.password || held.expires != g.held.expires || held.absolute != g.held.absolute ||
		!held.expires.IsZero() && !time.Now().Before(held.expires) || !held.absolute.IsZero() && !time.Now().Before(held.absolute) || held.issuer != nil && *held.issuer != g.issuer {
		return errAssistantRunScope
	}
	if g.selected != nil {
		if g.selected.self != g.selected || g.selected.authority != g || g.selected.s != g.s || g.selected.request != g.request ||
			!assistantRunIdentityMatches(g.selected.identity, g.expected) {
			return errRetainedArtifactAuthorityUnavailable
		}
		source := g.selected.scopes[g.selectedScope]
		if source == nil || source.injected == nil || source.injected.registration != g.registration ||
			source.injected.operation != g.operation || g.operation.finished.Load() || source.binding != g.binding {
			return errRetainedArtifactAuthorityUnavailable
		}
		if err := g.selected.checkSource(ctx, g.selectedScope); err != nil {
			return err
		}
	} else {
		current, err := g.s.injectedArtifactRegistration()
		if err != nil || current != g.registration || g.operation.provider != current.provider {
			return errors.Join(errRetainedArtifactAuthorityUnavailable, err)
		}
	}
	if err := checkInjectedArtifactEdges(ctx, g.edges); err != nil {
		return err
	}
	return g.physical()
}
func (g *dialectArtifactInjectedAuthorityHold) Close() error {
	if g == nil {
		return nil
	}
	if g.closed {
		return g.closeErr
	}
	if g.selected != nil && (g.selected.self != g.selected || g.selected.authority != g) {
		return errRetainedArtifactAuthorityUnavailable
	}
	g.closeErr = g.Check(g.request.Context())
	g.closed = true
	if g.signIn != nil {
		g.signIn.mu.Unlock()
	}
	g.sessions.mu.Unlock()
	if g.selected != nil {
		g.selected.authority = nil
	}
	g.closeErr = errors.Join(g.closeErr, dialectArtifactOwnerStep(g.request.Context(), "injected-authority.close"))
	return g.closeErr
}

// This continuation shares the original physical setup, marker/per-owner
// locks, opened data and original work FD with native enrollment. It cannot
// be called from a serialized owner record or from the factory effect path.
func (s *Server) enrollDialectArtifactInjectedOwnerRowsLocked(ctx context.Context, owner *Server, home *dialectArtifactHomeHold, work *os.File, data *chatDataStore,
	physical, full func() error, request *http.Request, registration *retainedArtifactProviderRegistration, operation *retainedArtifactInjectedOperation, use *dialectArtifactInjectedUse,
	rows dialectArtifactInjectedOwnerRows) (out dialectArtifactOwnerEnrollment, err error) {
	defer func() {
		if err != nil {
			out = dialectArtifactOwnerEnrollment{}
		}
	}()
	if registration == nil || operation == nil || registration.provider != operation.provider || request == nil {
		return out, errRetainedArtifactAuthorityUnavailable
	}
	if err = full(); err != nil {
		return out, err
	}
	if home != nil {
		if err = home.namespace(data.root, rows.namespace); err != nil {
			return out, err
		}
	}
	binding := retainedArtifactInjectedBinding{Version: 1, Provider: registration.provider.id, Owner: rows.marker.Owner, ProjectFolderIdentity: rows.projectIdentity, WorkDomain: rows.workDomain, Namespace: rows.namespace}
	if !binding.valid() {
		return out, errDialectArtifactOwner
	}
	bindingRaw, err := json.Marshal(binding)
	if err != nil {
		return out, err
	}
	scope := dialectArtifactOwnerScope{Version: 1, Kind: "native", Owner: binding.Owner, Namespace: binding.Namespace}
	scopeRaw, err := json.Marshal(scope)
	if err != nil {
		return out, err
	}
	workRaw, err := json.Marshal(dialectArtifactWorkDomainRecord{Version: 1, ID: rows.workDomain, Kind: "settings", PathBytes: []byte(s.configDir), FolderIdentity: rows.workIdentity, LockIdentity: identityKey(rows.workFileInfo)})
	if err != nil {
		return out, err
	}
	// Preflight directly selected existing host metadata without creating a
	// missing branch, before either new provider or host enrollment rows.
	scopeErr := readInjectedArtifactRow(ctx, rows.metadata, scopeRaw, binding.Owner+".json", "native", binding.Namespace)
	scopeNew := injectedArtifactOnlyNotExist(scopeErr)
	if scopeErr != nil && !scopeNew {
		return out, scopeErr
	}
	if err = preflightInjectedArtifactRow(ctx, rows.metadata, workRaw, rows.workDomain[2:]+".json", "work-domains", rows.workDomain[:2]); err != nil {
		return out, err
	}
	var homeRaw []byte
	if home != nil {
		homeRaw, err = json.Marshal(home.record)
		if err != nil {
			return out, err
		}
		if err = preflightInjectedArtifactRow(ctx, rows.metadata, homeRaw, home.record.ID[2:]+".json", "home-anchors", home.record.ID[:2]); err != nil {
			return out, err
		}
	}
	if err = full(); err != nil {
		return out, err
	}
	identity, _, err := s.assistantRunIdentity(request)
	if err != nil {
		return out, err
	}
	if err = full(); err != nil {
		return out, err
	}
	g, err := holdDialectArtifactInjectedAuthority(s, request, identity, registration, operation, physical)
	if err != nil {
		return out, err
	}
	g.binding = binding
	guardClosed := false
	defer func() {
		if !guardClosed {
			err = errors.Join(err, g.Close())
		}
	}()
	queue, err := registration.root.Open(".")
	if err != nil {
		return out, err
	}
	queueClosed := false
	defer func() {
		if !queueClosed {
			err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-enrollment-queue", queue.Close))
		}
	}()
	if err = dialectArtifactOwnerStep(ctx, "injected-before-enrollment-queue"); err != nil {
		return out, err
	}
	if err = lockPrivateData(queue, true); err != nil {
		if lockHeld(err) {
			err = errors.Join(errRetainedArtifactBusy, err)
		}
		return out, err
	}
	// The queue is the exclusive lock on the registration folder: its own
	// folders are made under it, never under a second lock on the same folder.
	queueCreator, err := heldInjectedArtifactCreator(registration.root, queue)
	if err != nil {
		return out, err
	}
	leaf := func() error {
		if err := g.Check(ctx); err != nil {
			return err
		}
		if err := registration.current(); err != nil {
			return err
		}
		if err := checkInjectedArtifactPrivateDataPath(ctx, data); err != nil {
			return err
		}
		projectInfo, projectErr := s.root.Stat(".")
		workInfo, workErr := s.assistant.root.Stat(".")
		fileInfo, fileErr := work.Stat()
		entry, entryErr := s.assistant.root.Lstat(".data-work.lock")
		if projectErr != nil || workErr != nil || fileErr != nil || entryErr != nil || identityKey(projectInfo) != rows.projectIdentity ||
			!os.SameFile(workInfo, rows.workInfo) || !os.SameFile(fileInfo, rows.workFileInfo) || !os.SameFile(entry, rows.workFileInfo) || !sameDirectory(s.configDir, workInfo) {
			return errors.Join(errDialectArtifactOwner, projectErr, workErr, fileErr, entryErr)
		}
		for _, lock := range []struct {
			file *os.File
			root *os.Root
			name string
		}{{rows.markerLock, rows.projectPrivate, ".dialect-artifact-owner.lock"}, {rows.ownerLock, rows.locks, binding.Owner[2:] + ".lock"}} {
			held, statErr := lock.file.Stat()
			entry, entryErr := lock.root.Lstat(lock.name)
			if statErr != nil || entryErr != nil || !os.SameFile(held, entry) || ownerOnlyFile(lock.name, held.Mode()) != nil || ownedByUs(lock.name, held) != nil {
				return errors.Join(errDialectArtifactOwner, statErr, entryErr)
			}
		}
		return registration.provider.check(ctx)
	}
	if err = leaf(); err != nil {
		return out, err
	}
	if rows.markerNew {
		err = preflightInjectedArtifactRow(ctx, registration.root, rows.ownerRaw, binding.Owner[2:]+".json", "d5-owners", binding.Owner[:2])
		if err == nil {
			err = preflightInjectedArtifactRow(ctx, registration.root, bindingRaw, binding.Owner+".json", "d5-native", binding.Namespace)
		}
	} else {
		// A locally selected existing scope is resolved, never reconstructed
		// from a surviving host row if its provider binding is absent.
		err = readInjectedArtifactRow(ctx, registration.root, rows.ownerRaw, binding.Owner[2:]+".json", "d5-owners", binding.Owner[:2])
		if err == nil {
			if scopeNew {
				err = preflightInjectedArtifactRow(ctx, registration.root, bindingRaw, binding.Owner+".json", "d5-native", binding.Namespace)
			} else {
				err = readInjectedArtifactRow(ctx, registration.root, bindingRaw, binding.Owner+".json", "d5-native", binding.Namespace)
			}
		}
	}
	if err != nil {
		return out, err
	}
	var edges []retainedArtifactInjectedEdge
	for _, pin := range []struct {
		root, held *os.Root
		parts      []string
	}{
		{s.root, rows.projectPrivate, []string{".desk-private"}},
		{owner.assistant.secrets, rows.metadata, []string{dialectArtifactOwnersFolder}},
		{rows.metadata, rows.rows, []string{"owners", binding.Owner[:2]}},
		{rows.metadata, rows.locks, []string{"locks", binding.Owner[:2]}},
	} {
		pinned, pinErr := pinInjectedArtifactHeldEdges(ctx, pin.root, pin.held, pin.parts...)
		if pinErr != nil {
			return out, pinErr
		}
		edges = append(edges, pinned...)
	}
	for _, branch := range []struct {
		root  *os.Root
		parts []string
	}{
		{registration.root, []string{"d5-owners", binding.Owner[:2]}},
		{registration.root, []string{"d5-native", binding.Namespace}},
		{rows.metadata, []string{"work-domains", rows.workDomain[:2]}},
		{rows.metadata, []string{"native", binding.Namespace}},
	} {
		var held *storageCreatorLease
		if branch.root == registration.root {
			held = queueCreator
		}
		pinned, pinErr := prepareInjectedArtifactEdges(ctx, branch.root, held, branch.parts...)
		if pinErr != nil {
			return out, pinErr
		}
		edges = append(edges, pinned...)
	}
	if home != nil {
		pinned, pinErr := prepareInjectedArtifactEdges(ctx, rows.metadata, nil, "home-anchors", home.record.ID[:2])
		if pinErr != nil {
			return out, pinErr
		}
		edges = append(edges, pinned...)
	}
	g.edges = append([]retainedArtifactInjectedEdge(nil), edges...)
	// All operations from here through queue Close receive leaf only. The
	// full ledger/source validator is deliberately outside this lexical span.
	publishedWork, err := s.publishDialectArtifactWorkDomain(ctx, rows.metadata, work, leaf)
	if err != nil || !bytes.Equal(publishedWork, workRaw) {
		return out, errors.Join(errDialectArtifactOwner, err)
	}
	if home != nil {
		publishedHome, publishErr := home.publish(rows.metadata, leaf)
		if publishErr != nil || !bytes.Equal(publishedHome, homeRaw) {
			return out, errors.Join(errDialectArtifactOwner, publishErr)
		}
	}
	if err = claimDialectArtifactOwner(ctx, registration.root, "d5-provider.json", registration.identity, "injected-identity", leaf); err != nil {
		return out, err
	}
	if err = claimInjectedArtifactRow(ctx, registration.root, rows.ownerRaw, binding.Owner[2:]+".json", "injected-provider-owner", leaf, "d5-owners", binding.Owner[:2]); err != nil {
		return out, err
	}
	if err = claimInjectedArtifactRow(ctx, registration.root, bindingRaw, binding.Owner+".json", "injected-provider-binding", leaf, "d5-native", binding.Namespace); err != nil {
		return out, err
	}
	if err = claimDialectArtifactOwner(ctx, rows.rows, binding.Owner[2:]+".json", rows.ownerRaw, "injected-owner", leaf); err != nil {
		return out, err
	}
	if rows.markerNew {
		if err = dialectArtifactOwnerStep(ctx, "before-marker"); err != nil {
			return out, err
		}
	}
	if err = claimDialectArtifactOwner(ctx, rows.projectPrivate, dialectArtifactOwnerMarker, rows.markerRaw, "marker", leaf); err != nil {
		return out, err
	}
	if err = claimInjectedArtifactRow(ctx, rows.metadata, scopeRaw, binding.Owner+".json", "scope", leaf, "native", binding.Namespace); err != nil {
		return out, err
	}
	if err = s.readbackDialectArtifactInjectedOwner(ctx, rows, scopeRaw, workRaw, homeRaw); err != nil {
		return out, err
	}
	if err = readInjectedArtifactRow(ctx, registration.root, rows.ownerRaw, binding.Owner[2:]+".json", "d5-owners", binding.Owner[:2]); err != nil {
		return out, err
	}
	if err = readInjectedArtifactRow(ctx, registration.root, bindingRaw, binding.Owner+".json", "d5-native", binding.Namespace); err != nil {
		return out, err
	}
	if err = checkInjectedArtifactEdges(ctx, edges); err != nil {
		return out, err
	}
	if err = leaf(); err != nil {
		return out, err
	}
	if use != nil {
		if use.use == nil {
			return out, errRetainedArtifactAuthorityUnavailable
		}
		selection, selectErr := newRetainedArtifactInjectedSelection(use.selection.refs, use.selection.restore)
		if selectErr != nil {
			return out, selectErr
		}
		v := &retainedArtifactInjectedView{registration: registration, binding: binding, ownerRaw: bytes.Clone(rows.ownerRaw), bindingRaw: bytes.Clone(bindingRaw), scope: scope, scopeRaw: bytes.Clone(scopeRaw), selection: selection, guard: g, queue: queue, ctx: ctx, held: true}
		// The callback uses the already-held original queue, and cannot
		// turn provenance strings into another view or acquire host custody.
		useErr := use.use(v)
		edgeErr := checkInjectedArtifactEdges(ctx, edges)
		queueClosed = true
		err = errors.Join(useErr, edgeErr, v.Close())
	} else {
		queueClosed = true
		err = closeDialectArtifactOwner(ctx, "injected-enrollment-queue", queue.Close)
	}
	guardClosed = true
	err = errors.Join(err, g.Close())
	if err != nil {
		return out, err
	}
	if err = full(); err != nil {
		return out, err
	}
	current := s.assistantRunsLocked(data.root)
	if current.problem != nil || current.project != binding.Namespace {
		return out, errors.Join(errDialectArtifactOwner, current.problem)
	}
	if home != nil {
		if err = home.namespace(data.root, binding.Namespace); err != nil {
			return out, err
		}
	}
	if err = dialectArtifactOwnerStep(ctx, "acknowledge"); err != nil {
		return out, err
	}
	out = dialectArtifactOwnerEnrollment{Owner: binding.Owner, Namespace: binding.Namespace, OwnerSHA256: digestOf(rows.ownerRaw), ScopeSHA256: digestOf(scopeRaw), ProjectIdentity: rows.projectIdentity, WorkIdentity: rows.workIdentity, WorkDomain: rows.workDomain, WorkSHA256: digestOf(workRaw)}
	if home != nil {
		out.HomeAnchor, out.HomeSHA256 = home.record.ID, digestOf(homeRaw)
	}
	return out, nil
}

func (s *Server) readbackDialectArtifactInjectedOwner(ctx context.Context, rows dialectArtifactInjectedOwnerRows, scopeRaw, workRaw, homeRaw []byte) (err error) {
	if err = dialectArtifactOwnerStep(ctx, "selected-readback"); err != nil {
		return err
	}
	if err = readInjectedArtifactRow(ctx, s.root, rows.markerRaw, dialectArtifactOwnerMarker, ".desk-private"); err != nil {
		return err
	}
	metadata, err := openDialectArtifactOwnerFolder(ctx, s.deskOwner().assistant.secrets, false, "metadata-folder", dialectArtifactOwnersFolder)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-metadata-readback", metadata.Close))
	}()
	id := rows.marker.Owner
	if err = readInjectedArtifactRow(ctx, metadata, rows.ownerRaw, id[2:]+".json", "owners", id[:2]); err != nil {
		return err
	}
	if err = readInjectedArtifactRow(ctx, metadata, scopeRaw, id+".json", "native", rows.namespace); err != nil {
		return err
	}
	if err = readbackDialectArtifactWorkDomain(ctx, metadata, workRaw); err != nil {
		return err
	}
	if homeRaw != nil {
		home, decodeErr := readDialectArtifactHomeAnchorRecord(homeRaw)
		if decodeErr != nil {
			return decodeErr
		}
		if err = readInjectedArtifactRow(ctx, metadata, homeRaw, home.ID[2:]+".json", "home-anchors", home.ID[:2]); err != nil {
			return err
		}
	}
	return nil
}

func checkInjectedArtifactPrivateDataPath(ctx context.Context, data *chatDataStore) (err error) {
	if data.legacy {
		return nil
	}
	current, err := openPrivateDataRoot(data.location.Path, false)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, closeDialectArtifactOwner(ctx, "injected-private-data-path", current.Close))
	}()
	before, err := data.root.Stat(".")
	if err != nil {
		return err
	}
	after, err := current.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	return nil
}
