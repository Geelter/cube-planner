// Package cards owns the Scryfall mirror: bulk import, daily sync, and
// local card search.
package cards

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// NormalizeName lowercases and strips combining diacritics. Stored in
// cards.normalized_name at import time and applied to queries at search
// time, so "Seance" matches "Séance". Done in Go instead of Postgres
// unaccent() because unaccent is not IMMUTABLE and can't back an index.
func NormalizeName(name string) string {
	// transform.Chain values carry internal buffers, so build per call
	// rather than sharing one across goroutines.
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	folded, _, err := transform.String(t, name)
	if err != nil {
		folded = name
	}
	return strings.ToLower(folded)
}

// escapeLike escapes LIKE/ILIKE metacharacters in user input so it matches
// literally (Postgres default escape character is backslash).
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
