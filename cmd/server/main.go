package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FinancyOrg/backend/internal/auth"
	"github.com/FinancyOrg/backend/internal/crdballowlist"
	"github.com/FinancyOrg/backend/internal/httpapi"
	"github.com/FinancyOrg/backend/internal/ledger"
	"github.com/FinancyOrg/backend/internal/mongo"
	"github.com/FinancyOrg/backend/internal/store"
)

func main() {
	databaseURL := getenv("DATABASE_URL", "postgresql://root@localhost:26257/defaultdb?sslmode=disable")
	addr := getenv("ADDR", ":8081")

	// Auth is mandatory: fail fast rather than serve an unprotected API.
	sessionSecret := os.Getenv("SESSION_SECRET")
	oauthClientID := os.Getenv("OAUTH_CLIENT_ID")
	if oauthClientID == "" {
		log.Fatal("OAUTH_CLIENT_ID is required")
	}

	allowlist, err := auth.LoadAllowlist(os.Getenv)
	if err != nil {
		log.Fatalf("allowlist: %v", err)
	}
	sessions, err := auth.NewSessionManager(sessionSecret, 12*time.Hour)
	if err != nil {
		log.Fatalf("session: %v", err)
	}
	verifier := auth.NewGoogleVerifier(oauthClientID)

	ctx := context.Background()
	startupCtx, startupCancel := context.WithTimeout(ctx, 100*time.Second)
	defer startupCancel()
	if err := crdballowlist.Run(startupCtx, os.Getenv); err != nil {
		log.Fatalf("crdb allowlist: %v", err)
	}

	pool, err := store.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	if err := store.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	fsCtx, fsCancel := context.WithTimeout(ctx, 15*time.Second)
	fs, err := mongo.Connect(fsCtx, os.Getenv("MONGO_URI"))
	fsCancel()
	if err != nil {
		log.Fatalf("mongo: %v", err)
	}
	defer fs.Close(ctx)

	var fsPing httpapi.Firestore
	if fs != nil {
		fsPing = fs
	}

	svc := ledger.New(pool, ledger.FetchEcbRatePerEur)
	if fs != nil {
		svc.WithEcbCache(fs)
		svc.WithCache(fs)
		svc.WithAccountFlags(fs)
		svc.WithSettings(fs)
		_ = svc.LedgerLocation(ctx)
	}
	handler := (&httpapi.Server{
		Ledger: svc,
		Auth: &httpapi.Auth{
			Verifier:  verifier,
			Sessions:  sessions,
			Allowlist: allowlist,
			DevLogin:  auth.DevLoginEnabled(os.Getenv),
		},
		Firestore: fsPing,
	}).Handler()

	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("financy backend listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
