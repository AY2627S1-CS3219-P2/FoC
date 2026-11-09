// AI Assistance Disclosure:
// Tool: Codex (model: GPT-6), date: 2026-11-08
// Scope: SendGrid request, configuration, cancellation, and failure/no-retry behavior tests.
// Author review: ZI YANG — reviewed and verified

package sendgrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDeliverOTPSendsSendGridMessage(t *testing.T) {
	requests := 0
	ctx := context.WithValue(context.Background(), contextKey{}, "caller")
	client, err := newClient("test-key", "verify@example.test", messagesURL, &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != http.MethodPost || r.URL.String() != "https://api.sendgrid.com/v3/mail/send" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("missing SendGrid bearer authentication or JSON content type")
		}
		if r.Context().Value(contextKey{}) != "caller" {
			t.Fatal("caller context not propagated")
		}
		// Decode independently of the adapter's structs so JSON field spelling is exercised.
		var payload struct {
			Personalizations []struct {
				To []struct {
					Email string `json:"email"`
				} `json:"to"`
			} `json:"personalizations"`
			From struct {
				Email string `json:"email"`
			} `json:"from"`
			Subject string `json:"subject"`
			Content []struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Personalizations) != 1 || len(payload.Personalizations[0].To) != 1 || payload.Personalizations[0].To[0].Email != "student@u.nus.edu" || payload.From.Email != "verify@example.test" || payload.Subject != "CampusRun Email Verification" {
			t.Fatalf("unexpected message addressing: %+v", payload)
		}
		text := "Your CampusRun verification code is: 123456. This code expires in 5 minutes."
		if len(payload.Content) != 2 || payload.Content[0].Type != "text/plain" || payload.Content[0].Value != text || payload.Content[1].Type != "text/html" || payload.Content[1].Value != "<p>"+text+"</p>" {
			t.Fatalf("unexpected OTP content: %+v", payload.Content)
		}
		return response(http.StatusAccepted), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DeliverOTP(ctx, "student@u.nus.edu", "123456"); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

type contextKey struct{}

func TestDeliverOTPRejectsOtherStatusesWithoutRetryOrSensitiveErrorDetails(t *testing.T) {
	for _, status := range []int{200, 400, 401, 403, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			requests := 0
			client, err := newClient("secret-key", "verify@example.test", messagesURL, &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
				requests++
				r := response(status)
				r.Body = io.NopCloser(strings.NewReader("secret-key student@u.nus.edu 123456"))
				return r, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			err = client.DeliverOTP(context.Background(), "student@u.nus.edu", "123456")
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) {
				t.Fatalf("error = %v, want HTTP %d", err, status)
			}
			for _, secret := range []string{"secret-key", "student@u.nus.edu", "123456"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("sensitive value exposed in error: %v", err)
				}
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want no retry", requests)
			}
		})
	}
}

func TestDeliverOTPWrapsContextErrorsWithoutRetry(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			requests := 0
			client, err := newClient("key", "verify@example.test", messagesURL, &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
				requests++
				return nil, cause
			})})
			if err != nil {
				t.Fatal(err)
			}
			if err := client.DeliverOTP(context.Background(), "student@u.nus.edu", "123456"); !errors.Is(err, cause) {
				t.Fatalf("error = %v, want wrapped %v", err, cause)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want no retry", requests)
			}
		})
	}
}

func TestNewClientConfiguration(t *testing.T) {
	client, err := NewClient("key", "verify@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient.Timeout != 5*time.Second {
		t.Fatalf("timeout = %s, want 5s", client.httpClient.Timeout)
	}
	for name, cfg := range map[string]struct{ key, sender string }{
		"missing key":    {sender: "verify@example.test"},
		"missing sender": {key: "key"},
		"blank key":      {key: "  ", sender: "verify@example.test"},
		"blank sender":   {key: "key", sender: "  "},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewClient(cfg.key, cfg.sender); err == nil {
				t.Fatal("NewClient succeeded without required configuration")
			}
		})
	}
	if _, err := newClient("key", "verify@example.test", messagesURL, nil); err == nil {
		t.Fatal("NewClient succeeded with nil HTTP client")
	}
}

func TestNilClientReturnsError(t *testing.T) {
	var client *Client
	if err := client.DeliverOTP(context.Background(), "student@u.nus.edu", "123456"); err == nil {
		t.Fatal("nil client returned no error")
	}
}

func response(status int) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (fn roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
