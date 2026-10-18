package desk

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The 128 KiB original plus the maximally escaped 128 KiB narrative and base64
// envelope fit inputRequest's existing 2 MiB body bound.
const policySourceByteLimit = 128 << 10

func validPolicySourceName(name string) bool {
	if name == "" || len(name) > 255 || !utf8.ValidString(name) || strings.TrimSpace(name) != name || strings.ContainsAny(name, `/\:`) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return false
		}
	}
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".md")
}
func validPolicySourceMediaType(media string) bool {
	return media == "text/plain" || media == "text/markdown"
}
func validPolicySource(source *policySource) bool {
	return source == nil || source.Origin == "user-upload" && validPolicySourceName(source.Name) && validPolicySourceMediaType(source.MediaType) && source.Content.valid() && source.Content.Bytes <= policySourceByteLimit
}
func samePolicySource(a, b *policySource) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func validatePolicyImport(source policyImport) error {
	if !validPolicySourceName(source.Name) || !validPolicySourceMediaType(source.MediaType) || len(source.Data) > policySourceByteLimit || !utf8.Valid(source.Data) {
		return policyRefusal(400, "invalid-policy-source", "Import a UTF-8 .txt or .md file up to 128 KiB with a simple filename.")
	}
	for _, r := range string(source.Data) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return policyRefusal(400, "invalid-policy-source", "Import plain UTF-8 text without binary or control characters.")
		}
	}
	return nil
}

// Only an authorized policy's retained binding can resolve an original source.
// There is no path, arbitrary digest or source URL read API.
func (s policyService) source(ctx context.Context, id string) (policySource, []byte, error) {
	reply, err := s.get(ctx, id)
	if err != nil {
		return policySource{}, nil, err
	}
	source := reply.Document.Candidate.Source
	if source == nil {
		return policySource{}, nil, policyRefusal(404, CodeNotFound, "This policy has no imported original source.")
	}
	raw, err := s.artifacts.Get(ctx, source.Content)
	if err != nil {
		return policySource{}, nil, err
	}
	if err = validatePolicyImport(policyImport{Name: source.Name, MediaType: source.MediaType, Data: raw}); err != nil {
		return policySource{}, nil, errors.New("retained policy source is inconsistent")
	}
	if err = s.authorized(ctx); err != nil {
		return policySource{}, nil, err
	}
	return *source, raw, nil
}
