// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Tests that an error summary carries no URL credentials, whatever the
//   password holds and however url.Parse quotes it, and is cut to 512 bytes
//   without splitting a character (D-135).
// Author review: James Teo — reviewed and verified.

package observability_test

import (
	"net/url"
	"strings"
	"testing"

	"foc/credit-service/internal/observability"
)

func TestSanitize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "a plain password",
			in:   "dial postgres://credit:hunter2@credit-db:5432/credit: connection refused",
			want: "dial postgres://REDACTED@credit-db:5432/credit: connection refused",
		},
		{
			name: "a password holding '/', ':' and '#'",
			in:   "dial amqp://foc:s3/cr:et#99@rabbitmq:5672/foc: connection refused",
			want: "dial amqp://REDACTED@rabbitmq:5672/foc: connection refused",
		},
		{
			name: "a percent-encoded password",
			in:   "dial amqp://foc:s3%2Fcr%3Aet%2399@rabbitmq:5672/foc: connection refused",
			want: "dial amqp://REDACTED@rabbitmq:5672/foc: connection refused",
		},
		{
			// What amqp091-go's Dial returns for this URL: the reason quotes the
			// password's first piece back as a port.
			name: "a url.Parse error quoting a password that holds '/'",
			in:   `dial RabbitMQ: parse "amqp://foc:pa/ss@rabbitmq:5672/foc": invalid port ":pa" after host`,
			want: `dial RabbitMQ: parse "amqp://REDACTED": invalid port "REDACTED" after host`,
		},
		{
			name: "a url.Parse error cut short at a '#' in the password",
			in:   urlParseError("amqp://foc:s3cr#et99@rabbitmq:5672/foc"),
			want: `parse "amqp://REDACTED": invalid port "REDACTED" after host`,
		},
		{
			name: "a url.Parse error quoting a bad escape in the password",
			in:   urlParseError("amqp://foc:s3cr%zzet99@rabbitmq:5672/foc"),
			want: `parse "amqp://REDACTED": invalid URL escape "REDACTED"`,
		},
		{
			name: "a url.Parse error quoting a password that holds whitespace and a quote",
			in:   urlParseError(`redis://default:s3 "cr@redis:6379/0`),
			want: `parse "redis://REDACTED": net/url: invalid userinfo`,
		},
		{
			name: "a quoted URL whose password holds whitespace",
			in:   `connect "amqp://foc:s3 cr@rabbitmq:5672/foc": refused`,
			want: `connect "amqp://REDACTED@rabbitmq:5672/foc": refused`,
		},
		{
			// pgx's own redaction keeps what precedes the password's last ':'.
			name: "a pgx error that kept part of the password",
			in:   "cannot parse `postgres://credit:s3cr:xxxxxx@credit-db:5432/credit`: failed to parse as URL (invalid port \":et99\" after host)",
			want: "cannot parse `postgres://REDACTED@credit-db:5432/credit`: failed to parse as URL (invalid port \"REDACTED\" after host)",
		},
		{
			// What main's fatal line logged for CREDIT_DB_URL=postgres://credit:my pass:word@credit-db:5432/credit.
			name: "a pgx error quoting a password that holds whitespace",
			in:   "create database pool: cannot parse `postgres://credit:my pass:xxxxxx@credit-db:5432/credit`: failed to parse as URL (net/url: invalid userinfo)",
			want: "create database pool: cannot parse `postgres://REDACTED@credit-db:5432/credit`: failed to parse as URL (net/url: invalid userinfo)",
		},
		{
			name: "a backtick-quoted URL whose password holds whitespace and '@'",
			in:   "dial `amqp://foc:s3 c@r@rabbitmq:5672/foc`: connection refused",
			want: "dial `amqp://REDACTED@rabbitmq:5672/foc`: connection refused",
		},
		{
			name: "text with no URL is left unchanged",
			in:   "read revocation keys: dial tcp 172.18.0.4:6379: connect: connection refused",
			want: "read revocation keys: dial tcp 172.18.0.4:6379: connect: connection refused",
		},
		{
			name: "a URL without credentials is left unchanged",
			in:   `Get "http://user-service:8081/.well-known/jwks.json": dial tcp: connection refused`,
			want: `Get "http://user-service:8081/.well-known/jwks.json": dial tcp: connection refused`,
		},
		{
			name: "a long error is cut to 512 bytes",
			in:   strings.Repeat("x", 600),
			want: strings.Repeat("x", 512),
		},
		{
			name: "the cut never splits a character",
			in:   strings.Repeat("x", 511) + "é",
			want: strings.Repeat("x", 511),
		},
		{
			name: "credentials past the cut are removed before it",
			in:   strings.Repeat("x", 500) + " amqp://foc:hunter2@rabbitmq:5672/foc",
			want: strings.Repeat("x", 500) + " amqp://REDA",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := observability.Sanitize(tc.in); got != tc.want {
				t.Errorf("Sanitize(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

// urlParseError is the text of the error url.Parse returns for raw, which
// amqp091-go's Dial and go-redis's ParseURL both return as it is.
func urlParseError(raw string) string {
	if _, err := url.Parse(raw); err != nil {
		return err.Error()
	}
	return "url.Parse accepted " + raw
}
