import { z } from 'zod'

// ---------------------------------------------------------------------------
// Enums
// ---------------------------------------------------------------------------

export const RunStatus = z.enum([
  'succeeded',
  'failed',
  'running',
  'pending',
  'cancelled',
])
export type RunStatusValue = z.infer<typeof RunStatus>

export const StepStatus = z.enum([
  'pending',
  'queued',
  'running',
  'waiting',
  'succeeded',
  'failed',
  'skipped',
  'cancelled',
])
export type StepStatusValue = z.infer<typeof StepStatus>

// Compact per-step summary carried on a run for the runs feed's stage pips.
export const RunStepSummarySchema = z.object({
  name: z.string(),
  status: StepStatus,
})
export type RunStepSummary = z.infer<typeof RunStepSummarySchema>

export const TriggerType = z.enum([
  'push',
  'pull_request',
  'manual',
  'schedule',
])

// ---------------------------------------------------------------------------
// Core domain schemas
// ---------------------------------------------------------------------------

export const PipelineRunSchema = z.object({
  id: z.string(),
  projectId: z.string(),
  projectName: z.string(),
  projectColour: z.string(),
  repo: z.string(),
  status: RunStatus,
  branch: z.string(),
  commitSha: z.string(),
  commitMessage: z.string(),
  triggeredBy: z.string(),
  triggerType: TriggerType,
  duration: z.string(),
  startedAt: z.string(),
  finishedAt: z.optional(z.string()),
  // Real wall-clock timestamps (epoch ms). Backend should populate; the mock
  // derives them. Optional so other producers stay valid.
  startedAtTs: z.optional(z.number()),
  finishedAtTs: z.optional(z.number()),
  workflowFile: z.string(),
  environment: z.optional(z.string()),
  errorMessage: z.optional(z.string()),
  // Per-step summary for the feed's stage pips (backend should populate this;
  // the mock derives it). Optional so other run producers stay valid.
  steps: z.optional(z.array(RunStepSummarySchema)),
})

export const LastRunSchema = z.object({
  id: z.string(),
  status: RunStatus,
  branch: z.string(),
  duration: z.string(),
  triggeredBy: z.string(),
  startedAt: z.string(),
})

// Rolling health over a recent window of runs, for the projects triage surface.
export const ProjectHealthSchema = z.object({
  recentRuns: z.array(RunStatus), // newest-first, up to ~12
  passRate: z.number(),           // 0..100 over the window
  failingNow: z.boolean(),        // most recent run failed
  totalRuns: z.number(),          // window size
})
export type ProjectHealth = z.infer<typeof ProjectHealthSchema>

export const ProjectSchema = z.object({
  id: z.string(),
  name: z.string(),
  repo: z.string(),
  workspace: z.string(),
  colour: z.string(),
  tags: z.array(z.string()),
  pipelineCount: z.number(),
  pipelineErrors: z.number(),
  lastRun: z.optional(LastRunSchema),
  // true when the workspace was inferred (not declared) — surfaced for triage.
  inferred: z.optional(z.boolean()),
  // Backend should populate this; the mock derives it. Optional for safety.
  health: z.optional(ProjectHealthSchema),
})

export const PipelineStepSchema = z.object({
  name: z.string(),
  status: StepStatus,
  execType: z.string(),
  wave: z.number(),
  attempt: z.number(),
  maxAttempts: z.number(),
  dependsOn: z.optional(z.array(z.string())),
  // When the step became ready and was scheduled onto a runner. The gap
  // between this and `startedAt` is runner-queue / pod cold-start wait.
  scheduledAt: z.optional(z.string()),
  startedAt: z.optional(z.string()),
  finishedAt: z.optional(z.string()),
  exitCode: z.optional(z.number()),
  error: z.optional(z.string()),
})

export const PipelineDefinitionStepSchema = PipelineStepSchema.omit({ status: true, attempt: true, maxAttempts: true, startedAt: true, finishedAt: true, exitCode: true, error: true })

export const PipelineValidationStatus = z.enum(['valid', 'invalid'])

export const DispatchInputSchema = z.object({
  name: z.string(),
  type: z.enum(['string', 'choice', 'boolean']),
  description: z.optional(z.string()),
  required: z.optional(z.boolean()),
  default: z.optional(z.string()),
  options: z.optional(z.array(z.string())),
})

export const PipelineDefinitionSchema = z.object({
  filename: z.string(),
  yaml: z.string(),
  status: PipelineValidationStatus,
  errors: z.optional(z.array(z.string())),
  steps: z.array(PipelineDefinitionStepSchema),
  dispatchInputs: z.optional(z.array(DispatchInputSchema)),
})

