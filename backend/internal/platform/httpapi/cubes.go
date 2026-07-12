package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/cubes"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

// Exported wire types: huma derives OpenAPI schema names (and generated
// TS type names) from Go type names.
type CubeSummary struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	OwnerName   string    `json:"ownerName"`
	CardCount   int64     `json:"cardCount" doc:"Sum of quantities"`
	Visibility  string    `json:"visibility" enum:"public,private"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type CubeDetail struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	OwnerID     uuid.UUID `json:"ownerId"`
	OwnerName   string    `json:"ownerName"`
	CardCount   int64     `json:"cardCount"`
	Visibility  string    `json:"visibility" enum:"public,private"`
	Version     int32     `json:"version" doc:"Optimistic-concurrency token for commits"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type CubeCardEntry struct {
	ScryfallID    uuid.UUID `json:"scryfallId"`
	OracleID      uuid.UUID `json:"oracleId"`
	Name          string    `json:"name"`
	ManaCost      string    `json:"manaCost"`
	TypeLine      string    `json:"typeLine"`
	Cmc           float64   `json:"cmc"`
	Colors        []string  `json:"colors"`
	ColorIdentity []string  `json:"colorIdentity"`
	Rarity        string    `json:"rarity"`
	ImageSmall    *string   `json:"imageSmall"`
	ImageNormal   *string   `json:"imageNormal"`
	Quantity      int32     `json:"quantity"`
}

type ChangelogItem struct {
	OracleID uuid.UUID `json:"oracleId"`
	Name     string    `json:"name"`
	Quantity int32     `json:"quantity"`
}

type ChangelogEntry struct {
	ID         uuid.UUID       `json:"id"`
	Version    int32           `json:"version"`
	AuthorName string          `json:"authorName"`
	Note       string          `json:"note"`
	CreatedAt  time.Time       `json:"createdAt"`
	Adds       []ChangelogItem `json:"adds"`
	Removes    []ChangelogItem `json:"removes"`
}

type CubeChangeAdd struct {
	ScryfallID uuid.UUID `json:"scryfallId"`
	Quantity   int32     `json:"quantity" minimum:"1" maximum:"99"`
}

type CubeChangeRemove struct {
	OracleID uuid.UUID `json:"oracleId"`
	Quantity int32     `json:"quantity" minimum:"1" maximum:"99"`
}

func cubeDetailFrom(r db.GetCubeRow) CubeDetail {
	return CubeDetail{
		ID: r.ID, Name: r.Name, Description: r.Description,
		OwnerID: r.OwnerID, OwnerName: r.OwnerName, CardCount: r.CardCount,
		Visibility: r.Visibility, Version: r.Version,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func cardEntryFrom(e cubes.CardEntry) CubeCardEntry {
	return CubeCardEntry{
		ScryfallID: e.ScryfallID, OracleID: e.OracleID, Name: e.Name,
		ManaCost: e.ManaCost, TypeLine: e.TypeLine, Cmc: e.Cmc,
		Colors: e.Colors, ColorIdentity: e.ColorIdentity, Rarity: e.Rarity,
		ImageSmall: e.ImageSmall, ImageNormal: e.ImageNormal, Quantity: e.Quantity,
	}
}

func changelogEntryFrom(e cubes.ChangeEntry) ChangelogEntry {
	out := ChangelogEntry{
		ID: e.ID, Version: e.Version, AuthorName: e.AuthorName,
		Note: e.Note, CreatedAt: e.CreatedAt,
		Adds: []ChangelogItem{}, Removes: []ChangelogItem{},
	}
	for _, i := range e.Adds {
		out.Adds = append(out.Adds, ChangelogItem(i))
	}
	for _, i := range e.Removes {
		out.Removes = append(out.Removes, ChangelogItem(i))
	}
	return out
}

// mapCubeErr translates service errors to problem+json responses. The 409
// and 422 problems carry distinct type URIs the frontend can branch on
// (status code is the primary discriminator).
func mapCubeErr(err error) error {
	switch {
	case errors.Is(err, cubes.ErrNotFound):
		return huma.Error404NotFound("cube not found")
	case errors.Is(err, cubes.ErrForbidden):
		return huma.Error403Forbidden("not the cube owner")
	case errors.Is(err, cubes.ErrVersionConflict):
		return &huma.ErrorModel{
			Status: http.StatusConflict, Type: "cube-version-conflict",
			Title: "Conflict", Detail: "cube was modified since you loaded it",
		}
	case errors.Is(err, cubes.ErrInvalidChange):
		return &huma.ErrorModel{
			Status: http.StatusUnprocessableEntity, Type: "invalid-cube-change",
			Title: "Unprocessable Entity", Detail: err.Error(),
		}
	default:
		return err
	}
}

// parseCubeID: malformed ids are just unknown cubes (same rationale as
// cardPrintings), not validation errors.
func parseCubeID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, huma.Error404NotFound("cube not found")
	}
	return id, nil
}

