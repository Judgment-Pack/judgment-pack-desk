package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type searchBindingAdapter struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}
type searchBindingDescriptor struct {
	Version  int                  `json:"version"`
	Shape    string               `json:"shape"`
	Adapter  searchBindingAdapter `json:"adapter"`
	Endpoint string               `json:"endpoint"`
	Research struct {
		Capabilities []string `json:"capabilities"`
		Model        string   `json:"model,omitempty"`
	} `json:"research"`
	FixedArguments map[string]string `json:"fixedArguments"`
	RequestSchema  json.RawMessage   `json:"requestSchema"`
	ResultRecord   string            `json:"resultRecord"`
	// Version 2 (Gateway ADR-0019) adds these three. Version 1 has none of them,
	// and a version-1 descriptor marshals without them, byte for byte.
	RequestSchemaVersion string          `json:"requestSchemaVersion,omitempty"`
	ResultSchema         json.RawMessage `json:"resultSchema,omitempty"`
	ResultSchemaVersion  string          `json:"resultSchemaVersion,omitempty"`
}

var (
	searchRequestSchemaVersion = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}-request-[1-9][0-9]{0,5}$`)
	searchResultSchemaVersion  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}-result-[1-9][0-9]{0,5}$`)
)

// The description is closed at the two versions this Desk reads. Version 1 holds
// none of the three version-2 members; version 2 holds all of them, in form.
func (d searchBindingDescriptor) versionMembersValid() bool {
	switch d.Version {
	case 1:
		return d.RequestSchemaVersion == "" && len(d.ResultSchema) == 0 && d.ResultSchemaVersion == ""
	case 2:
		trimmed := bytes.TrimSpace(d.ResultSchema)
		return searchRequestSchemaVersion.MatchString(d.RequestSchemaVersion) && searchResultSchemaVersion.MatchString(d.ResultSchemaVersion) && len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed)
	}
	return false
}

var managedSearchProfileID = regexp.MustCompile(`^desk-search-[a-f0-9]{32}$`)

func managedSearchBindingID(id string) bool { return managedSearchProfileID.MatchString(id) }

var errSearchBindingUnavailable = errors.New("The search binding could not be checked. Review the current connection and installed Gateway.")

func searchBindingID(choice inputResearchSearch, adapter searchBindingAdapter, gateway inputResearchPin) string {
	raw, _ := json.Marshal([]any{choice, adapter, gateway})
	return "desk-search-" + strings.TrimPrefix(sha256Digest(raw), "sha256:")[:32]
}
func searchBindingProfile(d searchBindingDescriptor, gateway inputResearchPin) json.RawMessage {
	choice := inputResearchSearch{Connection: d.FixedArguments["connection"], Revision: d.FixedArguments["revision"]}
	raw, _ := json.Marshal(map[string]any{"id": searchBindingID(choice, d.Adapter, gateway), "publicKey": gateway.Public, "class": "generated", "source": "web-search", "authority": gateway.Authority, "shape": d.Shape, "adapter": d.Adapter, "endpoint": d.Endpoint, "research": d.Research})
	return raw
}

// The executable is the verified installed companion, never a browser-supplied
// program or provider URL. Describing a binding performs no acquisition.
func (s *Server) describeSearchBinding(ctx context.Context, choice inputResearchSearch) (searchBindingDescriptor, inputResearchPin, error) {
	var d searchBindingDescriptor
	if !researchSourceName.MatchString(choice.Connection) || !researchConnectionRevision.MatchString(choice.Revision) {
		return d, inputResearchPin{}, errSearchBindingUnavailable
	}
	gateway, err := s.configuredResearch()
	if err != nil || !gateway.managedLocal || s.localGateway == nil {
		return d, inputResearchPin{}, errSearchBindingUnavailable
	}
	pin := inputResearchPin{Authority: gateway.authority, Public: gateway.signerPublic}
	bundle := s.localGateway.bundle
	executable, approvedDigest, err := verifiedGatewayProgram(bundle, "adapter-sources")
	if err != nil {
		return d, pin, errSearchBindingUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "--state-dir", platformPath(s.assistant.dir, "gateway-connections"), "--principal", "desk-local", "--provider", "web-search", "--describe-research")
	raw, _ := json.Marshal(choice)
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	output, err := cmd.StdoutPipe()
	if err != nil {
		return d, pin, errSearchBindingUnavailable
	}
	if cmd.Start() != nil {
		return d, pin, errSearchBindingUnavailable
	}
	defer output.Close()
	stopRead := context.AfterFunc(ctx, func() { _ = output.Close() })
	defer stopRead()
	raw, err = io.ReadAll(io.LimitReader(output, (16<<10)+1))
	if err != nil || len(raw) > 16<<10 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return d, pin, errSearchBindingUnavailable
	}
	if cmd.Wait() != nil || ctx.Err() != nil || decodeDataJSON(raw, &d) != nil {
		return d, pin, errSearchBindingUnavailable
	}
	if !d.versionMembersValid() || d.Shape != "http" || d.Adapter.Name != "adapter-sources" || d.Adapter.Version == "" || d.Endpoint == "" || d.ResultRecord != "web-search-v1" || !contains(d.Research.Capabilities, "search") || len(d.FixedArguments) != 2 || d.FixedArguments["connection"] != choice.Connection || d.FixedArguments["revision"] != choice.Revision || !json.Valid(d.RequestSchema) {
		return d, pin, errSearchBindingUnavailable
	}
	f, err := os.Open(executable)
	if err != nil {
		return d, pin, errSearchBindingUnavailable
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil || d.Adapter.Digest != "sha256:"+approvedDigest || hex.EncodeToString(h.Sum(nil)) != approvedDigest {
		return d, pin, errSearchBindingUnavailable
	}
	return d, pin, nil
}

