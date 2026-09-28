// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added contract tests for persisted account enum values and nullable login time.
// Author review: COMPLETED BY ZI YANG

package user

import (
	"reflect"
	"testing"
)

func TestAccountRoleValuesMatchDatabaseContract(t *testing.T) {
	tests := map[string]struct {
		got  AccountRole
		want AccountRole
	}{
		"student": {got: AccountRoleStudent, want: "STUDENT"},
		"admin":   {got: AccountRoleAdmin, want: "ADMIN"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("role = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestAccountStatusValuesMatchDatabaseContract(t *testing.T) {
	tests := map[string]struct {
		got  AccountStatus
		want AccountStatus
	}{
		"active":    {got: AccountStatusActive, want: "ACTIVE"},
		"suspended": {got: AccountStatusSuspended, want: "SUSPENDED"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("status = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestUserAllowsUnsetLastLoginDate(t *testing.T) {
	u := User{}
	if u.LastLoginDate != nil {
		t.Fatalf("LastLoginDate = %v, want nil", u.LastLoginDate)
	}
}

// AI-generated (edited by PENDING): the domain model does not define an HTTP JSON representation.
func TestUserHasNoJSONTags(t *testing.T) {
	userType := reflect.TypeFor[User]()
	for i := 0; i < userType.NumField(); i++ {
		field := userType.Field(i)
		if tag := field.Tag.Get("json"); tag != "" {
			t.Fatalf("User.%s JSON tag = %q, want none", field.Name, tag)
		}
	}
}
