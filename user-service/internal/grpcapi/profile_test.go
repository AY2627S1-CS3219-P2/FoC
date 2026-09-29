// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added in-process gRPC behavior tests for profile lookup and update.
// Author review: ZI YANG - validated correctness

package grpcapi_test

import (
	"context"
	"errors"
	"testing"
	"time"

	userv1 "foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/grpcapi"
	"foc/user-service/internal/grpcapi/interceptors"
	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type profileServiceStub struct {
	account *user.User
	err     error

	getCalls int
	getUID   uuid.UUID

	selfUpdateCalls  int
	adminUpdateCalls int
	updateUID        uuid.UUID
	username         string
	phone            string
	currentPassword  string
	newPassword      string
}

func (s *profileServiceStub) GetByID(_ context.Context, uid uuid.UUID) (*user.User, error) {
	s.getCalls++
	s.getUID = uid
	return s.account, s.err
}

func (s *profileServiceStub) UpdateProfile(_ context.Context, uid uuid.UUID, username, phone, currentPassword, newPassword string) (*user.User, error) {
	s.selfUpdateCalls++
	s.captureUpdate(uid, username, phone, currentPassword, newPassword)
	return s.account, s.err
}

func (s *profileServiceStub) UpdateProfileAsAdmin(_ context.Context, uid uuid.UUID, username, phone, newPassword string) (*user.User, error) {
	s.adminUpdateCalls++
	s.captureUpdate(uid, username, phone, "", newPassword)
	return s.account, s.err
}

func (s *profileServiceStub) captureUpdate(uid uuid.UUID, username, phone, currentPassword, newPassword string) {
	s.updateUID = uid
	s.username = username
	s.phone = phone
	s.currentPassword = currentPassword
	s.newPassword = newPassword
}

func TestServerGetProfileSelectsFieldsByVerifiedRole(t *testing.T) {
	targetID := uuid.New()
	createdAt := time.Date(2026, time.September, 29, 4, 30, 0, 0, time.UTC)
	account := &user.User{
		UID:           targetID,
		Username:      "student1",
		Email:         "student1@u.nus.edu",
		PhoneNum:      "+6512345678",
		AccountRole:   user.AccountRoleStudent,
		AccountStatus: user.AccountStatusActive,
		DateCreated:   createdAt,
	}

	tests := []struct {
		name            string
		callerID        uuid.UUID
		role            string
		wantAdminFields bool
	}{
		{name: "student reads self", callerID: targetID, role: "STUDENT"},
		{name: "student reads another account", callerID: uuid.New(), role: "STUDENT"},
		{name: "admin reads another account", callerID: uuid.New(), role: "ADMIN", wantAdminFields: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := &profileServiceStub{account: account}
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{ProfileGetter: profiles}))
			response, err := h.client.GetProfile(identityContext(test.callerID, test.role), &userv1.GetProfileRequest{Uid: targetID.String()})
			if err != nil {
				t.Fatal(err)
			}
			profile := response.GetProfile()
			if profile.GetUid() != targetID.String() || profile.GetUsername() != "student1" || profile.GetEmail() != "student1@u.nus.edu" || profile.GetPhoneNum() != "+6512345678" {
				t.Fatalf("profile = %#v", profile)
			}
			if profiles.getCalls != 1 || profiles.getUID != targetID {
				t.Fatalf("GetByID = (%d, %s), want (1, %s)", profiles.getCalls, profiles.getUID, targetID)
			}
			if test.wantAdminFields {
				if profile.AccountRole == nil || profile.GetAccountRole() != userv1.AccountRole_ACCOUNT_ROLE_STUDENT {
					t.Fatalf("account role = %#v", profile.AccountRole)
				}
				if profile.AccountStatus == nil || profile.GetAccountStatus() != userv1.AccountStatus_ACCOUNT_STATUS_ACTIVE {
					t.Fatalf("account status = %#v", profile.AccountStatus)
				}
				if profile.GetDateCreated() == nil || !profile.GetDateCreated().AsTime().Equal(createdAt) {
					t.Fatalf("date created = %v, want %v", profile.GetDateCreated(), createdAt)
				}
			} else if profile.AccountRole != nil || profile.AccountStatus != nil || profile.DateCreated != nil {
				t.Fatalf("restricted profile exposed administrative fields: %#v", profile)
			}
		})
	}
}

