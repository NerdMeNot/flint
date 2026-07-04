-- +goose Up
-- Consolidated baseline schema.
--
-- Flint is pre-1.0 and not yet deployed, so the incremental migration
-- history (001-020) was squashed into this single source-of-truth schema.
-- It is workflow-ready (pipeline_runs.project_id/workflow_file nullable,
-- kind discriminator) and carries no legacy Temporal columns.

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: EXTENSION pgcrypto; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON EXTENSION pgcrypto IS 'cryptographic functions';


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: api_key_environment_scope; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_key_environment_scope (
    api_key_id uuid NOT NULL,
    environment_id uuid NOT NULL
);


--
-- Name: api_key_workspace_scope; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_key_workspace_scope (
    api_key_id uuid NOT NULL,
    workspace_id uuid NOT NULL
);


--
-- Name: api_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_keys (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    user_id uuid,
    name text NOT NULL,
    key_hash text NOT NULL,
    scopes text[] NOT NULL,
    expires_at timestamptz,
    last_used_at timestamptz,
    created_at timestamptz DEFAULT now() NOT NULL,
    role_id uuid
);


--
-- Name: audit_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audit_log (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    user_id uuid,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text,
    metadata jsonb,
    ip_address inet,
    created_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: auth_provider_config; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.auth_provider_config (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    provider_type text NOT NULL,
    display_name text DEFAULT 'default'::text NOT NULL,
    config_enc bytea NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    CONSTRAINT auth_provider_config_provider_type_check CHECK ((provider_type = ANY (ARRAY['oidc'::text, 'saml'::text])))
);


--
-- Name: casbin_rules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.casbin_rules (
    id bigint NOT NULL,
    ptype text NOT NULL,
    v0 text DEFAULT ''::text NOT NULL,
    v1 text DEFAULT ''::text NOT NULL,
    v2 text DEFAULT ''::text NOT NULL,
    v3 text DEFAULT ''::text NOT NULL,
    v4 text DEFAULT ''::text NOT NULL,
    v5 text DEFAULT ''::text NOT NULL
);


--
-- Name: casbin_rules_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.casbin_rules_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: casbin_rules_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.casbin_rules_id_seq OWNED BY public.casbin_rules.id;


--
-- Name: env_variable_values; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.env_variable_values (
    variable_id uuid NOT NULL,
    environment_id uuid,
    value text NOT NULL,
    value_enc bytea,
    updated_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: env_variables; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.env_variables (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    name text NOT NULL,
    description text,
    scope text DEFAULT 'environment'::text NOT NULL,
    is_secret boolean DEFAULT false NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    CONSTRAINT env_variables_scope_check CHECK ((scope = ANY (ARRAY['global'::text, 'environment'::text])))
);


--
-- Name: environments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.environments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: flint_outbox; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.flint_outbox (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 5 NOT NULL,
    idempotency_key text NOT NULL,
    process_after timestamptz DEFAULT now() NOT NULL,
    claimed_at timestamptz,
    created_at timestamptz DEFAULT now() NOT NULL,
    resolved_at timestamptz,
    last_error text,
    CONSTRAINT flint_outbox_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'processing'::text, 'resolved'::text, 'failed'::text])))
);


--
-- Name: forge_connections; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.forge_connections (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    forge_type text NOT NULL,
    display_name text NOT NULL,
    app_id text,
    installation_id text,
    webhook_secret text NOT NULL,
    credentials_enc bytea NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    CONSTRAINT forge_connections_forge_type_check CHECK ((forge_type = ANY (ARRAY['github'::text, 'gitlab'::text, 'bitbucket'::text])))
);


--
-- Name: login_attempts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.login_attempts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email text NOT NULL,
    ip_address inet,
    success boolean NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: orgs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.orgs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    concurrency_limit integer DEFAULT 20 NOT NULL,
    -- When true, a Project must declare spec.workspace; the reconciler marks
    -- projects without one NotReady instead of inferring a workspace.
    require_project_workspace boolean DEFAULT false NOT NULL,
    -- When true, SSO users receive ONLY the roles their IdP groups map to
    -- (deny-by-default); the configured default role is not granted as a
    -- fallback. See sso_group_role_mappings.
    sso_strict_groups boolean DEFAULT false NOT NULL,
    -- When true, IdP-provisioned users must sign in via SSO; password login is
    -- rejected for them. Local/manual accounts (external_id = email) remain a
    -- break-glass path and are never locked out.
    require_sso boolean DEFAULT false NOT NULL
);


