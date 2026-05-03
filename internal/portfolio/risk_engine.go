package portfolio

import (
	"context"
	"fmt"
)

type RiskEngine struct {
	store *Store
}

func NewRiskEngine(store *Store) *RiskEngine {
	return &RiskEngine{store: store}
}

func (r *RiskEngine) ValidateOrder(ctx context.Context, userAlias, symbol, action string, quantity, price float64) (bool, string, float64) {
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
	if action == "SELL" && state.Position <= 0 {
		return false, "short selling not allowed", 0
	}

	// 2. 检查黑名单
	var banned bool
	err = r.store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM blacklist WHERE user_alias=$1 AND symbol=$2 AND banned_until > NOW())`, userAlias, symbol).Scan(&banned)
	if err == nil && banned {
		return false, "symbol is blacklisted", 0
	}

	// 3. 检查熔断
	var halted bool
	err = r.store.pool.QueryRow(ctx, `SELECT is_halted FROM risk_state WHERE user_alias=$1`, userAlias).Scan(&halted)
	if err == nil && halted && action == "BUY" {
		return false, "trading halted due to loss limit", 0
	}

	// 4. 计算订单金额与总仓位
	orderNotional := quantity * price
	maxOrderNotional := state.Balance * cfg.MaxOrderPct // 简化：按可用现金比例
	if orderNotional > maxOrderNotional {
		adjQty := maxOrderNotional / price
		return false, fmt.Sprintf("order value exceeds limit, max qty ~%.2f", adjQty), adjQty
	}

	if action == "BUY" {
		// 5. 单标的上限
		newPosition := state.Position + quantity
		if newPosition > cfg.PerSymbolMaxShares {
			adjQty := cfg.PerSymbolMaxShares - state.Position
			if adjQty <= 0 {
				return false, "already at max position limit", 0
			}
			return false, fmt.Sprintf("exceeds symbol max shares, max additional %.2f", adjQty), adjQty
		}

		// 6. 总仓位上限
		// 获取所有持仓市值
		var totalMarketValue float64
		rows, err := r.store.pool.Query(ctx, `SELECT quantity, avg_price FROM positions WHERE user_alias=$1`, userAlias)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var q, p float64
				if err := rows.Scan(&q, &p); err == nil {
					totalMarketValue += q * p
				}
			}
		}
		newTotalValue := totalMarketValue + orderNotional
		maxTotalValue := state.Balance * cfg.TotalMaxExposurePct // 这里简化，用余额代替净值
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
	if action == "SELL" && quantity > state.Position {
		return false, fmt.Sprintf("sell quantity exceeds holding (%.2f)", state.Position), state.Position
	}

	return true, "OK", quantity
}
