-- 009: tokenized-equity read capabilities (data.rwa.token_balance, market.equity_price).
-- Pyth Hermes serves equity reference prices; fail closed when unreachable.
INSERT INTO provider_settings(id,enabled,priority) VALUES('pyth-prices',true,100) ON CONFLICT(id) DO NOTHING;
