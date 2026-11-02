-- Additive seed only; does not overwrite an operator's existing settings.
INSERT INTO provider_settings(id,enabled,priority)
VALUES('defillama-prices',true,100)
ON CONFLICT(id) DO NOTHING;
