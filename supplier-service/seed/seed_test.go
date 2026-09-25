package seed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromCSV_ParsesTemplateSeedData(t *testing.T) {
	suppliers, err := FromCSV("../../data/csv/supplier-seed-data.csv")
	require.NoError(t, err)
	require.NotEmpty(t, suppliers)

	var found bool
	for _, s := range suppliers {
		if s.Name == "Octobox" {
			found = true
			assert.Equal(t, "Prince George’s Park", s.Building, "Windows-1252 apostrophe should decode cleanly")
		}
		assert.NotEmpty(t, s.Name)
		assert.NotEmpty(t, s.Type)
		assert.NotEmpty(t, s.Building)
	}
	assert.True(t, found, "expected to find Octobox in seed data")
}

func TestNormalizeHours(t *testing.T) {
	assert.Equal(t, "09:00", normalizeHours("0900hrs"))
	assert.Equal(t, "23:59", normalizeHours("2359hrs"))
	assert.Equal(t, "", normalizeHours(""))
}
