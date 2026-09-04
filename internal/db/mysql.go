package db

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

func Connect(ctx context.Context, dsn string, workers int) (*sql.DB, error) {
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	maxConns := poolSize(workers)
	conn.SetMaxOpenConns(maxConns)
	conn.SetMaxIdleConns(maxConns)

	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return conn, nil
}

func poolSize(workers int) int {
	if workers < 0 {
		workers = 0
	}
	return workers + 2
}
