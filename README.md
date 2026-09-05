# Campaign Events

Go HTTP service that accepts provider webhook events and serves per-campaign stats for a live dashboard. In-memory store, stdlib only. No auth. Open `http://localhost:8080/` for the demo UI. The original company pack (assignment text, skeleton, fixtures) is under `candidate-only/`; this root module is what to run.

Live deployment: https://tww-task.onrender.com

The frontend and backend are deployed together as one Go web service. The frontend is embedded into the Go binary and uses the same URL as the API.

Requires Go 1.22+ (`http.ServeMux` method+path patterns).

```
go test
go run .
```

Listens on `http://localhost:8080`.

```
curl -s -X POST localhost:8080/events -H "Content-Type: application/json" --data-binary @seed/events.json

curl -s localhost:8080/campaigns/cmp_summer_sale/stats

curl -s "localhost:8080/campaigns/cmp_summer_sale/events?limit=10"
```

PowerShell:

```
Invoke-RestMethod -Method POST -Uri http://localhost:8080/events -ContentType application/json -InFile seed/events.json
Invoke-RestMethod http://localhost:8080/campaigns/cmp_summer_sale/stats
```

Command Prompt with the deployed service:

```
curl.exe -X POST https://tww-task.onrender.com/events -H "Content-Type: application/json" --data-binary "@seed\\events.json"
curl.exe https://tww-task.onrender.com/campaigns/cmp_summer_sale/stats
```

## UI workflow

Open `http://localhost:8080/` locally or the live deployment URL in a browser. The dashboard loads the campaign list, lifetime stats, and recent events from the API, then refreshes every four seconds.

- **Load provider seed** uploads `seed/events.json`.
- **Replay same batch** simulates a provider retry. Existing `event_id` values become duplicates and are not counted again.
- **Fire live open** creates a new `opened` event for the selected campaign.
- **Fire live click** creates a new `clicked` event for the selected campaign.
- **Fire live delivered** creates a new `delivered` event for the selected campaign.
- **Send conflicting retry** reuses an existing ID with different fields. The first stored payload still wins and the response reports a payload conflict.

The dashboard shows event totals, unique contacts, observed rates, and recent activity. The `events` counts count distinct provider events; `unique_opens` counts distinct contacts who opened.

## Deploy with Render

Create a Render Web Service from the `main` branch of `santhoshkumaritla/TWW-task` with these settings:

```
Runtime: Go
Build Command: go build -o app .
Start Command: ./app
Root Directory: leave blank
```

The application reads Render's `PORT` environment variable and uses port `8080` locally when `PORT` is not set. No environment variables are required for this demo. Pushing a new commit to `main` triggers a new deployment.

The store is in memory, so events are cleared when the service restarts or redeploys. A persistent database is required for production data retention.

## Endpoints

| Method | Path | Notes |
|---|---|---|
| POST | `/events` | JSON **array** of event objects. |
| GET | `/campaigns/{campaign_id}/stats` | Lifetime counts. Unknown campaign → zeros, not 404. |
| GET | `/campaigns/{campaign_id}/events` | Optional. Newest-first by provider `timestamp`. `?limit=` (default 50, max 200) and `?cursor=`. |

### POST /events

- **200** if the body is a JSON array, even when some items are invalid or duplicates. Providers retry on non-2xx; rejecting a mixed batch would make them resend events we already stored.
- **400** if the body is not a JSON array (object, truncated JSON, too large).
- Each array element is decoded on its own (`[]json.RawMessage`). One malformed object does not discard the rest.
- Response: `{ accepted, duplicates, rejected, rejected_items, payload_conflicts }`.

### Identity

Same event = same `event_id`. First accepted write wins. A later POST with that id is a duplicate (200, counts unchanged). If campaign/contact/type/timestamp disagree, it is still a duplicate, plus `payload_conflicts`.

### Stats

- `events` — distinct `event_id`s per type (the dashboard “how many were sent/delivered/opened/clicked” if that means unique provider events).
- `unique_contacts` / `unique_opens` — distinct people. Two opens by the same contact count as 2 events and 1 unique open.
- No funnel. An `opened` that arrives before `delivered` still counts as an open. Opened may be greater than delivered.
- Late events count; `timestamp` is not compared to wall clock.

Invalid: missing ids, empty `contact_id`, type not exactly `sent|delivered|opened|clicked` (so `OPENED` and `spam_report` reject), timestamp not RFC3339.

## Part 3

```
cd debugging
go run . events.jsonl
```

Must match `expected_output.txt`. See `BUGS.md`.
