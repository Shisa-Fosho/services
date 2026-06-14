// Package testassert provides shared assertion helpers for tests.
package testassert

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// BodyContains asserts that an HTTP test response body contains want.
func BodyContains(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	if !strings.Contains(recorder.Body.String(), want) {
		t.Errorf("body = %q, want substring %q", recorder.Body.String(), want)
	}
}
