ALTER TABLE connect_transaction
 ADD COLUMN callback_origin text NOT NULL DEFAULT '',
 ADD COLUMN auth_config_revision bigint NOT NULL DEFAULT 1,
 ADD COLUMN authorization_ciphertext bytea,
 ADD COLUMN ticket_digest bytea,
 ADD COLUMN ticket_expires_at timestamptz,
 ADD COLUMN created_at timestamptz NOT NULL DEFAULT now(),
 ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
