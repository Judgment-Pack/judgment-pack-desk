package desk

import (
	"encoding/json"
	"reflect"
	"slices"
)

// Catalog owner declarations describe contracts, never execution permission.
// Keep historical catalogs absent of metadata rather than inventing owners.
type connectionSourceOwner struct {
	Taxonomy             json.RawMessage `json:"taxonomy,omitempty"`
	Profile              json.RawMessage `json:"profile,omitempty"`
	CapabilityTaxonomies json.RawMessage `json:"capabilityTaxonomies,omitempty"`
	DispatchTaxonomies   json.RawMessage `json:"dispatchTaxonomies,omitempty"`
}
type connectionProviderOwner struct {
	Taxonomy              json.RawMessage `json:"taxonomy,omitempty"`
	Profile               json.RawMessage `json:"profile,omitempty"`
	OperationTaxonomies   json.RawMessage `json:"operationTaxonomies,omitempty"`
	OperationDeclarations json.RawMessage `json:"operationDeclarations,omitempty"`
}
type catalogTaxonomy struct {
	Operation    string `json:"operation"`
	Effect       string `json:"effect"`
	CheckKind    string `json:"checkKind,omitempty"`
	Experimental bool   `json:"experimental,omitempty"`
}

var catalogEffects = []string{"pure", "read", "read-external", "propose", "act"}
var catalogOperations = []string{"discover", "read", "answer", "check", "transform", "evaluate", "propose", "act", "run", "own-state", "ask"}

func catalogObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	members, duplicate, err := topLevelMembers(raw)
	if err != nil || duplicate != "" || string(raw) == "null" {
		return nil, false
	}
	out := map[string]json.RawMessage{}
	for _, m := range members {
		out[m.name] = m.raw
	}
	return out, true
}
func readCatalogTaxonomy(raw json.RawMessage) (catalogTaxonomy, bool) {
	var t catalogTaxonomy
	fields, ok := catalogObject(raw)
	if !ok {
		return t, false
	}
	keys := []string{"operation", "effect"}
	if v, present := fields["checkKind"]; present {
		keys = append(keys, "checkKind")
		if string(v) == "null" {
			return t, false
		}
	}
	if v, present := fields["experimental"]; present {
		keys = append(keys, "experimental")
		if string(v) != "true" && string(v) != "false" {
			return t, false
		}
	}
	if _, ok := catalogMembers(raw, keys...); !ok || decodeDataJSON(raw, &t) != nil {
		return t, false
	}
	allowed := false
	switch t.Operation {
	case "discover", "answer", "check":
		allowed = slices.Contains(catalogEffects[:3], t.Effect)
	case "read":
		allowed = t.Effect == "read" || t.Effect == "read-external"
	case "transform", "ask":
		allowed = t.Effect == "pure"
	case "propose":
		allowed = t.Effect == "propose"
	case "act":
		allowed = t.Effect == "act"
	case "run":
		allowed = slices.Contains(catalogEffects[1:], t.Effect)
	case "own-state":
		allowed = t.Effect == "pure" || t.Effect == "read"
	}
	if t.Operation == "check" {
		allowed = allowed && slices.Contains([]string{"validate", "check", "verify", "test", "analyze", "advise"}, t.CheckKind)
	} else {
		_, has := fields["checkKind"]
		allowed = allowed && !has
	}
	return t, allowed
}
func readCatalogOwners(raw json.RawMessage) (map[string]catalogTaxonomy, bool) {
	fields, ok := catalogObject(raw)
	if !ok || len(fields) > 32 {
		return nil, false
	}
	out := map[string]catalogTaxonomy{}
	for name, value := range fields {
		if len(name) == 0 || len(name) > 96 {
			return nil, false
		}
		t, ok := readCatalogTaxonomy(value)
		if !ok {
			return nil, false
		}
		out[name] = t
	}
	return out, true
}
func validCatalogSummary(taxonomy, profile json.RawMessage, owners map[string]catalogTaxonomy) bool {
	if len(owners) == 0 {
		return false
	}
	t, ok := readCatalogTaxonomy(taxonomy)
	if !ok {
		return false
	}
	if _, ok := catalogMembers(profile, "effect", "operations"); !ok {
		return false
	}
	var p struct {
		Effect     string   `json:"effect"`
		Operations []string `json:"operations"`
	}
	if decodeDataJSON(profile, &p) != nil || p.Operations == nil {
		return false
	}
	summary := catalogTaxonomy{Effect: "pure"}
	strength := 0
	operations := map[string]bool{}
	same := true
	first := true
	for _, owner := range owners {
		operations[owner.Operation] = true
		rank := slices.Index(catalogEffects, owner.Effect)
		if rank > strength {
			strength = rank
			summary.Effect = owner.Effect
		}
		summary.Experimental = summary.Experimental || owner.Experimental
		if first {
			summary.Operation = owner.Operation
			summary.CheckKind = owner.CheckKind
			first = false
		} else if summary.Operation != owner.Operation || summary.CheckKind != owner.CheckKind {
			same = false
		}
	}
	if !same {
		summary.Operation = "run"
		summary.CheckKind = ""
	}
	ops := []string{}
	for _, op := range catalogOperations {
		if operations[op] {
			ops = append(ops, op)
		}
	}
	return t == summary && p.Effect == summary.Effect && slices.Equal(p.Operations, ops)
}
func catalogExternal(operation string) catalogTaxonomy {
	return catalogTaxonomy{Operation: operation, Effect: "read-external"}
}

