-- +goose Up
-- Add input_cost_per_1m and output_cost_per_1m to model_routings for custom per-model rate overrides
ALTER TABLE model_routings ADD COLUMN IF NOT EXISTS input_cost_per_1m NUMERIC(12, 6) DEFAULT NULL;
ALTER TABLE model_routings ADD COLUMN IF NOT EXISTS output_cost_per_1m NUMERIC(12, 6) DEFAULT NULL;

-- +goose Down
ALTER TABLE model_routings DROP COLUMN IF EXISTS output_cost_per_1m;
ALTER TABLE model_routings DROP COLUMN IF EXISTS input_cost_per_1m;
