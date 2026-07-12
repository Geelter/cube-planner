package cards

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultScryfallBaseURL = "https://api.scryfall.com"

// ScryfallClient talks to the Scryfall bulk-data API. Scryfall's API
// guidelines require identifying User-Agent and Accept headers.
type ScryfallClient struct {
	baseURL   string
	userAgent string
	http      *http.Client
}

func NewScryfallClient(baseURL, userAgent string) *ScryfallClient {
	if baseURL == "" {
		baseURL = defaultScryfallBaseURL
	}
	// No client-level timeout: the bulk download is ~450MB and can
	// legitimately take minutes. Cancellation comes from ctx.
	return &ScryfallClient{baseURL: baseURL, userAgent: userAgent, http: &http.Client{}}
}

type BulkMetadata struct {
	UpdatedAt   time.Time
	DownloadURI string
}

func (c *ScryfallClient) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: unexpected status %d", url, resp.StatusCode)
	}
	return resp, nil
}

// DefaultCardsMetadata fetches the bulk-data descriptor for the
// default_cards file: when it was last refreshed and where to download it.
func (c *ScryfallClient) DefaultCardsMetadata(ctx context.Context) (BulkMetadata, error) {
	resp, err := c.get(ctx, c.baseURL+"/bulk-data/default-cards")
	if err != nil {
		return BulkMetadata{}, err
	}
	defer resp.Body.Close()
	var body struct {
		UpdatedAt   time.Time `json:"updated_at"`
		DownloadURI string    `json:"download_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return BulkMetadata{}, fmt.Errorf("decode bulk metadata: %w", err)
	}
	return BulkMetadata{UpdatedAt: body.UpdatedAt, DownloadURI: body.DownloadURI}, nil
}

// StreamCards downloads the bulk file (a single huge JSON array) and
// invokes fn per card, decoding incrementally so the ~450MB body is never
// held in memory.
func (c *ScryfallClient) StreamCards(ctx context.Context, downloadURI string, fn func(scryfallCard) error) error {
	resp, err := c.get(ctx, downloadURI)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	if _, err := dec.Token(); err != nil { // opening '['
		return fmt.Errorf("read bulk array start: %w", err)
	}
	for dec.More() {
		var sc scryfallCard
		if err := dec.Decode(&sc); err != nil {
			return fmt.Errorf("decode bulk card: %w", err)
		}
		if err := fn(sc); err != nil {
			return err
		}
	}
	return nil
}
