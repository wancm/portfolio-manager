// cmd/portfolio-manager/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/wancm/portfolio-manager/internal/portfolio"
	"github.com/wancm/portfolio-manager/internal/shared"
)

func main() {

	wd, _ := os.Getwd()
	fmt.Println("current dir:", wd)

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

	ctx := context.Background()

	dbConn := os.Getenv("DB_CONN")

	if dbConn == "" {
		log.Fatal("DB_CONN environment variable is required")
	}

	pool, err := pgxpool.New(ctx, dbConn)
	if err != nil {
		log.Fatalf("database connect: %v", err)
	}
	defer pool.Close()

	store := portfolio.NewStore(pool)
	risk := portfolio.NewRiskEngine(store)

	// // 启动 Broker WebSocket 客户端
	// brokerURL := os.Getenv("BROKER_WS_URL")
	// if brokerURL == "" {
	// 	brokerURL = "ws://localhost:8085/ws"
	// }
	// brokerClient := portfolio.NewBrokerWSClient(brokerURL, "wancm", store, shared.AppLogger)
	// go func() {
	// 	if err := brokerClient.Connect(ctx); err != nil {
	// 		shared.AppLogger.Error("broker client failed", "err", err)
	// 	}
	// }()

	// 启动 Trader WebSocket 服务
	handler := portfolio.NewTraderWSHandler(store, risk, shared.AppLogger)
	http.Handle("/ws", handler)
	go func() {
		shared.AppLogger.Info("Portfolio Manager listening on :8081")
		if err := http.ListenAndServe(":8081", nil); err != nil {
			shared.AppLogger.Error("http server failed", "err", err)
		}
	}()

	// 等待退出信号
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	shared.AppLogger.Info("shutting down")
}
