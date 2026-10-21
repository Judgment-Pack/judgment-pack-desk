package desk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

func readAssistantWebsiteArguments(raw []byte) (assistantLinkArguments, error) {
	if _, err := assistantTestControls(raw, "url"); err != nil {
		return assistantLinkArguments{}, failure.ToolInput("Supply only the website URL.")
	}
	return readAssistantLinkArguments(raw)
}

// The catalog is queried only outside storage transactions. The locked phase
// verifies the pinned bundle whose immutable catalog supplied that capability;
// it never executes a companion or waits for a network source under the lock.
func (s *Server) assistantWebsiteCapability(ctx context.Context, c *assistantLinkConfiguration, locked bool) error {
	if !c.Gateway.managedLocal {
		_, raw, err := s.readDeskFile()
		if err != nil {
			return err
		}
		d := decodeDeskFile(raw)
		if d.refused() || d.Research == nil || d.Research.web == nil || d.Research.web.discovery != assistantWebsiteSource {
			return errAssistantRunScope
		}
		return nil
	}
	if s.localGateway == nil || !c.Gateway.documentProcessing {
		return errAssistantRunScope
	}
	bundle := s.localGateway.bundle
	if locked {
		if err := verifyGatewayBundle(bundle); err != nil {
			return err
		}
		raw, err := os.ReadFile(filepath.Join(bundle, "gateway-bundle.json"))
		if err != nil || digestOf(raw) != c.CatalogDigest {
			return errAssistantRunScope
		}
		return nil
	}
	raw, err := readConnectionCatalog(ctx, bundle)
	if err != nil {
		return err
	}
	var catalog connectionCatalog
	if json.Unmarshal(raw, &catalog) != nil {
		return errAssistantRunScope
	}
	web, discovery := false, false
	for _, v := range catalog.Sources {
		web = web || v.ID == "web" && v.Input == "url" && v.MaxBytes == 4<<20 && contains(v.MediaTypes, "text/html") && contains(v.MediaTypes, "text/plain") && contains(v.MediaTypes, "application/pdf")
		discovery = discovery || v.ID == assistantWebsiteSource && v.Input == "url" && v.MaxBytes == assistantWebsiteLimit && contains(v.MediaTypes, "application/vnd.jpack.web-discovery+json")
	}
	if !web || !discovery {
		return errAssistantRunScope
	}
	manifest, err := os.ReadFile(filepath.Join(bundle, "gateway-bundle.json"))
	if err != nil {
		return err
	}
	c.CatalogDigest = digestOf(manifest)
	return nil
}
func (e *assistantRunExecution) assistantWebsiteChoice(c *assistantLinkConfiguration) error {
	_, mode, pref, err := e.service.server.assistantSearchPreference(e.source)
	if err != nil {
		return err
	}
	choice, err := assistantSearchChoice(e.source, mode)
	if err != nil || choice != "research" {
		return failure.ToolInput("Website exploration needs the person's saved research choice.")
	}
	c.SearchPreferenceDigest = pref
	return nil
}
func (e *assistantRunExecution) assistantWebsiteTools() (*assistantHostRegistry, error) {
	schema := json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","maxLength":4096}},"required":["url"],"additionalProperties":false}`)
	tool := runcontext.HostTool{Name: assistantWebsiteTool, Effect: "read", Delegation: "research", InputSchema: schema, Description: "Discover a supplied website or currently verified search seed. One exploration per saved user turn, shared across delegates and retries. At most ten discovered pages, depth two, one hundred listed links, eight MiB and forty-five seconds. This is a navigation map, never page evidence; read exact discovered pages with read_link. Never replay uncertain effects."}
	tool.Execute = func(ctx context.Context, raw json.RawMessage) (runcontext.ToolResult, error) {
		args, err := readAssistantWebsiteArguments(raw)
		if err != nil {
			return assistantHostFailure(err), nil
		}
		return e.invokeObservedHost(ctx, assistantWebsiteTool, raw, func(ctx context.Context, _ bool) (assistantHostObservation, error) {
			c, err := e.service.server.assistantLinkConfiguration(true)
			if err != nil {
				return assistantHostObservation{Result: assistantHostFailure(err)}, nil
			}
			c.Discovery = true
			if err = e.assistantWebsiteChoice(&c); err == nil {
				err = e.service.server.assistantWebsiteCapability(ctx, &c, false)
			}
			if err == nil {
				err = e.recheckAssistantLink(ctx, c)
			}
			allowed := false
			if err == nil {
				allowed, err = e.assistantLinkAllowed(ctx, args.Link.FetchURL, &c)
			}
			if err != nil || !allowed {
				if err == nil {
					err = failure.ToolInput("Website exploration needs a supplied URL or a currently verified search result.")
				}
				return assistantHostObservation{Result: assistantHostFailure(err)}, nil
			}
			return e.acquireAssistantMaterial(ctx, args, c, true)
		})
	}
	return newAssistantHostRegistry([]runcontext.HostTool{tool})
}
