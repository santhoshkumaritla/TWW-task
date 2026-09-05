# AI usage

## Tools

- **Cursor** (Grok 4.6) — this session.
- The company pack (`ASSIGNMENT.md`, `starter/`, `debugging/`).
- Go stdlib docs (`encoding/json`, `net/http`, `time`).

## What I used them for

- First pass (wrong): MERN, because the pack was not used yet.
- This pass: company instructions — **Go** in `starter/`, four minimal fixes in `debugging/main.go`, tests against `starter/seed/events.json`.

## One suggestion from AI that I rejected or changed

Decoding POST into `[]Event`. The starter README warns that one bad element can fail the whole slice. I decode `[]json.RawMessage` and unmarshal each element.

Also rejected: rewriting `debugging/main.go`. Spec says minimal fixes: global `seenIDs`, campaign-scoped unique-open keys, `UTC()` dates, mutex in `apply`.

## One thing AI helped me understand

The comment “the stats map is never written concurrently” is true for inserting campaign keys (main goroutine) and false for `cs.Sent++` in `apply` (eight workers). That is the race `go run -race` is meant to show.

## Anything AI generated that I then had to debug

- `go run -race` needs cgo on this Windows install. Correctness checked with `go run . events.jsonl` vs `expected_output.txt`.
- Test helper used `t.fatal`; Go tests need `t.Fatal`.
