# Campaign Events — notes

Go backend, in-memory persistence, stdlib only. `go run .` from the repo root. Debugging fixes are in `debugging/`. No frontend.

---

## Part 1 — Read and think

### 1. Interpretation

Relay does not deliver mail. Providers do. This service is the webhook they call, and the numbers the dashboard reads:

- Providers POST batches of lifecycle events: `sent`, `delivered`, `opened`, `clicked`.
- The dashboard GETs counts for one campaign.
- The actual product problem is **identity**: retries must not inflate tiles, and an `opened` that arrives before `delivered` is still an open. Arrival order is not the story.

### 2. Assumptions

- `event_id` is the provider’s identity for one occurrence. Same id → same event. First successful write wins; we do not overwrite.
- Types are exactly `sent` | `delivered` | `opened` | `clicked` (lowercase). `OPENED` and `spam_report` are invalid, not aliases.
- `timestamp` is when it happened at the provider. Required RFC3339. We do not reject “late” events. We do not require funnel order (sent before delivered before opened).
- Lifetime unique counts are monotone if we never delete. If a dashboard tile goes down, that query is not this lifetime counter — or we have a bug.
- Providers retry on missing / slow / non-2xx. A duplicate of a valid event must still be 2xx, or they will keep sending it.
- A few thousand events/day, ~50 campaigns: one process, in-memory maps, one mutex is enough. Restart loses data. Acceptable for the take-home; SQLite would be the next step, not the first.
- No auth (brief says not to build it).

### 3. Ambiguities

- Same `event_id`, different `type` / campaign / contact — treat as conflict, last-write-wins, or 409?
- “How many opened”: unique `event_id`s, unique people, or people who also have `delivered`?
- May opened > delivered on the live view, or should the UI clamp?
- Mixed batch: 200 + partial success vs 400 all-or-nothing? Partial 200 means rejects will not be retried unless the provider sends a new request.
- Decode `[]Event` (one bad element can fail the whole slice) vs per-item `json.RawMessage`.
- Unknown `campaign_id` on GET: zeros vs 404.
- Lifetime vs windowed (“today”) stats? The brief never says.
- Do bounce / spam / retract events exist later? Seed already has `spam_report`.
- Is `event_id` unique globally, or only per provider / per campaign?
- Pagination cursor for “recent activity”: ingest order vs provider timestamp.

### 4. Questions I would ask the PM

1. Are the four tiles unique people or unique provider events? I return both (`events` and `unique_contacts` / `unique_opens`) so we can pick without a migration.
2. Is opened > delivered acceptable on the live campaign view?
3. Same `event_id`, different payload: ignore and alert, or overwrite?
4. Mixed batch: ack the good rows (I did). Confirm we are willing to drop invalid rows instead of failing the batch.
5. Time range on stats, or only lifetime while the campaign is running?
6. Do correction / suppression events exist, and should they ever decrease a tile?

### 5. Priority (time order)

1. These notes — the brief is incomplete on purpose.
2. Contract: `event_id` identity, first-write-wins, no funnel, per-item validation, 200 on mixed batches.
3. `POST /events` and `GET /campaigns/{id}/stats`.
4. One mutex so concurrent retries cannot both increment.
5. Tests for the behaviour I would be embarrassed to break, including `seed/events.json`.
6. Official `debugging/` four bugs.
7. Scale memo + angry marketer.
8. `GET .../events` if time (I had time).
9. Skip: UI, auth, k8s, queue, SQLite.

### Decisions that followed (so the code is not a surprise)

- Same event = same `event_id`. First write wins.
- Mixed batch → **200** with `accepted` / `duplicates` / `rejected`. Body that is not a JSON array → **400**.
- “How many opened” for the primary `events` object = distinct open `event_id`s. `unique_opens` = distinct contacts. Funnel is **not** applied.
- Store: in-memory event log + running counters + contact sets, guarded by one mutex. Counters increment only on insert-win.
- Unknown campaign: **200** with zeros (dashboard empty state, not a client error).
- Stats are **lifetime**. `as_of: lifetime` is in the JSON so a windowed tile is a different endpoint later.

---

## Part 4 — Scale memo (100 million events/day)

About 1,160 events/s average, with higher peaks. Today’s design is a single Go process holding every `event_id` and every campaign’s contact sets in RAM.

### What breaks first

1. **Memory and restart.** 100M unique ids/day, retained, is tens of GB quickly. A restart zeros the dashboard.
2. **The mutex.** Ingest and stats share `Store.mu`. At peak, GETs queue behind batch POSTs and the other way around.
3. **Webhook timeout.** We read the whole body, then ingest under the lock. A large batch that misses the provider’s timeout becomes more retries.
4. **One machine.** No disk, no replica, no way to ingest while the process is down.

