package eth

import "math/rand"

// testAddresses is the pool of pre-verified valid Ethereum addresses that
// TestAddress draws from. Expand this pool if test suites need more
// collision resistance.
var testAddresses = []string{
	"0x1111111111111111111111111111111111111111",
	"0x2222222222222222222222222222222222222222",
	"0x3333333333333333333333333333333333333333",
	"0x4444444444444444444444444444444444444444",
	"0x5555555555555555555555555555555555555555",
	"0x6666666666666666666666666666666666666666",
	"0x7777777777777777777777777777777777777777",
	"0x8888888888888888888888888888888888888888",
	"0x9999999999999999999999999999999999999999",
	"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	"0xcccccccccccccccccccccccccccccccccccccccc",
	"0xdddddddddddddddddddddddddddddddddddddddd",
	"0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	"0xffffffffffffffffffffffffffffffffffffffff",
}

// TestAddress returns a random valid Ethereum address from a pre-verified
// pool. Use this for test fixtures where any valid address will do instead
// of handwriting 40-hex-char strings (which is too easy to miscount).
//
// Successive calls may return the same address. If a test needs N distinct
// addresses (e.g., referrer and referred), call this in a loop and retry on
// collision, or add more entries to testAddresses.
func TestAddress() string {
	return testAddresses[rand.Intn(len(testAddresses))]
}
