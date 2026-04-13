package eth

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// safeSetupABI is the ABI for Safe.setup(), used by abi.Pack to correctly
// encode the calldata including dynamic types and offsets.
const safeSetupABI = `[{
	"type": "function",
	"name": "setup",
	"inputs": [
		{"name": "_owners",         "type": "address[]"},
		{"name": "_threshold",      "type": "uint256"},
		{"name": "to",              "type": "address"},
		{"name": "data",            "type": "bytes"},
		{"name": "fallbackHandler", "type": "address"},
		{"name": "paymentToken",    "type": "address"},
		{"name": "payment",         "type": "uint256"},
		{"name": "paymentReceiver", "type": "address"}
	]
}]`

// parsedSetupABI is parsed once at init to avoid repeated JSON parsing.
var parsedSetupABI abi.ABI

func init() {
	var err error
	parsedSetupABI, err = abi.JSON(strings.NewReader(safeSetupABI))
	if err != nil {
		panic(fmt.Sprintf("parsing safe setup ABI: %v", err))
	}
}

// proxyCreationCodeBytes is the standard GnosisSafeProxy creation bytecode
// for Safe v1.3.0. This is immutable — identical on every chain. Obtained
// by calling proxyCreationCode() on the deployed ProxyFactory contract.
var proxyCreationCodeBytes = common.FromHex(
	"608060405234801561001057600080fd5b506040516101e73803806101e78339" +
		"818101604052602081101561003357600080fd5b8101908080519060200190" +
		"929190505050600073ffffffffffffffffffffffffffffffffffffffff168173" +
		"ffffffffffffffffffffffffffffffffffffffff1614156100ca576040517f08" +
		"c379a000000000000000000000000000000000000000000000000000000000" +
		"81526004018080602001828103825260228152602001806101c56022913960" +
		"400191505060405180910390fd5b806000806101000a81548173ffffffffffff" +
		"ffffffffffffffffffffffffffff021916908373ffffffffffffffffffffff" +
		"ffffffffffffffffff16021790555050610076806101006000396000f3fe60" +
		"80604052366000803760008036600073ffffffffffffffffffffffffffffffff" +
		"ffffff60005416617530fa3d6000803e60003d9160005114601c57f35bfd" +
		"fea265627a7a72315820d8a00dc4fe6bf675a9d7416fc2d00bb3433362aa" +
		"8186b750f76c4027269667ff64736f6c634300050e0032496e76616c6964" +
		"206d617374657220636f707920616464726573732070726f76696465640000",
)

// SafeConfig holds the parameters for deterministic Safe address derivation.
// These values are deployment-specific (different for Polygon mainnet vs
// testnet vs local Anvil fork).
type SafeConfig struct {
	FactoryAddress   common.Address // Gnosis Safe ProxyFactory contract.
	SingletonAddress common.Address // Gnosis Safe singleton (mastercopy).
	FallbackHandler  common.Address // Default fallback handler.
}

// DeriveSafeAddress computes the deterministic CREATE2 address for a Gnosis
// Safe proxy deployed via the ProxyFactory for the given owner EOA.
// This does NOT deploy the Safe on-chain — it only computes the address.
//
// The derivation mirrors how Polymarket computes wallet addresses:
//
//	address = CREATE2(factory, salt, keccak256(initCode))
//
// where salt = keccak256(keccak256(initializer) ++ saltNonce) and initCode
// is the minimal proxy creation code pointing at the singleton.
func DeriveSafeAddress(cfg SafeConfig, owner common.Address) common.Address {
	initializer := buildSetupCalldata(cfg, owner)

	// Salt = keccak256(keccak256(initializer) ++ uint256(saltNonce))
	// saltNonce is 0 for the first Safe per owner.
	initializerHash := crypto.Keccak256(initializer)
	saltNonce := common.LeftPadBytes(big.NewInt(0).Bytes(), 32)
	var salt [32]byte
	copy(salt[:], crypto.Keccak256(append(initializerHash, saltNonce...)))

	// Init code = proxy creation code ++ abi-encoded singleton address.
	singletonPadded := common.LeftPadBytes(cfg.SingletonAddress.Bytes(), 32)
	initCode := make([]byte, 0, len(proxyCreationCodeBytes)+len(singletonPadded))
	initCode = append(initCode, proxyCreationCodeBytes...)
	initCode = append(initCode, singletonPadded...)

	return crypto.CreateAddress2(cfg.FactoryAddress, salt, crypto.Keccak256(initCode))
}

// buildSetupCalldata ABI-encodes the Safe.setup() call for a single-owner
// Safe with threshold=1 using go-ethereum's abi package.
func buildSetupCalldata(cfg SafeConfig, owner common.Address) []byte {
	data, err := parsedSetupABI.Pack("setup",
		[]common.Address{owner}, // owners
		big.NewInt(1),           // threshold
		common.Address{},        // to (no delegate call)
		[]byte{},                // data (no delegate call data)
		cfg.FallbackHandler,     // fallbackHandler
		common.Address{},        // paymentToken (ETH)
		big.NewInt(0),           // payment (none)
		common.Address{},        // paymentReceiver (none)
	)
	if err != nil {
		// ABI encoding with known types cannot fail at runtime.
		panic(fmt.Sprintf("encoding setup calldata: %v", err))
	}
	return data
}
