package auth

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var testDerivationSecret = []byte("test-derivation-secret-32-bytes!")
var testEncryptionKey = []byte("test-encryption-key-32-bytes!!xx")

func TestDeriveAPIKey_Deterministic(t *testing.T) {
	t.Parallel()

	sig := make([]byte, 65)
	for i := range sig {
		sig[i] = byte(i)
	}

	key1, secret1, pass1 := DeriveAPIKey(testDerivationSecret, sig)
	key2, secret2, pass2 := DeriveAPIKey(testDerivationSecret, sig)

	if key1 != key2 {
		t.Errorf("api keys differ: %q vs %q", key1, key2)
	}
	if secret1 != secret2 {
		t.Errorf("hmac secrets differ: %q vs %q", secret1, secret2)
	}
	if pass1 != pass2 {
		t.Errorf("passphrases differ: %q vs %q", pass1, pass2)
	}
}

func TestDeriveAPIKey_DifferentSignatures(t *testing.T) {
	t.Parallel()

	sig1 := make([]byte, 65)
	sig2 := make([]byte, 65)
	sig2[0] = 0xFF

	key1, secret1, pass1 := DeriveAPIKey(testDerivationSecret, sig1)
	key2, secret2, pass2 := DeriveAPIKey(testDerivationSecret, sig2)

	if key1 == key2 {
		t.Error("different signatures should produce different api keys")
	}
	if secret1 == secret2 {
		t.Error("different signatures should produce different hmac secrets")
	}
	if pass1 == pass2 {
		t.Error("different signatures should produce different passphrases")
	}
}

func TestDeriveAPIKey_KeyLength(t *testing.T) {
	t.Parallel()

	sig := make([]byte, 65)
	key, secret, passphrase := DeriveAPIKey(testDerivationSecret, sig)

	// API key: 16 bytes hex-encoded = 32 chars.
	if len(key) != 32 {
		t.Errorf("api key length = %d, want 32", len(key))
	}
	// HMAC secret: 32 bytes base64url-encoded with '=' padding = 44 chars.
	// Matches Polymarket clob-client v5.8.2 ApiKeyRaw.secret shape.
	if len(secret) != 44 {
		t.Errorf("hmac secret length = %d, want 44", len(secret))
	}
	// Passphrase: 16 bytes hex-encoded = 32 chars.
	if len(passphrase) != 32 {
		t.Errorf("passphrase length = %d, want 32", len(passphrase))
	}
}

func TestDeriveAPIKey_SecretIsBase64URL(t *testing.T) {
	t.Parallel()

	sig := make([]byte, 65)
	_, secret, _ := DeriveAPIKey(testDerivationSecret, sig)

	// SDK contract: client base64url-decodes the secret into raw bytes
	// (base64.urlsafe_b64decode / base64ToArrayBuffer). Must decode cleanly.
	decoded, err := base64.URLEncoding.DecodeString(secret)
	if err != nil {
		t.Fatalf("secret must be URL-safe base64 with '=' padding, decode failed: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("decoded secret length = %d, want 32 (sha256 output)", len(decoded))
	}
}

func TestDeriveAPIKey_KeyAndSecretDiffer(t *testing.T) {
	t.Parallel()

	sig := make([]byte, 65)
	key, secret, passphrase := DeriveAPIKey(testDerivationSecret, sig)

	if secret == key {
		t.Error("api key should not equal hmac secret")
	}
	if passphrase == key {
		t.Error("passphrase should not equal api key")
	}
	if passphrase == secret {
		t.Error("passphrase should not equal hmac secret")
	}
}

func TestHashAPIKey_Consistency(t *testing.T) {
	t.Parallel()

	h1 := HashAPIKey("test-key-123")
	h2 := HashAPIKey("test-key-123")

	if h1 != h2 {
		t.Errorf("hashes differ: %q vs %q", h1, h2)
	}

	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}

func TestEncryptDecryptSecret_RoundTrip(t *testing.T) {
	t.Parallel()

	plaintext := "my-hmac-secret-value"
	encrypted, err := EncryptSecret(testEncryptionKey, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decrypted, err := DecryptSecret(testEncryptionKey, encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptSecret_DifferentNonces(t *testing.T) {
	t.Parallel()

	plaintext := "same-plaintext"
	enc1, _ := EncryptSecret(testEncryptionKey, plaintext)
	enc2, _ := EncryptSecret(testEncryptionKey, plaintext)

	if enc1 == enc2 {
		t.Error("two encryptions of the same plaintext should differ (random nonce)")
	}
}

// eip712Hash builds the EIP-712 hash for a ClobAuth message (test helper).
func eip712Hash(t *testing.T, address string) []byte {
	t.Helper()
	td := clobAuthTypedData(address, "1700000000", "0", ClobAuthMessage, 137)
	domainHash, err := td.HashStruct("EIP712Domain", td.Domain.Map())
	if err != nil {
		t.Fatalf("hashing domain: %v", err)
	}
	messageHash, err := td.HashStruct(td.PrimaryType, td.Message)
	if err != nil {
		t.Fatalf("hashing message: %v", err)
	}
	rawData := append([]byte{0x19, 0x01}, domainHash...)
	rawData = append(rawData, messageHash...)
	return crypto.Keccak256(rawData)
}

func TestVerifyEIP712Signature_Valid(t *testing.T) {
	t.Parallel()

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	address := crypto.PubkeyToAddress(key.PublicKey).Hex()

	hash := eip712Hash(t, address)
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	sigHex := "0x" + hex.EncodeToString(sig)
	sigBytes, err := VerifyEIP712Signature(address, "1700000000", "0", ClobAuthMessage, sigHex, 137)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(sigBytes) != 65 {
		t.Errorf("sigBytes length = %d, want 65", len(sigBytes))
	}
}

func TestVerifyEIP712Signature_WrongSigner(t *testing.T) {
	t.Parallel()

	key, _ := crypto.GenerateKey()
	wrongAddress := common.HexToAddress("0x1111111111111111111111111111111111111111").Hex()

	hash := eip712Hash(t, wrongAddress)
	sig, _ := crypto.Sign(hash, key)
	sigHex := "0x" + hex.EncodeToString(sig)

	_, err := VerifyEIP712Signature(wrongAddress, "1700000000", "0", ClobAuthMessage, sigHex, 137)
	if err == nil {
		t.Fatal("expected error for wrong signer, got nil")
	}
}

func TestValidateAPIKeyConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     APIKeyConfig
		wantErr bool
	}{
		{
			name: "valid",
			cfg: APIKeyConfig{
				DerivationSecret: make([]byte, 32),
				EncryptionKey:    make([]byte, 32),
				ChainID:          137,
			},
		},
		{
			name: "short derivation secret",
			cfg: APIKeyConfig{
				DerivationSecret: make([]byte, 16),
				EncryptionKey:    make([]byte, 32),
				ChainID:          137,
			},
			wantErr: true,
		},
		{
			name: "wrong encryption key length",
			cfg: APIKeyConfig{
				DerivationSecret: make([]byte, 32),
				EncryptionKey:    make([]byte, 16),
				ChainID:          137,
			},
			wantErr: true,
		},
		{
			name: "zero chain ID",
			cfg: APIKeyConfig{
				DerivationSecret: make([]byte, 32),
				EncryptionKey:    make([]byte, 32),
				ChainID:          0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAPIKeyConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAPIKeyConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
