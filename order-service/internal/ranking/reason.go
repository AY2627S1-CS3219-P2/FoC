// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The recommendation's reason (D-087, D-094): the features that
//   pushed its score up most, said in words.
// Author review: PENDING — <reviewer to complete>

package ranking

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"foc/order-service/internal/forest"
)

// maxReasons is how many reasons a recommendation gives, keeping it within
// D-094's 1 to 3 lines.
const maxReasons = 2

// fallbackReason is said when no feature pushed the score up.
const fallbackReason = "Suggested from the errands you have taken before."

// reasonFor says why cand scored as it did: the features whose contributions
// raised its score most, each as a phrase about this errand. Two features that
// say the same thing (the hour and the hour share) give one phrase.
func reasonFor(cand Candidate, row []float64, e forest.Explanation) string {
	type pushed struct {
		feature int
		by      float64
	}
	var ups []pushed
	for f, by := range e.Contributions {
		if by > 0 {
			ups = append(ups, pushed{f, by})
		}
	}
	slices.SortFunc(ups, func(a, b pushed) int { return cmp.Or(cmp.Compare(b.by, a.by), cmp.Compare(a.feature, b.feature)) })

	var phrases []string
	for _, u := range ups {
		p := phraseFor(u.feature, cand, row)
		if p == "" || slices.Contains(phrases, p) {
			continue
		}
		phrases = append(phrases, p)
		if len(phrases) == maxReasons {
			break
		}
	}
	if len(phrases) == 0 {
		return fallbackReason
	}
	return "Suggested because " + strings.Join(phrases, " and ") + "."
}

// phraseFor says what feature f says about cand, or "" when its value gives
// nothing to say.
func phraseFor(f int, cand Candidate, row []float64) string {
	v := row[f]
	switch f {
	case fReward:
		return fmt.Sprintf("it pays %d credits", cand.Reward)
	case fAgeMinutes:
		return fmt.Sprintf("it was posted %s ago", minutesText(v))
	case fTripMetres:
		if v == missing {
			return ""
		}
		return fmt.Sprintf("it is a %s trip", metresText(v))
	case fHour, fHourShare:
		if row[fHourShare] == missing {
			return ""
		}
		return "you often take errands around this time"
	case fWeekday, fWeekdayShare:
		if row[fWeekdayShare] == missing {
			return ""
		}
		return fmt.Sprintf("you often take errands on %ss", time.Weekday(int(row[fWeekday])))
	case fCompleted:
		return "it suits the errands you have completed so far"
	case fTypeShare:
		if v == missing || cand.SupplierType == "" {
			return ""
		}
		return fmt.Sprintf("you often take %s errands", cand.SupplierType)
	case fTypeRating:
		if v == missing {
			return ""
		}
		return fmt.Sprintf("you are rated %.1f on %s errands", v, cand.SupplierType)
	case fRequesterRating:
		if v == missing {
			return ""
		}
		return fmt.Sprintf("its requester is rated %.1f", v)
	case fCarrying, fDetourMetres, fSharesRoute:
		if row[fCarrying] == 0 {
			return ""
		}
		if row[fSharesRoute] == 1 {
			return "a shuttle route links it to the errand you are carrying"
		}
		if row[fDetourMetres] != missing {
			return fmt.Sprintf("its pickup is %s from the errand you are carrying", metresText(row[fDetourMetres]))
		}
		return ""
	case fSearchedPickup:
		if v == missing || v == 0 {
			return ""
		}
		return fmt.Sprintf("you often look for errands from %s", cand.SupplierName)
	case fSearchedDestination:
		if v == missing || v == 0 {
			return ""
		}
		return fmt.Sprintf("you often look for errands to %s", cand.DeliveryLocation)
	}
	return ""
}

// metresText is "350 m" or "1.2 km".
func metresText(m float64) string {
	if m < 1000 {
		return fmt.Sprintf("%d m", max(10, int(m/10+0.5)*10))
	}
	return fmt.Sprintf("%.1f km", m/1000)
}

// minutesText is "5 min" or "2 h 10 min".
func minutesText(m float64) string {
	total := int(m + 0.5)
	if total < 60 {
		return fmt.Sprintf("%d min", total)
	}
	if total%60 == 0 {
		return fmt.Sprintf("%d h", total/60)
	}
	return fmt.Sprintf("%d h %d min", total/60, total%60)
}
