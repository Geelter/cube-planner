package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/collections"
)

// CollectionItemEntry is exported: huma derives OpenAPI schema names
// (and generated TS type names) from Go type names.
type CollectionItemEntry struct {
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
	Quantity        int32     `json:"quantity"`
}

func collectionEntryFrom(e collections.ItemEntry) CollectionItemEntry {
	return CollectionItemEntry{
		ScryfallID: e.ScryfallID, OracleID: e.OracleID, Name: e.Name,
		ManaCost: e.ManaCost, TypeLine: e.TypeLine, SetCode: e.SetCode,
		SetName: e.SetName, CollectorNumber: e.CollectorNumber,
		ImageSmall: e.ImageSmall, ImageNormal: e.ImageNormal,
		Quantity: e.Quantity,
	}
}

// ImportCardMatch is a resolved card for import review (match or
// suggestion) — CollectionItemEntry shape without quantity.
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

func importCardMatchFrom(r collections.CardRef) ImportCardMatch {
	return ImportCardMatch{
		ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
		ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
		SetName: r.SetName, CollectorNumber: r.CollectorNumber,
		ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
	}
}

func mapCollectionErr(err error) error {
	switch {
	case errors.Is(err, collections.ErrCubeNotFound):
		return huma.Error404NotFound("cube not found")
	case errors.Is(err, collections.ErrInvalidItem):
		return &huma.ErrorModel{
			Status: http.StatusUnprocessableEntity, Type: "invalid-collection-item",
			Title: "Unprocessable Entity", Detail: err.Error(),
		}
	case errors.Is(err, collections.ErrInvalidImport):
		return &huma.ErrorModel{
			Status: http.StatusUnprocessableEntity, Type: "invalid-import",
			Title: "Unprocessable Entity", Detail: err.Error(),
		}
	default:
		return err
	}
}

// parseScryfallID: a malformed printing id is just an unknown printing —
// same 422 family as the rest of the invalid-collection-item errors.
func parseScryfallID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, mapCollectionErr(collections.ErrInvalidItem)
	}
	return id, nil
}

type getCollectionInput struct {
	Search string `query:"search" maxLength:"100" doc:"Name filter (substring, case-insensitive)"`
	Limit  int32  `query:"limit" minimum:"1" maximum:"100" default:"50"`
	Offset int32  `query:"offset" minimum:"0" default:"0"`
}

type getCollectionOutput struct {
	Body struct {
		Items       []CollectionItemEntry `json:"items"`
		Total       int64                 `json:"total" doc:"Distinct printings matching the filter"`
		TotalCopies int64                 `json:"totalCopies" doc:"Sum of their quantities"`
	}
}

type setCollectionQuantityInput struct {
	ScryfallID string `path:"scryfallId"`
	Body       struct {
		Quantity int32 `json:"quantity" minimum:"0" maximum:"999" doc:"0 deletes the row"`
	}
}

type changePrintingInput struct {
	ScryfallID string `path:"scryfallId"`
	Body       struct {
		NewScryfallID uuid.UUID `json:"newScryfallId"`
	}
}

type collectionItemOutput struct {
	Body struct {
		Item *CollectionItemEntry `json:"item,omitempty" doc:"absent after a quantity-0 delete"`
	}
}

type resolveImportInput struct {
	Body struct {
		Text string `json:"text" minLength:"1" maxLength:"65536"`
	}
}

type resolveImportOutput struct {
	Body struct {
		Lines []ImportResolveLine `json:"lines"`
	}
}

type importItemsInput struct {
	Body struct {
		Items []struct {
			ScryfallID uuid.UUID `json:"scryfallId"`
			Quantity   int32     `json:"quantity" minimum:"1" maximum:"999"`
		} `json:"items" minItems:"1" maxItems:"500"`
	}
}

type importItemsOutput struct {
	Body struct {
		AddedRows   int32 `json:"addedRows"`
		UpdatedRows int32 `json:"updatedRows"`
	}
}

type WantlistEntry struct {
	OracleID        uuid.UUID `json:"oracleId"`
	ScryfallID      uuid.UUID `json:"scryfallId" doc:"The cube's chosen printing"`
	Name            string    `json:"name"`
	ManaCost        string    `json:"manaCost"`
	ImageSmall      *string   `json:"imageSmall"`
	ImageNormal     *string   `json:"imageNormal"`
	MissingQuantity int32     `json:"missingQuantity"`
	CubeQuantity    int32     `json:"cubeQuantity"`
	OwnedQuantity   int32     `json:"ownedQuantity"`
}

