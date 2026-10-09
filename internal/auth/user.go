// Package auth handles users, password hashing and access tokens.
package auth

import (
	"errors"
	"net/mail"
	"strings"
	"time"
)

const (
	MinPasswordLength = 8
	MaxPasswordLength = 72
)

var (
	ErrEmailTaken         = errors.New("email is already registered")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidEmail       = errors.New("email is invalid")
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong    = errors.New("password must be at most 72 characters")
)

// User is a registered account.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// Credentials is the payload for both registration and login.
type Credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Normalize trims and lower-cases the email so lookups are case-insensitive.
func (c *Credentials) Normalize() {
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
}

// ValidateForRegistration enforces email format and password strength.
func (c Credentials) ValidateForRegistration() error {
	addr, err := mail.ParseAddress(c.Email)
	if err != nil || addr.Address != c.Email {
		return ErrInvalidEmail
	}
	if len(c.Password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(c.Password) > MaxPasswordLength {
		return ErrPasswordTooLong
	}
	return nil
}

// IsValidationError reports whether err is one of the credential validation errors.
func IsValidationError(err error) bool {
	return errors.Is(err, ErrInvalidEmail) ||
		errors.Is(err, ErrPasswordTooShort) ||
		errors.Is(err, ErrPasswordTooLong)
}
