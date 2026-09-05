package main

import (
	"os"

	"github.com/joho/godotenv"
	"github.com/qwerty7415963/go_be_arbitrage/internal/app"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	application, err := app.New(cfg)
	if err != nil {
		panic(err)
	}

	if err := application.Run(); err != nil {
		os.Exit(1)
	}
}
