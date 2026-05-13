// Command readeck-bot is a Telegram bot that saves URLs to a Readeck instance.
// Multi-tenant: each Telegram user is mapped to a Readeck API token in
// config.toml. Unknown senders are silently ignored.
package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/dzarlax/readeck_toolkit/internal/bot"
)

func main() {
	cfgPath := flag.String("config", "config.toml", "path to TOML config")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := bot.Load(*cfgPath)
	if err != nil {
		log.Error("load config", "err", err)
		os.Exit(1)
	}
	b, err := bot.New(cfg, log)
	if err != nil {
		log.Error("init bot", "err", err)
		os.Exit(1)
	}
	b.Start()
}
