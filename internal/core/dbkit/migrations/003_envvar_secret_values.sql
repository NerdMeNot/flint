-- +goose Up
-- Encrypt Flint-managed secret env-variable values at rest. Non-secret values
-- continue to live in `value` (plaintext); for secret variables the AES-256-GCM
-- envelope ciphertext (same master key as forge credentials) goes in `value_enc`
-- and `value` is left empty. Decryption happens server-side in the agent
-- secret-injection path, so plaintext never lands in the pod spec or etcd.
ALTER TABLE env_variable_values ADD COLUMN value_enc bytea;

-- +goose Down
ALTER TABLE env_variable_values DROP COLUMN value_enc;
