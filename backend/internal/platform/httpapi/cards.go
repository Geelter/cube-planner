package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/cards"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

// CardSummary / CardDetail are exported: huma derives the OpenAPI schema
// names (and thus the generated TS type names) from the Go type names.
type CardSummary struct {
	ScryfallID uuid.UUID `json:"scryfallId" doc:"Representative printing"`
	OracleID   uuid.UUID `json:"oracleId"`
	Name       string    `json:"name"`
	ManaCost   string    `json:"manaCost"`
	TypeLine   string    `json:"typeLine"`
	Colors     []string  `json:"colors"`
	ImageSmall *string   `json:"imageSmall"`
}

type CardDetail struct {
	ScryfallID      uuid.UUID `json:"scryfallId"`
	OracleID        uuid.UUID `json:"oracleId"`
	Name            string    `json:"name"`
	ManaCost        string    `json:"manaCost"`
	TypeLine        string    `json:"typeLine"`
	OracleText      string    `json:"oracleText"`
	SetCode         string    `json:"setCode"`
	SetName         string    `json:"setName"`
	CollectorNumber string    `json:"collectorNumber"`
	Rarity          string    `json:"rarity"`
	ReleasedAt      string    `json:"releasedAt" doc:"YYYY-MM-DD"`
	Cmc             float64   `json:"cmc"`
	Colors          []string  `json:"colors"`
	ColorIdentity   []string  `json:"colorIdentity"`
	Promo           bool      `json:"promo"`
	ImageSmall      *string   `json:"imageSmall"`
	ImageNormal     *string   `json:"imageNormal"`
	BackImageNormal *string   `json:"backImageNormal"`
}

func cardDetailFrom(c db.Card) CardDetail {
	return CardDetail{
		ScryfallID:      c.ScryfallID,
		OracleID:        c.OracleID,
		Name:            c.Name,
		ManaCost:        c.ManaCost,
		TypeLine:        c.TypeLine,
		OracleText:      c.OracleText,
		SetCode:         c.SetCode,
		SetName:         c.SetName,
		CollectorNumber: c.CollectorNumber,
		Rarity:          c.Rarity,
		ReleasedAt:      c.ReleasedAt.Format(time.DateOnly),
		Cmc:             c.Cmc,
		Colors:          c.Colors,
		ColorIdentity:   c.ColorIdentity,
		Promo:           c.Promo,
		ImageSmall:      c.ImageSmall,
		ImageNormal:     c.ImageNormal,
		BackImageNormal: c.BackImageNormal,
	}
}

type autocompleteCardsInput struct {
	Q string `query:"q" required:"true" minLength:"2" maxLength:"100" doc:"Card name, fuzzy"`
}

type autocompleteCardsOutput struct {
	Body struct {
		Cards []CardSummary `json:"cards"`
	}
}

// huma v2.38 panics on pointer-typed query params ("pointers are not
// supported for form/header/path/query parameters"), so optional filters
// use value types: numeric sentinels (cmcMax -1 = no upper bound; cmcMin 0
// is a natural no-op) and empty string for rarity, mapped in the handler.
type searchCardsInput struct {
	Name   string   `query:"name" maxLength:"100" doc:"Fuzzy name filter"`
	Colors []string `query:"colors" enum:"W,U,B,R,G,C" doc:"Color identity ⊆ selection; C = include colorless-only"`
	Type   string   `query:"type" maxLength:"100" doc:"Type line substring"`
	CmcMin float64  `query:"cmcMin" minimum:"0" default:"0" doc:"Lower CMC bound (0 = none)"`
	CmcMax float64  `query:"cmcMax" minimum:"-1" default:"-1" doc:"Upper CMC bound (-1 = none)"`
	Rarity string   `query:"rarity" enum:"common,uncommon,rare,mythic,special,bonus"`
	Set    string   `query:"set" maxLength:"10" doc:"Set code"`
	Limit  int32    `query:"limit" minimum:"1" maximum:"100" default:"20"`
	Offset int32    `query:"offset" minimum:"0" default:"0"`
}

type searchCardsOutput struct {
	Body struct {
		Cards []CardDetail `json:"cards"`
		Total int64        `json:"total"`
	}
}

// No format:"uuid" tag: huma would reject malformed IDs with 422 before the
// handler runs, but oracle IDs are opaque — a malformed one is just an
// unknown card, and the handler maps it to 404.
type cardPrintingsInput struct {
	OracleID string `path:"oracleId"`
}

type cardPrintingsOutput struct {
	Body struct {
		Printings []CardDetail `json:"printings"`
	}
}

// ImportCardMatch is a resolved card for import review (match or
// suggestion) — CardSummary-shaped without the color/type fields the
// review UI doesn't render.
type ImportCardMatch struct {
	ScryfallID      uuid.UUID `json:"scryfallId"`
	OracleID        uuid.UUID `json:"oracleId"`
	Name            string    `json:"name"`
	ManaCost        string    `json:"manaCost"`
	TypeLine        string    `json:"typeLine"`
	SetCode         string    `json:"setCode"`
	SetName         string    `json:"setName"`
	CollectorNumber string    `json:"collectorNumber"`
	ImageSmall      *string   `json:"imageSmall"`
	ImageNormal     *string   `json:"imageNormal"`
}

type ImportResolveLine struct {
	LineNumber  int32             `json:"lineNumber"`
	Raw         string            `json:"raw"`
	Quantity    int32             `json:"quantity"`
	Status      string            `json:"status" enum:"matched,ambiguous,unmatched"`
	Match       *ImportCardMatch  `json:"match,omitempty"`
	Suggestions []ImportCardMatch `json:"suggestions,omitempty"`
}

