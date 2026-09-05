# Campaign Events

Company assignment: **Go** backend, in-memory store, stdlib only. No frontend.

## Service (their starter)

```
cd starter
go test
go run .
```

Port `8080`.

```
curl -s -X POST localhost:8080/events -H "Content-Type: application/json" --data-binary @seed/events.json
curl -s localhost:8080/campaigns/cmp_summer_sale/stats
curl -s "localhost:8080/campaigns/cmp_summer_sale/events?limit=10"
```

## Part 3

```
cd debugging
go run . events.jsonl
```

Must match `expected_output.txt`. See `BUGS.md`.

## Written work

`NOTES.md` (parts 1, 4, 5), `AI_USAGE.md`, `BUGS.md`.
