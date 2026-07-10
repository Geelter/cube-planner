// Package httpapi wires all HTTP routes: huma-managed /api operations and
// plain chi routes for browser redirect flows.
package httpapi

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

// Deps carries everything handlers need. Fields are added by later tasks;
// Build(Deps{}) must always be safe for OpenAPI generation (no I/O at
// registration time).
type Deps struct{}

func Build(deps Deps) (huma.API, http.Handler) {
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("Cube Planner API", "0.1.0"))
	registerHealth(api)
	return api, router
}
