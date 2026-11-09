package desk

import (
	"bytes"
	"encoding/json"
	"path/filepath"
)

// This plan is continuation of one confirmed home transaction. IDs select only
// fixed installation-private tables; paths never select an owner or provider.
type dialectArtifactHomeMoveProject struct {
	ID                 string   `json:"id"`
	Folder             string   `json:"folder"`
	From               homePath `json:"from"`
	To                 homePath `json:"to"`
	Identity           string   `json:"identity"`
	PrivateIdentity    string   `json:"privateIdentity"`
	MarkerIdentity     string   `json:"markerIdentity"`
	MarkerSHA256       string   `json:"markerSHA256"`
	MarkerLockIdentity string   `json:"markerLockIdentity"`
	OwnerLockIdentity  string   `json:"ownerLockIdentity"`
	Owner              string   `json:"owner"`
	Namespace          string   `json:"namespace"`
	Kind               string   `json:"kind"`
	Provider           string   `json:"provider"`
	ArtifactsIdentity  string   `json:"artifactsIdentity"`
}
type dialectArtifactHomeMovePin struct {
	Table          string `json:"table"`
	ID             string `json:"id"`
	Namespace      string `json:"namespace"`
	Identity       string `json:"identity"`
	FolderIdentity string `json:"folderIdentity"`
	SHA256         string `json:"sha256"`
}
type dialectArtifactHomeMoveRow struct {
	Table          string `json:"table"`
	ID             string `json:"id"`
	Identity       string `json:"identity"`
	FolderIdentity string `json:"folderIdentity"`
	BeforeKind     string `json:"beforeKind"`
	Before         []byte `json:"before"`
	BeforeSHA256   string `json:"beforeSHA256"`
	After          []byte `json:"after"`
	AfterSHA256    string `json:"afterSHA256"`
	Write          int    `json:"write"`
}
type dialectArtifactHomeMoveParent struct {
	Path     homePath `json:"path"`
	Identity string   `json:"identity"`
}
type dialectArtifactHomeMovePlan struct {
	Version             int                              `json:"version"`
	RenameParents       []dialectArtifactHomeMoveParent  `json:"renameParents"`
	ConfigIdentity      string                           `json:"configIdentity"`
	LeaseIdentity       string                           `json:"leaseIdentity"`
	WorkLockIdentity    string                           `json:"workLockIdentity"`
	PrivateLockIdentity string                           `json:"privateLockIdentity"`
	SecretsIdentity     string                           `json:"secretsIdentity"`
	MetadataIdentity    string                           `json:"metadataIdentity"`
	WorkDomain          string                           `json:"workDomain"`
	SourceHome          homePath                         `json:"sourceHome"`
	SourceHomeIdentity  string                           `json:"sourceHomeIdentity"`
	DestinationIdentity string                           `json:"destinationIdentity"`
	DestinationCreated  bool                             `json:"destinationCreated"`
	RegistryOwner       string                           `json:"registryOwner"`
	RegistryIdentity    string                           `json:"registryIdentity"`
	RegistrySHA256      string                           `json:"registrySHA256"`
	RegistryAfterSHA256 string                           `json:"registryAfterSHA256"`
	Projects            []dialectArtifactHomeMoveProject `json:"projects"`
	Pins                []dialectArtifactHomeMovePin     `json:"pins"`
	Rows                []dialectArtifactHomeMoveRow     `json:"rows"`
}