--
-- Name: personal_tokens; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.personal_tokens (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    name text NOT NULL,
    token_hash text NOT NULL,
    expires_at timestamptz,
    last_used_at timestamptz,
    created_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: pipeline_modules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.pipeline_modules (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid,
    name text NOT NULL,
    description text,
    oci_ref text NOT NULL,
    schema_version text NOT NULL,
    inputs_schema jsonb,
    outputs_schema jsonb,
    created_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: pipeline_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.pipeline_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid,
    org_id uuid NOT NULL,
    workflow_file text,
    trigger_type text NOT NULL,
    trigger_ref text,
    commit_sha text,
    commit_message text,
    triggered_by text,
    status text DEFAULT 'running'::text NOT NULL,
    started_at timestamptz DEFAULT now() NOT NULL,
    finished_at timestamptz,
    duration_ms integer,
    created_at timestamptz DEFAULT now() NOT NULL,
    workflow_id uuid,
    branch text,
    repo text,
    environment text,
    error_message text,
    kind text DEFAULT 'ci'::text NOT NULL,
    cleaned_at timestamptz,
    concurrency_group text
);


--
-- Name: project_favourites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.project_favourites (
    user_id uuid NOT NULL,
    project_id uuid NOT NULL
);


--
-- Name: projects; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.projects (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    forge_id uuid NOT NULL,
    repo_path text NOT NULL,
    repo_url text NOT NULL,
    display_name text,
    description text,
    colour text DEFAULT '#6366f1'::text NOT NULL,
    icon text,
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    default_branch text DEFAULT 'main'::text NOT NULL,
    pipeline_source jsonb DEFAULT '{"path": ".flint/", "type": "self"}'::jsonb NOT NULL,
    is_archived boolean DEFAULT false NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    workspace_id uuid NOT NULL,
    -- true when workspace_id was inferred (from the repo owner / fell back to
    -- the default) rather than declared on the Project's spec.workspace.
    workspace_inferred boolean DEFAULT false NOT NULL,
    -- id of the inbound webhook provisioned on the forge repo (so pushes arrive),
    -- recorded so it can be removed when the project is archived. Set after the
    -- project row exists; null until/unless provisioning succeeds. Written by both
    -- the API create path and the Project CRD reconciler.
    forge_webhook_id text
);


--
-- Name: protected_environments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.protected_environments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    name text NOT NULL,
    min_role text DEFAULT 'pipeline_admin'::text NOT NULL,
    approvers text[] DEFAULT '{}'::text[] NOT NULL,
    deploy_branches text[] DEFAULT '{}'::text[] NOT NULL,
    deploy_window jsonb,
    created_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: role_assignments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.role_assignments (
    subject text NOT NULL,
    role_id uuid NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    -- Origin of the assignment: 'internal' (manual/API, authoritative) or 'idp'
    -- (derived from an SSO group→role mapping, reconciled on each login). Only
    -- 'idp' rows are removed when the user leaves a mapped group; manual grants
    -- are never touched by group sync.
    source text DEFAULT 'internal' NOT NULL
);


--
-- Name: sso_group_role_mappings; Type: TABLE; Schema: public; Owner: -
--

-- Maps an IdP group name to a Flint role. Keyed by group NAME (not a team id)
-- so an admin can configure mappings before any member of the group has logged
-- in. On login, a user's group memberships are reconciled into role_assignments
-- with source='idp'. A group may map to several roles (multiple rows).
CREATE TABLE public.sso_group_role_mappings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    group_name text NOT NULL,
    role_id uuid NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    CONSTRAINT sso_group_role_mappings_pkey PRIMARY KEY (id),
    CONSTRAINT sso_group_role_mappings_unique UNIQUE (org_id, group_name, role_id)
);

CREATE INDEX idx_sso_group_role_mappings_org ON public.sso_group_role_mappings USING btree (org_id);


--
-- Name: scim_tokens; Type: TABLE; Schema: public; Owner: -
--

-- Bearer tokens for the SCIM 2.0 provisioning API. The plaintext token is shown
-- once at creation; only its SHA-256 hash is stored. One active token per org
-- (regenerating replaces it).
CREATE TABLE public.scim_tokens (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    token_hash text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    last_used_at timestamptz,
    CONSTRAINT scim_tokens_pkey PRIMARY KEY (id),
    CONSTRAINT scim_tokens_hash_unique UNIQUE (token_hash)
);

CREATE INDEX idx_scim_tokens_org ON public.scim_tokens USING btree (org_id);


--
-- Name: role_environment_scope; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.role_environment_scope (
    role_id uuid NOT NULL,
    environment_id uuid NOT NULL
);


--
-- Name: role_permissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.role_permissions (
    role_id uuid NOT NULL,
    object text NOT NULL,
    action text NOT NULL
);


--
-- Name: role_workspace_scope; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.role_workspace_scope (
    role_id uuid NOT NULL,
    workspace_id uuid NOT NULL
);


--
-- Name: roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.roles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    description text,
    is_system boolean DEFAULT false NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    require_mfa boolean DEFAULT false NOT NULL
);


--
-- Name: machine_pools; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.machine_pools (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    description text,
    -- compute_providers.name; 'static' pools have no elastic capacity — machines
    -- join via the pool join token instead of provider.Create.
    provider text DEFAULT 'static'::text NOT NULL,
    -- exactly one pool is the default — used when a pipeline sets no runner:. The
    -- default can't be deleted until another pool is promoted (enforced in the API).
    is_default boolean DEFAULT false NOT NULL,
    arch text DEFAULT 'amd64'::text NOT NULL,
    -- default machine shape (units strings: "4", "8Gi", "100Gi"); elastic pools use
    -- these as the Quote requirements floor, static pools as validation bounds.
    cpu text NOT NULL,
    memory text NOT NULL,
    disk text,
    gpu_vendor text,
    gpu_model text,
    gpu_count integer,
    -- optional provider allow-lists narrowing what Quote may offer
    instance_types text[],
    regions text[],
    capacity_type text DEFAULT 'on_demand'::text NOT NULL,
    -- how the fleet manager ranks quotes when provisioning for this pool
    objective text DEFAULT 'balanced'::text NOT NULL,
    min_warm integer DEFAULT 0 NOT NULL,
    max_machines integer DEFAULT 10 NOT NULL,
    idle_ttl_seconds integer DEFAULT 900 NOT NULL,
    -- per-branch/event policy overrides: [{match:{branch|event}, set:{minWarm,...}}]
    overrides jsonb,
    -- optional operator-declared amortized cost for static machines with no offer
    -- price; feeds the same economics pipeline as elastic accepted-offer prices.
    hourly_cost numeric(12,6),
    default_timeout text,
    -- static pools only: sha256 of the long-lived agent join token
    join_token_hash text,
    ready boolean DEFAULT true NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    CONSTRAINT machine_pools_capacity_check CHECK ((capacity_type = ANY (ARRAY['spot'::text, 'on_demand'::text, 'any'::text]))),
    CONSTRAINT machine_pools_objective_check CHECK ((objective = ANY (ARRAY['cost'::text, 'latency'::text, 'balanced'::text])))
);


