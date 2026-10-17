// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-10-10
// Scope: Added behavioral tests for the owner-recorded Mailgun OTP request and failure handling.
// Author review: ZI YANG - validated correctness.

package mailgun

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestClientDeliverOTPSendsRecordedMailgunMessage(t *testing.T) {
	client, err := newClient("test-key", "example.test", "verify@example.test", "https://mailgun.test/v3", &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/v3/example.test/messages" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		username, password, ok := r.BasicAuth()
		if !ok || username != "api" || password != "test-key" {
			t.Fatal("missing recorded Mailgun basic authentication")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		if form.Get("from") != "verify@example.test" || form.Get("to") != "student@u.nus.edu" || form.Get("subject") != otpSubject {
			t.Fatalf("unexpected recipient fields: %#v", form)
		}
		wantText := messageBody("123456")
		if form.Get("text") != wantText || form.Get("html") != "<p>"+wantText+"</p>" {
			t.Fatalf("unexpected OTP message: %#v", form)
		}
		return &http.Response{StatusCode: http.StatusAccepted, Status: "202 Accepted", Body: io.NopCloser(strings.NewReader(""))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DeliverOTP(context.Background(), "student@u.nus.edu", "123456"); err != nil {
		t.Fatal(err)
	}
}

func TestClientDeliverOTPReturnsErrorForMailgunServerFailure(t *testing.T) {
	requests := 0
	client, err := newClient("test-key", "example.test", "verify@example.test", "https://mailgun.test/v3", &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Status: "503 Service Unavailable", Body: io.NopCloser(strings.NewReader(""))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DeliverOTP(context.Background(), "student@u.nus.edu", "123456"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("error = %v, want Mailgun 503 error", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want no automatic retry", requests)
	}
}

func TestClientDeliverOTPReturnsTimeoutError(t *testing.T) {
	client, err := newClient("test-key", "example.test", "verify@example.test", "https://mailgun.test/v3", &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DeliverOTP(context.Background(), "student@u.nus.edu", "123456"); err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("error = %v, want timeout error", err)
	}
}

func TestNewClientUsesRecordedTimeout(t *testing.T) {
	client, err := NewClient("test-key", "example.test", "verify@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient.Timeout != deliveryTimeout {
		t.Fatalf("timeout = %s, want %s", client.httpClient.Timeout, deliveryTimeout)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (fn roundTripper) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestNewClientRequiresRecordedConfiguration(t *testing.T) {
	for name, testCase := range map[string]struct{ apiKey, domain, sender string }{
		"API key": {domain: "example.test", sender: "verify@example.test"},
		"domain":  {apiKey: "key", sender: "verify@example.test"},
		"sender":  {apiKey: "key", domain: "example.test"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewClient(testCase.apiKey, testCase.domain, testCase.sender); err == nil {
				t.Fatal("NewClient succeeded without required configuration")
			}
		})
	}
}