func importCardMatchFrom(r cards.CardRef) ImportCardMatch {
	return ImportCardMatch{
		ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
		ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
		SetName: r.SetName, CollectorNumber: r.CollectorNumber,
		ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
	}
}

func resolveLinesFrom(lines []cards.ResolvedLine) []ImportResolveLine {
	out := make([]ImportResolveLine, len(lines))
	for i, l := range lines {
		rl := ImportResolveLine{
			LineNumber: l.LineNumber, Raw: l.Raw, Quantity: l.Quantity, Status: l.Status,
		}
		if l.Match != nil {
			m := importCardMatchFrom(*l.Match)
			rl.Match = &m
		}
		for _, s := range l.Suggestions {
			rl.Suggestions = append(rl.Suggestions, importCardMatchFrom(s))
		}
		out[i] = rl
	}
	return out
}

type resolveListOutput struct {
	Body struct {
		Lines []ImportResolveLine `json:"lines"`
	}
}

func mapCardsErr(err error) error {
	switch {
	case errors.Is(err, cards.ErrTooManyLines):
		return &huma.ErrorModel{
			Status: http.StatusUnprocessableEntity, Type: "invalid-import",
			Title: "Unprocessable Entity", Detail: err.Error(),
		}
	default:
		return err
	}
}

func registerCards(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "autocompleteCards",
		Method:      http.MethodGet,
		Path:        "/api/cards/autocomplete",
		Summary:     "Card name autocomplete",
		Description: "Typo-tolerant name search. Oracle-level: one entry per card with a representative printing.",
		Tags:        []string{"cards"},
	}, func(ctx context.Context, in *autocompleteCardsInput) (*autocompleteCardsOutput, error) {
		rows, err := deps.Cards.Autocomplete(ctx, in.Q)
		if err != nil {
			return nil, err
		}
		out := &autocompleteCardsOutput{}
		out.Body.Cards = make([]CardSummary, len(rows))
		for i, r := range rows {
			out.Body.Cards[i] = CardSummary{
				ScryfallID: r.ScryfallID,
				OracleID:   r.OracleID,
				Name:       r.Name,
				ManaCost:   r.ManaCost,
				TypeLine:   r.TypeLine,
				Colors:     r.Colors,
				ImageSmall: r.ImageSmall,
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "searchCards",
		Method:      http.MethodGet,
		Path:        "/api/cards/search",
		Summary:     "Search cards with filters",
		Tags:        []string{"cards"},
	}, func(ctx context.Context, in *searchCardsInput) (*searchCardsOutput, error) {
		params := cards.SearchParams{
			Name:   in.Name,
			Colors: cards.NormalizeColorFilter(in.Colors),
			Type:   in.Type,
			Rarity: in.Rarity,
			Set:    in.Set,
			Limit:  in.Limit,
			Offset: in.Offset,
		}
		if in.CmcMin > 0 {
			params.CMCMin = &in.CmcMin
		}
		if in.CmcMax >= 0 {
			params.CMCMax = &in.CmcMax
		}
		rows, total, err := deps.Cards.Search(ctx, params)
		if err != nil {
			return nil, err
		}
		out := &searchCardsOutput{}
		out.Body.Total = total
		out.Body.Cards = make([]CardDetail, len(rows))
		for i, r := range rows {
			out.Body.Cards[i] = cardDetailFrom(db.Card{
				ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
				NormalizedName: r.NormalizedName, ReleasedAt: r.ReleasedAt,
				SetCode: r.SetCode, SetName: r.SetName, CollectorNumber: r.CollectorNumber,
				Rarity: r.Rarity, Layout: r.Layout, ManaCost: r.ManaCost, Cmc: r.Cmc,
				TypeLine: r.TypeLine, OracleText: r.OracleText, Colors: r.Colors,
				ColorIdentity: r.ColorIdentity, Promo: r.Promo, ImageSmall: r.ImageSmall,
				ImageNormal: r.ImageNormal, BackImageSmall: r.BackImageSmall,
				BackImageNormal: r.BackImageNormal, UpdatedAt: r.UpdatedAt,
			})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cardPrintings",
		Method:      http.MethodGet,
		Path:        "/api/cards/{oracleId}/printings",
		Summary:     "All printings of an oracle card, newest first",
		Tags:        []string{"cards"},
	}, func(ctx context.Context, in *cardPrintingsInput) (*cardPrintingsOutput, error) {
		oracleID, err := uuid.Parse(in.OracleID)
		if err != nil {
			// Oracle IDs are opaque; a malformed one is just unknown.
			return nil, huma.Error404NotFound("unknown card")
		}
		rows, err := deps.Cards.Printings(ctx, oracleID)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, huma.Error404NotFound("unknown card")
		}
		out := &cardPrintingsOutput{}
		out.Body.Printings = make([]CardDetail, len(rows))
		for i, r := range rows {
			out.Body.Printings[i] = cardDetailFrom(r)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "resolveCardList",
		Method:      http.MethodPost,
		Path:        "/api/cards/resolve-list",
		Summary:     "Resolve a pasted card list to printings (no side effects)",
		Tags:        []string{"cards"},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Text string `json:"text" minLength:"1" maxLength:"65536"`
		}
	},
	) (*resolveListOutput, error) {
		if _, ok := CurrentUserID(ctx); !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		lines, err := deps.Cards.ResolveList(ctx, in.Body.Text)
		if err != nil {
			return nil, mapCardsErr(err)
		}
		out := &resolveListOutput{}
		out.Body.Lines = resolveLinesFrom(lines)
		return out, nil
	})
}
