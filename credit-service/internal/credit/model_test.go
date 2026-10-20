// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Unit tests for checked arithmetic, limits validation, and domain error matching.
// Author review: PENDING - reviewer to complete

package credit

import (
	"errors"
	"math"
	"testing"
)

func TestCheckAdd(t *testing.T) {
	tests := []struct {
		name    string
		a, b    int64
		want    int64
		wantErr error
	}{
		{"positive happy path", 100, 200, 300, nil},
		{"zero", 0, 0, 0, nil},
		{"negative happy path", -50, -50, -100, nil},
		{"positive overflow", math.MaxInt64, 1, 0, ErrArithmeticOverflow},
		{"negative overflow", math.MinInt64, -1, 0, ErrArithmeticOverflow},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CheckAdd(tc.a, tc.b)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("CheckAdd(%d, %d) error = %v, want %v", tc.a, tc.b, err, tc.wantErr)
			}
			if tc.wantErr == nil && got != tc.want {
				t.Fatalf("CheckAdd(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestCheckSub(t *testing.T) {
	tests := []struct {
		name    string
		a, b    int64
		want    int64
		wantErr error
	}{
		{"happy path", 500, 200, 300, nil},
		{"negative result", 100, 200, -100, nil},
		{"overflow negative", math.MinInt64, 1, 0, ErrArithmeticOverflow},
		{"overflow positive", math.MaxInt64, -1, 0, ErrArithmeticOverflow},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CheckSub(tc.a, tc.b)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("CheckSub(%d, %d) error = %v, want %v", tc.a, tc.b, err, tc.wantErr)
			}
			if tc.wantErr == nil && got != tc.want {
				t.Fatalf("CheckSub(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestValidateRewardAmount(t *testing.T) {
	if err := ValidateRewardAmount(0); !errors.Is(err, ErrInvalidRewardAmount) {
		t.Fatalf("ValidateRewardAmount(0) expected error, got %v", err)
	}
	if err := ValidateRewardAmount(-1); !errors.Is(err, ErrInvalidRewardAmount) {
		t.Fatalf("ValidateRewardAmount(-1) expected error, got %v", err)
	}
	if err := ValidateRewardAmount(MaxErrandReward + 1); !errors.Is(err, ErrInvalidRewardAmount) {
		t.Fatalf("ValidateRewardAmount(over max) expected error, got %v", err)
	}
	if err := ValidateRewardAmount(MinErrandReward); err != nil {
		t.Fatalf("ValidateRewardAmount(min) unexpected error: %v", err)
	}
	if err := ValidateRewardAmount(MaxErrandReward); err != nil {
		t.Fatalf("ValidateRewardAmount(max) unexpected error: %v", err)
	}
}

func TestValidateBucketBalance(t *testing.T) {
	if err := ValidateBucketBalance(-1); !errors.Is(err, ErrCreditLimitExceeded) {
		t.Fatalf("ValidateBucketBalance(-1) expected error, got %v", err)
	}
	if err := ValidateBucketBalance(MaxBalanceCredits + 1); !errors.Is(err, ErrCreditLimitExceeded) {
		t.Fatalf("ValidateBucketBalance(over max) expected error, got %v", err)
	}
	if err := ValidateBucketBalance(MinBalanceCredits); err != nil {
		t.Fatalf("ValidateBucketBalance(0) unexpected error: %v", err)
	}
	if err := ValidateBucketBalance(MaxBalanceCredits); err != nil {
		t.Fatalf("ValidateBucketBalance(max) unexpected error: %v", err)
	}
}

