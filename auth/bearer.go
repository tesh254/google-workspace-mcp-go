// bearer.go implements bearer-token verification for the streamable-http
// transport using the official go-sdk auth middleware. This wires the server
// into the MCP authorization framework: HTTP clients present an
// Authorization: Bearer <token> header, the token is a JWT verified against
// the configured authorization server's JWKS, and failed attempts get a 401
// with a WWW-Authenticate header so compliant clients can discover the
// issuer (RFC 9728 protected resource metadata).
package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	gosdk "github.com/modelcontextprotocol/go-sdk/auth"
)

// BearerConfig configures the bearer-token middleware.
type BearerConfig struct {
	// JWKSURL is the JSON Web Key Set endpoint of the authorization server,
	// e.g. "https://auth.example.com/.well-known/jwks.json". Required.
	JWKSURL string
	// Issuer is the expected iss claim of validated tokens. Optional; when
	// set, tokens from other issuers are rejected.
	Issuer string
	// Audience is the expected audience of validated tokens (typically the
	// MCP resource URI). Optional; when set, tokens not issued for it are
	// rejected.
	Audience string
	// ResourceMetadataURL is advertised in WWW-Authenticate on failures so
	// clients can discover the protected resource metadata document.
	ResourceMetadataURL string
	// Scopes, if set, are advertised in WWW-Authenticate on failures.
	Scopes []string
	// HTTPClient, if non-nil, is used to fetch JWKS documents.
	HTTPClient *http.Client
}

// BearerMiddleware returns an http middleware that requires a valid bearer
// token on every request. Verified-token claims land in the request context
// as *gosdk.TokenInfo (see gosdk.TokenInfoFromContext). Returns an error
// when the configuration is incomplete.
func BearerMiddleware(cfg *BearerConfig) (func(http.Handler) http.Handler, error) {
	if cfg == nil || cfg.JWKSURL == "" {
		return nil, errors.New("bearer auth requires a JWKS URL")
	}
	verifier := &jwksTokenVerifier{
		jwksURL:  cfg.JWKSURL,
		issuer:   cfg.Issuer,
		audience: cfg.Audience,
		client:   cfg.HTTPClient,
	}
	return gosdk.RequireBearerToken(verifier.verify, &gosdk.RequireBearerTokenOptions{
		ResourceMetadataURL: cfg.ResourceMetadataURL,
		Scopes:              cfg.Scopes,
	}), nil
}

// jwksTokenVerifier validates JWT bearer tokens against an authorization
// server's JWKS document. The document is fetched on first use and refreshed
// when a token carries an unknown key ID, so key rotation is followed.
type jwksTokenVerifier struct {
	jwksURL  string
	issuer   string
	audience string
	client   *http.Client

	mu    sync.Mutex
	keys  []jwksKey
	noKid bool // no JWK carries a kid; tokens may omit it
	fetch bool // JWKS document fetched at least once
}

// jwksKey is a verification key parsed from a JWK entry.
type jwksKey struct {
	kid string
	rsa *rsa.PublicKey
	ec  *ecdsa.PublicKey
	alg string
}

func (v *jwksTokenVerifier) keysSnapshot() ([]jwksKey, bool, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.keys, v.noKid, v.fetch
}

// bearerClaims extends the standard registered claims with the OAuth scope
// claim, which the SDK middleware compares against the configured scopes.
type bearerClaims struct {
	jwt.RegisteredClaims
	Scope string `json:"scope,omitempty"`
}

// verify implements the go-sdk TokenVerifier contract: a non-nil error
// unwrapping to gosdk.ErrInvalidToken causes a 401 response.
func (v *jwksTokenVerifier) verify(_ context.Context, token string, _ *http.Request) (*gosdk.TokenInfo, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("%w: empty bearer token", gosdk.ErrInvalidToken)
	}
	tok, err := v.parse(token)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", gosdk.ErrInvalidToken, err)
	}
	claims := tok.Claims.(*bearerClaims)
	if claims.ExpiresAt == nil {
		return nil, fmt.Errorf("%w: token has no exp claim", gosdk.ErrInvalidToken)
	}
	info := &gosdk.TokenInfo{Expiration: claims.ExpiresAt.Time}
	if claims.Subject != "" {
		info.UserID = claims.Subject
	}
	if claims.Scope != "" {
		info.Scopes = strings.Fields(claims.Scope)
	}
	return info, nil
}

