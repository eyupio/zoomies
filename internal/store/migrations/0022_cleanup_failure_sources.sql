-- GitHub and the host finish independently. Keep their failures separately
-- so that a successful retry on one side cannot erase the other's complaint.
-- cleanup_error stays the combined, public description used by the UI.
ALTER TABLE runners ADD COLUMN host_cleanup_error TEXT NOT NULL DEFAULT '';
ALTER TABLE runners ADD COLUMN registration_cleanup_error TEXT NOT NULL DEFAULT '';

-- Older builds recorded one error only. Preserve the error they retained;
-- an error they already overwrote cannot be recovered from the database.
UPDATE runners SET
    registration_cleanup_error = CASE
        WHEN cleanup_error LIKE 'the GitHub runner registration could not be deleted:%'
        THEN cleanup_error ELSE '' END,
    host_cleanup_error = CASE
        WHEN cleanup_error LIKE 'the GitHub runner registration could not be deleted:%'
        THEN '' ELSE cleanup_error END,
    cleaned_up_at = CASE WHEN cleanup_error <> '' THEN NULL ELSE cleaned_up_at END;
