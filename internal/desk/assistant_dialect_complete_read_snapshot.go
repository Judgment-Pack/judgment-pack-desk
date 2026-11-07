package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

// These project inputs belong to the same lexical source read as the collection.
// Check reads only pinned files and paths; it does not authorize or acquire any
// source, work, private-data or artifact owner.
type assistantDialectCompleteProjectFile struct {
	file  *os.File
	info  os.FileInfo
	path  string
	raw   []byte
	limit int
	edges []struct {
		name string
		info os.FileInfo
	}
}
type assistantDialectCompleteProject struct {
	s                *Server
	root             *os.Root
	info             os.FileInfo
	config, pack     *assistantDialectCompleteProjectFile
	key, declaration string
	closed           bool
}

func (p *assistantDialectCompleteProject) open(ctx context.Context, name string, limit int) (_ *assistantDialectCompleteProjectFile, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	clean, err := wireRelativePath(name)
	if err != nil || clean != name {
		return nil, errAssistantDialectCompleteRead
	}
	f := &assistantDialectCompleteProjectFile{path: name, limit: limit}
	for i := range strings.Split(name, "/") {
		part := strings.Join(strings.Split(name, "/")[:i+1], "/")
		info, e := p.root.Lstat(osPath(part))
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.Join(errAssistantDialectCompleteRead, e)
		}
		f.edges = append(f.edges, struct {
			name string
			info os.FileInfo
		}{part, info})
	}
	f.file, err = p.root.OpenFile(osPath(name), os.O_RDONLY|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, closeDialectArtifactOwner(ctx, "complete-project-file", f.file.Close))
		}
	}()
	f.info, err = f.file.Stat()
	if err != nil || !f.info.Mode().IsRegular() {
		return nil, errors.Join(errAssistantDialectCompleteRead, err)
	}
	f.raw, err = readBounded(f.file, limit)
	if err != nil || !validUTF8(f.raw) || jsonscalar.ValidateEscapes(f.raw) != nil {
		return nil, errors.Join(errAssistantDialectCompleteRead, err)
	}
	if err = p.checkFile(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}
