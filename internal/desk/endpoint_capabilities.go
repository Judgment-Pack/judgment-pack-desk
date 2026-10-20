package desk

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// capabilityFacts are the members of the harness's capability record
// (`@judgment-pack/harness`, `capabilities.ts`) a connection can state about
// the endpoint behind it: what its probe found, and what its owner says
// instead. Every member is optional and absent means "the family's default",
// which is how the harness completes a record. Held identical to
// `CapabilityFacts` in `deskConfig.ts` by the shared fixtures.
//
// Nothing here is a credential, a destination or a permission: a record says
// how the endpoint behaves, and cannot widen what the assistant may reach.
type capabilityFacts struct {
	ContextWindow     *int   `json:"contextWindow,omitempty"`
	OutputReserve     *int   `json:"outputReserve,omitempty"`
	NativeToolCalls   *bool  `json:"nativeToolCalls,omitempty"`
	ParallelToolCalls *bool  `json:"parallelToolCalls,omitempty"`
	Vision            *bool  `json:"vision,omitempty"`
	ReasoningKind     string `json:"reasoningKind,omitempty"`
	ReasoningIgnored  *bool  `json:"reasoningIgnored,omitempty"`
	ReasoningField    string `json:"reasoningField,omitempty"`
	WholeJSONAnswer   *bool  `json:"wholeJsonAnswer,omitempty"`
	ToolSchemas       string `json:"toolSchemas,omitempty"`
	StatePlacement    string `json:"statePlacement,omitempty"`
	StrictAlternation *bool  `json:"strictAlternation,omitempty"`
}

// capabilityProbe is the record a probe wrote, and when.
type capabilityProbe struct {
	At string `json:"at"`
	capabilityFacts
}

// endpointCapabilityRecord is what a connection holds about its endpoint's
// capabilities: the probe's finding and the owner's overrides, member by
// member. The answer the harness is given is the probe's record with each
// override laid over it.
type endpointCapabilityRecord struct {
	Probe    *capabilityProbe `json:"probe,omitempty"`
	Override *capabilityFacts `json:"override,omitempty"`
}

var capabilityMembers = []string{
	"contextWindow", "outputReserve", "nativeToolCalls", "parallelToolCalls", "vision",
	"reasoningKind", "reasoningIgnored", "reasoningField", "wholeJsonAnswer", "toolSchemas",
	"statePlacement", "strictAlternation",
}

// reasoningKindsOn is the reasoning dialects each wire speaks (`WIRE_REASONING`
// in the harness): a record naming another is refused for that wire.
var reasoningKindsOn = map[string][]string{
	"openai-compatible": {"openai-effort", "none"},
	"anthropic":         {"anthropic-thinking", "none"},
	"gemini":            {"gemini-thinking", "none"},
}

// decodeCapabilityFacts reads one record of facts closed: every member named
// is checked and a member it does not have is refused. The bounds are the
// harness's own: a window of 4,096 to 2,000,000 tokens, an output reserve of
// 1,024 to half of it, the two stated together.
func decodeCapabilityFacts(value any, scope, kind string, extra []string) (*capabilityFacts, map[string]any, []deskProblem) {
	record, problems := object(value, scope, append(append([]string(nil), capabilityMembers...), extra...))
	if record == nil {
		return nil, nil, problems
	}
	bad := func(member, reason string) {
		problems = append(problems, deskProblem{Key: scope + "." + member, Reason: reason})
	}
	facts := &capabilityFacts{}
	flag := func(member string, into **bool) {
		given, present := record[member]
		if !present || given == nil {
			return
		}
		set, ok := given.(bool)
		if !ok {
			bad(member, fmt.Sprintf("must be true or false; found %s", describe(given)))
			return
		}
		*into = &set
	}
	number := func(member string, low, high int, into **int) {
		given, present := record[member]
		if !present || given == nil {
			return
		}
		n, ok := given.(float64)
		if !ok || n != float64(int64(n)) || n < float64(low) || n > float64(high) {
			bad(member, fmt.Sprintf("must be an integer from %d to %d; found %s", low, high, describe(given)))
			return
		}
		whole := int(n)
		*into = &whole
	}
	choice := func(member string, allowed []string, into *string) {
		given, present := record[member]
		if !present || given == nil {
			return
		}
		named, ok := given.(string)
		if !ok || !contains(allowed, named) {
			bad(member, fmt.Sprintf("must be one of %s; found %s", quotedList(allowed), describe(given)))
			return
		}
		*into = named
	}
	number("contextWindow", 4096, 2_000_000, &facts.ContextWindow)
	number("outputReserve", 1024, 1_000_000, &facts.OutputReserve)
	if facts.ContextWindow != nil && facts.OutputReserve != nil && *facts.OutputReserve > *facts.ContextWindow/2 {
		bad("outputReserve", "must be at most half the context window")
	}
	flag("nativeToolCalls", &facts.NativeToolCalls)
	flag("parallelToolCalls", &facts.ParallelToolCalls)
	flag("vision", &facts.Vision)
	allowedKinds := reasoningKindsOn[kind]
	if allowedKinds == nil {
		allowedKinds = []string{"none"}
	}
	choice("reasoningKind", allowedKinds, &facts.ReasoningKind)
	flag("reasoningIgnored", &facts.ReasoningIgnored)
	choice("reasoningField", []string{"reasoning", "reasoning_content"}, &facts.ReasoningField)
	flag("wholeJsonAnswer", &facts.WholeJSONAnswer)
	choice("toolSchemas", []string{"json-schema", "gemini-subset"}, &facts.ToolSchemas)
	choice("statePlacement", []string{"trailing-user", "system-tail"}, &facts.StatePlacement)
	flag("strictAlternation", &facts.StrictAlternation)
	return facts, record, problems
}

