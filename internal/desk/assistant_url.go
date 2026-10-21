package desk

// One standards-backed spelling owner for signed search validation and explicit
// link preparation. Parsing never rewrites signed payloads; Gateway alone owns
// DNS and redirect admission.
import (
	"errors"
	"regexp"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

const assistantJSWhitespace = "\u0009\u000a\u000b\u000c\u000d\u0020\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

func assistantJSTrim(s string) string { return strings.Trim(s, assistantJSWhitespace) }
func assistantPublicURL(raw string) (*whatwg.Url, error) {
	if len(raw) > 4096 || !strings.HasPrefix(raw, "https://") || strings.ContainsAny(raw, "\\#") {
		return nil, errAssistantDocument
	}
	for _, r := range raw {
		if r <= 32 {
			return nil, errAssistantDocument
		}
	}
	authority := strings.FieldsFunc(raw[8:], func(r rune) bool { return r == '/' || r == '?' })
	if len(authority) == 0 || strings.Contains(authority[0], "@") {
		return nil, errAssistantDocument
	}
	u, err := whatwg.Parse(raw)
	if err != nil || u.Protocol() != "https:" || u.Hostname() == "" || u.Username() != "" || u.Password() != "" || u.Port() != "" || u.Hash() != "" {
		return nil, errAssistantDocument
	}
	return u, nil
}

type assistantNormalizedLink struct {
	FetchURL   string `json:"fetchUrl"`
	DisplayURL string `json:"displayUrl"`
	Anchor     string `json:"anchor"`
	AnchorKind string `json:"anchorKind"`
}

const assistantBareDomain = `(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+(?:[a-z]{2,63}|xn--[a-z0-9-]{2,59})`

var assistantBareURL = regexp.MustCompile(`(?i)^` + assistantBareDomain + "(?:[/?#][^\\s<>\"'`" + `]*)?$`)
var assistantFileExtension = regexp.MustCompile(`(?i)\.(?:json|jsonl|md|txt|csv|tsv|pdf|js|jsx|ts|tsx|py|go|rs|yaml|yml|toml|xml|html|css|zip|png|jpg|jpeg|svg|log)$`)

func assistantBareAddress(raw string) bool {
	return assistantBareURL.MatchString(raw) && !assistantFileExtension.MatchString(strings.FieldsFunc(raw, func(r rune) bool { return r == '/' || r == '?' || r == '#' })[0])
}
func normalizeAssistantLink(raw string) (assistantNormalizedLink, error) {
	out := assistantNormalizedLink{DisplayURL: assistantJSTrim(raw), AnchorKind: "none"}
	given := out.DisplayURL
	refuse := func(reason string) (assistantNormalizedLink, error) {
		return assistantNormalizedLink{}, errors.New(reason)
	}
	if given == "" {
		return refuse("a link is needed")
	}
	if len(given) > 4096 {
		return refuse("a link is at most 4096 bytes")
	}
	for _, r := range given {
		if r == '\\' || r < 32 || r == 127 {
			return refuse("the link contains characters a link cannot")
		}
	}
	parse := given
	if assistantBareAddress(given) {
		parse = "https://" + given
	}
	u, err := whatwg.Parse(parse)
	if err != nil {
		return refuse("the link is not a well-formed address")
	}
	if u.Protocol() != "https:" {
		return refuse("only public https:// links can be read")
	}
	authority := regexp.MustCompile(`(?i)^https://`).ReplaceAllString(given, "")
	authority = strings.FieldsFunc(authority, func(r rune) bool { return r == '/' || r == '?' || r == '#' })[0]
	if u.Username() != "" || u.Password() != "" || strings.Contains(authority, "@") {
		return refuse("a link with a sign-in in it cannot be read")
	}
	// Browser uses origin + pathname + search; empty query/hash markers disappear.
	out.FetchURL = "https://" + u.Host() + u.Pathname() + u.Search()
	if _, err = assistantPublicURL(out.FetchURL); err != nil {
		return refuse("the link is not a public HTTPS address the gateway accepts")
	}
	out.Anchor = strings.TrimPrefix(u.Hash(), "#")
	if out.Anchor != "" {
		out.AnchorKind = "section"
		if strings.HasPrefix(out.Anchor, "/") || strings.HasPrefix(out.Anchor, "!") {
			out.AnchorKind = "route"
		}
	}
	return out, nil
}

var assistantLinkPattern = regexp.MustCompile("(?i)https://[^\\s<>\"'`]+|" + assistantBareDomain + "(?:[/?#][^\\s<>\"'`]*)?")

func extractAssistantLinks(text string) []string {
	out := []string{}
	for _, loc := range assistantLinkPattern.FindAllStringIndex(text, -1) {
		raw := text[loc[0]:loc[1]]
		https := strings.HasPrefix(strings.ToLower(raw), "https://")
		if !https && loc[0] > 0 && strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789_@:/\\.%+-", rune(strings.ToLower(text[loc[0]-1 : loc[0]])[0])) {
			continue
		}
		if !https && loc[1] < len(text) {
			tail := strings.ToLower(text[loc[1]:])
			if strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789_:@-", rune(tail[0])) || len(tail) > 1 && tail[0] == '.' && strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789", rune(tail[1])) {
				continue
			}
		}
		for len(raw) > 0 {
			last := raw[len(raw)-1:]
			opener := map[string]string{")": "(", "]": "[", "}": "{"}[last]
			if strings.Contains(".,;:!?*", last) || opener != "" && strings.Count(raw, last) > strings.Count(raw, opener) {
				raw = raw[:len(raw)-1]
				continue
			}
			break
		}
		if https && len(raw) > 8 || !https && assistantBareAddress(raw) {
			if !contains(out, raw) {
				out = append(out, raw)
			}
		}
	}
	return out
}
