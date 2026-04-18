// Package envutil provides small helpers for reading environment variables
// in service main functions. It replaces the duplicated getEnv/mustGetEnv
// helpers that used to live in each cmd/*/main.go.
package envutil

import (
	"fmt"
	"os"
)

// Get returns the value of the named environment variable, or fallback
// when the variable is unset or empty.
func Get(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// MustGet returns the value of the named environment variable, or panics
// with a descriptive message when the variable is unset or empty.
// Intended for service startup where missing config is unrecoverable.
func MustGet(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required environment variable %s is not set", key))
	}
	return v
}
