package desk

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Installation-owned registration settings, never provider tokens. Some public
// desktop registrations require a secret in the token request; it is not proof
// of client identity and is never embedded in Desk or returned to the browser.
type signInProvider struct {
	Label    string `json:"label"`
	Issuer   string `json:"issuer"`
	ClientID string `json:"clientId"`
	Secret   string `json:"clientSecret,omitempty"`
}
type signInOwner struct {
	Subject string `json:"subject"`
	Issuer  string `json:"issuer"`
	Name    string `json:"name"`
	Email   string `json:"email,omitempty"`
}
type signInProtocol struct {
	config   oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
	issuer   string
}

func signInURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" || strings.ContainsAny(raw, "\\\r\n\t") {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	// Local self-hosted issuers are explicit operator choices. DNS names that
	// might resolve to loopback do not qualify for the cleartext exception.
	return u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}
func (p signInProvider) validate() error {
	u, err := url.Parse(p.Issuer)
	if !signInURL(p.Issuer) || err != nil || u.RawQuery != "" || u.ForceQuery || len(p.Issuer) > 2048 ||
		strings.TrimSpace(p.ClientID) != p.ClientID || p.ClientID == "" || len(p.ClientID) > 512 ||
		strings.TrimSpace(p.Label) != p.Label || p.Label == "" || len(p.Label) > 80 || len(p.Secret) > 4096 ||
		strings.ContainsAny(p.ClientID+p.Label+p.Secret, "\r\n\x00") {
		return errors.New("invalid sign-in provider settings")
	}
	return nil
}

// Discovery, token exchange and key retrieval have finite time/body bounds.
// Redirects cannot silently forward a registration credential elsewhere.
type signInTransport struct{ base http.RoundTripper }

func (t signInTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !signInURL(r.URL.String()) {
		return nil, errors.New("invalid identity endpoint")
	}
	res, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	res.Body = &signInBody{ReadCloser: res.Body, left: 1 << 20}
	return res, nil
}

type signInBody struct {
	io.ReadCloser
	left int64
}

func (b *signInBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, errors.New("identity response exceeds limit")
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.ReadCloser.Read(p)
	b.left -= int64(n)
	return n, err
}

func discoverSignIn(ctx context.Context, cfg signInProvider, redirect string) (*signInProtocol, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Timeout: 15 * time.Second, Transport: signInTransport{transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("identity redirect refused") }}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), cfg.Issuer)
	if err != nil {
		return nil, errors.New("identity provider discovery failed")
	}
	endpoint := provider.Endpoint()
	var metadata struct {
		JWKS             string   `json:"jwks_uri"`
		ChallengeMethods []string `json:"code_challenge_methods_supported"`
	}
	if err := provider.Claims(&metadata); err != nil || !signInURL(endpoint.AuthURL) || !signInURL(endpoint.TokenURL) || !signInURL(metadata.JWKS) {
		return nil, errors.New("identity provider endpoints are invalid")
	}
	// The discovery field is optional; S256 is always sent and verified by the
	// token endpoint, including for older issuers that omit this metadata.
	if len(metadata.ChallengeMethods) > 0 {
		supported := false
		for _, method := range metadata.ChallengeMethods {
			supported = supported || method == "S256"
		}
		if !supported {
			return nil, errors.New("identity provider does not support S256 PKCE")
		}
	}
	endpoint.AuthStyle = oauth2.AuthStyleInParams
	return &signInProtocol{
		config:   oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.Secret, Endpoint: endpoint, RedirectURL: redirect, Scopes: []string{oidc.ScopeOpenID, "profile", "email"}},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID, SupportedSigningAlgs: []string{"RS256", "ES256", "PS256", "RS384", "RS512", "ES384", "ES512", "PS384", "PS512"}}),
		client:   client, issuer: cfg.Issuer,
	}, nil
}
func (p *signInProtocol) authorize(state, nonce, verifier string) string {
	return p.config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce), oauth2.SetAuthURLParam("prompt", "select_account"))
}
func (p *signInProtocol) exchange(ctx context.Context, code, nonce, verifier string) (signInOwner, error) {
	var none signInOwner
	ctx = oidc.ClientContext(ctx, p.client)
	token, err := p.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return none, errors.New("identity code exchange failed")
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 65536 {
		return none, errors.New("identity token missing")
	}
	verified, err := p.verifier.Verify(ctx, raw)
	if err != nil || verified.Nonce != nonce || verified.Subject == "" || len(verified.Subject) > 512 || verified.IssuedAt.After(time.Now().Add(time.Minute)) {
		return none, errors.New("identity verification failed")
	}
	var claims struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		AZP   string `json:"azp"`
	}
	if err := verified.Claims(&claims); err != nil || len(claims.Name) > 512 || len(claims.Email) > 512 ||
		(claims.AZP != "" && claims.AZP != p.config.ClientID) || (len(verified.Audience) > 1 && claims.AZP != p.config.ClientID) {
		return none, errors.New("identity claims invalid")
	}
	name := strings.TrimSpace(claims.Name)
	if name == "" {
		name = strings.TrimSpace(claims.Email)
	}
	if name == "" {
		name = verified.Subject
	}
	return signInOwner{Subject: verified.Subject, Issuer: p.issuer, Name: name, Email: claims.Email}, nil
}
