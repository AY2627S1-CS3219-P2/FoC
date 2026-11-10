// AI Assistance Disclosure:
// Tool: Claude Code (model: Fable 5), date: 2026-11-10
// Scope: Reads the expectations dataset in testdata/expectations, trains the
//   recommender on each made-up courier's history, and reports how often it
//   recommends the errand that courier would take. It reports and sets no pass
//   mark.
// Author review: PENDING — <reviewer to complete>

package ranking

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/order/ordertest"
	"foc/order-service/internal/shuttle"
)

// expectationTrainings is how many times each courier's model is trained, each from a
// different random seed, since a forest grows differently every time.
const expectationTrainings = 20

// expectationSizes are the amounts of history a courier is given: the newest
// 12, 24, 48 or all 96 of the errands they were shown.
var expectationSizes = []int{12, 24, 48, 96}

// outcome is how one recommendation compared with what the persona would do.
type outcome int

const (
	pickedRight outcome = iota // recommended the errand they would take
	pickedOther                // recommended another errand
	saidNothing                // recommended nothing though they would take one
	stayedQuiet                // recommended nothing, and they would take none
	spokeAnyway                // recommended one though they would take none
	outcomes
)

// agrees reports whether o matches the persona.
func (o outcome) agrees() bool { return o == pickedRight || o == stayedQuiet }

// probeList is one probe: a moment, the errands on the list, and which one
// the persona would take.
type probeList struct {
	ID       string
	At       time.Time
	Carrying *errand
	Errands  []errand
	// Expected indexes Errands; -1 when the persona would take none.
	Expected int
}

// readCSV returns the rows of a dataset file under its expected header.
func readCSV(t *testing.T, name string, header []string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(expectationsDir, name))
	require.NoError(t, err)
	rows, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	require.NoError(t, err, name)
	require.NotEmpty(t, rows, name)
	require.Equal(t, header, rows[0], "%s: header", name)
	return rows[1:]
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	require.NoError(t, err)
	return n
}

func parseStamp(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return at
}

// loadExpectations reads the dataset back from its files.
func loadExpectations(t *testing.T) dataset {
	t.Helper()
	suppliers := map[string]benchSupplier{}
	for _, r := range readCSV(t, "suppliers.csv", suppliersHeader) {
		lat, err := strconv.ParseFloat(r[2], 64)
		require.NoError(t, err)
		lon, err := strconv.ParseFloat(r[3], 64)
		require.NoError(t, err)
		suppliers[r[0]] = benchSupplier{Name: r[0], Type: r[1], Point: shuttle.Point{Lat: lat, Lon: lon}}
	}
	places := placesByName()
	errandOf := func(supplier, drop, reward, posted, requester, stars string) errand {
		s, ok := suppliers[supplier]
		require.True(t, ok, "supplier %q is not in suppliers.csv", supplier)
		d, ok := places[drop]
		require.True(t, ok, "%q is not a delivery location", drop)
		return errand{Supplier: s, Drop: d, Reward: atoi(t, reward), PostedAgo: atoi(t, posted), Requester: requester, Stars: atoi(t, stars)}
	}

	var d dataset
	for _, r := range readCSV(t, "history.csv", historyHeader) {
		d.History = append(d.History, historyRow{
			Persona: r[0], ShownAt: parseStamp(t, r[1]), errand: errandOf(r[3], r[4], r[5], r[6], r[7], r[8]),
			Accepted: r[9] == "yes", DeliveredAfter: atoi(t, r[10]), CourierStars: atoi(t, r[11]),
		})
	}
	for _, r := range readCSV(t, "searches.csv", searchesHeader) {
		d.Searches = append(d.Searches, searchRow{Persona: r[0], At: parseStamp(t, r[1]), To: r[2]})
	}
	for _, r := range readCSV(t, "probes.csv", probesHeader) {
		row := probeRow{Persona: r[0], Probe: r[1], At: parseStamp(t, r[2]), errand: errandOf(r[6], r[7], r[8], r[9], r[10], r[11]), Expected: r[12] == "yes"}
		if r[4] != "" {
			// Only where a carried errand takes the courier matters.
			carried := errandOf(r[4], r[5], "", "", "", "")
			row.Carrying = &carried
		}
		d.Probes = append(d.Probes, row)
	}
	return d
}

// probeLists groups a persona's probe rows into lists, earliest first.
func probeLists(t *testing.T, d dataset, persona string) []probeList {
	t.Helper()
	var lists []probeList
	index := map[string]int{}
	for _, r := range d.Probes {
		if r.Persona != persona {
			continue
		}
		i, ok := index[r.Probe]
		if !ok {
			i = len(lists)
			index[r.Probe] = i
			lists = append(lists, probeList{ID: r.Probe, At: r.At, Carrying: r.Carrying, Expected: -1})
		}
		if r.Expected {
			require.Equal(t, -1, lists[i].Expected, "probe %s expects more than one errand", r.Probe)
			lists[i].Expected = len(lists[i].Errands)
		}
		lists[i].Errands = append(lists[i].Errands, r.errand)
	}
	slices.SortStableFunc(lists, func(a, b probeList) int { return a.At.Compare(b.At) })
	return lists
}

