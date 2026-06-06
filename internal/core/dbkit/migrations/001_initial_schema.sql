-- +goose Up

-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ────────────────────────────────────────────────────────────
-- Organisations
-- ────────────────────────────────────────────────────────────
CREATE TABLE orgs (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                text NOT NULL UNIQUE,
    slug                text NOT NULL UNIQUE,
    temporal_namespace  text NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now()
);

-- ────────────────────────────────────────────────────────────
-- Users
-- ────────────────────────────────────────────────────────────
CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid NOT NULL REFERENCES orgs(id),
    email           text NOT NULL,
    external_id     text NOT NULL,
    name            text,
    avatar_url      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, external_id)
);

-- ────────────────────────────────────────────────────────────
-- Teams
-- ────────────────────────────────────────────────────────────
CREATE TABLE teams (
    id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id  uuid NOT NULL REFERENCES orgs(id),
    name    text NOT NULL,
    slug    text NOT NULL,
    UNIQUE (org_id, slug)
);

-- ────────────────────────────────────────────────────────────
-- RBAC: IdP group claim → Flint role
-- ────────────────────────────────────────────────────────────
CREATE TABLE rbac_policies (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES orgs(id),
    group_claim   text NOT NULL,
    role          text NOT NULL CHECK (role IN (
                      'org_admin', 'pipeline_admin', 'developer', 'viewer'
                  )),
    resource_type text,
    resource_id   uuid,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- ────────────────────────────────────────────────────────────
-- Forge connections
-- ────────────────────────────────────────────────────────────
CREATE TABLE forge_connections (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL REFERENCES orgs(id),
    forge_type       text NOT NULL CHECK (forge_type IN ('github','gitlab','bitbucket')),
    display_name     text NOT NULL,
    app_id           text,
    installation_id  text,
    webhook_secret   text NOT NULL,
    credentials_enc  bytea NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now()
);

-- ────────────────────────────────────────────────────────────
-- Projects
-- ────────────────────────────────────────────────────────────
CREATE TABLE projects (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL REFERENCES orgs(id),
    forge_id         uuid NOT NULL REFERENCES forge_connections(id),
    team_id          uuid REFERENCES teams(id),
    repo_path        text NOT NULL,
    repo_url         text NOT NULL,
    display_name     text,
    description      text,
    colour           text NOT NULL DEFAULT '#6366f1',
    icon             text,
    tags             text[] NOT NULL DEFAULT '{}',
    default_branch   text NOT NULL DEFAULT 'main',
    pipeline_source  jsonb NOT NULL DEFAULT '{"type":"self","path":".flint/"}',
    webhook_id       text,
    is_archived      boolean NOT NULL DEFAULT false,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (forge_id, repo_path)
);

-- ────────────────────────────────────────────────────────────
-- Project favourites
-- ────────────────────────────────────────────────────────────
CREATE TABLE project_favourites (
    user_id     uuid NOT NULL REFERENCES users(id),
    project_id  uuid NOT NULL REFERENCES projects(id),
    PRIMARY KEY (user_id, project_id)
);

-- ────────────────────────────────────────────────────────────
-- Pipeline runs
-- ────────────────────────────────────────────────────────────
CREATE TABLE pipeline_runs (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id            uuid NOT NULL REFERENCES projects(id),
    org_id                uuid NOT NULL REFERENCES orgs(id),
    temporal_workflow_id  text NOT NULL UNIQUE,
    workflow_file         text NOT NULL,
    trigger_type          text NOT NULL,
    trigger_ref           text,
    commit_sha            text,
    commit_message        text,
    triggered_by          text,
    status                text NOT NULL DEFAULT 'running',
    runner_pool           text,
    started_at            timestamptz NOT NULL DEFAULT now(),
    finished_at           timestamptz,
    duration_ms           int,
    created_at            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_pipeline_runs_project
    ON pipeline_runs (project_id, started_at DESC);
CREATE INDEX idx_pipeline_runs_running
    ON pipeline_runs (status) WHERE status = 'running';

-- ────────────────────────────────────────────────────────────
-- Secrets (envelope encrypted)
-- ────────────────────────────────────────────────────────────
CREATE TABLE secrets (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL REFERENCES orgs(id),
    project_id       uuid REFERENCES projects(id),
    name             text NOT NULL,
    encrypted_value  bytea NOT NULL,
    created_by       uuid REFERENCES users(id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, project_id, name)
);

-- ────────────────────────────────────────────────────────────
-- Pipeline modules
-- ────────────────────────────────────────────────────────────
CREATE TABLE pipeline_modules (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid REFERENCES orgs(id),
    name            text NOT NULL,
    description     text,
    oci_ref         text NOT NULL,
    schema_version  text NOT NULL,
    inputs_schema   jsonb,
    outputs_schema  jsonb,
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- ────────────────────────────────────────────────────────────
-- API keys
-- ────────────────────────────────────────────────────────────
CREATE TABLE api_keys (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES orgs(id),
    user_id      uuid REFERENCES users(id),
    name         text NOT NULL,
    key_hash     text NOT NULL UNIQUE,
    scopes       text[] NOT NULL,
    expires_at   timestamptz,
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- ────────────────────────────────────────────────────────────
-- Audit log
-- ────────────────────────────────────────────────────────────
CREATE TABLE audit_log (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES orgs(id),
    user_id       uuid REFERENCES users(id),
    action        text NOT NULL,
    resource_type text NOT NULL,
    resource_id   text,
    metadata      jsonb,
    ip_address    inet,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_org ON audit_log (org_id, created_at DESC);

-- ────────────────────────────────────────────────────────────
-- Outbox
-- ────────────────────────────────────────────────────────────
CREATE TABLE flint_outbox (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type       text NOT NULL,
    payload          jsonb NOT NULL,
    status           text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','processing','resolved','failed')),
    attempts         int NOT NULL DEFAULT 0,
    max_attempts     int NOT NULL DEFAULT 5,
    idempotency_key  text NOT NULL UNIQUE,
    process_after    timestamptz NOT NULL DEFAULT now(),
    created_at       timestamptz NOT NULL DEFAULT now(),
    resolved_at      timestamptz,
    last_error       text
);

CREATE INDEX idx_outbox_pending
    ON flint_outbox (status, process_after)
    WHERE status = 'pending';

CREATE INDEX idx_outbox_processing_stale
    ON flint_outbox (status, created_at)
    WHERE status = 'processing';


-- +goose Down

DROP TABLE IF EXISTS flint_outbox;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS pipeline_modules;
DROP TABLE IF EXISTS secrets;
DROP TABLE IF EXISTS pipeline_runs;
DROP TABLE IF EXISTS project_favourites;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS forge_connections;
DROP TABLE IF EXISTS rbac_policies;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS orgs;
