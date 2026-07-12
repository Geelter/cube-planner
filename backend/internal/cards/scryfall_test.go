package cards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultCardsMetadata(t *testing.T) {
	var gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bulk-data/default-cards" {
			http.NotFound(w, r)
			return
		}
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"updated_at":   "2026-07-12T09:00:00Z",
			"download_uri": "https://data.test/default-cards.json",
		})
	}))
	defer srv.Close()

	c := NewScryfallClient(srv.URL, "cube-planner/test")
	meta, err := c.DefaultCardsMetadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	if !meta.UpdatedAt.Equal(want) || meta.DownloadURI != "https://data.test/default-cards.json" {
		t.Fatalf("meta = %+v", meta)
	}
	if gotUA != "cube-planner/test" || gotAccept != "application/json" {
		t.Fatalf("headers UA=%q Accept=%q; Scryfall requires both", gotUA, gotAccept)
	}
}

func TestDefaultCardsMetadataNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := NewScryfallClient(srv.URL, "cube-planner/test")
	if _, err := c.DefaultCardsMetadata(context.Background()); err == nil {
		t.Fatal("want error on non-200")
	}
}

func TestStreamCards(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name": "A"}, {"name": "B"}, {"name": "C"}]`))
	}))
	defer srv.Close()

	c := NewScryfallClient(srv.URL, "cube-planner/test")
	var names []string
	err := c.StreamCards(context.Background(), srv.URL+"/file", func(sc scryfallCard) error {
		names = append(names, sc.Name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 || names[0] != "A" || names[2] != "C" {
		t.Fatalf("names = %v", names)
	}
}

func TestStreamCardsMalformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name": "A"}, {"name":`)) // truncated
	}))
	defer srv.Close()
	c := NewScryfallClient(srv.URL, "cube-planner/test")
	err := c.StreamCards(context.Background(), srv.URL+"/file", func(scryfallCard) error { return nil })
	if err == nil {
		t.Fatal("want error on truncated JSON")
	}
}
