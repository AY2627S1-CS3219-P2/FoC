// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-17
// Scope: Tests for OTP_DELIVERY selection and the log deliverer.
// Author review: James Teo — reviewed and verified.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"foc/user-service/internal/config"
	"foc/user-service/internal/mailgun"
)

func TestNewOTPDeliverer(t *testing.T) {
	mailgunSettings := config.Config{MailgunAPIKey: "key", MailgunDomain: "mg.example.test", MailgunSenderEmail: "verify@example.test"}
	withDelivery := func(cfg config.Config, mode string) config.Config {
		cfg.OTPDelivery = mode
		return cfg
	}
	tests := map[string]struct {
		cfg         config.Config
		wantMailgun bool
		wantLog     bool
	}{
		"defaults to Mailgun":                {cfg: mailgunSettings, wantMailgun: true},
		"selects Mailgun by name":            {cfg: withDelivery(mailgunSettings, "mailgun"), wantMailgun: true},
		"default Mailgun needs its settings": {cfg: config.Config{}},
		"named Mailgun needs its settings":   {cfg: withDelivery(config.Config{}, "mailgun")},
		"log needs no Mailgun settings":      {cfg: withDelivery(config.Config{}, "log"), wantLog: true},
		"rejects an unknown mode":            {cfg: withDelivery(mailgunSettings, "smtp")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			deliverer, err := newOTPDeliverer(tt.cfg, slog.New(slog.NewJSONHandler(&logs, nil)))
			if wantErr := !tt.wantMailgun && !tt.wantLog; (err != nil) != wantErr {
				t.Fatalf("newOTPDeliverer() error = %v, want error %t", err, wantErr)
			}
			if _, ok := deliverer.(*mailgun.Client); ok != tt.wantMailgun {
				t.Fatalf("deliverer = %T, want Mailgun client %t", deliverer, tt.wantMailgun)
			}
			if _, ok := deliverer.(logOTPDeliverer); ok != tt.wantLog {
				t.Fatalf("deliverer = %T, want log deliverer %t", deliverer, tt.wantLog)
			}
			if warned := strings.Contains(logs.String(), `"level":"WARN"`); warned != tt.wantLog {
				t.Fatalf("startup WARN logged = %t, want %t; logs = %s", warned, tt.wantLog, logs.String())
			}
		})
	}
}

func TestLogOTPDelivererWritesCodeAtWarn(t *testing.T) {
	var logs bytes.Buffer
	deliverer := logOTPDeliverer{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	if err := deliverer.DeliverOTP(context.Background(), "student@u.nus.edu", "123456"); err != nil {
		t.Fatal(err)
	}
	var entry struct {
		Level string `json:"level"`
		Msg   string `json:"msg"`
		Email string `json:"email"`
		OTP   string `json:"otp"`
	}
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry %q: %v", logs.String(), err)
	}
	if entry.Level != "WARN" || entry.Email != "student@u.nus.edu" || entry.OTP != "123456" || !strings.Contains(entry.Msg, "local development only") {
		t.Fatalf("log entry = %+v, want a WARN with the email, the code and a local-development note", entry)
	}
}
