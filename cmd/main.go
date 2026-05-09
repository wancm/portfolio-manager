// cmd/portfolio-manager/main.go
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
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
		".env",
		"../.env",
		"../../.env",
		"../../../.env",
	}

	envLoaded := false
	for _, p := range envCandidates {
		if err := godotenv.Load(p); err == nil {
			envLoaded = true
			break
		}
	}

	loggerWSURL := os.Getenv("LOGGER_WS_URL")
	if loggerWSURL == "" {
		loggerWSURL = "ws://127.0.0.1:6000"
	}
	wsLogger, logFwd := shared.NewLoggerWithWS(os.Getenv("LOG_FORMAT"), loggerWSURL, "portfolio-manager")
	shared.AppLogger = wsLogger

	if envLoaded {
		shared.AppLogger.Info("loaded env file")
	} else {
		shared.AppLogger.Info(".env not found in any candidate path, using system environment variables")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go logFwd.Run(ctx)

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

	portfolioMux := http.NewServeMux()
	portfolioMux.Handle("/ws", portfolio.NewPortfolioHandler(store, shared.AppLogger))

	riskMux := http.NewServeMux()
	riskMux.Handle("/ws", portfolio.NewRiskHandler(risk, shared.AppLogger))

	// 启动 Broker WebSocket 客户端
	brokerURL := os.Getenv("BROKER_WS_URL")
	if brokerURL == "" {
		brokerURL = "ws://localhost:7003/ws"
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

	hbSrv        := startHeartbeat(":9001", shared.AppLogger)
	portfolioSrv := &http.Server{Addr: ":7000", Handler: portfolioMux}
	riskSrv      := &http.Server{Addr: ":7001", Handler: riskMux}

	go func() {
		shared.AppLogger.Info("portfolio-manager portfolio handler listening on :7000")
		if err := portfolioSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			shared.AppLogger.Error("portfolio server failed", "err", err)
		}
	}()
	go func() {
		shared.AppLogger.Info("portfolio-manager risk handler listening on :7001")
		if err := riskSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			shared.AppLogger.Error("risk server failed", "err", err)
		}
	}()

	<-ctx.Done()
	shared.AppLogger.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = hbSrv.Shutdown(shutdownCtx)
	_ = portfolioSrv.Shutdown(shutdownCtx)
	_ = riskSrv.Shutdown(shutdownCtx)
}

func startHeartbeat(addr string, logger interface{ Info(string, ...any); Error(string, ...any) }) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case t := <-ticker.C:
				if err := wsjson.Write(r.Context(), conn, map[string]int64{"ts": t.UnixMilli()}); err != nil {
					return
				}
			}
		}
	})
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		logger.Info("heartbeat listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("heartbeat server error", "err", err)
		}
	}()
	return srv
}
