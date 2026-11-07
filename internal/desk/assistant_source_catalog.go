package desk

// Desk adapters own these complete singleton declarations. They are neither a
// Gateway inventory nor MCP tools/list snapshots. Catalog construction cannot
// execute a declaration, create an allowance or activate a source.
import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

const assistantSourceAdapterVersion = "1"

type assistantSourceDeclaration struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	InputSchema json.RawMessage       `json:"inputSchema"`
	Taxonomy    contract.ToolTaxonomy `json:"taxonomy"`
	EffectClass string                `json:"effectClass"`
	Delegation  string                `json:"delegation"`
	Approval    string                `json:"approval"`
}
type assistantSourceConfiguration struct {
	Target             string `json:"target"`
	TargetDigest       string `json:"targetDigest"`
	AuthorityDigest    string `json:"authorityDigest"`
	PublicKeyDigest    string `json:"publicKeyDigest"`
	DeskDigest         string `json:"deskDigest"`
	PreferenceDigest   string `json:"preferenceDigest"`
	Connection         string `json:"connection"`
	Revision           string `json:"revision"`
	Provider           string `json:"provider"`
	BundleDigest       string `json:"bundleDigest"`
	VerifierDigest     string `json:"verifierDigest"`
	GatewayBuildDigest string `json:"gatewayBuildDigest"`
	OwnerDigest        string `json:"ownerDigest"`
	CatalogDigest      string `json:"catalogDigest"`
	CatalogOwnerDigest string `json:"catalogOwnerDigest"`
}
type assistantSourceSnapshot struct {
	AdapterID            string                       `json:"adapterId"`
	AdapterVersion       string                       `json:"adapterVersion"`
	Kind                 string                       `json:"kind"`
	Trust                string                       `json:"trust"`
	Identity             *contract.ToolOfferIdentity  `json:"identity,omitempty"`
	IdentityDigest       string                       `json:"identityDigest,omitempty"`
	ConfigurationDigest  string                       `json:"configurationDigest"`
	ToolListDigest       string                       `json:"toolListDigest"`
	CompleteDeclarations []assistantSourceDeclaration `json:"completeDeclarations"`
	Configuration        assistantSourceConfiguration `json:"configuration"`
	Status               string                       `json:"status"`
}

type assistantSourceCatalogOwner struct {
	id          string
	declaration runcontext.HostTool
	taxonomy    *runcontext.ToolTaxonomy
}

func assistantSourceOwners() []assistantSourceCatalogOwner {
	return []assistantSourceCatalogOwner{
		{"desk-search", assistantSearchDeclaration(), &runcontext.ToolTaxonomy{Operation: "discover", Effect: "read-external"}},
		{"desk-link", assistantLinkDeclaration(), &runcontext.ToolTaxonomy{Operation: "read", Effect: "read-external"}},
		{"desk-website", assistantWebsiteDeclaration(), &runcontext.ToolTaxonomy{Operation: "discover", Effect: "read-external"}},
		{"desk-research-workflow", assistantResearchDeclaration(), &runcontext.ToolTaxonomy{Operation: "act", Effect: "act"}},
	}
}
func assistantSourceBaseCatalog() []assistantSourceSnapshot {
	out := make([]assistantSourceSnapshot, 0, 4)
	for _, owner := range assistantSourceOwners() {
		t := owner.declaration
		t.Effect, t.SourceTool, t.Execute = "", "", nil
		t.Taxonomy, t.EffectClass = owner.taxonomy, runcontext.LegacyClass(owner.taxonomy)
		approval := "none"
		if t.EffectClass == "act" {
			approval = "required"
		}
		taxonomy := contract.ToolTaxonomy{Operation: owner.taxonomy.Operation, Effect: owner.taxonomy.Effect}
		declaration := assistantSourceDeclaration{Name: t.Name, Description: t.Description, InputSchema: append(json.RawMessage{}, t.InputSchema...), Taxonomy: taxonomy, EffectClass: t.EffectClass, Delegation: t.Delegation, Approval: approval}
		digest := runcontext.ToolListDigest([]runcontext.ListedTool{{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, Taxonomy: owner.taxonomy}})
		out = append(out, assistantSourceSnapshot{AdapterID: owner.id, AdapterVersion: assistantSourceAdapterVersion, Kind: "custom", Trust: "first-party", ToolListDigest: digest, CompleteDeclarations: []assistantSourceDeclaration{declaration}, Status: "setup-required"})
	}
	return out
}

