CREATE EXTENSION IF NOT EXISTS vector;

CREATE SCHEMA IF NOT EXISTS rag;

CREATE TABLE IF NOT EXISTS rag.documents (
  source TEXT PRIMARY KEY,
  content_hash TEXT NOT NULL,
  embed_model TEXT NOT NULL,
  ingested_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS rag.chunks (
id BIGSERIAL PRIMARY KEY,
  source TEXT NOT NULL REFERENCES rag.documents (source) ON DELETE CASCADE,
  chunk_index INT NOT NULL,
  content TEXT NOT NULL,
  embedding vector(768) NOT NULL,
  tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', content)) STORED,
  UNIQUE (source, chunk_index)
);

CREATE INDEX IF NOT EXISTS chunks_embedding_hnsw ON rag.chunks USING hnsw (embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS chunks_tsv_gin ON rag.chunks USING gin (tsv);

CREATE SCHEMA IF NOT EXISTS uw;

CREATE TABLE IF NOT EXISTS uw.policies (
  policy_id BIGINT PRIMARY KEY,
  claim_count INT NOT NULL,
  exposure NUMERIC(9, 6) NOT NULL,
  driver_age INT NOT NULL,
  vehicle_age INT NOT NULL,
  fuel TEXT NOT NULL,
  area TEXT NOT NULL,
  bonus_malus INT NOT NULL,
  total_claim_amount NUMERIC(12, 2) NOT NULL
);

COMMENT ON COLUMN uw.policies.policy_id IS 'Policy identifier [IDpol]';
COMMENT ON COLUMN uw.policies.claim_count IS 'Number of claims made on the policy [ClaimNb]';
COMMENT ON COLUMN uw.policies.exposure IS 'Years the policy was in force, e.g. 0.5 = six months [Exposure]; weight rates by this';
COMMENT ON COLUMN uw.policies.driver_age IS 'Driver age in years [DrivAge]';
COMMENT ON COLUMN uw.policies.vehicle_age IS 'Vehicle age in years [VehAge]';
COMMENT ON COLUMN uw.policies.fuel IS 'Diesel or Regular [VehGas]';
COMMENT ON COLUMN uw.policies.area IS 'How urban the driver''s town is, A (rural) to F (city centre) [Area]';
COMMENT ON COLUMN uw.policies.bonus_malus IS 'No-claims discount level x 100: 50 = biggest discount, 100 = starting level, above 100 = penalty for past claims [BonusMalus]';
COMMENT ON COLUMN uw.policies.total_claim_amount IS 'Total euro paid out in claims on the policy [sum of ClaimAmount]';
