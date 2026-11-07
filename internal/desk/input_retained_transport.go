package desk

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const retainedSelectorHeaderName = "JPack-Conversion-Contract"
const retainedRunnerPrefix = "/v1/retained-column/1/"
const retainedSelectorHeaderBytes = 4096

func retainedRunnerRequiredRoles(method, path string, generated bool) ([]string, error) {
	// The adapter accepts an ordinary URL path, not an arbitrary URL/query or
	// an already-prefixed route. Existing ID/query validators still apply.
	if !strings.HasPrefix(path, "/v1/") || strings.ContainsAny(path, "?#\\%") {
		return nil, retainedContractFailure("capability-unsupported")
	}
	if path == "/v1/selected-inputs/next" {
		if method != http.MethodPost || !generated {
			return nil, retainedContractFailure("capability-unsupported")
		}
		return retainedPreparationRoles(), nil
	}
	parts := strings.Split(path, "/")
	runRoute := len(parts) == 5 && parts[1] == "v1" && parts[2] == "jobs" && parts[3] != "" && parts[4] == "runs"
	exportRoute := len(parts) == 5 && parts[1] == "v1" && parts[2] == "runs" && parts[3] != "" && parts[4] == "verification"
	if method == http.MethodGet && exportRoute {
		return []string{"preparation-verification"}, nil
	}
	if method != http.MethodPost || !(path == "/v1/inputs/next" || path == "/v1/inputs/preview" || path == "/v1/previews" || runRoute) {
		return nil, retainedContractFailure("capability-unsupported")
	}
	roles := []string{"document-validation", "preview", "preparation"}
	if generated {
		roles = append(roles, "producer-carrier")
	}
	return roles, nil
}
func retainedHeaderValues(header http.Header) []string {
	// HTTP parsing canonicalizes names, but programmatic maps can contain case
	// variants. Count them all instead of allowing an extra selector to hide.
	var values []string
	for name, entries := range header {
		if strings.EqualFold(name, retainedSelectorHeaderName) {
			values = append(values, entries...)
		}
	}
	return values
}
func retainedSelectorHeader(selection retainedConversionSelector) (string, error) {
	if err := retainedValidateSelector(selection); err != nil {
		return "", err
	}
	raw, err := json.Marshal(selection)
	if err != nil {
		return "", retainedContractFailure("input-malformed")
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) > retainedSelectorHeaderBytes {
		return "", retainedContractFailure("input-malformed")
	}
	return encoded, nil
}

// readRetainedRunnerSelection validates the actual route and sole canonical
// header before any larger body read. It does not authenticate capabilities.
func readRetainedRunnerSelection(method, path string, header http.Header, generated bool) (*retainedConversionSelector, error) {
	values := retainedHeaderValues(header)
	if !strings.HasPrefix(path, retainedRunnerPrefix) {
		if len(values) != 0 {
			return nil, retainedContractFailure("capability-unsupported")
		}
		return nil, nil
	}
	if len(values) != 1 || len(values[0]) > retainedSelectorHeaderBytes {
		return nil, retainedContractFailure("input-malformed")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(values[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != values[0] {
		return nil, retainedContractFailure("input-malformed")
	}
	ordinary := "/v1/" + strings.TrimPrefix(path, retainedRunnerPrefix)
	required, err := retainedRunnerRequiredRoles(method, ordinary, generated)
	if err != nil {
		return nil, err
	}
	selection, err := parseRetainedSelector(raw, required...)
	if err != nil {
		return nil, err
	}
	return &selection, nil
}

// retainedRunnerTransport prepares only explicitly selected, supported routes.
// The caller must supply an admission negotiated from the actual independent
// owner reports. Returning a header/path performs no dispatch or durable effect.
func retainedRunnerTransport(method, path string, admission *retainedAdmission, generated bool) (selectedPath, header string, err error) {
	if admission == nil {
		return path, "", nil
	}
	if admission.generated != generated {
		return "", "", retainedContractFailure("capability-unsupported")
	}
	required, err := retainedRunnerRequiredRoles(method, path, generated)
	if err != nil {
		return "", "", err
	}
	if err := retainedValidateSelector(admission.selection, required...); err != nil {
		return "", "", err
	}
	for _, role := range required {
		if admission.limits[role] == nil {
			return "", "", retainedContractFailure("capability-unsupported")
		}
	}
	if err := retainedValidateAdmission(admission); err != nil {
		return "", "", err
	}
	header, err = retainedSelectorHeader(admission.selection)
	if err != nil {
		return "", "", err
	}
	return retainedRunnerPrefix + strings.TrimPrefix(path, "/v1/"), header, nil
}
func retainedValidateAdmission(admission *retainedAdmission) error {
	if admission == nil || len(admission.limits) == 0 {
		return retainedContractFailure("capability-unsupported")
	}
	if err := retainedValidateSelector(admission.selection); err != nil {
		return err
	}
	for role, limits := range admission.limits {
		caps := retainedRoleCaps(role)
		if caps == nil || !contains(admission.selection.Roles, role) {
			return retainedContractFailure("capability-unsupported")
		}
		if len(limits) != len(caps) {
			return retainedContractFailure("input-malformed")
		}
		for key, cap := range caps {
			if limits[key] < 1 || limits[key] > cap {
				return retainedContractFailure("input-malformed")
			}
		}
	}
	return nil
}

// Every traversed owner constrains a shared complete artifact. A Runtime or
// producer lower cap cannot be bypassed by choosing a larger Runner cap.
func retainedArtifactLimit(admission *retainedAdmission, key string) (int64, error) {
	if err := retainedValidateAdmission(admission); err != nil {
		return 0, err
	}
	var limit int64
	for _, limits := range admission.limits {
		if cap, ok := limits[key]; ok && (limit == 0 || cap < limit) {
			limit = cap
		}
	}
	if limit == 0 {
		return 0, retainedContractFailure("capability-unsupported")
	}
	return limit, nil
}
func checkRetainedArtifact(admission *retainedAdmission, key string, raw []byte) error {
	limit, err := retainedArtifactLimit(admission, key)
	if err != nil {
		return err
	}
	if int64(len(raw)) > limit {
		return retainedContractFailure("input-over-bound")
	}
	return nil
}
func readRetainedArtifact(reader io.Reader, admission *retainedAdmission, key string) ([]byte, error) {
	limit, err := retainedArtifactLimit(admission, key)
	if err != nil {
		return nil, err
	}
	// No allocation or limit+1 value is derived directly from an untrusted report:
	// admission has already intersected and rechecked fixed installation ceilings.
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, retainedContractFailure("input-over-bound")
	}
	return raw, nil
}
func retainedRuntimeSelectors(operation string) (map[string]string, error) {
	switch operation {
	case "contract", "canonicalize", "digest", "validate", "check":
		installed := retainedInstalledSelector()
		return map[string]string{"draftContract": installed.Contract + "@" + installed.Spec.Revision, "draftProfile": installed.Profile}, nil
	default:
		return nil, retainedContractFailure("capability-unsupported")
	}
}