func viewerID(ctx context.Context) uuid.UUID {
	uid, _ := CurrentUserID(ctx)
	return uid // uuid.Nil when anonymous
}

type createCubeInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1" maxLength:"100"`
		Description string `json:"description,omitempty" maxLength:"2000"`
		Visibility  string `json:"visibility" enum:"public,private"`
	}
}

type cubeDetailOutput struct {
	Body CubeDetail
}

type getCubeInput struct {
	CubeID string `path:"cubeId"`
}

type updateCubeInput struct {
	CubeID string `path:"cubeId"`
	Body   struct {
		Name        *string `json:"name,omitempty" minLength:"1" maxLength:"100"`
		Description *string `json:"description,omitempty" maxLength:"2000"`
		Visibility  *string `json:"visibility,omitempty" enum:"public,private"`
	}
}

type listCubesInput struct {
	Q      string `query:"q" maxLength:"100" doc:"Fuzzy name filter (min 2 chars to take effect)"`
	Limit  int32  `query:"limit" minimum:"1" maximum:"100" default:"20"`
	Offset int32  `query:"offset" minimum:"0" default:"0"`
}

type listCubesOutput struct {
	Body struct {
		Cubes []CubeSummary `json:"cubes"`
		Total int64         `json:"total"`
	}
}

type getCubeCardsInput struct {
	CubeID string `path:"cubeId"`
	// huma v2.38 panics on pointer query params; -1 = current version.
	AtVersion int32 `query:"atVersion" minimum:"-1" default:"-1" doc:"-1 = current; otherwise a historical version to reconstruct"`
}

type getCubeCardsOutput struct {
	Body struct {
		Cards   []CubeCardEntry `json:"cards"`
		Version int32           `json:"version"`
	}
}

type listCubeChangesInput struct {
	CubeID string `path:"cubeId"`
	Limit  int32  `query:"limit" minimum:"1" maximum:"100" default:"20"`
	Offset int32  `query:"offset" minimum:"0" default:"0"`
}

type listCubeChangesOutput struct {
	Body struct {
		Changes []ChangelogEntry `json:"changes"`
		Total   int64            `json:"total"`
	}
}

type commitCubeChangeInput struct {
	CubeID string `path:"cubeId"`
	Body   struct {
		ExpectedVersion int32              `json:"expectedVersion" minimum:"0"`
		Note            string             `json:"note,omitempty" maxLength:"500"`
		Adds            []CubeChangeAdd    `json:"adds,omitempty" maxItems:"1000"`
		Removes         []CubeChangeRemove `json:"removes,omitempty" maxItems:"1000"`
	}
}

type commitCubeChangeOutput struct {
	Body struct {
		Change  ChangelogEntry `json:"change"`
		Version int32          `json:"version"`
	}
}