--
-- Name: compute_providers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.compute_providers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    provider_type text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    -- envelope-encrypted credentials (forge_connections precedent); NULL = ambient
    -- credentials (instance role / env / shared config chain).
    credentials_enc bytea,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    CONSTRAINT compute_providers_type_check CHECK ((provider_type = ANY (ARRAY['static'::text, 'aws'::text])))
);


--
-- Name: machines; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.machines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    pool_id uuid NOT NULL,
    status text DEFAULT 'requested'::text NOT NULL,
    provider text NOT NULL,
    -- provider instance id (i-0abc…); NULL until Create returns, and for static machines
    provider_ref text,
    instance_type text,
    region text,
    zone text,
    capacity_type text,
    -- from the accepted offer; static machines fall back to pool hourly_cost / costs config
    price_per_hour_usd numeric(12,6),
    accepted_offer jsonb,
    cpu_millis bigint DEFAULT 0 NOT NULL,
    memory_mb bigint DEFAULT 0 NOT NULL,
    disk_gb bigint DEFAULT 0 NOT NULL,
    arch text DEFAULT 'amd64'::text NOT NULL,
    os text DEFAULT 'linux'::text NOT NULL,
    labels jsonb,
    hostname text,
    agent_version text,
    -- sha256 of the one-time registration token; cleared when consumed
    bootstrap_token_hash text,
    -- sha256 of the per-machine bearer token minted at registration
    agent_token_hash text,
    -- lease columns swept with FOR UPDATE SKIP LOCKED (the timers *pattern*; a timer
    -- row updated on every heartbeat would be pure churn)
    boot_deadline_at timestamptz,
    heartbeat_interval_seconds integer DEFAULT 10 NOT NULL,
    last_heartbeat_at timestamptz,
    heartbeat_expires_at timestamptz,
    steps_completed integer DEFAULT 0 NOT NULL,
    idle_since timestamptz,
    drain_reason text,
    requested_at timestamptz DEFAULT now() NOT NULL,
    provisioned_at timestamptz,
    registered_at timestamptz,
    terminated_at timestamptz,
    updated_at timestamptz DEFAULT now() NOT NULL,
    CONSTRAINT machines_status_check CHECK ((status = ANY (ARRAY['requested'::text, 'provisioning'::text, 'idle'::text, 'busy'::text, 'draining'::text, 'terminating'::text, 'terminated'::text, 'failed'::text, 'lost'::text])))
);


--
-- Name: machine_events; Type: TABLE; Schema: public; Owner: -
-- Append-only machine transition log. Deliberately NO foreign key (engine_events
-- precedent: FK acquires row locks on the parent under concurrent transitions).
--

CREATE TABLE public.machine_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    machine_id uuid NOT NULL,
    event_type text NOT NULL,
    from_status text,
    to_status text,
    actor text DEFAULT 'fleet'::text NOT NULL,
    reason text,
    metadata jsonb,
    created_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: step_assignments; Type: TABLE; Schema: public; Owner: -
-- One row per dispatched container step: the machine executor inserts 'pending',
-- the fleet scheduler binds a machine, the agent claims and reports. The payload
-- is the full dispatch projection persisted at dispatch time so an agent can
-- claim later / from another process without recomputing env or secret merges.
--

CREATE TABLE public.step_assignments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    step_id uuid NOT NULL,
    workflow_id uuid NOT NULL,
    run_id uuid NOT NULL,
    step_name text NOT NULL,
    attempt integer NOT NULL,
    pool_id uuid NOT NULL,
    machine_id uuid,
    status text DEFAULT 'pending'::text NOT NULL,
    cpu_millis bigint NOT NULL,
    memory_mb bigint NOT NULL,
    disk_gb bigint DEFAULT 0 NOT NULL,
    payload jsonb NOT NULL,
    cancel_requested boolean DEFAULT false NOT NULL,
    error text,
    created_at timestamptz DEFAULT now() NOT NULL,
    assigned_at timestamptz,
    claim_deadline_at timestamptz,
    started_at timestamptz,
    finished_at timestamptz,
    CONSTRAINT step_assignments_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'assigned'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text, 'lost'::text])))
);


--
-- Name: fleet_decisions; Type: TABLE; Schema: public; Owner: -
-- The economics decision ledger: every provision/terminate/drain/reconcile decision
-- with the inputs it saw, the offer it chose, the alternatives it rejected, and the
-- outcome backfilled once known. Per-assignment binds are not ledgered (too hot).
--

CREATE TABLE public.fleet_decisions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    pool_id uuid,
    machine_id uuid,
    decision_type text NOT NULL,
    inputs jsonb NOT NULL,
    chosen jsonb,
    alternatives jsonb,
    outcome text,
    outcome_metadata jsonb,
    outcome_at timestamptz,
    created_at timestamptz DEFAULT now() NOT NULL,
    CONSTRAINT fleet_decisions_type_check CHECK ((decision_type = ANY (ARRAY['provision'::text, 'terminate'::text, 'drain'::text, 'reconcile_zombie'::text, 'no_capacity'::text])))
);


--
-- Name: secrets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.secrets (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    project_id uuid,
    name text NOT NULL,
    encrypted_value bytea NOT NULL,
    created_by uuid,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    environment text
);


--
-- Name: sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    token_hash text NOT NULL,
    idp_token_enc bytea,
    logout_state_enc bytea,
    ip_address inet,
    user_agent text,
    created_at timestamptz DEFAULT now() NOT NULL,
    last_activity timestamptz DEFAULT now() NOT NULL,
    last_synced_at timestamptz,
    expires_at timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);


