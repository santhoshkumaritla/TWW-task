package main

import (
	"sort"
	"sync"
	"time"
)

var allowedTypes = map[string]bool{
	"sent":      true,
	"delivered": true,
	"opened":    true,
	"clicked":   true,
}

type Counts struct {
	Sent      int `json:"sent"`
	Delivered int `json:"delivered"`
	Opened    int `json:"opened"`
	Clicked   int `json:"clicked"`
}

type campaignState struct {
	counts   Counts
	contacts map[string]map[string]struct{} // type -> contact_id set
	events   []Event
}

// Store is an in-memory event log plus per-campaign counters.
// A mutex covers ingest and reads: a few thousand events/day does not need
// sharding. Duplicate event_id is decided under the same lock so two retries
// cannot both increment.
type Store struct {
	mu       sync.Mutex
	byID     map[string]Event
	campaign map[string]*campaignState
}

func NewStore() *Store {
	return &Store{
		byID:     make(map[string]Event),
		campaign: make(map[string]*campaignState),
	}
}

type IngestResult struct {
	Status   string // accepted | duplicate | conflict
	Conflict bool
}

func (s *Store) Ingest(ev Event) IngestResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.byID[ev.EventID]; ok {
		conflict := existing.CampaignID != ev.CampaignID ||
			existing.ContactID != ev.ContactID ||
			existing.Type != ev.Type ||
			existing.Timestamp != ev.Timestamp
		return IngestResult{Status: "duplicate", Conflict: conflict}
	}

	s.byID[ev.EventID] = ev
	st := s.campaign[ev.CampaignID]
	if st == nil {
		st = &campaignState{contacts: map[string]map[string]struct{}{
			"sent": {}, "delivered": {}, "opened": {}, "clicked": {},
		}}
		s.campaign[ev.CampaignID] = st
	}
	switch ev.Type {
	case "sent":
		st.counts.Sent++
	case "delivered":
		st.counts.Delivered++
	case "opened":
		st.counts.Opened++
	case "clicked":
		st.counts.Clicked++
	}
	st.contacts[ev.Type][ev.ContactID] = struct{}{}
	st.events = append(st.events, ev)
	return IngestResult{Status: "accepted"}
}

type Stats struct {
	CampaignID     string `json:"campaign_id"`
	Counts         Counts `json:"events"`
	UniqueContacts Counts `json:"unique_contacts"`
	UniqueOpens    int    `json:"unique_opens"`
}

func (s *Store) CampaignIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.campaign))
	for id := range s.campaign {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (s *Store) Stats(campaignID string) Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Stats{CampaignID: campaignID}
	st := s.campaign[campaignID]
	if st == nil {
		return out
	}
	out.Counts = st.counts
	out.UniqueContacts = Counts{
		Sent:      len(st.contacts["sent"]),
		Delivered: len(st.contacts["delivered"]),
		Opened:    len(st.contacts["opened"]),
		Clicked:   len(st.contacts["clicked"]),
	}
	out.UniqueOpens = out.UniqueContacts.Opened
	return out
}

type EventPage struct {
	CampaignID string  `json:"campaign_id"`
	Events     []Event `json:"events"`
	NextCursor *int    `json:"next_cursor"`
}

func (s *Store) List(campaignID string, cursor, limit int) EventPage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if cursor < 0 {
		cursor = 0
	}
	st := s.campaign[campaignID]
	page := EventPage{CampaignID: campaignID, Events: []Event{}}
	if st == nil || cursor >= len(st.events) {
		return page
	}
	ordered := append([]Event{}, st.events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		ti, okI := parseTimestamp(ordered[i].Timestamp)
		tj, okJ := parseTimestamp(ordered[j].Timestamp)
		if okI && okJ && !ti.Equal(tj) {
			return ti.After(tj)
		}
		return ordered[i].EventID > ordered[j].EventID
	})
	end := cursor + limit
	if end > len(ordered) {
		end = len(ordered)
	}
	page.Events = append([]Event{}, ordered[cursor:end]...)
	if end < len(ordered) {
		n := end
		page.NextCursor = &n
	}
	return page
}

func parseTimestamp(ts string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t, true
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	return t, err == nil
}

func validTimestamp(ts string) bool {
	_, ok := parseTimestamp(ts)
	return ok
}

func validateEvent(ev Event) string {
	if ev.EventID == "" {
		return "invalid_event_id"
	}
	if ev.CampaignID == "" {
		return "invalid_campaign_id"
	}
	if ev.ContactID == "" {
		return "invalid_contact_id"
	}
	if !allowedTypes[ev.Type] {
		return "invalid_type"
	}
	if !validTimestamp(ev.Timestamp) {
		return "invalid_timestamp"
	}
	return ""
}
