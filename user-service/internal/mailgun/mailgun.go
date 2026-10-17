// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-10-10
// Scope: Added the owner-recorded Mailgun Messages API adapter for OTP email delivery.
// Author review: PENDING.

// Package mailgun delivers OTP messages through Mailgun's Messages API.
package mailgun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	messagesBaseURL = "https://api.mailgun.net/v3"
	deliveryTimeout = 5 * time.Second
	otpSubject      = "CampusRun Email Verification"
)

// Client sends owner-recorded OTP messages through the Mailgun Messages API.
type Client struct {
	apiKey     string
	domain     string
	sender     string
	baseURL    string
	httpClient *http.Client
}

// NewClient constructs a Mailgun client with the recorded five-second timeout.
func NewClient(apiKey, domain, sender string) (*Client, error) {
	return newClient(apiKey, domain, sender, messagesBaseURL, &http.Client{Timeout: deliveryTimeout})
}

func newClient(apiKey, domain, sender, baseURL string, httpClient *http.Client) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("Mailgun API key is required")
	}
	if strings.TrimSpace(domain) == "" {
		return nil, errors.New("Mailgun domain is required")
	}
	if strings.TrimSpace(sender) == "" {
		return nil, errors.New("Mailgun sender email is required")
	}
	if httpClient == nil {
		return nil, errors.New("Mailgun HTTP client is required")
	}
	return &Client{apiKey: apiKey, domain: domain, sender: sender, baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}, nil
}

// DeliverOTP sends the recorded plaintext and HTML verification message.
func (c *Client) DeliverOTP(ctx context.Context, recipient, otp string) error {
	if c == nil {
		return errors.New("Mailgun client is required")
	}
	values := url.Values{
		"from":    {c.sender},
		"to":      {recipient},
		"subject": {otpSubject},
		"text":    {messageBody(otp)},
		"html":    {"<p>" + messageBody(otp) + "</p>"},
	}
	endpoint := c.baseURL + "/" + url.PathEscape(c.domain) + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("create Mailgun request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("api", c.apiKey)

	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send Mailgun message: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, response.Body)
		return fmt.Errorf("Mailgun message response: %s", response.Status)
	}
	return nil
}

func messageBody(otp string) string {
	return "Your CampusRun verification code is: " + otp + ". This code expires in 5 minutes."
}