// Only start=false owner observations are permitted. Search status uses an
// already-running companion, outside the private-data lock. Missing observation
// remains setup-required; a configured URL never manufactures a search owner.
func (s *Server) assistantSourceCatalog(ctx context.Context, source assistantRunSource, project string) ([]assistantSourceSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := assistantSourceBaseCatalog()
	_, raw, err := s.readDeskFile()
	if err != nil {
		return nil, err
	}
	deskDigest := digestOf(raw)
	gateway, gatewayErr := s.configuredResearchCurrent(false)
	var search assistantSearchConfiguration
	var searchErr error
	if gatewayErr == nil {
		search, searchErr = s.assistantSearchConfigurationCurrent(ctx, source, false)
	} else {
		searchErr = gatewayErr
	}
	var bundleDigest, verifierDigest, buildDigest string
	if gatewayErr == nil && gateway.managedLocal && s.localGateway != nil {
		buildDigest, gatewayErr = s.assistantSourceGatewayBuildDigest()
		bundle := s.localGateway.bundle
		if gatewayErr != nil || verifyGatewayBundle(bundle) != nil {
			s.localGateway.invalidateCatalog()
			gatewayErr = errAssistantRunScope
		} else {
			manifest, readErr := assistantSourceManifest(bundle)
			_, verifier, verifyErr := verifiedGatewayProgram(bundle, "gateway")
			if readErr != nil || verifyErr != nil {
				s.localGateway.invalidateCatalog()
				gatewayErr = errAssistantRunScope
			} else {
				bundleDigest, verifierDigest = digestOf(manifest), verifier
			}
		}
	}
	for i := range out {
		row := &out[i]
		config := assistantSourceConfiguration{DeskDigest: deskDigest, OwnerDigest: digestOf(assistantChatJSON([]string{"desk-source-owner/v1", project, digestOf([]byte(s.projectDir)), row.AdapterID}))}
		available := false
		if row.AdapterID == "desk-research-workflow" {
			// This owner is Desk's authenticated allowance/queue workflow, not a
			// guessed remote source and not any temporary research allowance.
			config.Target = "Desk private research workflow"
			available = s.assistant.usable()
		} else if gatewayErr == nil {
			parsed, parseErr := url.Parse(gateway.url)
			if parseErr == nil {
				config.Target = parsed.Scheme + "://" + parsed.Host
			}
			config.TargetDigest, config.AuthorityDigest, config.PublicKeyDigest = digestOf([]byte(gateway.url)), digestOf([]byte(gateway.authority)), digestOf([]byte(gateway.signerPublic))
			config.BundleDigest, config.VerifierDigest, config.GatewayBuildDigest = bundleDigest, verifierDigest, buildDigest
			switch row.AdapterID {
			case "desk-search":
				if searchErr == nil {
					config.Connection, config.Revision, config.Provider, config.PreferenceDigest = search.Connection, search.Revision, search.Provider, search.PreferenceDigest
					available = true
				}
			case "desk-link":
				_, linkErr := s.assistantLinkConfiguration(false)
				available = linkErr == nil
			case "desk-website":
				if gateway.managedLocal {
					observation, err := s.localGateway.currentCatalog(s.assistant, bundleDigest, buildDigest)
					if err == nil && assistantSourceWebsiteCatalogAvailable(observation.raw) && gateway.documentProcessing && gateway.maxFileBytes > 0 {
						config.CatalogDigest, config.CatalogOwnerDigest = observation.digest, observation.owner
						available = true
					}
				} else {
					decoded := decodeDeskFile(raw)
					available = !decoded.refused() && decoded.Research != nil && decoded.Research.web != nil && decoded.Research.web.discovery == assistantWebsiteSource && gateway.maxFileBytes > 0
				}
			}
			// Include all execution-relevant gateway bounds without exposing its URL
			// path, key or authority in review text.
			config.OwnerDigest = digestOf(assistantChatJSON(struct {
				Owner   string
				Gateway assistantSearchGateway
			}{config.OwnerDigest, assistantSearchGatewaySnapshot(gateway)}))
		}
		row.Configuration = config
		row.ConfigurationDigest = assistantSourceConfigurationDigest(*row, project)
		if available {
			label := "Desk research HTTP "
			if row.AdapterID == "desk-research-workflow" {
				label = "Desk research workflow "
			}
			row.Identity = &contract.ToolOfferIdentity{Kind: "connection", Label: label + row.ConfigurationDigest}
			row.IdentityDigest = runcontext.IdentityDigest(*row.Identity)
			row.Status = "review-required"
		}
	}
	return out, ctx.Err()
}

