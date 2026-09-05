package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
)

type Database struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, cfg config.DatabaseConfig) (*Database, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host,
		cfg.Port,
		cfg.User,
		cfg.Password,
		cfg.Name,
		cfg.SSLMode,
	)

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	poolConfig.MaxConns = int32(cfg.MaxOpenConns)
	poolConfig.MinConns = int32(cfg.MaxIdleConns)
	poolConfig.MaxConnLifetime = cfg.ConnMaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Database{pool: pool}, nil
}

func (d *Database) Close() {
	d.pool.Close()
}

func (d *Database) Ping(ctx context.Context) error {
	return d.pool.Ping(ctx)
}

func (d *Database) Pool() *pgxpool.Pool {
	return d.pool
}

func (d *Database) Stats() *Stats {
	stats := d.pool.Stat()
	return &Stats{
		TotalConns:      stats.TotalConns(),
		IdleConns:       stats.IdleConns(),
		AcquiredConns:   stats.AcquiredConns(),
		MaxConns:        stats.MaxConns(),
		ConstructingConns: stats.ConstructingConns(),
		EmptyAcquireCount: stats.EmptyAcquireCount(),
		AcquireCount:    stats.AcquireCount(),
		AcquireDuration: stats.AcquireDuration(),
		CanceledAcquireCount: stats.CanceledAcquireCount(),
	}
}

type Stats struct {
	TotalConns        int32
	IdleConns         int32
	AcquiredConns     int32
	MaxConns          int32
	ConstructingConns int32
	EmptyAcquireCount int64
	AcquireCount      int64
	AcquireDuration   time.Duration
	CanceledAcquireCount int64
}
