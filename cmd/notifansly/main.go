package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NotiFansly/notifansly-bot/internal/bot"
	"github.com/NotiFansly/notifansly-bot/internal/config"
	"github.com/NotiFansly/notifansly-bot/internal/database"
	"github.com/NotiFansly/notifansly-bot/internal/health"
)

const version = "v0.1.2"

func main() {
	config.Load()

	log.Printf("Welcome to notifansly, version: %s", version)

	err := database.Init(config.DatabaseType, config.GetDatabaseConnectionString())
	if err != nil {
		log.Fatalf("Error initializing database: %v", err)
	}
	defer database.Close()

	repo := database.NewRepository()

	fanslyAggregator := health.NewAggregator(repo, "fansly_api")
	fanslyAggregator.Start(30 * time.Second)

	bot, err := bot.New(fanslyAggregator)
	if err != nil {
		log.Fatalf("Error creating bot: %v", err)
	}

	err = bot.Start()
	if err != nil {
		log.Fatalf("Error starting bot: %v", err)
	}

	// Wait for a SIGINT or SIGTERM signal
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	<-sc

	bot.Stop()
}
