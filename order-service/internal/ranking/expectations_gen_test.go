// AI Assistance Disclosure:
// Tool: Claude Code (model: Fable 5), date: 2026-11-10
// Scope: Generates the expectations dataset in testdata/expectations: made-up
//   couriers who each follow one plain rule, the errands they were shown and
//   took, and probe lists naming the errand such a courier would take. The
//   rules were written by the tool from the owner's instruction to use
//   "typical common sensical expectations of what a courier would prefer".
// Author review: PENDING — <reviewer to complete>

package ranking

import (
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
	"foc/order-service/internal/shuttle"
)

// updateExpectations rewrites the dataset's CSV files from this generator:
//
//	go test ./internal/ranking -run TestExpectationFilesMatchTheirGenerator -update-expectations
var updateExpectations = flag.Bool("update-expectations", false, "rewrite testdata/expectations from the generator")

const expectationsDir = "testdata/expectations"

// Sizes of the dataset. Each persona's history is historySessions lists of
// shownPerSession errands, spread over historyWeeks weeks; the probes fall in
// the week after.
const (
	historyWeeks    = 8
	historySessions = 24
	shownPerSession = 4
	probesPerPerson = 12
)

// sgt is Singapore's clock; the dataset's times are written in it.
var sgt = time.FixedZone("SGT", 8*60*60)

// firstMonday starts the history: Monday 31 August 2026. The probe week starts
// historyWeeks later, on Monday 26 October 2026.
var firstMonday = time.Date(2026, 8, 31, 0, 0, 0, 0, sgt)

// clockAt is hour:minute on the given day of the given week, day 0 a Monday.
func clockAt(week, day, hour, minute int) time.Time {
	return firstMonday.AddDate(0, 0, 7*week+day).Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
}

// benchSupplier is a pickup place: its name, type and position.
type benchSupplier struct {
	Name  string
	Type  string
	Point shuttle.Point
}

// benchSuppliers are the suppliers supplier-service seeds itself with
// (data/csv/supplier-seed-data.csv), so the dataset's pickups are the app's.
func benchSuppliers() []benchSupplier {
	s := func(name, kind string, lat, lon float64) benchSupplier {
		return benchSupplier{name, kind, shuttle.Point{Lat: lat, Lon: lon}}
	}
	return []benchSupplier{
		s("Anna's x Soup Union", "Food", 1.296444, 103.773032),
		s("NUS Co-op", "Shopping", 1.2967866, 103.7732677),
		s("Printer @ Com 2", "Printing", 1.2938347, 103.7744572),
		s("Cool Spot", "Food", 1.2940156, 103.7738478),
		s("InstaChef", "Food", 1.2938898, 103.7736305),
		s("Cafe+ Robot Cafe", "Food/Coffee", 1.296444, 103.773032),
		s("A Hot Hideout", "Food", 1.2908445, 103.7770891),
		s("Arise and Shine", "Food", 1.2991517, 103.769064),
		s("Bakehaus / Aurea", "Food", 1.2946778, 103.7707872),
		s("Central Square @ YIH", "Food", 1.2984401, 103.7726256),
		s("Pasta Express", "Food", 1.2947819, 103.7704435),
		s("TOMORO COFFEE", "Food/Coffee", 1.2931259, 103.7719943),
		s("Octobox", "Shopping", 1.2904347, 103.7787588),
		s("Smooy", "Food", 1.2948308, 103.7716305),
		s("Goh Bros E-Print Pte Ltd", "Printing", 1.2984905, 103.7720544),
		s("Cheers Unmanned Convenience Store", "Shopping", 1.2994341, 103.7526298),
		s("Nami", "Food", 1.2942982, 103.7708813),
		s("Supersnacks", "Food", 1.2913847, 103.7776367),
		s("Good Day Cafe", "Food/Coffee", 1.2967989, 103.7794336),
		s("The Coffee Roaster", "Food/Coffee", 1.296252229, 103.7720926),
		s("he by He Brews", "Food/Coffee", 1.300566804, 103.7707577),
		s("The Deck", "Food", 1.2944284, 103.7725623),
		s("Frontier Canteen", "Food", 1.296425, 103.7803638),
		s("Techno Edge", "Food", 1.2978525, 103.7716532),
		s("The Terrace", "Food", 1.294386, 103.7743402),
		s("Flavours @ UTown", "Food", 1.3044034, 103.7729838),
		s("Fine Food", "Food", 1.3040351, 103.7735416),
		s("PGP Food Court", "Food", 1.2907939, 103.7822219),
	}
}