### What I would change, in order

1. Persist an append-only event log (Postgres is fine; SQLite first if we want one box). Unique index on `event_id`.
2. Maintain **O(1) counter rows** updated only when the insert wins. Stop walking the log on every dashboard GET.
3. Ack the webhook after the idempotent write (or after a durable enqueue), not after extra work.
4. Split ingest and read. Introduce a queue only if p99 ingest latency still blows the provider SLA after (1)–(3).

### API / storage

HTTP paths and the event JSON stay. I might add `from`/`to` or `as_of` later; that is additive. Storage **does** change: log + small stats table. Unique-contact sets cannot stay as in-memory maps per campaign (HyperLogLog / bitmap / separate contact table, depending on how exact “unique people” must be).

### Queue?

Yes, when the HTTP handler cannot persist and ack in time. New problems: at-least-once workers, lag, poison messages, replay. Duplicates the in-process mutex used to stop can come back as “two consumer deliveries of one `event_id`”. Counters must increment **iff** the `event_id` insert won.

### Trustworthy counters

Insert `event_id` uniquely; increment only on that win. Periodically recompute a sample of campaigns from the log; alert on drift. Lifetime tiles stay monotone; windows are a separate query, not a decrement of the lifetime row.

### Monitor

Accept / duplicate / reject / payload-conflict rates, handler p99, queue lag, counter-vs-log drift, opened >> sent, memory, restart age of the stats row.

### Not yet

Multi-region active-active, “exactly-once” delivery, bot-open classification, infinite hot retention of the raw log.

---

## Part 5 — The angry marketer

10:00: sent 1,000,000 / delivered 970,000 / opened 250,000 / clicked 20,000  
10:30: sent 1,000,000 / delivered 975,000 / opened 248,000 / clicked 20,000

### Plausible explanations (before “the code is broken”)

Opened went down:

1. The tile is a **sliding window**, not lifetime. Yesterday’s opens aged out; deliveries are still arriving.
2. Deploy changed the definition (events → unique people, or “must have delivered”, or exclude Apple Mail Privacy / bots).
3. Privacy delete or suppression removed contacts who had opened.
4. Provider correction, or an internal “invalid open” job.
5. Two backends or a warm cache during a rollout; 10:00 and 10:30 hit different truths.
6. Approximate uniques (HLL). Small jitter is possible; 2,000 is large unless a sketch reset.
7. Timezone / “today in marketing-local” bucket rolled.
8. A segment or campaign-version filter applied at 10:30 that was not there at 10:00.
9. Actual bug: decrement-on-duplicate, race, funnel un-count, or stats rebuilt from a partial log.

Delivered went up 5,000:

10. Late `delivered` events — normal tail even after send volume is flat.
11. Retries of deliveries we previously failed to 2xx. That should **not** raise unique `event_id` counts if dedup works. If it does, either new ids or broken dedup.

### Is delivered rising suspicious?

**No.** Sent is unchanged; +5k deliveries in 30 minutes is about 0.5% of the send. Suspicious would be delivered > sent, or a jump with no provider traffic.

Opened **falling** is illegal for “lifetime unique contacts, append-only, no deletes”. So either the tile is not that definition, or something deleted/recomputed.

### Check first

1. What query the dashboard runs (lifetime vs window vs segment) and whether it changed between 10:00 and 10:30.
2. Ingest 10:00–10:30: about 5k new delivered ids? Any deletes, deploys, or filter jobs?
3. Recompute from stored unique ids vs the API. Log still says 250k and API says 248k → serving bug. Both say 248k → data or definition changed.

Under **this** service (no deletes, no funnel, lifetime uniques), opened dropping is a bug. Delivered rising is not.

---

## What I completed / what I intentionally skipped

Completed: Parts 1, 4, 5; Go service (`POST /events`, `GET .../stats`, `GET .../events`); tests including seed survival, concurrent retries, and payload conflicts; official debugging four bugs; `BUGS.md`, `AI_USAGE.md`, `README.md`.

Skipped: SQLite (memory is enough at the stated traffic; restart loss is documented), auth, deploy, UI (brief: no extra credit). `candidate-only/` is the company pack this repo was built from; the runnable service is the root module.

---

## If I had another day

SQLite with a unique index on `event_id`; a reconcile command (recompute vs live counters); `(timestamp, event_id)` cursors instead of integer offsets; persist payload conflicts to a log marketers/on-call can see; load-test batch ingest against a 2s provider timeout; optional `from`/`to` on stats so the angry-marketer window is an explicit query.