func benchID(kind, name string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(kind+":"+name))
}

func (e errand) candidate(shownAt time.Time) Candidate {
	return Candidate{
		OrderID: uuid.New(), RequesterID: benchID("requester", e.Requester), Reward: e.Reward,
		CreatedAt:  shownAt.Add(-time.Duration(e.PostedAgo) * time.Minute),
		SupplierID: benchID("supplier", e.Supplier.Name), SupplierName: e.Supplier.Name, SupplierType: e.Supplier.Type,
		Pickup: &e.Supplier.Point, DeliveryLocation: e.Drop.Name,
	}
}

// sourceFor is what order-service's database would hold for a courier whose
// whole record is the newest size errands of the persona's history.
func sourceFor(d dataset, persona string, size int) *fakeSource {
	var rows []historyRow
	for _, r := range d.History {
		if r.Persona == persona {
			rows = append(rows, r)
		}
	}
	slices.SortStableFunc(rows, func(a, b historyRow) int { return b.ShownAt.Compare(a.ShownAt) })
	rows = rows[:min(size, len(rows))]

	src := &fakeSource{ratings: map[uuid.UUID][]Rating{}}
	for requester, stars := range benchRequesters() {
		if stars > 0 {
			src.ratings[benchID("requester", requester)] = []Rating{{Stars: stars, At: firstMonday.AddDate(-1, 0, 0)}}
		}
	}
	for _, r := range rows {
		c := r.candidate(r.ShownAt)
		src.examples = append(src.examples, Example{Candidate: c, ShownAt: r.ShownAt, Accepted: r.Accepted})
		if !r.Accepted {
			continue
		}
		accepted := r.ShownAt.Add(time.Minute)
		delivered := accepted.Add(time.Duration(r.DeliveredAfter) * time.Minute)
		src.history.Accepts = append(src.history.Accepts, Accept{
			OrderID: c.OrderID, At: accepted, SupplierType: c.SupplierType, Until: &delivered,
			Pickup: c.Pickup, DeliveryLocation: c.DeliveryLocation,
		})
		src.history.Completions = append(src.history.Completions, delivered)
		if r.CourierStars > 0 {
			src.history.Ratings = append(src.history.Ratings, Rating{Stars: r.CourierStars, At: delivered.Add(5 * time.Minute), SupplierType: c.SupplierType})
		}
	}
	for _, s := range d.Searches {
		if s.Persona == persona {
			src.history.Searches = append(src.history.Searches, Search{To: s.To, At: s.At})
		}
	}
	return src
}

// ask puts one probe to the recommender and says how its answer compares.
func ask(t *testing.T, src *fakeSource, models ModelStore, seed uint64, p probeList) outcome {
	t.Helper()
	if p.Carrying != nil {
		// The courier accepted this a few minutes ago and still holds it.
		c := p.Carrying.candidate(p.At)
		carrying := *src
		carrying.history.Accepts = append(slices.Clone(src.history.Accepts), Accept{
			OrderID: c.OrderID, At: p.At.Add(-6 * time.Minute), SupplierType: c.SupplierType,
			Pickup: c.Pickup, DeliveryLocation: c.DeliveryLocation,
		})
		src = &carrying
	}
	open := make([]order.Order, len(p.Errands))
	for i, e := range p.Errands {
		c := e.candidate(p.At)
		open[i] = order.Order{
			ID: c.OrderID, RequesterID: c.RequesterID, Reward: c.Reward, State: order.StateOpen, CreatedAt: c.CreatedAt,
			Supplier:         order.SupplierSnapshot{ID: c.SupplierID, Name: c.SupplierName, Type: c.SupplierType, Point: c.Pickup},
			DeliveryLocation: c.DeliveryLocation,
		}
	}
	r := NewRecommender(src, models, ordertest.NewClock(p.At), func() *rand.Rand { return rand.New(rand.NewPCG(seed, seed+1)) },
		slog.New(slog.DiscardHandler))
	rec, err := r.Recommend(context.Background(), courier, open)
	require.NoError(t, err, p.ID)

	switch {
	case rec == nil && p.Expected < 0:
		return stayedQuiet
	case rec == nil:
		return saidNothing
	case p.Expected < 0:
		return spokeAnyway
	case rec.OrderID == open[p.Expected].ID:
		return pickedRight
	default:
		return pickedOther
	}
}

func percent(n, of int) string {
	if of == 0 {
		return "-"
	}
	return fmt.Sprintf("%d%%", (100*n+of/2)/of)
}

