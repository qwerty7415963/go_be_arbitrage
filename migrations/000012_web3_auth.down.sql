DROP TABLE IF EXISTS wallet_nonces;
DROP TABLE IF EXISTS wallet_addresses;

ALTER TABLE users DROP COLUMN IF EXISTS auth_method;
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;
ALTER TABLE users ALTER COLUMN email SET NOT NULL;

-- Restore original unique constraint
ALTER TABLE users ADD CONSTRAINT users_tenant_id_email_key UNIQUE (tenant_id, email);
