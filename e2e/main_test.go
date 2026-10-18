//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The suite's entry point: reads the E2E_* settings and runs a
//   preflight that fails fast, naming what is missing, when the stack lacks a
//   piece README.md lists under Preflight.
// Author review: James Teo — reviewed and verified.

package e2e

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Defaults for the E2E_* settings.
const (
	defaultGatewayURL = "http://localhost:8080"
	// The shared local admin account in root AGENTS.md §11.
	defaultAdminUsername = "admindemo"
	defaultAdminPassword = "AdminDemo123"
	// The T and C that README.md starts the stack with (D-049).
	defaultExpiryPeriod  = 20 * time.Second
	defaultConfirmWindow = 20 * time.Second
)

// preflightTimeout bounds the whole preflight, including the wait for a new
// student's credit account.
const preflightTimeout = 45 * time.Second

// settings is what the suite reads from its environment.
type settings struct {
	gatewayURL    string
	adminUsername string
	adminPassword string
	// expiryPeriod and confirmWindow are the T and C the stack runs with.
	expiryPeriod  time.Duration
	confirmWindow time.Duration
}

func loadSettings() (settings, error) {
	cfg := settings{
		gatewayURL:    strings.TrimRight(envOr("E2E_GATEWAY_URL", defaultGatewayURL), "/"),
		adminUsername: envOr("E2E_ADMIN_USERNAME", defaultAdminUsername),
		adminPassword: envOr("E2E_ADMIN_PASSWORD", defaultAdminPassword),
	}
	var err error
	if cfg.expiryPeriod, err = durationEnv("E2E_EXPIRY_PERIOD", defaultExpiryPeriod); err != nil {
		return settings{}, err
	}
	if cfg.confirmWindow, err = durationEnv("E2E_CONFIRMATION_WINDOW", defaultConfirmWindow); err != nil {
		return settings{}, err
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as 20s, got %q", key, v)
	}
	return d, nil
}

func TestMain(m *testing.M) {
	cfg, err := loadSettings()
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		os.Exit(2)
	}
	if err := preflight(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// preflight checks that the stack has every service the suite drives, so a
// missing one is named once instead of every test timing out on it.
func preflight(cfg settings) error {
	ctx, cancel := context.WithTimeout(context.Background(), preflightTimeout)
	defer cancel()
	s := &stack{cfg: cfg, client: newHTTPClient()}

	if err := s.checkGateway(ctx); err != nil {
		return preflightError(cfg, err.Error())
	}
	u, err := s.registerStudent(ctx)
	if err != nil {
		// AI-generated (edited by James Teo).
		return preflightError(cfg, "user-service, through the gateway's /auth routes and its own log: "+err.Error())
	}
	var missing []string
	if err := s.checkOrderService(ctx, u); err != nil {
		missing = append(missing, err.Error())
	}
	if err := s.checkCreditService(ctx, u); err != nil {
		missing = append(missing, err.Error())
	}
	if len(missing) > 0 {
		return preflightError(cfg, missing...)
	}
	return nil
}

func (s *stack) checkGateway(ctx context.Context) error {
	r, err := s.request(ctx, http.MethodGet, "/healthz", "", nil, nil)
	if err != nil {
		return fmt.Errorf("api-gateway: nothing answers at %s (%v); is the stack up?", s.cfg.gatewayURL, err)
	}
	if r.status != http.StatusOK {
		return fmt.Errorf("api-gateway: %s, want 200", r)
	}
	return nil
}

func (s *stack) checkOrderService(ctx context.Context, u user) error {
	r, err := s.request(ctx, http.MethodGet, ordersPath+"/locations", u.token, nil, nil)
	if err != nil {
		return fmt.Errorf("order-service: %w", err)
	}
	switch r.status {
	case http.StatusOK:
		return nil
	case http.StatusBadGateway:
		return fmt.Errorf("order-service: the gateway cannot reach it (%s); it is not running in this stack", r)
	default:
		return fmt.Errorf("order-service: %s, want 200", r)
	}
}

// checkCreditService waits for credit-service to open the new student's
// account. The account comes from user-service's user.registered event, so a
// 404 can mean only that the event has not landed yet; a 502 means
// credit-service is not there at all.
func (s *stack) checkCreditService(ctx context.Context, u user) error {
	ctx, cancel := context.WithTimeout(ctx, eventDeadline)
	defer cancel()
	path := creditsPath + "/accounts/me"
	outcome := errors.New("credit-service: no answer")
	poll(ctx, func(ctx context.Context) (bool, string) {
		r, err := s.request(ctx, http.MethodGet, path, u.token, nil, nil)
		if err != nil {
			if ctx.Err() == nil {
				outcome = fmt.Errorf("credit-service: %w", err)
			}
			return false, ""
		}
		switch r.status {
		case http.StatusOK:
			outcome = nil
			return true, ""
		case http.StatusNotFound:
			// AI-generated (edited by James Teo).
			outcome = fmt.Errorf("credit-service: no credit account for a student %s after verifying their email (%s). "+
				"Either the gateway does not route %s to credit-service's /api/v1/credits/accounts/me yet, "+
				"or nothing opens an account once an email is verified (user-service's user.registered producer, "+
				"RabbitMQ, credit-service's consumer)", eventDeadline, r, path)
			return false, ""
		case http.StatusBadGateway:
			outcome = fmt.Errorf("credit-service: the gateway cannot reach it (%s); it is not running in this stack", r)
			return true, ""
		default:
			outcome = fmt.Errorf("credit-service: %s, want 200", r)
			return true, ""
		}
	})
	return outcome
}

func preflightError(cfg settings, problems ...string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "e2e preflight: the stack behind %s cannot run this suite yet.\n", cfg.gatewayURL)
	for _, p := range problems {
		fmt.Fprintf(&b, "  - %s\n", p)
	}
	b.WriteString("The suite runs once order-service and its Compose wiring, RabbitMQ, " +
		"credit-service's reserve, adjust and event consumers, user-service's user.registered producer, " +
		"and the gateway's token forwarding and /api/credits change are all in the stack.\n" +
		"e2e/README.md has the command that starts the stack and runs the suite.")
	return errors.New(b.String())
}