// TestRecommendationsAgainstExpectations reports how closely the recommender
// follows the expectations dataset. For each persona and each amount of
// history it trains expectationTrainings models, puts every probe to each, and
// counts how often the answer is what the persona would do: the errand they
// would take, or no recommendation when they would take none.
//
// It only checks that the dataset is well formed. It sets no pass mark: what
// agreement is good enough is the owner's to decide. Run it with -v to read
// the report.
func TestRecommendationsAgainstExpectations(t *testing.T) {
	d := loadExpectations(t)
	personas := expectationPersonas()

	// counts[persona][size index][outcome], and at full history, how many
	// trainings agreed on each probe.
	counts := make([][][outcomes]int, len(personas))
	type probeScore struct {
		probeList
		agreed int
	}
	var perProbe [][]probeScore

	for pi, p := range personas {
		lists := probeLists(t, d, p.Name)
		require.Len(t, lists, probesPerPerson, "%s: probes", p.Name)
		for _, l := range lists {
			require.Len(t, l.Errands, shownPerSession, "probe %s", l.ID)
		}
		counts[pi] = make([][outcomes]int, len(expectationSizes))
		scores := make([]probeScore, len(lists))
		for i, l := range lists {
			scores[i].probeList = l
		}
		for si, size := range expectationSizes {
			src := sourceFor(d, p.Name, size)
			if size == expectationSizes[len(expectationSizes)-1] {
				require.Len(t, src.examples, historySessions*shownPerSession, "%s: history rows", p.Name)
			}
			for seed := uint64(1); seed <= expectationTrainings; seed++ {
				// One store per training: the model trained for the first
				// probe answers the rest, as a courier's saved model would.
				models := &fakeModels{saved: map[uuid.UUID]Saved{}}
				for i, l := range lists {
					o := ask(t, src, models, seed, l)
					counts[pi][si][o]++
					if si == len(expectationSizes)-1 && o.agrees() {
						scores[i].agreed++
					}
				}
			}
		}
		perProbe = append(perProbe, scores)
	}

	var out strings.Builder
	w := tabwriter.NewWriter(&out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(&out, "\nAgreement with what each made-up courier would do, over %d trainings.\n", expectationTrainings)
	fmt.Fprintf(&out, "Each probe lists %d errands, so picking at random is right 1 time in %d.\n\n", shownPerSession, shownPerSession)

	fmt.Fprint(w, "persona")
	for _, size := range expectationSizes {
		fmt.Fprintf(w, "\t%d shown", size)
	}
	fmt.Fprintln(w, "\thabit")
	total := make([][2]int, len(expectationSizes))
	for pi, p := range personas {
		fmt.Fprint(w, p.Name)
		for si := range expectationSizes {
			c := counts[pi][si]
			agree, all := c[pickedRight]+c[stayedQuiet], 0
			for _, n := range c {
				all += n
			}
			total[si][0] += agree
			total[si][1] += all
			fmt.Fprintf(w, "\t%s", percent(agree, all))
		}
		fmt.Fprintf(w, "\t%s\n", p.Habit)
	}
	fmt.Fprint(w, "all personas")
	for si := range expectationSizes {
		fmt.Fprintf(w, "\t%s", percent(total[si][0], total[si][1]))
	}
	fmt.Fprintln(w, "\t")
	require.NoError(t, w.Flush())

	full := len(expectationSizes) - 1
	fmt.Fprintf(&out, "\nWith all %d errands of history, what the recommender did:\n\n", expectationSizes[full])
	w = tabwriter.NewWriter(&out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "persona\tright errand\tother errand\tno suggestion\t|\tkept quiet\tsuggested anyway")
	for pi, p := range personas {
		c := counts[pi][full]
		picks := c[pickedRight] + c[pickedOther] + c[saidNothing]
		quiet := c[stayedQuiet] + c[spokeAnyway]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t|\t%s\t%s\n", p.Name,
			percent(c[pickedRight], picks), percent(c[pickedOther], picks), percent(c[saidNothing], picks),
			percent(c[stayedQuiet], quiet), percent(c[spokeAnyway], quiet))
	}
	require.NoError(t, w.Flush())
	fmt.Fprintln(&out, "\nThe first three columns are probes where the courier would take one errand;")
	fmt.Fprintln(&out, "the last two are probes where they would take none (\"-\" when a persona has no such probe).")

	fmt.Fprintf(&out, "\nProbes that fewer than half the trainings got right, with all %d errands of history:\n\n", expectationSizes[full])
	w = tabwriter.NewWriter(&out, 0, 0, 2, ' ', 0)
	missed := 0
	for _, scores := range perProbe {
		for _, s := range scores {
			if 2*s.agreed >= expectationTrainings {
				continue
			}
			missed++
			want := "no recommendation"
			if s.Expected >= 0 {
				e := s.Errands[s.Expected]
				want = fmt.Sprintf("%s -> %s, %d credits", e.Supplier.Name, e.Drop.Name, e.Reward)
			}
			fmt.Fprintf(w, "%s\t%s\t%d of %d\twanted: %s\n", s.ID, s.At.In(sgt).Format("Mon 15:04"), s.agreed, expectationTrainings, want)
		}
	}
	require.NoError(t, w.Flush())
	if missed == 0 {
		fmt.Fprintln(&out, "none")
	}
	t.Log(out.String())
}
