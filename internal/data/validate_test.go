package data

import (
	"errors"
	"testing"
)

func validUser() *User {
	return &User{
		Address:      "0x1234567890abcdef1234567890abcdef12345678",
		Username:     "testuser",
		SignupMethod: SignupMethodWallet,
	}
}

func TestValidateUser(t *testing.T) {
	t.Parallel()

	email := "test@example.com"

	tests := []struct {
		name    string
		modify  func(u *User)
		wantErr bool
	}{
		{
			name:    "valid wallet user passes",
			modify:  func(u *User) {},
			wantErr: false,
		},
		{
			name: "valid email user passes",
			modify: func(u *User) {
				u.SignupMethod = SignupMethodEmail
				u.Email = &email
			},
			wantErr: false,
		},
		{
			name:    "empty address",
			modify:  func(u *User) { u.Address = "" },
			wantErr: true,
		},
		{
			name:    "address without 0x prefix",
			modify:  func(u *User) { u.Address = "1234567890abcdef1234567890abcdef12345678" },
			wantErr: true,
		},
		{
			name:    "address too short",
			modify:  func(u *User) { u.Address = "0x1234" },
			wantErr: true,
		},
		{
			name:    "address too long",
			modify:  func(u *User) { u.Address = "0x1234567890abcdef1234567890abcdef123456789" },
			wantErr: true,
		},
		{
			name:    "empty username",
			modify:  func(u *User) { u.Username = "" },
			wantErr: true,
		},
		{
			name:    "invalid signup method",
			modify:  func(u *User) { u.SignupMethod = SignupMethod(99) },
			wantErr: true,
		},
		{
			name: "email signup without email",
			modify: func(u *User) {
				u.SignupMethod = SignupMethodEmail
				u.Email = nil
			},
			wantErr: true,
		},
		{
			name: "email signup with empty email",
			modify: func(u *User) {
				u.SignupMethod = SignupMethodEmail
				empty := ""
				u.Email = &empty
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := validUser()
			tt.modify(u)
			err := ValidateUser(u)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidUser) {
				t.Errorf("expected ErrInvalidUser, got: %v", err)
			}
		})
	}
}
