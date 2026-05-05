package portfolio

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) UpsertAccount(ctx context.Context, acct AccountUpdate) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO account_state (user_alias, balance, equity)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (user_alias) DO UPDATE SET balance=EXCLUDED.balance, equity=EXCLUDED.equity, updated_at=NOW()`,
		acct.UserAlias, acct.Balance, acct.Equity)
	return err
}

func (s *Store) UpsertPosition(ctx context.Context, pos PositionUpdate) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO positions (user_alias, symbol, quantity, avg_price, updated_at)
		 VALUES ($1,$2,$3,$4,NOW())
		 ON CONFLICT (user_alias, symbol) DO UPDATE SET quantity=EXCLUDED.quantity, avg_price=EXCLUDED.avg_price, updated_at=NOW()`,
		pos.UserAlias, pos.Symbol, pos.Quantity, pos.AvgPrice)
	return err
}

func (s *Store) GetPortfolioState(ctx context.Context, userAlias, symbol string) (PortfolioStateResponse, error) {
	var resp PortfolioStateResponse
	resp.Type = "portfolio_state_response"
	resp.UserAlias = userAlias
	resp.Symbol = symbol

	// 获取账户余额
	err := s.pool.QueryRow(ctx, `SELECT balance FROM account_state WHERE user_alias=$1`, userAlias).Scan(&resp.Balance)
	if err != nil {
		return resp, fmt.Errorf("account query: %w", err)
	}

	// 获取持仓
	var qty, avg float64
	err = s.pool.QueryRow(ctx,
		`SELECT quantity, avg_price FROM positions WHERE user_alias=$1 AND symbol=$2`,
		userAlias, symbol).Scan(&qty, &avg)
	if err != nil {
		// 无持仓也视为正常，返回0
		if err != pgx.ErrNoRows {
			return resp, fmt.Errorf("position query: %w", err)
		}
		qty = 0
	}
	resp.Position = qty
	resp.AvgCost = avg

	// 获取最大持仓限制
	var maxShares float64
	err = s.pool.QueryRow(ctx,
		`SELECT per_symbol_max_shares FROM risk_config WHERE user_alias=$1`,
		userAlias).Scan(&maxShares)
	if err != nil {
		maxShares = 200 // 默认值
	}
	resp.MaxLimit = maxShares
	return resp, nil
}

func (s *Store) GetRiskConfig(ctx context.Context, userAlias string) (RiskConfig, error) {
	var cfg RiskConfig
	err := s.pool.QueryRow(ctx,
		`SELECT user_alias, per_symbol_max_shares, per_symbol_max_pct, total_max_exposure_pct, max_order_pct, loss_limit_period, loss_limit_threshold_pct, reset_rule, max_consecutive_losses, ban_duration_days
		 FROM risk_config WHERE user_alias=$1`, userAlias).Scan(
		&cfg.UserAlias, &cfg.PerSymbolMaxShares, &cfg.PerSymbolMaxPct, &cfg.TotalMaxExposurePct, &cfg.MaxOrderPct,
		&cfg.LossLimitPeriod, &cfg.LossLimitThresholdPct, &cfg.ResetRule, &cfg.MaxConsecutiveLosses, &cfg.BanDurationDays)
	if err != nil {
		return cfg, fmt.Errorf("risk_config query: %w", err)
	}
	return cfg, nil
}

func (s *Store) IsBanned(ctx context.Context, userAlias, symbol string) (bool, error) {
	var banned bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM blacklist WHERE user_alias=$1 AND symbol=$2 AND banned_until > NOW())`,
		userAlias, symbol).Scan(&banned)
	if err != nil {
		return false, fmt.Errorf("blacklist query: %w", err)
	}
	return banned, nil
}

func (s *Store) IsHalted(ctx context.Context, userAlias string) (bool, error) {
	var halted bool
	err := s.pool.QueryRow(ctx,
		`SELECT is_halted FROM risk_state WHERE user_alias=$1`,
		userAlias).Scan(&halted)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("risk_state query: %w", err)
	}
	return halted, nil
}

func (s *Store) GetTotalMarketValue(ctx context.Context, userAlias string) (float64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT quantity, avg_price FROM positions WHERE user_alias=$1`, userAlias)
	if err != nil {
		return 0, fmt.Errorf("positions query: %w", err)
	}
	defer rows.Close()

	var total float64
	for rows.Next() {
		var q, p float64
		if err := rows.Scan(&q, &p); err != nil {
			return 0, fmt.Errorf("positions scan: %w", err)
		}
		total += q * p
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("positions rows: %w", err)
	}
	return total, nil
}