export const DashboardSummarySchema = z.object({
  totalRuns: z.number(),
  successRate: z.number(),
  pendingGates: z.number(),
  activeProjects: z.number(),
  runsToday: z.number(),
  avgDuration: z.string(),
})

// ---------------------------------------------------------------------------
// Workflows — generic declarative runs on the shared engine (no forge/repo).
// A run reuses RunStatus and the engine step IR (PipelineStep), so the DAG view
// and step rendering are shared with CI.
// ---------------------------------------------------------------------------

export const WorkflowTriggerType = z.enum(['manual', 'schedule', 'api'])

export const WorkflowRunSchema = z.object({
  id: z.string(),
  name: z.string(),
  status: RunStatus,
  triggerType: WorkflowTriggerType,
  triggeredBy: z.string(),
  startedAt: z.string(),
  finishedAt: z.optional(z.string()),
  duration: z.string(),
  stepCount: z.number(),
})

export const WorkflowRunDetailSchema = WorkflowRunSchema.extend({
  steps: z.array(PipelineStepSchema),
})

export type WorkflowRun = z.infer<typeof WorkflowRunSchema>
export type WorkflowRunDetail = z.infer<typeof WorkflowRunDetailSchema>

export const GateStatus = z.enum(['pending', 'approved', 'rejected'])

export const GateSchema = z.object({
  runId: z.string(),
  stepName: z.string(),
  status: GateStatus,
  message: z.string(),
  projectName: z.string(),
  projectColour: z.string(),
  workspace: z.string(),
  environment: z.string(),
  branch: z.string(),
  triggeredBy: z.string(),
  reviewedBy: z.optional(z.string()),
  reviewedAt: z.optional(z.string()),
  createdAt: z.string(),
})

export const WorkspaceSchema = z.object({
  id: z.string(),
  name: z.string(),
  slug: z.string(),
  description: z.optional(z.string()),
  projectCount: z.number(),
  createdAt: z.string(),
  // The org's default workspace — where new projects land when none is chosen.
  isDefault: z.optional(z.boolean()),
})

// A curated tag key in the registry. Projects carry tags as `key:value`
// strings (or bare free tags); a TagKey governs an allowed namespace —
// optionally constraining values and giving the key a label + color.
export const TagKeySchema = z.object({
  id: z.string(),
  key: z.string(),
  label: z.string(),
  // Empty/absent = free-form values allowed for this key.
  allowedValues: z.array(z.string()),
  color: z.string(),
})

export const UserSchema = z.object({
  id: z.string(),
  email: z.string(),
  name: z.optional(z.string()),
  avatarUrl: z.optional(z.string()),
})

export const TeamSource = z.enum(['idp', 'internal'])

export const TeamSchema = z.object({
  id: z.string(),
  name: z.string(),
  slug: z.string(),
  source: TeamSource,
  idpGroup: z.optional(z.string()),
  memberCount: z.number(),
})

export const TeamWithMembersSchema = TeamSchema.extend({
  members: z.array(UserSchema),
})

export const PermissionSchema = z.object({
  object: z.string(),
  action: z.string(),
})

export const RoleSchema = z.object({
  id: z.string(),
  name: z.string(),
  slug: z.string(),
  description: z.optional(z.string()),
  isSystem: z.boolean(),
  permissions: z.array(PermissionSchema),
  workspaces: z.array(z.string()),
  environments: z.array(z.string()),
})

export const AssignmentSchema = z.object({
  subject: z.string(),
  role: z.string(),
})

export const EnvironmentSchema = z.object({
  id: z.string(),
  name: z.string(),
  slug: z.string(),
  createdAt: z.string(),
})

export const VariableScope = z.enum(['global', 'environment'])

export const EnvVariableSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.optional(z.string()),
  scope: VariableScope,
  isSecret: z.boolean(),
  value: z.optional(z.string()),  // only for global scope — the single value
  createdAt: z.string(),
})

export const EnvVariableValueSchema = z.object({
  variableId: z.string(),
  environmentId: z.string(),
  value: z.string(),
  updatedAt: z.string(),
})

export const ApiKeySchema = z.object({
  id: z.string(),
  name: z.string(),
  role: z.string(),
  workspaces: z.array(z.string()),
  environments: z.array(z.string()),
  expiresAt: z.optional(z.string()),
  lastUsedAt: z.optional(z.string()),
  createdBy: z.string(),
  createdAt: z.string(),
})

