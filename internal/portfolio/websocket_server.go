package portfolio

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type TraderWSHandler struct {
	store      *Store
	riskEngine *RiskEngine
	logger     *slog.Logger
}

func NewTraderWSHandler(store *Store, risk *RiskEngine, logger *slog.Logger) *TraderWSHandler {
	return &TraderWSHandler{store: store, riskEngine: risk, logger: logger}
}

func (h *TraderWSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:5173", "localhost:8081"},
	})
	if err != nil {
		h.logger.Error("websocket accept", "err", err)
		return
	}
	defer conn.Close(websocket.StatusInternalError, "unexpected exit")

	ctx := r.Context()
	for {
		// Read the frame once as raw bytes so we can unmarshal it twice:
		// once for the type-discriminator envelope, then again into the
		// concrete request struct.
		_, msg, err := conn.Read(ctx)
		if err != nil {
			h.logger.Info("read from trader ws", "err", err)
			return
		}

		var base struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
			UserAlias string `json:"user_alias"`
		}
		if err := json.Unmarshal(msg, &base); err != nil {
			h.logger.Warn("unmarshal envelope", "err", err)
			if err := writeError(ctx, conn, h.logger, "", "invalid JSON: "+err.Error()); err != nil {
				return
			}
			continue
		}

		switch base.Type {
		case "get_portfolio_state":
			var req PortfolioStateRequest
			if err := json.Unmarshal(msg, &req); err != nil {
				h.logger.Warn("unmarshal get_portfolio_state", "err", err)
				if err := writeError(ctx, conn, h.logger, base.RequestID, err.Error()); err != nil {
					return
				}
				continue
			}
			resp, err := h.store.GetPortfolioState(ctx, req.UserAlias, req.Symbol)
			if err != nil {
				h.logger.Error("get portfolio state", "err", err)
				if err := writeError(ctx, conn, h.logger, req.RequestID, err.Error()); err != nil {
					return
				}
				continue
			}
			resp.RequestID = req.RequestID
			if err := wsjson.Write(ctx, conn, resp); err != nil {
				h.logger.Error("write portfolio state response", "err", err)
				return
			}

		case "validate_order":
			var req ValidateOrderRequest
			if err := json.Unmarshal(msg, &req); err != nil {
				h.logger.Warn("unmarshal validate_order", "err", err)
				if err := writeError(ctx, conn, h.logger, base.RequestID, err.Error()); err != nil {
					return
				}
				continue
			}
			if req.Action != "BUY" && req.Action != "SELL" {
				if err := writeError(ctx, conn, h.logger, req.RequestID, "action must be BUY or SELL"); err != nil {
					return
				}
				continue
			}
			allowed, reason, adjQty := h.riskEngine.ValidateOrder(ctx, req.UserAlias, req.Symbol, req.Action, req.Quantity, req.Price)
			resp := ValidationResponse{
				Type:             "validation_response",
				RequestID:        req.RequestID,
				Allowed:          allowed,
				Reason:           reason,
				AdjustedQuantity: adjQty,
			}
			if err := wsjson.Write(ctx, conn, resp); err != nil {
				h.logger.Error("write validation response", "err", err)
				return
			}

		default:
			h.logger.Warn("unknown trader request", "type", base.Type)
			if err := writeError(ctx, conn, h.logger, base.RequestID, "unknown request type: "+base.Type); err != nil {
				return
			}
		}
	}
}

func writeError(ctx context.Context, conn *websocket.Conn, logger *slog.Logger, requestID, message string) error {
	resp := map[string]any{
		"type":       "error",
		"request_id": requestID,
		"error":      message,
	}
	if err := wsjson.Write(ctx, conn, resp); err != nil {
		logger.Error("write error response", "err", err)
		return err
	}
	return nil
}
