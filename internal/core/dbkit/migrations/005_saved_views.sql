-- +goose Up
-- Saved views — a user's named navigation targets (a route + its URL filters).
-- Personal to the owning user; smart views are built-in (client-side) and not
-- stored here.
CREATE TABLE public.saved_views (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES public.orgs(id) ON DELETE CASCADE,
    owner_user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    name          text NOT NULL,
    route         text NOT NULL,
    selector      jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_saved_views_owner ON public.saved_views (org_id, owner_user_id);

-- +goose Down
DROP TABLE public.saved_views;
