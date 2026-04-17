// Package eth provides Ethereum-related utilities shared across domain packages.
package eth

import "github.com/ethereum/go-ethereum/common"

// IsValidAddress returns true if addr is a valid 0x-prefixed Ethereum address.
func IsValidAddress(addr string) bool {
	return len(addr) == 42 && common.IsHexAddress(addr)
}
