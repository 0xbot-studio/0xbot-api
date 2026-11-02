-- Additive migration: preserve all existing route settings.
INSERT INTO provider_settings(id,enabled,priority) VALUES
 ('publicnode-bsc',true,10),
 ('bnbchain-bsc',true,20),
 ('publicnode-polygon',true,10),
 ('drpc-polygon',true,20)
ON CONFLICT (id) DO NOTHING;
