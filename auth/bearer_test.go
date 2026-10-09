package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	gosdk "github.com/modelcontextprotocol/go-sdk/auth"
)

// b64URL encodes a big.Int as unpadded base64url (JWK form).
func b64URL(i *big.Int) string {
	return base64.RawURLEncoding.EncodeToString(i.Bytes())
}

// jwksBody renders a JWKS document for the given keys with bearer "kid"
// values ("rsa-0", "ec-0", ...) unless noKids is true (single-key JWKSet).
func jwksBody(rsas []*rsa.PublicKey, ecs []*ecdsa.PublicKey, noKids bool) string {
	var keys []map[string]string
	for i, r := range rsas {
		eRaw := make([]byte, 4)
		binary.BigEndian.PutUint32(eRaw, uint32(r.E))
		for len(eRaw) > 0 && eRaw[0] == 0 {
			eRaw = eRaw[1:]
		}
		k := map[string]string{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"n":   b64URL(r.N),
			"e":   base64.RawURLEncoding.EncodeToString(eRaw),
		}
		if !noKids {
			k["kid"] = "rsa-" + itoa(i)
		}
		keys = append(keys, k)
	}
	for i, e := range ecs {
		k := map[string]string{
			"kty": "EC",
			"use": "sig",
			"alg": "ES256",
			"crv": "P-256",
			"x":   b64URL(e.X),
			"y":   b64URL(e.Y),
		}
		if !noKids {
			k["kid"] = "ec-" + itoa(i)
		}
		keys = append(keys, k)
	}
	body, _ := json.Marshal(map[string]any{"keys": keys})
	return string(body)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// jwksTestServer starts an httptest server exposing the JWKS document and
// counts refreshes (to verify rotation handling).
func jwksTestServer(t *testing.T, body func() string) (url string, refreshes *int) {
	t.Helper()
	refreshes = new(int)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*refreshes++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body()))
	}))
	t.Cleanup(ts.Close)
	return ts.URL, refreshes
}

func signJWT(t *testing.T, method string, key any, claims jwt.MapClaims, kid string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.GetSigningMethod(method), claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func authedRequest(token string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func newBearer(t *testing.T, jwksURL string, extra *BearerConfig) http.Handler {
	t.Helper()
	cfg := &BearerConfig{JWKSURL: jwksURL}
	if extra != nil {
		cfg.Issuer = extra.Issuer
		cfg.Audience = extra.Audience
		cfg.ResourceMetadataURL = extra.ResourceMetadataURL
		cfg.Scopes = extra.Scopes
		cfg.HTTPClient = extra.HTTPClient
	}
	mw, err := BearerMiddleware(cfg)
	if err != nil {
		t.Fatalf("BearerMiddleware: %v", err)
	}
	return mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ti := gosdk.TokenInfoFromContext(r.Context())
		if ti == nil {
			t.Error("TokenInfo missing from context")
			return
		}
		if ti.Expiration.IsZero() {
			t.Error("TokenInfo.Expiration is zero")
		}
	}))
}

func TestBearerRejectsMissingToken(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	url, _ := jwksTestServer(t, func() string { return jwksBody([]*rsa.PublicKey{&rsaKey.PublicKey}, nil, true) })
	h := newBearer(t, url, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestBearerVerifiesRSAAndEC(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	ecPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecKey := ecPriv.Public().(*ecdsa.PublicKey)

	url, _ := jwksTestServer(t, func() string {
		return jwksBody([]*rsa.PublicKey{&rsaKey.PublicKey}, []*ecdsa.PublicKey{ecKey}, false)
	})
	h := newBearer(t, url, nil)

	now := time.Now()
	cases := []struct {
		name   string
		method string
		key    any
		kid    string
	}{
		{"rs256", "RS256", rsaKey, "rsa-0"},
		{"es256", "ES256", ecPriv, "ec-0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := signJWT(t, tc.method, tc.key, jwt.MapClaims{
				"exp": now.Add(time.Hour).Unix(),
			}, tc.kid)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, authedRequest(token))
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestBearerRejectsExpiredAndForged(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	url, _ := jwksTestServer(t, func() string { return jwksBody([]*rsa.PublicKey{&rsaKey.PublicKey}, nil, true) })
	h := newBearer(t, url, nil)

	expired := signJWT(t, "RS256", rsaKey, jwt.MapClaims{"exp": time.Now().Add(-time.Hour).Unix()}, "")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(expired))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: expected 401, got %d", rec.Code)
	}

	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	forged := signJWT(t, "RS256", other, jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()}, "")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(forged))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged token: expected 401, got %d", rec.Code)
	}
}

