ALTER TABLE runners ADD COLUMN startup_service_ms INTEGER;
CREATE INDEX runners_startup_history ON runners(created_at DESC) WHERE startup_service_ms IS NOT NULL;
