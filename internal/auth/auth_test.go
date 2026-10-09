package auth

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

// Low iteration count keeps tests fast; production uses DefaultIterations.
const testIterations = 1000

var testSecret = []byte("0123456789abcdef0123456789abcdef")

func TestCredentialsNormalize(t *testing.T) {
	c := Credentials{Email: "  Alice@Example.COM ", Password: " secret "}
	c.Normalize()
	if c.Email != "alice@example.com" {
		t.Errorf("email = %q", c.Email)
	}
	if c.Password != " secret " {
		t.Error("password must not be modified")
	}
}

func TestCredentialsValidateForRegistration(t *testing.T) {
	tests := []struct {
		name string
		c    Credentials
		want error
	}{
		{"valid", Credentials{"a@b.co", "password1"}, nil},
		{"missing email", Credentials{"", "password1"}, ErrInvalidEmail},
		{"no at sign", Credentials{"not-an-email", "password1"}, ErrInvalidEmail},
		{"display name form", Credentials{"Bob <bob@x.io>", "password1"}, ErrInvalidEmail},
		{"short password", Credentials{"a@b.co", "short"}, ErrPasswordTooShort},
		{"long password", Credentials{"a@b.co", strings.Repeat("p", MaxPasswordLength+1)}, ErrPasswordTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.c.ValidateForRegistration(); !errors.Is(err, tt.want) {
				t.Errorf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestIsValidationError(t *testing.T) {
	for _, err := range []error{ErrInvalidEmail, ErrPasswordTooShort, ErrPasswordTooLong} {
		if !IsValidationError(err) {
			t.Errorf("%v should be a validation error", err)
		}
	}
	if IsValidationError(ErrEmailTaken) || IsValidationError(nil) {
		t.Error("unexpected validation error match")
	}
}

func TestNewPasswordHasherDefaults(t *testing.T) {
	if got := NewPasswordHasher(0).Iterations; got != DefaultIterations {
		t.Errorf("iterations = %d, want default %d", got, DefaultIterations)
	}
	if got := NewPasswordHasher(-5).Iterations; got != DefaultIterations {
		t.Errorf("iterations = %d, want default %d", got, DefaultIterations)
	}
	if got := NewPasswordHasher(42).Iterations; got != 42 {
		t.Errorf("iterations = %d, want 42", got)
	}
}

func TestPasswordHashAndVerify(t *testing.T) {
	h := NewPasswordHasher(testIterations)

	hash, err := h.Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "pbkdf2-sha256$1000$") {
		t.Errorf("unexpected hash format: %s", hash)
	}

	other, _ := h.Hash("correct horse")
	if hash == other {
		t.Error("hashes of the same password must differ (random salt)")
	}

	if ok, err := h.Verify(hash, "correct horse"); err != nil || !ok {
		t.Errorf("Verify(correct) = %v, %v", ok, err)
	}
	if ok, err := h.Verify(hash, "wrong"); err != nil || ok {
		t.Errorf("Verify(wrong) = %v, %v", ok, err)
	}

	// A hasher configured with different iterations still verifies old hashes.
	if ok, _ := NewPasswordHasher(5).Verify(hash, "correct horse"); !ok {
		t.Error("verification should use iterations stored in the hash")
	}
}

func TestPasswordVerifyMalformed(t *testing.T) {
	h := NewPasswordHasher(testIterations)
	b64 := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
	for _, encoded := range []string{
		"",
		"plain",
		"bcrypt$10$" + b64 + "$" + b64,
		"pbkdf2-sha256$abc$" + b64 + "$" + b64,
		"pbkdf2-sha256$0$" + b64 + "$" + b64,
		"pbkdf2-sha256$10$!!!$" + b64,
		"pbkdf2-sha256$10$" + b64 + "$!!!",
		"pbkdf2-sha256$10$" + b64 + "$",
		"pbkdf2-sha256$10$" + b64 + "$" + b64 + "$extra",
	} {
		if ok, err := h.Verify(encoded, "pw"); !errors.Is(err, errMalformedHash) || ok {
			t.Errorf("Verify(%q) = %v, %v; want malformed error", encoded, ok, err)
		}
	}
}

func TestNewTokenManagerRejectsWeakSecret(t *testing.T) {
	if _, err := NewTokenManager([]byte("short"), time.Hour); !errors.Is(err, ErrWeakSecret) {
		t.Errorf("err = %v, want ErrWeakSecret", err)
	}
}

func newTestTokens(t *testing.T, now time.Time) *TokenManager {
	t.Helper()
	m, err := NewTokenManager(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return now }
	return m
}

func TestTokenRoundTrip(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	m := newTestTokens(t, now)

	tok, exp, err := m.Issue("user-1")
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(now.Add(time.Hour)) {
		t.Errorf("exp = %v, want %v", exp, now.Add(time.Hour))
	}
	if strings.Count(tok, ".") != 2 {
		t.Errorf("token is not a JWT: %s", tok)
	}

	c, err := m.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "user-1" || c.IssuedAt != now.Unix() || c.ExpiresAt != exp.Unix() {
		t.Errorf("claims = %+v", c)
	}
}

func TestTokenExpired(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	m := newTestTokens(t, now)
	tok, _, _ := m.Issue("u")

	m.now = func() time.Time { return now.Add(time.Hour) }
	if _, err := m.Parse(tok); !errors.Is(err, ErrTokenExpired) {
		t.Errorf("err = %v, want ErrTokenExpired", err)
	}
}

func TestTokenRejectsTampering(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	m := newTestTokens(t, now)
	tok, _, _ := m.Issue("u")
	parts := strings.Split(tok, ".")

	enc := base64.RawURLEncoding
	forgedPayload := enc.EncodeToString([]byte(`{"sub":"admin","iat":1,"exp":9999999999}`))

	otherKey, _ := NewTokenManager([]byte("ffffffffffffffffffffffffffffffff"), time.Hour)
	otherKey.now = m.now
	foreign, _, _ := otherKey.Issue("u")

	signed := func(header, payload string) string {
		in := header + "." + payload
		return in + "." + m.sign(in)
	}

	cases := map[string]string{
		"empty":            "",
		"two parts":        parts[0] + "." + parts[1],
		"forged payload":   parts[0] + "." + forgedPayload + "." + parts[2],
		"bad signature":    parts[0] + "." + parts[1] + ".AAAA",
		"other secret":     foreign,
		"alg none header":  enc.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + parts[1] + ".",
		"bad payload b64":  signed(jwtHeader, "!!!"),
		"bad payload json": signed(jwtHeader, enc.EncodeToString([]byte("not json"))),
		"missing subject":  signed(jwtHeader, enc.EncodeToString([]byte(`{"exp":9999999999}`))),
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := m.Parse(tok); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("err = %v, want ErrInvalidToken", err)
			}
		})
	}
}
