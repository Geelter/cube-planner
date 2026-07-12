package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/spf13/cobra"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/cards"
	dbgen "github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/config"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/mail"
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
		hooks.OnStart(func() {
			ctx := context.Background()
			pool, err := db.Connect(ctx, cfg.DatabaseURL)
			if err != nil {
				log.Fatalf("database: %v", err)
			}
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
			deps := httpapi.Deps{
				Auth:     auth.NewService(queries, mail.FromConfig(cfg), cfg.BaseURL),
				Sessions: sessions,
				Queries:  queries,
				OAuth:    auth.NewOAuth(queries, sessions, cfg.BaseURL, cfg.Secure(), oauthProviders).Routes(),
			}
			_, handler := httpapi.Build(deps)
			log.Printf("listening on :%d", cfg.Port)
			if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), handler); err != nil {
				log.Fatal(err)
			}
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