func TestBearerClaimsConfig(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	url, _ := jwksTestServer(t, func() string { return jwksBody([]*rsa.PublicKey{&rsaKey.PublicKey}, nil, true) })
	h := newBearer(t, url, &BearerConfig{
		Issuer:              "https://auth.example.com/",
		Audience:            "https://mcp.example.com/mcp",
		ResourceMetadataURL: "https://mcp.example.com/.well-known/oauth-protected-resource",
		Scopes:              []string{"mcp"},
	})

	wrongAud := signJWT(t, "RS256", rsaKey, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
		"iss": "https://auth.example.com/",
		"aud": "some-other-resource",
	}, "")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(wrongAud))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong audience: expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "resource_metadata") {
		t.Errorf("expected WWW-Authenticate resource_metadata hint, got %q", rec.Header().Get("WWW-Authenticate"))
	}

	right := signJWT(t, "RS256", rsaKey, jwt.MapClaims{
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iss":   "https://auth.example.com/",
		"aud":   "https://mcp.example.com/mcp",
		"scope": "mcp",
	}, "")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(right))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestBearerKeyRotation(t *testing.T) {
	rsaKey1, _ := rsa.GenerateKey(rand.Reader, 2048)
	rsaKey2, _ := rsa.GenerateKey(rand.Reader, 2048)
	rotated := false
	url, refreshes := jwksTestServer(t, func() string {
		if rotated {
			// Real rotation: the old key is gone, the new key carries a new kid.
			body, _ := json.Marshal(map[string]any{
				"keys": []map[string]string{{
					"kty": "RSA", "use": "sig", "alg": "RS256",
					"kid": "rsa-1",
					"n":   b64URL(rsaKey2.N),
					"e":   b64URL(big.NewInt(int64(rsaKey2.E))),
				}},
			})
			return string(body)
		}
		return jwksBody([]*rsa.PublicKey{&rsaKey1.PublicKey}, nil, false)
	})
	h := newBearer(t, url, nil)

	// Token signed by key 1 succeeds.
	t1 := signJWT(t, "RS256", rsaKey1, jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()}, "rsa-0")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(t1))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for original key, got %d", rec.Code)
	}

	// Server rotates to key 2 under a NEW kid (real rotation); token signed
	// by key 2 (unknown kid) forces a JWKS refresh and then succeeds.
	rotated = true
	t2 := signJWT(t, "RS256", rsaKey2, jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()}, "rsa-1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(t2))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after rotation, got %d", rec.Code)
	}
	if *refreshes < 2 {
		t.Errorf("expected JWKS refresh on rotation, refreshes=%d", *refreshes)
	}
}

func TestBearerExposesTokenInfoUserID(t *testing.T) {
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	url, _ := jwksTestServer(t, func() string { return jwksBody([]*rsa.PublicKey{&rsaKey.PublicKey}, nil, false) })

	gotSub := ""
	mw, err := BearerMiddleware(&BearerConfig{JWKSURL: url})
	if err != nil {
		t.Fatal(err)
	}
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if ti := gosdk.TokenInfoFromContext(r.Context()); ti != nil {
			gotSub = ti.UserID
		}
	}))
	token := signJWT(t, "RS256", rsaKey, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": "user@example.com",
	}, "rsa-0")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(token))
	if gotSub != "user@example.com" {
		t.Errorf("expected sub in TokenInfo.UserID, got %q", gotSub)
	}
}
