package main

import (
	"fmt"
	"os"

	"context"
	"time"

	"github.com/joho/godotenv"
	"github.com/qwerty7415963/go_be_arbitrage/internal/app"
	"github.com/qwerty7415963/go_be_arbitrage/internal/auth"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/database"
	"github.com/qwerty7415963/go_be_arbitrage/internal/i18n"

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

	if err := i18n.Load(); err != nil {
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

	// Subcommand: admin reset-password
	if len(os.Args) > 2 && os.Args[1] == "admin" && os.Args[2] == "reset-password" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		db, err := database.New(ctx, cfg.Database)
		if err != nil {
			fmt.Fprintln(os.Stderr, "db:", err)
			os.Exit(1)
		}
		defer db.Close()
		authRepo := auth.NewRepository(db.Pool())
		authSvc := auth.NewService(&cfg.Auth, authRepo)
		if err := authSvc.ResetAdminPassword(ctx, cfg.Auth.AdminEmail, cfg.Auth.AdminPassword); err != nil {
			fmt.Fprintln(os.Stderr, "reset-password:", err)
			os.Exit(1)
		}
		fmt.Println("admin password reset")
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
