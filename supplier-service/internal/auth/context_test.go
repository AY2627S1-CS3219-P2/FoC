// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Tests the caller and token carried on a request's context.
// Author review: PENDING — <reviewer to complete>

package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallerOnTheContext(t *testing.T) {
	_, ok := CallerFromContext(context.Background())
	assert.False(t, ok)

	want := Caller{UserID: uuid.New(), Role: "ADMIN"}
	got, ok := CallerFromContext(ContextWithCaller(context.Background(), want))
	require.True(t, ok)
	assert.Equal(t, want, got)
}

func TestTokenOnTheContext(t *testing.T) {
	_, ok := TokenFromContext(context.Background())
	assert.False(t, ok)
	_, ok = TokenFromContext(ContextWithToken(context.Background(), ""))
	assert.False(t, ok, "an empty token is no token")

	got, ok := TokenFromContext(ContextWithToken(context.Background(), "abc"))
	require.True(t, ok)
	assert.Equal(t, "abc", got)
}
