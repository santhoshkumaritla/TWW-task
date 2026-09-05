# AI usage

## Tools

- **Cursor** (Grok 4.6) — implementation, debugging, notes, tests.
- Company pack in `candidate-only/` (`ASSIGNMENT.md`, starter, debugging fixtures).
- Go stdlib docs (`encoding/json`, `net/http`, `time`, `sync`).

## What I used them for

- Reading the brief and writing Part 1 notes before treating the skeleton as finished.
- Filling `POST /events` / `GET .../stats` / `GET .../events` on the Go skeleton (per-item `json.RawMessage`, mutex store, tests against `seed/events.json`).
- Finding and fixing the four bugs in `debugging/main.go`.
- Drafting Parts 4–5 and tightening README / NOTES after a pass through the running tests.

## One suggestion from AI that I rejected or changed

Decoding the POST body into `[]Event`. That is what the starter `Event` type invites, and it is the failure mode the assignment calls out: one malformed element can fail the other 199. I decode `[]json.RawMessage` and unmarshal each element so `OPENED`, missing `contact_id`, and `not-a-time` reject *those* rows only.

Also rejected: rewriting `debugging/main.go`. The spec says minimal fixes. The four edits are global `seenIDs`, campaign-scoped unique-open keys, `UTC()` dates, and a mutex in `apply`.

## One thing AI helped me understand

In the official debugger, the comment “the stats map is never written concurrently” is true for *inserting campaign keys* (`processBatch` on the main goroutine) and false for `cs.Sent++` inside `apply`, which eight workers share. That is the race `go run -race` is meant to show.

## Anything AI generated that I then had to debug

- An earlier pass in this repo treated the stack as MERN and invented a debugging program. That cannot match `debugging/expected_output.txt`. Replaced with the official Go pack.
- `go run -race` needs cgo on this Windows install; I verified Part 3 with `go run . events.jsonl` vs `expected_output.txt`, not with the race detector.
- A concurrent-retry test originally sent `err` (still `nil`) when the status was not 200, so it would not have failed on a bad status. Fixed to report the status code.
