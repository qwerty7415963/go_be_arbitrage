package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/qwerty7415963/go_be_arbitrage/internal/app"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/database"

	_ "github.com/qwerty7415963/go_be_arbitrage/docs"
)

// @title           Arbitrage Platform API
// @version         1.0
// @description     Backend API for arbitrage trading platform

// @contact.name   API Support
// @contact.email  support@arbitrage.com

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      localhost:8080
// @BasePath  /

// @schemes  http https

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token
func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	// Subcommand: migrate (see internal/database/migrate.go for usage)
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		msg, err := database.RunMigrate(cfg.PostgresURL(), os.Args[2:])
		if msg != "" {
			fmt.Println(msg)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "migrate:", err)
			os.Exit(1)
		}
		return
	}

	application, err := app.New(cfg)
	if err != nil {
		panic(err)
	}

	if err := application.Run(); err != nil {
		os.Exit(1)
	}
}