// Resolve the local producer's opaque profile ID from current connection metadata.
// The private journal freezes the resulting descriptor; no second profile registry
// or provider-specific endpoint reconstruction is needed.
func (s *Server) reviewedSearchBinding(ctx context.Context, entry portableResearchProfile, gateway inputResearchPin) (searchBindingDescriptor, error) {
	var profile struct {
		ID      string               `json:"id"`
		Adapter searchBindingAdapter `json:"adapter"`
	}
	if json.Unmarshal(entry.Profile, &profile) != nil || !managedSearchBindingID(profile.ID) {
		return searchBindingDescriptor{}, errSearchBindingUnavailable
	}
	companion := s.connectionCompanion("web-search", true)
	if companion == nil {
		return searchBindingDescriptor{}, errSearchBindingUnavailable
	}
	raw, err := companion.call(ctx, s.localGateway.bundle, platformPath(s.assistant.dir, "gateway-connections"), "status", json.RawMessage(`{}`), "web-search", false)
	var status struct {
		Version     int `json:"version"`
		Connections []struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
		} `json:"connections"`
	}
	if err != nil || len(raw) > 64<<10 || json.Unmarshal(raw, &status) != nil || status.Version != 1 || len(status.Connections) > 32 {
		return searchBindingDescriptor{}, errSearchBindingUnavailable
	}
	for _, c := range status.Connections {
		choice := inputResearchSearch{Connection: c.ID, Revision: c.Revision}
		if searchBindingID(choice, profile.Adapter, gateway) != profile.ID {
			continue
		}
		descriptor, pin, err := s.describeSearchBinding(ctx, choice)
		if err != nil || pin != gateway {
			return descriptor, errSearchBindingUnavailable
		}
		verifier, err := s.researchVerifier()
		if err != nil {
			return descriptor, err
		}
		expected, err := researchCanon(ctx, verifier, searchBindingProfile(descriptor, pin))
		if err != nil {
			return descriptor, err
		}
		actual, err := researchCanon(ctx, verifier, entry.Profile)
		if err != nil || !bytes.Equal(expected, actual) {
			return descriptor, errSearchBindingUnavailable
		}
		return descriptor, nil
	}
	return searchBindingDescriptor{}, errSearchBindingUnavailable
}

func (s *Server) handlePortableSearchBinding(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		Choice         inputResearchSearch `json:"choice"`
		ReviewedDigest string              `json:"reviewedDigest,omitempty"`
		ChatID         string              `json:"chatId,omitempty"`
	}
	if r.URL.RawQuery != "" || inputRequest(r, &request) != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid search binding review.")
		return
	}
	if request.ChatID != "" {
		if !researchChatID.MatchString(request.ChatID) {
			writeJSONCoded(w, 400, CodeBadRequest, "Invalid research conversation.")
			return
		}
		if _, _, err := s.approvalChat(request.ChatID); err != nil {
			writeJSONCoded(w, 409, CodeBadRequest, err.Error())
			return
		}
	}
	descriptor, pin, err := s.describeSearchBinding(r.Context(), request.Choice)
	if err != nil {
		writeJSONCoded(w, 409, CodeBadRequest, err.Error())
		return
	}
	profile := searchBindingProfile(descriptor, pin)
	reviewed, _ := json.Marshal([]any{descriptor, profile})
	revision := sha256Digest(reviewed)
	result := map[string]any{"descriptor": descriptor, "profile": profile, "revision": revision}
	if request.ReviewedDigest != "" {
		if request.ReviewedDigest != revision {
			writeJSONCoded(w, 409, CodeBadRequest, "The search binding changed. Review it again.")
			return
		}
		if err := s.portableResearchChatWork(request.ChatID); err != nil {
			writeJSONCoded(w, 409, CodeBadRequest, err.Error())
			return
		}
		entry, err := s.inputRunner(r.Context(), "input-profiles", profile, "")
		if err != nil {
			writeJSONCoded(w, 409, CodeBadRequest, err.Error())
			return
		}
		result["entry"] = entry
	}
	writeJSON(w, 200, result)
}

func portableSearchArguments(run inputResearchRun, profile portableProfileDetails, raw json.RawMessage) (json.RawMessage, error) {
	if profile.Source != "web-search" || !managedSearchBindingID(profile.ID) {
		return raw, nil
	}
	binding, ok := run.Portable.Frozen.SearchBindings[profile.ID]
	if !ok {
		return nil, errSearchBindingUnavailable
	}
	var arguments map[string]json.RawMessage
	if decodeDataJSON(raw, &arguments) != nil || arguments == nil {
		return nil, errors.New("Use object arguments for source search.")
	}
	for key, value := range binding.FixedArguments {
		encoded, _ := json.Marshal(value)
		if supplied, present := arguments[key]; present {
			var actual string
			if json.Unmarshal(supplied, &actual) != nil || actual != value {
				return nil, errors.New("Use the reviewed search connection and revision.")
			}
		}
		arguments[key] = encoded
	}
	return json.Marshal(arguments)
}
