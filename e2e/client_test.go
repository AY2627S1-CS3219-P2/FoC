//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The suite's gateway client: register and log in (user-service's
//   spec), small JSON types for order-service's and credit-service's APIs, and
//   the calls the scenarios make, all on the browser's paths (D-027, D-053).
//   It imports nothing from the services.
// Author review: James Teo — reviewed and verified.
//
// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: supplierSummary reads the name, and availableSupplier skips suppliers
//   named e2e-, which suppliers_test.go creates and deletes.
// Author review: PENDING — <reviewer to complete>

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// requestTimeout bounds one call through the gateway.
	requestTimeout = 15 * time.Second
	// maxBodyBytes caps how much of an answer the suite reads.
	maxBodyBytes = 1 << 20
	// studentPassword meets user-service's policy: 8 to 128 characters with
	// an upper-case letter, a lower-case letter and a digit.
	studentPassword = "E2eStudent123"
)

// The browser's prefixes, which the gateway strips (D-027, D-053).
const (
	ordersPath    = "/api/orders"
	creditsPath   = "/api/credits"
	suppliersPath = "/api/suppliers"
	// AI-generated (edited by James Teo).
	usersPath = "/api/users"
)

// AI-generated (edited by James Teo).
// refreshCookie is the cookie the gateway keeps the refresh token in; logout
// reads it (D-033).
const refreshCookie = "foc_refresh"

// Errand states (D-045).
const (
	stateOpen      = "OPEN"
	stateAccepted  = "ACCEPTED"
	statePickedUp  = "PICKED_UP"
	stateDelivered = "DELIVERED"
	stateCompleted = "COMPLETED"
	stateCancelled = "CANCELLED"
	stateExpired   = "EXPIRED"
)

// stack is one test's handle on the running stack, reached only through the
// gateway.
type stack struct {
	cfg    settings
	client *http.Client
}

func newStack(t *testing.T) *stack {
	t.Helper()
	cfg, err := loadSettings()
	require.NoError(t, err)
	return &stack{cfg: cfg, client: newHTTPClient()}
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: requestTimeout}
}

// AI-generated (edited by James Teo).
// user is a signed-in account. id is the access token's subject, which each
// service reads from the token itself (D-056). refresh is the refresh token
// the gateway set as a cookie at login, which logout needs back.
type user struct {
	id       string
	username string
	role     string
	token    string
	refresh  string
}

// AI-generated (edited by James Teo).
// response is one answer from the gateway, kept whole for failure messages.
type response struct {
	method string
	path   string
	status int
	header http.Header
	body   []byte
}

func (r response) String() string {
	body := string(bytes.TrimSpace(r.body))
	if len(body) > 300 {
		body = body[:300] + "..."
	}
	return fmt.Sprintf("%s %s answered %d %s", r.method, r.path, r.status, body)
}

func (r response) decode(v any) error {
	if err := json.Unmarshal(r.body, v); err != nil {
		return fmt.Errorf("decode the body of %s: %w", r, err)
	}
	return nil
}

// request sends one request through the gateway. A nil body sends none; a
// token adds the bearer header the gateway verifies.
func (s *stack) request(ctx context.Context, method, path, token string, body any, header http.Header) (response, error) {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return response{}, fmt.Errorf("encode the body of %s %s: %w", method, path, err)
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.gatewayURL+path, payload)
	if err != nil {
		return response{}, fmt.Errorf("build %s %s: %w", method, path, err)
	}
	for name, values := range header {
		req.Header[name] = values
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return response{}, fmt.Errorf("read the answer to %s %s: %w", method, path, err)
	}
	// AI-generated (edited by James Teo).
	return response{method: method, path: path, status: res.StatusCode, header: res.Header, body: raw}, nil
}

// send is request for a test, where a failed round trip fails the test.
func (s *stack) send(t *testing.T, method, path, token string, body any) response {
	t.Helper()
	r, err := s.request(t.Context(), method, path, token, body, nil)
	require.NoError(t, err)
	return r
}

// expectStatus fails the test unless r has one of the wanted statuses.
func expectStatus(t *testing.T, r response, want ...int) {
	t.Helper()
	if !slices.Contains(want, r.status) {
		t.Fatalf("%s; want %v", r, want)
	}
}

// ---- Accounts (user-service's api/openapi.yaml, through /auth) ---------

type registerRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

// AI-generated (edited by James Teo).
type verifyOTPRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

