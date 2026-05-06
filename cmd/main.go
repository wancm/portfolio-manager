// cmd/portfolio-manager/main.go
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/wancm/portfolio-manager/internal/portfolio"
	"github.com/wancm/portfolio-manager/internal/shared"
)

func main() {
	// Load the first .env we can find. godotenv.Load(p1, p2, ...) returns the
	// first error it hits, which trips even when an earlier path loaded
	// successfully — so try each path independently and stop on the first hit.
	envCandidates := []string{
		"configs/.env",
		"../configs/.env",
		"../../configs/.env",
		"../../../configs/.env",
	}

	envLoaded := false
	for _, p := range envCandidates {
		if err := godotenv.Load(p); err == nil {
			shared.AppLogger.Info("loaded env file", "path", p)
			envLoaded = true
			break
		}
	}
	if !envLoaded {
		shared.AppLogger.Info(".env not found in any candidate path, using system environment variables")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dbConn := os.Getenv("DB_CONN")
	if dbConn == "" {
		shared.AppLogger.Error("DB_CONN environment variable is required")
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, dbConn)
	if err != nil {
		shared.AppLogger.Error("database connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	store := portfolio.NewStore(pool)
	risk := portfolio.NewRiskEngine(store)

	handler := portfolio.NewTraderWSHandler(store, risk, shared.AppLogger)
	http.Handle("/ws", handler)

	// 启动 Broker WebSocket 客户端
	brokerURL := os.Getenv("BROKER_WS_URL")
	if brokerURL == "" {
		brokerURL = "ws://localhost:8085/ws"
	}
	userAlias := os.Getenv("USER_ALIAS")
	if userAlias == "" {
		userAlias = "wancm"
	}
	brokerClient := portfolio.NewBrokerWSClient(brokerURL, userAlias, store, shared.AppLogger)
	go func() {
		if err := brokerClient.Connect(ctx); err != nil {
			shared.AppLogger.Error("broker client failed", "err", err)
		}
	}()

	// 启动 Trader WebSocket 服务
	srv := &http.Server{Addr: ":8081"}
	go func() {
		shared.AppLogger.Info("Portfolio Manager listening on :8081")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			shared.AppLogger.Error("http server failed", "err", err)
		}
	}()

	<-ctx.Done()
	shared.AppLogger.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		shared.AppLogger.Error("graceful shutdown failed", "err", err)
	}
}
