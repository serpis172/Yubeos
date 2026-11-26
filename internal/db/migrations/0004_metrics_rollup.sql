-- One row per series and minute (min/avg/max), kept 30 days: history beyond the one-hour ring buffer.
CREATE TABLE metrics_rollup(
  series TEXT NOT NULL,
  ts INTEGER NOT NULL,                      -- start of the minute, unix ms
  min REAL NOT NULL,
  avg REAL NOT NULL,
  max REAL NOT NULL,
  n INTEGER NOT NULL,
  PRIMARY KEY(series, ts)) WITHOUT ROWID;
