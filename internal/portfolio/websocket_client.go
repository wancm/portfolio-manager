package portfolio

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	reconnectDelay = 5 * time.Second
	dialTimeout    = 10 * time.Second
)

type BrokerWSClient struct {
	url    string
	user   string
	store  *Store
	logger *slog.Logger
}

func NewBrokerWSClient(url, user string, store *Store, logger *slog.Logger) *BrokerWSClient {
	return &BrokerWSClient{url: url, user: user, store: store, logger: logger}
}

// Connect starts the loop: connect, auth, read messages
func (c *BrokerWSClient) Connect(ctx context.Context) error {
	for {
		if err := c.connectAndServe(ctx); err != nil {
			c.logger.Error("broker ws connection ended", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reconnectDelay):
			// reconnect
		}
	}
}

func (c *BrokerWSClient) connectAndServe(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, c.url, &websocket.DialOptions{
		HTTPClient: &http.Client{Timeout: dialTimeout},
	})
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "client closing")

	c.logger.Info("connected to broker ws")

	// Send authentication message
	authMsg := map[string]string{
		"type":       "auth",
		"user_alias": c.user,
	}
	if err := wsjson.Write(ctx, conn, authMsg); err != nil {
		return err
	}

	// Read loop
	for {
		var envelope struct {
			Type string `json:"type"`
		}
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(msg, &envelope); err != nil {
			c.logger.Warn("unmarshal broker ws message", "err", err)
			continue
		}

		switch envelope.Type {
		case "account_update":
			var upd AccountUpdate
			if err := json.Unmarshal(msg, &upd); err != nil {
				c.logger.Warn("unmarshal account_update", "err", err)
				continue
			}
			if err := c.store.UpsertAccount(ctx, upd); err != nil {
				c.logger.Error("upsert account", "err", err)
			}
		case "position_update":
			var upd PositionUpdate
			if err := json.Unmarshal(msg, &upd); err != nil {
				c.logger.Warn("unmarshal position_update", "err", err)
				continue
			}
			if err := c.store.UpsertPosition(ctx, upd); err != nil {
				c.logger.Error("upsert position", "err", err)
			}
		// future order update handling
		default:
			c.logger.Debug("unhandled broker ws message", "type", envelope.Type)
		}
	}
}