// AI-generated (edited by James Teo).
// registerStudent registers a STUDENT whose name no other run uses, verifies
// their email with the code user-service sent, then logs in as them. Login
// refuses an account until it is verified.
func (s *stack) registerStudent(ctx context.Context) (user, error) {
	// user-service allows only letters and digits in a username.
	username := "e2e" + randomHex(6)
	email := username + "@u.nus.edu"
	r, err := s.request(ctx, http.MethodPost, "/auth/register", "", registerRequest{
		Email:    email,
		Username: username,
		Password: studentPassword,
	}, nil)
	if err != nil {
		return user{}, err
	}
	if r.status != http.StatusCreated {
		return user{}, fmt.Errorf("register %s: %s", username, r)
	}
	if err := s.verifyEmail(ctx, email); err != nil {
		return user{}, err
	}
	return s.login(ctx, username, studentPassword)
}

// AI-generated (edited by James Teo).
// repoRoot is where compose.yaml is; go test runs the suite in e2e/.
const repoRoot = ".."

// AI-generated (edited by James Teo).
// verifyEmail posts the code user-service sent to email to /auth/verify-otp,
// which activates the account.
func (s *stack) verifyEmail(ctx context.Context, email string) error {
	code, err := verificationCode(ctx, email)
	if err != nil {
		return err
	}
	r, err := s.request(ctx, http.MethodPost, "/auth/verify-otp", "", verifyOTPRequest{Email: email, OTP: code}, nil)
	if err != nil {
		return err
	}
	if r.status != http.StatusOK {
		return fmt.Errorf("verify %s: %s", email, r)
	}
	return nil
}

// AI-generated (edited by James Teo).
// verificationCode waits for the code user-service sent to email. Compose runs
// user-service with OTP_DELIVERY=log, which writes each code to its log
// instead of emailing it, so the suite reads it there the way root AGENTS.md
// §11 has a person do.
func verificationCode(ctx context.Context, email string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, eventDeadline)
	defer cancel()
	var code string
	last, ok := poll(ctx, func(ctx context.Context) (bool, string) {
		codes, err := verificationCodes(ctx, email)
		if err != nil {
			return false, err.Error()
		}
		if len(codes) > 0 {
			code = codes[len(codes)-1]
		}
		return code != "", "no code for " + email + " in the log yet"
	})
	if !ok {
		return "", fmt.Errorf("no verification code for %s within %s (last: %s). The suite reads codes from "+
			"`docker compose logs user-service` in the repository root, so the stack must run there with "+
			"OTP_DELIVERY=log, Compose's default", email, eventDeadline, last)
	}
	return code, nil
}

func verificationCodes(ctx context.Context, email string) ([]string, error) {
	// AI Assistance Disclosure: Codex (GPT-5), date: 2026-10-19 — exposes all
	// logged codes so the issue #31 E2E test can detect an unwanted resend.
	cmd := exec.CommandContext(ctx, "docker", "compose", "logs", "--no-log-prefix", "user-service")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return nil, fmt.Errorf("docker compose logs user-service: %s", strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("docker compose logs user-service: %w", err)
	}
	return codesFor(out, email), nil
}

// AI-generated (edited by James Teo).
// codeFor returns the last code user-service's log holds for email. Each line
// the service logs is one JSON object; anything else is skipped.
func codeFor(logs []byte, email string) string {
	codes := codesFor(logs, email)
	if len(codes) == 0 {
		return ""
	}
	return codes[len(codes)-1]
}