func homeMoveRowKey(table, id, namespace string) ([]string, string, int, bool) {
	if !assistantRunDigest(id) {
		return nil, "", 0, false
	}
	switch table {
	case "owners":
		return []string{table, id[:2]}, id[2:] + ".json", dialectArtifactOwnerRecordBytes, namespace == ""
	case "native":
		return []string{table, namespace}, id + ".json", dialectArtifactOwnerScopeBytes, assistantRunDigest(namespace)
	case "work-domains":
		return []string{table, id[:2]}, id[2:] + ".json", dialectArtifactWorkDomainRecordBytes, namespace == ""
	case "home-anchors":
		return []string{table, id[:2]}, id[2:] + ".json", dialectArtifactHomeAnchorRecordBytes, namespace == ""
	case "owner-locations":
		return []string{table, id[:2]}, id[2:] + ".json", dialectArtifactLocationRecordBytes, namespace == ""
	}
	return nil, "", 0, false
}
func homeMoveRowPath(config, table, id string) string {
	parts, name, _, ok := homeMoveRowKey(table, id, "")
	if !ok {
		return ""
	}
	return filepath.Join(append([]string{config, secretsDirName, dialectArtifactOwnersFolder}, append(parts, name)...)...)
}
func homeMoveRowValid(row dialectArtifactHomeMoveRow, tx *deskHomeTransaction, p *dialectArtifactHomeMovePlan) bool {
	_, _, _, ok := homeMoveRowKey(row.Table, row.ID, "")
	if !ok || row.After == nil || row.Before == nil || row.Write < 0 || row.Write >= len(tx.Writes)-1 || row.AfterSHA256 != digestOf(row.After) {
		return false
	}
	if row.BeforeKind == "present" {
		if !dialectArtifactOwnerFileIdentity(row.Identity) || !dialectArtifactOwnerFileIdentity(row.FolderIdentity) || row.BeforeSHA256 != digestOf(row.Before) {
			return false
		}
	} else if row.BeforeKind != "absent" || len(row.Before) != 0 || row.BeforeSHA256 != "absent" || row.Identity != "" || row.Table != "home-anchors" {
		return false
	}
	write := tx.Writes[row.Write]
	if write.Path != homeMoveRowPath(filepath.Join(tx.Destination, deskHomePlatform), row.Table, row.ID) || write.Before != row.BeforeSHA256 || !bytes.Equal(write.Data, row.After) || write.Executable {
		return false
	}
	switch row.Table {
	case "work-domains":
		before, e1 := readDialectArtifactWorkDomainRecord(row.Before)
		after, e2 := readDialectArtifactWorkDomainRecord(row.After)
		return e1 == nil && e2 == nil && before.ID == row.ID && after.ID == row.ID && row.ID == p.WorkDomain && before.FolderIdentity == p.ConfigIdentity && after.FolderIdentity == p.ConfigIdentity && before.LockIdentity == p.WorkLockIdentity && after.LockIdentity == p.WorkLockIdentity && bytes.Equal(before.PathBytes, []byte(tx.Config)) && bytes.Equal(after.PathBytes, []byte(filepath.Join(tx.Destination, deskHomePlatform)))
	case "home-anchors":
		after, e := readDialectArtifactHomeAnchorRecord(row.After)
		if e != nil || after.ID != row.ID || after.FolderIdentity != p.DestinationIdentity || after.ID != dialectArtifactHomeAnchorID(p.RegistryOwner, p.DestinationIdentity) || !bytes.Equal(after.PathBytes, []byte(tx.Destination)) {
			return false
		}
		if row.BeforeKind == "absent" {
			return p.SourceHome == ""
		}
		before, e := readDialectArtifactHomeAnchorRecord(row.Before)
		return e == nil && before.ID == after.ID && before.FolderIdentity == after.FolderIdentity && (bytes.Equal(before.PathBytes, []byte(p.SourceHome)) || p.SourceHome == "" && bytes.Equal(row.Before, row.After))
	case "owner-locations":
		before, e1 := readDialectArtifactLocationRecord(row.Before)
		after, e2 := readDialectArtifactLocationRecord(row.After)
		ownerHash := ""
		for _, pin := range p.Pins {
			if pin.Table == "owners" && pin.ID == row.ID {
				ownerHash = pin.SHA256
			}
		}
		if e1 != nil || e2 != nil || before.Owner != row.ID || after.Owner != row.ID || before.FolderIdentity != after.FolderIdentity || before.LegacySHA256 != after.LegacySHA256 || before.LegacySHA256 != ownerHash || before.WorkDomain != p.WorkDomain || after.WorkDomain != p.WorkDomain {
			return false
		}
		for _, project := range p.Projects {
			if project.Owner == row.ID && project.Kind == "file" {
				return before.FolderIdentity == project.Identity && after.Location.Kind == "home" && after.Location.Anchor == dialectArtifactHomeAnchorID(p.RegistryOwner, p.DestinationIdentity) && string(after.Location.PathBytes) == project.Folder
			}
		}
	}
	return false
}
func (p *dialectArtifactHomeMovePlan) valid(tx *deskHomeTransaction) bool {
	if p == nil || p.Version != 2 || p.RenameParents == nil || p.Projects == nil || p.Pins == nil || p.Rows == nil || len(p.Projects) == 0 || len(p.Projects) > maxDesks+1 || !assistantRunDigest(p.RegistryOwner) || p.WorkDomain != dialectArtifactWorkDomainID(p.ConfigIdentity, p.WorkLockIdentity) {
		return false
	}
	parents := homeMoveOriginalParents(tx.Renames)
	if len(parents) != len(p.RenameParents) {
		return false
	}
	for i, pin := range p.RenameParents {
		if string(pin.Path) != parents[i] || !dialectArtifactOwnerFileIdentity(pin.Identity) {
			return false
		}
		if string(pin.Path) == tx.Config && pin.Identity != p.ConfigIdentity || string(pin.Path) == string(p.SourceHome) && pin.Identity != p.SourceHomeIdentity {
			return false
		}
		for _, project := range p.Projects {
			if pin.Path == project.From && pin.Identity != project.Identity {
				return false
			}
		}
	}
	for _, id := range []string{p.ConfigIdentity, p.LeaseIdentity, p.WorkLockIdentity, p.PrivateLockIdentity, p.SecretsIdentity, p.MetadataIdentity, p.DestinationIdentity} {
		if !dialectArtifactOwnerFileIdentity(id) {
			return false
		}
	}
	if p.SourceHome != "" {
		if !validProjectHistoryPath(string(p.SourceHome)) || p.SourceHomeIdentity != p.DestinationIdentity || !assistantRunDigest(p.RegistrySHA256) || !dialectArtifactOwnerFileIdentity(p.RegistryIdentity) || p.RegistryAfterSHA256 != "" {
			return false
		}
	} else if p.SourceHomeIdentity != "" || p.RegistryIdentity != "" || p.RegistrySHA256 != "absent" || !assistantRunDigest(p.RegistryAfterSHA256) {
		return false
	}
	ids, owners, rows := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, project := range p.Projects {
		if !deskIDPattern.MatchString(project.ID) || !validHomeFolder(project.Folder) || ids[project.ID] || !validProjectHistoryPath(string(project.From)) || string(project.To) != filepath.Join(tx.Destination, project.Folder) || tx.relocate(string(project.From)) != string(project.To) || !dialectArtifactOwnerFileIdentity(project.Identity) {
			return false
		}
		ids[project.ID] = true
		if project.PrivateIdentity != "" && !dialectArtifactOwnerFileIdentity(project.PrivateIdentity) {
			return false
		}
		if project.Owner != "" {
			if !assistantRunDigest(project.Owner) || owners[project.Owner] || !assistantRunDigest(project.Namespace) || !assistantRunDigest(project.MarkerSHA256) || !dialectArtifactOwnerFileIdentity(project.MarkerIdentity) || !dialectArtifactOwnerFileIdentity(project.MarkerLockIdentity) || !dialectArtifactOwnerFileIdentity(project.OwnerLockIdentity) || project.Kind != "file" && project.Kind != "injected" {
				return false
			}
			owners[project.Owner] = true
			if project.Kind == "injected" && !assistantRunDigest(project.Provider) || project.Kind == "file" && project.Provider != "" {
				return false
			}
		} else if project.Kind != "" || project.Provider != "" || project.Namespace != "" || project.MarkerSHA256 != "" || project.MarkerIdentity != "" || project.MarkerLockIdentity != "" || project.OwnerLockIdentity != "" {
			return false
		}
		if project.ArtifactsIdentity != "" && !dialectArtifactOwnerFileIdentity(project.ArtifactsIdentity) {
			return false
		}
	}
	for _, pin := range p.Pins {
		_, _, _, ok := homeMoveRowKey(pin.Table, pin.ID, pin.Namespace)
		key := pin.Table + ":" + pin.ID + ":" + pin.Namespace
		if !ok || pin.Table != "owners" && pin.Table != "native" || rows[key] || !owners[pin.ID] || !assistantRunDigest(pin.SHA256) || !dialectArtifactOwnerFileIdentity(pin.Identity) || !dialectArtifactOwnerFileIdentity(pin.FolderIdentity) {
			return false
		}
		rows[key] = true
	}
	indexes := map[int]bool{}
	work := false
	home := false
	for _, row := range p.Rows {
		key := row.Table + ":" + row.ID
		if rows[key] || indexes[row.Write] || !homeMoveRowValid(row, tx, p) {
			return false
		}
		rows[key] = true
		indexes[row.Write] = true
		if row.Table == "work-domains" {
			work = true
		}
		if row.Table == "home-anchors" {
			home = true
		}
	}
	if !work || len(owners) > 0 && !home {
		return false
	}
	for _, project := range p.Projects {
		if project.Owner != "" {
			if !rows["owners:"+project.Owner+":"] || !rows["native:"+project.Owner+":"+project.Namespace] || project.Kind == "file" && !rows["owner-locations:"+project.Owner] {
				return false
			}
		}
	}
	return true
}
func homeMoveSnapshotDigest(p *dialectArtifactHomeMovePlan) string {
	if p == nil {
		return "absent"
	}
	raw, _ := json.Marshal(p)
	return digestOf(raw)
}
