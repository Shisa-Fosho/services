package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/Shisa-Fosho/services/internal/platform/eth"
)

func testJWTConfig() JWTConfig {
	return JWTConfig{
		AccessSecret:  []byte("test-access-secret-that-is-32-bytes!!"),
		RefreshSecret: []byte("test-refresh-secret-that-is-32-bytes!"),
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    7 * 24 * time.Hour,
		Issuer:        "shisa-test",
	}
}

func TestNewJWTManager_RejectsShortSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		accessSecret  []byte
		refreshSecret []byte
		wantErr       string
	}{
		{
			name:          "short access secret",
			accessSecret:  []byte("too-short"),
			refreshSecret: []byte("test-refresh-secret-that-is-32-bytes!"),
			wantErr:       "access secret",
		},
		{
			name:          "short refresh secret",
			accessSecret:  []byte("test-access-secret-that-is-32-bytes!!"),
			refreshSecret: []byte("too-short"),
			wantErr:       "refresh secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewJWTManager(JWTConfig{
				AccessSecret:  tt.accessSecret,
				RefreshSecret: tt.refreshSecret,
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestIssueAccessToken_RoundTrip(t *testing.T) {
	t.Parallel()
	mgr, err := NewJWTManager(testJWTConfig())
	if err != nil {
		t.Fatalf("creating manager: %v", err)
	}

	addr := "0x1234567890abcdef1234567890abcdef12345678"
	tokenStr, err := mgr.IssueAccessToken(addr)
	if err != nil {
		t.Fatalf("issuing access token: %v", err)
	}

	claims, err := mgr.ValidateAccessToken(tokenStr)
	if err != nil {
		t.Fatalf("validating access token: %v", err)
	}
	if claims.Subject != addr {
		t.Errorf("address = %q, want %q", claims.Subject, addr)
	}
	if claims.Kind != TokenAccess {
		t.Errorf("kind = %q, want %q", claims.Kind, TokenAccess)
	}
	if claims.Issuer != "shisa-test" {
		t.Errorf("issuer = %q, want %q", claims.Issuer, "shisa-test")
	}
}

func TestIssueRefreshToken_RoundTrip(t *testing.T) {
	t.Parallel()
	mgr, err := NewJWTManager(testJWTConfig())
	if err != nil {
		t.Fatalf("creating manager: %v", err)
	}

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tokenStr, jti, expiresAt, err := mgr.IssueRefreshToken(addr)
	if err != nil {
		t.Fatalf("issuing refresh token: %v", err)
	}
	if jti == "" {
		t.Error("expected non-empty JTI")
	}
	if expiresAt.Before(time.Now()) {
		t.Error("expiresAt should be in the future")
	}

	claims, err := mgr.ValidateRefreshToken(tokenStr)
	if err != nil {
		t.Fatalf("validating refresh token: %v", err)
	}
	if claims.Subject != addr {
		t.Errorf("address = %q, want %q", claims.Subject, addr)
	}
	if claims.Kind != TokenRefresh {
		t.Errorf("kind = %q, want %q", claims.Kind, TokenRefresh)
	}
	if claims.ID != jti {
		t.Errorf("JTI = %q, want %q", claims.ID, jti)
	}
}

// TestValidateAccessToken_RejectsRefreshToken verifies cross-type rejection:
// a refresh token (signed with the refresh secret) must not be accepted by
// ValidateAccessToken (which uses the access secret). With dual secrets the
// signature check fails first, returning ErrInvalidToken.
func TestValidateAccessToken_RejectsRefreshToken(t *testing.T) {
	t.Parallel()

	mgr, err := NewJWTManager(testJWTConfig())
	if err != nil {
		t.Fatalf("creating manager: %v", err)
	}

	address := eth.TestAddress()

	signed, _, _, err := mgr.IssueRefreshToken(address)
	if err != nil {
		t.Fatalf("issuing token: %v", err)
	}

	_, err = mgr.ValidateAccessToken(signed)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got: %v", err)
	}
}

func TestValidateAccessToken_WrongSecret(t *testing.T) {
	t.Parallel()
	mgr, err := NewJWTManager(testJWTConfig())
	if err != nil {
		t.Fatalf("creating manager: %v", err)
	}

	tokenStr, err := mgr.IssueAccessToken("0x1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("issuing token: %v", err)
	}

	// Create a different manager with a different secret.
	otherCfg := testJWTConfig()
	otherCfg.AccessSecret = []byte("a-completely-different-secret-32bytes")
	otherMgr, err := NewJWTManager(otherCfg)
	if err != nil {
		t.Fatalf("creating other manager: %v", err)
	}

	_, err = otherMgr.ValidateAccessToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got: %v", err)
	}
}
