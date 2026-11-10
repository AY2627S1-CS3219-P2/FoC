<!--
AI Assistance Disclosure:
Tool: Claude Code (model: Fable 5), date: 2026-11-10
Scope: This file and the four CSV files beside it, which CSV cannot mark
  itself. The personas and their rules were written by the tool from the
  owner's instruction to use "typical common sensical expectations of what a
  courier would prefer"; the CSV files are written by
  ../../expectations_gen_test.go.
Author review: nigeltzy - checked file was written as expected, readme accurately reflected
-->

# Expectations dataset for errand ranking

Made-up couriers, each following one plain habit, to compare the recommender
against: train it on what a courier was shown and took, then ask whether it
recommends the errand that courier would take.

Nothing here is real usage data. Each habit is a common-sense guess at how a
courier might choose, written as a rule with a gap between what is taken and
what is passed over, so no errand is a borderline case. Agreement with this
dataset says the recommender picks up habits like these; it does not say how
well it predicts real couriers.

## The couriers

| Persona | Habit |
| --- | --- |
| `type-food` | Takes food and coffee runs; passes over printing and shopping |
| `high-reward` | Takes errands paying 8 credits or more; passes over 6 or fewer |
| `short-trips` | Takes errands of 350 m or less from pickup to drop-off; passes over 700 m or more |
| `rated-requesters` | Takes errands from requesters rated 4 stars or more; passes over 3 or fewer |
| `home-bound` | Lives in a UTown residence: keeps filtering Find errands to "UTown residences" and takes what is delivered there |
| `monday-class` | Has a Monday morning class: on Mondays from 9 to 12 takes errands from the Central Library and Arts side to Business & Computing; takes nothing at other times or on other routes |
| `lunch-food` | Takes food and coffee runs from 12 to 2; takes nothing at other hours |
| `on-the-way` | Takes a food run, then, while carrying it, a second errand only if its pickup is within 250 m of where the first takes them; passes over 600 m or more |
| `type-food-noisy` | `type-food`, breaking the habit in 15% of the errands shown |
| `monday-class-noisy` | `monday-class`, breaking the habit in 15% of the errands shown |

Each courier has 8 weeks of history from Monday 31 August 2026: 24 lists of 4
errands, 96 in all. Their 12 probes fall in the week of Monday 26 October 2026.
Pickups are the suppliers supplier-service seeds itself with, and drop-offs are
order-service's delivery locations.

## Files

`suppliers.csv`: the pickup places, with type and position.

`history.csv`: one row for each errand a courier was shown.

| Column | Meaning |
| --- | --- |
| `persona` | The courier |
| `shown_at`, `weekday` | When the list was shown, Singapore time. Rows with the same time were one list |
| `supplier`, `drop_off` | Pickup place (a row of `suppliers.csv`) and delivery location |
| `reward` | Credits |
| `posted_min_before` | How many minutes before the list the errand was posted |
| `requester`, `requester_stars` | Who posted it, and their mean stars; blank when nobody has rated them |
| `accepted` | `yes` when the courier took it |
| `delivered_after_min` | Minutes from accepting to handing over; blank when passed over |
| `courier_stars` | What the requester then rated the courier; blank when none |

`searches.csv`: each time a courier set Find errands' deliver-to filter.

`probes.csv`: the probe lists. Rows with the same `probe` are one list of 4
open errands at the moment `at`. `expected` is `yes` on the errand the courier
would take; a probe with no `yes` is one where they would take none, so the
right answer is no recommendation. `carrying_supplier` and `carrying_drop_off`
name an errand the courier accepted 6 minutes earlier and still holds.

## Compare the recommender against it

From `order-service/`:

```bash
go test ./internal/ranking -run TestRecommendationsAgainstExpectations -v -count=1
```

The report gives, for each courier, how often the recommender did what they
would do, with the newest 12, 24, 48 and all 96 errands of history, over 20
trainings each (a forest grows differently every time). It then breaks the
full-history result down and lists the probes most trainings got wrong.

The test sets no pass mark. What agreement is good enough is the owner's to
decide.

## Change it

The CSV files are written by `expectations_gen_test.go`. After changing a rule
there, rewrite them:

```bash
go test ./internal/ranking -run TestExpectationFilesMatchTheirGenerator -update-expectations
```

Without the flag, that test fails when the files and the generator disagree.