export const ApiKeyCreatedSchema = z.object({
  id: z.string(),
  name: z.string(),
  token: z.string(),
  role: z.string(),
  workspaces: z.array(z.string()),
  environments: z.array(z.string()),
  createdBy: z.string(),
  createdAt: z.string(),
})
export type ApiKeyCreated = z.infer<typeof ApiKeyCreatedSchema>

export const PersonalTokenSchema = z.object({
  id: z.string(),
  name: z.string(),
  expiresAt: z.optional(z.string()),
  lastUsedAt: z.optional(z.string()),
  createdAt: z.string(),
})

export const AuditEntrySchema = z.object({
  id: z.string(),
  userId: z.optional(z.string()),
  action: z.string(),
  resourceType: z.string(),
  resourceId: z.optional(z.string()),
  metadata: z.optional(z.record(z.string(), z.unknown())),
  ipAddress: z.optional(z.string()),
  createdAt: z.string(),
})

export const RunnerPoolSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.optional(z.string()),
  cpu: z.string(),
  memory: z.string(),
  arch: z.string(),
  gpuVendor: z.optional(z.string()),
  gpuModel: z.optional(z.string()),
  gpuCount: z.optional(z.number()),
  mode: z.optional(z.string()),
  // The default pool is used when a pipeline sets no runner:. Exactly one is true.
  isDefault: z.optional(z.boolean()),
  // How a reference pool targets existing nodes (managed pools derive these).
  nodeSelector: z.optional(z.record(z.string(), z.string())),
  tolerations: z.optional(z.array(z.object({
    key: z.string(),
    operator: z.optional(z.string()),
    value: z.optional(z.string()),
    effect: z.optional(z.string()),
  }))),
  // Managed capacity envelope — present only for mode === 'managed'. Lets the
  // editor round-trip a managed pool's Karpenter intent.
  managed: z.optional(
    z.object({
      capacityType: z.optional(z.string()),
      instanceFamilies: z.optional(z.array(z.string())),
      cpuLimit: z.optional(z.number()),
      gpuLimit: z.optional(z.number()),
      scaleToZero: z.optional(z.boolean()),
      consolidateAfter: z.optional(z.string()),
      diskGiB: z.optional(z.number()),
      amiFamily: z.optional(z.string()),
    }),
  ),
  ready: z.boolean(),
  createdAt: z.string(),
})

export const ForgeConnectionSchema = z.object({
  id: z.string(),
  forgeType: z.string(),
  displayName: z.string(),
  createdAt: z.string(),
})

export const AuthUserSchema = z.object({
  userId: z.string(),
  email: z.string(),
  name: z.string(),
  avatarUrl: z.optional(z.string()),
  role: z.string(),
  permissions: z.array(z.string()),
  groups: z.array(z.string()),
  provider: z.optional(z.string()),
  // Appearance preferences, persisted server-side against the user.
  themeMode: z.optional(z.string()),
  colorTheme: z.optional(z.string()),
  // Whether TOTP two-factor is enabled for this user.
  mfaEnabled: z.optional(z.boolean()),
})

// An active sign-in session for the current user.
export const SessionSchema = z.object({
  id: z.string(),
  ipAddress: z.optional(z.string()),
  userAgent: z.optional(z.string()),
  createdAt: z.string(),
  lastActivity: z.string(),
  current: z.optional(z.boolean()),
})
export type Session = z.infer<typeof SessionSchema>

// TOTP enrollment payload returned when starting MFA setup.
export const MfaSetupSchema = z.object({
  secret: z.string(),
  qrCodeURL: z.string(),
  recoveryCodes: z.array(z.string()),
})
export type MfaSetup = z.infer<typeof MfaSetupSchema>

// The org and its governance policies.
export const OrgSchema = z.object({
  id: z.string(),
  name: z.string(),
  slug: z.string(),
  concurrencyLimit: z.optional(z.number()),
  requireProjectWorkspace: z.boolean(),
})
export type Org = z.infer<typeof OrgSchema>

// Configured SSO providers (secrets never leave the server).
export const AuthProvidersSchema = z.object({
  providers: z.array(z.object({ id: z.string(), providerType: z.string(), displayName: z.string() })),
  oidcConfigured: z.boolean(),
  samlConfigured: z.boolean(),
})
export type AuthProviders = z.infer<typeof AuthProvidersSchema>

