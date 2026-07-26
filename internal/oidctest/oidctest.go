// Package oidctest is a minimal in-process OpenID Connect issuer for
// application-edge tests of the Google login flow. The world ends at the
// identity provider (SPEC.md Testing Decisions), so tests swap
// accounts.google.com for this issuer via config — everything server-side
// of the seam (go-oidc discovery, JWKS fetch, code exchange, full ID-token
// verification) runs for real against RS256-signed tokens.
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

const keyID = "oidctest-key"

type identity struct {
	Sub           string
	Email         string
	EmailVerified bool
}

// Issuer is a fake OIDC provider. Its /authorize endpoint auto-approves
// as the identity set with SetIdentity, so a redirect-following client
// completes the whole login in one request chain; MintCode exists for
// tests that drive the callback manually (state tampering etc.).
type Issuer struct {
	Server *httptest.Server

	key *rsa.PrivateKey

	mu    sync.Mutex
	ident identity
	codes map[string]codeGrant
}

type codeGrant struct {
	identity identity
	nonce    string
	clientID string
}

// New starts a fake issuer; it is closed via t.Cleanup.
func New(t *testing.T) *Issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("oidctest: generate key: %v", err)
	}
	iss := &Issuer{
		key:   key,
		ident: identity{Sub: "default-sub", Email: "default@example.test", EmailVerified: true},
		codes: map[string]codeGrant{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", iss.discovery)
	mux.HandleFunc("GET /keys", iss.jwks)
	mux.HandleFunc("GET /authorize", iss.authorize)
	mux.HandleFunc("POST /token", iss.token)
	iss.Server = httptest.NewServer(mux)
	t.Cleanup(iss.Server.Close)
	return iss
}

func (i *Issuer) URL() string { return i.Server.URL }

// SetIdentity chooses who the next authorizations sign in as.
func (i *Issuer) SetIdentity(sub, email string, verified bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.ident = identity{Sub: sub, Email: email, EmailVerified: verified}
}

// MintCode registers an authorization code outside the /authorize flow,
// for tests that assemble the callback request by hand.
func (i *Issuer) MintCode(nonce, clientID string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	code := fmt.Sprintf("code-%d", len(i.codes)+1)
	i.codes[code] = codeGrant{identity: i.ident, nonce: nonce, clientID: clientID}
	return code
}

func (i *Issuer) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                i.URL(),
		"authorization_endpoint":                i.URL() + "/authorize",
		"token_endpoint":                        i.URL() + "/token",
		"jwks_uri":                              i.URL() + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (i *Issuer) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": keyID,
			"n":   base64.RawURLEncoding.EncodeToString(i.key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(i.key.E)).Bytes()),
		}},
	})
}

// authorize plays the consenting user: it immediately redirects back to
// redirect_uri with a fresh code bound to the request's nonce.
func (i *Issuer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Scheme == "" {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	code := i.MintCode(q.Get("nonce"), q.Get("client_id"))
	out := redirect.Query()
	out.Set("code", code)
	out.Set("state", q.Get("state"))
	redirect.RawQuery = out.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (i *Issuer) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	i.mu.Lock()
	grant, ok := i.codes[r.PostFormValue("code")]
	delete(i.codes, r.PostFormValue("code"))
	i.mu.Unlock()
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant"}`)
		return
	}

	now := time.Now()
	idToken := i.signJWT(map[string]any{
		"iss":            i.URL(),
		"aud":            grant.clientID,
		"sub":            grant.identity.Sub,
		"email":          grant.identity.Email,
		"email_verified": grant.identity.EmailVerified,
		"nonce":          grant.nonce,
		"iat":            now.Unix(),
		"exp":            now.Add(time.Hour).Unix(),
	})
	writeJSON(w, map[string]any{
		"access_token": "oidctest-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	})
}

func (i *Issuer) signJWT(claims map[string]any) string {
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": keyID})
	payload, _ := json.Marshal(claims)
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, digest[:])
	if err != nil {
		panic("oidctest: sign: " + err.Error())
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic("oidctest: encode json: " + err.Error())
	}
}
