package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func errStatus(code int) error {
	return fmt.Errorf("unexpected status %d", code)
}

func sample(over map[string]any) map[string]any {
	ev := map[string]any{
		"event_id":    "evt_00042",
		"campaign_id": "cmp_summer_sale",
		"contact_id":  "ct_017",
		"type":        "opened",
		"timestamp":   "2026-08-10T09:15:04Z",
		"metadata":    map[string]string{"url": "https://example.com/offer"},
	}
	for k, v := range over {
		ev[k] = v
	}
	return ev
}

func post(t *testing.T, srv *httptest.Server, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(srv.URL+"/events", "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestRejectsNonArray(t *testing.T) {
	srv := httptest.NewServer(newMux(NewStore()))
	defer srv.Close()
	res := post(t, srv, sample(nil))
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestMixedBatchAndDuplicates(t *testing.T) {
	srv := httptest.NewServer(newMux(NewStore()))
	defer srv.Close()
	res := post(t, srv, []any{
		sample(map[string]any{"event_id": "evt_a", "type": "sent"}),
		"not-an-object",
		sample(map[string]any{"event_id": "evt_b", "type": "spam_report"}),
		sample(map[string]any{"event_id": "evt_c", "timestamp": "not-a-time"}),
		sample(map[string]any{"event_id": "evt_d", "campaign_id": ""}),
	})
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var got ingestResponse
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Accepted != 1 || got.Rejected != 4 {
		t.Fatalf("accepted=%d rejected=%d", got.Accepted, got.Rejected)
	}

	res2 := post(t, srv, []any{
		sample(map[string]any{"event_id": "evt_a", "type": "sent"}),
		sample(map[string]any{"event_id": "evt_e", "type": "opened"}),
	})
	defer res2.Body.Close()
	var again ingestResponse
	json.NewDecoder(res2.Body).Decode(&again)
	if again.Accepted != 1 || again.Duplicates != 1 {
		t.Fatalf("accepted=%d duplicates=%d", again.Accepted, again.Duplicates)
	}
}

func TestDoesNotDoubleCount(t *testing.T) {
	store := NewStore()
	srv := httptest.NewServer(newMux(store))
	defer srv.Close()
	body := []any{sample(map[string]any{"event_id": "dup_1", "type": "sent", "campaign_id": "cmp_dedup"})}
	res1 := post(t, srv, body)
	res1.Body.Close()
	res2 := post(t, srv, body)
	res2.Body.Close()
	st := store.Stats("cmp_dedup")
	if st.Counts.Sent != 1 {
		t.Fatalf("sent=%d", st.Counts.Sent)
	}
}

func TestOpenedBeforeDelivered(t *testing.T) {
	store := NewStore()
	srv := httptest.NewServer(newMux(store))
	defer srv.Close()
	res := post(t, srv, []any{
		sample(map[string]any{"event_id": "late_open", "campaign_id": "cmp_order", "contact_id": "ct_1", "type": "opened"}),
		sample(map[string]any{"event_id": "later_del", "campaign_id": "cmp_order", "contact_id": "ct_1", "type": "delivered", "timestamp": "2026-08-10T09:14:00Z"}),
	})
	res.Body.Close()
	st := store.Stats("cmp_order")
	if st.UniqueContacts.Opened != 1 || st.UniqueContacts.Delivered != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestUniqueContactsVsEvents(t *testing.T) {
	store := NewStore()
	srv := httptest.NewServer(newMux(store))
	defer srv.Close()
	res := post(t, srv, []any{
		sample(map[string]any{"event_id": "o1", "campaign_id": "cmp_u", "contact_id": "ct_1", "type": "opened"}),
		sample(map[string]any{"event_id": "o2", "campaign_id": "cmp_u", "contact_id": "ct_1", "type": "opened"}),
	})
	res.Body.Close()
	st := store.Stats("cmp_u")
	if st.Counts.Opened != 2 || st.UniqueOpens != 1 {
		t.Fatalf("events.opened=%d unique_opens=%d", st.Counts.Opened, st.UniqueOpens)
	}
}

func TestConcurrentRetries(t *testing.T) {
	store := NewStore()
	srv := httptest.NewServer(newMux(store))
	defer srv.Close()
	payload := `[{"event_id":"race_1","campaign_id":"cmp_race","contact_id":"ct_017","type":"clicked","timestamp":"2026-08-10T09:15:04Z"}]`
	var wg sync.WaitGroup
	errCh := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := http.Post(srv.URL+"/events", "application/json", strings.NewReader(payload))
			if err != nil {
				errCh <- err
				return
			}
			res.Body.Close()
			if res.StatusCode != 200 {
				errCh <- errStatus(res.StatusCode)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	st := store.Stats("cmp_race")
	if st.Counts.Clicked != 1 {
		t.Fatalf("clicked=%d", st.Counts.Clicked)
	}
}

func TestSeedSurvives(t *testing.T) {
	raw, err := os.ReadFile("seed/events.json")
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore()
	srv := httptest.NewServer(newMux(store))
	defer srv.Close()
	res, err := http.Post(srv.URL+"/events", "application/json", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var got ingestResponse
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Accepted == 0 {
		t.Fatal("expected some accepted events")
	}
	if got.Rejected == 0 {
		t.Fatal("seed is supposed to contain rejects")
	}
	st := store.Stats("cmp_summer_sale")
	if st.Counts.Sent == 0 || st.Counts.Delivered == 0 {
		t.Fatalf("summer_sale stats %+v", st)
	}
}

func TestEventListPagination(t *testing.T) {
	store := NewStore()
	srv := httptest.NewServer(newMux(store))
	defer srv.Close()
	res := post(t, srv, []any{
		sample(map[string]any{"event_id": "p1", "campaign_id": "cmp_page", "type": "sent"}),
		sample(map[string]any{"event_id": "p2", "campaign_id": "cmp_page", "type": "delivered"}),
		sample(map[string]any{"event_id": "p3", "campaign_id": "cmp_page", "type": "opened"}),
	})
	res.Body.Close()
	r, err := http.Get(srv.URL + "/campaigns/cmp_page/events?limit=2")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var page EventPage
	json.NewDecoder(r.Body).Decode(&page)
	if len(page.Events) != 2 || page.NextCursor == nil || *page.NextCursor != 2 {
		t.Fatalf("%+v", page)
	}
	if page.Events[0].EventID != "p3" || page.Events[1].EventID != "p2" {
		t.Fatalf("expected newest-first by event_id tie-break, got %s %s", page.Events[0].EventID, page.Events[1].EventID)
	}
}

func TestPayloadConflictIsDuplicateNotRecount(t *testing.T) {
	store := NewStore()
	srv := httptest.NewServer(newMux(store))
	defer srv.Close()
	res := post(t, srv, []any{sample(map[string]any{"event_id": "same", "type": "sent", "campaign_id": "cmp_c"})})
	res.Body.Close()
	res2 := post(t, srv, []any{sample(map[string]any{"event_id": "same", "type": "opened", "campaign_id": "cmp_other"})})
	defer res2.Body.Close()
	var got ingestResponse
	json.NewDecoder(res2.Body).Decode(&got)
	if got.Duplicates != 1 || got.Accepted != 0 || len(got.PayloadConflicts) != 1 {
		t.Fatalf("%+v", got)
	}
	if store.Stats("cmp_c").Counts.Sent != 1 || store.Stats("cmp_other").Counts.Opened != 0 {
		t.Fatal("conflict must not move counts")
	}
}
