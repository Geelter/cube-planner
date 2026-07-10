package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/spf13/cobra"

	"github.com/mjabloniec/cube-planner/backend/internal/platform/config"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
)

type options struct{}

func main() {
	cli := humacli.New(func(hooks humacli.Hooks, _ *options) {
		cfg := config.Load()
		_, handler := httpapi.Build(httpapi.Deps{})
		hooks.OnStart(func() {
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
