-- +goose Up
-- Curated tag-key registry — governs the `key:value` namespaces projects may
-- use in projects.tags[]. (Free-form tags need no registry entry.)
CREATE TABLE public.tag_keys (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         uuid NOT NULL REFERENCES public.orgs(id) ON DELETE CASCADE,
    key            text NOT NULL,
    label          text NOT NULL,
    allowed_values text[],
    color          text NOT NULL DEFAULT '#6366f1',
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, key)
);

-- +goose Down
DROP TABLE public.tag_keys;
