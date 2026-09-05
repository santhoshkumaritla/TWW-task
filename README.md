# Campaign Events

Go HTTP service for receiving provider webhook events and serving per-campaign
statistics to a live dashboard. The service uses the Go standard library and an
in-memory store. The frontend is embedded in the Go binary.

## Quick Start

Requirements: Go 1.22 or later.

```bash
go test
go run .
```

Open <http://localhost:8080/>. The server uses the `PORT` environment variable
when deployed and port `8080` locally by default.

The live deployment is available at <https://tww-task.onrender.com>.

## API

### `POST /events`

Accepts a JSON array of provider events. Each event has this shape:

```json
{
	"event_id": "evt_00042",
	"campaign_id": "cmp_summer_sale",
	"contact_id": "ct_017",
	"type": "opened",
	"timestamp": "2026-08-10T09:15:04Z",
	"metadata": {"url": "https://example.com/offer"}
}
```

Behavior:

- Returns `200` for a valid JSON array, including mixed accepted, duplicate,
	and rejected items.
- Returns `400` when the request body is not a JSON array or cannot be decoded.
- Decodes each array item independently, so one malformed item does not discard
	the rest of the batch.
- Response fields are `accepted`, `duplicates`, `rejected`, `rejected_items`,
	and `payload_conflicts`.

### `GET /campaigns/{campaign_id}/stats`

Returns lifetime statistics. An unknown campaign returns zero values with a
`200` response.

```powershell
Invoke-RestMethod http://localhost:8080/campaigns/cmp_summer_sale/stats
```

### `GET /campaigns/{campaign_id}/events`

Returns recent events ordered newest first by provider timestamp.

- `limit`: default `50`, maximum `200`
- `cursor`: zero-based pagination cursor

```bash
curl -s "http://localhost:8080/campaigns/cmp_summer_sale/events?limit=10"
```

## Counting Rules

- `event_id` is the provider identity for one event. The first accepted payload
	wins; retries do not increment counts again.
- `events` counts distinct provider events by type: `sent`, `delivered`,
	`opened`, and `clicked`.
- `unique_contacts` counts distinct contacts per campaign and event type.
- `unique_opens` counts distinct contacts who opened. Two opens by one contact
	count as two events but one unique open.
- There is no funnel requirement. An `opened` event can arrive before
	`delivered` and still counts.
- Late events are accepted. The provider timestamp is not compared with the
	server clock.
- Valid types are exactly `sent`, `delivered`, `opened`, and `clicked`.
- Timestamps must be RFC3339 values.

## Dashboard Workflow

Open <http://localhost:8080/> and select a campaign.

- **Load provider seed** uploads `seed/events.json`.
- **Replay same batch** simulates a retry; duplicate IDs do not increase counts.
- **Fire live open** creates an `opened` event.
- **Fire live click** creates a `clicked` event.
- **Fire live delivered** creates a `delivered` event.
- **Send conflicting retry** reuses an ID with different fields and reports a
	payload conflict while keeping the first payload.

The activity table displays provider timestamps in India Standard Time
(`Asia/Kolkata`) and labels them `IST`. The dashboard refreshes every four
seconds and shows event totals, unique contacts, observed rates, and recent
activity.

## Verification

Run the complete test suite:

```bash
go test ./...
```

Run the self-contained debugging exercise:

```bash
cd debugging
go run . events.jsonl
```

The output must match `expected_output.txt` exactly. The four fixes and their
verification notes are recorded in the root `BUGS.md`.

## Deployment

Render settings:

```text
Runtime: Go
Build Command: go build -o app .
Start Command: ./app
Root Directory: leave blank
```

Pushing to `main` triggers deployment. The store is in memory, so events are
cleared when the process restarts or redeploys. Persistent production retention
would require a database.

## Repository Layout

- `main.go`, `store.go`, `types.go`: HTTP API and in-memory persistence
- `web/`: embedded dashboard frontend
- `seed/events.json`: provider traffic fixture
- `debugging/`: self-contained debugging exercise
- `candidate-only/`: original assignment pack and starter materials
- `NOTES.md`: design decisions, scale memo, and angry-marketer analysis
- `BUGS.md`: debugging exercise findings
- `AI_USAGE.md`: AI usage disclosure
