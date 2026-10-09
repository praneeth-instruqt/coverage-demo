package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrTokenExpired = errors.New("token has expired")
	ErrWeakSecret   = errors.New("token secret must be at least 32 bytes")
)

// MinSecretLength is the minimum HMAC key size accepted by NewTokenManager.
const MinSecretLength = 32

// jwtHeader is the fixed, pre-encoded header for HS256 JWTs.
var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// Claims is the payload carried by an access token.
type Claims struct {
	Subject   string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// TokenManager issues and verifies HS256-signed JWT access tokens.
type TokenManager struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewTokenManager returns a manager signing with secret and issuing tokens valid for ttl.
func NewTokenManager(secret []byte, ttl time.Duration) (*TokenManager, error) {
	if len(secret) < MinSecretLength {
		return nil, ErrWeakSecret
	}
	return &TokenManager{secret: secret, ttl: ttl, now: time.Now}, nil
}

// Issue creates a signed token for userID and returns it with its expiry.
func (m *TokenManager) Issue(userID string) (string, time.Time, error) {
	now := m.now()
	exp := now.Add(m.ttl)
	payload, err := json.Marshal(Claims{Subject: userID, IssuedAt: now.Unix(), ExpiresAt: exp.Unix()})
	if err != nil {
		return "", time.Time{}, err
	}
	signingInput := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	return signingInput + "." + m.sign(signingInput), exp, nil
}

// Parse verifies the token's signature and expiry and returns its claims.
func (m *TokenManager) Parse(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != jwtHeader {
		return Claims{}, ErrInvalidToken
	}
	signingInput := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(m.sign(signingInput))) {
		return Claims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil || c.Subject == "" {
		return Claims{}, ErrInvalidToken
	}
	if m.now().Unix() >= c.ExpiresAt {
		return Claims{}, ErrTokenExpired
	}
	return c, nil
}

func (m *TokenManager) sign(input string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
