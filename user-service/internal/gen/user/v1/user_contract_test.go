// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added generated-contract regression tests for the recorded user gRPC API.
// Author review: PENDING

package userv1

import "testing"

func TestGeneratedUserServiceContract(t *testing.T) {
	service := File_user_v1_user_proto.Services().ByName("UserService")
	if service == nil {
		t.Fatal("generated descriptor has no UserService")
	}

	wantMethods := []string{
		"Register",
		"Login",
		"GetProfile",
		"UpdateProfile",
		"UpdateStatus",
		"RefreshSession",
		"Logout",
	}
	if service.Methods().Len() != len(wantMethods) {
		t.Fatalf("method count = %d, want %d", service.Methods().Len(), len(wantMethods))
	}
	for index, want := range wantMethods {
		if got := string(service.Methods().Get(index).Name()); got != want {
			t.Errorf("method %d = %q, want %q", index, got, want)
		}
	}

	refresh := service.Methods().ByName("RefreshSession")
	if refresh == nil {
		t.Fatal("generated descriptor has no RefreshSession method")
	}
	field := refresh.Input().Fields().ByName("refresh_token")
	if field == nil {
		t.Fatal("RefreshSession request has no refresh_token field")
	}
	if got := field.JSONName(); got != "refreshToken" {
		t.Errorf("refresh_token JSON name = %q, want refreshToken", got)
	}
}
