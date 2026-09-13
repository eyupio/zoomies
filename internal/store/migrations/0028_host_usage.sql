-- Measurements expire independently of agent connectivity. Existing hosts
-- keep reservation-based placement until their agents report usage.
ALTER TABLE hosts ADD COLUMN usage TEXT NOT NULL DEFAULT '{}';
