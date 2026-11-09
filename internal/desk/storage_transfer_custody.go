package desk

import (
	"context"
	"errors"
	"net/http"
	"os"
)

// Not an interface, serialized claim or context flag. Only changeChatStorage's
// owning continuation constructs this original object while its work, writes,
// private, settings, source and empty target locals are actually alive. It owns
// none of those borrowed handles and offers no Close/relock operation.
type chatStorageTransferCustody struct {
	self     *chatStorageTransferCustody
	server   *Server
	request  *http.Request
	work     *os.File
	private  *privateDataShortLock
	settings *os.Root
	source   *chatDataStore
	target   *chatDataStore
	original chatStorageTransferOriginal
	active   bool
}

type chatStorageTransferOriginal struct {
	server                                                                    *Server
	request                                                                   *http.Request
	project                                                                   *ProjectRoot
	root                                                                      *os.Root
	assistant                                                                 *assistantStore
	assistantRoot                                                             *os.Root
	work                                                                      *os.File
	private                                                                   *privateDataShortLock
	privateFile                                                               *os.File
	settings                                                                  *os.Root
	source                                                                    *chatDataStore
	sourceRoot                                                                *os.Root
	target                                                                    *chatDataStore
	targetRoot                                                                *os.Root
	sourceLocation, targetLocation                                            dataLocation
	sourceRevision, targetRevision                                            string
	sourceLegacy, targetLegacy                                                bool
	workInfo, privateInfo, workRootInfo, settingsInfo, sourceInfo, targetInfo os.FileInfo
}

// capture is called only by the storage owner immediately before admission.
// A live caller cannot recapture a mutated graph as a fresh baseline.
func (c *chatStorageTransferCustody) capture() (err error) {
	if c == nil || c.self != c || c.active || c.original.server != nil || c.server == nil || c.request == nil || c.work == nil ||
		c.private == nil || c.private.File == nil || c.settings == nil || c.source == nil || c.target == nil || c.source.root == nil || c.target.root == nil ||
		c.server.assistant == nil || !c.server.assistant.usable() || c.server.assistant.root == nil || c.server.project == nil || c.server.root == nil {
		return errPrivateDataChanged
	}
	s := c.server
	o := chatStorageTransferOriginal{server: s, request: c.request, project: s.project, root: s.root, assistant: s.assistant, assistantRoot: s.assistant.root,
		work: c.work, private: c.private, privateFile: c.private.File, settings: c.settings, source: c.source, sourceRoot: c.source.root, target: c.target, targetRoot: c.target.root,
		sourceLocation: c.source.location, targetLocation: c.target.location, sourceRevision: c.source.revision, targetRevision: c.target.revision,
		sourceLegacy: c.source.legacy, targetLegacy: c.target.legacy}
	for _, pin := range []struct {
		stat func() (os.FileInfo, error)
		out  *os.FileInfo
	}{
		{c.work.Stat, &o.workInfo}, {c.private.File.Stat, &o.privateInfo}, {func() (os.FileInfo, error) { return s.assistant.root.Stat(".") }, &o.workRootInfo},
		{func() (os.FileInfo, error) { return c.settings.Stat(".") }, &o.settingsInfo}, {func() (os.FileInfo, error) { return c.source.root.Stat(".") }, &o.sourceInfo}, {func() (os.FileInfo, error) { return c.target.root.Stat(".") }, &o.targetInfo},
	} {
		*pin.out, err = pin.stat()
		if err != nil {
			return err
		}
	}
	c.original = o
	return nil
}

func (c *chatStorageTransferCustody) check(ctx context.Context) (err error) {
	if c == nil || c.self != c || !c.active || ctx == nil {
		return errPrivateDataChanged
	}
	o := c.original
	if o.server == nil || o.request == nil || o.private == nil || o.privateFile == nil || o.source == nil || o.target == nil || o.project == nil || o.assistant == nil {
		return errPrivateDataChanged
	}
	if c.server != o.server || c.request != o.request || c.work != o.work || c.private != o.private || c.private.File != o.privateFile ||
		c.settings != o.settings || c.source != o.source || c.target != o.target || c.source.root != o.sourceRoot || c.target.root != o.targetRoot ||
		c.server.project != o.project || c.server.root != o.root || c.server.assistant != o.assistant || c.server.assistant.root != o.assistantRoot ||
		c.source.location != o.sourceLocation || c.target.location != o.targetLocation || c.source.revision != o.sourceRevision || c.target.revision != o.targetRevision ||
		c.source.legacy != o.sourceLegacy || c.target.legacy != o.targetLegacy {
		return errPrivateDataChanged
	}
	if err = errors.Join(ctx.Err(), c.request.Context().Err()); err != nil {
		return err
	}
	for _, pin := range []struct {
		stat func() (os.FileInfo, error)
		info os.FileInfo
	}{
		{c.work.Stat, o.workInfo}, {c.private.File.Stat, o.privateInfo}, {func() (os.FileInfo, error) { return c.server.assistant.root.Stat(".") }, o.workRootInfo},
		{func() (os.FileInfo, error) { return c.settings.Stat(".") }, o.settingsInfo}, {func() (os.FileInfo, error) { return c.source.root.Stat(".") }, o.sourceInfo}, {func() (os.FileInfo, error) { return c.target.root.Stat(".") }, o.targetInfo},
	} {
		info, statErr := pin.stat()
		if statErr != nil || !os.SameFile(info, pin.info) {
			return errors.Join(errPrivateDataChanged, statErr)
		}
	}
	for _, pin := range []struct {
		name string
		info os.FileInfo
	}{{".data-work.lock", o.workInfo}, {".data.lock", o.privateInfo}} {
		entry, lookErr := c.server.assistant.root.Lstat(pin.name)
		if lookErr != nil || !os.SameFile(entry, pin.info) || ownerOnlyFile(pin.name, entry.Mode()) != nil || ownedByUs(pin.name, entry) != nil {
			return errors.Join(errPrivateDataChanged, lookErr)
		}
	}
	if !sameDirectory(c.server.configDir, o.workRootInfo) || !sameDirectory(c.server.projectDir, o.project.info) {
		return errPrivateDataChanged
	}
	if err = checkPrivateDataPath(c.source); err != nil {
		return err
	}
	if err = checkPrivateDataPath(c.target); err != nil {
		return err
	}
	fresh, err := c.server.storageSettingsRoot()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, fresh.Close()) }()
	info, err := fresh.Stat(".")
	if err != nil || !os.SameFile(info, o.settingsInfo) {
		return errors.Join(errPrivateDataChanged, err)
	}
	return nil
}