// decodeEndpointCapabilities reads `assistant.endpoint.capabilities`.
func decodeEndpointCapabilities(value any, kind string) (*endpointCapabilityRecord, []deskProblem) {
	record, problems := object(value, "assistant.endpoint.capabilities", []string{"probe", "override"})
	if record == nil {
		return nil, problems
	}
	held := &endpointCapabilityRecord{}
	if given, present := record["probe"]; present && given != nil {
		facts, raw, found := decodeCapabilityFacts(given, "assistant.endpoint.capabilities.probe", kind, []string{"at"})
		problems = append(problems, found...)
		if facts != nil {
			at, ok := raw["at"].(string)
			if parsed, err := time.Parse(time.RFC3339, at); !ok || err != nil || len(at) > 64 || parsed.IsZero() {
				problems = append(problems, deskProblem{Key: "assistant.endpoint.capabilities.probe.at",
					Reason: fmt.Sprintf("must be the time the probe ran, as an RFC 3339 timestamp; found %s", describe(raw["at"]))})
			} else {
				held.Probe = &capabilityProbe{At: at, capabilityFacts: *facts}
			}
		}
	}
	if given, present := record["override"]; present && given != nil {
		facts, _, found := decodeCapabilityFacts(given, "assistant.endpoint.capabilities.override", kind, nil)
		problems = append(problems, found...)
		held.Override = facts
	}
	if len(problems) > 0 {
		return nil, problems
	}
	if held.Probe == nil && held.Override == nil {
		return nil, nil
	}
	return held, nil
}

// lanHost says whether a host is a private-network address: a literal in
// RFC 1918 (10/8, 172.16/12, 192.168/16), a name of one label (a machine on
// the same network, `gpu-box`), or one under `.local`, `.lan`, `.internal` or
// `.home.arpa`. It is a rule about where a request may go *unencrypted when
// its owner has said so*, and it reads the host as written: nothing resolves it.
func lanHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	if parts := strings.Split(host, "."); len(parts) == 4 {
		octets := make([]int, 4)
		for i, part := range parts {
			n := 0
			if part == "" || len(part) > 3 {
				octets = nil
				break
			}
			for _, r := range part {
				if r < '0' || r > '9' {
					octets = nil
					break
				}
				n = n*10 + int(r-'0')
			}
			if octets == nil || n > 255 {
				octets = nil
				break
			}
			octets[i] = n
		}
		if octets != nil {
			return octets[0] == 10 || octets[0] == 172 && octets[1] >= 16 && octets[1] <= 31 || octets[0] == 192 && octets[1] == 168
		}
	}
	if !strings.Contains(host, ".") {
		return !strings.ContainsAny(host, ":[]")
	}
	for _, suffix := range []string{".local", ".lan", ".internal", ".home.arpa"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// loopbackHost says whether a host is this computer: the two spellings the
// transport rule admits, `localhost` and `127.0.0.1`.
func loopbackHost(host string) bool {
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1"
}

// keyless says whether this is an endpoint a key is not required for: one on
// this computer, by the address the configuration names. Nothing resolves the
// name; `localhost` and `127.0.0.1` are the two spellings the transport rule
// admits for it, and `lan` never makes another address one.
func (e assistantEndpoint) keyless() bool {
	parsed, err := url.Parse(e.url)
	return err == nil && loopbackHost(parsed.Hostname())
}
