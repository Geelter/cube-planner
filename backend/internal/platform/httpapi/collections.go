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

type collectionItemOutput struct {
	Body struct {
		Item *CollectionItemEntry `json:"item" doc:"null after a quantity-0 delete"`
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
}
