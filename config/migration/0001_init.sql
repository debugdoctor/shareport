CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- Runtime state is intentionally separate from "settings" because SaveDB clears
-- settings as part of config rewrite.
CREATE TABLE IF NOT EXISTS runtime_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pools (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  policy TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS links (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  pool_id INTEGER NOT NULL,
  link TEXT NOT NULL,
  position INTEGER NOT NULL,
  FOREIGN KEY(pool_id) REFERENCES pools(id)
);

CREATE TABLE IF NOT EXISTS link_attrs (
  link_hash TEXT PRIMARY KEY,
  limit_bytes INTEGER NOT NULL,
  reset_day INTEGER NOT NULL,
  reset_time TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_links_pool_position ON links(pool_id, position);