// A transaction may inspect local configuration and existing process facts, but
// never call a companion, execute a verifier, acquire/seal or hold a network lock.
func (s *Server) assistantSourceCatalogLocalCurrent(source assistantRunSource, held []assistantSourceSnapshot) error {
	_, raw, err := s.readDeskFile()
	if err != nil {
		return err
	}
	for _, row := range held {
		if row.Configuration.DeskDigest != digestOf(raw) {
			return errAssistantRunConflict
		}
		if row.AdapterID == "desk-research-workflow" {
			continue
		}
		if row.Status == "setup-required" {
			continue
		}
		gateway, err := s.configuredResearchCurrent(false)
		if err != nil || row.Configuration.TargetDigest != digestOf([]byte(gateway.url)) || row.Configuration.AuthorityDigest != digestOf([]byte(gateway.authority)) || row.Configuration.PublicKeyDigest != digestOf([]byte(gateway.signerPublic)) {
			return errAssistantRunConflict
		}
		expectedOwner := digestOf(assistantChatJSON([]string{"desk-source-owner/v1", source.Binding.Project, digestOf([]byte(s.projectDir)), row.AdapterID}))
		expectedOwner = digestOf(assistantChatJSON(struct {
			Owner   string
			Gateway assistantSearchGateway
		}{expectedOwner, assistantSearchGatewaySnapshot(gateway)}))
		if expectedOwner != row.Configuration.OwnerDigest {
			return errAssistantRunConflict
		}
		if row.AdapterID == "desk-search" {
			id, _, preference, err := s.assistantSearchPreference(source)
			if err != nil || id != row.Configuration.Connection || preference != row.Configuration.PreferenceDigest {
				return errAssistantRunConflict
			}
		}
		if gateway.managedLocal {
			buildDigest, buildErr := s.assistantSourceGatewayBuildDigest()
			if buildErr != nil || buildDigest != row.Configuration.GatewayBuildDigest {
				return errAssistantRunConflict
			}
			if s.localGateway == nil || verifyGatewayBundle(s.localGateway.bundle) != nil {
				s.localGateway.invalidateCatalog()
				return errAssistantRunConflict
			}
			manifest, err := assistantSourceManifest(s.localGateway.bundle)
			_, verifier, verifyErr := verifiedGatewayProgram(s.localGateway.bundle, "gateway")
			if err != nil || verifyErr != nil || row.Configuration.BundleDigest != digestOf(manifest) || verifier != row.Configuration.VerifierDigest {
				return errAssistantRunConflict
			}
			if row.AdapterID == "desk-website" {
				observation, err := s.localGateway.currentCatalog(s.assistant, row.Configuration.BundleDigest, buildDigest)
				if err != nil || observation.digest != row.Configuration.CatalogDigest || observation.owner != row.Configuration.CatalogOwnerDigest || !assistantSourceWebsiteCatalogAvailable(observation.raw) {
					return errAssistantRunConflict
				}
			}
		}
	}
	return nil
}
func assistantSourceSnapshotEqual(a, b assistantSourceSnapshot) bool { return reflect.DeepEqual(a, b) }

func assistantSourceManifest(bundle string) ([]byte, error) {
	file, err := os.Open(filepath.Join(bundle, "gateway-bundle.json"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readBounded(file, 8192)
}

func (s *Server) assistantSourceGatewayBuildDigest() (string, error) {
	if s.localGateway == nil {
		return "", errAssistantRunScope
	}
	gateway := s.localGateway
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if gateway.closed || gateway.done == nil || gateway.pin == nil || gateway.pin.build == nil {
		return "", errAssistantRunScope
	}
	select {
	case <-gateway.done:
		return "", errAssistantRunScope
	default:
	}
	return digestOf(assistantChatJSON(*gateway.pin.build)), nil
}

// Apply the actual existing website capability domain to the complete validated
// retained catalog; a manifest alone or provider label supplies no capability.
func assistantSourceWebsiteCatalogAvailable(raw json.RawMessage) bool {
	var catalog connectionCatalog
	if json.Unmarshal(raw, &catalog) != nil {
		return false
	}
	web, discovery := false, false
	for _, source := range catalog.Sources {
		web = web || source.ID == "web" && source.Input == "url" && source.MaxBytes == 4<<20 && contains(source.MediaTypes, "text/html") && contains(source.MediaTypes, "text/plain") && contains(source.MediaTypes, "application/pdf")
		discovery = discovery || source.ID == assistantWebsiteSource && source.Input == "url" && source.MaxBytes == assistantWebsiteLimit && contains(source.MediaTypes, "application/vnd.jpack.web-discovery+json")
	}
	return web && discovery
}
