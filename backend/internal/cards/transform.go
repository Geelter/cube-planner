package cards

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// scryfallCard is the subset of a Scryfall bulk-data card object we read.
// https://scryfall.com/docs/api/cards
type scryfallImageURIs struct {
	Small  string `json:"small"`
	Normal string `json:"normal"`
}

type scryfallFace struct {
	OracleID   string             `json:"oracle_id"`
	ManaCost   string             `json:"mana_cost"`
	OracleText string             `json:"oracle_text"`
	Colors     []string           `json:"colors"`
	ImageURIs  *scryfallImageURIs `json:"image_uris"`
}

type scryfallCard struct {
	ID              string             `json:"id"`
	OracleID        string             `json:"oracle_id"`
	Name            string             `json:"name"`
	ReleasedAt      string             `json:"released_at"`
	Set             string             `json:"set"`
	SetName         string             `json:"set_name"`
	SetType         string             `json:"set_type"`
	CollectorNumber string             `json:"collector_number"`
	Rarity          string             `json:"rarity"`
	Layout          string             `json:"layout"`
	ManaCost        string             `json:"mana_cost"`
	CMC             float64            `json:"cmc"`
	TypeLine        string             `json:"type_line"`
	OracleText      string             `json:"oracle_text"`
	Colors          []string           `json:"colors"`
	ColorIdentity   []string           `json:"color_identity"`
	Promo           bool               `json:"promo"`
	Oversized       bool               `json:"oversized"`
	Games           []string           `json:"games"`
	ImageURIs       *scryfallImageURIs `json:"image_uris"`
	CardFaces       []scryfallFace     `json:"card_faces"`
}

// Card is one printing ready for the cards_staging COPY.
type Card struct {
	ScryfallID      uuid.UUID
	OracleID        uuid.UUID
	Name            string
	NormalizedName  string
	ReleasedAt      time.Time
	SetCode         string
	SetName         string
	CollectorNumber string
	Rarity          string
	Layout          string
	ManaCost        string
	CMC             float64
	TypeLine        string
	OracleText      string
	Colors          []string
	ColorIdentity   []string
	Promo           bool
	ImageSmall      *string
	ImageNormal     *string
	BackImageSmall  *string
	BackImageNormal *string
}

var excludedLayouts = map[string]bool{
	"token":              true,
	"double_faced_token": true,
	"emblem":             true,
	"art_series":         true,
}

// transformCard maps one Scryfall card object to an import row. ok=false
// means the card is not paper-playable (or malformed) and must be skipped.
func transformCard(sc scryfallCard) (Card, bool) {
	if !slices.Contains(sc.Games, "paper") || excludedLayouts[sc.Layout] ||
		sc.Oversized || sc.SetType == "memorabilia" {
		return Card{}, false
	}
	// Reversible cards carry oracle_id per face instead of top-level.
	oracleRaw := sc.OracleID
	if oracleRaw == "" && len(sc.CardFaces) > 0 {
		oracleRaw = sc.CardFaces[0].OracleID
	}
	scryfallID, err := uuid.Parse(sc.ID)
	if err != nil {
		return Card{}, false
	}
	oracleID, err := uuid.Parse(oracleRaw)
	if err != nil {
		return Card{}, false
	}
	releasedAt, err := time.Parse("2006-01-02", sc.ReleasedAt)
	if err != nil {
		return Card{}, false
	}

	c := Card{
		ScryfallID:      scryfallID,
		OracleID:        oracleID,
		Name:            sc.Name,
		NormalizedName:  NormalizeName(sc.Name),
		ReleasedAt:      releasedAt,
		SetCode:         sc.Set,
		SetName:         sc.SetName,
		CollectorNumber: sc.CollectorNumber,
		Rarity:          sc.Rarity,
		Layout:          sc.Layout,
		ManaCost:        sc.ManaCost,
		CMC:             sc.CMC,
		TypeLine:        sc.TypeLine,
		OracleText:      sc.OracleText,
		Colors:          sc.Colors,
		ColorIdentity:   sc.ColorIdentity,
		Promo:           sc.Promo,
	}
	if sc.ImageURIs != nil {
		c.ImageSmall = nonEmpty(sc.ImageURIs.Small)
		c.ImageNormal = nonEmpty(sc.ImageURIs.Normal)
	}
	if len(sc.CardFaces) > 0 {
		front := sc.CardFaces[0]
		if c.ManaCost == "" {
			c.ManaCost = front.ManaCost
		}
		if c.OracleText == "" {
			texts := make([]string, 0, len(sc.CardFaces))
			for _, f := range sc.CardFaces {
				if f.OracleText != "" {
					texts = append(texts, f.OracleText)
				}
			}
			c.OracleText = strings.Join(texts, "\n//\n")
		}
		if c.Colors == nil {
			seen := map[string]bool{}
			for _, f := range sc.CardFaces {
				for _, col := range f.Colors {
					if !seen[col] {
						seen[col] = true
						c.Colors = append(c.Colors, col)
					}
				}
			}
		}
		if c.ImageSmall == nil && front.ImageURIs != nil {
			c.ImageSmall = nonEmpty(front.ImageURIs.Small)
			c.ImageNormal = nonEmpty(front.ImageURIs.Normal)
		}
		if len(sc.CardFaces) > 1 && sc.CardFaces[1].ImageURIs != nil {
			c.BackImageSmall = nonEmpty(sc.CardFaces[1].ImageURIs.Small)
			c.BackImageNormal = nonEmpty(sc.CardFaces[1].ImageURIs.Normal)
		}
	}
	// text[] columns are NOT NULL; pgx encodes nil slices as NULL.
	if c.Colors == nil {
		c.Colors = []string{}
	}
	if c.ColorIdentity == nil {
		c.ColorIdentity = []string{}
	}
	return c, true
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
