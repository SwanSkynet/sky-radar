-- api_keys backs the public API v1's elevated tier per
-- docs/architecture/data-model.md's "API authentication & rate limiting"
-- section: keys are issued out-of-band (cmd/apigateway -issue-key) and
-- looked up by the SHA-256 hash of the presented key, never the raw key
-- itself, so a database read alone can't leak a usable credential.
CREATE TABLE IF NOT EXISTS api_keys (
  id uuid PRIMARY KEY,
  key_hash text NOT NULL UNIQUE,
  label text NOT NULL,
  tier text NOT NULL CHECK (tier IN ('anonymous', 'elevated')),
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz
);
