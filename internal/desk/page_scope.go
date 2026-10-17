package desk

// Spec dialect §5.1 and §12: compare constrained page endpoints as submitted.
// Do not use net/url here: decoding, cleaning a path or normalizing a port
// would change the reviewed scope. Runner separately verifies final redirects.
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
	if !(last >= 'a' && last <= 'z' || last >= 'A' && last <= 'Z') || strings.ContainsAny(path, "?#\\") || !pagePercentForm(path) {
		return "", "", false
	}
	for _, segment := range strings.Split(path, "/") {
		if pageDotSegment.MatchString(segment) {
			return "", "", false
		}
	}
	return strings.ToLower(host), path, true
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

// The pre-acquisition scope check applies only to scoped profiles. Legacy
// profiles retain their existing request contract: the submitted URL may have
// a query or redirect to the exact endpoint Runner checks in the receipt.
// Neither that endpoint comparison nor the legacy profile's form is changed.
func admitPortablePage(profile portableProfileDetails, raw json.RawMessage) error {
	if profile.EndpointScope == nil {
		return nil
	}
	refuse := func() error {
		return errors.New("acquisition-invalid: The requested page is invalid or outside the reviewed page profile.")
	}
	var arguments map[string]json.RawMessage
	var requested string
	if rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &arguments) != nil || json.Unmarshal(arguments["url"], &requested) != nil || requested == "" {
		return refuse()
	}
	if err := validatePageScope(profile.EndpointScope); err != nil {
		return fmt.Errorf("acquisition-invalid: %w", err)
	}
	if profile.Class != "generated" || profile.Shape != "http" || profile.Adapter.Name != "adapter-web" || profile.Endpoint != "" || len(profile.Research.Capabilities) != 1 || profile.Research.Capabilities[0] != "page" || !requestedPageInScope(requested, profile.EndpointScope) {
		return refuse()
	}
	return nil
}
