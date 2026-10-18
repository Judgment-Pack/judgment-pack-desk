package desk

import (
	"errors"
	"os"
	"path/filepath"
)

// Configured startup never runs portable home/project recovery automatically.
func CheckPolicyAuthorityStartup(options *PolicyAuthorityOptions) error {
	if options == nil {
		return nil
	}
	if !policyMachineOptionsValid(*options) {
		return policyMachineRefusal()
	}
	return policyMachineStartupJournals()
}
func policyMachineStartupJournals() error {
	config, err := ResolveDeskConfigDir()
	if err != nil {
		return policyMachineRefusal()
	}
	for _, path := range []string{homeJournalPath(legacyConfigDir()), homeJournalPath(config)} {
		tx, e := readHomeTransaction(path)
		if e != nil || tx != nil && !tx.Complete {
			return policyMachineRefusal()
		}
	}
	return nil
}
func policyMachineProjectIdle(config string, p *ProjectRoot) error {
	for _, path := range []string{homeJournalPath(legacyConfigDir()), homeJournalPath(config)} {
		tx, e := readHomeTransaction(path)
		if e != nil || tx != nil && !tx.Complete {
			return policyMachineRefusal()
		}
	}
	settings, err := openPrivateDataRoot(config, false)
	if err == nil {
		defer settings.Close()
		_, found, e := readProjectMove(settings)
		if e != nil || found {
			return policyMachineRefusal()
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return policyMachineRefusal()
	}
	if p != nil {
		private, _, e := openOwnFolder(p.own.root, []string{startupIdentityDir}, false, "project state")
		if e == nil {
			defer private.Close()
			_, found, e := readProjectMove(private)
			if e != nil || found {
				return policyMachineRefusal()
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return policyMachineRefusal()
		}
	}
	return nil
}
func policyMachineProtectedPaths(machine, config string, p *ProjectRoot) error {
	paths := []string{config, legacyConfigDir(), deskApplicationDataPath()}
	home, err := readDeskHome(config)
	if err != nil {
		return policyMachineRefusal()
	}
	if home != nil {
		paths = append(paths, home.Path)
	}
	if p != nil {
		paths = append(paths, p.dir)
	}
	if policyMachineSeparate(machine, paths...) != nil {
		return policyMachineRefusal()
	}
	if err = policyMachineStoragePaths(machine, config, false); err != nil {
		return err
	}
	if p != nil {
		return policyMachineStoragePaths(machine, filepath.Join(p.dir, startupIdentityDir), true)
	}
	return nil
}
func policyMachineStoragePaths(machine, settings string, relative bool) error {
	root, err := openPrivateDataRoot(settings, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return policyMachineRefusal()
	}
	defer root.Close()
	raw, err := readPrivateData(root, dataLocationName, 16<<10)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var location dataLocation
	if err != nil || rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &location) != nil || location.Version != 1 {
		return policyMachineRefusal()
	}
	for _, path := range []string{location.Path, location.Previous} {
		if path == "" {
			continue
		}
		if relative {
			if !filepath.IsLocal(path) {
				return policyMachineRefusal()
			}
			path = filepath.Join(settings, path)
		} else if !filepath.IsAbs(path) {
			return policyMachineRefusal()
		}
		if policyMachineSeparate(machine, path) != nil {
			return policyMachineRefusal()
		}
	}
	return nil
}
func (s *Server) policyMachineTarget(path string) error {
	if s.cfg.PolicyAuthority == nil {
		return nil
	}
	return policyMachineSeparate(s.cfg.PolicyAuthority.Root, path)
}
