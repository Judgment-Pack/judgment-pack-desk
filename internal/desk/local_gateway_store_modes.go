package desk

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Bounds on the start-time repair below. A store larger or deeper than this is
// left as it is past the bound, for the census to measure or refuse.
const (
	localGatewayStoreRepairEntries = 65536
	localGatewayStoreRepairDepth   = 32
)

var errLocalGatewayStoreRepairBounds = errors.New("the local gateway's store is larger than the start-time permission repair covers")

// narrowLocalGatewayStore repairs the local gateway's store that an earlier
// Desk let the gateway create at the inherited mask, 0755 folders and 0644
// files at the usual 022, inside Desk's config folder. The storage census
// refuses any entry there that is not private, so that store made every start
// read-only. The worker now gives the gateway an owner-only mask, which covers
// what it creates from now on; this narrows what it created before.
//
// Only the store Desk makes for the local gateway is touched ("local-gateway",
// or "services/gateway" in a Desk home), and only before the census, while
// this start holds the installation exclusively. Custody's repair rule
// applies: a folder or a single-named regular file owned by this user that
// neither group nor others may write loses its group and other bits (and any
// special bits), through a descriptor checked to be the entry inspected.
// Everything else is left exactly as it is: a link is not followed, another
// user's entry, a file with another name, and an entry group or others may
// write are not repaired, and a folder that is not repaired is not entered.
// The census still decides, and refuses what is left as before.
func (s *Server) narrowLocalGatewayStore() error {
	if !custodyChecked || s.assistant == nil || !s.assistant.usable() || s.assistant.root == nil {
		return nil
	}
	if p := s.storageInfrastructure; p == nil || p.home == nil || !p.home.exclusive {
		return nil
	}
	parts := strings.Split(filepath.ToSlash(platformRelative(s.configDir, "local-gateway")), "/")
	current := s.assistant.root
	var opened []*os.Root
	defer func() {
		for _, r := range opened {
			r.Close()
		}
	}()
	// A Desk home's "services" folder is the infrastructure's, checked there;
	// it is passed through, never narrowed here.
	for _, part := range parts[:len(parts)-1] {
		info, err := current.Lstat(part)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || !localGatewayStoreOurs(info) {
			return nil
		}
		next, err := current.OpenRoot(part)
		if err != nil {
			return err
		}
		opened = append(opened, next)
		held, err := next.Stat(".")
		if err != nil {
			return err
		}
		if !os.SameFile(info, held) {
			return nil
		}
		current = next
	}
	name := parts[len(parts)-1]
	info, err := current.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	budget := localGatewayStoreRepairEntries
	return narrowLocalGatewayStoreEntry(current, name, info, 0, &budget)
}

// localGatewayStoreOurs is custody's precondition for a repair: owned by the
// user running Desk and writable by nobody else.
func localGatewayStoreOurs(info fs.FileInfo) bool {
	owner, known := ownerOf(info)
	return known && owner == effectiveUser() && info.Mode()&fs.ModeSymlink == 0 && info.Mode().Perm()&worldMode == 0
}

// localGatewayStoreNarrowed is the owner-only mode for an entry, and whether
// it differs from the entry's mode. Only group, other and special bits go.
func localGatewayStoreNarrowed(mode fs.FileMode) (fs.FileMode, bool) {
	narrowed := mode.Perm() & 0o700
	return narrowed, mode.Perm()&0o077 != 0 || mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0
}

func narrowLocalGatewayStoreEntry(parent *os.Root, name string, info fs.FileInfo, depth int, budget *int) error {
	if *budget <= 0 || depth > localGatewayStoreRepairDepth {
		return errLocalGatewayStoreRepairBounds
	}
	*budget--
	if !localGatewayStoreOurs(info) {
		return nil
	}
	switch {
	case info.Mode().IsRegular():
		if links, known := linkCount(info); !known || links != 1 {
			return nil
		}
		mode, needed := localGatewayStoreNarrowed(info.Mode())
		if !needed {
			return nil
		}
		f, err := parent.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		held, err := f.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(info, held) || !held.Mode().IsRegular() || !localGatewayStoreOurs(held) {
			return nil
		}
		if links, known := linkCount(held); !known || links != 1 {
			return nil
		}
		return f.Chmod(mode)
	case info.IsDir():
		child, err := parent.OpenRoot(name)
		if err != nil {
			return err
		}
		defer child.Close()
		dir, err := child.Open(".")
		if err != nil {
			return err
		}
		defer dir.Close()
		held, err := dir.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(info, held) || !held.IsDir() || !localGatewayStoreOurs(held) {
			return nil
		}
		if mode, needed := localGatewayStoreNarrowed(held.Mode()); needed {
			if err = dir.Chmod(mode); err != nil {
				return err
			}
		}
		for {
			entries, readErr := dir.ReadDir(256)
			for _, entry := range entries {
				entryInfo, err := child.Lstat(entry.Name())
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				if err != nil {
					return err
				}
				if err = narrowLocalGatewayStoreEntry(child, entry.Name(), entryInfo, depth+1, budget); err != nil {
					return err
				}
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	return nil
}
