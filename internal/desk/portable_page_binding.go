package desk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type pageBindingDescriptor struct {
	Version  int                  `json:"version"`
	Shape    string               `json:"shape"`
	Adapter  searchBindingAdapter `json:"adapter"`
	Endpoint json.RawMessage      `json:"endpoint"`
	Research struct {
		Capabilities []string `json:"capabilities"`
	} `json:"research"`
	FixedArguments map[string]string `json:"fixedArguments"`
	RequestSchema  json.RawMessage   `json:"requestSchema"`
	ResultRecord   string            `json:"resultRecord"`
}

var errPageBindingUnavailable = errors.New("The page profile could not be checked. Review the installed Gateway.")

// The verified adapter describes its own identity and request contract. This
// operation does not read a page, inspect a search connection or call a model.
func (s *Server) describePageBinding(ctx context.Context) (pageBindingDescriptor, inputResearchPin, error) {
	var descriptor pageBindingDescriptor
	gateway, err := s.configuredResearch()
	if err != nil || !gateway.managedLocal || s.localGateway == nil {
		return descriptor, inputResearchPin{}, errPageBindingUnavailable
	}
	pin := inputResearchPin{Authority: gateway.authority, Public: gateway.signerPublic}
	executable, digest, err := verifiedGatewayProgram(s.localGateway.bundle, "adapter-web")
	if err != nil {
		return descriptor, pin, errPageBindingUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "--describe-research")
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return descriptor, pin, errPageBindingUnavailable
	}
	defer out.Close()
	if cmd.Start() != nil {
		return descriptor, pin, errPageBindingUnavailable
	}
	stop := context.AfterFunc(ctx, func() { _ = out.Close() })
	defer stop()
	raw, err := io.ReadAll(io.LimitReader(out, (16<<10)+1))
	if err != nil || len(raw) > 16<<10 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return descriptor, pin, errPageBindingUnavailable
	}
	if cmd.Wait() != nil || ctx.Err() != nil || decodeDataJSON(raw, &descriptor) != nil {
		return descriptor, pin, errPageBindingUnavailable
	}
	if descriptor.Version != 1 || descriptor.Shape != "http" || descriptor.Adapter.Name != "adapter-web" || descriptor.Adapter.Version == "" || descriptor.Adapter.Digest != "sha256:"+digest || string(descriptor.Endpoint) != "null" || len(descriptor.Research.Capabilities) != 1 || descriptor.Research.Capabilities[0] != "page" || descriptor.FixedArguments == nil || len(descriptor.FixedArguments) != 0 || !json.Valid(descriptor.RequestSchema) || descriptor.ResultRecord != "attachment-v1" {
		return descriptor, pin, errPageBindingUnavailable
	}
	// Recheck the trusted file after execution; a replacement cannot contribute
	// an identity to a profile authorized for the earlier executable.
	program, err := os.Open(executable)
	if err != nil {
		return descriptor, pin, errPageBindingUnavailable
	}
	defer program.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, program); err != nil || hex.EncodeToString(hash.Sum(nil)) != digest {
		return descriptor, pin, errPageBindingUnavailable
	}
	return descriptor, pin, nil
}

func pageBindingProfile(descriptor pageBindingDescriptor, pin inputResearchPin, scope EndpointScope) json.RawMessage {
	identity, _ := json.Marshal([]any{scope, descriptor.Adapter, pin})
	id := "desk-page-" + strings.TrimPrefix(sha256Digest(identity), "sha256:")[:32]
	profile, _ := json.Marshal(map[string]any{"id": id, "publicKey": pin.Public, "class": "generated", "source": "web", "authority": pin.Authority, "shape": "http", "adapter": descriptor.Adapter, "endpoint": nil, "endpointScope": scope, "research": descriptor.Research})
	return profile
}

// Registered profiles are immutable, but an installed adapter can change.
// Check any scoped page assignment, including operator-owned profile IDs,
// before a review/confirmation/worker start can spend research allowance.
func (s *Server) validatePortablePageProfile(ctx context.Context, profile portableProfileDetails, pin inputResearchPin) error {
	if err := validatePortablePageForm(profile); err != nil {
		return err
	}
	if profile.EndpointScope == nil {
		return nil
	}
	descriptor, current, err := s.describePageBinding(ctx)
	if err != nil {
		return err
	}
	if current != pin || profile.Shape != descriptor.Shape || profile.Adapter != descriptor.Adapter {
		return errors.New("The reviewed page adapter changed. Review and bind a current page profile before research.")
	}
	return validatePageScope(profile.EndpointScope)
}

func (s *Server) handlePortablePageBinding(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var request struct {
		EndpointScope  EndpointScope `json:"endpointScope"`
		ReviewedDigest string        `json:"reviewedDigest,omitempty"`
		ChatID         string        `json:"chatId,omitempty"`
	}
	if r.URL.RawQuery != "" || inputRequest(r, &request) != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "Invalid page profile review.")
		return
	}
	if err := validatePageScope(request.EndpointScope); err != nil {
		writeJSONCoded(w, 400, CodeBadRequest, err.Error())
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
	descriptor, pin, err := s.describePageBinding(r.Context())
	if err != nil {
		writeJSONCoded(w, 409, CodeBadRequest, err.Error())
		return
	}
	profile := pageBindingProfile(descriptor, pin, request.EndpointScope)
	reviewed, _ := json.Marshal([]any{descriptor, profile})
	revision := sha256Digest(reviewed)
	result := map[string]any{"descriptor": descriptor, "profile": profile, "revision": revision}
	if request.ReviewedDigest != "" {
		if request.ReviewedDigest != revision {
			writeJSONCoded(w, 409, CodeBadRequest, "The page profile changed. Review it again.")
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
