-- +goose Up

CREATE TABLE runner_pools (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name             text NOT NULL UNIQUE,
    description      text,
    cpu              text NOT NULL,
    memory           text NOT NULL,
    gpu_vendor       text,
    gpu_model        text,
    gpu_count        int,
    arch             text NOT NULL DEFAULT 'amd64',
    node_selector    jsonb,
    tolerations      jsonb,
    spot_preferred   boolean NOT NULL DEFAULT false,
    spot_fallback    text NOT NULL DEFAULT 'on-demand',
    default_timeout  text,
    ready            boolean NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS runner_pools;