// parse validates the JWT signature and claims.
func (v *jwksTokenVerifier) parse(raw string) (*jwt.Token, error) {
	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}),
	}
	if v.issuer != "" {
		parserOpts = append(parserOpts, jwt.WithIssuer(v.issuer))
	}
	if v.audience != "" {
		parserOpts = append(parserOpts, jwt.WithAudience(v.audience))
	}
	claims := &bearerClaims{}
	return jwt.NewParser(parserOpts...).ParseWithClaims(raw, claims, func(tk *jwt.Token) (any, error) {
		return v.keyFor(tk)
	})
}

// keyFor resolves the verification key for a token header against the cached
// JWKS; unknown key IDs trigger a single JWKS refresh (key rotation) first.
func (v *jwksTokenVerifier) keyFor(tk *jwt.Token) (any, error) {
	kid, kidKnown := tk.Header["kid"].(string)

	keys, noKid, fetched := v.keysSnapshot()
	if !fetched {
		if err := v.refreshKeys(); err != nil {
			return nil, fmt.Errorf("loading JWKS: %w", err)
		}
		keys, noKid, _ = v.keysSnapshot()
	}

	if !kidKnown && noKid && len(keys) == 1 {
		return matchKey(keys[0], tk)
	}
	for _, k := range keys {
		if k.kid == kid {
			return matchKey(k, tk)
		}
	}

	if err := v.refreshKeys(); err != nil {
		return nil, fmt.Errorf("refreshing JWKS: %w", err)
	}
	keys, noKid, _ = v.keysSnapshot()
	if !kidKnown && noKid && len(keys) == 1 {
		return matchKey(keys[0], tk)
	}
	for _, k := range keys {
		if k.kid == kid {
			return matchKey(k, tk)
		}
	}
	return nil, fmt.Errorf("no JWKS key matches kid %q", kid)
}

func matchKey(k jwksKey, tk *jwt.Token) (any, error) {
	if k.alg != "" {
		if alg, ok := tk.Header["alg"].(string); ok && alg != "" && alg != k.alg {
			return nil, fmt.Errorf("token alg %q does not match JWK alg %q", alg, k.alg)
		}
	}
	switch {
	case k.rsa != nil:
		return k.rsa, nil
	case k.ec != nil:
		return k.ec, nil
	default:
		return nil, errors.New("JWK carries no usable key material")
	}
}

// refreshKeys fetches and parses the JWKS document, replacing the cache.
func (v *jwksTokenVerifier) refreshKeys() error {
	client := v.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Get(v.jwksURL)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", v.jwksURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching %s: status %d", v.jwksURL, resp.StatusCode)
	}
	limit := io.LimitReader(resp.Body, 1<<20)
	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(limit).Decode(&doc); err != nil {
		return fmt.Errorf("parsing JWKS: %w", err)
	}

	var keys []jwksKey
	noKid := true
	for _, k := range doc.Keys {
		if k.Use == "enc" {
			continue
		}
		if k.Kid != "" {
			noKid = false
		}
		out := jwksKey{kid: k.Kid, alg: k.Alg}
		switch k.Kty {
		case "RSA":
			pub, err := rsaFromJWK(k.N, k.E)
			if err != nil {
				continue
			}
			out.rsa = pub
		case "EC":
			pub, err := ecdsaFromJWK(k.Crv, k.X, k.Y)
			if err != nil {
				continue
			}
			out.ec = pub
		default:
			continue
		}
		keys = append(keys, out)
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	v.keys = keys
	v.noKid = noKid
	v.fetch = true
	return nil
}

// rsaFromJWK decodes an RSA public key's modulus and exponent.
func rsaFromJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	if nB64 == "" || eB64 == "" {
		return nil, errors.New("missing n/e")
	}
	n, err := bigIntFromBase64URL(nB64)
	if err != nil {
		return nil, fmt.Errorf("decoding n: %w", err)
	}
	e, err := bigIntFromBase64URL(eB64)
	if err != nil {
		return nil, fmt.Errorf("decoding e: %w", err)
	}
	if !e.IsInt64() || e.Int64() <= 0 || e.Int64() > 1<<31 {
		return nil, errors.New("unreasonable exponent")
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

// ecdsaFromJWK decodes an EC public key's curve and coordinates.
func ecdsaFromJWK(crv, xB64, yB64 string) (*ecdsa.PublicKey, error) {
	if crv == "" || xB64 == "" || yB64 == "" {
		return nil, errors.New("missing crv/x/y")
	}
	var curve elliptic.Curve
	switch crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported curve %q", crv)
	}
	x, err := bigIntFromBase64URL(xB64)
	if err != nil {
		return nil, fmt.Errorf("decoding x: %w", err)
	}
	y, err := bigIntFromBase64URL(yB64)
	if err != nil {
		return nil, fmt.Errorf("decoding y: %w", err)
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

func bigIntFromBase64URL(s string) (*big.Int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(raw), nil
}
