package desk

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

const assistantLinkTool = "read_link"

type assistantLinkRequest struct {
	URL string `json:"url"`
}
type assistantLinkArguments struct {
	Link   assistantNormalizedLink
	Offset *int
	Find   string
}
type assistantLinkConfiguration struct {
	Gateway                   researchGateway
	Snapshot                  assistantSearchGateway
	DeskDigest                string
	SearchConfigurationDigest string
	SearchPreferenceDigest    string
	AuthorityPacket           *attachmentReceipt
}

func readAssistantLinkArguments(raw []byte) (assistantLinkArguments, error) {
	var out assistantLinkArguments
	bad := func() (assistantLinkArguments, error) {
		return out, failure.ToolInput("Supply a URL, an optional nonnegative integer UTF-16 offset, and optional find words of at most 200 UTF-16 units.")
	}
	fields, err := assistantTestControls(raw, "url", "offset", "find")
	if len(raw) > 32<<10 || err != nil || !validUTF8(raw) || !uniqueJSON(raw) {
		return bad()
	}
	if _, err = assistantChatValue(raw); err != nil {
		return bad()
	}
	var url, find string
	if fields["url"] == nil || json.Unmarshal(fields["url"], &url) != nil {
		return bad()
	}
	if v := fields["offset"]; v != nil {
		var n int
		if json.Unmarshal(v, &n) != nil || string(v) == "null" || n < 0 || uint64(n) > 9007199254740991 {
			return bad()
		}
		out.Offset = &n
	}
	if v := fields["find"]; v != nil {
		if json.Unmarshal(v, &find) != nil || string(v) == "null" || runcontext.U16Length(find) > 200 {
			return bad()
		}
	}
	out.Find = assistantJSTrim(find)
	out.Link, err = normalizeAssistantLink(url)
	if err != nil {
		return out, failure.ToolInput("read_link cannot read this link: " + err.Error())
	}
	return out, nil
}
func (s *Server) assistantLinkConfiguration(start bool) (assistantLinkConfiguration, error) {
	var out assistantLinkConfiguration
	g, err := s.configuredResearchCurrent(start)
	if err != nil {
		return out, err
	}
	if g.maxFileBytes <= 0 || g.managedLocal && !g.documentProcessing {
		return out, errors.New("Link reading needs current document processing and a web source.")
	}
	present, raw, err := s.readDeskFile()
	if err != nil {
		return out, err
	}
	if !g.managedLocal {
		d := decodeDeskFile(raw)
		if !present || d.refused() || d.Research == nil || d.Research.web == nil {
			return out, errors.New("No current web source is configured.")
		}
	}
	out.Gateway, out.Snapshot, out.DeskDigest = g, assistantSearchGatewaySnapshot(g), digestOf(raw)
	return out, nil
}
func (e *assistantRunExecution) recheckAssistantLink(ctx context.Context, held assistantLinkConfiguration) error {
	if err := e.recheckAssistantInput(ctx); err != nil {
		return err
	}
	now, err := e.service.server.assistantLinkConfiguration(false)
	now.SearchConfigurationDigest, now.SearchPreferenceDigest = held.SearchConfigurationDigest, held.SearchPreferenceDigest
	now.AuthorityPacket = held.AuthorityPacket
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(now, held) {
		return errAssistantRunScope
	}
	if held.SearchPreferenceDigest != "" {
		_, _, pref, err := e.service.server.assistantSearchPreference(e.source)
		if err != nil || pref != held.SearchPreferenceDigest {
			return errAssistantRunScope
		}
	}
	if held.SearchConfigurationDigest != "" {
		current, err := e.service.server.assistantSearchConfigurationCurrent(ctx, e.source, false)
		if err != nil || assistantSearchConfigurationDigest(current) != held.SearchConfigurationDigest {
			return errAssistantRunScope
		}
	}
	if held.AuthorityPacket != nil {
		raw, err := e.assistantLinkPacket(ctx, held.AuthorityPacket.ID)
		if err != nil || digestOf(raw) != held.AuthorityPacket.SHA256 {
			return errAssistantRunScope
		}
	}
	return ctx.Err()
}
func (e *assistantRunExecution) recheckAssistantLinkLocked(ctx context.Context, held assistantLinkConfiguration) error {
	if err := e.authorizeAssistantResearchLocked(ctx); err != nil {
		return err
	}
	now, err := e.service.server.assistantLinkConfiguration(false)
	now.SearchConfigurationDigest, now.SearchPreferenceDigest = held.SearchConfigurationDigest, held.SearchPreferenceDigest
	now.AuthorityPacket = held.AuthorityPacket
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(now, held) {
		return errAssistantRunScope
	}
	if held.SearchPreferenceDigest != "" {
		_, _, pref, err := e.service.server.assistantSearchPreference(e.source)
		if err != nil || pref != held.SearchPreferenceDigest {
			return errAssistantRunScope
		}
	}
	if held.AuthorityPacket != nil {
		return e.recheckAssistantLinkAuthorityLocked(held.AuthorityPacket)
	}
	return nil
}
func (e *assistantRunExecution) recheckAssistantLinkAuthorityLocked(expected *attachmentReceipt) error {
	if expected == nil {
		return nil
	}
	if !attachmentID.MatchString(expected.ID) || !assistantRunDigest(expected.SHA256) {
		return errAssistantRunScope
	}
	s := e.service.server
	data, err := s.openChatData()
	if err != nil {
		return err
	}
	defer data.root.Close()
	if err = e.writer.lease.store.checkScope(data.root); err != nil {
		return err
	}
	name, err := resolveConversationName(data.root, s.conversationIdentity())
	if err != nil {
		return err
	}
	raw, err := readPrivateData(data.root, attachmentProjectPrefix(name)+expected.ID+".json", maxAttachmentObjectBytes)
	if err != nil || digestOf(raw) != expected.SHA256 {
		return errAssistantRunScope
	}
	return nil
}
func (e *assistantRunExecution) assistantLinkTools() (*assistantHostRegistry, error) {
	schema := json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","maxLength":4096},"offset":{"type":"integer","minimum":0,"maximum":9007199254740991},"find":{"type":"string","maxLength":200}},"required":["url"],"additionalProperties":false}`)
	tool := runcontext.HostTool{Name: assistantLinkTool, Effect: "read", Delegation: "read", InputSchema: schema, Description: "Read a public HTTPS page actually supplied by the person or authorized by a currently verified search. Bare website domains assume HTTPS. Read one bounded window; offset uses UTF-16 units. Cite exact quotes using the given attachment page citations. Section anchors are reported as text matches, never fetched sections. Three new page reads per saved user turn, shared across delegates and retries. Discovery-only URLs are not available in this private executor. Never repeat uncertain effects."}
	tool.Execute = func(ctx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
		args, err := readAssistantLinkArguments(raw)
		if err != nil {
			return assistantHostFailure(err), nil
		}
		return e.invokeObservedHost(ctx, assistantLinkTool, raw, func(ctx context.Context, _ bool) (assistantHostObservation, error) {
			return e.executeAssistantLink(ctx, args)
		})
	}
	return newAssistantHostRegistry([]runcontext.HostTool{tool})
}
func (e *assistantRunExecution) assistantLinkHost(ctx context.Context, args assistantLinkArguments) (string, error) {
	run, err := e.writer.lease.store.Read(ctx, e.record.Binding, e.record.ID)
	if err != nil {
		return "", err
	}
	callback, _ := ctx.Value(assistantHostCallbackKey{}).(string)
	for _, op := range run.Operations {
		var in assistantHostIntent
		if op.Kind == "host" && op.Name == assistantLinkTool && op.State == "dispatched" && readAssistantHostReceipt(op.Intent, &in) == nil && in.CallID == runcontext.CallIDOf(ctx) && in.CallbackID == callback {
			a, err := readAssistantLinkArguments(in.Arguments)
			if err == nil && reflect.DeepEqual(a, args) {
				return op.ID, nil
			}
		}
	}
	return "", errAssistantRunScope
}
