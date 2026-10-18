package desk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type portableResearchRequest struct {
	Question     string          `json:"question"`
	Constraints  json.RawMessage `json:"constraints"`
	Model        string          `json:"model"`
	Evidence     []string        `json:"evidence"`
	Fields       json.RawMessage `json:"fields"`
	Capabilities []string        `json:"capabilities"`
}
type portableProfileDetails struct {
	ID            string               `json:"id"`
	PublicKey     string               `json:"publicKey"`
	Class         string               `json:"class"`
	Source        string               `json:"source"`
	Authority     string               `json:"authority"`
	Shape         string               `json:"shape"`
	Endpoint      string               `json:"endpoint"`
	EndpointScope EndpointScope        `json:"endpointScope,omitempty"`
	Adapter       searchBindingAdapter `json:"adapter"`
	Tools         []string             `json:"tools"`
	Research      struct {
		Capabilities []string `json:"capabilities"`
		Model        string   `json:"model"`
	} `json:"research"`
}

// Review resolves through Runner, not a second Desk template interpreter. It
// performs no acquisition and does not register or replace installation profiles.
func (s *Server) reviewPortableResearch(ctx context.Context, input json.RawMessage, choice backgroundModelChoice, limits inputResearchLimits, chat, scope string) (inputResearchRun, error) {
	var run inputResearchRun
	if len(input) > 1<<20 || !json.Valid(input) {
		return run, errors.New("The portable research input is invalid or too large.")
	}
	if err := s.validateBackgroundModelConnection(ctx, choice); err != nil {
		return run, err
	}
	raw, err := s.inputRunner(ctx, "inputs/next", input, "")
	if err != nil {
		return run, err
	}
	var plan struct {
		Next *portableResearchPlan `json:"next"`
	}
	if json.Unmarshal(raw, &plan) != nil || plan.Next == nil || plan.Next.Kind != "research" {
		return run, errors.New("Prepare the preceding sources before reviewing this research source.")
	}
	var request portableResearchRequest
	if decodeDataJSON(plan.Next.Arguments, &request) != nil || request.Model != choice.Model || !contains(request.Capabilities, "answer") {
		return run, errors.New("Choose the exact model declared by the portable Dialect.")
	}
	if err := validatePortableSessionExtension(input); err != nil {
		return run, err
	}
	profilesRaw, err := s.inputRunner(ctx, "input-profiles", nil, "")
	if err != nil {
		return run, err
	}
	profiles, err := readInputProfileListing(profilesRaw)
	if err != nil {
		return run, err
	}
	assignments, err := portableResearchAssignments(input, plan.Next.Name)
	if err != nil {
		return run, err
	}
	// Every requested capability's pin is resolved against the installed
	// profiles before anything else is checked: a pin that names a profile
	// Runner set aside is refused by its name with Runner's reason.
	resolved := map[string]*listedInputProfile{}
	for _, capability := range request.Capabilities {
		assignment, ok := assignments[capability]
		if !ok {
			return run, errors.New("Bind every requested research capability before running it.")
		}
		if resolved[capability], err = profiles.installedProfile(assignment.Profile, digestIs(assignment.Digest)); err != nil {
			return run, err
		}
	}
	gateway, err := s.configuredResearch()
	if err != nil {
		return run, err
	}
	selected := []portableResearchProfile{}
	bindings := map[string]searchBindingDescriptor{}
	seen := map[string]bool{}
	for _, capability := range request.Capabilities {
		var selectedProfile *portableResearchProfile
		var details portableProfileDetails
		if entry := resolved[capability]; entry != nil && json.Unmarshal(entry.Profile, &details) == nil {
			selectedProfile = &portableResearchProfile{Profile: entry.Profile, Digest: entry.Digest}
		}
		if selectedProfile == nil || details.Class != "generated" || details.Authority != gateway.authority || details.PublicKey != gateway.signerPublic || !contains(details.Research.Capabilities, capability) {
			return run, errors.New("The reviewed source profile does not match this Desk Gateway and capability.")
		}
		if capability == "answer" && (details.Source != modelSourceName || details.ID != plan.Next.Profile || details.Endpoint != modelSourceEndpoint(choice) || details.Research.Model != choice.Model || details.Shape != "mcp" || !contains(details.Tools, modelSourceTool)) {
			return run, errors.New("Bind this source to the selected connection's model profile before research.")
		}
		if capability == "search" && details.Source == "web-search" && managedSearchBindingID(details.ID) {
			if !gateway.managedLocal {
				return run, errSearchBindingUnavailable
			}
			descriptor, err := s.reviewedSearchBinding(ctx, *selectedProfile, inputResearchPin{Authority: gateway.authority, Public: gateway.signerPublic})
			if err != nil {
				return run, err
			}
			bindings[details.ID] = descriptor
		}
		if capability == "page" {
			if err := s.validatePortablePageProfile(ctx, details, inputResearchPin{Authority: gateway.authority, Public: gateway.signerPublic}); err != nil {
				return run, err
			}
		}
		if !seen[details.ID] {
			selected = append(selected, *selectedProfile)
			seen[details.ID] = true
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return run, err
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	frozen := portableResearchFrozen{Input: append(json.RawMessage(nil), input...), Plan: *plan.Next, Choice: choice, Limits: limits, Profiles: selected, SearchBindings: bindings, ChatID: chat, SessionScope: scope}
	run = inputResearchRun{Version: 2, ID: "research_" + hex.EncodeToString(nonce[:]), Revision: portableResearchRevision(frozen), State: "review", CreatedAt: at, UpdatedAt: at, Gateway: inputResearchPin{Authority: gateway.authority, Public: gateway.signerPublic}, Portable: &portableResearchJournal{Frozen: frozen}, Attempts: []inputResearchAttempt{}}
	return run, validateResearchJournal(run, run.ID)
}

type portableProfileAssignment struct {
	Profile     string                               `json:"profile"`
	Digest      string                               `json:"profileDigest"`
	WebProfiles map[string]portableProfileAssignment `json:"webProfiles,omitempty"`
}

func portableResearchAssignments(input json.RawMessage, name string) (map[string]portableProfileAssignment, error) {
	// Runner already validated this input. Decode only the binding coordinates;
	// never rewrite its definition, case values, dependencies or request templates.
	var body struct {
		Source struct {
			Mapping struct {
				Sources []struct {
					Name string `json:"name"`
					portableProfileAssignment
				} `json:"sources"`
			} `json:"mapping"`
			Import *struct {
				Binding struct {
					Sources map[string]portableProfileAssignment `json:"sources"`
				} `json:"binding"`
			} `json:"import"`
		} `json:"source"`
	}
	if json.Unmarshal(input, &body) != nil {
		return nil, errors.New("The reviewed input is invalid.")
	}
	var assignment portableProfileAssignment
	if body.Source.Import != nil {
		assignment = body.Source.Import.Binding.Sources[name]
	} else {
		for _, source := range body.Source.Mapping.Sources {
			if source.Name == name {
				assignment = source.portableProfileAssignment
				break
			}
		}
	}
	if assignment.Profile == "" || assignment.Digest == "" {
		return nil, errors.New("The research source has no installation binding.")
	}
	result := map[string]portableProfileAssignment{"answer": {Profile: assignment.Profile, Digest: assignment.Digest}}
	for capability, pin := range assignment.WebProfiles {
		result[capability] = pin
	}
	return result, nil
}

func portableResearchInputWithResponse(run inputResearchRun, response json.RawMessage) (json.RawMessage, error) {
	var input map[string]json.RawMessage
	if json.Unmarshal(run.Portable.Frozen.Input, &input) != nil {
		return nil, errors.New("Invalid frozen research input.")
	}
	var source map[string]json.RawMessage
	if json.Unmarshal(input["source"], &source) != nil {
		return nil, errors.New("Invalid frozen research source.")
	}
	var sources map[string]json.RawMessage
	if json.Unmarshal(source["sources"], &sources) != nil || sources == nil {
		sources = map[string]json.RawMessage{}
	}
	name := run.Portable.Frozen.Plan.Name
	if _, present := sources[name]; present {
		return nil, errors.New("The frozen source already contains a response.")
	}
	entry, err := json.Marshal(map[string]any{"response": response})
	if err != nil {
		return nil, err
	}
	sources[name] = entry
	source["sources"], _ = json.Marshal(sources)
	input["source"], _ = json.Marshal(source)
	return json.Marshal(input)
}

// This producer opens one singleton Gateway session per operation. A frozen
// dependency with a nonzero receipt index belongs to a shared-session chain;
// appending a new session would inevitably fail Spec §5.2. Refuse before compute.
func validatePortableSessionExtension(input json.RawMessage) error {
	var body struct {
		Source struct {
			Mapping struct {
				Sources []struct {
					Name string `json:"name"`
					Kind string `json:"kind"`
				} `json:"sources"`
			} `json:"mapping"`
			Import *struct {
				Dialect struct {
					Sources []struct {
						Name string `json:"name"`
						Kind string `json:"kind"`
					} `json:"sources"`
				} `json:"dialect"`
			} `json:"import"`
			Sources map[string]struct {
				Response json.RawMessage `json:"response"`
				Snapshot struct {
					Proof struct {
						Response string `json:"response"`
					} `json:"proof"`
				} `json:"snapshot"`
			} `json:"sources"`
		} `json:"source"`
	}
	if json.Unmarshal(input, &body) != nil {
		return errors.New("Invalid frozen input.")
	}
	declared := body.Source.Mapping.Sources
	if body.Source.Import != nil {
		declared = body.Source.Import.Dialect.Sources
	}
	seen := map[string]bool{}
	check := func(raw json.RawMessage) error {
		if len(raw) == 0 {
			return nil
		}
		var receipt struct {
			Session string `json:"sessionId"`
			Index   int    `json:"callIndex"`
		}
		if json.Unmarshal(raw, &receipt) != nil || receipt.Session == "" {
			return errors.New("The retained receipt is invalid.")
		}
		if receipt.Index != 0 || seen[receipt.Session] {
			return errors.New("These retained dependencies use a shared Gateway session. Fresh portable research requires independent singleton sessions; prepare the dependencies again before reviewing research.")
		}
		seen[receipt.Session] = true
		return nil
	}
	for _, source := range declared {
		value, present := body.Source.Sources[source.Name]
		if !present {
			continue
		}
		raw := value.Response
		if value.Snapshot.Proof.Response != "" {
			raw = json.RawMessage(value.Snapshot.Proof.Response)
		}
		if len(raw) == 0 {
			continue
		}
		var response struct {
			Receipt json.RawMessage `json:"receipt"`
			Result  struct {
				Responses []struct {
					Receipt json.RawMessage `json:"receipt"`
				} `json:"responses"`
			} `json:"result"`
		}
		// Results from ordinary operations may have any shape; extract receipt alone.
		var envelope map[string]json.RawMessage
		if json.Unmarshal(raw, &envelope) != nil {
			return errors.New("Invalid retained acquisition.")
		}
		if err := check(envelope["receipt"]); err != nil {
			return err
		}
		if source.Kind == "research" {
			if json.Unmarshal(raw, &response) != nil {
				return errors.New("Invalid retained research record.")
			}
			for _, retained := range response.Result.Responses {
				if err := check(retained.Receipt); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
