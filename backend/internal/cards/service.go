package cards

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

// Service answers card search queries from the local mirror. Results are
// oracle-level with a representative printing; the SQL picks it.
type Service struct {
	queries *db.Queries
}

func NewService(queries *db.Queries) *Service {
	return &Service{queries: queries}
}

func (s *Service) Autocomplete(ctx context.Context, query string) ([]db.AutocompleteCardsRow, error) {
	normalized := NormalizeName(strings.TrimSpace(query))
	prefix := escapeLike(normalized)
	return s.queries.AutocompleteCards(ctx, db.AutocompleteCardsParams{
		Query:  normalized,
		Prefix: &prefix,
	})
}

// SearchParams: zero values mean "no filter". Colors: nil = no color
// filter; empty non-nil = colorless only (color_identity ⊆ {}).
type SearchParams struct {
	Name   string
	Colors []string
	Type   string
	CMCMin *float64
	CMCMax *float64
	Rarity string
	Set    string
	Limit  int32
	Offset int32
}

func (s *Service) Search(ctx context.Context, p SearchParams) ([]db.SearchCardsRow, int64, error) {
	params := db.SearchCardsParams{
		Colors:     p.Colors,
		CmcMin:     p.CMCMin,
		CmcMax:     p.CMCMax,
		PageLimit:  p.Limit,
		PageOffset: p.Offset,
	}
	if name := strings.TrimSpace(p.Name); name != "" {
		normalized := NormalizeName(name)
		prefix := escapeLike(normalized)
		params.Name = &normalized
		params.NamePrefix = &prefix
	}
	if t := strings.TrimSpace(p.Type); t != "" {
		escaped := escapeLike(t)
		params.CardType = &escaped
	}
	if p.Rarity != "" {
		params.Rarity = &p.Rarity
	}
	if p.Set != "" {
		set := strings.ToLower(p.Set)
		params.SetCode = &set
	}
	rows, err := s.queries.SearchCards(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}

func (s *Service) Printings(ctx context.Context, oracleID uuid.UUID) ([]db.Card, error) {
	return s.queries.GetPrintingsByOracleID(ctx, oracleID)
}

// NormalizeColorFilter maps the API color filter onto query semantics:
// nil = no filter. "C" (colorless) is not an identity color — it means
// "colorless cards wanted", and since colorless (empty identity) is a
// subset of any selection, stripping it is correct; "C" alone yields an
// empty set, i.e. colorless only.
func NormalizeColorFilter(colors []string) []string {
	if len(colors) == 0 {
		return nil
	}
	out := make([]string, 0, len(colors))
	for _, c := range colors {
		if c != "C" {
			out = append(out, c)
		}
	}
	return out
}