// benchRequesters are the people posting errands, each with the mean stars
// they have received; 0 is someone nobody has rated yet.
func benchRequesters() map[string]int {
	return map[string]int{
		"req-01": 5, "req-02": 5, "req-03": 4, "req-04": 4, "req-05": 5, "req-06": 4,
		"req-07": 3, "req-08": 2, "req-09": 1, "req-10": 3, "req-11": 2, "req-12": 2,
		"req-13": 0, "req-14": 0, "req-15": 0, "req-16": 0,
	}
}

// errand is one open errand as the dataset describes it.
type errand struct {
	Supplier  benchSupplier
	Drop      order.Location
	Reward    int
	PostedAgo int // minutes before it is shown
	Requester string
	Stars     int // the requester's mean stars, 0 when unrated
}

func (e errand) tripMetres() float64 { return shuttle.Distance(e.Supplier.Point, e.Drop.Point) }

func (e errand) isFood() bool { return e.Supplier.Type == "Food" || e.Supplier.Type == "Food/Coffee" }

// detourFrom is how far e's pickup is from the nearer end of carried.
func (e errand) detourFrom(carried errand) float64 {
	return min(shuttle.Distance(carried.Supplier.Point, e.Supplier.Point), shuttle.Distance(carried.Drop.Point, e.Supplier.Point))
}

// historyRow is one errand a persona was shown, and whether they took it.
type historyRow struct {
	Persona string
	ShownAt time.Time
	errand
	Accepted bool
	// DeliveredAfter is minutes from accepting to handing over; 0 when passed.
	DeliveredAfter int
	// CourierStars is what the requester then rated the courier; 0 when none.
	CourierStars int
}

// searchRow is one time a persona set Find errands' deliver-to filter.
type searchRow struct {
	Persona string
	At      time.Time
	To      string
}

// probeRow is one errand on a probe list. The rows of a probe share Persona,
// Probe, At and Carrying; at most one is Expected.
type probeRow struct {
	Persona string
	Probe   string
	At      time.Time
	// Carrying is the errand the persona accepted a few minutes before the
	// probe and still holds; nil when their hands are free.
	Carrying *errand
	errand
	Expected bool
}

// dataset is everything in testdata/expectations.
type dataset struct {
	History  []historyRow
	Searches []searchRow
	Probes   []probeRow
}

// gen builds one persona's rows.
type gen struct {
	persona    string
	rng        *rand.Rand
	suppliers  []benchSupplier
	drops      []order.Location
	requesters []string
	stars      map[string]int
	probeCount int
	// weekdays holds, for each history week, the order its sessions take the
	// weekdays in.
	weekdays [historyWeeks][]int
	out      *dataset
}

func newGen(persona string, out *dataset) *gen {
	h := fnv.New64a()
	_, _ = h.Write([]byte(persona))
	stars := benchRequesters()
	names := make([]string, 0, len(stars))
	for name := range stars {
		names = append(names, name)
	}
	slices.Sort(names)
	return &gen{
		persona: persona, rng: rand.New(rand.NewPCG(h.Sum64(), 2026)),
		suppliers: benchSuppliers(), drops: order.DeliveryLocations(),
		requesters: names, stars: stars, out: out,
	}
}

func (g *gen) between(lo, hi int) int { return lo + g.rng.IntN(hi-lo+1) }

// anyErrand draws an errand with nothing special about it.
func (g *gen) anyErrand() errand {
	req := g.requesters[g.rng.IntN(len(g.requesters))]
	return errand{
		Supplier: g.suppliers[g.rng.IntN(len(g.suppliers))], Drop: g.drops[g.rng.IntN(len(g.drops))],
		Reward: g.between(2, 12), PostedAgo: g.between(2, 40), Requester: req, Stars: g.stars[req],
	}
}

