package desk

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type jobsLocation struct{ Dir, Workspace, Owner string }

// Server and offline enablement share the same retained Runner namespace.
// Machine workspace identity is deliberately not a Runner namespace.
func resolveJobsLocation(config, project, deskID string, home *deskHome) (jobsLocation, error) {
	location := jobsLocation{filepath.Join(config, "jobs", digestOf([]byte(project))), digestOf([]byte(project)), "local-owner:" + digestOf([]byte(config))}
	if home != nil {
		location.Owner = "local-owner:" + home.Registry.Owner
		entry := home.entry(project)
		if entry != nil {
			return jobsLocation{filepath.Join(project, ".desk-private", "jobs"), entry.RunnerWorkspace, location.Owner}, nil
		}
	}
	if deskID != "" {
		if !deskIDPattern.MatchString(deskID) {
			return jobsLocation{}, policyMachineRefusal()
		}
		location.Dir = filepath.Join(project, ".desk-private", "jobs")
		location.Workspace = deskID
	}
	return location, nil
}
func offlineJobsLocation(config string, project *ProjectRoot) (jobsLocation, error) {
	home, err := readDeskHome(config)
	if err != nil {
		return jobsLocation{}, policyMachineRefusal()
	}
	if home != nil {
		if home.entry(project.dir) == nil {
			return jobsLocation{}, policyMachineRefusal()
		}
		return resolveJobsLocation(config, project.dir, "", home)
	}
	id := ""
	if filepath.Dir(project.dir) == filepath.Join(config, "desks") {
		id = filepath.Base(project.dir)
		if _, err = readDeskManifest(project.own.root, id); err != nil {
			return jobsLocation{}, policyMachineRefusal()
		}
	} else {
		// A managed manifest elsewhere cannot silently become a legacy startup.
		private, _, e := openOwnFolder(project.own.root, []string{".desk-private"}, false, "project state")
		if e == nil {
			_, e = private.Lstat("desk.json")
			private.Close()
			if !errors.Is(e, os.ErrNotExist) {
				return jobsLocation{}, policyMachineRefusal()
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return jobsLocation{}, policyMachineRefusal()
		}
		if strings.HasPrefix(project.dir, filepath.Join(config, "desks")+string(filepath.Separator)) {
			return jobsLocation{}, policyMachineRefusal()
		}
	}
	return resolveJobsLocation(config, project.dir, id, nil)
}
