// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The in-house random forest behind errand ranking (D-087, D-088):
//   training, scoring, per-score explanations and the saved JSON form.
// Author review: PENDING — <reviewer to complete>

// Package forest is the small random-forest classifier errand ranking is built
// on (D-087): CART trees split by Gini impurity, each grown on a bootstrap
// sample of the rows and a random subset of the features at every split.
// It knows nothing about errands. A caller turns what it ranks into a row of
// numbers and a true/false label (D-088), and gets back a score from 0 to 1
// and, for any row, how much each feature moved that score.
package forest

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
)

// Errors a caller can tell apart.
var (
	ErrNoRows       = errors.New("forest: no training rows")
	ErrShape        = errors.New("forest: rows and labels do not line up")
	ErrNonFinite    = errors.New("forest: a feature is NaN or infinite")
	ErrConfig       = errors.New("forest: invalid config")
	ErrFeatureCount = errors.New("forest: wrong number of features")
	ErrCorrupt      = errors.New("forest: saved forest is malformed")
)

// formatVersion is written into every saved forest, so a later change to the
// layout can refuse an old file instead of misreading it.
const formatVersion = 1

// leaf marks a node with no split.
const leaf = -1

// minGain is the smallest impurity decrease worth a split; below it the
// difference is floating-point noise, not signal.
const minGain = 1e-12

// Config sets how a forest is grown.
type Config struct {
	// Trees is how many trees to grow.
	Trees int
	// MaxDepth caps how deep a tree grows; the root is depth 0.
	MaxDepth int
	// MinLeaf is the fewest training rows a leaf may hold.
	MinLeaf int
	// FeaturesPerSplit is how many features each split considers. Zero means
	// the square root of the feature count, rounded up.
	FeaturesPerSplit int
}

// Forest is a trained random forest. Its zero value has no trees and refuses
// every row with ErrFeatureCount.
type Forest struct {
	features   int
	trees      []tree
	importance []float64
}

// tree keeps its nodes in one slice, root first. A node's children always sit
// after it, which is what lets a loaded forest be checked for cycles cheaply.
type tree struct {
	nodes []node
}

type node struct {
	feature   int     // leaf, or the feature this node splits on
	threshold float64 // rows with x[feature] <= threshold go left
	left      int
	right     int
	positive  float64 // share of this node's training rows labelled true
}

// Explanation says why a row got its score. Score equals Base plus the sum of
// Contributions, up to floating-point rounding.
type Explanation struct {
	// Score is the forest's score for the row, from 0 to 1.
	Score float64
	// Base is the score before any split: the share of true labels the trees
	// were trained on.
	Base float64
	// Contributions holds, per feature, how far that feature's splits moved
	// the score from Base. Positive pushes towards true.
	Contributions []float64
}

// Train grows a forest from rows x and their labels y. rng drives the bootstrap
// samples and feature choices, so the same rng seed and inputs grow the same
// forest.
func Train(x [][]float64, y []bool, cfg Config, rng *rand.Rand) (*Forest, error) {
	features, err := checkRows(x, y)
	if err != nil {
		return nil, err
	}
	perSplit, err := checkConfig(cfg, features)
	if err != nil {
		return nil, err
	}
	if rng == nil {
		return nil, fmt.Errorf("%w: rng is nil", ErrConfig)
	}

	g := grower{x: x, y: y, cfg: cfg, perSplit: perSplit, rng: rng, gain: make([]float64, features)}
	f := &Forest{features: features, trees: make([]tree, 0, cfg.Trees)}
	for range cfg.Trees {
		sample := make([]int, len(x))
		for i := range sample {
			sample[i] = rng.IntN(len(x))
		}
		var t tree
		g.grow(&t, sample, 0)
		f.trees = append(f.trees, t)
	}
	f.importance = normalise(g.gain)
	return f, nil
}

// Features is how many features each row must have.
func (f *Forest) Features() int { return f.features }

// Importance returns each feature's share of the impurity decrease across all
// splits, summing to 1. All zeros means no tree split at all.
func (f *Forest) Importance() []float64 { return slices.Clone(f.importance) }

// Score returns the forest's score for row: the mean, over its trees, of the
// share of true labels in the leaf the row reaches.
func (f *Forest) Score(row []float64) (float64, error) {
	if err := f.checkRow(row); err != nil {
		return 0, err
	}
	var sum float64
	for _, t := range f.trees {
		sum += t.nodes[t.reach(row)].positive
	}
	return sum / float64(len(f.trees)), nil
}

