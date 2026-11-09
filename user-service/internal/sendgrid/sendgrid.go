// AI Assistance Disclosure:
// Tool: Codex (model: GPT-6), date: 2026-11-08
// Scope: Replaced the Mailgun adapter with the user-authorized SendGrid Mail Send API adapter using the standard library.
// Author review: ZI YANG — reviewed and verified

// Package sendgrid delivers OTP messages through Twilio SendGrid's Mail Send API.
package sendgrid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	messagesURL     = "https://api.sendgrid.com/v3/mail/send"
	deliveryTimeout = 5 * time.Second
	otpSubject      = "CampusRun Email Verification"
)

// Client sends OTP messages through the recorded SendGrid delivery contract.
type Client struct {
	apiKey     string
	sender     string
	endpoint   string
	httpClient *http.Client
}

// NewClient constructs a SendGrid client with the recorded five-second timeout.
func NewClient(apiKey, sender string) (*Client, error) {
	return newClient(apiKey, sender, messagesURL, &http.Client{Timeout: deliveryTimeout})
}

func newClient(apiKey, sender, endpoint string, httpClient *http.Client) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("SendGrid API key is required")
	}
	if strings.TrimSpace(sender) == "" {
		return nil, errors.New("SendGrid sender email is required")
	}
	if httpClient == nil {
		return nil, errors.New("SendGrid HTTP client is required")
	}
	return &Client{apiKey: apiKey, sender: sender, endpoint: endpoint, httpClient: httpClient}, nil
}

type address struct {
	Email string `json:"email"`
}

type personalization struct {
	To []address `json:"to"`
}

type content struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type message struct {
	Personalizations []personalization `json:"personalizations"`
	From             address           `json:"from"`
	Subject          string            `json:"subject"`
	Content          []content         `json:"content"`
}

// DeliverOTP submits plaintext and HTML OTP content; acceptance does not confirm inbox delivery.
func (c *Client) DeliverOTP(ctx context.Context, recipient, otp string) error {
	if c == nil {
		return errors.New("SendGrid client is required")
	}
	text := "Your CampusRun verification code is: " + otp + ". This code expires in 5 minutes."
	payload := message{
		Personalizations: []personalization{{To: []address{{Email: recipient}}}},
		From:             address{Email: c.sender},
		Subject:          otpSubject,
		Content: []content{
			{Type: "text/plain", Value: text},
			{Type: "text/html", Value: "<p>" + text + "</p>"},
		},
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return fmt.Errorf("encode SendGrid message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, &body)
	if err != nil {
		return fmt.Errorf("create SendGrid request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send SendGrid message: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		// Provider error bodies may contain the recipient or message; expose only the status.
		return fmt.Errorf("SendGrid message response: HTTP %d", response.StatusCode)
	}
	return nil
}
