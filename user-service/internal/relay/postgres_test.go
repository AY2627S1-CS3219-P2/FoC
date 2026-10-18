// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: Unit tests of how a failed publish's error text is made fit for the
//   outbox_events last_error column, adapted from order-service/internal/relay.
// Author review: James Teo — reviewed and verified.

package relay

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLastError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "short text is kept", err: errors.New("connection refused"), want: "connection refused"},
		{
			name: "text at the limit is kept",
			err:  errors.New(strings.Repeat("a", maxLastError)),
			want: strings.Repeat("a", maxLastError),
		},
		{
			name: "longer text is cut to the limit",
			err:  errors.New(strings.Repeat("a", maxLastError+100)),
			want: strings.Repeat("a", maxLastError),
		},
		{
			name: "the limit counts characters, not bytes",
			err:  errors.New(strings.Repeat("é", maxLastError+1)),
			want: strings.Repeat("é", maxLastError),
		},
		{name: "invalid UTF-8 is replaced", err: errors.New("bad \xff byte"), want: "bad � byte"},
		{name: "NUL bytes are dropped", err: errors.New("nul\x00 byte"), want: "nul byte"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lastError(tc.err)
			if got != tc.want {
				t.Errorf("lastError() = %q, want %q", got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("lastError() = %q is not valid UTF-8", got)
			}
			if n := utf8.RuneCountInString(got); n > maxLastError {
				t.Errorf("lastError() has %d characters, want at most %d", n, maxLastError)
			}
		})
	}
}
