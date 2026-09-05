# Campaign Events — notes

Company pack: **Go** backend, in-memory persistence, stdlib only. Service is `starter/` (`go run .`). Debugging fixes are in `debugging/`. No frontend.

## Part 1 — Read and think

### 1. Interpretation

Relay does not send mail. Providers do. This service is the webhook + the dashboard numbers:

- Providers POST batches of lifecycle events (`sent`, `delivered`, `opened`, `clicked`).
- The dashboard GETs per-campaign counts.
- The real work is **identity and idempotency**: retries must not double-count, and an `opened` that arrives before `delivered` is still an open.

### 2. Assumptions

- `event_id` identifies one provider event. Same id → same event. First write wins.
- Types are exactly `sent` | `delivered` | `opened` | `clicked` (lowercase). `OPENED` and `spam_report` are invalid.
- `timestamp` is provider time. We require RFC3339. We do not reject late events. We do not require funnel order.
- Lifetime unique counts are monotone if we never delete. If a tile goes down, that is a different query or a bug.
- Non-2xx makes providers retry the batch, so a duplicate of a *valid* event must still be 2xx.
- A few thousand events/day: in-memory + a mutex is enough. Restart loses data; that is acceptable for this take-home (SQLite would be the next step, not the first).
- No auth (brief says not to).

### 3. Ambiguities

- Same `event_id`, different `type` / contact — conflict vs last-write-wins?
- “How many opened”: unique events, unique people, or people who also have delivered?
- May opened > delivered?
- Mixed batch: 200 partial vs 400 all-or-nothing?
- Typed `[]Event` vs per-item `json.RawMessage` (the starter README flags this).
- Unknown campaign: zeros vs 404?
- Windowed stats vs lifetime?
- Retract / bounce / spam after a click?

### 4. Questions for the PM

1. Dashboard tiles: unique people or unique provider events? I return both (`events` and `unique_contacts` / `unique_opens`).
2. Is opened > delivered OK on the live view?
3. Same `event_id`, different payload: alert, or overwrite?
4. Mixed batch: ack the good ones (I did) knowing rejects will not be retried unless the provider sends a new request?
5. Do correction events exist?

### 5. Priority

1. These notes.
2. Contract: `event_id` identity, first-write-wins, no funnel, per-item validation.
3. `POST /events` + `GET .../stats` on the starter.
4. Mutex so concurrent retries cannot double-count.
5. Tests for the behaviour I would be embarrassed to break + survive `seed/events.json`.
6. Official `debugging/` four bugs.
7. Scale memo + angry marketer.
8. `GET .../events` if time.
9. Skip: UI, auth, k8s, queue.

---

## Part 4 — Scale memo (100 million events/day)

~1,160 events/s average, higher peaks.

### What breaks first

1. **Process memory and restart.** Every unique `event_id` and every campaign’s contact sets live in one Go process. 100M events/day with retention is tens of GB quickly, and a restart zeros the dashboard.
2. **A single mutex.** Ingest and stats share `Store.mu`. At peak, stats GETs queue behind batch POSTs (and vice versa).
3. **Webhook timeout.** We decode the whole body, then ingest sequentially under the lock. A 5k-event batch at peak will miss provider timeouts → more retries.
4. **One machine.** No disk, no replica.

### What I would change, in order

1. Persist an append-only event log (Postgres/SQLite first, then partitioned log). Unique index on `event_id`.
2. Maintain **O(1) counter rows** on insert-win only. Stop walking the log on every dashboard GET.
3. Ack the webhook after the idempotent insert (or after enqueue), not after extra work.
4. Split ingest and read. Queue only if p99 ingest latency still blows the provider SLA.

### API / storage

HTTP paths and the event JSON: keep. I might add `as_of` / `from`/`to` later. Storage: yes — log + small stats table. Unique-contact sets cannot stay as in-memory maps per campaign.

### Queue?

Yes, when the HTTP handler cannot persist+ack in time. New problems: at-least-once workers (increment after crash), lag, poison messages, replay. Duplicates that the mutex stopped can come back as “two consumer deliveries of one `event_id`”. Counters must increment **iff** the `event_id` insert won.

### Trustworthy counters

Insert `event_id` uniquely; `$inc` only on that win. Recompute a sample of campaigns from the log on a schedule; alert on drift. Lifetime tiles stay monotone; windows are a separate query.

### Monitor

Accept / duplicate / reject / payload-conflict rates, handler p99, queue lag, counter-vs-log drift, opened >> sent.

### Not yet

Multi-region, “exactly-once”, bot-open ML, infinite hot retention.

---

## Part 5 — The angry marketer

10:00: sent 1M / delivered 970k / opened 250k / clicked 20k  
10:30: sent 1M / delivered 975k / opened 248k / clicked 20k

### Plausible explanations (before “the code is broken”)

Opened down:

1. Tile is a **sliding window**, not lifetime. Yesterday’s opens dropped out; deliveries still arriving.
2. Deploy changed the definition (events → unique people, or “must have delivered”, or exclude Apple MPP / bots).
3. Privacy delete / suppression stripped contacts who had opened.
4. Provider correction / internal “invalid open” job.
5. Two backends or a cache during a rollout.
6. Approximate uniques (HLL) — possible small moves; 2,000 is a lot unless a sketch reset.
7. Timezone / “today” bucket.
8. Segment filter applied at 10:30 that was not there at 10:00.
9. Actual bug: decrement-on-duplicate, race, funnel un-count.

Delivered up 5k:

10. Late `delivered` events — normal tail on a finished send.
11. Retries of deliveries we previously failed to 2xx — should **not** raise unique `event_id` counts if dedup works. If it does, either new ids or broken dedup.

### Is delivered rising suspicious?

**No.** Sent is flat; 5k more deliveries in 30 minutes is ~0.5% of the send. Suspicious would be delivered > sent, or a huge jump with no provider traffic.

Opened **falling** is illegal for “lifetime unique contacts, append-only”. So the tile is not that definition, or something deleted/recomputed.

### Check first

1. What query the dashboard runs (lifetime vs window vs segment) and whether it changed.
2. Ingest log 10:00–10:30: ~5k new delivered ids? Any deletes / deploys / filter jobs?
3. Recompute from stored unique ids vs the API. Log says 250k and API says 248k → serving bug. Both say 248k → data or definition changed.

Under **this** service (no deletes, no funnel, lifetime uniques), opened dropping is a bug. Delivered rising is not.

---

## What I completed / what I intentionally skipped

Completed: Parts 1, 4, 5; Go service (`POST /events`, `GET .../stats`, `GET .../events`); tests including seed survival and concurrent retries; official debugging four bugs; `BUGS.md`, `AI_USAGE.md`, `README.md`.

Skipped: SQLite (memory is enough at stated traffic; restart loss is documented), auth, deploy, UI (brief: no extra credit). An earlier MERN pass in this repo was discarded once `candidate-only/` was available — that pack is Go.

---

## If I had another day

SQLite with a unique index on `event_id`; a reconcile command (recompute vs live counters); `(timestamp, event_id)` cursors; persist payload conflicts; load-test batch ingest against a 2s timeout; optional `from`/`to` on stats so the angry-marketer window is explicit.