// Exact distribution-owned selectors from the pinned Gateway v5 contract.
func catalogSourceOwners(id string) map[string]catalogTaxonomy {
	switch id {
	case "web":
		return map[string]catalogTaxonomy{"page": catalogExternal("read")}
	case "web-discovery":
		return map[string]catalogTaxonomy{"discover": catalogExternal("discover")}
	case "web-search":
		return map[string]catalogTaxonomy{"answer": catalogExternal("answer"), "search": catalogExternal("discover")}
	case "drive", "gmail", "notion", "obsidian", "aws-s3":
		return map[string]catalogTaxonomy{"read": catalogExternal("read")}
	case "databricks":
		return map[string]catalogTaxonomy{"statement": {Operation: "act", Effect: "act"}, "catalog": catalogExternal("discover"), "mcp": {Operation: "run", Effect: "act"}}
	case "azure-devops":
		return map[string]catalogTaxonomy{"workitems.query": catalogExternal("discover"), "workitems.query-and-get": catalogExternal("discover"), "workitems.get": catalogExternal("read"), "repos.list": catalogExternal("discover"), "repos.items": catalogExternal("read"), "repos.commits": catalogExternal("discover"), "pullrequests.list": catalogExternal("discover"), "pipelines.list": catalogExternal("discover"), "pipelines.runs": catalogExternal("discover"), "pipelines.run": catalogExternal("read"), "mcp": {Operation: "run", Effect: "act"}}
	}
	return nil
}
func catalogControlOwner(method string) (catalogTaxonomy, bool) {
	switch method {
	case "status", "files-status", "source-bindings":
		return catalogTaxonomy{Operation: "read", Effect: "read"}, true
	case "configure", "disconnect", "select", "files-commit", "source-bind", "source-revoke":
		return catalogTaxonomy{Operation: "act", Effect: "act"}, true
	case "connect", "poll", "cancel":
		return catalogTaxonomy{Operation: "run", Effect: "act"}, true
	case "search", "files-list":
		return catalogExternal("discover"), true
	case "files-read", "source-observe":
		return catalogExternal("read"), true
	case "files-prepare", "files-prepare-google-document":
		return catalogTaxonomy{Operation: "propose", Effect: "propose"}, true
	case "test":
		return catalogTaxonomy{Operation: "check", Effect: "read-external", CheckKind: "test"}, true
	}
	return catalogTaxonomy{}, false
}
func validCatalogSourceOwner(id string, capabilities []string, metadata connectionSourceOwner) bool {
	dispatch, ok := readCatalogOwners(metadata.DispatchTaxonomies)
	expected := catalogSourceOwners(id)
	if !ok || expected == nil || !reflect.DeepEqual(dispatch, expected) || !validCatalogSummary(metadata.Taxonomy, metadata.Profile, dispatch) {
		return false
	}
	caps, ok := readCatalogOwners(metadata.CapabilityTaxonomies)
	if !ok || len(caps) != len(capabilities) {
		return false
	}
	for _, cap := range capabilities {
		op := map[string]string{"answer": "answer", "search": "discover", "page": "read"}[cap]
		if op == "" || caps[cap] != catalogExternal(op) {
			return false
		}
	}
	return true
}
func validConnectionCatalogV5(c connectionCatalog) bool {
	discovery, ok := readCatalogTaxonomy(c.DiscoveryTaxonomy)
	if !ok || discovery != (catalogTaxonomy{Operation: "discover", Effect: "pure"}) {
		return false
	}
	for _, s := range c.Sources {
		if !validCatalogSourceOwner(s.ID, s.Capabilities, s.connectionSourceOwner) {
			return false
		}
	}
	for _, p := range c.Providers {
		if p.Source == nil || !validCatalogSourceOwner(p.Source.ID, p.Source.Capabilities, p.Source.connectionSourceOwner) {
			return false
		}
		owners, ok := readCatalogOwners(p.OperationTaxonomies)
		declarations, valid := catalogObject(p.OperationDeclarations)
		if !ok || !valid || len(owners) != len(p.Operations) || len(declarations) != len(p.Operations) {
			return false
		}
		constituents := map[string]catalogTaxonomy{}
		for _, method := range p.Operations {
			intrinsic, ok := catalogControlOwner(method)
			if !ok {
				return false
			}
			raw := declarations[method]
			if _, ok := catalogMembers(raw, "taxonomy", "profile", "components"); !ok {
				return false
			}
			var d struct {
				Taxonomy   json.RawMessage `json:"taxonomy"`
				Profile    json.RawMessage `json:"profile"`
				Components json.RawMessage `json:"components"`
			}
			if decodeDataJSON(raw, &d) != nil {
				return false
			}
			parts, ok := readCatalogOwners(d.Components)
			if !ok || len(parts) != 2 || parts["operation"] != intrinsic || parts["operator-policy"] != (catalogTaxonomy{Operation: "act", Effect: "act"}) || !validCatalogSummary(d.Taxonomy, d.Profile, parts) {
				return false
			}
			summary, _ := readCatalogTaxonomy(d.Taxonomy)
			if owners[method] != summary {
				return false
			}
			for role, t := range parts {
				constituents[method+"/"+role] = t
			}
		}
		if !validCatalogSummary(p.Taxonomy, p.Profile, constituents) {
			return false
		}
	}
	return true
}