func TestServerGetProfileMapsLookupFailures(t *testing.T) {
	targetID := uuid.New()
	tests := []struct {
		name     string
		account  *user.User
		err      error
		wantCode codes.Code
	}{
		{name: "missing sentinel", err: user.ErrNotFound, wantCode: codes.NotFound},
		{name: "nil account", wantCode: codes.NotFound},
		{name: "database unavailable", err: connectionFailure{}, wantCode: codes.Unavailable},
		{name: "unexpected failure", err: errors.New("secret query failure"), wantCode: codes.Internal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{ProfileGetter: &profileServiceStub{account: test.account, err: test.err}}))
			_, err := h.client.GetProfile(identityContext(uuid.New(), "STUDENT"), &userv1.GetProfileRequest{Uid: targetID.String()})
			if status.Code(err) != test.wantCode {
				t.Fatalf("code = %s, want %s; error = %v", status.Code(err), test.wantCode, err)
			}
		})
	}
}

func TestServerGetProfileValidationAndAuthenticationPrecedeLookup(t *testing.T) {
	profiles := &profileServiceStub{}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{ProfileGetter: profiles}))

	_, unauthenticatedErr := h.client.GetProfile(context.Background(), &userv1.GetProfileRequest{Uid: uuid.NewString()})
	_, invalidErr := h.client.GetProfile(identityContext(uuid.New(), "STUDENT"), &userv1.GetProfileRequest{Uid: "not-a-uuid"})
	if status.Code(unauthenticatedErr) != codes.Unauthenticated || status.Code(invalidErr) != codes.InvalidArgument {
		t.Fatalf("codes = (%s, %s), want (Unauthenticated, InvalidArgument)", status.Code(unauthenticatedErr), status.Code(invalidErr))
	}
	if profiles.getCalls != 0 {
		t.Fatalf("GetByID calls = %d, want 0", profiles.getCalls)
	}
}

func TestServerUpdateProfileUsesSelfAndAdminPaths(t *testing.T) {
	targetID := uuid.New()
	account := profileAccount(targetID)
	tests := []struct {
		name           string
		callerID       uuid.UUID
		role           string
		wantSelfCalls  int
		wantAdminCalls int
	}{
		{name: "student updates self", callerID: targetID, role: "STUDENT", wantSelfCalls: 1},
		{name: "admin updates self", callerID: targetID, role: "ADMIN", wantSelfCalls: 1},
		{name: "admin updates another account", callerID: uuid.New(), role: "ADMIN", wantAdminCalls: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := &profileServiceStub{account: account}
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{
				ProfileUpdater:      profiles,
				AdminProfileUpdater: profiles,
			}))
			response, err := h.client.UpdateProfile(identityContext(test.callerID, test.role), &userv1.UpdateProfileRequest{
				Uid:             targetID.String(),
				Username:        stringPointer("updated1"),
				CurrentPassword: stringPointer("OldPass1"),
				NewPassword:     stringPointer("NewPass1"),
				PhoneNum:        stringPointer("91234567"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if profiles.selfUpdateCalls != test.wantSelfCalls || profiles.adminUpdateCalls != test.wantAdminCalls {
				t.Fatalf("update calls = (self %d, admin %d), want (%d, %d)", profiles.selfUpdateCalls, profiles.adminUpdateCalls, test.wantSelfCalls, test.wantAdminCalls)
			}
			if profiles.updateUID != targetID || profiles.username != "updated1" || profiles.phone != "91234567" || profiles.newPassword != "NewPass1" {
				t.Fatalf("captured update = %#v", profiles)
			}
			if test.wantSelfCalls == 1 && profiles.currentPassword != "OldPass1" {
				t.Fatalf("current password = %q", profiles.currentPassword)
			}
			if test.wantAdminCalls == 1 && profiles.currentPassword != "" {
				t.Fatalf("admin path received current password %q", profiles.currentPassword)
			}
			assertFullProfile(t, response.GetProfile(), account)
		})
	}
}

