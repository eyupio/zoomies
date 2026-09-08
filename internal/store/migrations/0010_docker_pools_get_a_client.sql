-- A pool whose docker_mode gives its jobs a daemon needs an image with a Docker
-- client, and the stock runner image carries none: on such a pool the daemon
-- came up unused and the job failed at its first docker step with "Unable to
-- locate executable file: docker". The API now swaps the stock image for its
-- Docker variant as the pool is saved, and the controller does the same as it
-- makes a runner. This brings the pools saved before either did into line, so
-- that the image a pool shows is the image its runners run.
--
-- Only the stock repository under a moving tag is touched -- no tag, :latest
-- or :main, which CI publishes for both images from the same commit. A pinned
-- tag stays pinned: the variant exists only beside the tags made since it was
-- added, and a pool moved onto a tag the registry lacks would stop running
-- every job. A digest names one exact image and cannot be moved to another,
-- and an image of the operator's own is theirs, whatever it is called
-- elsewhere. The timestamp moves because the row did; the arithmetic is the
-- store's ms() in SQL, rounded rather than truncated because the float trip
-- through julianday lands a hair under the integer about half the time.
UPDATE pools
   SET image = 'ghcr.io/eyupio/zoomies-runner-docker'
               || substr(image, length('ghcr.io/eyupio/zoomies-runner') + 1),
       updated_at = CAST(round((julianday('now') - 2440587.5) * 86400000) AS INTEGER)
 WHERE docker_mode <> 'none'
   AND image IN ('ghcr.io/eyupio/zoomies-runner',
                 'ghcr.io/eyupio/zoomies-runner:latest',
                 'ghcr.io/eyupio/zoomies-runner:main');