func codesFor(logs []byte, email string) []string {
	var codes []string
	for _, line := range bytes.Split(logs, []byte("\n")) {
		var entry struct {
			Email string `json:"email"`
			OTP   string `json:"otp"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.Email == email && entry.OTP != "" {
			codes = append(codes, entry.OTP)
		}
	}
	return codes
}

// login signs in through the gateway, which moves the refresh token into a
// cookie (D-033), so the body carries only the access token.
func (s *stack) login(ctx context.Context, identifier, password string) (user, error) {
	r, err := s.request(ctx, http.MethodPost, "/auth/login", "", loginRequest{Identifier: identifier, Password: password}, nil)
	if err != nil {
		return user{}, err
	}
	if r.status != http.StatusOK {
		return user{}, fmt.Errorf("log in as %s: %s", identifier, r)
	}
	var body struct {
		AccessToken string `json:"accessToken"`
	}
	if err := r.decode(&body); err != nil {
		return user{}, err
	}
	claims, err := claimsOf(body.AccessToken)
	if err != nil {
		return user{}, fmt.Errorf("log in as %s: %w", identifier, err)
	}
	// AI-generated (edited by James Teo).
	u := user{id: claims.Subject, username: identifier, role: claims.Role, token: body.AccessToken}
	for _, c := range (&http.Response{Header: r.header}).Cookies() {
		if c.Name == refreshCookie {
			u.refresh = c.Value
		}
	}
	return u, nil
}

// AI-generated (edited by James Teo).
// logout signs u out through the gateway, sending the refresh cookie the way
// a browser does, so user-service ends the session and blocklists u's access
// token in the shared Redis (D-054).
func (s *stack) logout(t *testing.T, u user) {
	t.Helper()
	require.NotEmpty(t, u.refresh, "the gateway set no %s cookie when %s logged in", refreshCookie, u.username)
	header := http.Header{}
	header.Set("Cookie", (&http.Cookie{Name: refreshCookie, Value: u.refresh}).String())
	r, err := s.request(t.Context(), http.MethodPost, "/auth/logout", u.token, struct{}{}, header)
	require.NoError(t, err)
	expectStatus(t, r, http.StatusNoContent)
}

// AI-generated (edited by James Teo).
// statusRequest is user-service's UpdateStatusRequest.
type statusRequest struct {
	Status string `json:"status"`
}

// AI-generated (edited by James Teo).
// suspend has admin suspend u's account, which writes the suspension key every
// service checks in the shared Redis (D-054).
func (s *stack) suspend(t *testing.T, admin, u user) {
	t.Helper()
	r := s.send(t, http.MethodPatch, usersPath+"/"+u.id+"/status", admin.token, statusRequest{Status: "SUSPENDED"})
	expectStatus(t, r, http.StatusOK)
}

type tokenClaims struct {
	Subject string `json:"sub"`
	Role    string `json:"role"`
}

// claimsOf reads who an access token names. The gateway verifies its
// signature (D-023); the suite only needs the subject and role.
func claimsOf(token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, errors.New("the access token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, fmt.Errorf("decode the access token's claims: %w", err)
	}
	var c tokenClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return tokenClaims{}, fmt.Errorf("decode the access token's claims: %w", err)
	}
	if c.Subject == "" {
		return tokenClaims{}, errors.New("the access token has no sub claim")
	}
	return c, nil
}

func (s *stack) newStudent(t *testing.T) user {
	t.Helper()
	u, err := s.registerStudent(t.Context())
	require.NoError(t, err)
	return u
}

// admin logs in as the ADMIN account the settings name, by default the shared
// local one in root AGENTS.md §11.
func (s *stack) admin(t *testing.T) user {
	t.Helper()
	u, err := s.login(t.Context(), s.cfg.adminUsername, s.cfg.adminPassword)
	require.NoError(t, err, "set E2E_ADMIN_USERNAME and E2E_ADMIN_PASSWORD to an ADMIN account")
	require.Equal(t, "ADMIN", u.role, "%s is not an ADMIN account", s.cfg.adminUsername)
	return u
}

// randomHex returns n random bytes, hex-encoded.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // never fails since Go 1.24
	return hex.EncodeToString(b)
}

// randomUUID returns a random version 4 UUID.
func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // never fails since Go 1.24
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ---- Errands (order-service's api/openapi.yaml, under /api/orders) -----

type supplierSnapshot struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
}

type errand struct {
	ID               string           `json:"id"`
	State            string           `json:"state"`
	Version          int              `json:"version"`
	Supplier         supplierSnapshot `json:"supplier"`
	ItemDescription  string           `json:"itemDescription"`
	DeliveryLocation string           `json:"deliveryLocation"`
	Reward           int              `json:"reward"`
	RequesterID      string           `json:"requesterId"`
	CourierID        string           `json:"courierId"`
	CreatedAt        time.Time        `json:"createdAt"`
	StateChangedAt   time.Time        `json:"stateChangedAt"`
	ExpiresAt        *time.Time       `json:"expiresAt"`
	ConfirmBy        *time.Time       `json:"confirmBy"`
	Actions          []string         `json:"actions"`
}

type historyEntry struct {
	Action     string    `json:"action"`
	FromState  string    `json:"fromState"`
	ToState    string    `json:"toState"`
	OccurredAt time.Time `json:"occurredAt"`
}

type errandDetail struct {
	Errand     errand         `json:"errand"`
	History    []historyEntry `json:"history"`
	ServerTime time.Time      `json:"serverTime"`
}

type assignmentRecord struct {
	ErrandID string `json:"errandId"`
	Outcome  string `json:"outcome"`
}

type assignedList struct {
	Items   []errand           `json:"items"`
	History []assignmentRecord `json:"history"`
}

type locationList struct {
	Items []struct {
		Name string `json:"name"`
		Area string `json:"area"`
	} `json:"items"`
}

type createErrandRequest struct {
	SupplierID       string `json:"supplierId"`
	ItemDescription  string `json:"itemDescription"`
	DeliveryLocation string `json:"deliveryLocation"`
	Reward           int    `json:"reward"`
}

type versionRequest struct {
	Version int `json:"version"`
}

// AI-generated (edited by James Teo).
// amendRewardRequest is order-service's AmendErrandRequest changing only the reward.
type amendRewardRequest struct {
	Version int `json:"version"`
	Reward  int `json:"reward"`
}

// supplierSummary is the part of supplier-service's list the suite reads; the
// list is a bare JSON array with snake_case names (D-036, D-037).
type supplierSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	IsAvailable bool   `json:"is_available"`
}

func errandPath(id string) string { return ordersPath + "/" + id }

func actionPath(id, action string) string { return errandPath(id) + "/" + action }

// errandInput is a valid create body: a supplier that is taking errands and
// a delivery location from order-service's own list (D-052, D-053).
func (s *stack) errandInput(t *testing.T, u user, reward int) createErrandRequest {
	t.Helper()
	return createErrandRequest{
		SupplierID:       s.availableSupplier(t, u),
		ItemDescription:  "e2e " + t.Name(),
		DeliveryLocation: s.deliveryLocation(t, u),
		Reward:           reward,
	}
}

func (s *stack) availableSupplier(t *testing.T, u user) string {
	t.Helper()
	r := s.send(t, http.MethodGet, suppliersPath, u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var suppliers []supplierSummary
	require.NoError(t, r.decode(&suppliers))
	for _, sup := range suppliers {
		// The e2e- suppliers are other tests' own and may be deleted while this
		// one runs (suppliers_test.go), so an errand never names one.
		if sup.IsAvailable && !strings.HasPrefix(sup.Name, "e2e-") {
			return sup.ID
		}
	}
	t.Fatalf("supplier-service lists no available supplier among %d; it seeds itself from data/csv on first boot", len(suppliers))
	return ""
}

func (s *stack) deliveryLocation(t *testing.T, u user) string {
	t.Helper()
	r := s.send(t, http.MethodGet, ordersPath+"/locations", u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var list locationList
	require.NoError(t, r.decode(&list))
	require.NotEmpty(t, list.Items, "order-service lists no delivery locations")
	return list.Items[0].Name
}

// createErrand posts an errand as u and expects 201. Each create carries a
// fresh X-Request-Id, as the browser sends one per create form (D-047).
func (s *stack) createErrand(t *testing.T, u user, in createErrandRequest) errandDetail {
	t.Helper()
	header := http.Header{}
	header.Set("X-Request-Id", randomUUID())
	r, err := s.request(t.Context(), http.MethodPost, ordersPath, u.token, in, header)
	require.NoError(t, err)
	d := detailOf(t, r, http.StatusCreated)
	require.Equal(t, stateOpen, d.Errand.State)
	return d
}

// AI-generated (edited by James Teo).
// amendReward changes e's reward as its requester u, sending the version u was
// shown. A raise is held before the 200, and a decrease is normally released
// before it too; errand.reward_set covers a release call that failed (D-085).
func (s *stack) amendReward(t *testing.T, u user, e errand, reward int) errand {
	t.Helper()
	r := s.send(t, http.MethodPatch, errandPath(e.ID), u.token, amendRewardRequest{Version: e.Version, Reward: reward})
	return detailOf(t, r, http.StatusOK).Errand
}

// accept sends the version u was shown, as accept requires (D-046).
func (s *stack) accept(t *testing.T, u user, e errand) response {
	t.Helper()
	return s.send(t, http.MethodPost, actionPath(e.ID, "accept"), u.token, versionRequest{Version: e.Version})
}

// act runs one of the bodiless actions: withdraw, pickup, deliver, confirm,
// cancel or complete (D-053).
func (s *stack) act(t *testing.T, u user, id, action string) response {
	t.Helper()
	return s.send(t, http.MethodPost, actionPath(id, action), u.token, nil)
}

func (s *stack) errand(t *testing.T, u user, id string) errandDetail {
	t.Helper()
	return detailOf(t, s.send(t, http.MethodGet, errandPath(id), u.token, nil), http.StatusOK)
}

// tryErrand is errand for a poll, where a bad answer is a status, not a failure.
func (s *stack) tryErrand(ctx context.Context, u user, id string) (errandDetail, error) {
	r, err := s.request(ctx, http.MethodGet, errandPath(id), u.token, nil, nil)
	if err != nil {
		return errandDetail{}, err
	}
	if r.status != http.StatusOK {
		return errandDetail{}, errors.New(r.String())
	}
	var d errandDetail
	if err := r.decode(&d); err != nil {
		return errandDetail{}, err
	}
	return d, nil
}

// detailOf checks r's status and decodes the errand it carries.
func detailOf(t *testing.T, r response, want int) errandDetail {
	t.Helper()
	expectStatus(t, r, want)
	var d errandDetail
	require.NoError(t, r.decode(&d))
	return d
}

// waitForState polls the errand as u until it reaches state.
func (s *stack) waitForState(t *testing.T, u user, id, state string, timeout time.Duration) errandDetail {
	t.Helper()
	var last errandDetail
	eventually(t, timeout, fmt.Sprintf("errand %s reaches %s", id, state), func(ctx context.Context) (bool, string) {
		d, err := s.tryErrand(ctx, u, id)
		if err != nil {
			return false, err.Error()
		}
		last = d
		return d.Errand.State == state, "state " + d.Errand.State
	})
	return last
}

// waitForServerTime polls the errand as u until order-service's clock, as its
// serverTime reports it (D-053), reaches at; a skewed local clock cannot make
// the suite act early.
func (s *stack) waitForServerTime(t *testing.T, u user, id string, at time.Time, timeout time.Duration) {
	t.Helper()
	eventually(t, timeout, "order-service's clock reaches "+at.Format(time.RFC3339), func(ctx context.Context) (bool, string) {
		d, err := s.tryErrand(ctx, u, id)
		if err != nil {
			return false, err.Error()
		}
		return !d.ServerTime.Before(at), "server time " + d.ServerTime.Format(time.RFC3339Nano)
	})
}

// assignmentHistory is u's ended assignments, withdrawals included (F3.5.3).
func (s *stack) assignmentHistory(t *testing.T, u user) []assignmentRecord {
	t.Helper()
	r := s.send(t, http.MethodGet, ordersPath+"/assigned?scope=history", u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var list assignedList
	require.NoError(t, r.decode(&list))
	return list.History
}

// actionsOf lists an errand's history actions, oldest first.
func actionsOf(history []historyEntry) []string {
	out := make([]string, 0, len(history))
	for _, h := range history {
		out = append(out, h.Action)
	}
	return out
}

// ---- Credits (credit-service's api/openapi.yaml, under /api/credits) ---

type creditAccount struct {
	AvailableCredits int `json:"availableCredits"`
	ReservedCredits  int `json:"reservedCredits"`
}

// balance is the pair of numbers the scenarios follow.
type balance struct {
	available int
	reserved  int
}

func (b balance) String() string {
	return fmt.Sprintf("%d available, %d reserved", b.available, b.reserved)
}

func (s *stack) readBalance(ctx context.Context, u user) (balance, error) {
	r, err := s.request(ctx, http.MethodGet, creditsPath+"/accounts/me", u.token, nil, nil)
	if err != nil {
		return balance{}, err
	}
	if r.status != http.StatusOK {
		return balance{}, errors.New(r.String())
	}
	var a creditAccount
	if err := r.decode(&a); err != nil {
		return balance{}, err
	}
	return balance{available: a.AvailableCredits, reserved: a.ReservedCredits}, nil
}

// startingBalance waits for credit-service to open u's account, which it does
// on user-service's user.registered event, and returns it.
func (s *stack) startingBalance(t *testing.T, u user) balance {
	t.Helper()
	var got balance
	eventually(t, eventDeadline, "a credit account for "+u.username, func(ctx context.Context) (bool, string) {
		b, err := s.readBalance(ctx, u)
		if err != nil {
			return false, err.Error()
		}
		got = b
		return true, b.String()
	})
	return got
}

// requireBalance checks u's credits once, for a change made synchronously.
func (s *stack) requireBalance(t *testing.T, u user, want balance) {
	t.Helper()
	got, err := s.readBalance(t.Context(), u)
	require.NoError(t, err)
	require.Equal(t, want, got, "credits of %s", u.username)
}

// waitForBalance polls u's credits until they equal want. Settlement travels
// as an event (D-048), so it lands a moment after the action that caused it.
func (s *stack) waitForBalance(t *testing.T, u user, want balance) {
	t.Helper()
	eventually(t, eventDeadline, fmt.Sprintf("credits of %s reach %s", u.username, want), func(ctx context.Context) (bool, string) {
		got, err := s.readBalance(ctx, u)
		if err != nil {
			return false, err.Error()
		}
		return got == want, got.String()
	})
}
