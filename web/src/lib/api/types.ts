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
  workflowFile: z.string(),
  environment: z.optional(z.string()),
})

export const LastRunSchema = z.object({
  id: z.string(),
  status: RunStatus,
  branch: z.string(),
  duration: z.string(),
  triggeredBy: z.string(),
  startedAt: z.string(),
})

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
})

export const PipelineStepSchema = z.object({
  name: z.string(),
  status: StepStatus,
  execType: z.string(),
  wave: z.number(),
  dependsOn: z.optional(z.array(z.string())),
  startedAt: z.optional(z.string()),
  finishedAt: z.optional(z.string()),
})

export const PipelineDefinitionStepSchema = PipelineStepSchema.omit({ status: true, startedAt: true, finishedAt: true })

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
  role: z.string(),
  permissions: z.array(z.string()),
  groups: z.array(z.string()),
})

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
