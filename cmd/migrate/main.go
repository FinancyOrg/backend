package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/FinancyOrg/backend/internal/store"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgresql://root@localhost:26257/defaultdb?sslmode=disable"
	}
	pool, err := store.Connect(ctx, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := store.RunMigrations(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM commodities`).Scan(&n); err != nil {
		fmt.Fprintf(os.Stderr, "commodities: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("migrations ok; commodities=%d\n", n)
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "versions: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		fmt.Println("applied:", v)
	}
}
