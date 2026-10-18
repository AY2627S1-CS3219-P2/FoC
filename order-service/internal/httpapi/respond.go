// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: JSON responses, the {"error": ...} body and strict request-body
//   decoding for order-service's HTTP API (api/openapi.yaml, D-053).
// Author review: James Teo — reviewed and verified.

package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"foc/order-service/internal/gen"
)

// Messages the transport itself sends; every other error message comes from
// order.MessageOf.
const (
	msgInternal        = "internal error"
	msgUnauthenticated = "missing, invalid or expired access token"
	msgKeysUnavailable = "sign-in cannot be checked right now; please try again"
	msgBodyTooLarge    = "request body too large"
)

// writeJSON sends v as the JSON body of a status response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		// Unreachable for the generated types this package sends; answer
		// rather than leave a half-written response.
		status, body = http.StatusInternalServerError, []byte(`{"error":"`+msgInternal+`"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

// writeError sends the contract's error body, {"error": msg}.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, gen.Error{Error: msg})
}

// decodeBody decodes r's body into dst. The body must be exactly one JSON
// value with no field the contract does not name; otherwise decodeBody answers
// 400, or 413 when the body is over maxBodyBytes, and returns false.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		status, msg := decodeFailure(err)
		writeError(w, status, msg)
		return false
	}
	switch err := dec.Decode(&struct{}{}); {
	case errors.Is(err, io.EOF):
		return true
	case isTooLarge(err):
		writeError(w, http.StatusRequestEntityTooLarge, msgBodyTooLarge)
	default:
		writeError(w, http.StatusBadRequest, "the request body must be a single JSON object")
	}
	return false
}

// decodeFailure picks the status and message for a body that did not decode.
func decodeFailure(err error) (int, string) {
	var (
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
	)
	switch {
	case isTooLarge(err):
		return http.StatusRequestEntityTooLarge, msgBodyTooLarge
	case errors.Is(err, io.EOF):
		return http.StatusBadRequest, "a JSON request body is required"
	case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
		return http.StatusBadRequest, "the request body is not valid JSON"
	case errors.As(err, &typeErr):
		if typeErr.Field == "" {
			return http.StatusBadRequest, "the request body must be a JSON object"
		}
		return http.StatusBadRequest, typeErr.Field + " has the wrong type"
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		// encoding/json has no typed error for DisallowUnknownFields.
		return http.StatusBadRequest, strings.TrimPrefix(err.Error(), "json: ")
	default:
		// A value whose own decoding failed, such as a supplierId that is not
		// a UUID.
		return http.StatusBadRequest, "the request body has an invalid value: " + err.Error()
	}
}

func isTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}
