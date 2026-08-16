CREATE TABLE IF NOT EXISTS runtime_settings_revision (
  singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
  version INTEGER NOT NULL CHECK (version >= 0),
  updated_at INTEGER NOT NULL
);