--
-- Name: signals; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.signals (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workflow_id uuid NOT NULL,
    signal_name text NOT NULL,
    payload jsonb NOT NULL,
    consumed boolean DEFAULT false NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: steps; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.steps (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workflow_id uuid NOT NULL,
    name text NOT NULL,
    exec_type text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    wave integer NOT NULL,
    attempt integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 1 NOT NULL,
    step_def jsonb NOT NULL,
    result jsonb,
    -- opaque executor correlation handle (machine executor: step_assignments.id)
    dispatch_handle text,
    task_token text,
    on_failure text DEFAULT 'fail'::text NOT NULL,
    timeout_seconds integer DEFAULT 7200 NOT NULL,
    retry_backoff text DEFAULT 'exponential'::text NOT NULL,
    retry_interval_seconds integer DEFAULT 5 NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    queued_at timestamptz,
    started_at timestamptz,
    finished_at timestamptz,
    deadline_at timestamptz,
    dispatched_at timestamptz,
    CONSTRAINT steps_exec_type_check CHECK ((exec_type = ANY (ARRAY['run'::text, 'use'::text, 'steps'::text, 'gate'::text, 'wait'::text]))),
    CONSTRAINT steps_name_check CHECK ((length(name) > 0)),
    CONSTRAINT steps_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'retry_wait'::text, 'queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'skipped'::text, 'cancelled'::text, 'waiting'::text])))
);


--
-- Name: team_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.team_members (
    team_id uuid NOT NULL,
    user_id uuid NOT NULL
);


--
-- Name: teams; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.teams (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    source text DEFAULT 'internal'::text NOT NULL,
    idp_group text
);


--
-- Name: timers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.timers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workflow_id uuid NOT NULL,
    step_name text NOT NULL,
    timer_type text NOT NULL,
    fires_at timestamptz NOT NULL,
    fired boolean DEFAULT false NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    CONSTRAINT timers_timer_type_check CHECK ((timer_type = ANY (ARRAY['timeout'::text, 'gate_timeout'::text, 'retry_backoff'::text, 'wait_timeout'::text])))
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    email text NOT NULL,
    external_id text NOT NULL,
    name text,
    avatar_url text,
    created_at timestamptz DEFAULT now() NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    password_hash text,
    totp_secret_enc bytea,
    totp_verified boolean DEFAULT false NOT NULL,
    mfa_required_override boolean,
    password_changed_at timestamptz,
    recovery_codes text[],
    force_password_change boolean DEFAULT false NOT NULL,
    mfa_last_used_period bigint,
    theme_mode text,
    color_theme text
);


--
-- Name: webhooks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webhooks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    url text NOT NULL,
    secret text DEFAULT ''::text NOT NULL,
    events jsonb DEFAULT '["run.completed"]'::jsonb NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: workflows; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workflows (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    run_id uuid NOT NULL,
    parent_id uuid,
    parent_step text,
    status text DEFAULT 'pending'::text NOT NULL,
    input jsonb NOT NULL,
    output jsonb,
    pipeline_yaml bytea,
    pipeline_def jsonb,
    dag_waves jsonb,
    step_outputs jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    started_at timestamptz,
    finished_at timestamptz,
    cancelled_at timestamptz,
    CONSTRAINT workflows_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'running'::text, 'paused'::text, 'succeeded'::text, 'failed'::text, 'cancelled'::text])))
);


--
-- Name: engine_events; Type: TABLE; Schema: public; Owner: -
--
-- Append-only audit/history log of every workflow and step state transition.
-- Written in the same transaction as the state change (CQRS-lite: the current
-- state still lives on steps/workflows; this is the durable history sidecar that
-- powers the run timeline, per-attempt retry history, and operator audit). Rows
-- are never updated; the sweep prunes old ones.

CREATE TABLE public.engine_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workflow_id uuid NOT NULL,
    step_name text,
    attempt integer,
    event_type text NOT NULL,
    from_status text,
    to_status text,
    actor text DEFAULT 'engine'::text NOT NULL,
    reason text,
    metadata jsonb,
    created_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: workspaces; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workspaces (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    org_id uuid NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    description text,
    is_default boolean DEFAULT false NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
);


--
-- Name: casbin_rules id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.casbin_rules ALTER COLUMN id SET DEFAULT nextval('public.casbin_rules_id_seq'::regclass);


--
-- Name: api_key_environment_scope api_key_environment_scope_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_key_environment_scope
    ADD CONSTRAINT api_key_environment_scope_pkey PRIMARY KEY (api_key_id, environment_id);


--
-- Name: api_key_workspace_scope api_key_workspace_scope_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_key_workspace_scope
    ADD CONSTRAINT api_key_workspace_scope_pkey PRIMARY KEY (api_key_id, workspace_id);


--
-- Name: api_keys api_keys_key_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_key_hash_key UNIQUE (key_hash);


--
-- Name: api_keys api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);


--
-- Name: audit_log audit_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_pkey PRIMARY KEY (id);


--
-- Name: auth_provider_config auth_provider_config_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_provider_config
    ADD CONSTRAINT auth_provider_config_pkey PRIMARY KEY (id);


--
-- Name: auth_provider_config auth_provider_config_provider_type_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_provider_config
    ADD CONSTRAINT auth_provider_config_provider_type_key UNIQUE (provider_type);


--
-- Name: casbin_rules casbin_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.casbin_rules
    ADD CONSTRAINT casbin_rules_pkey PRIMARY KEY (id);


--
-- Name: casbin_rules casbin_rules_ptype_v0_v1_v2_v3_v4_v5_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.casbin_rules
    ADD CONSTRAINT casbin_rules_ptype_v0_v1_v2_v3_v4_v5_key UNIQUE (ptype, v0, v1, v2, v3, v4, v5);


--
-- Name: env_variable_values env_variable_values_variable_id_environment_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.env_variable_values
    ADD CONSTRAINT env_variable_values_variable_id_environment_id_key UNIQUE (variable_id, environment_id);


