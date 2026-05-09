package portfolio

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// PortfolioHandler serves portfolio state queries on :7000.
type PortfolioHandler struct {
	store  *Store
	logger *slog.Logger
}

func NewPortfolioHandler(store *Store, logger *slog.Logger) *PortfolioHandler {
	return &PortfolioHandler{store: store, logger: logger}
}

func (h *PortfolioHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		h.logger.Error("websocket accept", "err", err)
		return
	}
	defer conn.Close(websocket.StatusInternalError, "unexpected exit")

	ctx := r.Context()
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			h.logger.Info("portfolio ws closed", "err", err)
			return
		}

		var base struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(msg, &base); err != nil {
			if err := writeWSError(ctx, conn, "", "invalid JSON: "+err.Error()); err != nil {
				return
			}
			continue
		}

		if base.Type != MsgGetPortfolioState {
			if err := writeWSError(ctx, conn, base.RequestID, "unknown request type: "+base.Type); err != nil {
				return
			}
			continue
		}

		var req PortfolioStateRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			if err := writeWSError(ctx, conn, base.RequestID, err.Error()); err != nil {
				return
			}
			continue
		}
		resp, err := h.store.GetPortfolioState(ctx, req.UserAlias, req.Symbol)
		if err != nil {
			h.logger.Error("get portfolio state", "err", err)
			if err := writeWSError(ctx, conn, req.RequestID, err.Error()); err != nil {
				return
			}
			continue
		}
		resp.RequestID = req.RequestID
		if err := wsjson.Write(ctx, conn, resp); err != nil {
			return
		}
	}
}

// RiskHandler serves order validation requests on :7001.
type RiskHandler struct {
	riskEngine *RiskEngine
	logger     *slog.Logger
}

func NewRiskHandler(risk *RiskEngine, logger *slog.Logger) *RiskHandler {
	return &RiskHandler{riskEngine: risk, logger: logger}
}

func (h *RiskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		h.logger.Error("websocket accept", "err", err)
		return
	}
	defer conn.Close(websocket.StatusInternalError, "unexpected exit")

	ctx := r.Context()
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			h.logger.Info("risk ws closed", "err", err)
			return
		}

		var base struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(msg, &base); err != nil {
			if err := writeWSError(ctx, conn, "", "invalid JSON: "+err.Error()); err != nil {
				return
			}
			continue
		}

		if base.Type != MsgValidateOrder {
			if err := writeWSError(ctx, conn, base.RequestID, "unknown request type: "+base.Type); err != nil {
				return
			}
			continue
		}

		var req ValidateOrderRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			if err := writeWSError(ctx, conn, base.RequestID, err.Error()); err != nil {
				return
			}
			continue
		}
		if req.Action != actionBuy && req.Action != actionSell {
			if err := writeWSError(ctx, conn, req.RequestID, "action must be BUY or SELL"); err != nil {
				return
			}
			continue
		}
		allowed, reason, adjQty := h.riskEngine.ValidateOrder(ctx, req.UserAlias, req.Symbol, req.Action, req.Quantity, req.Price)
		resp := ValidationResponse{
			Type:             MsgValidationResp,
			RequestID:        req.RequestID,
			Allowed:          allowed,
			Reason:           reason,
			AdjustedQuantity: adjQty,
		}
		if err := wsjson.Write(ctx, conn, resp); err != nil {
			return
		}
	}
}

func writeWSError(ctx context.Context, conn *websocket.Conn, requestID, message string) error {
	return wsjson.Write(ctx, conn, map[string]any{
		"type":       "error",
		"request_id": requestID,
		"error":      message,
	})
}
