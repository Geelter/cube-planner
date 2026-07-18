package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/cards"
	"github.com/mjabloniec/cube-planner/backend/internal/collections"
	"github.com/mjabloniec/cube-planner/backend/internal/cubes"
	dbgen "github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/events"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/config"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/mail"
	"github.com/mjabloniec/cube-planner/backend/internal/tournaments"
)

// Injected at build time via -ldflags (see backend/Dockerfile and Makefile).
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

type options struct{}

func main() {
	slog.Info("cube-planner api", "version", version, "commit", commit, "buildTime", buildTime)
	cli := humacli.New(func(hooks humacli.Hooks, _ *options) {
		cfg := config.Load()
		// Shared shutdown state: OnStop (SIGINT/SIGTERM via humacli) drains
		// the server, stops the background loops, and closes the pool.
		// humacli runs OnStart in a goroutine, so a signal landing before it
		// finishes races OnStop's reads under the Go memory model; atomic.
		// Pointer makes the handoff race-clean without a mutex.
		var srvPtr atomic.Pointer[http.Server]
		var poolPtr atomic.Pointer[pgxpool.Pool]
		ctx, stopBackground := context.WithCancel(context.Background())
		hooks.OnStart(func() {
			// A half-configured payment system must not start: it would
			// charge cards it can never confirm (or mount a dead webhook).
			if err := cfg.ValidateStripe(); err != nil {
				log.Fatalf("stripe config: %v", err)
			}
			pool, err := db.Connect(ctx, cfg.DatabaseURL)
			if err != nil {
				log.Fatalf("database: %v", err)
			}
			poolPtr.Store(pool)
			queries := dbgen.New(pool)
			oauthProviders := map[string]*auth.ProviderConfig{}
			if cfg.Discord.ClientID != "" {
				oauthProviders["discord"] = auth.DiscordProvider(cfg.Discord, cfg.BaseURL+"/auth/oauth/discord/callback")
			}
			if cfg.Google.ClientID != "" {
				oauthProviders["google"] = auth.GoogleProvider(cfg.Google, cfg.BaseURL+"/auth/oauth/google/callback")
			}
			sessions := auth.NewSessions(queries, cfg.Secure())
			if cfg.CardsSyncEnabled {
				syncer := cards.NewSyncer(pool,
					cards.NewScryfallClient(cfg.ScryfallBaseURL, "cube-planner/"+version),
					slog.Default())
				go syncer.RunScheduler(ctx, cards.DefaultSyncCheckInterval)
			}
			mailer := mail.FromConfig(cfg)
			eventsSvc := events.NewService(queries, pool,
				events.NewStripeClient(cfg.StripeSecretKey), mailer,
				cfg.BaseURL, slog.Default())
			go eventsSvc.RunSweeper(ctx, events.DefaultSweepInterval)
			deps := httpapi.Deps{
				Auth:                auth.NewService(queries, mailer, cfg.BaseURL),
				Sessions:            sessions,
				Queries:             queries,
				Cards:               cards.NewService(queries),
				Cubes:               cubes.NewService(queries, pool),
				Collections:         collections.NewService(queries, pool),
				OAuth:               auth.NewOAuth(queries, sessions, cfg.BaseURL, cfg.Secure(), oauthProviders).Routes(),
				Events:              eventsSvc,
				Tournaments:         tournaments.NewService(queries, pool),
				StripeWebhookSecret: cfg.StripeWebhookSecret,
			}
			_, handler := httpapi.Build(deps)
			log.Printf("listening on :%d", cfg.Port)
			srv := &http.Server{
				Addr:              fmt.Sprintf(":%d", cfg.Port),
				Handler:           handler,
				ReadHeaderTimeout: 10 * time.Second,
			}
			srvPtr.Store(srv)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatal(err)
			}
		})
		hooks.OnStop(func() {
			// Drain in-flight requests (webhooks especially) before pulling
			// the plug on the scheduler, sweeper, and DB pool.
			if srv := srvPtr.Load(); srv != nil {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := srv.Shutdown(shutdownCtx); err != nil {
					log.Printf("http shutdown: %v", err)
				}
			}
			stopBackground()
			if pool := poolPtr.Load(); pool != nil {
				pool.Close()
			}
			slog.Info("cube-planner api stopped")
		})
	})

	cli.Root().AddCommand(&cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI spec as YAML",
		Run: func(_ *cobra.Command, _ []string) {
			api, _ := httpapi.Build(httpapi.Deps{})
			b, err := api.OpenAPI().YAML()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(string(b))
		},
	})

	cli.Run()
}