--
-- Name: env_variables env_variables_org_id_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.env_variables
    ADD CONSTRAINT env_variables_org_id_name_key UNIQUE (org_id, name);


--
-- Name: env_variables env_variables_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.env_variables
    ADD CONSTRAINT env_variables_pkey PRIMARY KEY (id);


--
-- Name: environments environments_org_id_slug_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.environments
    ADD CONSTRAINT environments_org_id_slug_key UNIQUE (org_id, slug);


--
-- Name: environments environments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.environments
    ADD CONSTRAINT environments_pkey PRIMARY KEY (id);


--
-- Name: flint_outbox flint_outbox_idempotency_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.flint_outbox
    ADD CONSTRAINT flint_outbox_idempotency_key_key UNIQUE (idempotency_key);


--
-- Name: flint_outbox flint_outbox_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.flint_outbox
    ADD CONSTRAINT flint_outbox_pkey PRIMARY KEY (id);


--
-- Name: forge_connections forge_connections_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.forge_connections
    ADD CONSTRAINT forge_connections_pkey PRIMARY KEY (id);


--
-- Name: login_attempts login_attempts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.login_attempts
    ADD CONSTRAINT login_attempts_pkey PRIMARY KEY (id);


--
-- Name: orgs orgs_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orgs
    ADD CONSTRAINT orgs_name_key UNIQUE (name);


--
-- Name: orgs orgs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orgs
    ADD CONSTRAINT orgs_pkey PRIMARY KEY (id);


--
-- Name: orgs orgs_slug_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orgs
    ADD CONSTRAINT orgs_slug_key UNIQUE (slug);


--
-- Name: personal_tokens personal_tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_tokens
    ADD CONSTRAINT personal_tokens_pkey PRIMARY KEY (id);


--
-- Name: personal_tokens personal_tokens_token_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_tokens
    ADD CONSTRAINT personal_tokens_token_hash_key UNIQUE (token_hash);


--
-- Name: pipeline_modules pipeline_modules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pipeline_modules
    ADD CONSTRAINT pipeline_modules_pkey PRIMARY KEY (id);


--
-- Name: pipeline_runs pipeline_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pipeline_runs
    ADD CONSTRAINT pipeline_runs_pkey PRIMARY KEY (id);


--
-- Name: project_favourites project_favourites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_favourites
    ADD CONSTRAINT project_favourites_pkey PRIMARY KEY (user_id, project_id);


--
-- Name: projects projects_forge_id_repo_path_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_forge_id_repo_path_key UNIQUE (forge_id, repo_path);


--
-- Name: projects projects_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_pkey PRIMARY KEY (id);


--
-- Name: protected_environments protected_environments_org_id_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protected_environments
    ADD CONSTRAINT protected_environments_org_id_name_key UNIQUE (org_id, name);


--
-- Name: protected_environments protected_environments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protected_environments
    ADD CONSTRAINT protected_environments_pkey PRIMARY KEY (id);


--
-- Name: role_assignments role_assignments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_assignments
    ADD CONSTRAINT role_assignments_pkey PRIMARY KEY (subject, role_id);


--
-- Name: role_environment_scope role_environment_scope_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_environment_scope
    ADD CONSTRAINT role_environment_scope_pkey PRIMARY KEY (role_id, environment_id);


--
-- Name: role_permissions role_permissions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_pkey PRIMARY KEY (role_id, object, action);


--
-- Name: role_workspace_scope role_workspace_scope_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_workspace_scope
    ADD CONSTRAINT role_workspace_scope_pkey PRIMARY KEY (role_id, workspace_id);


--
-- Name: roles roles_org_id_slug_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_org_id_slug_key UNIQUE (org_id, slug);


--
-- Name: roles roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);


--
-- Name: machine_pools machine_pools_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.machine_pools
    ADD CONSTRAINT machine_pools_name_key UNIQUE (name);


--
-- Name: machine_pools machine_pools_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.machine_pools
    ADD CONSTRAINT machine_pools_pkey PRIMARY KEY (id);


--
-- Name: compute_providers compute_providers_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.compute_providers
    ADD CONSTRAINT compute_providers_name_key UNIQUE (name);


--
-- Name: compute_providers compute_providers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.compute_providers
    ADD CONSTRAINT compute_providers_pkey PRIMARY KEY (id);


--
-- Name: machines machines_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.machines
    ADD CONSTRAINT machines_pkey PRIMARY KEY (id);


--
-- Name: machines machines_pool_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
-- Pools are cold rows (CRUD only), so the FK lock risk that keeps FKs off
-- engine_events/machine_events does not apply here.
--

ALTER TABLE ONLY public.machines
    ADD CONSTRAINT machines_pool_id_fkey FOREIGN KEY (pool_id) REFERENCES public.machine_pools(id);


--
-- Name: machine_events machine_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.machine_events
    ADD CONSTRAINT machine_events_pkey PRIMARY KEY (id);


--
-- Name: step_assignments step_assignments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.step_assignments
    ADD CONSTRAINT step_assignments_pkey PRIMARY KEY (id);


--
-- Name: fleet_decisions fleet_decisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.fleet_decisions
    ADD CONSTRAINT fleet_decisions_pkey PRIMARY KEY (id);


--
-- Name: secrets secrets_org_id_project_id_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secrets
    ADD CONSTRAINT secrets_org_id_project_id_name_key UNIQUE (org_id, project_id, name);


--
-- Name: secrets secrets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secrets
    ADD CONSTRAINT secrets_pkey PRIMARY KEY (id);


--
-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);


--
-- Name: sessions sessions_token_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_token_hash_key UNIQUE (token_hash);


--
-- Name: signals signals_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.signals
    ADD CONSTRAINT signals_pkey PRIMARY KEY (id);


--
-- Name: steps steps_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.steps
    ADD CONSTRAINT steps_pkey PRIMARY KEY (id);