func (p *assistantDialectCompleteProject) checkFile(ctx context.Context, f *assistantDialectCompleteProjectFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, edge := range f.edges {
		now, err := p.root.Lstat(osPath(edge.name))
		if err != nil || !os.SameFile(now, edge.info) || now.Mode()&os.ModeSymlink != 0 {
			return errors.Join(errAssistantDialectCompleteRead, err)
		}
	}
	held, err := f.file.Stat()
	if err != nil || !os.SameFile(held, f.info) {
		return errors.Join(errAssistantDialectCompleteRead, err)
	}
	raw, err := readBounded(io.NewSectionReader(f.file, 0, int64(f.limit)+1), f.limit)
	if err != nil || !bytes.Equal(raw, f.raw) {
		return errors.Join(errAssistantDialectCompleteRead, err)
	}
	return nil
}
func (p *assistantDialectCompleteProject) Check(ctx context.Context) error {
	if p == nil || p.closed || p.s.root != p.root || !sameDirectory(p.s.projectDir, p.info) {
		return errAssistantDialectCompleteRead
	}
	held, err := p.root.Stat(".")
	if err != nil || !os.SameFile(held, p.info) {
		return errors.Join(errAssistantDialectCompleteRead, err)
	}
	for _, f := range []*assistantDialectCompleteProjectFile{p.config, p.pack} {
		if f == nil {
			return errAssistantDialectCompleteRead
		}
		if err = p.checkFile(ctx, f); err != nil {
			return err
		}
	}
	return nil
}
func (p *assistantDialectCompleteProject) Close(ctx context.Context) error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	var err error
	for _, f := range []*assistantDialectCompleteProjectFile{p.pack, p.config} {
		if f != nil {
			err = errors.Join(err, closeDialectArtifactOwner(ctx, "complete-project-file", f.file.Close))
		}
	}
	return err
}
func (s *Server) acquireAssistantDialectCompleteProject(ctx context.Context, key string) (_ *assistantDialectCompleteProject, err error) {
	if s == nil || s.root == nil {
		return nil, errAssistantDialectCompleteRead
	}
	p := &assistantDialectCompleteProject{s: s, root: s.root, key: key}
	p.info, err = p.root.Stat(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, p.Close(ctx))
		}
	}()
	p.config, err = p.open(ctx, runtimeConfigName, maxFileBytes)
	if err != nil {
		return nil, err
	}
	// The shared parser keeps exact declaration member digests. This passive
	// root read does not call graphDeclarations/Authorize or enter private data.
	config, err := parseAssistantDecisionDeclarations(p.config.raw)
	if err != nil {
		return nil, err
	}
	entry, ok := config.Packs[key]
	if !ok {
		return nil, errAssistantRunScope
	}
	path, e := wireRelativePath(entry.Path)
	if e != nil || path != entry.Path {
		return nil, errAssistantRunScope
	}
	p.declaration = assistantDeclarationDigest(entry)
	p.pack, err = p.open(ctx, path, 1<<20)
	if err != nil {
		return nil, err
	}
	if err = p.Check(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// Build every final-package field from the actual bounded source owner. There
// is no legacy safe View, candidate snapshot or caller-supplied current state.
func (h *assistantDialectCompleteSourceHold) snapshot(ctx context.Context, p *assistantDialectCompleteProject) (runcontext.DialectSnapshot, error) {
	var out runcontext.DialectSnapshot
	if err := h.Check(ctx); err != nil {
		return out, err
	}
	if p == nil || p.s != h.s {
		return out, errAssistantDialectCompleteRead
	}
	if err := p.Check(ctx); err != nil {
		return out, err
	}
	saved, err := h.Get(h.saved.Document.ID)
	if err != nil {
		return out, err
	}
	doc := saved.Document
	out = runcontext.DialectSnapshot{ID: doc.ID, Revision: saved.SHA256, PackKey: p.key, Name: doc.Name, RecordCount: int64(len(doc.Records)), PackJSON: string(p.pack.raw), Columns: []runcontext.DialectColumn{}, FactTargets: []string{}, EvidenceRequirements: []string{}, Conversions: map[string]runcontext.DialectConversion{}}
	out.ResearchIndex, err = runcontext.DialectResearchIndexOf(h.inventory.Declarations)
	if err != nil {
		return out, err
	}
	facts, err := assistantDialectFacts(p.pack.raw)
	if err != nil {
		return out, err
	}
	for _, fact := range facts {
		out.FactTargets = append(out.FactTargets, fact.Path)
	}
	fields, err := assistantTestObject(p.pack.raw)
	if err != nil {
		return out, err
	}
	if fields["evidenceRequirements"] != nil {
		requirements, e := assistantChatArray(fields["evidenceRequirements"])
		if e != nil {
			return out, e
		}
		for _, raw := range requirements {
			f, e := assistantTestObject(raw)
			if e != nil {
				return out, e
			}
			id, e := assistantTestString(f["id"])
			if e != nil {
				return out, e
			}
			out.EvidenceRequirements = append(out.EvidenceRequirements, id)
		}
	}
	// Exact optional mapping token from the original collection, rather than
	// a marshal of the decoded DTO that would change historical inner bytes.
	root, err := parseRetainedJSON(h.collection.raw)
	if err != nil {
		return out, err
	}
	if q := doc.Recipe.Qualification; q != nil {
		mapping := root.member("recipe").member("qualification").member("mapping")
		if mapping == nil || mapping.kind != '{' {
			return out, errAssistantDialectCompleteRead
		}
		value := string(mapping.raw)
		out.DialectJSON = &value
		for target, c := range q.Conversions {
			conversion := runcontext.DialectConversion{Kind: c.Kind}
			if c.Kind == "boolean" {
				yes, no := c.TrueValue, c.FalseValue
				conversion.TrueValue, conversion.FalseValue = &yes, &no
			}
			out.Conversions[target] = conversion
		}
		if q.ConversionContract != nil {
			if _, err = retainedInputMappingRules(q); err != nil {
				return out, err
			}
			out.RetainedRead = &runcontext.DialectRetainedRead{DocumentJSON: value, Occurrence: runcontext.DialectRetainedOccurrence{Kind: "case"}, FramePointer: "/retained"}
		}
	}
	names := map[string]bool{}
	for _, name := range assistantDialectColumns(doc) {
		names[name] = true
	}
	declarations := map[string]*runcontext.DialectResearchDeclaration{}
	ambiguous := map[string]bool{}
	for _, revision := range h.inventory.Declarations {
		for _, field := range revision.Fields {
			if prior := declarations[field.Name]; prior != nil && (prior.Revision != revision.Revision || prior.FieldJSON != field.FieldJSON) {
				ambiguous[field.Name] = true
			}
			declarations[field.Name] = &runcontext.DialectResearchDeclaration{Revision: revision.Revision, FieldJSON: field.FieldJSON}
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	sourceRows := root.member("records")
	if len(doc.Records) > 0 && (sourceRows == nil || sourceRows.kind != '[' || len(sourceRows.elements) != len(doc.Records)) {
		return out, errAssistantDialectCompleteRead
	}
	for _, name := range ordered {
		column := runcontext.DialectColumn{Name: name, Samples: []runcontext.DialectSample{}}
		if !ambiguous[name] {
			column.Research = declarations[name]
		}
		for i, record := range doc.Records {
			cells := sourceRows.elements[i].member("values")
			if cells == nil || cells.kind == 'n' {
				continue
			}
			if cells.kind != '{' {
				return out, errAssistantDialectCompleteRead
			}
			cell := cells.member(name)
			if cell == nil {
				continue
			}
			raw := bytes.Clone(cell.raw)
			if cell.kind == '"' {
				var value string
				if json.Unmarshal(raw, &value) != nil {
					return out, errAssistantDialectCompleteRead
				}
				value, _ = assistantDialectSampleText(value)
				raw, err = assistantDialectCompleteJSON(value)
				if err != nil {
					return out, err
				}
			} else if cell.kind != 'n' {
				return out, errAssistantDialectCompleteRead
			}
			column.Samples = append(column.Samples, runcontext.DialectSample{ValueJSON: string(raw), Origin: record.Class})
			if len(column.Samples) == runcontext.DialectSamples {
				break
			}
		}
		out.Columns = append(out.Columns, column)
	}
	if err = h.Check(ctx); err != nil {
		return out, err
	}
	if err = p.Check(ctx); err != nil {
		return out, err
	}
	// The actual owner binds the complete snapshot identity and validates exact
	// final indexes/pages, all field guards and the unchanged serialized cap.
	if _, err = h.encode(ctx, out); err != nil {
		return out, err
	}
	return out, nil
}
