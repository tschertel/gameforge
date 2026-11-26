-- The checks for new versions, counted per day (UTC), app version and
-- system. Nothing else is stored: no IP address, no ID, no time of day.
CREATE TABLE IF NOT EXISTS checks (
  day     TEXT    NOT NULL, -- 2026-11-26
  version TEXT    NOT NULL, -- 2.22.0
  os      TEXT    NOT NULL, -- windows, linux, darwin
  arch    TEXT    NOT NULL, -- amd64, arm64
  count   INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (day, version, os, arch)
);

-- The newest release as GitHub gave it last, asked again after an hour.
CREATE TABLE IF NOT EXISTS release (
  id      INTEGER PRIMARY KEY CHECK (id = 1),
  tag     TEXT    NOT NULL,
  page    TEXT    NOT NULL,
  fetched INTEGER NOT NULL -- milliseconds since 1970
);
