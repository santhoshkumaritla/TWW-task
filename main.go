package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
)

type API struct {
	store *Store
}

func newMux(store *Store) http.Handler {
	api := &API{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", api.handlePostEvents)
	mux.HandleFunc("GET /campaigns", api.handleListCampaigns)
	mux.HandleFunc("GET /campaigns/{campaignID}/stats", api.handleGetStats)
	mux.HandleFunc("GET /campaigns/{campaignID}/events", api.handleGetEvents)
	mux.HandleFunc("GET /seed/events.json", api.handleSeed)
	web, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	assets, err := fs.Sub(web, "assets")
	if err != nil {
		panic(err)
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, web, "index.html")
	})
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	return mux
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	fmt.Printf("listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, newMux(NewStore())))
}

type rejectedItem struct {
	Index   int    `json:"index"`
	EventID string `json:"event_id,omitempty"`
	Reason  string `json:"reason"`
}

type ingestResponse struct {
	Accepted         int            `json:"accepted"`
	Duplicates       int            `json:"duplicates"`
	Rejected         int            `json:"rejected"`
	PayloadConflicts []string       `json:"payload_conflicts,omitempty"`
	RejectedItems    []rejectedItem `json:"rejected_items,omitempty"`
}

// handlePostEvents reads a JSON array as []json.RawMessage so one bad object
// does not fail the decode of its siblings.
func (a *API) handlePostEvents(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body_too_large_or_unreadable"})
		return
	}

	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "body_must_be_json_array",
			"hint":  "POST a JSON array of event objects. One syntactically invalid JSON document cannot be recovered per-item.",
		})
		return
	}

	resp := ingestResponse{RejectedItems: []rejectedItem{}, PayloadConflicts: []string{}}
	for i, raw := range items {
		var ev Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			resp.Rejected++
			resp.RejectedItems = append(resp.RejectedItems, rejectedItem{Index: i, Reason: "not_an_object"})
			continue
		}
		if reason := validateEvent(ev); reason != "" {
			resp.Rejected++
			resp.RejectedItems = append(resp.RejectedItems, rejectedItem{Index: i, EventID: ev.EventID, Reason: reason})
			continue
		}
		out := a.store.Ingest(ev)
		switch out.Status {
		case "accepted":
			resp.Accepted++
		default:
			resp.Duplicates++
			if out.Conflict {
				resp.PayloadConflicts = append(resp.PayloadConflicts, ev.EventID)
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleListCampaigns(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"campaigns": a.store.CampaignIDs()})
}

func (a *API) handleSeed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(seedJSON)
}

func (a *API) handleGetStats(w http.ResponseWriter, r *http.Request) {
	campaignID := r.PathValue("campaignID")
	st := a.store.Stats(campaignID)
	writeJSON(w, http.StatusOK, map[string]any{
		"campaign_id":     st.CampaignID,
		"events":          st.Counts,
		"unique_contacts": st.UniqueContacts,
		"unique_opens":    st.UniqueOpens,
		"as_of":           "lifetime",
		"meaning": map[string]string{
			"events":          "Count of distinct event_ids per type. Same event_id never increments twice.",
			"unique_contacts": "Distinct contact_ids with at least one stored event of that type.",
			"unique_opens":    "unique_contacts.opened — people who opened, not raw open events.",
		},
	})
}

func (a *API) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	campaignID := r.PathValue("campaignID")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	cursor, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	writeJSON(w, http.StatusOK, a.store.List(campaignID, cursor, limit))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