// errandWhere draws errands until one satisfies ok.
func (g *gen) errandWhere(ok func(errand) bool) errand {
	for range 100000 {
		if e := g.anyErrand(); ok(e) {
			return e
		}
	}
	panic("expectations generator: no errand satisfies the rule for " + g.persona)
}

func (g *gen) errandsWhere(n int, ok func(errand) bool) []errand {
	out := make([]errand, n)
	for i := range out {
		out[i] = g.errandWhere(ok)
	}
	return out
}

// show records one list the persona was shown at at: they took taken and
// passed over passed. deliveredAfter is how long each taken errand stays in
// their hands, in minutes.
func (g *gen) show(at time.Time, taken, passed []errand, deliveredAfter int) {
	rows := make([]historyRow, 0, len(taken)+len(passed))
	for _, e := range taken {
		stars := 0
		if g.rng.IntN(5) > 0 {
			stars = g.between(4, 5)
		}
		rows = append(rows, historyRow{Persona: g.persona, ShownAt: at, errand: e, Accepted: true, DeliveredAfter: deliveredAfter, CourierStars: stars})
	}
	for _, e := range passed {
		rows = append(rows, historyRow{Persona: g.persona, ShownAt: at, errand: e})
	}
	g.rng.Shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
	g.out.History = append(g.out.History, rows...)
}

// probe records one probe list. want is the errand the persona would take, or
// nil when they would take none of others.
func (g *gen) probe(at time.Time, carrying *errand, want *errand, others []errand) {
	g.probeCount++
	id := fmt.Sprintf("%s-%02d", g.persona, g.probeCount)
	rows := make([]probeRow, 0, len(others)+1)
	if want != nil {
		rows = append(rows, probeRow{Persona: g.persona, Probe: id, At: at, Carrying: carrying, errand: *want, Expected: true})
	}
	for _, e := range others {
		rows = append(rows, probeRow{Persona: g.persona, Probe: id, At: at, Carrying: carrying, errand: e})
	}
	g.rng.Shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
	g.out.Probes = append(g.out.Probes, rows...)
}

// sessionTime is session s of historySessions: three a week, each on its own
// weekday so that one day's errand is handed over before the next list, at an
// hour from hours.
func (g *gen) sessionTime(s int, hours []int) time.Time {
	perWeek := historySessions / historyWeeks
	week := s / perWeek
	if g.weekdays[week] == nil {
		g.weekdays[week] = g.rng.Perm(5)
	}
	return clockAt(week, g.weekdays[week][s%perWeek], hours[g.rng.IntN(len(hours))], g.rng.IntN(60))
}

// probeTime is a weekday moment in the probe week, at an hour from hours.
func (g *gen) probeTime(hours []int) time.Time {
	return clockAt(historyWeeks, g.rng.IntN(5), hours[g.rng.IntN(len(hours))], g.rng.IntN(60))
}

var workingHours = []int{9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}

// always builds a persona whose rule does not depend on the time: in every
// list they take the one or two errands that satisfy takes and pass over
// the rest, which all satisfy passes.
func (g *gen) always(takes, passes func(errand) bool) {
	for s := range historySessions {
		n := g.between(1, 2)
		g.show(g.sessionTime(s, workingHours), g.errandsWhere(n, takes), g.errandsWhere(shownPerSession-n, passes), g.between(15, 30))
	}
	for range probesPerPerson {
		want := g.errandWhere(takes)
		g.probe(g.probeTime(workingHours), nil, &want, g.errandsWhere(shownPerSession-1, passes))
	}
}

// flipLabels turns share of the persona's history labels over, the way a real
// courier does not always follow their own habit.
func (g *gen) flipLabels(share float64) {
	for i := range g.out.History {
		r := &g.out.History[i]
		if r.Persona != g.persona || g.rng.Float64() >= share {
			continue
		}
		r.Accepted = !r.Accepted
		r.DeliveredAfter, r.CourierStars = 0, 0
		if r.Accepted {
			r.DeliveredAfter = g.between(15, 30)
		}
	}
}

