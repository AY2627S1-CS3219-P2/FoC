// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-09-30
// Scope: New file. Covers EnableReflection's default and override, added
//   in response to a Copilot review finding on PR #8.
// Author review: PENDING — <reviewer to complete>

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad_EnableReflection_DefaultsTrue(t *testing.T) {
	t.Setenv("SUPPLIER_DISABLE_REFLECTION", "")
	assert.True(t, Load().EnableReflection)
}

func TestLoad_EnableReflection_DisabledWhenSet(t *testing.T) {
	t.Setenv("SUPPLIER_DISABLE_REFLECTION", "1")
	assert.False(t, Load().EnableReflection)
}
