package desk

import (
	"debug/buildinfo"
	"runtime/debug"
)

// BuildIdentity contains only public compiler metadata, never build flags or
// environment values. A missing identity stays unknown rather than borrowing
// the version of another component.
type BuildIdentity struct {
	ModuleVersion string `json:"moduleVersion,omitempty"`
	Revision      string `json:"revision,omitempty"`
	Modified      bool   `json:"modified,omitempty"`
}

type ComponentBuilds struct {
	Desk    BuildIdentity  `json:"desk"`
	Runtime BuildIdentity  `json:"runtime"`
	Runner  *BuildIdentity `json:"runner,omitempty"`
}

func buildIdentity(info *debug.BuildInfo) BuildIdentity {
	var identity BuildIdentity
	if info == nil {
		return identity
	}
	if info.Main.Version != "(devel)" {
		identity.ModuleVersion = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			identity.Revision = setting.Value
		case "vcs.modified":
			identity.Modified = setting.Value == "true"
		}
	}
	return identity
}

// Read installed companions once at startup, without executing them or reading
// source checkouts. These describe the binaries selected by this installation.
func componentBuilds(runtimeBin, runnerBin string) ComponentBuilds {
	info, _ := debug.ReadBuildInfo()
	result := ComponentBuilds{Desk: buildIdentity(info)}
	info, _ = buildinfo.ReadFile(runtimeBin)
	result.Runtime = buildIdentity(info)
	if runnerBin != "" {
		info, _ = buildinfo.ReadFile(runnerBin)
		runner := buildIdentity(info)
		result.Runner = &runner
	}
	return result
}
