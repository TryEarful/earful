package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// GoogleOIDC wraps the OpenID Connect authorization-code flow (PLAN.md
// M2-T2: OIDC only, openid+email scopes, nothing more). The issuer URL is
// configuration so tests can stand up a fake issuer (internal/oidctest);
// production always uses https://accounts.google.com.
type GoogleOIDC struct {
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
}

// ErrEmailUnverified rejects Google accounts whose email Google has not
// verified — we must not attach such an email to a user row.
var ErrEmailUnverified = errors.New("auth: google email not verified")

func NewGoogleOIDC(ctx context.Context, issuer, clientID, clientSecret, redirectURL string) (*GoogleOIDC, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("auth: oidc discovery for %s: %w", issuer, err)
	}
	return &GoogleOIDC{
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
		oauth: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  redirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "email"},
		},
	}, nil
}

// AuthCodeURL builds the provider redirect carrying our anti-CSRF state
// and the nonce that must round-trip into the ID token.
func (g *GoogleOIDC) AuthCodeURL(state, nonce string) string {
	return g.oauth.AuthCodeURL(state, oidc.Nonce(nonce))
}

// GoogleIdentity is the verified subject+email pair the callback yields.
type GoogleIdentity struct {
	Sub   string
	Email string
}

// Exchange redeems the authorization code and fully verifies the ID token
// (signature, issuer, audience, expiry, nonce, email_verified).
func (g *GoogleOIDC) Exchange(ctx context.Context, code, nonce string) (GoogleIdentity, error) {
	tok, err := g.oauth.Exchange(ctx, code)
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("auth: code exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return GoogleIdentity{}, errors.New("auth: token response missing id_token")
	}
	idt, err := g.verifier.Verify(ctx, rawID)
	if err != nil {
		return GoogleIdentity{}, fmt.Errorf("auth: verify id token: %w", err)
	}
	if idt.Nonce != nonce {
		return GoogleIdentity{}, errors.New("auth: id token nonce mismatch")
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idt.Claims(&claims); err != nil {
		return GoogleIdentity{}, fmt.Errorf("auth: parse claims: %w", err)
	}
	if claims.Email == "" {
		return GoogleIdentity{}, errors.New("auth: id token missing email claim")
	}
	if !claims.EmailVerified {
		return GoogleIdentity{}, ErrEmailUnverified
	}
	return GoogleIdentity{Sub: idt.Subject, Email: claims.Email}, nil
}
