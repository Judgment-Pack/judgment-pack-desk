package desk

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Project Files edits authored inputs. Runtime lockfiles and audit output have
// dedicated writers and remain inspectable without accepting arbitrary saves.
//
// **Names are compared without case, and files by identity.** On a
// case-insensitive filesystem (APFS, NTFS) `AUDIT/Evaluations.JSONL` and
// `JPACK.LOCK.JSON` are the record and the lock, and a declared directory
// `Records` is the folder `records`: a comparison by spelling would let either
// be replaced as an ordinary file. So the lock's name, the record names, the
// default audit directories and the declared one are compared with
// `strings.EqualFold`, as `isExcludedName` compares `.git`; and a file that
// exists is also compared, with `os.SameFile`, against the lock and the records
// at their canonical paths, which holds whatever spelling (case, Unicode
// normalization, a hard link) reaches them.
type fileAccessPolicy struct {
	auditDir string
	// auditDirUnknown is true when jpack.json is there but could not be read
	// or decoded now, so where the runtime writes its records is not known.
	// Every file with a record's name is then held read-only, wherever it is:
	// a read that failed is never taken to mean that no directory is declared.
	auditDirUnknown bool
	// canonical is what is at the lock's and the records' own paths now, each
	// with the reason a file that is the same file is read-only.
	canonical []canonicalFile
	// inCustody is whether a project path is in Desk's own custody
	// (`signingCustody`, issue #329); nil holds nothing.
	inCustody func(name string) bool
}

type canonicalFile struct {
	info   fs.FileInfo
	reason string
}

// auditRecordNames are the files the runtime writes in an audit directory.
var auditRecordNames = []string{"evaluations.jsonl", "signatures.jsonl", "stamps.jsonl"}

// defaultAuditDirs are held whatever jpack.json declares.
var defaultAuditDirs = []string{"audit", ".desk-private/audit"}

func (s *Server) fileAccessPolicy() fileAccessPolicy {
	policy := fileAccessPolicy{}
	dir, declared, err := s.projectAuditDir()
	switch {
	case err != nil && codeOf(err) != CodeNotFound:
		policy.auditDirUnknown = true
	case err == nil && declared:
		if _, err := auditDirParts(dir); err == nil {
			policy.auditDir = path.Clean(dir)
		}
	}
	policy.canonical = s.canonicalOutputs(policy.auditDir)
	custody := s.signingCustody()
	policy.inCustody = func(name string) bool { return custody.holds(s.root, name) }
	return policy
}

// signingCustody is Desk's own custody, the `secrets/` folder of its
// configuration folder (the signing keys, their lists, their archive and
// the credentials), as the project's root sees it (issue #329). A project
// that holds Desk's configuration folder, or that is held in it, reaches
// those files by an ordinary path, and Project Files writes, replaces and
// removes none of them: a key leaves Desk's custody only by the owner's
// Remove, under the signing folder's lock.
//
// **Each custody folder resolved on its own** (review round 1 of #335,
// finding 1): `secrets/`, the signing folder, its archive, Runner's folder
// and Runner's archive, each through every symbolic link on its way, so
// that one moved into the project, with a link left at its own name, is
// found where it is. A path is in custody by its spelling under the
// project, compared without case, and by the identity of any folder on its
// way, which holds whatever spelling reaches it (a case-insensitive
// volume's, a bind mount's).
type signingCustody struct {
	// roots are the custody's folders, each resolved.
	roots []custodyRoot
	// rels are the roots' paths in the project, slash-separated, where the
	// project holds them.
	rels []string
	// whole is a project in Desk's custody: every path of it is.
	whole bool
}

// custodyRoot is one folder of Desk's custody: its path, with every symbolic
// link on its way resolved, and the folder found there; info is nil where
// it is not there.
type custodyRoot struct {
	path string
	info fs.FileInfo
}

// custodyRootAt is the custody folder named path, resolved through every
// symbolic link on its way; where it is not there, its path as named.
func custodyRootAt(path string) custodyRoot {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		if info, err := os.Stat(resolved); err == nil && info.IsDir() {
			return custodyRoot{path: resolved, info: info}
		}
	}
	return custodyRoot{path: path}
}

