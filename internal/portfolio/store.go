package portfolio

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultPerSymbolMaxShares = 200.0

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
	if err != nil {
		return fmt.Errorf("upsert account %s: %w", acct.UserAlias, err)
	}
	return nil
}

func (s *Store) UpsertPosition(ctx context.Context, pos PositionUpdate) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO positions (user_alias, symbol, quantity, avg_price, updated_at)
		 VALUES ($1,$2,$3,$4,NOW())
		 ON CONFLICT (user_alias, symbol) DO UPDATE SET quantity=EXCLUDED.quantity, avg_price=EXCLUDED.avg_price, updated_at=NOW()`,
		pos.UserAlias, pos.Symbol, pos.Quantity, pos.AvgPrice)
	if err != nil {
		return fmt.Errorf("upsert position %s/%s: %w", pos.UserAlias, pos.Symbol, err)
	}
	return nil
}

func (s *Store) GetPortfolioState(ctx context.Context, userAlias, symbol string) (PortfolioStateResponse, error) {
	resp := PortfolioStateResponse{
		Type:      MsgPortfolioState,
		UserAlias: userAlias,
		Symbol:    symbol,
	}
	err := s.pool.QueryRow(ctx, `
		SELECT a.balance,
		       COALESCE(p.quantity, 0), COALESCE(p.avg_price, 0),
		       COALESCE(r.per_symbol_max_shares, $3)
		FROM account_state a
		LEFT JOIN positions p ON p.user_alias = a.user_alias AND p.symbol = $2
		LEFT JOIN risk_config r ON r.user_alias = a.user_alias
		WHERE a.user_alias = $1`,
		userAlias, symbol, defaultPerSymbolMaxShares,
	).Scan(&resp.Balance, &resp.Position, &resp.AvgCost, &resp.MaxLimit)
	if err != nil {
		return resp, fmt.Errorf("portfolio state query: %w", err)
	}
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
