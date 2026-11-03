-- The inbox compares each attempt with newer work on the same PR. Keep that
-- lookup indexed as history grows, including when GitHub's repository casing
-- differs between a request and a webhook.
CREATE INDEX eli_repairs_pr_latest ON eli_repairs(repo COLLATE NOCASE, pull_number, created_at DESC, id DESC);