// Explain returns row's score and how much each feature moved it. Along a
// row's path through a tree, every split shifts the share of true labels from
// the parent's to the child's; that shift is credited to the split's feature,
// and the credits are averaged over the trees.
func (f *Forest) Explain(row []float64) (Explanation, error) {
	if err := f.checkRow(row); err != nil {
		return Explanation{}, err
	}
	e := Explanation{Contributions: make([]float64, f.features)}
	for _, t := range f.trees {
		at := 0
		e.Base += t.nodes[0].positive
		for t.nodes[at].feature != leaf {
			n := t.nodes[at]
			next := n.right
			if row[n.feature] <= n.threshold {
				next = n.left
			}
			e.Contributions[n.feature] += t.nodes[next].positive - n.positive
			at = next
		}
		e.Score += t.nodes[at].positive
	}
	count := float64(len(f.trees))
	e.Base /= count
	e.Score /= count
	for i := range e.Contributions {
		e.Contributions[i] /= count
	}
	return e, nil
}

func (f *Forest) checkRow(row []float64) error {
	if len(f.trees) == 0 || len(row) != f.features {
		return fmt.Errorf("%w: got %d, want %d", ErrFeatureCount, len(row), f.features)
	}
	for i, v := range row {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("%w: feature %d", ErrNonFinite, i)
		}
	}
	return nil
}

// reach returns the index of the leaf row lands in.
func (t tree) reach(row []float64) int {
	at := 0
	for t.nodes[at].feature != leaf {
		n := t.nodes[at]
		if row[n.feature] <= n.threshold {
			at = n.left
		} else {
			at = n.right
		}
	}
	return at
}

func checkRows(x [][]float64, y []bool) (int, error) {
	if len(x) == 0 {
		return 0, ErrNoRows
	}
	if len(y) != len(x) {
		return 0, fmt.Errorf("%w: %d rows, %d labels", ErrShape, len(x), len(y))
	}
	features := len(x[0])
	if features == 0 {
		return 0, fmt.Errorf("%w: rows have no features", ErrShape)
	}
	for i, row := range x {
		if len(row) != features {
			return 0, fmt.Errorf("%w: row %d has %d features, row 0 has %d", ErrShape, i, len(row), features)
		}
		for j, v := range row {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return 0, fmt.Errorf("%w: row %d, feature %d", ErrNonFinite, i, j)
			}
		}
	}
	return features, nil
}

// checkConfig returns how many features each split considers.
func checkConfig(cfg Config, features int) (int, error) {
	switch {
	case cfg.Trees < 1:
		return 0, fmt.Errorf("%w: Trees must be at least 1", ErrConfig)
	case cfg.MaxDepth < 1:
		return 0, fmt.Errorf("%w: MaxDepth must be at least 1", ErrConfig)
	case cfg.MinLeaf < 1:
		return 0, fmt.Errorf("%w: MinLeaf must be at least 1", ErrConfig)
	case cfg.FeaturesPerSplit < 0 || cfg.FeaturesPerSplit > features:
		return 0, fmt.Errorf("%w: FeaturesPerSplit must be 0 to %d", ErrConfig, features)
	case cfg.FeaturesPerSplit == 0:
		return int(math.Ceil(math.Sqrt(float64(features)))), nil
	default:
		return cfg.FeaturesPerSplit, nil
	}
}

// grower holds what every split while training needs. gain accumulates each
// feature's impurity decrease for Importance.
type grower struct {
	x        [][]float64
	y        []bool
	cfg      Config
	perSplit int
	rng      *rand.Rand
	gain     []float64
}

// grow adds the subtree for rows to t and returns its root's index.
func (g *grower) grow(t *tree, rows []int, depth int) int {
	positives := g.positives(rows)
	at := len(t.nodes)
	t.nodes = append(t.nodes, node{feature: leaf, positive: float64(positives) / float64(len(rows))})

	if depth >= g.cfg.MaxDepth || len(rows) < 2*g.cfg.MinLeaf || positives == 0 || positives == len(rows) {
		return at
	}
	s, ok := g.bestSplit(rows, positives)
	if !ok {
		return at
	}
	var left, right []int
	for _, r := range rows {
		if g.x[r][s.feature] <= s.threshold {
			left = append(left, r)
		} else {
			right = append(right, r)
		}
	}
	g.gain[s.feature] += s.gain
	l := g.grow(t, left, depth+1)
	r := g.grow(t, right, depth+1)
	t.nodes[at].feature, t.nodes[at].threshold = s.feature, s.threshold
	t.nodes[at].left, t.nodes[at].right = l, r
	return at
}

func (g *grower) positives(rows []int) int {
	n := 0
	for _, r := range rows {
		if g.y[r] {
			n++
		}
	}
	return n
}

type split struct {
	feature   int
	threshold float64
	gain      float64
}

