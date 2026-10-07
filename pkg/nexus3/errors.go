package nexus3

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

// maxErrorBody caps how many bytes of a response body an APIError retains.
const maxErrorBody = 512

// APIError is returned (wrapped; find it with errors.As) when the Nexus
// server answers a request with a non-2xx HTTP status. The generated client's
// own error only carries the status text (e.g. "403 Forbidden"); APIError
// additionally preserves the server's response body, which usually holds the
// real reason (e.g. "Repository does not allow updating assets").
type APIError struct {
	// StatusCode is the HTTP status code of the response.
	StatusCode int
	// Status is the HTTP status line text, e.g. "403 Forbidden".
	Status string
	// Body is the trimmed response body, truncated to 512 bytes.
	Body string

	err error
}

// Error implements the error interface. It includes the status and, when the
// server sent one, the response body.
func (e *APIError) Error() string {
	status := e.Status
	if status == "" {
		status = fmt.Sprintf("%d %s", e.StatusCode, http.StatusText(e.StatusCode))
	}
	if e.Body == "" {
		return status
	}
	return status + ": " + e.Body
}

// Unwrap returns the original error from the generated client, typically a
// *v3.GenericOpenAPIError.
func (e *APIError) Unwrap() error { return e.err }

// truncateBody trims b and caps it to maxErrorBody bytes on a rune boundary.
func truncateBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= maxErrorBody {
		return s
	}
	s = s[:maxErrorBody]
	for s != "" && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

// wrapAPIError is the single place every wrapper routes its error through.
// It returns "nexus3: <op>: <err>"; when the failure was an HTTP error
// response, err is first converted to an *APIError carrying the status code
// and response body. Non-HTTP errors (network, file I/O) are wrapped as-is.
func wrapAPIError(op string, resp *http.Response, err error) error {
	if err == nil {
		return nil
	}
	if resp != nil && resp.StatusCode >= 300 {
		var (
			ptr *v3.GenericOpenAPIError
			val v3.GenericOpenAPIError
			raw []byte
			ok  bool
		)
		switch {
		case errors.As(err, &ptr) && ptr != nil:
			raw, ok = ptr.Body(), true
		case errors.As(err, &val):
			raw, ok = val.Body(), true
		}
		if ok {
			err = &APIError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				Body:       truncateBody(raw),
				err:        err,
			}
		}
	}
	return fmt.Errorf("nexus3: %s: %w", op, err)
}
