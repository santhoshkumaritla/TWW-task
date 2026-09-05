# Part 3 — bugs in `debugging/` (official program)

Fixes are minimal. The program is the one in `candidate-only/debugging/`.

## Bug 1 — Dedup is per batch, not per file

**What:** `seen` was created inside `processBatch`. Only collisions *inside the same 200-line batch* were dropped.

**Why:** The spec says each `event_id` at most once across the whole file. Retries that land in a later batch were counted again, so sent/delivered/opened/clicked were all high (e.g. cmp_A sent 2023 vs expected 1987).

**Fix:** Package-level `seenIDs`. Skip if that id was already accepted in any earlier batch.

**Verified:** After the fix, campaign totals match `expected_output.txt` (cmp_A sent=1987, etc.). Before, those totals were strictly larger.

## Bug 2 — `unique_opens` keyed by contact only

**What:** `openedBy[ev.ContactID]` is global across campaigns.

**Why:** Spec: one contact opening in two campaigns counts once *in each*. A contact who opened in cmp_A then cmp_B was ignored in B, so unique_opens was far too low (cmp_A 549 vs expected 942).

**Fix:** Key `openedBy` with `campaign_id + NUL + contact_id`.

**Verified:** unique_opens on every campaign now matches expected (942 / 833 / 710 / 514 / 380).

## Bug 3 — Daily buckets use local time, not UTC

**What:** `ev.Timestamp.Local().Format("2006-01-02")`.

**Why:** Spec: UTC date. On a machine east of UTC, late-evening UTC timestamps roll into the next local day, which invented `2026-08-08` and shifted the 01–07 counts.

**Fix:** `ev.Timestamp.UTC().Format("2006-01-02")`.

**Verified:** No 2026-08-08 line; daily delivered rows match expected exactly.

## Bug 4 — Data race on counter increments

**What:** `apply` runs on 8 workers and does `cs.Sent++` (and the other ints) with no lock. The map of campaigns is pre-created, but the *fields* are still shared.

**Why:** Concurrent unsynchronized writes to the same int are a data race. Counts can tear. `go run -race . events.jsonl` reports it. This can look “fine” on a quiet run; it is still wrong against the spec (identical output every run, defined Go).

**Fix:** `applyMu` around the body of `apply`. `track` still runs on the main goroutine before workers start.

**Verified:** `go run . events.jsonl` matches `expected_output.txt` on repeated runs. `go run -race` needs cgo; this Windows install has no C compiler, so the race detector did not execute here. The mutex is still the correct fix for the concurrent `++`.
