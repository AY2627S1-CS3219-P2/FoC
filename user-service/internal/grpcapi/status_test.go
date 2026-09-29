// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Added in-process gRPC behavior tests for administrative status updates.
// Author review: ZI YANG - validated correctness

package grpcapi_test

import (
	"context"
	"errors"
	"testing"

	userv1 "foc/user-service/internal/gen/user/v1"
	"foc/user-service/internal/grpcapi"
	"foc/user-service/internal/user"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type statusUpdaterStub struct {
	calls  int
	uid    uuid.UUID
	status user.AccountStatus
	err    error
}

func (s *statusUpdaterStub) UpdateAccountStatus(_ context.Context, uid uuid.UUID, accountStatus user.AccountStatus) error {
	s.calls++
	s.uid = uid
	s.status = accountStatus
	return s.err
}

func TestServerUpdateStatusDelegatesRecordedTransitions(t *testing.T) {
	targetID := uuid.New()
	tests := []struct {
		name       string
		request    userv1.AccountStatus
		wantStatus user.AccountStatus
	}{
		{name: "suspend", request: userv1.AccountStatus_ACCOUNT_STATUS_SUSPENDED, wantStatus: user.AccountStatusSuspended},
		{name: "reactivate", request: userv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, wantStatus: user.AccountStatusActive},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updater := &statusUpdaterStub{}
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{StatusUpdater: updater}))
			response, err := h.client.UpdateStatus(identityContext(uuid.New(), "ADMIN"), &userv1.UpdateStatusRequest{
				Uid:    targetID.String(),
				Status: test.request,
			})
			if err != nil {
				t.Fatal(err)
			}
			if response == nil {
				t.Fatal("UpdateStatus response is nil")
			}
			if updater.calls != 1 || updater.uid != targetID || updater.status != test.wantStatus {
				t.Fatalf("update = (%d, %s, %q), want (1, %s, %q)", updater.calls, updater.uid, updater.status, targetID, test.wantStatus)
			}
		})
	}
}

func TestServerUpdateStatusRequiresAdministrator(t *testing.T) {
	targetID := uuid.New()
	updater := &statusUpdaterStub{}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{StatusUpdater: updater}))
	request := &userv1.UpdateStatusRequest{Uid: targetID.String(), Status: userv1.AccountStatus_ACCOUNT_STATUS_SUSPENDED}

	_, unauthenticatedErr := h.client.UpdateStatus(context.Background(), request)
	_, forbiddenErr := h.client.UpdateStatus(identityContext(uuid.New(), "STUDENT"), request)
	if status.Code(unauthenticatedErr) != codes.Unauthenticated || status.Code(forbiddenErr) != codes.PermissionDenied {
		t.Fatalf("codes = (%s, %s), want (Unauthenticated, PermissionDenied)", status.Code(unauthenticatedErr), status.Code(forbiddenErr))
	}
	if updater.calls != 0 {
		t.Fatalf("update calls = %d, want 0", updater.calls)
	}
}

func TestServerUpdateStatusValidationPrecedesDomainService(t *testing.T) {
	updater := &statusUpdaterStub{}
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{StatusUpdater: updater}))
	ctx := identityContext(uuid.New(), "ADMIN")

	_, invalidIDErr := h.client.UpdateStatus(ctx, &userv1.UpdateStatusRequest{Uid: "not-a-uuid", Status: userv1.AccountStatus_ACCOUNT_STATUS_ACTIVE})
	_, unspecifiedErr := h.client.UpdateStatus(ctx, &userv1.UpdateStatusRequest{Uid: uuid.NewString(), Status: userv1.AccountStatus_ACCOUNT_STATUS_UNSPECIFIED})
	// AI-assisted (Codex GPT-5, 2026-09-29; review: validated correctness): retain legacy
	// status-input field attribution through gRPC validation.
	invalidIDStatus := status.Convert(invalidIDErr)
	unspecifiedStatus := status.Convert(unspecifiedErr)
	if invalidIDStatus.Code() != codes.InvalidArgument || !hasBadRequestField(invalidIDStatus, "uid") || unspecifiedStatus.Code() != codes.InvalidArgument || !hasBadRequestField(unspecifiedStatus, "status") {
		t.Fatalf("codes = (%s, %s), want InvalidArgument", status.Code(invalidIDErr), status.Code(unspecifiedErr))
	}
	if updater.calls != 0 {
		t.Fatalf("update calls = %d, want 0", updater.calls)
	}
}

func TestServerUpdateStatusMapsDomainFailures(t *testing.T) {
	targetID := uuid.New()
	tests := []struct {
		name       string
		err        error
		wantCode   codes.Code
		wantReason string
	}{
		{name: "missing account", err: user.ErrNotFound, wantCode: codes.NotFound},
		{name: "last active admin", err: user.ErrLastAdmin, wantCode: codes.FailedPrecondition, wantReason: "ErrLastAdmin"},
		{name: "redis or database unavailable", err: connectionFailure{}, wantCode: codes.Unavailable},
		{name: "unexpected failure", err: errors.New("secret status failure"), wantCode: codes.Internal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updater := &statusUpdaterStub{err: test.err}
			h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{StatusUpdater: updater}))
			_, err := h.client.UpdateStatus(identityContext(uuid.New(), "ADMIN"), &userv1.UpdateStatusRequest{
				Uid:    targetID.String(),
				Status: userv1.AccountStatus_ACCOUNT_STATUS_SUSPENDED,
			})
			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != test.wantCode || errorInfoReason(grpcStatus) != test.wantReason {
				t.Fatalf("status = (%s, %q), want (%s, %q); error = %v", grpcStatus.Code(), errorInfoReason(grpcStatus), test.wantCode, test.wantReason, err)
			}
		})
	}
}

func TestServerUpdateStatusMissingDependencyReturnsInternal(t *testing.T) {
	h := newHarness(t, grpcapi.NewServer(grpcapi.Dependencies{}))
	_, err := h.client.UpdateStatus(identityContext(uuid.New(), "ADMIN"), &userv1.UpdateStatusRequest{
		Uid:    uuid.NewString(),
		Status: userv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %s, want %s; error = %v", status.Code(err), codes.Internal, err)
	}
}
