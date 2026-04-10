package data

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// ValidateUser checks that a user meets all creation rules.
// Returns ErrInvalidUser wrapping a descriptive message on failure.
func ValidateUser(user *User) error {
	if len(user.Address) != 42 || !common.IsHexAddress(user.Address) {
		return fmt.Errorf("invalid ethereum address %q: %w", user.Address, ErrInvalidUser)
	}

	if user.Username == "" {
		return fmt.Errorf("username is required: %w", ErrInvalidUser)
	}

	if !user.SignupMethod.IsValid() {
		return fmt.Errorf("invalid signup method %d: %w", user.SignupMethod, ErrInvalidUser)
	}

	if user.SignupMethod == SignupMethodEmail && (user.Email == nil || *user.Email == "") {
		return fmt.Errorf("email is required for email signup: %w", ErrInvalidUser)
	}

	return nil
}
