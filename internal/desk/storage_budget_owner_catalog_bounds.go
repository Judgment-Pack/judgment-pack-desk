package desk

// Every serialized occurrence counts; neither equal byte strings nor related
// root/control projections are deduplicated. Derived digest DTOs are not wire
// members and are never counted a second time.
func storageOwnerAggregateByteField(field string) bool {
	switch field {
	case "ConfigPath", "HomePath", "Folder", "HistoryIdentity", "Name", "Path", "Previous", "Relative":
		return true
	}
	return false
}

// Author-side bounds precede maps, material expansion and JSON allocation.
// Decoder-side equivalents run against raw bytes before Unmarshal.
func (c storageOwnerCatalogV2) bounded() bool {
	if len(c.Objects)+len(c.OwnerControls) > 8192 || len(c.Roots) > 4096 || len(c.Workspaces) > 1024 || len(c.Filesystems) > 128 || len(c.Constraints) > 512 || len(c.Profiles) > 64 || len(c.OriginalOwner.Entries) > 1024 || len(c.BeforeDefinitionIDs.Roots) > 4096 || len(c.BeforeDefinitionIDs.Workspaces) > 1024 || len(c.BeforeDefinitionIDs.Filesystems) > 128 || len(c.BeforeDefinitionIDs.Constraints) > 512 {
		return false
	}
	total, refs := 0, 0
	add := func(b []byte, limit int, count bool) bool {
		if len(b) > limit {
			return false
		}
		if count {
			total += len(b)
		}
		return total <= 4<<20
	}
	s := c.OriginalOwner
	if len(s.HomeOwner) > 64 || !add(s.ConfigPath, 4096, true) || !add(s.HomePath, 4096, true) {
		return false
	}
	for _, h := range s.Entries {
		if len(h.DeskID) > 32 || len(h.StorageOwner) > 64 || len(h.RunnerWorkspace) > 64 || !add(h.Folder, 120, true) || !add(h.HistoryIdentity, 4096, true) {
			return false
		}
	}
	for _, o := range c.Objects {
		if !add(o.Name, 1024, true) {
			return false
		}
	}
	for _, v := range c.OwnerControls {
		if !add(v.Name, 1024, true) || !add(v.OwnerKey, 4096, false) || !add(v.Location.Path, 4096, true) || !add(v.Location.Previous, 4096, true) {
			return false
		}
	}
	for _, r := range c.Roots {
		if len(r.Definition.Locator) > 256 || len(r.References) > 1024 || len(r.Membership.ExistingObjects) > 8192 || !add(r.Source.OwnerKey, 4096, false) || !add(r.Source.Path, 4096, true) || !add(r.Source.Relative, 65599, true) {
			return false
		}
		refs += len(r.References)
		if refs > 65536 {
			return false
		}
	}
	for _, w := range c.Workspaces {
		if !add(w.OwnerKey, 256, false) || !add(w.HistoryIdentity, 4096, true) {
			return false
		}
	}
	return true
}