func not(f func(errand) bool) func(errand) bool { return func(e errand) bool { return !f(e) } }

// The rules. Each leaves a gap between what is taken and what is passed over,
// so no errand in the dataset is a borderline case.
var (
	isFood      = func(e errand) bool { return e.isFood() }
	paysWell    = func(e errand) bool { return e.Reward >= 8 }
	paysLittle  = func(e errand) bool { return e.Reward <= 6 }
	shortTrip   = func(e errand) bool { return e.tripMetres() <= 350 }
	longTrip    = func(e errand) bool { return e.tripMetres() >= 700 }
	wellRated   = func(e errand) bool { return e.Stars >= 4 }
	poorlyRated = func(e errand) bool { return e.Stars >= 1 && e.Stars <= 3 }
	toUTownHome = func(e errand) bool { return e.Drop.Area == "UTown residences" }
	awayFromUT  = func(e errand) bool { return e.Drop.Area != "UTown residences" && e.Drop.Area != "UTown" }
	// classRoute is the walk from the Central Library and Arts side to a
	// class in Business & Computing.
	classRoute = func(e errand) bool {
		from := []string{"Anna's x Soup Union", "Cafe+ Robot Cafe", "NUS Co-op", "The Coffee Roaster", "The Deck"}
		return slices.Contains(from, e.Supplier.Name) && e.Drop.Area == "Business & Computing"
	}
)

// mondayClass builds the persona with a Monday morning class: on Mondays from
// 9 to 12 they take errands along classRoute; at any other time, and for any
// other route, they take nothing.
func (g *gen) mondayClass() {
	for week := range historyWeeks {
		for _, hour := range []int{9, 11} {
			n := g.between(1, 2)
			g.show(clockAt(week, 0, hour, g.rng.IntN(60)), g.errandsWhere(n, classRoute), g.errandsWhere(shownPerSession-n, not(classRoute)), g.between(15, 30))
		}
		// A third look each week, outside the class: Monday afternoon on even
		// weeks, another morning on odd ones. The route is on the list; they
		// pass it over.
		at := clockAt(week, 0, g.between(14, 18), g.rng.IntN(60))
		if week%2 == 1 {
			at = clockAt(week, g.between(1, 4), g.between(9, 11), g.rng.IntN(60))
		}
		n := g.between(1, 2)
		g.show(at, nil, append(g.errandsWhere(n, classRoute), g.errandsWhere(shownPerSession-n, not(classRoute))...), 0)
	}
	for _, minute := range []int{10, 40, 65, 90, 110, 135, 155, 175} {
		want := g.errandWhere(classRoute)
		g.probe(clockAt(historyWeeks, 0, 9, minute), nil, &want, g.errandsWhere(shownPerSession-1, not(classRoute)))
	}
	for _, at := range []time.Time{
		clockAt(historyWeeks, 0, 15, 30), clockAt(historyWeeks, 2, 10, 0), clockAt(historyWeeks, 3, 10, 30), clockAt(historyWeeks, 4, 16, 0),
	} {
		g.probe(at, nil, nil, append(g.errandsWhere(1, classRoute), g.errandsWhere(shownPerSession-1, not(classRoute))...))
	}
}

// lunchFood builds the persona who does food runs over lunch, from 12 to 2,
// and nothing at other hours.
func (g *gen) lunchFood() {
	lunch, other := []int{12, 13}, []int{9, 10, 16, 17, 18, 19}
	for s := range historySessions {
		n := g.between(1, 2)
		food, rest := g.errandsWhere(n, isFood), g.errandsWhere(shownPerSession-n, not(isFood))
		if s%2 == 0 {
			g.show(g.sessionTime(s, lunch), food, rest, g.between(15, 30))
		} else {
			g.show(g.sessionTime(s, other), nil, append(food, rest...), 0)
		}
	}
	for i := range probesPerPerson {
		food, rest := g.errandWhere(isFood), g.errandsWhere(shownPerSession-1, not(isFood))
		if i < 8 {
			g.probe(g.probeTime(lunch), nil, &food, rest)
		} else {
			g.probe(g.probeTime(other), nil, nil, append(rest, food))
		}
	}
}

