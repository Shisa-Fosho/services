package auth

import (
	"crypto/ecdsa"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
)

// testSIWEKey generates a fresh ECDSA key pair for SIWE test signing.
func testSIWEKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey).Hex()
	return key, addr
}

// signSIWEMessage signs a SIWE message with EIP-191 personal_sign.
func signSIWEMessage(t *testing.T, key *ecdsa.PrivateKey, message string) string {
	t.Helper()
	hash := accounts.TextHash([]byte(message))
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("signing message: %v", err)
	}
	// Convert V from 0/1 to 27/28 (Ethereum convention).
	sig[64] += 27
	return fmt.Sprintf("0x%x", sig)
}

// buildSIWEMessage constructs a valid EIP-4361 message string.
func buildSIWEMessage(domain, address, nonce string, expiration *time.Time) string {
	now := time.Now().UTC().Truncate(time.Second)
	var b strings.Builder
	fmt.Fprintf(&b, "%s wants you to sign in with your Ethereum account:\n", domain)
	fmt.Fprintf(&b, "%s\n", address)
	fmt.Fprintf(&b, "\nSign in to Shisa\n\n")
	fmt.Fprintf(&b, "URI: https://%s\n", domain)
	fmt.Fprintf(&b, "Version: 1\n")
	fmt.Fprintf(&b, "Chain ID: 137\n")
	fmt.Fprintf(&b, "Nonce: %s\n", nonce)
	fmt.Fprintf(&b, "Issued At: %s", now.Format(time.RFC3339))
	if expiration != nil {
		fmt.Fprintf(&b, "\nExpiration Time: %s", expiration.Format(time.RFC3339))
	}
	return b.String()
}

func TestSIWEVerify_ValidMessage(t *testing.T) {
	t.Parallel()

	key, addr := testSIWEKey(t)
	domain := "app.shisa.market"
	nonce := GenerateNonce()
	exp := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second)

	message := buildSIWEMessage(domain, addr, nonce, &exp)
	signature := signSIWEMessage(t, key, message)

	verifier := NewSIWEVerifier(SIWEConfig{Domain: domain})
	recovered, err := verifier.Verify(message, signature)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if !strings.EqualFold(recovered, addr) {
		t.Errorf("address = %q, want %q", recovered, addr)
	}
}

func TestSIWEVerify_WrongDomain(t *testing.T) {
	t.Parallel()

	key, addr := testSIWEKey(t)
	nonce := GenerateNonce()

	message := buildSIWEMessage("evil.com", addr, nonce, nil)
	signature := signSIWEMessage(t, key, message)

	verifier := NewSIWEVerifier(SIWEConfig{Domain: "app.shisa.market"})
	_, err := verifier.Verify(message, signature)
	if err == nil {
		t.Fatal("expected domain mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "domain mismatch") {
		t.Errorf("error = %q, want domain mismatch", err.Error())
	}
}

func TestSIWEVerify_ExpiredMessage(t *testing.T) {
	t.Parallel()

	key, addr := testSIWEKey(t)
	domain := "app.shisa.market"
	nonce := GenerateNonce()
	exp := time.Now().Add(-1 * time.Minute).UTC().Truncate(time.Second)

	message := buildSIWEMessage(domain, addr, nonce, &exp)
	signature := signSIWEMessage(t, key, message)

	verifier := NewSIWEVerifier(SIWEConfig{Domain: domain})
	_, err := verifier.Verify(message, signature)
	if err == nil {
		t.Fatal("expected expiration error, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %q, want expired", err.Error())
	}
}

func TestSIWEVerify_InvalidSignature(t *testing.T) {
	t.Parallel()

	_, addr := testSIWEKey(t)
	otherKey, _ := testSIWEKey(t) // Sign with a different key.
	domain := "app.shisa.market"
	nonce := GenerateNonce()

	message := buildSIWEMessage(domain, addr, nonce, nil)
	signature := signSIWEMessage(t, otherKey, message)

	verifier := NewSIWEVerifier(SIWEConfig{Domain: domain})
	_, err := verifier.Verify(message, signature)
	if err == nil {
		t.Fatal("expected signer mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "signer mismatch") {
		t.Errorf("error = %q, want signer mismatch", err.Error())
	}
}

func TestParseSIWEMessage_Valid(t *testing.T) {
	t.Parallel()

	_, addr := testSIWEKey(t)
	domain := "app.shisa.market"
	nonce := "abc123"
	exp := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second)

	raw := buildSIWEMessage(domain, addr, nonce, &exp)
	msg, err := ParseSIWEMessage(raw)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if msg.Domain != domain {
		t.Errorf("domain = %q, want %q", msg.Domain, domain)
	}
	if !strings.EqualFold(msg.Address, addr) {
		t.Errorf("address = %q, want %q", msg.Address, addr)
	}
	if msg.Nonce != nonce {
		t.Errorf("nonce = %q, want %q", msg.Nonce, nonce)
	}
}

func TestParseSIWEMessage_MissingFields(t *testing.T) {
	t.Parallel()

	_, err := ParseSIWEMessage("too short")
	if err == nil {
		t.Fatal("expected error for short message, got nil")
	}
}

func TestGenerateNonce_Uniqueness(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		n := GenerateNonce()
		if seen[n] {
			t.Fatalf("duplicate nonce at iteration %d: %s", i, n)
		}
		seen[n] = true
	}
}

func TestGenerateNonce_Length(t *testing.T) {
	t.Parallel()

	n := GenerateNonce()
	if len(n) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("nonce length = %d, want 32", len(n))
	}
}
