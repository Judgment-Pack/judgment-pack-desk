package desk

import "encoding/json"

// A capability describes an adapter contract, never an execution permission.
func validResearchCapabilities(names []string) bool {
	if names == nil {
		return false
	}
	last := -1
	for _, name := range names {
		rank := -1
		switch name {
		case "answer":
			rank = 0
		case "search":
			rank = 1
		case "page":
			rank = 2
		}
		if rank <= last {
			return false
		}
		last = rank
	}
	return true
}
func validCatalogAdapter(shape, adapter string) bool {
	if shape == "command" {
		return adapter == ""
	}
	return catalogIdentifier.MatchString(adapter)
}
func catalogSourceMembers(raw json.RawMessage, version int, provider bool) bool {
	keys := []string{"id", "input", "mediaTypes", "maxBytes"}
	if provider {
		keys = []string{"id", "shape", "record"}
	}
	if version == 4 {
		keys = append(keys, "capabilities")
		var members map[string]json.RawMessage
		if json.Unmarshal(raw, &members) != nil {
			return false
		}
		if _, present := members["adapter"]; present {
			var adapter string
			if json.Unmarshal(members["adapter"], &adapter) != nil || !catalogIdentifier.MatchString(adapter) {
				return false
			}
			keys = append(keys, "adapter")
		}
	}
	_, ok := catalogMembers(raw, keys...)
	return ok
}