type getWantlistInput struct {
	CubeID string `path:"cubeId"`
}

type getWantlistOutput struct {
	Body struct {
		CubeName     string          `json:"cubeName"`
		Items        []WantlistEntry `json:"items"`
		TotalMissing int64           `json:"totalMissing"`
	}
}

func registerCollections(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "getCollection",
		Method:      http.MethodGet,
		Path:        "/api/collection",
		Summary:     "The session user's collection",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *getCollectionInput) (*getCollectionOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		entries, total, copies, err := deps.Collections.List(ctx, uid, in.Search, in.Limit, in.Offset)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &getCollectionOutput{}
		out.Body.Total = total
		out.Body.TotalCopies = copies
		out.Body.Items = make([]CollectionItemEntry, len(entries))
		for i, e := range entries {
			out.Body.Items[i] = collectionEntryFrom(e)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "setCollectionQuantity",
		Method:      http.MethodPut,
		Path:        "/api/collection/cards/{scryfallId}",
		Summary:     "Set the owned quantity of a printing (0 deletes)",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *setCollectionQuantityInput) (*collectionItemOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseScryfallID(in.ScryfallID)
		if err != nil {
			return nil, err
		}
		entry, err := deps.Collections.SetQuantity(ctx, uid, id, in.Body.Quantity)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &collectionItemOutput{}
		if entry != nil {
			e := collectionEntryFrom(*entry)
			out.Body.Item = &e
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "changeCollectionPrinting",
		Method:      http.MethodPost,
		Path:        "/api/collection/cards/{scryfallId}/change-printing",
		Summary:     "Re-key an owned printing to another printing of the same card",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *changePrintingInput) (*collectionItemOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseScryfallID(in.ScryfallID)
		if err != nil {
			return nil, err
		}
		entry, err := deps.Collections.ChangePrinting(ctx, uid, id, in.Body.NewScryfallID)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &collectionItemOutput{}
		e := collectionEntryFrom(*entry)
		out.Body.Item = &e
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "resolveCollectionImport",
		Method:      http.MethodPost,
		Path:        "/api/collection/import/resolve",
		Summary:     "Resolve a pasted card list (pure read, nothing is written)",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *resolveImportInput) (*resolveImportOutput, error) {
		if _, ok := CurrentUserID(ctx); !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		lines, err := deps.Collections.ResolveImport(ctx, in.Body.Text)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &resolveImportOutput{}
		out.Body.Lines = make([]ImportResolveLine, len(lines))
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
			out.Body.Lines[i] = rl
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "importCollectionItems",
		Method:      http.MethodPost,
		Path:        "/api/collection/import",
		Summary:     "Add quantities onto the collection (bulk import commit)",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *importItemsInput) (*importItemsOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		items := make([]collections.ImportItem, len(in.Body.Items))
		for i, it := range in.Body.Items {
			items[i] = collections.ImportItem{ScryfallID: it.ScryfallID, Quantity: it.Quantity}
		}
		added, updated, err := deps.Collections.ApplyImport(ctx, uid, items)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &importItemsOutput{}
		out.Body.AddedRows = added
		out.Body.UpdatedRows = updated
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getCubeWantlist",
		Method:      http.MethodGet,
		Path:        "/api/cubes/{cubeId}/wantlist",
		Summary:     "Cards in the cube missing from the caller's collection",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *getWantlistInput) (*getWantlistOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseCubeID(in.CubeID)
		if err != nil {
			return nil, err
		}
		name, items, totalMissing, err := deps.Collections.Wantlist(ctx, id, uid)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &getWantlistOutput{}
		out.Body.CubeName = name
		out.Body.TotalMissing = totalMissing
		out.Body.Items = make([]WantlistEntry, len(items))
		for i, it := range items {
			out.Body.Items[i] = WantlistEntry{
				OracleID: it.OracleID, ScryfallID: it.ScryfallID, Name: it.Name,
				ManaCost: it.ManaCost, ImageSmall: it.ImageSmall, ImageNormal: it.ImageNormal,
				MissingQuantity: it.MissingQuantity, CubeQuantity: it.CubeQuantity,
				OwnedQuantity: it.OwnedQuantity,
			}
		}
		return out, nil
	})
}
