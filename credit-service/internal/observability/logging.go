// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: The sanitized error summary a log line carries in place of a raw
//   error, moved here from internal/httpapi and internal/messaging: URL
//   credentials removed whatever the password holds, and the text bounded
//   (D-135).
// Author review: James Teo — reviewed and verified.

// Package observability makes what credit-service logs safe to log.
package observability

import (
	"regexp"
	"strings"
)

// maxLoggedErrorBytes bounds an error in a log line, as sourceOfTruth.md bounds
// a stored last_error.
const maxLoggedErrorBytes = 512

var (
	// rejectedURL is the URL a url.Parse error quotes. Parse quotes it only up
	// to its first '#', which may lie inside the password and leave no '@' to
	// stop at, so all of it after the scheme goes.
	rejectedURL = regexp.MustCompile(`(parse "[A-Za-z][A-Za-z0-9+.-]*://)(?:[^"\\]|\\.)*"`)
	// userinfo runs to the URL's last '@', so a password holding '/', ':' or
	// '#' goes whole. A URL quoted with %q ends at its closing quote, one in
	// backticks, as pgx quotes it, at its closing backtick, and any other at
	// whitespace.
	userinfo = regexp.MustCompile(`("[A-Za-z][A-Za-z0-9+.-]*://)(?:[^"\\]|\\.)*@` +
		"|(`[A-Za-z][A-Za-z0-9+.-]*://)[^`]*@" +
		`|([A-Za-z][A-Za-z0-9+.-]*://)\S*@`)
	// rejectedPart is what net/url quotes back from a URL it rejected as a bad
	// port or escape, which can be a piece of the password.
	rejectedPart = regexp.MustCompile(`(invalid port |invalid URL escape )"(?:[^"\\]|\\.)*"`)
)

// Sanitize returns the summary of an error's text that a log line may carry. A
// vendor error can quote a connection string, so its credentials are removed
// before the text is cut to maxLoggedErrorBytes, which then cannot leave a URL
// cut short of its '@'.
func Sanitize(s string) string {
	s = rejectedURL.ReplaceAllString(s, `${1}REDACTED"`)
	s = userinfo.ReplaceAllString(s, "${1}${2}${3}REDACTED@")
	s = rejectedPart.ReplaceAllString(s, `${1}"REDACTED"`)
	if len(s) > maxLoggedErrorBytes {
		s = strings.ToValidUTF8(s[:maxLoggedErrorBytes], "")
	}
	return s
}
