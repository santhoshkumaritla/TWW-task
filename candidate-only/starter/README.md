# campaign-events starter

Go 1.22+, stdlib only, in-memory store. Built on the company skeleton.

## Run

    go test
    go run .

Then, in another terminal:

    curl -s -X POST localhost:8080/events \
      -H 'Content-Type: application/json' \
      --data-binary @seed/events.json

    curl -s localhost:8080/campaigns/cmp_summer_sale/stats

    curl -s "localhost:8080/campaigns/cmp_summer_sale/events?limit=10"

`POST /events` accepts a JSON array. Each element is decoded on its own so one bad object does not discard the rest. Duplicates (`event_id`) return 200 and do not increment counts.
