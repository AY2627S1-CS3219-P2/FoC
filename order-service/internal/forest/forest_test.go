// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Unit tests for the random forest (D-087): datasets with known
//   answers, explanations, determinism, the saved form and every error path.
// Author review: PENDING — <reviewer to complete>

package forest_test

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/forest"
)

var small = forest.Config{Trees: 50, MaxDepth: 6, MinLeaf: 1}

func seeded(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, seed+1)) }

// dataset draws n rows of width features in [0, 1) and labels each with label.
func dataset(n, width int, seed uint64, label func(row []float64, rng *rand.Rand) bool) ([][]float64, []bool) {
	rng := seeded(seed)
	x := make([][]float64, n)
	y := make([]bool, n)
	for i := range x {
		x[i] = make([]float64, width)
		for j := range x[i] {
			x[i][j] = rng.Float64()
		}
		y[i] = label(x[i], rng)
	}
	return x, y
}

func train(t *testing.T, x [][]float64, y []bool, cfg forest.Config) *forest.Forest {
	t.Helper()
	f, err := forest.Train(x, y, cfg, seeded(7))
	require.NoError(t, err)
	return f
}

func score(t *testing.T, f *forest.Forest, row ...float64) float64 {
	t.Helper()
	s, err := f.Score(row)
	require.NoError(t, err)
	return s
}

func TestSeparableDataScoresNearItsLabels(t *testing.T) {
	x, y := dataset(200, 1, 1, func(r []float64, _ *rand.Rand) bool { return r[0] >= 0.5 })
	f := train(t, x, y, small)

	assert.Less(t, score(t, f, 0.1), 0.1)
	assert.Less(t, score(t, f, 0.4), 0.1)
	assert.Greater(t, score(t, f, 0.6), 0.9)
	assert.Greater(t, score(t, f, 0.9), 0.9)
}

func TestTheOneFeatureThatDecidesGetsTheImportanceAndTheCredit(t *testing.T) {
	x, y := dataset(300, 3, 2, func(r []float64, _ *rand.Rand) bool { return r[1] > 0.5 })
	f := train(t, x, y, forest.Config{Trees: 60, MaxDepth: 6, MinLeaf: 2})

	imp := f.Importance()
	require.Len(t, imp, 3)
	assert.Greater(t, imp[1], imp[0]+imp[2], "importance %v", imp)

	e, err := f.Explain([]float64{0.5, 0.9, 0.5})
	require.NoError(t, err)
	assert.Greater(t, e.Contributions[1], 0.0)
	for _, other := range []int{0, 2} {
		assert.Greater(t, e.Contributions[1], math.Abs(e.Contributions[other]), "contributions %v", e.Contributions)
	}
}

func TestNoiseScoresNearTheBaseRate(t *testing.T) {
	x, y := dataset(400, 2, 3, func(_ []float64, rng *rand.Rand) bool { return rng.IntN(2) == 0 })
	f := train(t, x, y, forest.Config{Trees: 80, MaxDepth: 6, MinLeaf: 5})

	probe, _ := dataset(200, 2, 4, func([]float64, *rand.Rand) bool { return false })
	var sum float64
	for _, row := range probe {
		sum += score(t, f, row...)
	}
	assert.InDelta(t, 0.5, sum/float64(len(probe)), 0.1)
}

func TestAnExplanationAddsUpToTheScore(t *testing.T) {
	x, y := dataset(200, 4, 5, func(r []float64, _ *rand.Rand) bool { return r[0]+r[2] > 1 })
	f := train(t, x, y, small)

	probe, _ := dataset(20, 4, 6, func([]float64, *rand.Rand) bool { return false })
	for _, row := range probe {
		e, err := f.Explain(row)
		require.NoError(t, err)
		sum := e.Base
		for _, c := range e.Contributions {
			sum += c
		}
		assert.InDelta(t, e.Score, sum, 1e-9)
		assert.InDelta(t, score(t, f, row...), e.Score, 1e-12)
	}
}

func TestOneClassScoresThatClassWithNoSplits(t *testing.T) {
	for _, label := range []bool{true, false} {
		x, _ := dataset(30, 2, 8, func([]float64, *rand.Rand) bool { return false })
		y := make([]bool, len(x))
		for i := range y {
			y[i] = label
		}
		f := train(t, x, y, small)

		want := 0.0
		if label {
			want = 1
		}
		e, err := f.Explain([]float64{0.3, 0.7})
		require.NoError(t, err)
		assert.Equal(t, want, e.Score)
		assert.Equal(t, []float64{0, 0}, e.Contributions)
		assert.Equal(t, []float64{0, 0}, f.Importance())
	}
}

func TestIdenticalFeaturesCannotSplit(t *testing.T) {
	x := [][]float64{{1, 2}, {1, 2}, {1, 2}, {1, 2}}
	y := []bool{true, false, true, false}
	f := train(t, x, y, small)

	assert.Equal(t, []float64{0, 0}, f.Importance())
	assert.InDelta(t, 0.5, score(t, f, 1, 2), 0.2, "only the bootstrap samples move it from 0.5")
}

func TestMinLeafKeepsLeavesFromHoldingFewerRows(t *testing.T) {
	// Two rows can never be split into leaves of at least two rows each.
	f := train(t, [][]float64{{0}, {1}}, []bool{false, true}, forest.Config{Trees: 10, MaxDepth: 4, MinLeaf: 2})
	assert.Equal(t, []float64{0}, f.Importance())
}

