-- +goose Up
-- Cron-scheduled workflow runs (the Workflows product's scheduler).
CREATE TABLE public.workflow_schedules (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES public.orgs(id),
    name        text NOT NULL,
    cron        text NOT NULL,
    definition  text NOT NULL,
    enabled     boolean NOT NULL DEFAULT true,
    next_run_at timestamptz NOT NULL,
    last_run_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_workflow_schedules_due ON public.workflow_schedules (next_run_at) WHERE enabled;

-- +goose Down
DROP TABLE public.workflow_schedules;
