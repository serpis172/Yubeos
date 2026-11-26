-- Dashboard history beyond the one-hour in-memory ring: one row per series and minute (docs/DATA-MODEL.md).
-- ts_min is the unix time (seconds) of the start of the minute. Rows older than 30 days are pruned by the sampler.
CREATE TABLE metrics_rollup(
  series TEXT NOT NULL,
  ts_min INTEGER NOT NULL,
  min REAL, avg REAL, max REAL,
  PRIMARY KEY(series, ts_min)) WITHOUT ROWID;