func TestServerUpdateProfilePreservesOmittedFields(t *testing.T) {
	targetID := uuid.New()
	profiles := &profileServiceStub{account: profileAccount(targetID)}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{ProfileUpdater: profiles}))

	_, err := h.client.UpdateProfile(identityContext(targetID, "STUDENT"), &userv1.UpdateProfileRequest{
		Uid:      targetID.String(),
		PhoneNum: stringPointer("91234567"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if profiles.username != "" || profiles.currentPassword != "" || profiles.newPassword != "" || profiles.phone != "91234567" {
		t.Fatalf("captured update = %#v", profiles)
	}
}

func TestServerUpdateProfileRejectsAnotherStudent(t *testing.T) {
	targetID := uuid.New()
	profiles := &profileServiceStub{account: profileAccount(targetID)}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{ProfileUpdater: profiles, AdminProfileUpdater: profiles}))

	_, err := h.client.UpdateProfile(identityContext(uuid.New(), "STUDENT"), &userv1.UpdateProfileRequest{Uid: targetID.String(), Username: stringPointer("updated1")})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %s, want %s; error = %v", status.Code(err), codes.PermissionDenied, err)
	}
	if profiles.selfUpdateCalls != 0 || profiles.adminUpdateCalls != 0 {
		t.Fatalf("update calls = (%d, %d), want zero", profiles.selfUpdateCalls, profiles.adminUpdateCalls)
	}
}

func TestServerUpdateProfileMapsDomainFailures(t *testing.T) {
	targetID := uuid.New()
	tests := []struct {
		name       string
		err        error
		wantCode   codes.Code
		wantReason string
	}{
		{name: "incorrect current password", err: user.ErrInvalidCurrentPassword, wantCode: codes.Unauthenticated},
		{name: "duplicate username", err: user.ErrDuplicateUsername, wantCode: codes.AlreadyExists, wantReason: "ErrDuplicateUsername"},
		{name: "missing account", err: user.ErrNotFound, wantCode: codes.NotFound},
		{name: "redis or database unavailable", err: connectionFailure{}, wantCode: codes.Unavailable},
		{name: "unexpected failure", err: errors.New("secret update failure"), wantCode: codes.Internal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := &profileServiceStub{err: test.err}
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{ProfileUpdater: profiles}))
			_, err := h.client.UpdateProfile(identityContext(targetID, "STUDENT"), &userv1.UpdateProfileRequest{Uid: targetID.String(), Username: stringPointer("updated1")})
			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != test.wantCode || errorInfoReason(grpcStatus) != test.wantReason {
				t.Fatalf("status = (%s, %q), want (%s, %q); error = %v", grpcStatus.Code(), errorInfoReason(grpcStatus), test.wantCode, test.wantReason, err)
			}
		})
	}
}

// AI Assistance Disclosure: Codex (GPT-5), 2026-09-29 — verifies legacy
// profile validation sentinels through the complete gRPC path. Author review: validated correctness.
func TestServerUpdateProfileMapsDomainValidationFailures(t *testing.T) {
	targetID := uuid.New()
	tests := []struct {
		name      string
		err       error
		wantField string
	}{
		{name: "invalid username", err: user.ErrInvalidUsername, wantField: "username"},
		{name: "invalid phone", err: user.ErrInvalidPhone, wantField: "phone_num"},
		{name: "invalid new password", err: user.ErrInvalidPassword, wantField: "new_password"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := &profileServiceStub{err: test.err}
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{ProfileUpdater: profiles}))
			_, err := h.client.UpdateProfile(identityContext(targetID, "STUDENT"), &userv1.UpdateProfileRequest{
				Uid: targetID.String(), Username: stringPointer("updated1"), PhoneNum: stringPointer("91234567"), NewPassword: stringPointer("NewPass1"),
			})
			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != codes.InvalidArgument || !hasBadRequestField(grpcStatus, test.wantField) {
				t.Fatalf("status/details = (%s, %#v), want InvalidArgument field %q", grpcStatus.Code(), grpcStatus.Details(), test.wantField)
			}
		})
	}
}