func TestTheSameSeedGrowsTheSameForest(t *testing.T) {
	x, y := dataset(100, 3, 9, func(r []float64, _ *rand.Rand) bool { return r[0] > r[1] })
	a, err := forest.Train(x, y, small, seeded(42))
	require.NoError(t, err)
	b, err := forest.Train(x, y, small, seeded(42))
	require.NoError(t, err)

	ja, err := json.Marshal(a)
	require.NoError(t, err)
	jb, err := json.Marshal(b)
	require.NoError(t, err)
	assert.JSONEq(t, string(ja), string(jb))
}

func TestASavedForestScoresTheSameWhenLoaded(t *testing.T) {
	x, y := dataset(150, 3, 10, func(r []float64, _ *rand.Rand) bool { return r[2] > 0.3 })
	f := train(t, x, y, small)

	saved, err := json.Marshal(f)
	require.NoError(t, err)
	var loaded forest.Forest
	require.NoError(t, json.Unmarshal(saved, &loaded))

	assert.Equal(t, f.Features(), loaded.Features())
	assert.Equal(t, f.Importance(), loaded.Importance())
	probe, _ := dataset(20, 3, 11, func([]float64, *rand.Rand) bool { return false })
	for _, row := range probe {
		assert.Equal(t, score(t, f, row...), score(t, &loaded, row...))
	}
}

func TestTrainRefusesBadInput(t *testing.T) {
	nan := math.NaN()
	ok := forest.Config{Trees: 1, MaxDepth: 1, MinLeaf: 1}
	cases := []struct {
		name string
		x    [][]float64
		y    []bool
		cfg  forest.Config
		want error
	}{
		{"no rows", nil, nil, ok, forest.ErrNoRows},
		{"fewer labels than rows", [][]float64{{1}, {2}}, []bool{true}, ok, forest.ErrShape},
		{"rows with no features", [][]float64{{}}, []bool{true}, ok, forest.ErrShape},
		{"ragged rows", [][]float64{{1, 2}, {3}}, []bool{true, false}, ok, forest.ErrShape},
		{"NaN feature", [][]float64{{nan}}, []bool{true}, ok, forest.ErrNonFinite},
		{"infinite feature", [][]float64{{math.Inf(1)}}, []bool{true}, ok, forest.ErrNonFinite},
		{"no trees", [][]float64{{1}}, []bool{true}, forest.Config{MaxDepth: 1, MinLeaf: 1}, forest.ErrConfig},
		{"no depth", [][]float64{{1}}, []bool{true}, forest.Config{Trees: 1, MinLeaf: 1}, forest.ErrConfig},
		{"no leaf size", [][]float64{{1}}, []bool{true}, forest.Config{Trees: 1, MaxDepth: 1}, forest.ErrConfig},
		{"too many features per split", [][]float64{{1}}, []bool{true}, forest.Config{Trees: 1, MaxDepth: 1, MinLeaf: 1, FeaturesPerSplit: 2}, forest.ErrConfig},
		{"negative features per split", [][]float64{{1}}, []bool{true}, forest.Config{Trees: 1, MaxDepth: 1, MinLeaf: 1, FeaturesPerSplit: -1}, forest.ErrConfig},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := forest.Train(c.x, c.y, c.cfg, seeded(1))
			assert.ErrorIs(t, err, c.want)
		})
	}
	t.Run("nil rng", func(t *testing.T) {
		_, err := forest.Train([][]float64{{1}}, []bool{true}, ok, nil)
		assert.ErrorIs(t, err, forest.ErrConfig)
	})
}

func TestScoreAndExplainRefuseBadRows(t *testing.T) {
	f := train(t, [][]float64{{0, 0}, {1, 1}}, []bool{false, true}, small)
	cases := []struct {
		name string
		f    *forest.Forest
		row  []float64
		want error
	}{
		{"too few features", f, []float64{1}, forest.ErrFeatureCount},
		{"too many features", f, []float64{1, 2, 3}, forest.ErrFeatureCount},
		{"NaN", f, []float64{1, math.NaN()}, forest.ErrNonFinite},
		{"untrained forest", &forest.Forest{}, nil, forest.ErrFeatureCount},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.f.Score(c.row)
			assert.ErrorIs(t, err, c.want)
			_, err = c.f.Explain(c.row)
			assert.ErrorIs(t, err, c.want)
		})
	}
}

func TestLoadingRefusesAMalformedForest(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"fields of the wrong type", `{"version":"one","features":1}`},
		{"another version", `{"version":2,"features":1,"importance":[0],"trees":[[{"f":-1,"p":0.5}]]}`},
		{"no features", `{"version":1,"features":0,"importance":[],"trees":[[{"f":-1,"p":0.5}]]}`},
		{"importance of the wrong length", `{"version":1,"features":2,"importance":[1],"trees":[[{"f":-1,"p":0.5}]]}`},
		{"no trees", `{"version":1,"features":1,"importance":[0],"trees":[]}`},
		{"an empty tree", `{"version":1,"features":1,"importance":[0],"trees":[[]]}`},
		{"a share above 1", `{"version":1,"features":1,"importance":[0],"trees":[[{"f":-1,"p":1.5}]]}`},
		{"a split on a missing feature", `{"version":1,"features":1,"importance":[1],"trees":[[{"f":3,"l":1,"r":2,"p":0.5},{"f":-1,"p":0},{"f":-1,"p":1}]]}`},
		{"a child out of range", `{"version":1,"features":1,"importance":[1],"trees":[[{"f":0,"l":1,"r":9,"p":0.5},{"f":-1,"p":0}]]}`},
		{"a loop back to the root", `{"version":1,"features":1,"importance":[1],"trees":[[{"f":0,"l":0,"r":1,"p":0.5},{"f":-1,"p":1}]]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var f forest.Forest
			err := json.Unmarshal([]byte(c.json), &f)
			require.Error(t, err)
			assert.True(t, errors.Is(err, forest.ErrCorrupt), "got %v", err)
		})
	}
}
