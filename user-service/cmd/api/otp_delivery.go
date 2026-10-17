// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-17
// Scope: OTP_DELIVERY selection at startup, and the log deliverer for local runs without a Mailgun account.
// Author review: James Teo — reviewed and verified.

package main

import (
	"context"
	"fmt"
	"log/slog"

	"foc/user-service/internal/config"
	"foc/user-service/internal/mailgun"
	"foc/user-service/internal/user"
)

// The values OTP_DELIVERY accepts.
const (
	otpDeliveryMailgun = "mailgun"
	otpDeliveryLog     = "log"
)

// newOTPDeliverer returns the OTP delivery that OTP_DELIVERY selects: Mailgun
// when it is empty or "mailgun", the service log when it is "log". Any other
// value is a startup error, so a typo cannot silently stop codes being emailed.
func newOTPDeliverer(cfg config.Config, logger *slog.Logger) (user.OTPDeliverer, error) {
	switch cfg.OTPDelivery {
	case "", otpDeliveryMailgun:
		client, err := mailgun.NewClient(cfg.MailgunAPIKey, cfg.MailgunDomain, cfg.MailgunSenderEmail)
		if err != nil {
			return nil, fmt.Errorf("create Mailgun client: %w", err)
		}
		return client, nil
	case otpDeliveryLog:
		logger.Warn("OTP_DELIVERY=log: verification codes are written to this log instead of being emailed; local development only")
		return logOTPDeliverer{logger: logger}, nil
	default:
		return nil, fmt.Errorf("OTP_DELIVERY %q is not supported; use %q or %q", cfg.OTPDelivery, otpDeliveryMailgun, otpDeliveryLog)
	}
}

// logOTPDeliverer writes each OTP to the service log instead of emailing it.
// Anyone who can read the log can verify any account, so it is for local
// development only.
type logOTPDeliverer struct{ logger *slog.Logger }

// DeliverOTP logs the code at WARN so it stands out from routine output.
func (d logOTPDeliverer) DeliverOTP(ctx context.Context, email, otp string) error {
	d.logger.WarnContext(ctx, "verification code (OTP_DELIVERY=log, local development only)", "email", email, "otp", otp)
	return nil
}
