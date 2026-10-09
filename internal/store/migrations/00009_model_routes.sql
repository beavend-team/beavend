-- +goose Up
-- Add model_routes to ai_apps for flexible multimodal model routing
ALTER TABLE ai_apps ADD COLUMN IF NOT EXISTS model_routes JSONB DEFAULT NULL;

-- Add input_modalities and output_modalities to model_routings
ALTER TABLE model_routings ADD COLUMN IF NOT EXISTS input_modalities TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE model_routings ADD COLUMN IF NOT EXISTS output_modalities TEXT[] NOT NULL DEFAULT '{}';

-- Backfill default from model_routing_id in active app version
UPDATE ai_apps a
SET model_routes = jsonb_build_object('default', aav.model_routing_id, 'rules', '[]'::jsonb)
FROM ai_app_versions aav
WHERE a.id = aav.app_id
  AND a.active_version = aav.version
  AND aav.model_routing_id IS NOT NULL
  AND aav.model_routing_id <> ''
  AND a.model_routes IS NULL;

-- +goose Down
ALTER TABLE model_routings DROP COLUMN IF EXISTS output_modalities;
ALTER TABLE model_routings DROP COLUMN IF EXISTS input_modalities;
ALTER TABLE ai_apps DROP COLUMN IF EXISTS model_routes;
