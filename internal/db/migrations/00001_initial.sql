-- +goose Up
CREATE TABLE sources (
    id INTEGER PRIMARY KEY,
    distribution TEXT NOT NULL,
    branch TEXT NOT NULL,
    version TEXT NOT NULL,
    channel TEXT,
    upstream_url TEXT NOT NULL,
    image TEXT NOT NULL,
    tag_template TEXT NOT NULL,
    package_manager TEXT NOT NULL,
    origin TEXT NOT NULL,
    rolling BOOLEAN NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    overrides JSON NOT NULL DEFAULT '[]',
    targets JSON NOT NULL DEFAULT '{}',
    synced_at TIMESTAMP,
    UNIQUE (distribution, version)
);

CREATE TABLE jobs (
    request_hash TEXT PRIMARY KEY NOT NULL,
    replay_of TEXT,
    source_id INTEGER REFERENCES sources(id) ON DELETE SET NULL,
    version TEXT NOT NULL,
    target TEXT NOT NULL,
    profile TEXT NOT NULL,
    status TEXT NOT NULL,
    stage TEXT,
    enqueued_at TIMESTAMP NOT NULL,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    expires_at TIMESTAMP,
    purge_at TIMESTAMP,
    detail TEXT,
    error TEXT,
    client TEXT,
    request JSON NOT NULL,
    result JSON,
    handle JSON,
    stages JSON NOT NULL DEFAULT '[]',
    options JSON
);
CREATE INDEX jobs_version_target_profile ON jobs(version, target, profile);
CREATE INDEX jobs_status ON jobs(status);
CREATE INDEX jobs_expires_at ON jobs(expires_at);
CREATE INDEX jobs_purge_at ON jobs(purge_at);
CREATE INDEX jobs_replay_of ON jobs(replay_of);
CREATE INDEX jobs_source_id ON jobs(source_id);

CREATE TABLE stat_counters (
    day TEXT NOT NULL,
    metric TEXT NOT NULL,
    dim TEXT NOT NULL,
    value INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, metric, dim)
);

CREATE TABLE kv (
    key TEXT PRIMARY KEY NOT NULL,
    value JSON NOT NULL
);

-- +goose Down
DROP TABLE kv;
DROP TABLE stat_counters;
DROP TABLE jobs;
DROP TABLE sources;
