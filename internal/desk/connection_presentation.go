package desk

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"
)

type connectionSourceContract struct {
	ID     string `json:"id"`
	Shape  string `json:"shape"`
	Record string `json:"record"`
}
type connectionSetupField struct {
	Key      string            `json:"key"`
	Type     string            `json:"type"`
	Label    map[string]string `json:"label"`
	Required bool              `json:"required"`
}

func validConnectionPresentation(d connectionDescriptor) bool {
	if !catalogIdentifier.MatchString(d.QueryMode) || !catalogIdentifier.MatchString(d.Protocol) || d.Source == nil || !catalogIdentifier.MatchString(d.Source.ID) || !catalogIdentifier.MatchString(d.Source.Shape) || !catalogIdentifier.MatchString(d.Source.Record) {
		return false
	}
	if _, ok := catalogMembers(d.Presentation, "name", "description", "instructions", "icon"); !ok {
		return false
	}
	var p struct {
		Icon         string            `json:"icon"`
		Name         string            `json:"name"`
		Description  map[string]string `json:"description"`
		Instructions map[string]string `json:"instructions"`
	}
	if decodeDataJSON(d.Presentation, &p) != nil || !plainConnectionText(p.Name, 120) || p.Name == "" || !validConnectionIcon(p.Icon) || !localizedConnectionText(p.Description, 512) || !localizedConnectionText(p.Instructions, 4096) {
		return false
	}
	var fields []json.RawMessage
	if json.Unmarshal(d.Setup, &fields) != nil || fields == nil || len(fields) > 12 {
		return false
	}
	seen := map[string]bool{}
	for _, raw := range fields {
		if _, ok := catalogMembers(raw, "key", "type", "label", "required"); !ok {
			return false
		}
		var f connectionSetupField
		if decodeDataJSON(raw, &f) != nil || !catalogIdentifier.MatchString(f.Key) || f.Key == "constructor" || f.Key == "prototype" || seen[f.Key] || !catalogIdentifier.MatchString(f.Type) || !localizedConnectionText(f.Label, 120) {
			return false
		}
		seen[f.Key] = true
	}
	if d.AuthorizationEndpoints == nil || len(d.AuthorizationEndpoints) > 4 {
		return false
	}
	for _, endpoint := range d.AuthorizationEndpoints {
		u, err := url.Parse(endpoint)
		if err != nil || len(endpoint) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(endpoint, "\\\r\n\t ") {
			return false
		}
	}
	return true
}
func plainConnectionText(value string, limit int) bool {
	if !utf8.ValidString(value) || len(value) > limit {
		return false
	}
	for _, r := range value {
		if r < 32 && r != '\n' && r != '\t' || r == 127 {
			return false
		}
	}
	return true
}
func localizedConnectionText(values map[string]string, limit int) bool {
	if values == nil || len(values) > 32 {
		return false
	}
	if _, ok := values["en"]; !ok {
		return false
	}
	for locale, value := range values {
		if len(locale) > 32 || locale == "" || !plainConnectionText(value, limit) {
			return false
		}
	}
	return true
}

func validConnectionIcon(icon string) bool {
	if icon == "" {
		return true
	}
	if !strings.HasPrefix(icon, "data:image/png;base64,") || len(icon) > 22000 {
		return false
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(icon, "data:image/png;base64,"))
	return err == nil && len(raw) >= 24 && string(raw[:8]) == "\x89PNG\r\n\x1a\n" && string(raw[12:16]) == "IHDR" && binary.BigEndian.Uint32(raw[16:20]) > 0 && binary.BigEndian.Uint32(raw[16:20]) <= 128 && binary.BigEndian.Uint32(raw[20:24]) > 0 && binary.BigEndian.Uint32(raw[20:24]) <= 128
}
