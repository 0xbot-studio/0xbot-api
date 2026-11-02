-- Additive; preserve operator settings. Missing credentials fail closed.
INSERT INTO provider_settings(id,enabled,priority) VALUES('lifi',true,100) ON CONFLICT(id) DO NOTHING;