--
-- Name: engine_events engine_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.engine_events
    ADD CONSTRAINT engine_events_pkey PRIMARY KEY (id);


--
-- Name: steps steps_workflow_id_name_attempt_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.steps
    ADD CONSTRAINT steps_workflow_id_name_attempt_key UNIQUE (workflow_id, name, attempt);


--
-- Name: team_members team_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.team_members
    ADD CONSTRAINT team_members_pkey PRIMARY KEY (team_id, user_id);


--
-- Name: teams teams_org_id_slug_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.teams
    ADD CONSTRAINT teams_org_id_slug_key UNIQUE (org_id, slug);


--
-- Name: teams teams_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.teams
    ADD CONSTRAINT teams_pkey PRIMARY KEY (id);


--
-- Name: timers timers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timers
    ADD CONSTRAINT timers_pkey PRIMARY KEY (id);


--
-- Name: timers timers_workflow_id_step_name_timer_type_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timers
    ADD CONSTRAINT timers_workflow_id_step_name_timer_type_key UNIQUE (workflow_id, step_name, timer_type);


--
-- Name: users users_org_id_external_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_org_id_external_id_key UNIQUE (org_id, external_id);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: webhooks webhooks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhooks
    ADD CONSTRAINT webhooks_pkey PRIMARY KEY (id);


--
-- Name: workflows workflows_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflows
    ADD CONSTRAINT workflows_pkey PRIMARY KEY (id);


--
-- Name: workspaces workspaces_org_id_slug_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_org_id_slug_key UNIQUE (org_id, slug);


--
-- Name: workspaces workspaces_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_pkey PRIMARY KEY (id);


--
-- Name: idx_api_keys_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_org ON public.api_keys USING btree (org_id);


--
-- Name: idx_audit_log_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_log_org ON public.audit_log USING btree (org_id, created_at DESC);


--
-- Name: idx_casbin_rules_ptype; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_casbin_rules_ptype ON public.casbin_rules USING btree (ptype);


--
-- Name: idx_env_variable_values_env; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_env_variable_values_env ON public.env_variable_values USING btree (environment_id);


--
-- Name: idx_login_attempts_cleanup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_login_attempts_cleanup ON public.login_attempts USING btree (created_at);


--
-- Name: idx_login_attempts_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_login_attempts_email ON public.login_attempts USING btree (email, created_at DESC);


--
-- Name: idx_outbox_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_outbox_pending ON public.flint_outbox USING btree (status, process_after) WHERE (status = 'pending'::text);


--
-- Name: idx_outbox_processing_stale; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_outbox_processing_stale ON public.flint_outbox USING btree (status, created_at) WHERE (status = 'processing'::text);


--
-- Name: idx_personal_tokens_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_personal_tokens_hash ON public.personal_tokens USING btree (token_hash);


--
-- Name: idx_personal_tokens_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_personal_tokens_user ON public.personal_tokens USING btree (user_id);


--
-- Name: idx_pipeline_runs_branch; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pipeline_runs_branch ON public.pipeline_runs USING btree (branch) WHERE (branch IS NOT NULL);


--
-- Name: idx_pipeline_runs_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pipeline_runs_environment ON public.pipeline_runs USING btree (environment) WHERE (environment IS NOT NULL);


--
-- Name: idx_pipeline_runs_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pipeline_runs_project ON public.pipeline_runs USING btree (project_id, started_at DESC);


--
-- Name: idx_pipeline_runs_repo; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pipeline_runs_repo ON public.pipeline_runs USING btree (repo) WHERE (repo IS NOT NULL);


--
-- Name: idx_pipeline_runs_running; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pipeline_runs_running ON public.pipeline_runs USING btree (status) WHERE (status = 'running'::text);


--
-- Name: idx_projects_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_projects_org ON public.projects USING btree (org_id);


--
-- Name: idx_projects_workspace; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_projects_workspace ON public.projects USING btree (workspace_id) WHERE (workspace_id IS NOT NULL);


--
-- Name: idx_role_assignments_role; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_role_assignments_role ON public.role_assignments USING btree (role_id);


--
-- Name: idx_role_assignments_subject; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_role_assignments_subject ON public.role_assignments USING btree (subject);


--
-- Name: idx_role_assignments_subject_role; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_role_assignments_subject_role ON public.role_assignments USING btree (subject, role_id);


--
-- Name: idx_roles_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_roles_org ON public.roles USING btree (org_id);


--
-- Name: idx_sessions_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_expiry ON public.sessions USING btree (expires_at) WHERE (revoked_at IS NULL);


--
-- Name: idx_sessions_sync; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_sync ON public.sessions USING btree (last_synced_at) WHERE ((revoked_at IS NULL) AND (idp_token_enc IS NOT NULL));


--
-- Name: idx_sessions_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_user ON public.sessions USING btree (user_id) WHERE (revoked_at IS NULL);


--
-- Name: idx_sessions_user_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_user_expires ON public.sessions USING btree (user_id, expires_at);


--
-- Name: idx_signals_unconsumed; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_signals_unconsumed ON public.signals USING btree (workflow_id, signal_name) WHERE (consumed = false);


--
-- Name: idx_runs_concurrency_group; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runs_concurrency_group ON public.pipeline_runs USING btree (project_id, concurrency_group) WHERE (status = 'running' AND concurrency_group IS NOT NULL);


--
-- Name: idx_steps_latest_attempt; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_steps_latest_attempt ON public.steps USING btree (workflow_id, name, attempt DESC);


--
-- Name: idx_engine_events_workflow; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_engine_events_workflow ON public.engine_events USING btree (workflow_id, created_at);


--
-- Name: idx_engine_events_step; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_engine_events_step ON public.engine_events USING btree (workflow_id, step_name, attempt);


--
-- Name: idx_engine_events_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_engine_events_created ON public.engine_events USING btree (created_at);