// bestSplit tries perSplit randomly chosen features and returns the split with
// the lowest weighted Gini impurity that leaves at least MinLeaf rows on each
// side. ok is false when no split lowers the impurity.
func (g *grower) bestSplit(rows []int, positives int) (split, bool) {
	parent := weightedGini(positives, len(rows))
	best := split{gain: minGain}
	found := false
	sorted := slices.Clone(rows)
	for _, f := range g.rng.Perm(len(g.x[0]))[:g.perSplit] {
		// Sorting by row index too makes the order, and so the forest,
		// independent of how the sort breaks ties.
		slices.SortFunc(sorted, func(a, b int) int {
			return cmp.Or(cmp.Compare(g.x[a][f], g.x[b][f]), cmp.Compare(a, b))
		})
		leftPos := 0
		for k := 0; k < len(sorted)-1; k++ {
			if g.y[sorted[k]] {
				leftPos++
			}
			lo, hi := g.x[sorted[k]][f], g.x[sorted[k+1]][f]
			if lo == hi {
				continue
			}
			nl := k + 1
			nr := len(sorted) - nl
			if nl < g.cfg.MinLeaf || nr < g.cfg.MinLeaf {
				continue
			}
			gain := parent - weightedGini(leftPos, nl) - weightedGini(positives-leftPos, nr)
			if gain > best.gain {
				threshold := lo + (hi-lo)/2
				if threshold >= hi { // lo and hi are adjacent floats
					threshold = lo
				}
				best = split{feature: f, threshold: threshold, gain: gain}
				found = true
			}
		}
	}
	return best, found
}

// weightedGini is n times the Gini impurity of n rows of which positives are
// true, so a parent's and its children's values can be compared directly.
func weightedGini(positives, n int) float64 {
	if n == 0 {
		return 0
	}
	p, q := float64(positives), float64(n-positives)
	return float64(n) - (p*p+q*q)/float64(n)
}

func normalise(gain []float64) []float64 {
	var total float64
	for _, v := range gain {
		total += v
	}
	out := make([]float64, len(gain))
	if total == 0 {
		return out
	}
	for i, v := range gain {
		out[i] = v / total
	}
	return out
}

// savedForest is the JSON form of a Forest, one per courier (D-087).
type savedForest struct {
	Version    int           `json:"version"`
	Features   int           `json:"features"`
	Importance []float64     `json:"importance"`
	Trees      [][]savedNode `json:"trees"`
}

// savedNode keeps keys short because a forest saves every node of every tree.
type savedNode struct {
	Feature   int     `json:"f"`
	Threshold float64 `json:"t,omitempty"`
	Left      int     `json:"l,omitempty"`
	Right     int     `json:"r,omitempty"`
	Positive  float64 `json:"p"`
}

// MarshalJSON saves the forest.
func (f *Forest) MarshalJSON() ([]byte, error) {
	s := savedForest{Version: formatVersion, Features: f.features, Importance: f.importance, Trees: make([][]savedNode, len(f.trees))}
	for i, t := range f.trees {
		s.Trees[i] = make([]savedNode, len(t.nodes))
		for j, n := range t.nodes {
			s.Trees[i][j] = savedNode{Feature: n.feature, Threshold: n.threshold, Left: n.left, Right: n.right, Positive: n.positive}
		}
	}
	return json.Marshal(s)
}

// UnmarshalJSON loads a saved forest, refusing one whose trees could send a
// row out of range or round in a loop.
func (f *Forest) UnmarshalJSON(data []byte) error {
	var s savedForest
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if s.Version != formatVersion {
		return fmt.Errorf("%w: version %d, want %d", ErrCorrupt, s.Version, formatVersion)
	}
	if s.Features < 1 || len(s.Importance) != s.Features || len(s.Trees) == 0 {
		return fmt.Errorf("%w: %d features, %d importances, %d trees", ErrCorrupt, s.Features, len(s.Importance), len(s.Trees))
	}
	trees := make([]tree, len(s.Trees))
	for i, saved := range s.Trees {
		if len(saved) == 0 {
			return fmt.Errorf("%w: tree %d is empty", ErrCorrupt, i)
		}
		nodes := make([]node, len(saved))
		for j, n := range saved {
			if n.Positive < 0 || n.Positive > 1 {
				return fmt.Errorf("%w: tree %d, node %d: share %v", ErrCorrupt, i, j, n.Positive)
			}
			if n.Feature != leaf {
				// Children after their parent: no node can lead back to itself.
				if n.Feature < 0 || n.Feature >= s.Features ||
					n.Left <= j || n.Left >= len(saved) || n.Right <= j || n.Right >= len(saved) {
					return fmt.Errorf("%w: tree %d, node %d does not split cleanly", ErrCorrupt, i, j)
				}
			}
			nodes[j] = node{feature: n.Feature, threshold: n.Threshold, left: n.Left, right: n.Right, positive: n.Positive}
		}
		trees[i] = tree{nodes: nodes}
	}
	*f = Forest{features: s.Features, trees: trees, importance: s.Importance}
	return nil
}
