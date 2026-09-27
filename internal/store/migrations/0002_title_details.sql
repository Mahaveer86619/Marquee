-- Display details (tagline, dates, rating, credits, networks, episode counts)
-- stored as JSON; see catalog.Details. Episode ratings for the season view.

ALTER TABLE titles ADD COLUMN details TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(details));
ALTER TABLE items ADD COLUMN rating REAL;