func registerCubes(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID:   "createCube",
		Method:        http.MethodPost,
		Path:          "/api/cubes",
		Summary:       "Create a cube",
		Tags:          []string{"cubes"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createCubeInput) (*cubeDetailOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		row, err := deps.Cubes.Create(ctx, uid, in.Body.Name, in.Body.Description, in.Body.Visibility)
		if err != nil {
			return nil, mapCubeErr(err)
		}
		return &cubeDetailOutput{Body: cubeDetailFrom(row)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listCubes",
		Method:      http.MethodGet,
		Path:        "/api/cubes",
		Summary:     "Browse public cubes",
		Tags:        []string{"cubes"},
	}, func(ctx context.Context, in *listCubesInput) (*listCubesOutput, error) {
		q := in.Q
		if len(q) < 2 {
			q = ""
		}
		rows, total, err := deps.Cubes.ListPublic(ctx, q, in.Limit, in.Offset)
		if err != nil {
			return nil, err
		}
		out := &listCubesOutput{}
		out.Body.Total = total
		out.Body.Cubes = make([]CubeSummary, len(rows))
		for i, r := range rows {
			out.Body.Cubes[i] = CubeSummary{
				ID: r.ID, Name: r.Name, Description: r.Description,
				OwnerName: r.OwnerName, CardCount: r.CardCount,
				Visibility: r.Visibility, UpdatedAt: r.UpdatedAt,
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listMyCubes",
		Method:      http.MethodGet,
		Path:        "/api/me/cubes",
		Summary:     "List own cubes (both visibilities)",
		Tags:        []string{"cubes"},
	}, func(ctx context.Context, _ *struct{}) (*listCubesOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		rows, err := deps.Cubes.ListMine(ctx, uid)
		if err != nil {
			return nil, err
		}
		out := &listCubesOutput{}
		out.Body.Total = int64(len(rows))
		out.Body.Cubes = make([]CubeSummary, len(rows))
		for i, r := range rows {
			out.Body.Cubes[i] = CubeSummary{
				ID: r.ID, Name: r.Name, Description: r.Description,
				OwnerName: r.OwnerName, CardCount: r.CardCount,
				Visibility: r.Visibility, UpdatedAt: r.UpdatedAt,
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getCube",
		Method:      http.MethodGet,
		Path:        "/api/cubes/{cubeId}",
		Summary:     "Cube metadata",
		Tags:        []string{"cubes"},
	}, func(ctx context.Context, in *getCubeInput) (*cubeDetailOutput, error) {
		id, err := parseCubeID(in.CubeID)
		if err != nil {
			return nil, err
		}
		row, err := deps.Cubes.Get(ctx, id, viewerID(ctx))
		if err != nil {
			return nil, mapCubeErr(err)
		}
		return &cubeDetailOutput{Body: cubeDetailFrom(row)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateCube",
		Method:      http.MethodPatch,
		Path:        "/api/cubes/{cubeId}",
		Summary:     "Update cube metadata",
		Tags:        []string{"cubes"},
	}, func(ctx context.Context, in *updateCubeInput) (*cubeDetailOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseCubeID(in.CubeID)
		if err != nil {
			return nil, err
		}
		row, err := deps.Cubes.Update(ctx, id, uid, cubes.UpdateParams{
			Name: in.Body.Name, Description: in.Body.Description, Visibility: in.Body.Visibility,
		})
		if err != nil {
			return nil, mapCubeErr(err)
		}
		return &cubeDetailOutput{Body: cubeDetailFrom(row)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "deleteCube",
		Method:        http.MethodDelete,
		Path:          "/api/cubes/{cubeId}",
		Summary:       "Delete a cube",
		Tags:          []string{"cubes"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *getCubeInput) (*struct{}, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseCubeID(in.CubeID)
		if err != nil {
			return nil, err
		}
		if err := deps.Cubes.Delete(ctx, id, uid); err != nil {
			return nil, mapCubeErr(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getCubeCards",
		Method:      http.MethodGet,
		Path:        "/api/cubes/{cubeId}/cards",
		Summary:     "Cube card list, current or at a historical version",
		Tags:        []string{"cubes"},
	}, func(ctx context.Context, in *getCubeCardsInput) (*getCubeCardsOutput, error) {
		id, err := parseCubeID(in.CubeID)
		if err != nil {
			return nil, err
		}
		entries, version, err := deps.Cubes.GetCards(ctx, id, viewerID(ctx), in.AtVersion)
		if err != nil {
			return nil, mapCubeErr(err)
		}
		out := &getCubeCardsOutput{}
		out.Body.Version = version
		out.Body.Cards = make([]CubeCardEntry, len(entries))
		for i, e := range entries {
			out.Body.Cards[i] = cardEntryFrom(e)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listCubeChanges",
		Method:      http.MethodGet,
		Path:        "/api/cubes/{cubeId}/changes",
		Summary:     "Changelog, newest first",
		Tags:        []string{"cubes"},
	}, func(ctx context.Context, in *listCubeChangesInput) (*listCubeChangesOutput, error) {
		id, err := parseCubeID(in.CubeID)
		if err != nil {
			return nil, err
		}
		entries, total, err := deps.Cubes.ListChanges(ctx, id, viewerID(ctx), in.Limit, in.Offset)
		if err != nil {
			return nil, mapCubeErr(err)
		}
		out := &listCubeChangesOutput{}
		out.Body.Total = total
		out.Body.Changes = make([]ChangelogEntry, len(entries))
		for i, e := range entries {
			out.Body.Changes[i] = changelogEntryFrom(e)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "commitCubeChange",
		Method:        http.MethodPost,
		Path:          "/api/cubes/{cubeId}/changes",
		Summary:       "Commit a batched diff as one changelog entry",
		Tags:          []string{"cubes"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *commitCubeChangeInput) (*commitCubeChangeOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseCubeID(in.CubeID)
		if err != nil {
			return nil, err
		}
		adds := make([]cubes.AddInput, len(in.Body.Adds))
		for i, a := range in.Body.Adds {
			adds[i] = cubes.AddInput{ScryfallID: a.ScryfallID, Quantity: a.Quantity}
		}
		removes := make([]cubes.RemoveInput, len(in.Body.Removes))
		for i, r := range in.Body.Removes {
			removes[i] = cubes.RemoveInput{OracleID: r.OracleID, Quantity: r.Quantity}
		}
		entry, version, err := deps.Cubes.ApplyChange(ctx, id, uid, in.Body.ExpectedVersion, in.Body.Note, adds, removes)
		if err != nil {
			return nil, mapCubeErr(err)
		}
		out := &commitCubeChangeOutput{}
		out.Body.Change = changelogEntryFrom(entry)
		out.Body.Version = version
		return out, nil
	})
}