// SSO provider configuration. A single generic shape carries both OIDC and SAML
// fields plus the configurable claim/attribute mapping; the UI presets merely
// pre-fill these. Mirrors the Go auth.ProviderConfig struct (JSON tags).
export const providerConfigSchema = z.object({
  // OIDC
  issuerUrl: z.optional(z.string()),
  clientId: z.optional(z.string()),
  clientSecret: z.optional(z.string()),
  scopes: z.optional(z.array(z.string())),
  emailClaim: z.optional(z.string()),
  nameClaim: z.optional(z.string()),
  groupsClaim: z.optional(z.string()),
  // SAML
  metadataUrl: z.optional(z.string()),
  metadataXml: z.optional(z.string()),
  entityId: z.optional(z.string()),
  nameIdFormat: z.optional(z.string()),
  emailAttributes: z.optional(z.array(z.string())),
  nameAttributes: z.optional(z.array(z.string())),
  groupsAttributes: z.optional(z.array(z.string())),
  spCertPem: z.optional(z.string()),
  spKeyPem: z.optional(z.string()),
})
export type ProviderConfig = z.infer<typeof providerConfigSchema>

// IdP group → role mappings + strict (deny-by-default) flag.
export interface GroupRoleMapping {
  groupName: string
  roleId: string
  roleSlug?: string
  roleName?: string
}
export interface GroupMappings {
  mappings: GroupRoleMapping[]
  strict: boolean
}

// Decoded test sign-in (B1): a real login round-trip that captures the exact
// claims/assertion the IdP emits.
export interface TestLoginStart {
  testId: string
  authUrl: string
}
export interface TestLoginResult {
  status: 'pending' | 'complete' | 'error'
  error?: string
  result?: {
    email: string
    name: string
    groups: string[]
    raw: Record<string, unknown>
  }
}

// SCIM provisioning status.
export interface ScimStatus {
  configured: boolean
  baseUrl: string
}

// Result of POST /auth/provider/test — a non-persisting connection check.
export interface ProviderTestResult {
  ok: boolean
  error?: string
  oidc?: {
    issuer: string
    authorizationEndpoint: string
    tokenEndpoint: string
    userinfoEndpoint?: string
    scopesSupported?: string[]
    claimsSupported?: string[]
  }
  saml?: {
    idpEntityId: string
    ssoUrl?: string
  }
}

export const SearchResultSchema = z.object({
  projects: z.array(ProjectSchema),
  runs: z.array(PipelineRunSchema),
})
export type SearchResult = z.infer<typeof SearchResultSchema>

// A saved view is a named navigation target — a route plus its URL filters
// (selector). Smart views are built-in client-side versions of the same.
export const SavedViewSchema = z.object({
  id: z.string(),
  name: z.string(),
  route: z.string(),
  search: z.record(z.string(), z.unknown()),
})
export type SavedView = z.infer<typeof SavedViewSchema>

// ---------------------------------------------------------------------------
// Inferred TypeScript types
// ---------------------------------------------------------------------------

export type PipelineRun = z.infer<typeof PipelineRunSchema>
export type LastRun = z.infer<typeof LastRunSchema>
export type Project = z.infer<typeof ProjectSchema>
export type PipelineStep = z.infer<typeof PipelineStepSchema>
export type PipelineDefinition = z.infer<typeof PipelineDefinitionSchema>
export type DashboardSummary = z.infer<typeof DashboardSummarySchema>
export type Gate = z.infer<typeof GateSchema>
export type Workspace = z.infer<typeof WorkspaceSchema>
export type TagKey = z.infer<typeof TagKeySchema>
export type User = z.infer<typeof UserSchema>
export type Team = z.infer<typeof TeamSchema>
export type TeamWithMembers = z.infer<typeof TeamWithMembersSchema>
export type Permission = z.infer<typeof PermissionSchema>
export type Role = z.infer<typeof RoleSchema>
export type Assignment = z.infer<typeof AssignmentSchema>
export type Environment = z.infer<typeof EnvironmentSchema>
export type EnvVariable = z.infer<typeof EnvVariableSchema>
export type EnvVariableValue = z.infer<typeof EnvVariableValueSchema>
export type ApiKey = z.infer<typeof ApiKeySchema>
export type PersonalToken = z.infer<typeof PersonalTokenSchema>
export type AuditEntry = z.infer<typeof AuditEntrySchema>
export type RunnerPool = z.infer<typeof RunnerPoolSchema>
export type ForgeConnection = z.infer<typeof ForgeConnectionSchema>
export type AuthUser = z.infer<typeof AuthUserSchema>
