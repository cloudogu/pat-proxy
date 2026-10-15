package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/urfave/cli"
)

func newApp() *cli.App {
	app := cli.NewApp()
	app.Name = "patproxy"
	app.Usage = "HTTP reverse proxy with CAS PAT validation"
	app.HideVersion = true
	app.Flags = createGlobalFlags()
	app.Action = func(c *cli.Context) error {
		logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("component", "pat-proxy")
		proxy := NewProxy(readCLIConfig(c), logger)
		return proxy.Run()
	}
	return app
}

func main() {
	if err := newApp().Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}