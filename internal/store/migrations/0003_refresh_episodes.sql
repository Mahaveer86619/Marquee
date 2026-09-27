-- Episodes cached before 0002 have no ratings; mark every season's episode
-- list as not fetched so it is refreshed on next view.
UPDATE seasons SET episodes_fetched_at = NULL;
