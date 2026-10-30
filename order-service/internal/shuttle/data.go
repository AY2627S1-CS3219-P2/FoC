// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The NUS shuttle stops and routes, looked up from the public sources
//   named below on 2026-10-29 (D-090, D-097).
// Author review: PENDING — <reviewer to complete>

package shuttle

// AsOf is the date this data was taken from its sources. The app shows it with
// the route data (D-090).
const AsOf = "2026-10-29"

// Sources are where the data came from (D-097).
//
// Stop order, operating hours and each route's Updated date are from the
// route pages on Land Transport Guru, one per service, e.g.
// https://landtransportguru.net/nus-isb-a1/ (and nus-isb-a2, -d1, -d2, -k,
// -p, -r1, -r2). Its overview page lists A1, A2, D1, D2, K, P, R1 and R2 as
// the services running, with E and BTC withdrawn.
//
// Stop positions are OpenStreetMap's bus-stop nodes, read through the
// Overpass API (© OpenStreetMap contributors, ODbL 1.0); a stop's Code is the
// LTA code OSM carries as `ref`.
const Sources = "Routes and hours: landtransportguru.net/nus-isb-<service>/. " +
	"Stop positions: © OpenStreetMap contributors (ODbL), via the Overpass API."

// Stops returns every stop any route serves. A function, not a package-level
// slice, so callers cannot change the data.
func Stops() []Stop {
	return []Stop{
		{Key: "kr-ter", Name: "Kent Ridge Ter", Code: "16009", Point: Point{1.294245, 103.769680}},
		{Key: "lt13", Name: "LT 13", Point: Point{1.294745, 103.770597}},
		{Key: "as5", Name: "AS 5", Point: Point{1.293493, 103.771818}},
		{Key: "biz2", Name: "BIZ 2", Point: Point{1.293242, 103.775068}},
		{Key: "opp-tcoms", Name: "Opp TCOMS", Point: Point{1.293720, 103.777015}},
		{Key: "tcoms", Name: "TCOMS", Point: Point{1.293572, 103.777046}},
		// OSM maps the terminal as a bus station, not a bus stop.
		{Key: "pgp-ter", Name: "Prince George's Park Ter", Point: Point{1.291825, 103.780421}},
		{Key: "pgp-foyer", Name: "Prince George's Park Foyer", Point: Point{1.290956, 103.781066}},
		{Key: "kr-stn-a", Name: "Kent Ridge Stn Exit A/NUH", Code: "18331", Point: Point{1.294818, 103.784376}},
		{Key: "opp-kr-stn-a", Name: "Opp Kent Ridge Stn Exit A", Code: "18339", Point: Point{1.294988, 103.784599}},
		{Key: "lt27", Name: "Lim Seng Tjoe Bldg (LT 27)", Code: "18301", Point: Point{1.297406, 103.780990}},
		{Key: "s12", Name: "Blk S12", Code: "18311", Point: Point{1.297111, 103.778749}},
		{Key: "s17", Name: "Blk S17", Code: "18309", Point: Point{1.297540, 103.780691}},
		{Key: "opp-uhc", Name: "Opp University Health Ctr", Code: "18321", Point: Point{1.298742, 103.775659}},
		{Key: "uhc", Name: "University Health Ctr", Code: "18329", Point: Point{1.298959, 103.776059}},
		{Key: "opp-uhall", Name: "Opp University Hall", Code: "18319", Point: Point{1.297574, 103.778088}},
		{Key: "yih", Name: "Yusof Ishak Hse", Code: "16171", Point: Point{1.298863, 103.774321}},
		{Key: "opp-yih", Name: "Opp Yusof Ishak Hse", Code: "16179", Point: Point{1.298924, 103.774048}},
		{Key: "clb", Name: "Ctrl Lib", Code: "16181", Point: Point{1.296275, 103.772205}},
		{Key: "it", Name: "Information Technology", Code: "16189", Point: Point{1.297221, 103.772714}},
		{Key: "museum", Name: "NUS Museums", Code: "16161", Point: Point{1.301168, 103.773471}},
		{Key: "raffles", Name: "NUS Raffles Hall", Code: "16169", Point: Point{1.300969, 103.772746}},
		{Key: "opp-hssml", Name: "Opp Hon Sui Sen Memorial Library", Point: Point{1.292953, 103.775118}},
		{Key: "opp-nuss", Name: "Opp NUSS", Point: Point{1.293243, 103.772438}},
		{Key: "ventus", Name: "Ventus", Point: Point{1.295339, 103.770561}},
		{Key: "com3", Name: "COM 3", Point: Point{1.294920, 103.775011}},
		{Key: "utown", Name: "University Town", Point: Point{1.303535, 103.774431}},
		{Key: "mosque", Name: "Tentera Diraja Mque", Code: "16141", Point: Point{1.297889, 103.769638}},
		{Key: "jps", Name: "The Japanese Pr Sch", Code: "16151", Point: Point{1.300770, 103.769904}},
		{Key: "kent-vale", Name: "Kent Vale", Point: Point{1.301833, 103.769613}},
		{Key: "opp-bg-stn", Name: "Opp Botanic Gdns Stn", Code: "41029", Point: Point{1.323345, 103.816273}},
		{Key: "othb", Name: "Oei Tiong Ham Bldg", Point: Point{1.319858, 103.817835}},
		{Key: "bg-pudo", Name: "Botanic Gardens Stn (PUDO Point)", Point: Point{1.322619, 103.815902},
			Approximate: "OSM has no node for the pick-up/drop-off point; this is public bus stop 41021 (Botanic Gardens Station) on the same road"},
	}
}