// withinPath is whether rel, a filepath.Rel answer, stays at or under its
// base.
func withinPath(rel string) bool {
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// signingCustody is Desk's custody as this server's project sees it now.
func (s *Server) signingCustody() signingCustody {
	var custody signingCustody
	if s.configDir == "" {
		return custody
	}
	secrets := filepath.Join(s.configDir, secretsDirName)
	signing := filepath.Join(secrets, signingDirName)
	runner := filepath.Join(signing, runnerSigningDirName)
	for _, path := range []string{secrets, signing, filepath.Join(signing, archiveDirName), runner, filepath.Join(runner, archiveDirName)} {
		root := custodyRootAt(path)
		if root.info == nil {
			if path != secrets {
				continue
			}
			// Not there yet: found by the path it would take.
			if resolved, err := filepath.EvalSymlinks(s.configDir); err == nil {
				root.path = filepath.Join(resolved, secretsDirName)
			}
		}
		custody.roots = append(custody.roots, root)
		if rel, err := filepath.Rel(s.projectDir, root.path); err == nil && rel != "." && withinPath(rel) {
			custody.rels = append(custody.rels, filepath.ToSlash(rel))
		}
		// The project in it: the project's own folder, or one above it, is
		// the folder itself.
		for dir := s.projectDir; root.info != nil && !custody.whole; {
			if info, err := os.Lstat(dir); err == nil && os.SameFile(info, root.info) {
				custody.whole = true
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return custody
}

// holds is whether clean, a project path, is in Desk's custody: the project
// is, or the path is a custody folder or under one, by its spelling,
// compared without case, or by the identity of any folder on its way, as
// root finds it.
func (c signingCustody) holds(root *os.Root, clean string) bool {
	if c.whole {
		return true
	}
	parts := strings.Split(clean, "/")
	for _, rel := range c.rels {
		custody := strings.Split(rel, "/")
		if len(parts) >= len(custody) {
			same := true
			for i := range custody {
				same = same && strings.EqualFold(parts[i], custody[i])
			}
			if same {
				return true
			}
		}
	}
	return c.byIdentity(root, parts)
}

// byIdentity is whether any folder on the way to the project path parts,
// as root finds it, is a custody folder itself.
func (c signingCustody) byIdentity(root *os.Root, parts []string) bool {
	if root == nil {
		return false
	}
	for i := range parts {
		info, err := root.Lstat(filepath.Join(parts[:i+1]...))
		if err != nil {
			return false
		}
		for _, held := range c.roots {
			if held.info != nil && os.SameFile(info, held.info) {
				return true
			}
		}
	}
	return false
}

// canonicalOutputs is what is at the lock's path and at each record's path in
// the default and the declared audit directories, where something is there.
// Nothing found is nothing to compare: the names are still compared.
func (s *Server) canonicalOutputs(declared string) []canonicalFile {
	var found []canonicalFile
	add := func(rel, reason string) {
		if info, err := s.root.Lstat(osPath(rel)); err == nil {
			found = append(found, canonicalFile{info: info, reason: reason})
		}
	}
	add(runtimeLockName, "runtime-lock")
	dirs := append([]string{}, defaultAuditDirs...)
	if declared != "" {
		dirs = append(dirs, declared)
	}
	for _, dir := range dirs {
		for _, name := range auditRecordNames {
			add(path.Join(dir, name), "audit-record")
		}
	}
	return found
}

func isAuditRecordName(name string) bool {
	for _, record := range auditRecordNames {
		if strings.EqualFold(name, record) {
			return true
		}
	}
	return false
}

func (p fileAccessPolicy) isAuditDir(dir string) bool {
	for _, held := range defaultAuditDirs {
		if strings.EqualFold(dir, held) {
			return true
		}
	}
	return p.auditDir != "" && strings.EqualFold(dir, p.auditDir)
}

func (p fileAccessPolicy) readOnlyReason(name string, info fs.FileInfo) string {
	if p.inCustody != nil && p.inCustody(name) {
		return "signing-custody"
	}
	if strings.EqualFold(name, runtimeLockName) {
		return "runtime-lock"
	}
	if isAuditRecordName(path.Base(name)) && (p.auditDirUnknown || p.isAuditDir(path.Dir(name))) {
		return "audit-record"
	}
	if info != nil {
		for _, held := range p.canonical {
			if os.SameFile(info, held.info) {
				return held.reason
			}
		}
		if info.Mode().Perm()&0o222 == 0 {
			return "file-permissions"
		}
	}
	return ""
}

func readOnlyFileMessage(reason string) string {
	switch reason {
	case "runtime-lock":
		return "This lockfile is generated by the runtime. Update it through the pack review and lock workflow."
	case "audit-record":
		return "Audit records are generated by the runtime and cannot be edited in Project files."
	case "signing-custody":
		return signingCustodyWords
	default:
		return "This file is read-only on disk. Change its filesystem permissions before editing it."
	}
}

// signingCustodyWords is what Project Files says of a path in Desk's own
// custody (issue #329).
const signingCustodyWords = "Desk keeps its signing keys and credentials in this folder, and they cannot be edited in Project files. An archived key is removed on the decision record."