// onTheWay builds the persona who adds a second errand only when it is on
// their way. With free hands they take a food run; a few minutes later, still
// carrying it, they take one whose pickup is within 250 m of where the first
// takes them, and pass over any 600 m or more away.
func (g *gen) onTheWay() {
	near := func(carried errand) func(errand) bool {
		return func(e errand) bool { return e.detourFrom(carried) <= 250 }
	}
	far := func(carried errand) func(errand) bool {
		return func(e errand) bool { return e.detourFrom(carried) >= 600 }
	}
	for round := range historySessions / 2 {
		at := g.sessionTime(round*2, workingHours)
		first := g.errandWhere(isFood)
		g.show(at, []errand{first}, g.errandsWhere(shownPerSession-1, not(isFood)), 40)
		g.show(at.Add(8*time.Minute), g.errandsWhere(1, near(first)), g.errandsWhere(shownPerSession-1, far(first)), g.between(15, 30))
	}
	for i := range probesPerPerson {
		if i < 9 {
			carried := g.errandWhere(isFood)
			want := g.errandWhere(near(carried))
			g.probe(g.probeTime(workingHours), &carried, &want, g.errandsWhere(shownPerSession-1, far(carried)))
			continue
		}
		want := g.errandWhere(isFood)
		g.probe(g.probeTime(workingHours), nil, &want, g.errandsWhere(shownPerSession-1, not(isFood)))
	}
}

// homeBound builds the persona who lives in a UTown residence: they keep
// filtering Find errands to "UTown residences" and take what is delivered
// there.
func (g *gen) homeBound() {
	g.always(toUTownHome, awayFromUT)
	seen := map[time.Time]bool{}
	for _, r := range g.out.History {
		if r.Persona == g.persona && !seen[r.ShownAt] && len(seen)%2 == 0 {
			g.out.Searches = append(g.out.Searches, searchRow{Persona: g.persona, At: r.ShownAt.Add(-time.Minute), To: "area:UTown residences"})
		}
		if r.Persona == g.persona {
			seen[r.ShownAt] = true
		}
	}
}

// expectationPersonas are the dataset's couriers, in file order, each with the
// habit it stands for.
func expectationPersonas() []struct {
	Name  string
	Habit string
	build func(*gen)
} {
	return []struct {
		Name  string
		Habit string
		build func(*gen)
	}{
		{"type-food", "takes food and coffee runs, passes over printing and shopping", func(g *gen) { g.always(isFood, not(isFood)) }},
		{"high-reward", "takes errands paying 8 credits or more, passes over 6 or fewer", func(g *gen) { g.always(paysWell, paysLittle) }},
		{"short-trips", "takes errands of 350 m or less from pickup to drop-off, passes over 700 m or more", func(g *gen) { g.always(shortTrip, longTrip) }},
		{"rated-requesters", "takes errands from requesters rated 4 stars or more, passes over 3 or fewer", func(g *gen) { g.always(wellRated, poorlyRated) }},
		{"home-bound", "lives in a UTown residence: filters to it and takes what is delivered there", (*gen).homeBound},
		{"monday-class", "Monday 9 to 12 only: takes errands from the Central Library and Arts side to Business & Computing", (*gen).mondayClass},
		{"lunch-food", "12 to 2 only: takes food and coffee runs", (*gen).lunchFood},
		{"on-the-way", "takes a food run, then a second errand only within 250 m of where the first takes them", (*gen).onTheWay},
		{"type-food-noisy", "type-food, breaking the habit in 15% of the errands shown", func(g *gen) { g.always(isFood, not(isFood)); g.flipLabels(0.15) }},
		{"monday-class-noisy", "monday-class, breaking the habit in 15% of the errands shown", func(g *gen) { g.mondayClass(); g.flipLabels(0.15) }},
	}
}

