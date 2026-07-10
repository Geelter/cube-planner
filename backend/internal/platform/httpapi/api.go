// Package httpapi wires all HTTP routes: huma-managed /api operations and
// plain chi routes for browser redirect flows.
package httpapi

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

// Deps carries everything handlers need. Build(Deps{}) must stay safe for
// OpenAPI generation (no I/O at registration time).
type Deps struct {
	Auth     *auth.Service
	Sessions *auth.Sessions
	Queries  *db.Queries
	OAuth    http.Handler
}

func Build(deps Deps) (huma.API, http.Handler) {
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("Cube Planner API", "0.1.0"))
	if deps.Sessions != nil {
		api.UseMiddleware(sessionMiddleware(deps.Sessions))
	}
	registerHealth(api)
	registerAuth(api, deps)
	registerSession(api, deps)
	registerPasswordReset(api, deps)
	if deps.OAuth != nil {
		router.Mount("/auth/oauth", deps.OAuth)
	}
	return api, router
}
