package portfolio

const (
	MsgGetPortfolioState = "get_portfolio_state"
	MsgValidateOrder     = "validate_order"
	MsgPortfolioState    = "portfolio_state_response"
	MsgValidationResp    = "validation_response"
)

// 更新自 Broker Manager
type AccountUpdate struct {
	UserAlias string  `json:"user_alias"`
	Balance   float64 `json:"balance"`
	Equity    float64 `json:"equity"`
}

type PositionUpdate struct {
	UserAlias string  `json:"user_alias"`
	Symbol    string  `json:"symbol"`
	Quantity  float64 `json:"quantity"`
	AvgPrice  float64 `json:"avg_price"`
}

// 风控配置
type RiskConfig struct {
	UserAlias             string  `json:"user_alias"`
	PerSymbolMaxShares    float64 `json:"per_symbol_max_shares"`
	PerSymbolMaxPct       float64 `json:"per_symbol_max_pct"`
	TotalMaxExposurePct   float64 `json:"total_max_exposure_pct"`
	MaxOrderPct           float64 `json:"max_order_pct"`
	LossLimitPeriod       string  `json:"loss_limit_period"`
	LossLimitThresholdPct float64 `json:"loss_limit_threshold_pct"`
	ResetRule             string  `json:"reset_rule"`
	MaxConsecutiveLosses  int     `json:"max_consecutive_losses"`
	BanDurationDays       int     `json:"ban_duration_days"`
}

// Trader Bot 请求
type PortfolioStateRequest struct {
	Type      string `json:"type"` // "get_portfolio_state"
	RequestID string `json:"request_id"`
	UserAlias string `json:"user_alias"`
	Symbol    string `json:"symbol"`
}

type ValidateOrderRequest struct {
	Type      string  `json:"type"` // "validate_order"
	RequestID string  `json:"request_id"`
	UserAlias string  `json:"user_alias"`
	Symbol    string  `json:"symbol"`
	Action    string  `json:"action"` // BUY / SELL
	Quantity  float64 `json:"quantity"`
	Price     float64 `json:"price"`
}

// Portfolio Manager 响应
type PortfolioStateResponse struct {
	Type      string  `json:"type"` // "portfolio_state_response"
	RequestID string  `json:"request_id"`
	UserAlias string  `json:"user_alias"`
	Symbol    string  `json:"symbol"`
	Position  float64 `json:"current_position"`
	AvgCost   float64 `json:"avg_cost"`
	MaxLimit  float64 `json:"max_limit"`
	Balance   float64 `json:"account_balance"`
}

type ValidationResponse struct {
	Type             string  `json:"type"` // "validation_response"
	RequestID        string  `json:"request_id"`
	Allowed          bool    `json:"allowed"`
	Reason           string  `json:"reason"`
	AdjustedQuantity float64 `json:"adjusted_quantity,omitempty"`
}