func TestServerUpdateProfileValidationPrecedesDomainService(t *testing.T) {
	tests := []struct {
		name      string
		request   *userv1.UpdateProfileRequest
		wantField string
	}{
		{name: "malformed uid", request: &userv1.UpdateProfileRequest{Uid: "not-a-uuid", Username: stringPointer("updated1")}, wantField: "uid"},
		{name: "invalid username", request: &userv1.UpdateProfileRequest{Uid: uuid.NewString(), Username: stringPointer("invalid username")}, wantField: "username"},
		{name: "phone exceeds database width", request: &userv1.UpdateProfileRequest{Uid: uuid.NewString(), PhoneNum: stringPointer("123456789012345678901")}, wantField: "phone_num"},
		{name: "weak new password", request: &userv1.UpdateProfileRequest{Uid: uuid.NewString(), NewPassword: stringPointer("weakpass")}, wantField: "new_password"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles := &profileServiceStub{}
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{ProfileUpdater: profiles}))
			_, err := h.client.UpdateProfile(identityContext(uuid.New(), "STUDENT"), test.request)
			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != codes.InvalidArgument || !hasBadRequestField(grpcStatus, test.wantField) {
				t.Fatalf("status/details = (%s, %#v), want InvalidArgument field %q", grpcStatus.Code(), grpcStatus.Details(), test.wantField)
			}
			if profiles.selfUpdateCalls != 0 {
				t.Fatalf("update calls = %d, want 0", profiles.selfUpdateCalls)
			}
		})
	}
}

// AI Assistance Disclosure: Codex (GPT-5), 2026-09-29 — preserves missing
// profile dependency failures from the legacy handler. Author review: validated correctness.
func TestServerProfileRPCsReportMissingDependencies(t *testing.T) {
	targetID := uuid.New()
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{}))
	ctx := identityContext(targetID, "STUDENT")
	_, getErr := h.client.GetProfile(ctx, &userv1.GetProfileRequest{Uid: targetID.String()})
	_, updateErr := h.client.UpdateProfile(ctx, &userv1.UpdateProfileRequest{Uid: targetID.String(), Username: stringPointer("updated1")})
	_, adminUpdateErr := h.client.UpdateProfile(identityContext(uuid.New(), "ADMIN"), &userv1.UpdateProfileRequest{Uid: targetID.String(), Username: stringPointer("updated1")})
	if status.Code(getErr) != codes.Internal || status.Code(updateErr) != codes.Internal || status.Code(adminUpdateErr) != codes.Internal {
		t.Fatalf("codes = (%s, %s, %s), want Internal", status.Code(getErr), status.Code(updateErr), status.Code(adminUpdateErr))
	}
}

func identityContext(uid uuid.UUID, role string) context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		interceptors.UserIDMetadataKey, uid.String(),
		interceptors.UserRoleMetadataKey, role,
	))
}

func stringPointer(value string) *string { return &value }

func profileAccount(uid uuid.UUID) *user.User {
	return &user.User{
		UID:           uid,
		Username:      "updated1",
		Email:         "student1@u.nus.edu",
		PhoneNum:      "91234567",
		AccountRole:   user.AccountRoleStudent,
		AccountStatus: user.AccountStatusActive,
		DateCreated:   time.Date(2026, time.September, 29, 4, 30, 0, 0, time.UTC),
	}
}

func assertFullProfile(t *testing.T, got *userv1.UserProfile, want *user.User) {
	t.Helper()
	if got == nil || got.GetUid() != want.UID.String() || got.GetUsername() != want.Username || got.GetEmail() != want.Email || got.GetPhoneNum() != want.PhoneNum {
		t.Fatalf("profile = %#v", got)
	}
	if got.AccountRole == nil || got.GetAccountRole() != userv1.AccountRole_ACCOUNT_ROLE_STUDENT {
		t.Fatalf("account role = %#v", got.AccountRole)
	}
	if got.AccountStatus == nil || got.GetAccountStatus() != userv1.AccountStatus_ACCOUNT_STATUS_ACTIVE {
		t.Fatalf("account status = %#v", got.AccountStatus)
	}
	if got.GetDateCreated() == nil || !got.GetDateCreated().AsTime().Equal(want.DateCreated) {
		t.Fatalf("date created = %v, want %v", got.GetDateCreated(), want.DateCreated)
	}
}
