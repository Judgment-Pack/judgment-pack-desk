package desk

import (
	"bytes"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageOwnerPOCompare(a, b storageOwnerPersistentObject) int {
	if a.Backend < b.Backend {
		return -1
	}
	if a.Backend > b.Backend {
		return 1
	}
	if n := bytes.Compare(a.VolumeUUID[:], b.VolumeUUID[:]); n != 0 {
		return n
	}
	if a.Object < b.Object {
		return -1
	}
	if a.Object > b.Object {
		return 1
	}
	if a.Incarnation < b.Incarnation {
		return -1
	}
	if a.Incarnation > b.Incarnation {
		return 1
	}
	if a.IncarnationKind < b.IncarnationKind {
		return -1
	}
	if a.IncarnationKind > b.IncarnationKind {
		return 1
	}
	if a.ObjectKind < b.ObjectKind {
		return -1
	}
	if a.ObjectKind > b.ObjectKind {
		return 1
	}
	return 0
}
func storageOwnerCatalogNameComponent(b []byte) bool {
	return len(b) > 0 && len(b) <= 1024 && !bytes.Equal(b, []byte(".")) && !bytes.Equal(b, []byte("..")) && !bytes.ContainsAny(b, "/\\\x00\r\n")
}
func storageOwnerAbsolute(b []byte) bool {
	if len(b) == 0 || len(b) > 4096 || b[0] != '/' || bytes.ContainsAny(b, "\x00\r\n") {
		return false
	}
	if len(b) == 1 {
		return true
	}
	start := 1
	for i := 1; i <= len(b); i++ {
		if i == len(b) || b[i] == '/' {
			p := b[start:i]
			if len(p) == 0 || bytes.Equal(p, []byte(".")) || bytes.Equal(p, []byte("..")) {
				return false
			}
			start = i + 1
		}
	}
	return true
}
func storageOwnerRelative(b []byte) bool {
	if len(b) > 65599 {
		return false
	}
	if len(b) == 0 {
		return true
	}
	start, count := 0, 0
	for i := 0; i <= len(b); i++ {
		if i == len(b) || b[i] == '/' {
			count++
			if count > 64 || !storageOwnerCatalogNameComponent(b[start:i]) {
				return false
			}
			start = i + 1
		}
	}
	return true
}
func storageOwnerText(s string, n int, empty bool) bool {
	return len(s) <= n && (empty || len(s) > 0) && !strings.ContainsAny(s, "\x00\r\n") && !strings.ContainsRune(s, '\ufffd')
}
func storageOwnerIdentityString(s string) bool {
	return storageOwnerHex(s, 32) || storageOwnerHex(s, 64)
}

var storageOwnerRoleNames = [28]string{1: "", 2: "storage-budget", 3: "", 4: "storage-installation.claim.json", 5: "storage-installation.json", 6: ".storage-bootstrap.lock", 7: ".storage-use.lock", 8: ".home-use.lock", 9: "storage-owner.a", 10: "storage-owner.b", 11: "storage-host-intents", 12: "lock", 13: "checkpoint.a", 14: "checkpoint.b", 15: "tickets.a", 16: "tickets.b", 17: "manifests", 18: ".data.lock", 19: ".data-work.lock", 23: ".jpack-data.json", 24: "d5-provider.json", 25: ".data-transfer.lock", 26: storageTransferBankA, 27: storageTransferBankB}
var storageOwnerRoleLengths = [28]uint64{4: 4096, 5: 4096, 9: 8388608, 10: 8388608, 11: 2097152, 13: 4194304, 14: 4194304, 15: 1048576, 16: 1048576, 17: 33554432, 26: storageTransferBankBytes, 27: storageTransferBankBytes}

func (c storageOwnerCatalogV2) validateObjects(record storageOwnerRecordV2) error {
	counts := [28]int{}
	edges := map[struct {
		Parent uint32
		Name   string
	}]bool{}
	for i, o := range c.Objects {
		if o.Role < 1 || o.Role > 27 || o.Role >= 25 && c.Version != 3 || o.Source < 1 || o.Source > 3 || !o.Persistent.valid(o.ModeClass) || o.ModeClass < 1 || o.ModeClass > 2 || i > 0 && storageOwnerPOCompare(c.Objects[i-1].Persistent, o.Persistent) >= 0 {
			return storagebudget.ErrBounds
		}
		counts[o.Role]++
		kind := uint8(2)
		if o.Role <= 3 || o.Role >= 20 && o.Role <= 22 {
			kind = 1
		}
		if o.ModeClass != kind || o.OwnerClass != 1 && o.Source < 3 || o.OwnerClass != 2 && o.Source == 3 {
			return storagebudget.ErrBounds
		}
		if o.Role == 23 {
			if o.FixedLength < 1 || o.FixedLength > 4096 {
				return storagebudget.ErrBounds
			}
		} else if o.Role == 24 {
			if o.FixedLength < 1 || o.FixedLength > 159 {
				return storagebudget.ErrBounds
			}
		} else if o.FixedLength != storageOwnerRoleLengths[o.Role] {
			return storagebudget.ErrBounds
		}
		boundary := o.Parent == storageOwnerNoObject
		if boundary {
			if len(o.Name) != 0 || !(o.Role == 1 && o.Source == 1 || o.Role == 3 && o.Source == 2 || o.Role >= 20 && o.Role <= 22 && o.Source == 3) {
				return storagebudget.ErrBounds
			}
		} else {
			if int(o.Parent) >= len(c.Objects) || !storageOwnerCatalogNameComponent(o.Name) {
				return storagebudget.ErrBounds
			}
			p := c.Objects[o.Parent]
			if p.ModeClass != 1 || o.Source != p.Source {
				return storagebudget.ErrBounds
			}
			key := struct {
				Parent uint32
				Name   string
			}{o.Parent, string(o.Name)}
			if edges[key] {
				return storagebudget.ErrBounds
			}
			edges[key] = true
			wantParent := uint8(1)
			switch {
			case o.Role == 2 || o.Role >= 4 && o.Role <= 11 || o.Role == 18 || o.Role == 19 || o.Role >= 25:
				if o.Source != 1 || p.Role != wantParent {
					return storagebudget.ErrBounds
				}
			case o.Role >= 12 && o.Role <= 17:
				if o.Source != 1 || p.Role != 2 {
					return storagebudget.ErrBounds
				}
			case o.Role == 20 || o.Role == 21:
				// CATC2: inherited Source1 under config/private; Source3 under
				// private only. A named config-to-Source3 edge is impossible.
				if !(o.Source == 1 && (p.Role == 1 || p.Role == 20) || o.Source == 3 && p.Role == 20) {
					return storagebudget.ErrBounds
				}
			case o.Role == 23:
				if p.Role != 20 && p.Role != 21 {
					return storagebudget.ErrBounds
				}
			case o.Role == 24:
				if o.Source != 3 || p.Role != 22 {
					return storagebudget.ErrBounds
				}
			default:
				return storagebudget.ErrBounds
			}
		}
		if o.Role != 20 && o.Role != 21 && string(o.Name) != storageOwnerRoleNames[o.Role] {
			return storagebudget.ErrBounds
		}
	}
	for role := 1; role <= 19; role++ {
		want := 1
		if role == 3 {
			want = 0
			if c.OriginalOwner.Kind == 2 {
				want = 1
			}
		}
		if role == 18 || role == 19 {
			if counts[role] > 1 {
				return storagebudget.ErrBounds
			}
			continue
		}
		if counts[role] != want {
			return storagebudget.ErrBounds
		}
	}
	// The unpublished v3 transfer feature is all-or-none. V2 readers refuse
	// these roles, rather than silently omitting their control promises.
	if counts[25] > 1 || counts[26] > 1 || counts[27] > 1 || counts[25] != counts[26] || counts[26] != counts[27] {
		return storagebudget.ErrBounds
	}
	// Parent indices can point forward; color the independent graph rather than
	// assuming serialized order proves ancestry. Regular parents already refuse.
	color := make([]uint8, len(c.Objects))
	var walk func(uint32) bool
	walk = func(i uint32) bool {
		if color[i] == 1 {
			return false
		}
		if color[i] == 2 {
			return true
		}
		color[i] = 1
		p := c.Objects[i].Parent
		if p != storageOwnerNoObject && !walk(p) {
			return false
		}
		color[i] = 2
		return true
	}
	for i := range c.Objects {
		if !walk(uint32(i)) {
			return storagebudget.ErrBounds
		}
	}
	witnesses := map[uint8]storageOwnerPersistentObject{1: record.Config, 2: record.Anchor, 6: record.BootstrapLock, 7: record.UseLock, 8: record.HomeLease, 9: record.CatalogA, 10: record.CatalogB}
	for _, o := range c.Objects {
		if p, ok := witnesses[o.Role]; ok && p != o.Persistent {
			return storagebudget.ErrBounds
		}
		if o.Role == 3 && o.Persistent != c.OriginalOwner.HomeObject {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

func (c storageOwnerCatalogV2) directoryPO(p storageOwnerPersistentObject) bool {
	if !p.valid(1) {
		return false
	}
	for _, o := range c.Objects {
		if o.Persistent == p {
			return true
		}
	}
	return false
}
func (c storageOwnerCatalogV2) validateControls() error {
	for i, v := range c.OwnerControls {
		if int(v.ParentObject) >= len(c.Objects) || c.Objects[v.ParentObject].ModeClass != 1 || len(v.OwnerKey) == 0 || len(v.OwnerKey) > 4096 {
			return storagebudget.ErrBounds
		}
		if i > 0 {
			prev := c.OwnerControls[i-1]
			n := storageOwnerPOCompare(c.Objects[prev.ParentObject].Persistent, c.Objects[v.ParentObject].Persistent)
			if n > 0 || n == 0 && bytes.Compare(prev.Name, v.Name) >= 0 {
				return storagebudget.ErrBounds
			}
		}
		p := c.Objects[v.ParentObject]
		l := v.Location
		switch v.Role {
		case 1:
			if c.OriginalOwner.Kind != 2 || p.Role != 1 || string(v.Name) != "desks.json" || v.OwnerKind != 1 || string(v.OwnerKey) != c.OriginalOwner.HomeOwner || v.MaxBytes != 1048576 || l.Version != 0 || len(l.Path) != 0 || len(l.Previous) != 0 || l.Selected != (storageOwnerPersistentObject{}) || l.PreviousSelected != (storageOwnerPersistentObject{}) {
				return storagebudget.ErrBounds
			}
		case 2:
			if p.Role != 1 && p.Role != 20 || string(v.Name) != "data-location.json" || v.OwnerKind != 2 || !storageOwnerIdentityString(string(v.OwnerKey)) || v.MaxBytes != 16384 || l.Version != 1 || !storageEnrollmentCatalogPath(c, l.Path) || !c.directoryPO(l.Selected) {
				return storagebudget.ErrBounds
			}
			if len(l.Previous) == 0 {
				if l.PreviousSelected != (storageOwnerPersistentObject{}) {
					return storagebudget.ErrBounds
				}
			} else if !storageEnrollmentCatalogPath(c, l.Previous) || !c.directoryPO(l.PreviousSelected) {
				return storagebudget.ErrBounds
			}
		default:
			return storagebudget.ErrBounds
		}
		d, err := c.controlDigest(v)
		if err != nil || d != v.RelationDigest {
			return storagebudget.ErrBounds
		}
	}
	return nil
}

func (c storageOwnerCatalogV2) validateOriginal(record storageOwnerRecordV2) error {
	s := c.OriginalOwner
	if !storageOwnerAbsolute(s.ConfigPath) || s.HomeOwner != record.HomeOwner {
		return storagebudget.ErrBounds
	}
	switch s.Kind {
	case 1:
		if len(s.HomePath) != 0 || s.HomeObject != (storageOwnerPersistentObject{}) || s.RegistryControl != storageOwnerNoControl || len(s.Entries) != 0 {
			return storagebudget.ErrBounds
		}
	case 2:
		if !storageOwnerAbsolute(s.HomePath) || !s.HomeObject.valid(1) || int(s.RegistryControl) >= len(c.OwnerControls) || c.OwnerControls[s.RegistryControl].Role != 1 {
			return storagebudget.ErrBounds
		}
		folders := map[string]bool{}
		for i, h := range s.Entries {
			folder := strings.ToLower(string(h.Folder))
			if !storageOwnerHex(h.DeskID, 32) || i > 0 && s.Entries[i-1].DeskID >= h.DeskID || !storageOwnerCatalogNameComponent(h.Folder) || !validHomeFolder(string(h.Folder)) || folders[folder] || len(h.HistoryIdentity) == 0 || len(h.HistoryIdentity) > 4096 || !storageOwnerIdentityString(h.StorageOwner) || !storageOwnerIdentityString(h.RunnerWorkspace) {
				return storagebudget.ErrBounds
			}
			folders[folder] = true
			if h.Kind == 2 {
				if string(h.HistoryIdentity) != "desk:"+h.DeskID || h.StorageOwner != h.DeskID || h.RunnerWorkspace != h.DeskID {
					return storagebudget.ErrBounds
				}
			} else if h.Kind != 1 || !validProjectHistoryPath(string(h.HistoryIdentity)) {
				return storagebudget.ErrBounds
			}
		}
	default:
		return storagebudget.ErrBounds
	}
	return nil
}
