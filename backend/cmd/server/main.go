package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/spf13/cobra"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	dbgen "github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/config"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/mail"
)

type options struct{}

func main() {
	cli := humacli.New(func(hooks humacli.Hooks, _ *options) {
		cfg := config.Load()
		hooks.OnStart(func() {
			ctx := context.Background()
			pool, err := db.Connect(ctx, cfg.DatabaseURL)
			if err != nil {
				log.Fatalf("database: %v", err)
			}
			queries := dbgen.New(pool)
			deps := httpapi.Deps{
				Auth:     auth.NewService(queries, mail.FromConfig(cfg), cfg.BaseURL),
				Sessions: auth.NewSessions(queries, cfg.Secure()),
				Queries:  queries,
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
