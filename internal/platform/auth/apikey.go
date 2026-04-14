package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const (
	minDerivationSecretLen = 32
	minEncryptionKeyLen    = 32 // AES-256 requires exactly 32 bytes.

	// ClobAuthMessage is the EIP-712 message text for Polymarket-compatible auth.
	ClobAuthMessage = "This message attests that I control the given wallet"
)

// APIKeyConfig holds the secrets needed for API key derivation and encryption.
type APIKeyConfig struct {
	DerivationSecret []byte // HMAC key for deterministic derivation (>= 32 bytes).
	EncryptionKey    []byte // AES-256-GCM key for HMAC secret encryption (== 32 bytes).
	ChainID          int64  // EIP-712 domain chain ID (e.g. 137 for Polygon mainnet).
}

// ValidateAPIKeyConfig checks that the config secrets meet minimum length requirements.
func ValidateAPIKeyConfig(cfg APIKeyConfig) error {
	if len(cfg.DerivationSecret) < minDerivationSecretLen {
		return fmt.Errorf("api key derivation secret must be at least %d bytes", minDerivationSecretLen)
	}
	if len(cfg.EncryptionKey) != minEncryptionKeyLen {
		return fmt.Errorf("api key encryption key must be exactly %d bytes", minEncryptionKeyLen)
	}
	if cfg.ChainID <= 0 {
		return fmt.Errorf("api key chain ID must be positive")
	}
	return nil
}

// clobAuthTypedData builds the EIP-712 TypedData for a ClobAuth message.
// This matches Polymarket's CLOB authentication format.
func clobAuthTypedData(address, timestamp, nonce, message string, chainID int64) apitypes.TypedData {
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"ClobAuth": {
				{Name: "address", Type: "address"},
				{Name: "timestamp", Type: "string"},
				{Name: "nonce", Type: "uint256"},
				{Name: "message", Type: "string"},
			},
		},
		PrimaryType: "ClobAuth",
		Domain: apitypes.TypedDataDomain{
			Name:              "ClobAuthDomain",
			Version:           "1",
			ChainId:           math.NewHexOrDecimal256(chainID),
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Message: apitypes.TypedDataMessage{
			"address":   address,
			"timestamp": timestamp,
			"nonce":     nonce,
			"message":   message,
		},
	}
}

// VerifyEIP712Signature verifies that the given EIP-712 ClobAuth signature was
// produced by the claimed address. Returns the raw signature bytes on success.
func VerifyEIP712Signature(address, timestamp, nonce, message, signature string, chainID int64) ([]byte, error) {
	td := clobAuthTypedData(address, timestamp, nonce, message, chainID)

	domainHash, err := td.HashStruct("EIP712Domain", td.Domain.Map())
	if err != nil {
		return nil, fmt.Errorf("hashing EIP-712 domain: %w", err)
	}
	messageHash, err := td.HashStruct(td.PrimaryType, td.Message)
	if err != nil {
		return nil, fmt.Errorf("hashing EIP-712 message: %w", err)
	}

	// EIP-712 encoding: 0x19 0x01 || domainSeparator || messageHash
	rawData := append([]byte{0x19, 0x01}, domainHash...)
	rawData = append(rawData, messageHash...)
	hash := crypto.Keccak256(rawData)

	sigBytes, err := hexToBytes(signature)
	if err != nil {
		return nil, fmt.Errorf("decoding signature hex: %w", err)
	}
	if len(sigBytes) != 65 {
		return nil, fmt.Errorf("signature must be 65 bytes, got %d", len(sigBytes))
	}

	// Normalize V: Ethereum uses 27/28, crypto.SigToPub expects 0/1.
	if sigBytes[64] >= 27 {
		sigBytes[64] -= 27
	}

	pubKey, err := crypto.SigToPub(hash, sigBytes)
	if err != nil {
		return nil, fmt.Errorf("recovering public key: %w", err)
	}

	recovered := crypto.PubkeyToAddress(*pubKey).Hex()
	if !strings.EqualFold(recovered, address) {
		return nil, fmt.Errorf("signer %s does not match claimed address %s", recovered, address)
	}

	return sigBytes, nil
}

// DeriveAPIKey deterministically derives an API key and HMAC secret from
// a server-side secret and the user's EIP-712 signature bytes.
// Same inputs always produce the same outputs (idempotent).
func DeriveAPIKey(secret, sigBytes []byte) (apiKey string, hmacSecret string) {
	mac1 := hmac.New(sha256.New, secret)
	mac1.Write(append([]byte("api-key"), sigBytes...))
	result1 := mac1.Sum(nil)

	mac2 := hmac.New(sha256.New, secret)
	mac2.Write(append([]byte("hmac-secret"), sigBytes...))
	result2 := mac2.Sum(nil)

	return hex.EncodeToString(result1[:16]), hex.EncodeToString(result2)
}

// HashAPIKey returns the hex-encoded SHA-256 hash of an API key.
// Used for storage and lookup — the raw key is never persisted.
func HashAPIKey(apiKey string) string {
	h := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(h[:])
}

// EncryptSecret encrypts plaintext using AES-256-GCM. Returns hex-encoded
// nonce || ciphertext. The nonce is randomly generated per call.
func EncryptSecret(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// DecryptSecret decrypts hex-encoded nonce || ciphertext produced by EncryptSecret.
func DecryptSecret(key []byte, ciphertextHex string) (string, error) {
	data, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", fmt.Errorf("decoding ciphertext hex: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypting: %w", err)
	}
	return string(plaintext), nil
}