--
-- Name: idx_steps_queued; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_steps_queued ON public.steps USING btree (status, queued_at) WHERE (status = 'queued'::text);


--
-- Name: idx_steps_claim_order; Type: INDEX; Schema: public; Owner: -
-- Matches ClaimQueuedSteps' ORDER BY wave, queued_at so a large queued backlog
-- is claimed via index scan, not a per-tick sort of the whole backlog.
--

CREATE INDEX idx_steps_claim_order ON public.steps USING btree (wave, queued_at) WHERE (status = 'queued'::text);


--
-- Name: idx_steps_running; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_steps_running ON public.steps USING btree (status, deadline_at) WHERE (status = 'running'::text);


--
-- Name: idx_steps_undispatched; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_steps_undispatched ON public.steps USING btree (started_at) WHERE ((status = 'running'::text) AND (dispatched_at IS NULL));


--
-- Name: idx_steps_waiting; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_steps_waiting ON public.steps USING btree (status) WHERE (status = 'waiting'::text);


--
-- Name: idx_steps_workflow_wave; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_steps_workflow_wave ON public.steps USING btree (workflow_id, wave, status);


--
-- Name: idx_machines_pool_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_machines_pool_active ON public.machines USING btree (pool_id, status) WHERE (status = ANY (ARRAY['requested'::text, 'provisioning'::text, 'idle'::text, 'busy'::text, 'draining'::text]));


--
-- Name: idx_machines_hb_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_machines_hb_expiry ON public.machines USING btree (heartbeat_expires_at) WHERE (status = ANY (ARRAY['idle'::text, 'busy'::text, 'draining'::text]));


--
-- Name: idx_machines_boot_deadline; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_machines_boot_deadline ON public.machines USING btree (boot_deadline_at) WHERE (status = ANY (ARRAY['requested'::text, 'provisioning'::text]));


--
-- Name: idx_machines_provider_ref; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_machines_provider_ref ON public.machines USING btree (provider, provider_ref) WHERE (provider_ref IS NOT NULL);


--
-- Name: idx_machine_events_machine; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_machine_events_machine ON public.machine_events USING btree (machine_id, created_at);


--
-- Name: idx_assignments_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_assignments_pending ON public.step_assignments USING btree (pool_id, created_at) WHERE (status = 'pending'::text);


--
-- Name: idx_assignments_machine_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_assignments_machine_active ON public.step_assignments USING btree (machine_id) WHERE (status = ANY (ARRAY['assigned'::text, 'running'::text]));


--
-- Name: idx_assignments_run; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_assignments_run ON public.step_assignments USING btree (run_id) WHERE (status = ANY (ARRAY['pending'::text, 'assigned'::text, 'running'::text]));


--
-- Name: idx_assignments_step_active; Type: INDEX; Schema: public; Owner: -
-- At most one live assignment per step attempt.
--

CREATE UNIQUE INDEX idx_assignments_step_active ON public.step_assignments USING btree (step_id) WHERE (status = ANY (ARRAY['pending'::text, 'assigned'::text, 'running'::text]));


--
-- Name: idx_fleet_decisions_pool; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_fleet_decisions_pool ON public.fleet_decisions USING btree (pool_id, created_at DESC);


--
-- Name: idx_fleet_decisions_machine; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_fleet_decisions_machine ON public.fleet_decisions USING btree (machine_id) WHERE (machine_id IS NOT NULL);


--
-- Name: idx_teams_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_teams_org ON public.teams USING btree (org_id);


--
-- Name: idx_timers_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_timers_pending ON public.timers USING btree (fires_at) WHERE (fired = false);


--
-- Name: idx_users_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_email ON public.users USING btree (email);


--
-- Name: idx_users_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_org ON public.users USING btree (org_id);


--
-- Name: idx_webhooks_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhooks_project ON public.webhooks USING btree (project_id) WHERE (is_active = true);


--
-- Name: idx_workflows_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workflows_active ON public.workflows USING btree (status) WHERE (status = ANY (ARRAY['pending'::text, 'running'::text]));


--
-- Name: idx_workflows_parent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workflows_parent ON public.workflows USING btree (parent_id) WHERE (parent_id IS NOT NULL);


--
-- Name: idx_workflows_run_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workflows_run_id ON public.workflows USING btree (run_id);


--
-- Name: idx_workflows_unique_root; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_workflows_unique_root ON public.workflows USING btree (run_id) WHERE (parent_id IS NULL);


--
-- Name: idx_workspaces_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workspaces_org ON public.workspaces USING btree (org_id);


--
-- Name: api_key_environment_scope api_key_environment_scope_api_key_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_key_environment_scope
    ADD CONSTRAINT api_key_environment_scope_api_key_id_fkey FOREIGN KEY (api_key_id) REFERENCES public.api_keys(id) ON DELETE CASCADE;


--
-- Name: api_key_environment_scope api_key_environment_scope_environment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_key_environment_scope
    ADD CONSTRAINT api_key_environment_scope_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES public.environments(id) ON DELETE CASCADE;


--
-- Name: api_key_workspace_scope api_key_workspace_scope_api_key_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_key_workspace_scope
    ADD CONSTRAINT api_key_workspace_scope_api_key_id_fkey FOREIGN KEY (api_key_id) REFERENCES public.api_keys(id) ON DELETE CASCADE;


--
-- Name: api_key_workspace_scope api_key_workspace_scope_workspace_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_key_workspace_scope
    ADD CONSTRAINT api_key_workspace_scope_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;


--
-- Name: api_keys api_keys_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: api_keys api_keys_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id);


--
-- Name: api_keys api_keys_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);


--
-- Name: audit_log audit_log_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: audit_log audit_log_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);


--
-- Name: env_variable_values env_variable_values_environment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.env_variable_values
    ADD CONSTRAINT env_variable_values_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES public.environments(id) ON DELETE CASCADE;