// generateExpectations builds the whole dataset. It is deterministic: each
// persona's random source is seeded from its name.
func generateExpectations() dataset {
	var out dataset
	for _, p := range expectationPersonas() {
		p.build(newGen(p.Name, &out))
	}
	return out
}

func stamp(t time.Time) string { return t.In(sgt).Format(time.RFC3339) }

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// blankZero writes 0 as an empty cell.
func blankZero(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func encodeCSV(header []string, rows [][]string) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(header)
	_ = w.WriteAll(rows)
	w.Flush()
	return buf.Bytes()
}

var (
	suppliersHeader = []string{"supplier", "type", "latitude", "longitude"}
	historyHeader   = []string{"persona", "shown_at", "weekday", "supplier", "drop_off", "reward", "posted_min_before", "requester", "requester_stars", "accepted", "delivered_after_min", "courier_stars"}
	searchesHeader  = []string{"persona", "at", "deliver_to_filter"}
	probesHeader    = []string{"persona", "probe", "at", "weekday", "carrying_supplier", "carrying_drop_off", "supplier", "drop_off", "reward", "posted_min_before", "requester", "requester_stars", "expected"}
)

// expectationFiles renders the dataset as the CSV files kept in
// testdata/expectations, by file name.
func expectationFiles(d dataset) map[string][]byte {
	var suppliers, history, searches, probes [][]string
	for _, s := range benchSuppliers() {
		suppliers = append(suppliers, []string{s.Name, s.Type, strconv.FormatFloat(s.Point.Lat, 'f', -1, 64), strconv.FormatFloat(s.Point.Lon, 'f', -1, 64)})
	}
	for _, r := range d.History {
		history = append(history, []string{
			r.Persona, stamp(r.ShownAt), r.ShownAt.In(sgt).Weekday().String(), r.Supplier.Name, r.Drop.Name,
			strconv.Itoa(r.Reward), strconv.Itoa(r.PostedAgo), r.Requester, blankZero(r.Stars),
			yesNo(r.Accepted), blankZero(r.DeliveredAfter), blankZero(r.CourierStars),
		})
	}
	for _, s := range d.Searches {
		searches = append(searches, []string{s.Persona, stamp(s.At), s.To})
	}
	for _, p := range d.Probes {
		var carryingSupplier, carryingDrop string
		if p.Carrying != nil {
			carryingSupplier, carryingDrop = p.Carrying.Supplier.Name, p.Carrying.Drop.Name
		}
		probes = append(probes, []string{
			p.Persona, p.Probe, stamp(p.At), p.At.In(sgt).Weekday().String(), carryingSupplier, carryingDrop,
			p.Supplier.Name, p.Drop.Name, strconv.Itoa(p.Reward), strconv.Itoa(p.PostedAgo), p.Requester, blankZero(p.Stars), yesNo(p.Expected),
		})
	}
	return map[string][]byte{
		"suppliers.csv": encodeCSV(suppliersHeader, suppliers),
		"history.csv":   encodeCSV(historyHeader, history),
		"searches.csv":  encodeCSV(searchesHeader, searches),
		"probes.csv":    encodeCSV(probesHeader, probes),
	}
}

// TestExpectationFilesMatchTheirGenerator keeps the checked-in CSV files and
// this generator saying the same thing. Run it with -update-expectations to
// rewrite the files after changing a rule.
func TestExpectationFilesMatchTheirGenerator(t *testing.T) {
	files := expectationFiles(generateExpectations())
	if *updateExpectations {
		require.NoError(t, os.MkdirAll(expectationsDir, 0o755))
		for name, data := range files {
			require.NoError(t, os.WriteFile(filepath.Join(expectationsDir, name), data, 0o644))
		}
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(expectationsDir, name))
		require.NoError(t, err, "run this test with -update-expectations to write the dataset")
		// A checkout may have turned line endings into CRLF.
		got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
		require.True(t, bytes.Equal(want, got), "%s differs from what the generator writes; rerun with -update-expectations", name)
	}
}
