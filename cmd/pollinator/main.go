package main

import (
	"log/slog"
	"os"

	"github.com/alphasecio/pollinator/internal"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := internal.LoadConfig()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	app, err := internal.NewApp(cfg, logger)
	if err != nil {
		logger.Error("failed to create app", "error", err)
		os.Exit(1)
	}

	adminURL := app.AdminURL()
	if cfg.BaseURL != "" {
		adminURL = cfg.BaseURL + adminURL
	} else {
		adminURL += " (domain not yet known — resolves from the first request to /display)"
	}

	logger.Info(
		"starting server",
		"port", cfg.Port,
		"admin", adminURL,
	)

	if err := app.Run(); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
