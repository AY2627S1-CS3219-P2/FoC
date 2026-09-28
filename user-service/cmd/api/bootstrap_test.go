// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-22
// Scope: Added focused tests for the recorded initial ADMIN bootstrap workflow.
// Author review: Verified tests reflect intended requirements

package main

import (
	"context"
	"errors"
	"testing"
)

type fakeInitialAdminBootstrapper struct {
	email    string
	username string
	password string
	err      error
}

func (f *fakeInitialAdminBootstrapper) BootstrapInitialAdmin(_ context.Context, email, username, password string) error {
	f.email, f.username, f.password = email, username, password
	return f.err
}

// AI-generated (edited by PENDING): bootstrap forwards raw configuration input to the domain service.
func TestBootstrapInitialAdminPassesRawCredentialsToService(t *testing.T) {
	service := &fakeInitialAdminBootstrapper{}

	if err := bootstrapInitialAdmin(context.Background(), service, " Admin@U.NUS.EDU ", "initialadmin", "StrongAdmin1"); err != nil {
		t.Fatal(err)
	}
	if service.email != " Admin@U.NUS.EDU " || service.username != "initialadmin" || service.password != "StrongAdmin1" {
		t.Fatalf("bootstrap credentials = %#v, want raw configured values", service)
	}
}

func TestBootstrapInitialAdminPropagatesServiceFailure(t *testing.T) {
	want := errors.New("database unavailable")
	if err := bootstrapInitialAdmin(context.Background(), &fakeInitialAdminBootstrapper{err: want}, "admin@u.nus.edu", "initialadmin", "StrongAdmin1"); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestBootstrapInitialAdminRejectsMissingService(t *testing.T) {
	if err := bootstrapInitialAdmin(context.Background(), nil, "admin@u.nus.edu", "initialadmin", "StrongAdmin1"); err == nil {
		t.Fatal("missing bootstrap service was accepted")
	}
}
