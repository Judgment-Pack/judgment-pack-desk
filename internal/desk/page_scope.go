package desk

// Spec dialect §5.1 and §12 (RFC 0018 rounds 15 and 16): compare constrained
// page endpoints as submitted. Do not use net/url here: decoding, cleaning a
// path or normalizing a port would change the reviewed scope. One form and one
// matcher hold every place a scoped page profile is used: registration of its
// scope, admission of each requested URL before acquisition, and the check of
// each acquired page's receipt endpoint (the final URL after redirects) before
// the page can reach the model. Runner's check 3 remains the verifier.
import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type EndpointScope []string

func (s *EndpointScope) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		return errors.New("endpointScope must be an array")
	}
	var entries []string
	if err := json.Unmarshal(raw, &entries); err != nil {
		return err
	}
	*s = entries
	return nil
}

const pageScopeWildcard = "https://*"

var pageHostLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
var pageDotSegment = regexp.MustCompile(`^(?:\.|%2[eE]){1,2}$`)

// Round 16: an escaped slash or backslash anywhere in the path, and the
// delimiters before which a segment's part is held to the dot-segment test.
// Every % in a path of the form begins an escape of two hexadecimal digits
// (pagePercentForm), so a match here is always a whole escape.
var pageEscapedSlash = regexp.MustCompile(`%(?:2[fF]|5[cC])`)
var pageSegmentDelimiter = regexp.MustCompile(`;|%(?:3[bBfF]|23|00)`)

func pageHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func pageASCII(s string) bool {
	for i := range s {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func pagePercentForm(s string) bool {
	for i := range s {
		if s[i] == '%' && (i+2 >= len(s) || !pageHex(s[i+1]) || !pageHex(s[i+2])) {
			return false
		}
	}
	return true
}

func constrainedPageEndpoint(endpoint string) (host, path string, ok bool) {
	if len(endpoint) > 2048 || !pageASCII(endpoint) || !strings.HasPrefix(endpoint, "https://") {
		return "", "", false
	}
	authority := strings.TrimPrefix(endpoint, "https://")
	if slash := strings.IndexByte(authority, '/'); slash >= 0 {
		authority, path = authority[:slash], authority[slash:]
	}
	host, port, hasPort := strings.Cut(authority, ":")
	if hasPort && port != "" && port != "443" {
		return "", "", false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", "", false
	}
	for _, label := range labels {
		if !pageHostLabel.MatchString(label) {
			return "", "", false
		}
	}
	last := labels[len(labels)-1][0]
	if !(last >= 'a' && last <= 'z' || last >= 'A' && last <= 'Z') || !pagePathForm(path) {
		return "", "", false
	}
	return strings.ToLower(host), path, true
}

// pagePathForm is §5.1's path, read without decoding: no ?, # or backslash;
// each % followed by two hexadecimal digits; no escaped slash or backslash
// (%2f, %2F, %5c, %5C) anywhere; and, split at each literal /, no segment
// whose part before its first ;, %3b, %3B, %3f, %3F, %23 or %00, whichever
// comes first, is a dot segment (one or two tokens, each ., %2e or %2E). A
// segment without such a delimiter is its own part, so one test holds both.
// Every other escape, %2e outside a dot segment included, stays literal.
func pagePathForm(path string) bool {
	if strings.ContainsAny(path, "?#\\") || !pagePercentForm(path) || pageEscapedSlash.MatchString(path) {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if at := pageSegmentDelimiter.FindStringIndex(segment); at != nil {
			segment = segment[:at[0]]
		}
		if pageDotSegment.MatchString(segment) {
			return false
		}
	}
	return true
}

func validatePageScope(scope EndpointScope) error {
	if len(scope) > 32 {
		return errors.New("input-over-bound: A page scope has at most 32 entries.")
	}
	for _, entry := range scope {
		if len(entry) > 2048 {
			return errors.New("input-over-bound: A page scope entry has at most 2048 bytes.")
		}
	}
	if len(scope) == 0 {
		return errors.New("input-malformed: Choose at least one page scope entry.")
	}
	seen := map[string]bool{}
	for _, entry := range scope {
		if seen[entry] {
			return errors.New("input-malformed: Page scope entries must be distinct.")
		}
		seen[entry] = true
		if entry == pageScopeWildcard {
			continue
		}
		if _, _, ok := constrainedPageEndpoint(entry); !ok {
			return errors.New("input-malformed: Use constrained HTTPS page prefixes, or explicitly review https://*.")
		}
	}
	return nil
}

func pageEndpointInScope(endpoint string, scope EndpointScope) bool {
	host, path, ok := constrainedPageEndpoint(endpoint)
	if !ok {
		return false
	}
	for _, prefix := range scope {
		if prefix == pageScopeWildcard {
			return true
		}
		prefixHost, prefixPath, valid := constrainedPageEndpoint(prefix)
		if !valid || host != prefixHost {
			continue
		}
		if prefixPath == "" || prefixPath == "/" || path == prefixPath || strings.HasPrefix(path, strings.TrimSuffix(prefixPath, "/")+"/") {
			return true
		}
	}
	return false
}

func requestedPageInScope(requested string, scope EndpointScope) bool {
	if len(requested) > 2048 || !pageASCII(requested) {
		return false
	}
	endpoint, query, _ := strings.Cut(requested, "?")
	return !strings.Contains(query, "#") && pagePercentForm(query) && pageEndpointInScope(endpoint, scope)
}

// Exact HTTP profiles compare the URL before its query byte-for-byte. Queries
// remain acquisition arguments; redirects are checked again at the receipt.
// Fixed-service profiles use their tool contract rather than an HTTP URL.
func admitPortablePage(profile portableProfileDetails, raw json.RawMessage) error {
	refuse := func() error {
		return errors.New("acquisition-invalid: The requested page is invalid or outside the reviewed page profile.")
	}
	if err := validatePortablePageForm(profile); err != nil {
		return err
	}
	var arguments map[string]json.RawMessage
	if rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &arguments) != nil || arguments == nil {
		return refuse()
	}
	if profile.EndpointScope == nil && profile.Shape != "http" {
		return nil
	}
	var requested string
	if json.Unmarshal(arguments["url"], &requested) != nil || requested == "" {
		return refuse()
	}
	if profile.EndpointScope != nil {
		if !requestedPageInScope(requested, profile.EndpointScope) {
			return refuse()
		}
	} else {
		endpoint, query, _ := strings.Cut(requested, "?")
		if len(requested) > 2048 || !pageASCII(requested) || strings.Contains(requested, "#") || !pagePercentForm(query) || endpoint != profile.Endpoint {
			return refuse()
		}
	}
	return nil
}

func validatePortablePageForm(profile portableProfileDetails) error {
	if profile.EndpointScope != nil {
		if err := validatePageScope(profile.EndpointScope); err != nil {
			return fmt.Errorf("acquisition-invalid: %w", err)
		}
		if scopedPageProfile(profile) {
			return nil
		}
	} else if profile.Class == "generated" && (profile.Shape == "http" || profile.Shape == "mcp") && contains(profile.Research.Capabilities, "page") && profile.Endpoint != "" && len(profile.Endpoint) <= 2048 {
		return nil
	}
	return errors.New("acquisition-invalid: The reviewed page profile has no valid endpoint or scope.")
}

// scopedPageProfile is the one profile form the Desk reads a scoped page
// through: class generated, shape http, adapter-web, no exact endpoint, and
// the research capability page alone (§5.1).
func scopedPageProfile(profile portableProfileDetails) bool {
	return profile.Class == "generated" && profile.Shape == "http" && profile.Adapter.Name == "adapter-web" && profile.Endpoint == "" && len(profile.Research.Capabilities) == 1 && profile.Research.Capabilities[0] == "page"
}

// errScopedPageReceipt names a page acquisition whose signed receipt
// endpoint, the final URL after followed redirects with its query removed
// (spec dialect §5.2, "Which URL is held"), is outside the reviewed scope or
// constrained form, or differs from its exact endpoint. It carries no URL or
// page text: it becomes a
// validation finding the model reads.
var errScopedPageReceipt = errors.New("acquisition-invalid: The page's final address after redirects is outside the reviewed page profile. Its receipt was kept for audit; the page was left out of the research and of the answer.")

// errScopedPageRefusedAgain answers a request with the same arguments as one
// whose acquisition this run refused at its receipt check: it is not acquired
// again.
var errScopedPageRefusedAgain = errors.New("acquisition-invalid: This page was already acquired and its final address after redirects is outside the reviewed page profile. It was not requested again.")

// admitPortablePageReceipt checks a retained page acquisition before it
// can reach the model or an answer: the signed receipt's acquisition.endpoint
// matches the exact endpoint or holds §5.1's form and matches its scope. It
// reads the receipt as the Gateway wrote it and verifies no signature; Runner's
// check 3 remains the verifier of every retained receipt. A profile without a
// scope uses literal equality against its exact endpoint.
func admitPortablePageReceipt(profile portableProfileDetails, retained json.RawMessage) error {
	if validatePortablePageForm(profile) != nil || rejectDuplicateMembers(retained) != nil {
		return errScopedPageReceipt
	}
	var response map[string]json.RawMessage
	var receipt, acquisition map[string]json.RawMessage
	var endpoint string
	if json.Unmarshal(retained, &response) != nil || rejectDuplicateMembers(response["receipt"]) != nil || json.Unmarshal(response["receipt"], &receipt) != nil || json.Unmarshal(receipt["acquisition"], &acquisition) != nil || json.Unmarshal(acquisition["endpoint"], &endpoint) != nil {
		return errScopedPageReceipt
	}
	if profile.EndpointScope == nil && endpoint != profile.Endpoint || profile.EndpointScope != nil && !pageEndpointInScope(endpoint, profile.EndpointScope) {
		return errScopedPageReceipt
	}
	return nil
}
