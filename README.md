## Portfolio-Manager — Functionality Documentation

### Overview

A multi-server risk management and portfolio state service. It operates three servers and one outbound client.

---

### Packages & Files

| File                                                                                                                                                                                  | Role                                                   |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| [cmd/main.go](vscode-webview://1aivhi7oljnrj0i3au5ck69s13t3laetfefv80otm5u1viq4sg57/applications/portfolio-manager/cmd/main.go)                                                       | Wires servers, DB pool, broker client, signal handling |
| [internal/portfolio/types.go](vscode-webview://1aivhi7oljnrj0i3au5ck69s13t3laetfefv80otm5u1viq4sg57/applications/portfolio-manager/internal/portfolio/types.go)                       | All message structs and type constants                 |
| [internal/portfolio/websocket_server.go](vscode-webview://1aivhi7oljnrj0i3au5ck69s13t3laetfefv80otm5u1viq4sg57/applications/portfolio-manager/internal/portfolio/websocket_server.go) | `PortfolioHandler` (:7000) and `RiskHandler` (:7001)   |
| [internal/portfolio/websocket_client.go](vscode-webview://1aivhi7oljnrj0i3au5ck69s13t3laetfefv80otm5u1viq4sg57/applications/portfolio-manager/internal/portfolio/websocket_client.go) | Outbound client to broker-manager (:7003)              |
| [internal/portfolio/risk_engine.go](vscode-webview://1aivhi7oljnrj0i3au5ck69s13t3laetfefv80otm5u1viq4sg57/applications/portfolio-manager/internal/portfolio/risk_engine.go)           | 8-rule order validation engine                         |
| [internal/portfolio/store.go](vscode-webview://1aivhi7oljnrj0i3au5ck69s13t3laetfefv80otm5u1viq4sg57/applications/portfolio-manager/internal/portfolio/store.go)                       | All DB access (pgx)                                    |
| [internal/shared/ws_logger.go](vscode-webview://1aivhi7oljnrj0i3au5ck69s13t3laetfefv80otm5u1viq4sg57/applications/portfolio-manager/internal/shared/ws_logger.go)                     | Dual-output logger (console + logger service WS)       |

---

### Functionality Details

#### 1. Portfolio State Query (:7000)

- Serves `get_portfolio_state` requests — joins `account_state`, `positions`, and `risk_config`
- Returns current position qty, avg cost, per-symbol limit, and cash balance for a given `(user_alias, symbol)` pair
- Stateless per request; connection stays open for repeated queries

#### 2. Risk Validation (:7001)

- Validates `validate_order` requests before execution
- Runs 8 sequential rules (see below); first failure short-circuits and returns an adjusted quantity
- Callers: `trader-bot` before submitting any order

#### 3. Broker Update Ingestion (client → :7003)

- Connects to broker-manager on startup, sends `auth`, then receives push updates
- `account_update` → upserts `account_state` (balance, equity)
- `position_update` → upserts `positions` (quantity, avg_price)
- Auto-reconnects every 5 seconds on failure

#### 4. Heartbeat (:9001)

- Sends `{ "ts": <unix_ms> }` every second to any connected WS client

#### 5. Log Forwarding (client → :6000)

- All `slog` records are dual-output: console AND async queue to logger service
- Queue: 512 entries; drops silently on overflow (non-blocking)
- Reconnects every 5 seconds on failure

---

### Risk Engine — 8 Rules (in order)

| #   | Rule                                                                                                                             | Applies To | Result if Violated               |
| --- | -------------------------------------------------------------------------------------------------------------------------------- | ---------- | -------------------------------- |
| 1   | No short selling (`position ≤ 0`)                                                                                                | SELL       | Reject                           |
| 2   | Symbol blacklist (`banned_until > NOW()`)                                                                                        | BUY + SELL | Reject                           |
| 3   | Trading halt (`risk_state.is_halted`)                                                                                            | BUY only   | Reject                           |
| 4   | Single-order notional cap (`qty × price > balance × max_order_pct`)                                                              | BUY + SELL | Reject + adjusted qty            |
| 5   | Per-symbol cap — both absolute (`per_symbol_max_shares`) and relative (`per_symbol_max_pct × balance`) enforced; binding = lower | BUY only   | Reject + adjusted qty            |
| 6   | Total portfolio exposure cap (`all_positions_value + order > balance × total_max_exposure_pct`)                                  | BUY only   | Reject + adjusted qty            |
| 7   | Cash availability (`order_notional > balance`)                                                                                   | BUY only   | Reject + adjusted qty            |
| 8   | Sell qty vs holdings (`qty > position`)                                                                                          | SELL only  | Reject + adjusted qty = position |

**Note:** Rules 5–9 of `risk_config` (`loss_limit_*`, `max_consecutive_losses`, `ban_duration_days`) are stored and readable but the **loss-accumulation logic that writes `is_halted` and `blacklist` is not yet implemented** — this service only reads those states.

---

### Database Table Usage

| Table            | Read                                   | Write          | By                         |
| ---------------- | -------------------------------------- | -------------- | -------------------------- |
| `account_state`  | GetPortfolioState                      | UpsertAccount  | Broker client              |
| `positions`      | GetPortfolioState, GetTotalMarketValue | UpsertPosition | Broker client              |
| `risk_config`    | GetRiskConfig                          | —              | Manual seed only           |
| `risk_state`     | IsHalted                               | —              | External (not implemented) |
| `blacklist`      | IsBanned                               | —              | External (not implemented) |
| `validation_log` | —                                      | —              | Schema exists, unused      |

---

### Environment Variables

| Variable        | Default                  | Required |
| --------------- | ------------------------ | -------- |
| `DB_CONN`       | —                        | **Yes**  |
| `BROKER_WS_URL` | `ws://localhost:7003/ws` | No       |
| `USER_ALIAS`    | `wancm`                  | No       |
| `LOGGER_WS_URL` | `ws://127.0.0.1:6000`    | No       |
| `LOG_FORMAT`    | `text`                   | No       |

---

## WebSocket Summary

### Inbound (portfolio-manager as server)

| Port    | Path  | Direction | Message Type                     | Sample Schema                                                                                                                                                                       |
| ------- | ----- | --------- | -------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `:9001` | `/ws` | → client  | heartbeat push                   | `{"ts": 1746793200000}`                                                                                                                                                             |
| `:7000` | `/ws` | ← client  | `get_portfolio_state`            | `{"type":"get_portfolio_state","request_id":"r1","user_alias":"wancm","symbol":"AAPL"}`                                                                                             |
| `:7000` | `/ws` | → client  | `portfolio_state_response`       | `{"type":"portfolio_state_response","request_id":"r1","user_alias":"wancm","symbol":"AAPL","current_position":100.5,"avg_cost":150.25,"max_limit":200.0,"account_balance":50000.0}` |
| `:7000` | `/ws` | → client  | `error`                          | `{"type":"error","request_id":"r1","error":"db error: ..."}`                                                                                                                        |
| `:7001` | `/ws` | ← client  | `validate_order`                 | `{"type":"validate_order","request_id":"r2","user_alias":"wancm","symbol":"AAPL","action":"BUY","quantity":10.5,"price":150.25}`                                                    |
| `:7001` | `/ws` | → client  | `validation_response` (allowed)  | `{"type":"validation_response","request_id":"r2","allowed":true,"reason":"OK","adjusted_quantity":10.5}`                                                                            |
| `:7001` | `/ws` | → client  | `validation_response` (rejected) | `{"type":"validation_response","request_id":"r2","allowed":false,"reason":"exceeds symbol position limit, max additional 50.5","adjusted_quantity":50.5}`                           |

### Outbound (portfolio-manager as client)

| Target         | Default URL              | Direction | Message Type                  | Sample Schema                                                                                                                       |
| -------------- | ------------------------ | --------- | ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| broker-manager | `ws://localhost:7003/ws` | → broker  | `auth` (sent once on connect) | `{"type":"auth","user_alias":"wancm"}`                                                                                              |
| broker-manager | `ws://localhost:7003/ws` | ← broker  | `account_update`              | `{"type":"account_update","user_alias":"wancm","balance":50000.0,"equity":51234.56}`                                                |
| broker-manager | `ws://localhost:7003/ws` | ← broker  | `position_update`             | `{"type":"position_update","user_alias":"wancm","symbol":"AAPL","quantity":100.5,"avg_price":150.25}`                               |
| logger         | `ws://127.0.0.1:6000`    | → logger  | `log`                         | `{"source_system":"portfolio-manager","action":"log","timestamp":"2026-05-09T12:34:56Z","type":"info","message":"...","tick":null}` |