--
-- Name: env_variable_values env_variable_values_variable_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.env_variable_values
    ADD CONSTRAINT env_variable_values_variable_id_fkey FOREIGN KEY (variable_id) REFERENCES public.env_variables(id) ON DELETE CASCADE;


--
-- Name: env_variables env_variables_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.env_variables
    ADD CONSTRAINT env_variables_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: environments environments_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.environments
    ADD CONSTRAINT environments_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: forge_connections forge_connections_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.forge_connections
    ADD CONSTRAINT forge_connections_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: personal_tokens personal_tokens_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.personal_tokens
    ADD CONSTRAINT personal_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: pipeline_modules pipeline_modules_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pipeline_modules
    ADD CONSTRAINT pipeline_modules_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: pipeline_runs pipeline_runs_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pipeline_runs
    ADD CONSTRAINT pipeline_runs_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: pipeline_runs pipeline_runs_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pipeline_runs
    ADD CONSTRAINT pipeline_runs_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: project_favourites project_favourites_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_favourites
    ADD CONSTRAINT project_favourites_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: project_favourites project_favourites_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_favourites
    ADD CONSTRAINT project_favourites_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);


--
-- Name: projects projects_forge_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_forge_id_fkey FOREIGN KEY (forge_id) REFERENCES public.forge_connections(id);


--
-- Name: projects projects_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: projects projects_workspace_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id);


--
-- Name: protected_environments protected_environments_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protected_environments
    ADD CONSTRAINT protected_environments_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: role_assignments role_assignments_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_assignments
    ADD CONSTRAINT role_assignments_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;


--
-- Name: sso_group_role_mappings sso_group_role_mappings_fkeys; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sso_group_role_mappings
    ADD CONSTRAINT sso_group_role_mappings_org_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.sso_group_role_mappings
    ADD CONSTRAINT sso_group_role_mappings_role_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.scim_tokens
    ADD CONSTRAINT scim_tokens_org_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id) ON DELETE CASCADE;


--
-- Name: role_environment_scope role_environment_scope_environment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_environment_scope
    ADD CONSTRAINT role_environment_scope_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES public.environments(id) ON DELETE CASCADE;


--
-- Name: role_environment_scope role_environment_scope_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_environment_scope
    ADD CONSTRAINT role_environment_scope_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;


--
-- Name: role_permissions role_permissions_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;


--
-- Name: role_workspace_scope role_workspace_scope_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_workspace_scope
    ADD CONSTRAINT role_workspace_scope_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;


--
-- Name: role_workspace_scope role_workspace_scope_workspace_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.role_workspace_scope
    ADD CONSTRAINT role_workspace_scope_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;


--
-- Name: roles roles_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: secrets secrets_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secrets
    ADD CONSTRAINT secrets_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: secrets secrets_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secrets
    ADD CONSTRAINT secrets_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: secrets secrets_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secrets
    ADD CONSTRAINT secrets_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: sessions sessions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: signals signals_workflow_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.signals
    ADD CONSTRAINT signals_workflow_id_fkey FOREIGN KEY (workflow_id) REFERENCES public.workflows(id) ON DELETE CASCADE;


--
-- Name: steps steps_workflow_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.steps
    ADD CONSTRAINT steps_workflow_id_fkey FOREIGN KEY (workflow_id) REFERENCES public.workflows(id) ON DELETE CASCADE;

-- NOTE: engine_events intentionally has NO foreign key to workflows. It is an
-- append-only audit sidecar written in the same transaction as the state change
-- it records. A hard FK would take a FOR KEY SHARE lock on the workflow row on
-- every event insert, which deadlocks against advanceWorkflow's FOR UPDATE on the
-- same row under concurrent step completion. workflow_id stays an indexed column;
-- retention is by age (CleanupOldEngineEvents), not cascade.


--
-- Name: team_members team_members_team_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.team_members
    ADD CONSTRAINT team_members_team_id_fkey FOREIGN KEY (team_id) REFERENCES public.teams(id) ON DELETE CASCADE;


--
-- Name: team_members team_members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.team_members
    ADD CONSTRAINT team_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: teams teams_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.teams
    ADD CONSTRAINT teams_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: timers timers_workflow_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.timers
    ADD CONSTRAINT timers_workflow_id_fkey FOREIGN KEY (workflow_id) REFERENCES public.workflows(id) ON DELETE CASCADE;


--
-- Name: users users_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- Name: webhooks webhooks_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhooks
    ADD CONSTRAINT webhooks_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: workflows workflows_parent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflows
    ADD CONSTRAINT workflows_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.workflows(id) ON DELETE CASCADE;


--
-- Name: workflows workflows_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflows
    ADD CONSTRAINT workflows_run_id_fkey FOREIGN KEY (run_id) REFERENCES public.pipeline_runs(id) ON DELETE CASCADE;


--
-- Name: workspaces workspaces_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.orgs(id);


--
-- PostgreSQL database dump complete
--

-- ---------------------------------------------------------------------------
-- Indexes on columns folded into the baseline tables above. Pre-live, column
-- additions live in the owning table's CREATE rather than as ALTER migrations.
-- New *tables* added since the baseline are their own migration files (002+).
-- ---------------------------------------------------------------------------

-- Per-run executor-cleanup tracking (exactly-once teardown).
CREATE INDEX idx_pipeline_runs_needs_cleanup
    ON public.pipeline_runs (id)
    WHERE cleaned_at IS NULL AND status IN ('succeeded', 'failed', 'cancelled');

-- Exactly one default workspace per org (new projects land here).
CREATE UNIQUE INDEX idx_workspaces_one_default_per_org ON public.workspaces (org_id) WHERE is_default;


-- +goose Down
DROP SCHEMA public CASCADE;
CREATE SCHEMA public;
