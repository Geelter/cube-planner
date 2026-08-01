package cards

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// gzipJSONL encodes each value as one JSON line and gzips the result,
// mirroring Scryfall's bulk .jsonl.gz files.
func gzipJSONL(t *testing.T, values ...any) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	for _, v := range values {
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

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
			"updated_at":         "2026-07-12T09:00:00Z",
			"jsonl_download_uri": "https://data.test/default-cards.jsonl.gz",
		})
	}))
	defer srv.Close()

	c := NewScryfallClient(srv.URL, "cube-planner/test")
	meta, err := c.DefaultCardsMetadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	if !meta.UpdatedAt.Equal(want) || meta.DownloadURI != "https://data.test/default-cards.jsonl.gz" {
		t.Fatalf("meta = %+v", meta)
	}
	if gotUA != "cube-planner/test" || gotAccept != "application/json" {
		t.Fatalf("headers UA=%q Accept=%q; Scryfall requires both", gotUA, gotAccept)
	}
}

func TestDefaultCardsMetadataMissingDownloadURI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"updated_at": "2026-07-12T09:00:00Z",
		})
	}))
	defer srv.Close()
	c := NewScryfallClient(srv.URL, "cube-planner/test")
	if _, err := c.DefaultCardsMetadata(context.Background()); err == nil {
		t.Fatal("want error when descriptor has no jsonl_download_uri")
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
		_, _ = w.Write(gzipJSONL(
			t,
			map[string]string{"name": "A"},
			map[string]string{"name": "B"},
			map[string]string{"name": "C"},
		))
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
		full := gzipJSONL(t, map[string]string{"name": "A"}, map[string]string{"name": "B"})
		_, _ = w.Write(full[:len(full)-10]) // truncated mid-stream
	}))
	defer srv.Close()
	c := NewScryfallClient(srv.URL, "cube-planner/test")
	err := c.StreamCards(context.Background(), srv.URL+"/file", func(scryfallCard) error { return nil })
	if err == nil {
		t.Fatal("want error on truncated gzip stream")
	}
}

func TestStreamCardsNotGzip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name": "A"}]`)) // plain JSON, not gzip
	}))
	defer srv.Close()
	c := NewScryfallClient(srv.URL, "cube-planner/test")
	err := c.StreamCards(context.Background(), srv.URL+"/file", func(scryfallCard) error { return nil })
	if err == nil {
		t.Fatal("want error on non-gzip body")
	}
}
