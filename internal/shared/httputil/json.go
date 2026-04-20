// Package httputil provides shared HTTP handler utilities: JSON encoding/decoding,
// standard error responses, and middleware for request ID, logging, and recovery.
package httputil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// maxBodySize is the maximum allowed request body size (1 MB).
const maxBodySize = 1 << 20

// DecodeJSON reads the request body as JSON into dst. It enforces a 1 MB size
// limit and rejects unknown fields. Returns an error suitable for direct use
// in an ErrorResponse if the body is empty, oversized, or malformed.
func DecodeJSON(request *http.Request, dst any) error {
	reader := http.MaxBytesReader(nil, request.Body, maxBodySize)
	defer func() { _ = reader.Close() }()

	dec := json.NewDecoder(reader)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return fmt.Errorf("request body too large")
		}
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("request body is empty")
		}
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// EncodeJSON writes data as a JSON response with the given HTTP status code.
// Sets Content-Type to application/json before writing the status header.
func EncodeJSON(writer http.ResponseWriter, status int, data any) error {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(data); err != nil {
		return fmt.Errorf("encoding JSON response: %w", err)
	}
	return nil
}

// errorEnvelope is the standard JSON error response shape.
type errorEnvelope struct {
	Error string `json:"error"`
}

// ErrorResponse writes a JSON error response with the given status code
// and message. Uses the standard {"error": "..."} envelope.
func ErrorResponse(writer http.ResponseWriter, status int, msg string) {
	_ = EncodeJSON(writer, status, errorEnvelope{Error: msg})
}