// Routes returns every service running on the AsOf date, each with its stops
// in the order the source lists them.
func Routes() []Route {
	return []Route{
		{Code: "A1", Loop: true, Updated: "2026-03-02",
			Stops: []string{"kr-ter", "lt13", "as5", "biz2", "opp-tcoms", "pgp-ter", "kr-stn-a", "lt27", "s12", "opp-uhc", "yih", "clb"},
			Hours: "Weekdays and Saturdays 07:15–23:00; Sundays and public holidays 09:07–23:00 (departures from Kent Ridge Ter)"},
		{Code: "A2", Loop: true, Updated: "2025-12-31",
			Stops: []string{"kr-ter", "it", "opp-yih", "museum", "uhc", "opp-uhall", "s17", "opp-kr-stn-a", "pgp-foyer", "tcoms", "opp-hssml", "opp-nuss", "ventus"},
			Hours: "Weekdays and Saturdays 07:15–23:00; Sundays and public holidays 09:00–23:00 (departures from Kent Ridge Ter)"},
		{Code: "D1", Loop: true, Updated: "2026-02-25",
			Stops: []string{"com3", "opp-hssml", "opp-nuss", "ventus", "it", "opp-yih", "museum", "utown", "yih", "clb", "lt13", "as5", "biz2"},
			Hours: "Weekdays in term 07:15–23:00; weekdays in vacation and Saturdays 07:20–23:00; Sundays and public holidays 09:10–23:00 (departures from COM 3)"},
		{Code: "D2", Loop: true, Updated: "2025-12-31",
			Stops: []string{"com3", "opp-tcoms", "pgp-ter", "kr-stn-a", "lt27", "s12", "opp-uhc", "museum", "utown", "uhc", "opp-uhall", "s17", "opp-kr-stn-a", "pgp-foyer", "tcoms"},
			Hours: "Weekdays and Saturdays 07:15–23:00; Sundays and public holidays 09:00–23:00 (departures from COM 3)"},
		{Code: "K", Loop: true, Updated: "2026-01-31",
			Stops: []string{"pgp-ter", "kr-stn-a", "lt27", "s12", "opp-uhc", "yih", "clb", "mosque", "jps", "kent-vale", "museum", "uhc", "opp-uhall", "s17", "opp-kr-stn-a", "pgp-foyer"},
			Hours: "Weekdays 07:04–23:04; Saturdays 07:04–19:44; no service on Sundays and public holidays (departures from Prince George's Park)"},
		{Code: "P", Loop: true, Updated: "2026-08-10",
			Stops: []string{"kent-vale", "opp-bg-stn", "othb", "bg-pudo", "kr-stn-a", "opp-uhc", "utown"},
			Hours: "Weekdays only, not public holidays: departures from Kent Vale 08:20–17:25"},
		{Code: "R1", Loop: false, Updated: "2026-08-10",
			Stops: []string{"kent-vale", "museum", "utown", "yih", "clb", "lt13", "as5", "biz2", "pgp-ter"},
			Hours: "Weekdays only, not public holidays: departures from Kent Vale 07:40–19:30"},
		{Code: "R2", Loop: false, Updated: "2026-08-10",
			Stops: []string{"pgp-ter", "opp-hssml", "opp-nuss", "ventus", "it", "opp-yih", "utown", "raffles", "kent-vale"},
			Hours: "Weekdays only, not public holidays: departures from Prince George's Park 08:20–19:30"},
	}
}
