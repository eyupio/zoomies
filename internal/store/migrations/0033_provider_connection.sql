-- A provider whose API is reachable only through a private connection.
--
-- The Tailcat address is sealed with the instance key, exactly as the
-- credential beside it is: it is a persistent connection capability, and a
-- row that can be read back over the API or copied into a bug report must not
-- carry it. NULL means the controller dials the endpoint directly.
ALTER TABLE providers ADD COLUMN tailcat_address_enc BLOB;
