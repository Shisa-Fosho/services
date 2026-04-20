package auth

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Shisa-Fosho/services/internal/shared/eth"
)

// EIP-4361 field prefixes used during message parsing.
const (
	uriPrefix        = "URI: "
	versionPrefix    = "Version: "
	chainIDPrefix    = "Chain ID: "
	noncePrefix      = "Nonce: "
	issuedAtPrefix   = "Issued At: "
	expirationPrefix = "Expiration Time: "
)

// SIWEMessage represents a parsed EIP-4361 Sign-In with Ethereum message.
type SIWEMessage struct {
	Domain         string
	Address        string
	Statement      string
	URI            string
	Version        string
	ChainID        string
	Nonce          string
	IssuedAt       time.Time
	ExpirationTime *time.Time // Optional.
}

// ParseSIWEMessage parses an EIP-4361 message string into its structured fields.
// The expected format is:
//
//	{domain} wants you to sign in with your Ethereum account:
//	{address}
//
//	{statement (optional)}
//
//	URI: {uri}
//	Version: {version}
//	Chain ID: {chain-id}
//	Nonce: {nonce}
//	Issued At: {iso8601}
//	Expiration Time: {iso8601} (optional)
func ParseSIWEMessage(message string) (*SIWEMessage, error) {
	lines := strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n")
	if len(lines) < 7 {
		return nil, fmt.Errorf("SIWE message too short: %d lines", len(lines))
	}

	// Line 0: "{domain} wants you to sign in with your Ethereum account:"
	header := lines[0]
	const suffix = " wants you to sign in with your Ethereum account:"
	if !strings.HasSuffix(header, suffix) {
		return nil, fmt.Errorf("invalid SIWE header: %q", header)
	}
	domain := strings.TrimSuffix(header, suffix)

	// Line 1: address.
	address := strings.TrimSpace(lines[1])
	if !eth.IsValidAddress(address) {
		return nil, fmt.Errorf("invalid Ethereum address: %q", address)
	}

	msg := &SIWEMessage{
		Domain:  domain,
		Address: common.HexToAddress(address).Hex(), // Checksummed.
	}

	// Find the field block by scanning for "URI: " line.
	fieldStart := -1
	for i := 2; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], uriPrefix) {
			fieldStart = i
			break
		}
	}
	if fieldStart < 0 {
		return nil, fmt.Errorf("SIWE message missing URI field")
	}

	// Everything between the address and fields block is the statement.
	// Typically separated by blank lines.
	stmtLines := lines[2:fieldStart]
	stmt := strings.TrimSpace(strings.Join(stmtLines, "\n"))
	msg.Statement = stmt

	// Parse required and optional fields.
	for i := fieldStart; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, uriPrefix):
			msg.URI = strings.TrimPrefix(line, uriPrefix)
		case strings.HasPrefix(line, versionPrefix):
			msg.Version = strings.TrimPrefix(line, versionPrefix)
		case strings.HasPrefix(line, chainIDPrefix):
			msg.ChainID = strings.TrimPrefix(line, chainIDPrefix)
		case strings.HasPrefix(line, noncePrefix):
			msg.Nonce = strings.TrimPrefix(line, noncePrefix)
		case strings.HasPrefix(line, issuedAtPrefix):
			t, err := time.Parse(time.RFC3339, strings.TrimPrefix(line, issuedAtPrefix))
			if err != nil {
				return nil, fmt.Errorf("parsing issued at: %w", err)
			}
			msg.IssuedAt = t
		case strings.HasPrefix(line, expirationPrefix):
			t, err := time.Parse(time.RFC3339, strings.TrimPrefix(line, expirationPrefix))
			if err != nil {
				return nil, fmt.Errorf("parsing expiration time: %w", err)
			}
			msg.ExpirationTime = &t
		}
	}

	if msg.URI == "" || msg.Version == "" || msg.Nonce == "" {
		return nil, fmt.Errorf("SIWE message missing required fields")
	}
	return msg, nil
}

// SIWEConfig holds verification parameters.
type SIWEConfig struct {
	Domain string // Expected domain (e.g., "app.shisa.market").
}

// MessageVerifier verifies signed authentication messages.
type MessageVerifier interface {
	Verify(message string, signature string) (string, error)
}

// SIWEVerifier validates SIWE messages and extracts the signer address.
type SIWEVerifier struct {
	cfg SIWEConfig
}

// NewSIWEVerifier creates a verifier with the given config.
func NewSIWEVerifier(cfg SIWEConfig) *SIWEVerifier {
	return &SIWEVerifier{cfg: cfg}
}

// Verify parses a SIWE message, validates the EIP-191 personal_sign signature,
// checks the domain and expiration, and returns the checksummed signer address.
func (verifier *SIWEVerifier) Verify(message string, signature string) (string, error) {
	parsed, err := ParseSIWEMessage(message)
	if err != nil {
		return "", fmt.Errorf("parsing SIWE message: %w", err)
	}

	// Validate domain.
	if parsed.Domain != verifier.cfg.Domain {
		return "", fmt.Errorf("domain mismatch: got %q, want %q", parsed.Domain, verifier.cfg.Domain)
	}

	// Validate expiration.
	if parsed.ExpirationTime != nil && time.Now().After(*parsed.ExpirationTime) {
		return "", fmt.Errorf("SIWE message expired at %s", parsed.ExpirationTime)
	}

	// Decode signature hex.
	sigBytes, err := hexToBytes(signature)
	if err != nil {
		return "", fmt.Errorf("decoding signature: %w", err)
	}
	if len(sigBytes) != 65 {
		return "", fmt.Errorf("invalid signature length: %d", len(sigBytes))
	}

	// EIP-191 recovery: transform V from Ethereum's 27/28 to 0/1.
	if sigBytes[64] >= 27 {
		sigBytes[64] -= 27
	}

	// Hash with EIP-191 personal_sign prefix.
	hash := accounts.TextHash([]byte(message))

	// Recover public key from signature.
	pubKey, err := crypto.SigToPub(hash, sigBytes)
	if err != nil {
		return "", fmt.Errorf("recovering public key: %w", err)
	}

	recoveredAddr := crypto.PubkeyToAddress(*pubKey).Hex()

	// Verify recovered address matches the claimed address.
	if !strings.EqualFold(recoveredAddr, parsed.Address) {
		return "", fmt.Errorf("signer mismatch: recovered %s, claimed %s", recoveredAddr, parsed.Address)
	}

	return recoveredAddr, nil
}

// GenerateNonce returns a cryptographically random 16-byte hex nonce
// for use in SIWE messages. Panics if the CSPRNG fails, since a broken
// random source is an unrecoverable condition for an auth system.
func GenerateNonce() string {
	nonce, err := randomHex(16)
	if err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return nonce
}

// hexToBytes decodes a hex string, stripping an optional "0x" prefix.
func hexToBytes(s string) ([]byte, error) {
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}
