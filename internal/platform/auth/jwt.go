// Package auth provides authentication and session management for the
// prediction market platform: SIWE wallet verification, JWT token issuance
// and validation, and auth middleware.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors for token operations.
var (
	ErrInvalidToken = errors.New("invalid token")
	ErrTokenKind    = errors.New("wrong token kind")
)

// TokenKind distinguishes access and refresh tokens.
type TokenKind string

// Token kinds.
const (
	TokenAccess  TokenKind = "access"
	TokenRefresh TokenKind = "refresh"
)

// Claims is the custom JWT claims type used for both access and refresh tokens.
// The user's Ethereum address is stored in the standard Subject field.
type Claims struct {
	jwt.RegisteredClaims
	Kind TokenKind `json:"kind"` // "access" or "refresh".
}

// JWTConfig holds the signing secrets and token lifetimes.
type JWTConfig struct {
	AccessSecret  []byte        // HMAC-SHA256 signing key for access tokens.
	RefreshSecret []byte        // HMAC-SHA256 signing key for refresh tokens.
	AccessTTL     time.Duration // Default: 15 minutes.
	RefreshTTL    time.Duration // Default: 7 days.
	Issuer        string        // Token issuer (e.g., "shisa-trading").
}

const minSecretLen = 32

// JWTManager creates and validates JWT tokens.
type JWTManager struct {
	cfg JWTConfig
}

// NewJWTManager creates a JWTManager from the given config.
// Returns an error if secrets are empty or shorter than 32 bytes.
func NewJWTManager(cfg JWTConfig) (*JWTManager, error) {
	if len(cfg.AccessSecret) < minSecretLen {
		return nil, fmt.Errorf("access secret must be at least %d bytes", minSecretLen)
	}
	if len(cfg.RefreshSecret) < minSecretLen {
		return nil, fmt.Errorf("refresh secret must be at least %d bytes", minSecretLen)
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 7 * 24 * time.Hour
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "shisa"
	}
	return &JWTManager{cfg: cfg}, nil
}

// IssueAccessToken creates a signed access token for the given address.
func (manager *JWTManager) IssueAccessToken(address string) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    manager.cfg.Issuer,
			Subject:   address,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(manager.cfg.AccessTTL)),
		},
		Kind: TokenAccess,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(manager.cfg.AccessSecret)
	if err != nil {
		return "", fmt.Errorf("signing access token: %w", err)
	}
	return signed, nil
}

// IssueRefreshToken creates a signed refresh token for the given address.
// Returns (tokenString, jti, expiresAt, error) so the caller can persist
// the JTI for rotation tracking.
func (manager *JWTManager) IssueRefreshToken(address string) (string, string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(manager.cfg.RefreshTTL)
	jti, err := randomHex(16)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("generating JTI: %w", err)
	}

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Issuer:    manager.cfg.Issuer,
			Subject:   address,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		Kind: TokenRefresh,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(manager.cfg.RefreshSecret)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("signing refresh token: %w", err)
	}
	return signed, jti, expiresAt, nil
}

// ValidateAccessToken parses and validates an access token string.
// Returns the Claims or an error. Rejects refresh tokens.
func (manager *JWTManager) ValidateAccessToken(tokenStr string) (*Claims, error) {
	return manager.validateToken(tokenStr, manager.cfg.AccessSecret, TokenAccess)
}

// ValidateRefreshToken parses and validates a refresh token string.
// Returns the Claims or an error. Rejects access tokens.
func (manager *JWTManager) ValidateRefreshToken(tokenStr string) (*Claims, error) {
	return manager.validateToken(tokenStr, manager.cfg.RefreshSecret, TokenRefresh)
}

func (manager *JWTManager) validateToken(tokenStr string, secret []byte, expectedKind TokenKind) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}
	if claims.Kind != expectedKind {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrTokenKind, expectedKind, claims.Kind)
	}
	return claims, nil
}

// randomHex generates size cryptographically random bytes and returns them
// as a hex-encoded string.
func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
