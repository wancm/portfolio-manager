package portfolio

import (
	"context"
	"fmt"
)

const (
	actionBuy  = "BUY"
	actionSell = "SELL"
)

type portfolioQuerier interface {
	GetRiskConfig(ctx context.Context, userAlias string) (RiskConfig, error)
	GetPortfolioState(ctx context.Context, userAlias, symbol string) (PortfolioStateResponse, error)
	IsBanned(ctx context.Context, userAlias, symbol string) (bool, error)
	IsHalted(ctx context.Context, userAlias string) (bool, error)
	GetTotalMarketValue(ctx context.Context, userAlias string) (float64, error)
}

type RiskEngine struct {
	store portfolioQuerier
}

func NewRiskEngine(store *Store) *RiskEngine {
	return &RiskEngine{store: store}
}

func (r *RiskEngine) ValidateOrder(ctx context.Context, userAlias, symbol, action string,
	quantity, price float64) (allowed bool, reason string, adjustedQty float64) {
	cfg, err := r.store.GetRiskConfig(ctx, userAlias)
	if err != nil {
		return false, fmt.Sprintf("config error: %v", err), 0
	}

	// 获取持仓与账户
	state, err := r.store.GetPortfolioState(ctx, userAlias, symbol)
	if err != nil {
		return false, fmt.Sprintf("state error: %v", err), 0
	}

	// 1. 禁止卖空
	if action == actionSell && state.Position <= 0 {
		return false, "short selling not allowed", 0
	}

	// 2. 检查黑名单
	banned, err := r.store.IsBanned(ctx, userAlias, symbol)
	if err != nil {
		return false, fmt.Sprintf("blacklist error: %v", err), 0
	}
	if banned {
		return false, "symbol is blacklisted", 0
	}

	// 3. 检查熔断 (仅阻止开仓)
	halted, err := r.store.IsHalted(ctx, userAlias)
	if err != nil {
		return false, fmt.Sprintf("halt check error: %v", err), 0
	}
	if halted && action == actionBuy {
		return false, "trading halted due to loss limit", 0
	}

	// 4. 计算订单金额与总仓位
	orderNotional := quantity * price
	maxOrderNotional := state.Balance * cfg.MaxOrderPct
	if orderNotional > maxOrderNotional {
		adjQty := maxOrderNotional / price
		return false, fmt.Sprintf("order value exceeds limit, max qty ~%.2f", adjQty), adjQty
	}

	if action == actionBuy {
		// 5. 单标的上限 — shares cap AND pct cap; lower binding cap holds
		newPosition := state.Position + quantity

		adjByShares := cfg.PerSymbolMaxShares - state.Position
		adjByPct := (cfg.PerSymbolMaxPct*state.Balance/price) - state.Position
		// pick the more restrictive cap
		adjMax := adjByShares
		if adjByPct < adjMax {
			adjMax = adjByPct
		}

		if newPosition*price/state.Balance > cfg.PerSymbolMaxPct || newPosition > cfg.PerSymbolMaxShares {
			if adjMax <= 0 {
				return false, "already at max position limit", 0
			}
			return false, fmt.Sprintf("exceeds symbol position limit, max additional %.2f", adjMax), adjMax
		}

		// 6. 总仓位上限
		totalMarketValue, err := r.store.GetTotalMarketValue(ctx, userAlias)
		if err != nil {
			return false, fmt.Sprintf("market value error: %v", err), 0
		}
		newTotalValue := totalMarketValue + orderNotional
		maxTotalValue := state.Balance * cfg.TotalMaxExposurePct
		if newTotalValue > maxTotalValue {
			remainingRoom := maxTotalValue - totalMarketValue
			adjQty := remainingRoom / price
			return false, fmt.Sprintf("total exposure limit reached, max additional %.2f", adjQty), adjQty
		}

		// 7. 可用现金
		if orderNotional > state.Balance {
			adjQty := state.Balance / price
			return false, fmt.Sprintf("insufficient cash, max buyable %.2f", adjQty), adjQty
		}
	}

	// 卖出时检查数量不超过持仓
	if action == actionSell && quantity > state.Position {
		return false, fmt.Sprintf("sell quantity exceeds holding (%.2f)", state.Position), state.Position
	}

	return true, "OK", quantity
}
