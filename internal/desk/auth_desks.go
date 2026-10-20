package desk

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// At most 257 rows scan 128 KiB of project branding each; marks are limited
// to 8 KiB each (under 3 MiB total after base64). Installation branding is read
// once. Invalid/oversized branding uses the shared bundled logo.
// Entry identity is public by design. Never serialize a deskRecord or a
// configuration here: their paths, custody and other settings remain private.
const entryConfigLimit = 128 << 10
const entryMarkLimit = 8 << 10

type entryDesk struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Mark *string `json:"mark"`
}
type entryDirectory struct {
	Startup string      `json:"startup"`
	Desks   []entryDesk `json:"desks"`
}

func (s *Server) handleEntryDesks(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if value, _, err := net.SplitHostPort(host); err == nil {
		host = value
	}
	ip := net.ParseIP(host)
	if !localHomeRequest(r) || (host != "localhost" && (ip == nil || !ip.IsLoopback())) || !s.originAllowed(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		signInFailure(w, http.StatusForbidden, "origin")
		return
	}
	if r.Method != http.MethodGet || r.URL.RawQuery != "" {
		signInFailure(w, http.StatusBadRequest, "invalid-settings")
		return
	}
	owner := s.deskOwner()
	owner.desksMu.Lock()
	defer owner.desksMu.Unlock()
	directory, err := owner.entryDirectory()
	if err != nil {
		signInFailure(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	signInReply(w, http.StatusOK, directory)
}

// This reads metadata only. In particular it never calls openDeskLocked, New,
// resumeDesks, or registryRoot(true), even for a registered but unopened desk.
func (s *Server) entryDirectory() (entryDirectory, error) {
	out := entryDirectory{Desks: []entryDesk{}}
	fallback := s.entryDefaultMark()
	if s.home != nil {
		home, err := readDeskHome(s.configDir)
		if err != nil {
			return out, err
		}
		root, err := s.registryRoot(false)
		if err != nil {
			return out, err
		}
		defer root.Close()
		for _, entry := range home.Registry.Desks {
			id := entry.ID
			if filepath.Join(home.Path, entry.Folder) == s.projectDir {
				id = ""
			}
			if entry.ID == home.Registry.Startup {
				out.Startup = id
			}
			row := entryDesk{ID: id, Name: entry.Name}
			// A missing folder remains a known choice; authenticated opening reports
			// its error. An unknown ID is never replaced with the owner alias.
			if info, err := root.Lstat(entry.Folder); err == nil && deskFolderAccepted(info) {
				if folder, err := root.OpenRoot(entry.Folder); err == nil {
					if held, err := folder.Stat("."); err == nil && os.SameFile(info, held) {
						row.Mark = projectEntryMark(folder, fallback)
					}
					folder.Close()
				}
			}
			out.Desks = append(out.Desks, row)
		}
	} else {
		owner, err := s.deskRecord()
		if err != nil {
			return out, err
		}
		out.Desks = append(out.Desks, entryDesk{ID: "", Name: owner.Name, Mark: owner.Mark})
		if !s.assistant.usable() {
			return out, nil
		}
		root, err := s.registryRoot(false)
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		defer root.Close()
		dir, err := root.Open(".")
		if err != nil {
			return out, err
		}
		defer dir.Close()
		entries, err := dir.ReadDir(registryReadLimit + 1)
		if err != nil && err != io.EOF || len(entries) > registryReadLimit {
			return out, errors.New("invalid registry")
		}
		for _, entry := range entries {
			if !deskIDPattern.MatchString(entry.Name()) || entry.Name() == s.cfg.deskID {
				continue
			}
			info, err := root.Lstat(entry.Name())
			if err != nil || !deskFolderAccepted(info) {
				continue
			}
			folder, err := root.OpenRoot(entry.Name())
			if err != nil {
				continue
			}
			saved, err := readDeskManifest(folder, entry.Name())
			if err == nil {
				out.Desks = append(out.Desks, entryDesk{ID: saved.ID, Name: saved.Name, Mark: projectEntryMark(folder, fallback)})
			}
			folder.Close()
			if len(out.Desks) > maxDesks+1 {
				return out, errors.New("invalid registry")
			}
		}
	}
	sort.Slice(out.Desks, func(i, j int) bool { return out.Desks[i].Name < out.Desks[j].Name })
	return out, nil
}

// Read the same whole organization section precedence as effectiveConfig:
// accepted project file, else accepted installation file, else the default.
// Only the approved image value crosses the anonymous boundary.
func (s *Server) entryDefaultMark() *string {
	if !s.assistant.usable() {
		return nil
	}
	raw, err := readPrivateData(s.assistant.root, deskConfigName, entryConfigLimit)
	if err != nil {
		return nil
	}
	mark, _ := organizationMark(raw)
	return mark
}
func (s *Server) entryMark(root *os.Root) *string {
	return projectEntryMark(root, s.entryDefaultMark())
}
func projectEntryMark(root *os.Root, mark *string) *string {
	info, err := root.Lstat(projectConfigName)
	if err != nil || !info.Mode().IsRegular() {
		return mark
	}
	file, err := root.OpenFile(projectConfigName, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return mark
	}
	defer file.Close()
	held, err := file.Stat()
	if err != nil || !os.SameFile(info, held) {
		return mark
	}
	raw, err := readBounded(file, entryConfigLimit)
	if err != nil {
		return mark
	}
	if value, present := organizationMark(raw); present {
		return value
	}
	return mark
}
func organizationMark(raw []byte) (*string, bool) {
	if decodeDeskFile(raw).refused() {
		return nil, false
	}
	var value struct {
		Organization *struct {
			Mark *string `json:"mark"`
		} `json:"organization"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Organization == nil {
		return nil, false
	}
	if value.Organization.Mark == nil {
		return nil, true
	}
	return approvedEntryMark(*value.Organization.Mark), true
}

// Self-contained image resources only. SVG is parsed with a small graphics
// allowlist; scripts, foreign content, CSS, external resources and entities are
// rejected. It is always used through <img>, never injected as markup.
func approvedEntryMark(mark string) *string {
	if len(mark) > entryMarkLimit {
		return nil
	}
	mark = strings.TrimSpace(mark)
	raw := []byte(mark)
	if strings.HasPrefix(mark, "data:image/") {
		kind, data, ok := strings.Cut(mark, ",")
		if !ok {
			return nil
		}
		if strings.HasSuffix(kind, ";base64") {
			var err error
			raw, err = base64.StdEncoding.DecodeString(data)
			if err != nil {
				return nil
			}
		} else {
			var err error
			decoded, err := url.PathUnescape(data)
			if err != nil {
				return nil
			}
			raw = []byte(decoded)
		}
		if len(raw) > entryMarkLimit {
			return nil
		}
		if kind != "data:image/svg+xml" && kind != "data:image/svg+xml;base64" {
			if kind != "data:image/png;base64" && kind != "data:image/jpeg;base64" && kind != "data:image/gif;base64" {
				return nil
			}
			config, format, err := image.DecodeConfig(bytes.NewReader(raw))
			if err != nil || config.Width > 4096 || config.Height > 4096 || "data:image/"+format+";base64" != kind {
				return nil
			}
			return &mark
		}
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	elements := map[string]bool{}
	for _, name := range strings.Fields("svg g path rect circle ellipse line polyline polygon defs linearGradient radialGradient stop title desc clipPath use") {
		elements[name] = true
	}
	attrs := map[string]bool{}
	for _, name := range strings.Fields("xmlns version id viewBox width height x y x1 y1 x2 y2 cx cy r rx ry d points transform fill fill-rule fill-opacity stroke stroke-width stroke-linecap stroke-linejoin stroke-opacity opacity offset stop-color stop-opacity gradientUnits gradientTransform clip-path href preserveAspectRatio") {
		attrs[name] = true
	}
	depth, roots := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		switch node := token.(type) {
		case xml.StartElement:
			if !elements[node.Name.Local] || (node.Name.Space != "" && node.Name.Space != "http://www.w3.org/2000/svg") {
				return nil
			}
			if depth == 0 {
				roots++
				if node.Name.Local != "svg" || roots != 1 {
					return nil
				}
			}
			depth++
			for _, attr := range node.Attr {
				if !attrs[attr.Name.Local] || (attr.Name.Space != "" && attr.Name.Space != "http://www.w3.org/1999/xlink" && attr.Name.Space != "xmlns") {
					return nil
				}
				val := strings.TrimSpace(attr.Value)
				if attr.Name.Local != "xmlns" && strings.ContainsAny(val, "\\:") {
					return nil
				}
				if attr.Name.Local == "xmlns" && val != "http://www.w3.org/2000/svg" {
					return nil
				}
				if attr.Name.Local == "href" && !strings.HasPrefix(val, "#") {
					return nil
				}
				if strings.Contains(strings.ToLower(val), "url") && !(strings.HasPrefix(val, "url(#") && strings.HasSuffix(val, ")") && strings.Count(val, "(") == 1) {
					return nil
				}
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(node)) != "" {
				return nil
			}
		case xml.Comment:
		default:
			return nil
		}
	}
	if roots != 1 || depth != 0 {
		return nil
	}
	value := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(raw)
	return &value
}
