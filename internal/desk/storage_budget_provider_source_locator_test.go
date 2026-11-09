package desk

import (
	"os"
	"strings"
	"testing"
)

func TestStorageInjectedProviderSourceLocatorLexical(t *testing.T) {
	limit := "/" + strings.Repeat("a", 4095)
	for _, tc := range []struct {
		name, raw, want string
		valid           bool
	}{
		{"canonical", "/provider/data", "/provider/data", true},
		{"constructor-suffix", "/provider/data/.", "/provider/data", true},
		{"root", "/", "/", true},
		{"root-constructor-suffix", "/.", "/", true},
		{"duplicate-root-constructor-suffix", "//.", "", false},
		{"repeated-root-separators-constructor-suffix", "///.", "", false},
		{"duplicate-root-separators", "//", "", false},
		{"repeated-suffix", "/provider/./.", "", false},
		{"interior-dot", "/provider/./data/.", "", false},
		{"interior-dotdot", "/provider/../data/.", "", false},
		{"duplicate-separator", "/provider//data/.", "", false},
		{"duplicate-before-suffix", "/provider//.", "", false},
		{"trailing-separator", "/provider/", "", false},
		// The existing absolute validator permits a literal Unix backslash.
		{"literal-backslash", `/provider\data/.`, `/provider\data`, true},
		{"newline", "/provider\ndata/.", "", false},
		{"carriage-return", "/provider\rdata/.", "", false},
		{"nul", "/provider\x00data/.", "", false},
		{"empty", "", "", false},
		{"relative", "provider/data/.", "", false},
		{"relative-dot", ".", "", false},
		{"canonical-limit", limit, limit, true},
		{"raw-limit-with-suffix", limit + "/.", limit, true},
		{"raw-over-limit-with-suffix", limit + "a/.", "", false},
		{"result-over-limit-without-suffix", limit + "a", "", false},
		{"raw-limit-without-suffix", limit + "aa", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, valid := storageInjectedProviderSourceLocator(tc.raw)
			if got != tc.want || valid != tc.valid {
				t.Fatalf("locator length=%d valid=%t; want length=%d valid=%t", len(got), valid, len(tc.want), tc.valid)
			}
		})
	}
}

func TestStorageInjectedProviderSourceLocatorOriginalConstructor(t *testing.T) {
	p, registry, original, path := injectedArtifactProviderFixture(t)
	if !storageOwnerAbsolute([]byte(path)) || original.Name() != path {
		t.Fatal("fixture did not start with the original canonical provider name")
	}
	// This is the actual constructor-owned description, not a serialized or
	// synthesized name. The previous source join rejected it before addRoot.
	raw := p.root.Name()
	if raw != path+"/." || storageOwnerAbsolute([]byte(raw)) {
		t.Fatal("constructor no longer reproduces the rejected source locator")
	}
	owned := p.root
	beforeOriginal, err := original.Stat(".")
	assistantRunMust(t, err)
	beforeOwned, err := owned.Stat(".")
	assistantRunMust(t, err)
	if !os.SameFile(beforeOriginal, beforeOwned) {
		t.Fatal("constructor did not retain the original provider object")
	}
	registration := registry.byID[p.id]
	assistantRunMust(t, registration.current())
	assistantRunMust(t, p.check(t.Context()))
	locator, valid := storageInjectedProviderSourceLocator(raw)
	if !valid || locator != path {
		t.Fatal("source comparison did not recover the original canonical name")
	}
	afterOriginal, err := original.Stat(".")
	assistantRunMust(t, err)
	afterOwned, err := p.root.Stat(".")
	assistantRunMust(t, err)
	if p.root != owned || registration.root != owned || registry.byID[p.id] != registration ||
		!os.SameFile(beforeOriginal, afterOriginal) || !os.SameFile(beforeOwned, afterOwned) {
		t.Fatal("source comparison changed original descriptor or registry custody")
	}
	assistantRunMust(t, registration.current())
	assistantRunMust(t, p.check(t.Context()))
}
