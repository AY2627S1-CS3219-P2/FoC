// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-21
// Scope: Added focused regression coverage for the relocated authentication handler.
// Author review: COMPLETED BY ZI YANG

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foc/user-service/internal/session"
)

type loginServiceStub struct {
	pair   session.TokenPair
	called *bool
}

func (s loginServiceStub) Login(context.Context, string, string) (session.TokenPair, error) {
	if s.called != nil {
		*s.called = true
	}
	return s.pair, nil
}

func TestAuthHandlerLoginReturnsTokenPair(t *testing.T) {
	handler := NewAuthHandler(AuthDependencies{
		LoginService: loginServiceStub{pair: session.TokenPair{AccessToken: "access", RefreshToken: "refresh"}},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/login", strings.NewReader(`{"identifier":"student","password":"ValidPass1"}`))
	response := httptest.NewRecorder()

	handler.Login(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var payload AuthResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload != (AuthResponse{AccessToken: "access", RefreshToken: "refresh"}) {
		t.Fatalf("payload = %#v, want token pair", payload)
	}
}

// AI-generated (edited by PENDING): the recorded body limit rejects oversized credentials before authentication.
func TestAuthHandlerLoginRejectsOversizedRequest(t *testing.T) {
	called := false
	handler := NewAuthHandler(AuthDependencies{
		LoginService: loginServiceStub{called: &called},
	})
	body := `{"identifier":"student","password":"` + strings.Repeat("a", 10*1024) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/login", strings.NewReader(body))
	response := httptest.NewRecorder()

	handler.Login(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
	if called {
		t.Fatal("login service was called for an oversized request")
	}
}
