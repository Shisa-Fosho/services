package eth

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func testSafeConfig() SafeConfig {
	return SafeConfig{
		FactoryAddress:   common.HexToAddress("0xa6B71E26C5e0845f74c812102Ca7114b6a896AB2"),
		SingletonAddress: common.HexToAddress("0xd9Db270c1B5E3Bd161E8c8503c55cEABeE709552"),
		FallbackHandler:  common.HexToAddress("0xf48f2B2d2a534e402487b3ee7C18c33Aec0Fe5e4"),
	}
}

func TestDeriveSafeAddress_Deterministic(t *testing.T) {
	t.Parallel()

	cfg := testSafeConfig()
	owner := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	addr1 := DeriveSafeAddress(cfg, owner)
	addr2 := DeriveSafeAddress(cfg, owner)

	if addr1 != addr2 {
		t.Errorf("non-deterministic: %s != %s", addr1.Hex(), addr2.Hex())
	}
	if addr1 == (common.Address{}) {
		t.Error("derived zero address")
	}
}

func TestDeriveSafeAddress_DifferentOwners(t *testing.T) {
	t.Parallel()

	cfg := testSafeConfig()
	owner1 := common.HexToAddress("0x1111111111111111111111111111111111111111")
	owner2 := common.HexToAddress("0x2222222222222222222222222222222222222222")

	addr1 := DeriveSafeAddress(cfg, owner1)
	addr2 := DeriveSafeAddress(cfg, owner2)

	if addr1 == addr2 {
		t.Errorf("different owners produced same address: %s", addr1.Hex())
	}
}

func TestDeriveSafeAddress_DifferentConfigs(t *testing.T) {
	t.Parallel()

	owner := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	cfg1 := testSafeConfig()
	cfg2 := testSafeConfig()
	cfg2.FactoryAddress = common.HexToAddress("0x4e1DCf7AD4e460CfD30791CCC4F9c8a4f820ec67")

	addr1 := DeriveSafeAddress(cfg1, owner)
	addr2 := DeriveSafeAddress(cfg2, owner)

	if addr1 == addr2 {
		t.Errorf("different factories produced same address: %s", addr1.Hex())
	}
}

func TestDeriveSafeAddress_ValidAddress(t *testing.T) {
	t.Parallel()

	cfg := testSafeConfig()
	owner := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	addr := DeriveSafeAddress(cfg, owner)
	if !IsValidAddress(addr.Hex()) {
		t.Errorf("derived address is not valid: %s", addr.Hex())
	}
}
