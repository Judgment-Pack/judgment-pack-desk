// Package releaseplan carries the same component lock used by CI and packaging.
package releaseplan

import (
	"embed"
	"encoding/json"
)

//go:embed components.json
var files embed.FS

// Version is supplied by the release builder. Source builds remain development builds.
var Version = "development"

type Component struct {
	Repository string `json:"repository"`
	Version    string `json:"version"`
	Revision   string `json:"revision"`
	Channel    string `json:"channel"`
}
type Plan struct {
	SchemaVersion int                  `json:"schemaVersion"`
	StateEpoch    int                  `json:"stateEpoch"`
	Components    map[string]Component `json:"components"`
}

var Locked = func() Plan {
	raw, err := files.ReadFile("components.json")
	if err != nil {
		panic(err)
	}
	var plan Plan
	if err = json.Unmarshal(raw, &plan); err != nil {
		panic(err)
	}
	return plan
}()
