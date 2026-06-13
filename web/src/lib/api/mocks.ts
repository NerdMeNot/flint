import type {
  PipelineRun,
  Project,
  PipelineStep,
  PipelineDefinition as PipelineDef,
  DashboardSummary,
  Gate,
  Workspace,
  TagKey,
  Team,
  TeamWithMembers,
  User,
  Role,
  Assignment,
  Environment,
  EnvVariable,
  EnvVariableValue,
  ApiKey,
  AuditEntry,
  RunnerPool,
  ForgeConnection,
  AuthUser,
  Session,
  MfaSetup,
  AuthProviders,
  PersonalToken,
  WorkflowRun,
  WorkflowRunDetail,
} from './types'

// ---------------------------------------------------------------------------
// Users (shared across teams, audit log, etc.)
// ---------------------------------------------------------------------------

const users: User[] = [
  { id: 'u-1', email: 'alice@acme.dev', name: 'Alice Chen', avatarUrl: 'https://i.pravatar.cc/150?u=alice' },
  { id: 'u-2', email: 'bob@acme.dev', name: 'Bob Patel', avatarUrl: 'https://i.pravatar.cc/150?u=bob' },
  { id: 'u-3', email: 'carol@acme.dev', name: 'Carol Reyes', avatarUrl: 'https://i.pravatar.cc/150?u=carol' },
  { id: 'u-4', email: 'dave@acme.dev', name: 'Dave Kim', avatarUrl: 'https://i.pravatar.cc/150?u=dave' },
  { id: 'u-5', email: 'eve@acme.dev', name: 'Eve Nakamura', avatarUrl: 'https://i.pravatar.cc/150?u=eve' },
  { id: 'u-6', email: 'frank@acme.dev', name: 'Frank Osei', avatarUrl: 'https://i.pravatar.cc/150?u=frank' },
  { id: 'u-7', email: 'grace@acme.dev', name: 'Grace Liu', avatarUrl: 'https://i.pravatar.cc/150?u=grace' },
  { id: 'u-8', email: 'hiro@acme.dev', name: 'Hiro Tanaka', avatarUrl: 'https://i.pravatar.cc/150?u=hiro' },
  { id: 'u-9', email: 'iris@acme.dev', name: 'Iris Johansson', avatarUrl: 'https://i.pravatar.cc/150?u=iris' },
  { id: 'u-10', email: 'jake@acme.dev', name: 'Jake Morales', avatarUrl: 'https://i.pravatar.cc/150?u=jake' },
]

export function getUsers(): User[] {
  return users
}

// ---------------------------------------------------------------------------
// Projects (6 projects, 3 workspaces)
// ---------------------------------------------------------------------------

export function getProjects(): Project[] {
  return [
    {
      id: 'p-1',
      name: 'Checkout Service',
      repo: 'acme/checkout-service',
      workspace: 'payments',
      lastRun: {
        id: 'r-101',
        status: 'succeeded',
        branch: 'main',
        duration: '2m 34s',
        triggeredBy: 'alice',
        startedAt: '3 min ago',
      },
      tags: ['backend', 'critical', 'domain:checkout', 'tier:1', 'lang:go'],
      pipelineCount: 2,
      pipelineErrors: 0,
      colour: '#3b82f6',
    },
    {
      id: 'p-2',
      name: 'Payment Gateway',
      repo: 'acme/payment-gateway',
      workspace: 'payments',
      lastRun: {
        id: 'r-102',
        status: 'failed',
        branch: 'feat/stripe-v3',
        duration: '1m 12s',
        triggeredBy: 'bob',
        startedAt: '8 min ago',
      },
      tags: ['backend', 'pci', 'domain:checkout', 'tier:1', 'lang:go', 'compliance:pci'],
      pipelineCount: 1,
      pipelineErrors: 0,
      colour: '#ef4444',
    },
    {
      id: 'p-3',
      name: 'Product Search',
      repo: 'acme/product-search',
      workspace: 'catalog',
      lastRun: {
        id: 'r-103',
        status: 'running',
        branch: 'main',
        duration: '1m 45s',
        triggeredBy: 'carol',
        startedAt: '1 min ago',
      },
      tags: ['backend', 'elasticsearch', 'domain:catalog', 'tier:2', 'lang:go'],
      pipelineCount: 2,
      pipelineErrors: 1,
      colour: '#8b5cf6',
    },
    {
      id: 'p-4',
      name: 'Terraform Infra',
      repo: 'acme/terraform-infra',
      workspace: 'platform',
      lastRun: {
        id: 'r-104',
        status: 'succeeded',
        branch: 'main',
        duration: '4m 02s',
        triggeredBy: 'dave',
        startedAt: '22 min ago',
      },
      tags: ['iac', 'domain:platform', 'tier:2', 'lang:go'],
      pipelineCount: 2,
      pipelineErrors: 0,
      colour: '#22c55e',
    },
    {
      id: 'p-5',
      name: 'Auth Service',
      repo: 'acme/auth-service',
      workspace: 'platform',
      lastRun: {
        id: 'r-105',
        status: 'succeeded',
        branch: 'release/2.1',
        duration: '3m 18s',
        triggeredBy: 'eve',
        startedAt: '1 hour ago',
      },
      tags: ['backend', 'security', 'domain:identity', 'tier:1', 'lang:go'],
      pipelineCount: 1,
      pipelineErrors: 0,
      colour: '#f59e0b',
    },
    {
      id: 'p-6',
      name: 'Web Storefront',
      repo: 'acme/web-storefront',
      workspace: 'storefront',
      tags: ['frontend', 'nextjs', 'domain:catalog', 'tier:2', 'lang:ts'],
      pipelineCount: 1,
      pipelineErrors: 0,
      colour: '#06b6d4',
    },
  ]
}

// ---------------------------------------------------------------------------
// Pipeline runs (20 runs with realistic variety)
// ---------------------------------------------------------------------------

const projectEnvironment: Record<string, string> = {
  'p-1': 'production',
  'p-2': 'production',
  'p-3': 'staging',
  'p-4': 'production',
  'p-5': 'production',
  'p-6': 'dev',
}

function seedRuns(): PipelineRun[] {
  return [
    {
      // Designed test-bed for the gate panel: a production deploy that
      // has cleared all upstream checks and is currently parked at the
      // approve-production gate. Use this run id when poking at gate UX.
      id: 'r-gate-1',
      projectId: 'p-5',
      projectName: 'Auth Service',
      projectColour: '#f59e0b',
      repo: 'acme/auth-service',
      status: 'running',
      branch: 'main',
      commitSha: 'a3b4c5d',
      commitMessage: 'fix: payment retry on 5xx responses',
      triggeredBy: 'alice',
      triggerType: 'push',
      duration: '4m 12s',
      startedAt: '18 min ago',
      workflowFile: 'deploy.yaml',
    },
    {
      id: 'r-101',
      projectId: 'p-1',
      projectName: 'Checkout Service',
      projectColour: '#3b82f6',
      repo: 'acme/api-gateway',
      status: 'succeeded',
      branch: 'main',
      commitSha: 'a3f8c21',
      commitMessage: 'fix: rate limiter race condition on high concurrency',
      triggeredBy: 'alice',
      triggerType: 'push',
      duration: '2m 34s',
      startedAt: '3 min ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-102',
      projectId: 'p-2',
      projectName: 'Payment Gateway',
      projectColour: '#ef4444',
      repo: 'acme/payment-service',
      status: 'failed',
      branch: 'feat/stripe-v3',
      commitSha: 'b7e4d09',
      commitMessage: 'feat: migrate to Stripe API v3 webhooks',
      triggeredBy: 'bob',
      triggerType: 'push',
      duration: '1m 12s',
      startedAt: '8 min ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-103',
      projectId: 'p-3',
      projectName: 'Product Search',
      projectColour: '#8b5cf6',
      repo: 'acme/web-dashboard',
      status: 'running',
      branch: 'main',
      commitSha: 'c9d1e33',
      commitMessage: 'chore: upgrade TanStack Router to v1.168',
      triggeredBy: 'carol',
      triggerType: 'push',
      duration: '1m 45s',
      startedAt: '1 min ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-104',
      projectId: 'p-4',
      projectName: 'Terraform Infra',
      projectColour: '#22c55e',
      repo: 'acme/terraform-infra',
      status: 'succeeded',
      branch: 'main',
      commitSha: 'de82f71',
      commitMessage: 'infra: add Redis cluster for session store',
      triggeredBy: 'dave',
      triggerType: 'pull_request',
      duration: '4m 02s',
      startedAt: '22 min ago',
      workflowFile: 'plan.yaml',
    },
    {
      id: 'r-105',
      projectId: 'p-5',
      projectName: 'Auth Service',
      projectColour: '#f59e0b',
      repo: 'acme/auth-service',
      status: 'succeeded',
      branch: 'release/2.1',
      commitSha: 'ef23a98',
      commitMessage: 'release: v2.1.0 with SAML SP support',
      triggeredBy: 'eve',
      triggerType: 'push',
      duration: '3m 18s',
      startedAt: '1 hour ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-106',
      projectId: 'p-1',
      projectName: 'Checkout Service',
      projectColour: '#3b82f6',
      repo: 'acme/api-gateway',
      status: 'cancelled',
      branch: 'feat/grpc-gateway',
      commitSha: 'f1a2b3c',
      commitMessage: 'wip: gRPC gateway proxy layer',
      triggeredBy: 'alice',
      triggerType: 'push',
      duration: '0m 28s',
      startedAt: '2 hours ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-107',
      projectId: 'p-2',
      projectName: 'Payment Gateway',
      projectColour: '#ef4444',
      repo: 'acme/payment-service',
      status: 'succeeded',
      branch: 'main',
      commitSha: 'd4c7e82',
      commitMessage: 'fix: idempotency key collision on retry',
      triggeredBy: 'bob',
      triggerType: 'push',
      duration: '2m 51s',
      startedAt: '3 hours ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-108',
      projectId: 'p-3',
      projectName: 'Product Search',
      projectColour: '#8b5cf6',
      repo: 'acme/web-dashboard',
      status: 'succeeded',
      branch: 'feat/dark-mode',
      commitSha: 'e5f9a12',
      commitMessage: 'feat: dark mode with system preference detection',
      triggeredBy: 'carol',
      triggerType: 'pull_request',
      duration: '1m 58s',
      startedAt: '4 hours ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-109',
      projectId: 'p-4',
      projectName: 'Terraform Infra',
      projectColour: '#22c55e',
      repo: 'acme/terraform-infra',
      status: 'succeeded',
      branch: 'feat/aurora-upgrade',
      commitSha: 'f6a0b34',
      commitMessage: 'infra: upgrade Aurora PostgreSQL to 16.2',
      triggeredBy: 'dave',
      triggerType: 'pull_request',
      duration: '5m 12s',
      startedAt: '5 hours ago',
      workflowFile: 'plan.yaml',
    },
    {
      id: 'r-110',
      projectId: 'p-5',
      projectName: 'Auth Service',
      projectColour: '#f59e0b',
      repo: 'acme/auth-service',
      status: 'failed',
      branch: 'feat/webauthn',
      commitSha: 'a7b1c45',
      commitMessage: 'feat: WebAuthn passkey registration flow',
      triggeredBy: 'eve',
      triggerType: 'push',
      duration: '2m 04s',
      startedAt: '6 hours ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-111',
      projectId: 'p-6',
      projectName: 'Web Storefront',
      projectColour: '#06b6d4',
      repo: 'acme/helm-charts',
      status: 'succeeded',
      branch: 'main',
      commitSha: 'b8c2d56',
      commitMessage: 'chore: bump ingress-nginx chart to 4.10.0',
      triggeredBy: 'frank',
      triggerType: 'push',
      duration: '0m 42s',
      startedAt: '7 hours ago',
      workflowFile: 'lint.yaml',
    },
    {
      id: 'r-112',
      projectId: 'p-1',
      projectName: 'Checkout Service',
      projectColour: '#3b82f6',
      repo: 'acme/api-gateway',
      status: 'succeeded',
      branch: 'main',
      commitSha: 'c9d3e67',
      commitMessage: 'perf: connection pool tuning for 10k concurrent',
      triggeredBy: 'alice',
      triggerType: 'push',
      duration: '2m 48s',
      startedAt: '8 hours ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-113',
      projectId: 'p-2',
      projectName: 'Payment Gateway',
      projectColour: '#ef4444',
      repo: 'acme/payment-service',
      status: 'pending',
      branch: 'feat/apple-pay',
      commitSha: 'd0e4f78',
      commitMessage: 'feat: Apple Pay integration with merchant validation',
      triggeredBy: 'grace',
      triggerType: 'pull_request',
      duration: '0m 00s',
      startedAt: '9 hours ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-114',
      projectId: 'p-3',
      projectName: 'Product Search',
      projectColour: '#8b5cf6',
      repo: 'acme/web-dashboard',
      status: 'failed',
      branch: 'feat/a11y-audit',
      commitSha: 'e1f5a89',
      commitMessage: 'fix: ARIA roles for pipeline DAG visualization',
      triggeredBy: 'hiro',
      triggerType: 'push',
      duration: '1m 33s',
      startedAt: '10 hours ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-115',
      projectId: 'p-4',
      projectName: 'Terraform Infra',
      projectColour: '#22c55e',
      repo: 'acme/terraform-infra',
      status: 'succeeded',
      branch: 'main',
      commitSha: 'f2a6b90',
      commitMessage: 'infra: enable CloudTrail for audit logging',
      triggeredBy: 'dave',
      triggerType: 'push',
      duration: '3m 45s',
      startedAt: '12 hours ago',
      workflowFile: 'plan.yaml',
    },
    {
      id: 'r-116',
      projectId: 'p-5',
      projectName: 'Auth Service',
      projectColour: '#f59e0b',
      repo: 'acme/auth-service',
      status: 'succeeded',
      branch: 'main',
      commitSha: 'a3b7c01',
      commitMessage: 'fix: token refresh race when multiple tabs open',
      triggeredBy: 'eve',
      triggerType: 'push',
      duration: '3m 02s',
      startedAt: '14 hours ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-117',
      projectId: 'p-1',
      projectName: 'Checkout Service',
      projectColour: '#3b82f6',
      repo: 'acme/api-gateway',
      status: 'succeeded',
      branch: 'fix/cors-headers',
      commitSha: 'b4c8d12',
      commitMessage: 'fix: CORS preflight headers missing on 204 responses',
      triggeredBy: 'frank',
      triggerType: 'pull_request',
      duration: '2m 22s',
      startedAt: '1 day ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-118',
      projectId: 'p-6',
      projectName: 'Web Storefront',
      projectColour: '#06b6d4',
      repo: 'acme/helm-charts',
      status: 'succeeded',
      branch: 'feat/cert-manager',
      commitSha: 'c5d9e23',
      commitMessage: 'feat: add cert-manager chart with Let\'s Encrypt',
      triggeredBy: 'dave',
      triggerType: 'pull_request',
      duration: '0m 38s',
      startedAt: '1 day ago',
      workflowFile: 'lint.yaml',
    },
    {
      id: 'r-119',
      projectId: 'p-2',
      projectName: 'Payment Gateway',
      projectColour: '#ef4444',
      repo: 'acme/payment-service',
      status: 'succeeded',
      branch: 'main',
      commitSha: 'd6e0f34',
      commitMessage: 'refactor: extract webhook handler into separate package',
      triggeredBy: 'bob',
      triggerType: 'push',
      duration: '2m 38s',
      startedAt: '2 days ago',
      workflowFile: 'ci.yaml',
    },
    {
      id: 'r-120',
      projectId: 'p-3',
      projectName: 'Product Search',
      projectColour: '#8b5cf6',
      repo: 'acme/web-dashboard',
      status: 'running',
      branch: 'feat/notifications',
      commitSha: 'e7f1a45',
      commitMessage: 'feat: real-time build notifications via SSE',
      triggeredBy: 'iris',
      triggerType: 'push',
      duration: '0m 52s',
      startedAt: '30 sec ago',
      workflowFile: 'ci.yaml',
    },
  ].map((r) => ({ ...r, environment: projectEnvironment[r.projectId] }) as PipelineRun)
}

// Mutable, lazily-seeded run store. In mock mode the router's writes mutate this
// so the UI reflects triggers/cancels/retries without a backend.
let _runs: PipelineRun[] | undefined
export function getRuns(): PipelineRun[] {
  return (_runs ??= seedRuns())
}

let _mockRunSeq = 0
function nextRunId(): string {
  _mockRunSeq += 1
  return `r-mock-${_mockRunSeq}`
}

// triggerRun creates a new 'pending' run for a project and prepends it.
export function triggerRun(input: {
  projectId: string
  branch?: string
  workflowFile?: string
  environment?: string
}): PipelineRun {
  const runs = getRuns()
  const template = runs.find((r) => r.projectId === input.projectId)
  const run: PipelineRun = {
    id: nextRunId(),
    projectId: input.projectId,
    projectName: template?.projectName ?? input.projectId,
    projectColour: template?.projectColour ?? '#6366f1',
    repo: template?.repo ?? 'acme/repo',
    status: 'pending',
    branch: input.branch ?? 'main',
    commitSha: Math.random().toString(16).slice(2, 9),
    commitMessage: 'manual trigger',
    triggeredBy: 'you',
    triggerType: 'manual',
    duration: '—',
    startedAt: 'just now',
    workflowFile: input.workflowFile ?? template?.workflowFile ?? 'ci.yaml',
    environment: input.environment ?? projectEnvironment[input.projectId],
  }
  runs.unshift(run)
  return run
}

// cancelRun marks a run cancelled. Returns the updated run (or undefined).
export function cancelRun(runId: string): PipelineRun | undefined {
  const run = getRuns().find((r) => r.id === runId)
  if (run) run.status = 'cancelled'
  return run
}

// retryRun creates a fresh 'pending' run cloned from an existing one.
export function retryRun(runId: string): PipelineRun {
  const src = getRuns().find((r) => r.id === runId)
  const run: PipelineRun = {
    ...(src ?? (getRuns()[0] as PipelineRun)),
    id: nextRunId(),
    status: 'pending',
    duration: '—',
    startedAt: 'just now',
    finishedAt: undefined,
    triggeredBy: 'you',
    triggerType: 'manual',
  }
  getRuns().unshift(run)
  return run
}

// ---------------------------------------------------------------------------
// Pipeline steps (11-step pipeline for default run)
// ---------------------------------------------------------------------------

export function getSteps(runId?: string): PipelineStep[] {
  if (runId === 'r-gate-1') return getGatePausedSteps()
  return [
    {
      name: 'install-deps',
      status: 'succeeded',
      execType: 'run',
      wave: 0,
      attempt: 1,
      maxAttempts: 1,
      startedAt: '2024-01-15T10:00:00Z',
      finishedAt: '2024-01-15T10:00:27Z',
    },
    {
      name: 'lint',
      status: 'succeeded',
      execType: 'run',
      wave: 1,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['install-deps'],
      startedAt: '2024-01-15T10:00:28Z',
      finishedAt: '2024-01-15T10:00:48Z',
    },
    {
      name: 'unit-tests',
      status: 'succeeded',
      execType: 'run',
      wave: 1,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['install-deps'],
      startedAt: '2024-01-15T10:00:28Z',
      finishedAt: '2024-01-15T10:01:15Z',
    },
    {
      name: 'integration-tests',
      status: 'running',
      execType: 'run',
      wave: 1,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['install-deps'],
      startedAt: '2024-01-15T10:00:28Z',
    },
    {
      name: 'build',
      status: 'pending',
      execType: 'run',
      wave: 2,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['lint', 'unit-tests', 'integration-tests'],
    },
    {
      name: 'docker-push',
      status: 'pending',
      execType: 'run',
      wave: 3,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['build'],
    },
    {
      name: 'deploy-staging',
      status: 'pending',
      execType: 'run',
      wave: 4,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['docker-push'],
    },
    {
      name: 'smoke-tests',
      status: 'pending',
      execType: 'run',
      wave: 5,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['deploy-staging'],
    },
    {
      name: 'approve-production',
      status: 'pending',
      execType: 'gate',
      wave: 6,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['smoke-tests'],
    },
    {
      name: 'deploy-prod',
      status: 'pending',
      execType: 'run',
      wave: 7,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['approve-production'],
    },
  ]
}

/**
 * Step list for the dedicated gate-test run (r-gate-1). All upstream
 * stages have completed; the production-approval gate is currently
 * waiting on a human, and the post-gate deploy is pending.
 */
function getGatePausedSteps(): PipelineStep[] {
  return [
    {
      name: 'install-deps',
      status: 'succeeded',
      execType: 'run',
      wave: 0,
      attempt: 1,
      maxAttempts: 1,
      startedAt: '2024-01-15T10:00:00Z',
      finishedAt: '2024-01-15T10:00:24Z',
    },
    {
      name: 'lint',
      status: 'succeeded',
      execType: 'run',
      wave: 1,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['install-deps'],
      startedAt: '2024-01-15T10:00:25Z',
      finishedAt: '2024-01-15T10:00:42Z',
    },
    {
      name: 'unit-tests',
      status: 'succeeded',
      execType: 'run',
      wave: 1,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['install-deps'],
      startedAt: '2024-01-15T10:00:25Z',
      finishedAt: '2024-01-15T10:01:08Z',
    },
    {
      name: 'integration-tests',
      status: 'succeeded',
      execType: 'run',
      wave: 1,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['install-deps'],
      startedAt: '2024-01-15T10:00:25Z',
      finishedAt: '2024-01-15T10:01:42Z',
    },
    {
      name: 'build',
      status: 'succeeded',
      execType: 'run',
      wave: 2,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['lint', 'unit-tests', 'integration-tests'],
      startedAt: '2024-01-15T10:01:43Z',
      finishedAt: '2024-01-15T10:02:31Z',
    },
    {
      name: 'docker-push',
      status: 'succeeded',
      execType: 'run',
      wave: 3,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['build'],
      startedAt: '2024-01-15T10:02:32Z',
      finishedAt: '2024-01-15T10:03:10Z',
    },
    {
      name: 'deploy-staging',
      status: 'succeeded',
      execType: 'run',
      wave: 4,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['docker-push'],
      startedAt: '2024-01-15T10:03:11Z',
      finishedAt: '2024-01-15T10:03:48Z',
    },
    {
      name: 'smoke-tests',
      status: 'succeeded',
      execType: 'run',
      wave: 5,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['deploy-staging'],
      startedAt: '2024-01-15T10:03:49Z',
      finishedAt: '2024-01-15T10:04:12Z',
    },
    {
      // The gate the run is currently parked at — startedAt set so the
      // rail/Gantt see it as actively waiting (clickable, animatable),
      // no finishedAt yet because the human hasn't decided.
      name: 'approve-production',
      status: 'waiting',
      execType: 'gate',
      wave: 6,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['smoke-tests'],
      startedAt: '2024-01-15T10:04:13Z',
    },
    {
      name: 'deploy-prod',
      status: 'pending',
      execType: 'run',
      wave: 7,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['approve-production'],
    },
    {
      name: 'post-deploy-smoke',
      status: 'pending',
      execType: 'run',
      wave: 8,
      attempt: 1,
      maxAttempts: 1,
      dependsOn: ['deploy-prod'],
    },
  ]
}

// ---------------------------------------------------------------------------
// Step logs
// ---------------------------------------------------------------------------

export function getStepLogs(): Record<string, string> {
  return {
    'install-deps': `[10:00:00] Running: go mod download
[10:00:12] go: downloading github.com/gin-gonic/gin v1.9.1
[10:00:18] go: downloading github.com/redis/go-redis/v9 v9.5.1
[10:00:24] go: downloading google.golang.org/grpc v1.62.0
[10:00:30] Dependencies installed (42 packages, 25.3s)
[10:00:32] Cache saved: go-mod-cache (128MB)`,

    lint: `[10:00:32] Running: golangci-lint run ./...
[10:00:38] internal/handler/ratelimit.go:42: SA1019 deprecated: use sync/atomic
[10:00:42] Fixed 1 issue
[10:00:45] Running: go vet ./...
[10:00:48] Lint passed (16.2s)`,

    'unit-tests': `[10:00:32] Running: go test -race -count=1 ./...
[10:00:45] ok  	acme/api-gateway/internal/handler   0.8s
[10:00:52] ok  	acme/api-gateway/internal/middleware 1.2s
[10:01:01] ok  	acme/api-gateway/internal/ratelimit  2.1s
[10:01:08] ok  	acme/api-gateway/pkg/auth           0.4s
[10:01:15] PASS (43.1s) -- 142 tests, 0 failures`,

    'integration-tests': `[10:00:32] Starting postgres:16-alpine...
[10:00:38] Starting redis:7-alpine...
[10:00:42] Waiting for services to be healthy...
[10:00:48] Running: go test -tags=integration ./...
[10:01:02] ok  	acme/api-gateway/tests/integration  14.2s
[10:01:15] Running TestRateLimiter_Concurrent...`,

    build: `[10:01:20] Running: go build -ldflags="-s -w" -o bin/api-gateway ./cmd/server
[10:01:28] Compiling internal/handler...
[10:01:35] Compiling internal/middleware...
[10:01:42] Compiling internal/ratelimit...
[10:01:48] Compiling cmd/server...
[10:01:52] Build succeeded: bin/api-gateway (18.4MB)
[10:01:53] Build completed in 33.1s`,

    'docker-push': `[10:01:53] Building Docker image acme/api-gateway:a3f8c21...
[10:02:01] Step 1/8: FROM golang:1.22-alpine AS builder
[10:02:08] Step 5/8: COPY --from=builder /app/bin/api-gateway /usr/local/bin/
[10:02:12] Step 8/8: ENTRYPOINT ["/usr/local/bin/api-gateway"]
[10:02:15] Image built: acme/api-gateway:a3f8c21 (42.1MB)
[10:02:22] Pushing to registry.acme.dev/api-gateway:a3f8c21
[10:02:30] Push complete: sha256:e4d8f2a...`,
  }
}

// ---------------------------------------------------------------------------
// Pipeline definitions (per-project YAML + parsed steps)
// ---------------------------------------------------------------------------

const projectPipelines: Record<string, PipelineDef[]> = {
  'p-1': [{
    filename: 'ci.yaml',
    status: 'valid',
    yaml: `# API Gateway — CI/CD Pipeline
image: golang:1.23-alpine

triggers:
  push:
    branches: [main, "release/**"]
  pull_request:
    branches: [main]
  manual:
    environments: [staging, production]
    inputs:
      - name: environment
        type: choice
        options: [staging, production]
        default: staging
      - name: skip_tests
        type: boolean
        description: Skip test steps for hotfixes
        default: "false"

steps:
  - name: install-deps
    run: go mod download
    cache:
      key: go-mod-\${{ hashFiles('go.sum') }}
      paths: [/go/pkg/mod]

  - name: lint
    dependsOn: [install-deps]
    run: golangci-lint run --timeout 5m ./...

  - name: unit-tests
    dependsOn: [install-deps]
    run: go test -race -count=1 -coverprofile=coverage.out ./...
    if: \${{ inputs.skip_tests != 'true' }}

  - name: integration-tests
    dependsOn: [install-deps]
    run: go test -tags=integration -timeout 10m ./...
    if: \${{ inputs.skip_tests != 'true' }}
    services:
      - name: postgres
        image: postgres:16-alpine
        env:
          POSTGRES_DB: testdb
          POSTGRES_PASSWORD: test
      - name: redis
        image: redis:7-alpine

  - name: build
    dependsOn: [lint, unit-tests, integration-tests]
    run: |
      CGO_ENABLED=0 go build -trimpath \\
        -ldflags="-s -w -X main.version=\${{ shortSha }}" \\
        -o bin/api-gateway ./cmd/server
    continueOnError: false

  - name: docker-push
    dependsOn: [build]
    image: docker:24
    run: |
      docker build -t ghcr.io/acme/api-gateway:\${{ commitSha }} .
      docker push ghcr.io/acme/api-gateway:\${{ commitSha }}
    secrets:
      REGISTRY_TOKEN: ghcr-push-token

  - name: deploy-staging
    dependsOn: [docker-push]
    environments: [staging]
    run: |
      helm upgrade --install api-gateway ./charts/api-gateway \\
        --namespace api --set image.tag=\${{ commitSha }}

  - name: smoke-tests
    dependsOn: [deploy-staging]
    run: ./scripts/smoke-test.sh --env staging --timeout 120s
    timeout: 5m
    retry:
      attempts: 2
      delay: 30s

  - name: approve-production
    dependsOn: [smoke-tests]
    gate:
      approvers:
        - team:platform
        - user:alice@acme.dev
      minApprovals: 1

  - name: deploy-prod
    dependsOn: [approve-production]
    environments: [production]
    run: |
      helm upgrade --install api-gateway ./charts/api-gateway \\
        --namespace api --set image.tag=\${{ commitSha }} --set env=prod`,
    steps: [
      { name: 'install-deps', execType: 'run', wave: 0 },
      { name: 'lint', execType: 'run', wave: 1, dependsOn: ['install-deps'] },
      { name: 'unit-tests', execType: 'run', wave: 1, dependsOn: ['install-deps'] },
      { name: 'integration-tests', execType: 'run', wave: 1, dependsOn: ['install-deps'] },
      { name: 'build', execType: 'run', wave: 2, dependsOn: ['lint', 'unit-tests', 'integration-tests'] },
      { name: 'docker-push', execType: 'run', wave: 3, dependsOn: ['build'] },
      { name: 'deploy-staging', execType: 'run', wave: 4, dependsOn: ['docker-push'] },
      { name: 'smoke-tests', execType: 'run', wave: 5, dependsOn: ['deploy-staging'] },
      { name: 'approve-production', execType: 'gate', wave: 6, dependsOn: ['smoke-tests'] },
      { name: 'deploy-prod', execType: 'run', wave: 7, dependsOn: ['approve-production'] },
    ],
    dispatchInputs: [
      { name: 'environment', type: 'choice', description: 'Target environment', required: true, default: 'staging', options: ['dev', 'staging', 'production'] },
      { name: 'skip_tests', type: 'boolean', description: 'Skip test steps', default: 'false' },
      { name: 'ref', type: 'string', description: 'Git ref to build', default: 'main' },
    ],
  }, {
    filename: 'nightly.yaml',
    status: 'valid',
    yaml: `# Nightly Performance Benchmarks
image: golang:1.23-alpine

triggers:
  schedule:
    cron: "0 3 * * *"

steps:
  - name: build
    run: go build -o bin/api-gateway ./cmd/server

  - name: load-test
    dependsOn: [build]
    image: grafana/k6:latest
    run: k6 run --vus 50 --duration 10m scripts/load-test.js
    timeout: 30m

  - name: report
    dependsOn: [load-test]
    run: ./scripts/publish-perf-report.sh
    when: always`,
    steps: [
      { name: 'build', execType: 'run', wave: 0 },
      { name: 'load-test', execType: 'run', wave: 1, dependsOn: ['build'] },
      { name: 'report', execType: 'run', wave: 2, dependsOn: ['load-test'] },
    ],
  }],
  'p-2': [{
    filename: 'ci.yaml',
    status: 'valid',
    yaml: `# Payment Service — PCI-Compliant Pipeline
image: golang:1.23-alpine

triggers:
  push:
    branches: [main]
  pull_request:
    branches: [main]

steps:
  - name: install-deps
    run: go mod download

  - name: lint
    dependsOn: [install-deps]
    run: golangci-lint run ./...

  - name: unit-tests
    dependsOn: [install-deps]
    run: go test -race ./...

  - name: security-scan
    dependsOn: [install-deps]
    image: securego/gosec:latest
    run: gosec -fmt json -out report.json ./...

  - name: build
    dependsOn: [lint, unit-tests, security-scan]
    run: go build -o bin/payment-service ./cmd/server

  - name: docker-push
    dependsOn: [build]
    image: docker:24
    run: |
      docker build -t ghcr.io/acme/payment-service:\${{ commitSha }} .
      docker push ghcr.io/acme/payment-service:\${{ commitSha }}
    secrets:
      REGISTRY_TOKEN: ghcr-push-token

  - name: deploy-staging
    dependsOn: [docker-push]
    environments: [staging]
    run: helm upgrade --install payment-service ./charts/payment-service

  - name: pci-compliance
    dependsOn: [deploy-staging]
    run: ./scripts/pci-scan.sh --level 2 --env staging
    timeout: 15m

  - name: approve-production
    dependsOn: [pci-compliance]
    gate:
      approvers:
        - role:admin
        - user:alice@acme.dev
      minApprovals: 2

  - name: deploy-prod
    dependsOn: [approve-production]
    environments: [production]
    run: helm upgrade --install payment-service ./charts/payment-service --set env=prod`,
    steps: [
      { name: 'install-deps', execType: 'run', wave: 0 },
      { name: 'lint', execType: 'run', wave: 1, dependsOn: ['install-deps'] },
      { name: 'unit-tests', execType: 'run', wave: 1, dependsOn: ['install-deps'] },
      { name: 'security-scan', execType: 'run', wave: 1, dependsOn: ['install-deps'] },
      { name: 'build', execType: 'run', wave: 2, dependsOn: ['lint', 'unit-tests', 'security-scan'] },
      { name: 'docker-push', execType: 'run', wave: 3, dependsOn: ['build'] },
      { name: 'deploy-staging', execType: 'run', wave: 4, dependsOn: ['docker-push'] },
      { name: 'pci-compliance', execType: 'run', wave: 5, dependsOn: ['deploy-staging'] },
      { name: 'approve-production', execType: 'gate', wave: 6, dependsOn: ['pci-compliance'] },
      { name: 'deploy-prod', execType: 'run', wave: 7, dependsOn: ['approve-production'] },
    ],
  }],
  'p-3': [{
    filename: 'ci.yaml',
    status: 'valid',
    yaml: `# Customer Portal — Nested Steps + Matrix Testing
image: node:20-alpine

triggers:
  push:
    branches: [main]
  pull_request:
    branches: [main]

steps:
  # Nested step group: install + codegen run sequentially in the same pod
  - name: setup
    steps:
      - name: install
        run: npm ci --ignore-scripts
      - name: codegen
        run: npm run generate:api-types

  # Parallel quality checks after setup
  - name: quality
    dependsOn: [setup]
    steps:
      - name: lint
        run: npm run lint -- --max-warnings 0
      - name: typecheck
        run: npx tsc --noEmit --pretty
      - name: stylelint
        run: npx stylelint "src/**/*.css"

  # Matrix: test across browsers
  - name: test
    dependsOn: [setup]
    run: npx playwright test --reporter=html
    matrix:
      browser: [chromium, firefox, webkit]
    timeout: 10m

  - name: build
    dependsOn: [quality, test]
    run: npm run build
    env:
      NODE_ENV: production
      VITE_API_URL: https://api.acme.dev
    cache:
      key: next-cache-\${{ hashFiles('package-lock.json') }}
      paths: [/workspace/.next/cache]

  - name: deploy-preview
    dependsOn: [build]
    environments: [staging]
    run: bunx wrangler pages deploy dist/
    secrets:
      CLOUDFLARE_API_TOKEN: cf-deploy-token`,
    steps: [
      { name: 'setup', execType: 'steps', wave: 0 },
      { name: 'quality', execType: 'steps', wave: 1, dependsOn: ['setup'] },
      { name: 'test', execType: 'run', wave: 1, dependsOn: ['setup'] },
      { name: 'build', execType: 'run', wave: 2, dependsOn: ['quality', 'test'] },
      { name: 'deploy-preview', execType: 'run', wave: 3, dependsOn: ['build'] },
    ],
  }, {
    filename: 'deploy.yaml',
    status: 'invalid',
    errors: [
      'Step "deploy-prod" depends on "approval-gate" which does not exist (did you mean "approve-release"?)',
      'triggers: at least one trigger is required',
    ],
    yaml: `# Deploy to Production — BROKEN
# TODO: fix the missing gate step and add triggers

steps:
  - name: build
    run: bun run build

  - name: deploy-prod
    dependsOn: [approval-gate]
    environments: [production]
    run: bunx wrangler pages deploy dist/ --env production

  - name: notify
    dependsOn: [deploy-prod]
    run: ./scripts/notify-slack.sh
    when: always`,
    steps: [
      { name: 'build', execType: 'run', wave: 0 },
      { name: 'deploy-prod', execType: 'run', wave: 1, dependsOn: ['approval-gate'] },
      { name: 'notify', execType: 'run', wave: 2, dependsOn: ['deploy-prod'] },
    ],
  }],
  'p-4': [{
    filename: 'plan.yaml',
    status: 'valid',
    yaml: `# Infrastructure — Terraform Plan & Apply
image: hashicorp/terraform:1.7

triggers:
  pull_request:
    branches: [main]
  manual:
    environments: [staging, production]
    inputs:
      - name: workspace
        type: choice
        options: [default, staging, production]
        default: default
      - name: auto_approve
        type: boolean
        description: Skip the approval gate
        default: "false"

steps:
  - name: init
    run: terraform init -backend-config=backend.hcl
    cache:
      key: tf-providers-\${{ hashFiles('.terraform.lock.hcl') }}
      paths: [/workspace/.terraform]

  - name: validate
    dependsOn: [init]
    run: terraform validate

  - name: plan
    dependsOn: [validate]
    run: terraform plan -out=tfplan -no-color
    env:
      TF_WORKSPACE: \${{ inputs.workspace }}

  - name: approve-apply
    dependsOn: [plan]
    if: \${{ inputs.auto_approve != 'true' }}
    gate:
      approvers:
        - team:infrastructure
        - user:dave@acme.dev
      minApprovals: 1

  - name: apply
    dependsOn: [approve-apply]
    environments: [production]
    run: terraform apply -auto-approve tfplan
    secrets:
      AWS_ACCESS_KEY_ID: aws-infra-key
      AWS_SECRET_ACCESS_KEY: aws-infra-secret`,
    steps: [
      { name: 'init', execType: 'run', wave: 0 },
      { name: 'validate', execType: 'run', wave: 1, dependsOn: ['init'] },
      { name: 'plan', execType: 'run', wave: 2, dependsOn: ['validate'] },
      { name: 'approve-apply', execType: 'gate', wave: 3, dependsOn: ['plan'] },
      { name: 'apply', execType: 'run', wave: 4, dependsOn: ['approve-apply'] },
    ],
    dispatchInputs: [
      { name: 'workspace', type: 'choice', description: 'Terraform workspace', required: true, default: 'default', options: ['default', 'production', 'staging'] },
      { name: 'auto_approve', type: 'boolean', description: 'Auto-approve the apply step', default: 'false' },
    ],
  }, {
    filename: 'drift.yaml',
    status: 'valid',
    yaml: `# Infrastructure Drift Detection
image: hashicorp/terraform:1.7

triggers:
  schedule:
    cron: "0 8 * * 1-5"

steps:
  - name: init
    run: terraform init -backend-config=backend.hcl

  - name: plan
    dependsOn: [init]
    run: terraform plan -detailed-exitcode -no-color 2>&1 | tee drift-report.txt

  - name: notify
    dependsOn: [plan]
    when: always
    run: |
      if [ -s drift-report.txt ]; then
        ./scripts/slack-drift-alert.sh
      fi`,
    steps: [
      { name: 'init', execType: 'run', wave: 0 },
      { name: 'plan', execType: 'run', wave: 1, dependsOn: ['init'] },
      { name: 'notify', execType: 'run', wave: 2, dependsOn: ['plan'] },
    ],
  }],
  'p-5': [{
    filename: 'ci.yaml',
    status: 'valid',
    yaml: `# Auth Service — Templates, Artifacts, and Promotion
image: golang:1.23-alpine
environments: [staging, production]

triggers:
  push:
    branches: [main]
    environments: [staging]
  promotion:
    - from: staging
      environments: [production]
  pull_request:
    branches: [main]

steps:
  # Reusable template for AWS ECR login
  - name: ecr-login
    use: ./fragments/ecr-login.yaml
    with:
      region: us-east-1
      registry: "123456789.dkr.ecr.us-east-1.amazonaws.com"

  - name: install-deps
    run: go mod download
    cache:
      key: go-\${{ hashFiles('go.sum') }}
      paths: [/go/pkg/mod]

  # Nested: lint + security in one pod
  - name: checks
    dependsOn: [install-deps]
    steps:
      - name: lint
        run: golangci-lint run --timeout 5m ./...
      - name: gosec
        run: gosec -fmt json -out /workspace/security-report.json ./...
      - name: govulncheck
        run: govulncheck ./...

  - name: test
    dependsOn: [install-deps]
    run: go test -race -count=1 -coverprofile=coverage.out ./...
    services:
      - name: postgres
        image: postgres:16-alpine
        env:
          POSTGRES_DB: auth_test
          POSTGRES_PASSWORD: test
    # Upload coverage as artifact for downstream steps
    outputs:
      - path: /workspace/coverage.out

  - name: build
    dependsOn: [checks, test]
    run: |
      CGO_ENABLED=0 go build -trimpath \\
        -ldflags="-s -w -X main.version=\${{ shortSha }}" \\
        -o bin/auth-service ./cmd/server
    outputs:
      - path: /workspace/bin

  - name: docker-push
    dependsOn: [build, ecr-login]
    image: docker:24
    run: |
      docker build -t auth-service:\${{ commitSha }} .
      docker push auth-service:\${{ commitSha }}
    inputs:
      - from: build
        path: /workspace/bin

  - name: deploy
    dependsOn: [docker-push]
    run: |
      helm upgrade --install auth-service ./charts/auth-service \\
        --set image.tag=\${{ commitSha }}

  - name: e2e-tests
    dependsOn: [deploy]
    run: ./scripts/e2e-auth.sh --timeout 5m
    timeout: 10m
    retry:
      attempts: 3
      delay: 10s

  - name: approve-production
    dependsOn: [e2e-tests]
    environments: [production]
    gate:
      approvers:
        - team:security
        - role:admin
      minApprovals: 2`,
    steps: [
      { name: 'ecr-login', execType: 'use', wave: 0 },
      { name: 'install-deps', execType: 'run', wave: 0 },
      { name: 'checks', execType: 'steps', wave: 1, dependsOn: ['install-deps'] },
      { name: 'test', execType: 'run', wave: 1, dependsOn: ['install-deps'] },
      { name: 'build', execType: 'run', wave: 2, dependsOn: ['checks', 'test'] },
      { name: 'docker-push', execType: 'run', wave: 3, dependsOn: ['build', 'ecr-login'] },
      { name: 'deploy', execType: 'run', wave: 4, dependsOn: ['docker-push'] },
      { name: 'e2e-tests', execType: 'run', wave: 5, dependsOn: ['deploy'] },
      { name: 'approve-production', execType: 'gate', wave: 6, dependsOn: ['e2e-tests'] },
    ],
  }],
  'p-6': [{
    filename: 'lint.yaml',
    status: 'valid',
    yaml: `# Helm Charts — Lint & Test
image: alpine/helm:3.14

triggers:
  pull_request:
    branches: [main]

steps:
  - name: helm-lint
    run: |
      for chart in charts/*/; do
        helm lint "$chart" --strict
      done

  - name: kubeval
    run: |
      helm template charts/* | kubeval --strict

  - name: chart-test
    dependsOn: [helm-lint, kubeval]
    run: ct lint-and-install --config ct.yaml
    services:
      - name: kind
        image: kindest/node:v1.29.0`,
    steps: [
      { name: 'helm-lint', execType: 'run', wave: 0 },
      { name: 'kubeval', execType: 'run', wave: 0 },
      { name: 'chart-test', execType: 'run', wave: 1, dependsOn: ['helm-lint', 'kubeval'] },
    ],
  }],
}

export function getProjectPipelines(projectId: string): PipelineDef[] {
  return projectPipelines[projectId] ?? []
}

// ---------------------------------------------------------------------------
// Dashboard summary
// ---------------------------------------------------------------------------

export function getDashboardSummary(): DashboardSummary {
  return {
    totalRuns: 1247,
    successRate: 94.2,
    pendingGates: 3,
    activeProjects: 6,
    runsToday: 23,
    avgDuration: '2m 48s',
  }
}

// ---------------------------------------------------------------------------
// Pending gates
// ---------------------------------------------------------------------------

function seedGates(): Gate[] {
  return [
    // Pending
    {
      runId: 'r-101',
      stepName: 'gate-prod',
      status: 'pending',
      message: 'Approve production deployment for Checkout Service v2.8.1',
      projectName: 'Checkout Service',
      projectColour: '#3b82f6',
      workspace: 'payments',
      environment: 'production',
      branch: 'main',
      triggeredBy: 'alice',
      createdAt: '2024-01-15T10:05:00Z',
    },
    {
      runId: 'r-105',
      stepName: 'gate-prod',
      status: 'pending',
      message: 'Approve production deployment for Auth Service v2.1.0',
      projectName: 'Auth Service',
      projectColour: '#f59e0b',
      workspace: 'platform',
      environment: 'production',
      branch: 'release/2.1',
      triggeredBy: 'eve',
      createdAt: '2024-01-15T09:30:00Z',
    },
    {
      runId: 'r-104',
      stepName: 'gate-prod',
      status: 'pending',
      message: 'Approve terraform apply for Redis cluster (3 new resources)',
      projectName: 'Terraform Infra',
      projectColour: '#22c55e',
      workspace: 'platform',
      environment: 'production',
      branch: 'main',
      triggeredBy: 'dave',
      createdAt: '2024-01-15T09:00:00Z',
    },
    // Approved
    {
      runId: 'r-107',
      stepName: 'gate-prod',
      status: 'approved',
      message: 'Approve production deployment for Payment Gateway v3.2.0',
      projectName: 'Payment Gateway',
      projectColour: '#ef4444',
      workspace: 'payments',
      environment: 'production',
      branch: 'main',
      triggeredBy: 'bob',
      reviewedBy: 'alice',
      reviewedAt: '2024-01-14T16:30:00Z',
      createdAt: '2024-01-14T16:00:00Z',
    },
    {
      runId: 'r-109',
      stepName: 'gate-apply',
      status: 'approved',
      message: 'Approve terraform apply for Aurora PostgreSQL upgrade',
      projectName: 'Terraform Infra',
      projectColour: '#22c55e',
      workspace: 'platform',
      environment: 'production',
      branch: 'feat/aurora-upgrade',
      triggeredBy: 'dave',
      reviewedBy: 'alice',
      reviewedAt: '2024-01-14T11:15:00Z',
      createdAt: '2024-01-14T10:30:00Z',
    },
    {
      runId: 'r-108',
      stepName: 'gate-staging',
      status: 'approved',
      message: 'Approve staging deployment for Product Search reindex',
      projectName: 'Product Search',
      projectColour: '#8b5cf6',
      workspace: 'catalog',
      environment: 'staging',
      branch: 'feat/reindex-v2',
      triggeredBy: 'carol',
      reviewedBy: 'eve',
      reviewedAt: '2024-01-14T09:00:00Z',
      createdAt: '2024-01-14T08:45:00Z',
    },
    // Rejected
    {
      runId: 'r-110',
      stepName: 'gate-prod',
      status: 'rejected',
      message: 'Approve production deployment for Auth Service WebAuthn feature',
      projectName: 'Auth Service',
      projectColour: '#f59e0b',
      workspace: 'platform',
      environment: 'production',
      branch: 'feat/webauthn',
      triggeredBy: 'eve',
      reviewedBy: 'alice',
      reviewedAt: '2024-01-13T14:00:00Z',
      createdAt: '2024-01-13T13:30:00Z',
    },
    {
      runId: 'r-106',
      stepName: 'gate-prod',
      status: 'rejected',
      message: 'Approve production deployment for Checkout Service gRPC migration',
      projectName: 'Checkout Service',
      projectColour: '#3b82f6',
      workspace: 'payments',
      environment: 'production',
      branch: 'feat/grpc-migration',
      triggeredBy: 'alice',
      reviewedBy: 'eve',
      reviewedAt: '2024-01-13T10:00:00Z',
      createdAt: '2024-01-13T09:30:00Z',
    },
  ]
}

// Mutable, lazily-seeded gate store. Approve/reject mutate this so the gates
// list and run view reflect decisions in mock mode.
let _gates: Gate[] | undefined
export function getGates(): Gate[] {
  return (_gates ??= seedGates())
}

function decideGate(runId: string, stepName: string, status: 'approved' | 'rejected'): Gate | undefined {
  const gate = getGates().find((g) => g.runId === runId && g.stepName === stepName)
  if (gate) {
    gate.status = status
    gate.reviewedBy = 'you'
    gate.reviewedAt = 'just now'
  }
  return gate
}

export function approveGate(runId: string, stepName: string): Gate | undefined {
  return decideGate(runId, stepName, 'approved')
}

export function rejectGate(runId: string, stepName: string): Gate | undefined {
  return decideGate(runId, stepName, 'rejected')
}

// ---------------------------------------------------------------------------
// Workspaces
// ---------------------------------------------------------------------------

function seedWorkspaces(): Workspace[] {
  return [
    {
      id: 'ws-1',
      name: 'Payments',
      slug: 'payments',
      description: 'Checkout, billing, and payment processing services',
      projectCount: 2,
      createdAt: '2023-06-01T00:00:00Z',
      isDefault: true,
    },
    {
      id: 'ws-2',
      name: 'Catalog',
      slug: 'catalog',
      description: 'Product catalog, search, and inventory management',
      projectCount: 1,
      createdAt: '2023-06-01T00:00:00Z',
    },
    {
      id: 'ws-3',
      name: 'Storefront',
      slug: 'storefront',
      description: 'Customer-facing web app and mobile backend',
      projectCount: 1,
      createdAt: '2023-07-10T00:00:00Z',
    },
    {
      id: 'ws-4',
      name: 'Platform',
      slug: 'platform',
      description: 'Shared infrastructure, auth, and developer tooling',
      projectCount: 2,
      createdAt: '2023-08-15T00:00:00Z',
    },
  ]
}

// ---------------------------------------------------------------------------
// Teams
// ---------------------------------------------------------------------------

function seedTeams(): Team[] {
  return [
    { id: 't-1', name: 'Backend Devs', slug: 'backend-devs', source: 'idp', idpGroup: 'engineering-backend', memberCount: 5 },
    { id: 't-2', name: 'Frontend Devs', slug: 'frontend-devs', source: 'idp', idpGroup: 'engineering-frontend', memberCount: 3 },
    { id: 't-3', name: 'Platform Engineering', slug: 'platform-eng', source: 'idp', idpGroup: 'platform', memberCount: 4 },
    { id: 't-4', name: 'Release Managers', slug: 'release-mgrs', source: 'internal', memberCount: 3 },
  ]
}

// Mutable membership store so add/remove behave like a real backend in mock mode.
let _teamMembers: Record<string, User[]> | undefined
function teamMemberStore(): Record<string, User[]> {
  return (_teamMembers ??= {
    't-1': [users[0]!, users[1]!, users[4]!, users[5]!, users[7]!],   // alice, bob, eve, frank, hiro
    't-2': [users[2]!, users[8]!, users[9]!],                          // carol, iris, jake
    't-3': [users[3]!, users[5]!, users[6]!, users[7]!],               // dave, frank, grace, hiro
    't-4': [users[0]!, users[4]!, users[6]!],                          // alice, eve, grace
  })
}

export function getTeamWithMembers(teamId: string): TeamWithMembers | undefined {
  const team = getTeams().find((t) => t.id === teamId)
  if (!team) return undefined
  const members = teamMemberStore()[teamId] ?? []
  return { ...team, members, memberCount: members.length }
}

export function addTeamMembers(teamId: string, userIds: string[]): { success: true } {
  const store = teamMemberStore()
  const current = store[teamId] ?? (store[teamId] = [])
  for (const id of userIds) {
    if (current.some((u) => u.id === id)) continue
    const u = users.find((x) => x.id === id)
    if (u) current.push(u)
  }
  const team = getTeams().find((t) => t.id === teamId)
  if (team) team.memberCount = current.length
  return { success: true }
}

export function removeTeamMember(teamId: string, userId: string): { success: true } {
  const store = teamMemberStore()
  store[teamId] = (store[teamId] ?? []).filter((u) => u.id !== userId)
  const team = getTeams().find((t) => t.id === teamId)
  if (team) team.memberCount = store[teamId].length
  return { success: true }
}

// ---------------------------------------------------------------------------
// Roles
// ---------------------------------------------------------------------------

function seedRoles(): Role[] {
  return [
    // ── System roles (unscoped, immutable) ──
    {
      id: 'role-1',
      name: 'Admin',
      slug: 'admin',
      description: 'Full platform and CI access',
      isSystem: true,
      permissions: [{ object: '*', action: '*' }],
      workspaces: [],
      environments: [],
    },
    {
      id: 'role-2',
      name: 'Developer',
      slug: 'developer',
      description: 'Trigger and manage CI runs, read admin resources',
      isSystem: true,
      permissions: [
        // Admin — read-only
        { object: 'environment', action: 'read' },
        { object: 'runner', action: 'read' },
        { object: 'secret', action: 'read' },
        // CI — full dev access
        { object: 'project', action: 'read' },
        { object: 'project', action: 'write' },
        { object: 'run', action: 'read' },
        { object: 'run', action: 'trigger' },
        { object: 'run', action: 'cancel' },
      ],
      workspaces: [],
      environments: [],
    },
    {
      id: 'role-3',
      name: 'Viewer',
      slug: 'viewer',
      description: 'Read-only access across the platform',
      isSystem: true,
      permissions: [
        // Admin — read-only
        { object: 'workspace', action: 'read' },
        { object: 'team', action: 'read' },
        { object: 'environment', action: 'read' },
        { object: 'runner', action: 'read' },
        // CI — read-only
        { object: 'project', action: 'read' },
        { object: 'run', action: 'read' },
      ],
      workspaces: [],
      environments: [],
    },
    {
      id: 'role-4',
      name: 'Platform Manager',
      slug: 'platform-manager',
      description: 'Full admin access without CI write permissions',
      isSystem: true,
      permissions: [
        // Admin — full manage
        { object: 'workspace', action: 'read' },
        { object: 'workspace', action: 'manage' },
        { object: 'team', action: 'read' },
        { object: 'team', action: 'manage' },
        { object: 'environment', action: 'read' },
        { object: 'environment', action: 'manage' },
        { object: 'runner', action: 'read' },
        { object: 'runner', action: 'manage' },
        { object: 'connection', action: 'read' },
        { object: 'connection', action: 'manage' },
        { object: 'apikey', action: 'read' },
        { object: 'apikey', action: 'manage' },
        { object: 'secret', action: 'read' },
        { object: 'secret', action: 'manage' },
        { object: 'role', action: 'read' },
        { object: 'role', action: 'manage' },
        { object: 'audit', action: 'read' },
        // CI — read-only
        { object: 'project', action: 'read' },
        { object: 'run', action: 'read' },
      ],
      workspaces: [],
      environments: [],
    },
    // ── Custom roles (scoped) ──
    {
      id: 'role-5',
      name: 'Payments Release Manager',
      slug: 'payments-release-manager',
      description: 'Approve gates and trigger deployments for payment services',
      isSystem: false,
      permissions: [
        { object: 'project', action: 'read' },
        { object: 'run', action: 'read' },
        { object: 'run', action: 'trigger' },
        { object: 'gate', action: 'approve' },
        { object: 'gate', action: 'reject' },
      ],
      workspaces: ['payments'],
      environments: ['production'],
    },
    {
      id: 'role-6',
      name: 'Security Auditor',
      slug: 'security-auditor',
      description: 'Read-only access to CI resources plus audit logs and secrets',
      isSystem: false,
      permissions: [
        // Admin — read audit + secrets
        { object: 'audit', action: 'read' },
        { object: 'secret', action: 'read' },
        { object: 'environment', action: 'read' },
        // CI — read-only
        { object: 'project', action: 'read' },
        { object: 'run', action: 'read' },
      ],
      workspaces: ['payments', 'catalog'],
      environments: ['production', 'staging'],
    },
    {
      id: 'role-7',
      name: 'Staging Operator',
      slug: 'staging-operator',
      description: 'Full CI operations scoped to the staging environment',
      isSystem: false,
      permissions: [
        { object: 'project', action: 'read' },
        { object: 'run', action: 'read' },
        { object: 'run', action: 'trigger' },
        { object: 'run', action: 'cancel' },
        { object: 'gate', action: 'approve' },
        { object: 'gate', action: 'reject' },
      ],
      workspaces: [],
      environments: ['staging'],
    },
  ]
}

// ---------------------------------------------------------------------------
// Role assignments
// ---------------------------------------------------------------------------

function seedAssignments(): Assignment[] {
  return [
    { subject: 'alice@acme.dev', role: 'admin' },
    { subject: 'bob@acme.dev', role: 'developer' },
    { subject: 'carol@acme.dev', role: 'developer' },
    { subject: 'carol@acme.dev', role: 'staging-operator' },
    { subject: 'dave@acme.dev', role: 'developer' },
    { subject: 'eve@acme.dev', role: 'prod-release-manager' },
    { subject: 'eve@acme.dev', role: 'security-auditor' },
    { subject: 'team:backend-devs', role: 'developer' },
    { subject: 'team:release-mgrs', role: 'prod-release-manager' },
  ]
}

// ---------------------------------------------------------------------------
// Environments (simplified — just name + slug)
// ---------------------------------------------------------------------------

function seedEnvironments(): Environment[] {
  return [
    { id: 'env-1', name: 'production', slug: 'production', createdAt: '2023-06-01T00:00:00Z' },
    { id: 'env-2', name: 'staging', slug: 'staging', createdAt: '2023-06-01T00:00:00Z' },
    { id: 'env-3', name: 'dev', slug: 'dev', createdAt: '2023-06-01T00:00:00Z' },
  ]
}

// ---------------------------------------------------------------------------
// Environment variables + per-env values
// ---------------------------------------------------------------------------

function seedEnvVariables(): EnvVariable[] {
  return [
    // Environment-scoped variables
    { id: 'var-1', name: 'CLUSTER_URL', description: 'Base URL for the deployment cluster', scope: 'environment', isSecret: false, createdAt: '2023-06-01T00:00:00Z' },
    { id: 'var-2', name: 'REPLICAS', description: 'Number of pod replicas', scope: 'environment', isSecret: false, createdAt: '2023-06-01T00:00:00Z' },
    { id: 'var-3', name: 'DB_HOST', description: 'Database hostname', scope: 'environment', isSecret: false, createdAt: '2023-07-01T00:00:00Z' },
    { id: 'var-4', name: 'LOG_LEVEL', description: 'Application log level', scope: 'environment', isSecret: false, createdAt: '2023-08-01T00:00:00Z' },
    { id: 'var-5', name: 'STRIPE_SECRET_KEY', description: 'Stripe API secret key', scope: 'environment', isSecret: true, createdAt: '2023-09-10T00:00:00Z' },
    { id: 'var-6', name: 'SENTRY_DSN', description: 'Sentry error tracking DSN', scope: 'environment', isSecret: true, createdAt: '2023-10-05T00:00:00Z' },
    { id: 'var-7', name: 'AWS_ACCESS_KEY_ID', description: 'AWS access key', scope: 'environment', isSecret: true, createdAt: '2023-06-01T00:00:00Z' },
    // Global variables
    { id: 'var-8', name: 'APP_NAME', description: 'Application name used in logs and metrics', scope: 'global', isSecret: false, value: 'flint', createdAt: '2023-06-01T00:00:00Z' },
    { id: 'var-9', name: 'DOCKER_REGISTRY', description: 'Docker registry URL', scope: 'global', isSecret: false, value: 'registry.acme.dev', createdAt: '2023-06-15T00:00:00Z' },
    { id: 'var-10', name: 'SLACK_WEBHOOK_URL', description: 'Slack notifications webhook', scope: 'global', isSecret: true, value: 'https://hooks.slack.com/services/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', createdAt: '2023-07-01T00:00:00Z' },
  ]
}

function seedEnvVariableValues(): EnvVariableValue[] {
  return [
    // CLUSTER_URL
    { variableId: 'var-1', environmentId: 'env-1', value: 'https://api.acme.com', updatedAt: '2024-01-01T00:00:00Z' },
    { variableId: 'var-1', environmentId: 'env-2', value: 'https://staging-api.acme.com', updatedAt: '2024-01-01T00:00:00Z' },
    { variableId: 'var-1', environmentId: 'env-3', value: 'http://localhost:8080', updatedAt: '2024-01-01T00:00:00Z' },
    // REPLICAS
    { variableId: 'var-2', environmentId: 'env-1', value: '5', updatedAt: '2024-01-01T00:00:00Z' },
    { variableId: 'var-2', environmentId: 'env-2', value: '2', updatedAt: '2024-01-01T00:00:00Z' },
    { variableId: 'var-2', environmentId: 'env-3', value: '1', updatedAt: '2024-01-01T00:00:00Z' },
    // DB_HOST
    { variableId: 'var-3', environmentId: 'env-1', value: 'prod-db.internal', updatedAt: '2024-01-01T00:00:00Z' },
    { variableId: 'var-3', environmentId: 'env-2', value: 'staging-db.internal', updatedAt: '2024-01-01T00:00:00Z' },
    { variableId: 'var-3', environmentId: 'env-3', value: 'localhost', updatedAt: '2024-01-01T00:00:00Z' },
    // LOG_LEVEL
    { variableId: 'var-4', environmentId: 'env-1', value: 'warn', updatedAt: '2024-01-01T00:00:00Z' },
    { variableId: 'var-4', environmentId: 'env-2', value: 'info', updatedAt: '2024-01-01T00:00:00Z' },
    { variableId: 'var-4', environmentId: 'env-3', value: 'debug', updatedAt: '2024-01-01T00:00:00Z' },
    // STRIPE_SECRET_KEY (secret — prod + staging only, missing in dev)
    { variableId: 'var-5', environmentId: 'env-1', value: 'sk_live_51NzAqRCjT8Yb4kWpX7mL9vQ3dF6gH2jK8nP0rS5tU', updatedAt: '2023-12-01T00:00:00Z' },
    { variableId: 'var-5', environmentId: 'env-2', value: 'sk_test_51NzAqRCjT8Yb4kWpX7mL9vQ3dF6gH2jK8nP0rS5tU', updatedAt: '2023-11-15T00:00:00Z' },
    // SENTRY_DSN (secret — prod + staging)
    { variableId: 'var-6', environmentId: 'env-1', value: 'https://e4d8f2a1b3c5@o482951.ingest.sentry.io/5847293', updatedAt: '2023-10-05T00:00:00Z' },
    { variableId: 'var-6', environmentId: 'env-2', value: 'https://a7b1c45d2e3f@o482951.ingest.sentry.io/5847294', updatedAt: '2023-10-05T00:00:00Z' },
    // AWS_ACCESS_KEY_ID (secret — prod only)
    { variableId: 'var-7', environmentId: 'env-1', value: 'AKIAIOSFODNN7EXAMPLE', updatedAt: '2024-01-01T00:00:00Z' },
  ]
}

// ---------------------------------------------------------------------------
// API keys
// ---------------------------------------------------------------------------

function seedApiKeys(): ApiKey[] {
  return [
    {
      id: 'ak-1',
      name: 'CI Bot',
      role: 'developer',
      workspaces: ['payments'],
      environments: ['production'],
      expiresAt: '2025-06-01T00:00:00Z',
      lastUsedAt: '2024-01-15T09:45:00Z',
      createdBy: 'alice@acme.dev',
      createdAt: '2024-01-01T00:00:00Z',
    },
    {
      id: 'ak-2',
      name: 'Monitoring Dashboard',
      role: 'viewer',
      workspaces: [],
      environments: [],
      lastUsedAt: '2024-01-15T10:00:00Z',
      createdBy: 'alice@acme.dev',
      createdAt: '2023-11-01T00:00:00Z',
    },
    {
      id: 'ak-3',
      name: 'Terraform Automation',
      role: 'developer',
      workspaces: ['platform'],
      environments: [],
      expiresAt: '2024-12-31T00:00:00Z',
      lastUsedAt: '2024-01-14T22:00:00Z',
      createdBy: 'dave@acme.dev',
      createdAt: '2024-01-01T00:00:00Z',
    },
  ]
}

// ---------------------------------------------------------------------------
// Audit log
// ---------------------------------------------------------------------------

export function getAuditEntries(): AuditEntry[] {
  return [
    {
      id: 'audit-1',
      userId: 'u-1',
      action: 'run.trigger',
      resourceType: 'run',
      resourceId: 'r-101',
      metadata: { branch: 'main', trigger: 'push' },
      ipAddress: '10.0.1.42',
      createdAt: '2024-01-15T10:00:00Z',
    },
    {
      id: 'audit-2',
      userId: 'u-1',
      action: 'gate.approve',
      resourceType: 'gate',
      resourceId: 'r-101:gate-prod',
      metadata: { comment: 'LGTM, metrics look good' },
      ipAddress: '10.0.1.42',
      createdAt: '2024-01-15T10:05:30Z',
    },
    {
      id: 'audit-3',
      userId: 'u-2',
      action: 'run.trigger',
      resourceType: 'run',
      resourceId: 'r-102',
      metadata: { branch: 'feat/stripe-v3', trigger: 'push' },
      ipAddress: '10.0.1.55',
      createdAt: '2024-01-15T09:52:00Z',
    },
    {
      id: 'audit-4',
      userId: 'u-4',
      action: 'secret.create',
      resourceType: 'secret',
      resourceId: 'DATADOG_API_KEY',
      ipAddress: '10.0.2.10',
      createdAt: '2024-01-14T16:00:00Z',
    },
    {
      id: 'audit-5',
      userId: 'u-1',
      action: 'environment.update',
      resourceType: 'environment',
      resourceId: 'env-1',
      metadata: { field: 'deployWindow', oldValue: '09:00-16:00', newValue: '09:00-17:00' },
      ipAddress: '10.0.1.42',
      createdAt: '2024-01-14T14:30:00Z',
    },
    {
      id: 'audit-6',
      userId: 'u-5',
      action: 'role.assign',
      resourceType: 'role',
      resourceId: 'role-5',
      metadata: { subject: 'eve@acme.dev', workspace: 'payments' },
      ipAddress: '10.0.1.80',
      createdAt: '2024-01-14T11:00:00Z',
    },
    {
      id: 'audit-7',
      action: 'apikey.create',
      resourceType: 'apikey',
      resourceId: 'ak-3',
      metadata: { name: 'Terraform Automation', scopes: ['run:trigger', 'run:read'] },
      ipAddress: '10.0.3.5',
      createdAt: '2024-01-13T09:00:00Z',
    },
    {
      id: 'audit-8',
      userId: 'u-3',
      action: 'run.cancel',
      resourceType: 'run',
      resourceId: 'r-106',
      metadata: { reason: 'Superseded by newer commit' },
      ipAddress: '10.0.1.67',
      createdAt: '2024-01-13T08:30:00Z',
    },
    {
      id: 'audit-9',
      userId: 'u-4',
      action: 'runner.create',
      resourceType: 'runner',
      resourceId: 'rp-3',
      metadata: { name: 'gpu', arch: 'amd64', gpuVendor: 'nvidia' },
      ipAddress: '10.0.2.10',
      createdAt: '2024-01-12T15:00:00Z',
    },
    {
      id: 'audit-10',
      userId: 'u-6',
      action: 'project.create',
      resourceType: 'project',
      resourceId: 'p-6',
      metadata: { name: 'Helm Charts', workspace: 'platform' },
      ipAddress: '10.0.1.90',
      createdAt: '2024-01-12T10:00:00Z',
    },
  ]
}

// ---------------------------------------------------------------------------
// Runner pools
// ---------------------------------------------------------------------------

export function getRunnerPools(): RunnerPool[] {
  return [
    {
      id: 'rp-1',
      name: 'standard',
      description: 'General-purpose build runners for most CI workloads',
      cpu: '4 vCPU',
      memory: '8 GB',
      arch: 'amd64',
      ready: true,
      createdAt: '2023-06-01T00:00:00Z',
    },
    {
      id: 'rp-2',
      name: 'high-memory',
      description: 'High-memory runners for integration tests and large builds',
      cpu: '8 vCPU',
      memory: '32 GB',
      arch: 'amd64',
      ready: true,
      createdAt: '2023-09-15T00:00:00Z',
    },
    {
      id: 'rp-3',
      name: 'gpu',
      description: 'GPU-accelerated runners for ML model training and validation',
      cpu: '8 vCPU',
      memory: '64 GB',
      arch: 'amd64',
      gpuVendor: 'nvidia',
      gpuModel: 'A100',
      gpuCount: 1,
      ready: false,
      createdAt: '2024-01-10T00:00:00Z',
    },
  ]
}

// ---------------------------------------------------------------------------
// Forge connections
// ---------------------------------------------------------------------------

export function getForgeConnections(): ForgeConnection[] {
  return [
    {
      id: 'fc-1',
      forgeType: 'github',
      displayName: 'GitHub - Acme Org',
      createdAt: '2023-06-01T00:00:00Z',
    },
    {
      id: 'fc-2',
      forgeType: 'gitlab',
      displayName: 'GitLab - Platform Team',
      createdAt: '2023-11-20T00:00:00Z',
    },
  ]
}

// ---------------------------------------------------------------------------
// Auth user (current session)
// ---------------------------------------------------------------------------

let _authUser: AuthUser | undefined
export function getAuthUser(): AuthUser {
  // Seed appearance from localStorage so the mock "server" agrees with the
  // local choice — otherwise reconcile (server-wins) would revert it on reload.
  const ls = typeof window !== 'undefined' ? window.localStorage : undefined
  return (_authUser ??= {
    userId: 'u-1',
    email: 'alice@acme.dev',
    name: 'Alice Chen',
    avatarUrl: '',
    role: 'admin',
    permissions: ['*:*'],
    groups: ['backend-devs', 'release-mgrs'],
    provider: 'local',
    themeMode: ls?.getItem('theme') ?? 'auto',
    colorTheme: ls?.getItem('flint-color-theme') ?? 'ocean',
    mfaEnabled: false,
  })
}

export function updateAuthProfile(input: {
  name?: string
  avatarUrl?: string
  themeMode?: string
  colorTheme?: string
}): { success: true } {
  const u = getAuthUser()
  if (input.name !== undefined) u.name = input.name
  if (input.avatarUrl !== undefined) u.avatarUrl = input.avatarUrl
  if (input.themeMode !== undefined) u.themeMode = input.themeMode
  if (input.colorTheme !== undefined) u.colorTheme = input.colorTheme
  return { success: true }
}

export function changePassword(): { success: true } {
  return { success: true }
}

// ── Sessions (current user) ─────────────────────────────────
let _sessions: Session[] | undefined
function seedSessions(): Session[] {
  return [
    { id: 'sess-1', ipAddress: '192.168.1.42', userAgent: 'Mozilla/5.0 (Macintosh; Apple Silicon) Chrome/124', createdAt: '2026-06-14T08:00:00Z', lastActivity: nowIso(), current: true },
    { id: 'sess-2', ipAddress: '10.0.0.7', userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_2) Safari', createdAt: '2026-06-12T14:30:00Z', lastActivity: '2026-06-13T19:05:00Z' },
    { id: 'sess-3', ipAddress: '172.16.4.19', userAgent: 'flint-cli/1.4.0 (linux; amd64)', createdAt: '2026-06-10T09:12:00Z', lastActivity: '2026-06-13T11:40:00Z' },
  ]
}
export function getSessions(): Session[] {
  return (_sessions ??= seedSessions())
}
export function revokeSession(id: string): { success: true } {
  const list = getSessions()
  const i = list.findIndex((s) => s.id === id)
  if (i >= 0) list.splice(i, 1)
  return { success: true }
}

// ── MFA enrollment ──────────────────────────────────────────
export function mfaSetup(): MfaSetup {
  return {
    secret: 'JBSWY3DPEHPK3PXP',
    qrCodeURL: 'otpauth://totp/Flint:alice@acme.dev?secret=JBSWY3DPEHPK3PXP&issuer=Flint',
    recoveryCodes: ['8f3k-2m9x', 'q7w2-p4r8', 'z1c5-v6b3', 'n9d4-h2j7', 'a5s8-g3f1', 't6y9-u4i2'],
  }
}
export function mfaVerifySetup(): { success: true } {
  getAuthUser().mfaEnabled = true
  return { success: true }
}
export function mfaDisable(): { success: true } {
  getAuthUser().mfaEnabled = false
  return { success: true }
}

// ── SSO providers ───────────────────────────────────────────
let _ssoProviders: { id: string; providerType: string; displayName: string }[] | undefined
export function getAuthProviders(): AuthProviders {
  const list = (_ssoProviders ??= [])
  return {
    providers: list,
    oidcConfigured: list.some((p) => p.providerType === 'oidc'),
    samlConfigured: list.some((p) => p.providerType === 'saml'),
  }
}
export function saveAuthProvider(input: { providerType: string; displayName?: string }): { success: true } {
  const list = (_ssoProviders ??= [])
  const existing = list.find((p) => p.providerType === input.providerType)
  if (existing) existing.displayName = input.displayName || input.providerType
  else list.push({ id: nextId('sso'), providerType: input.providerType, displayName: input.displayName || input.providerType })
  return { success: true }
}
export function deleteAuthProvider(providerType: string): { success: true } {
  _ssoProviders = (_ssoProviders ?? []).filter((p) => p.providerType !== providerType)
  return { success: true }
}

// ---------------------------------------------------------------------------
// Personal tokens
// ---------------------------------------------------------------------------

const personalTokens: Record<string, PersonalToken[]> = {
  'u-1': [
    {
      id: 'pt-1',
      name: 'MacBook Pro',
      expiresAt: '2026-12-31T00:00:00Z',
      lastUsedAt: '2026-03-31T14:30:00Z',
      createdAt: '2026-01-15T10:00:00Z',
    },
    {
      id: 'pt-2',
      name: 'CI Workstation',
      lastUsedAt: '2026-03-28T09:00:00Z',
      createdAt: '2026-02-01T08:00:00Z',
    },
  ],
  'u-2': [
    {
      id: 'pt-3',
      name: 'Laptop',
      expiresAt: '2026-06-01T00:00:00Z',
      lastUsedAt: '2026-03-30T11:00:00Z',
      createdAt: '2026-03-01T10:00:00Z',
    },
  ],
}

export function getPersonalTokens(userId: string): PersonalToken[] {
  return personalTokens[userId] ?? []
}

// ---------------------------------------------------------------------------
// Workflows — generic engine runs (no forge/repo). Mock-first per the
// API-first approach; reconcile with the backend (list endpoint) later.
// ---------------------------------------------------------------------------

const workflowRuns: WorkflowRun[] = [
  { id: 'wf-1042', name: 'nightly-data-export', status: 'running', triggerType: 'schedule', triggeredBy: 'cron', startedAt: '2026-06-07T02:00:00Z', duration: '4m 12s', stepCount: 6 },
  { id: 'wf-1041', name: 'reindex-search', status: 'succeeded', triggerType: 'manual', triggeredBy: 'srinivas', startedAt: '2026-06-06T18:22:00Z', finishedAt: '2026-06-06T18:25:41Z', duration: '3m 41s', stepCount: 4 },
  { id: 'wf-1040', name: 'rotate-credentials', status: 'succeeded', triggerType: 'schedule', triggeredBy: 'cron', startedAt: '2026-06-06T06:00:00Z', finishedAt: '2026-06-06T06:01:08Z', duration: '1m 08s', stepCount: 3 },
  { id: 'wf-1039', name: 'nightly-data-export', status: 'failed', triggerType: 'schedule', triggeredBy: 'cron', startedAt: '2026-06-06T02:00:00Z', finishedAt: '2026-06-06T02:03:55Z', duration: '3m 55s', stepCount: 6 },
  { id: 'wf-1038', name: 'backfill-metrics', status: 'cancelled', triggerType: 'manual', triggeredBy: 'priya', startedAt: '2026-06-05T14:10:00Z', finishedAt: '2026-06-05T14:12:30Z', duration: '2m 30s', stepCount: 5 },
  { id: 'wf-1037', name: 'reindex-search', status: 'succeeded', triggerType: 'api', triggeredBy: 'api', startedAt: '2026-06-05T09:48:00Z', finishedAt: '2026-06-05T09:51:20Z', duration: '3m 20s', stepCount: 4 },
]

const workflowSteps: Record<string, PipelineStep[]> = {
  'nightly-data-export': [
    { name: 'snapshot-db', status: 'succeeded', execType: 'run', wave: 0, attempt: 1, maxAttempts: 1, startedAt: '2026-06-07T02:00:00Z', finishedAt: '2026-06-07T02:01:10Z' },
    { name: 'export-orders', status: 'succeeded', execType: 'run', wave: 1, attempt: 1, maxAttempts: 1, dependsOn: ['snapshot-db'], startedAt: '2026-06-07T02:01:10Z', finishedAt: '2026-06-07T02:02:40Z' },
    { name: 'export-users', status: 'succeeded', execType: 'run', wave: 1, attempt: 1, maxAttempts: 1, dependsOn: ['snapshot-db'], startedAt: '2026-06-07T02:01:10Z', finishedAt: '2026-06-07T02:02:55Z' },
    { name: 'transform', status: 'running', execType: 'run', wave: 2, attempt: 1, maxAttempts: 1, dependsOn: ['export-orders', 'export-users'], startedAt: '2026-06-07T02:02:55Z' },
    { name: 'upload-s3', status: 'pending', execType: 'run', wave: 3, attempt: 1, maxAttempts: 1, dependsOn: ['transform'] },
    { name: 'notify', status: 'pending', execType: 'http', wave: 4, attempt: 1, maxAttempts: 1, dependsOn: ['upload-s3'] },
  ],
  'reindex-search': [
    { name: 'fetch-documents', status: 'succeeded', execType: 'run', wave: 0, attempt: 1, maxAttempts: 1, startedAt: '2026-06-06T18:22:00Z', finishedAt: '2026-06-06T18:23:10Z' },
    { name: 'build-index', status: 'succeeded', execType: 'run', wave: 1, attempt: 1, maxAttempts: 1, dependsOn: ['fetch-documents'], startedAt: '2026-06-06T18:23:10Z', finishedAt: '2026-06-06T18:25:00Z' },
    { name: 'swap-alias', status: 'succeeded', execType: 'run', wave: 2, attempt: 1, maxAttempts: 1, dependsOn: ['build-index'], startedAt: '2026-06-06T18:25:00Z', finishedAt: '2026-06-06T18:25:30Z' },
    { name: 'notify', status: 'succeeded', execType: 'http', wave: 3, attempt: 1, maxAttempts: 1, dependsOn: ['swap-alias'], startedAt: '2026-06-06T18:25:30Z', finishedAt: '2026-06-06T18:25:41Z' },
  ],
}

function defaultWorkflowSteps(): PipelineStep[] {
  return [
    { name: 'prepare', status: 'succeeded', execType: 'run', wave: 0, attempt: 1, maxAttempts: 1, startedAt: '2026-06-06T06:00:00Z', finishedAt: '2026-06-06T06:00:30Z' },
    { name: 'execute', status: 'succeeded', execType: 'run', wave: 1, attempt: 1, maxAttempts: 1, dependsOn: ['prepare'], startedAt: '2026-06-06T06:00:30Z', finishedAt: '2026-06-06T06:00:58Z' },
    { name: 'finalize', status: 'succeeded', execType: 'run', wave: 2, attempt: 1, maxAttempts: 1, dependsOn: ['execute'], startedAt: '2026-06-06T06:00:58Z', finishedAt: '2026-06-06T06:01:08Z' },
  ]
}

export function getWorkflowRuns(): WorkflowRun[] {
  return workflowRuns
}

export function getWorkflowRun(id: string): WorkflowRunDetail | undefined {
  const run = workflowRuns.find((r) => r.id === id)
  if (!run) return undefined
  const steps = workflowSteps[run.name] ?? defaultWorkflowSteps()
  return { ...run, steps }
}

// ---------------------------------------------------------------------------
// Stateful mock store
//
// The getters above (seedX) build the initial data; these lazily snapshot it
// into mutable collections so that, in mock mode (FLINT_API_MODE=mock), the
// router's write handlers mutate real state and subsequent reads reflect it.
// Pure UI development against mocks then behaves like a real backend.
// ---------------------------------------------------------------------------

function nowIso(): string {
  // Avoid Date.now()/new Date() jitter concerns — a fixed-ish stamp is fine for
  // mock data; uniqueness for ids comes from the per-entity sequences below.
  return new Date().toISOString()
}

let _seq = 0
const nextId = (prefix: string) => `${prefix}-${(_seq += 1)}`

// Workspaces
let _workspaces: Workspace[] | undefined
export function getWorkspaces(): Workspace[] {
  return (_workspaces ??= seedWorkspaces())
}
export function createWorkspace(input: { name: string; slug: string; description?: string }): Workspace {
  const ws: Workspace = { id: nextId('ws'), name: input.name, slug: input.slug, description: input.description, projectCount: 0, createdAt: nowIso() }
  getWorkspaces().unshift(ws)
  return ws
}
export function deleteWorkspace(id: string): { success: true } {
  const list = getWorkspaces()
  const i = list.findIndex((w) => w.id === id)
  if (i >= 0) list.splice(i, 1)
  return { success: true }
}

// Tag registry (curated namespaced keys)
function seedTagKeys(): TagKey[] {
  return [
    { id: 'tk-1', key: 'domain', label: 'Domain', allowedValues: ['checkout', 'catalog', 'identity', 'platform'], color: '#6366f1' },
    { id: 'tk-2', key: 'tier', label: 'Tier', allowedValues: ['1', '2', '3'], color: '#ef4444' },
    { id: 'tk-3', key: 'lang', label: 'Language', allowedValues: ['go', 'ts', 'python', 'rust'], color: '#10b981' },
    { id: 'tk-4', key: 'compliance', label: 'Compliance', allowedValues: ['pci', 'sox', 'gdpr'], color: '#f59e0b' },
  ]
}
let _tagKeys: TagKey[] | undefined
export function getTagKeys(): TagKey[] {
  return (_tagKeys ??= seedTagKeys())
}
export function createTagKey(input: { key: string; label: string; allowedValues?: string[]; color?: string }): TagKey {
  const tk: TagKey = {
    id: nextId('tk'),
    key: input.key,
    label: input.label,
    allowedValues: input.allowedValues ?? [],
    color: input.color ?? '#64748b',
  }
  getTagKeys().unshift(tk)
  return tk
}
export function updateTagKey(input: { id: string; label: string; allowedValues?: string[]; color?: string }): TagKey {
  const tk = getTagKeys().find((t) => t.id === input.id)
  if (!tk) throw new Error('Tag key not found')
  tk.label = input.label
  if (input.allowedValues !== undefined) tk.allowedValues = input.allowedValues
  if (input.color !== undefined) tk.color = input.color
  return tk
}
export function deleteTagKey(id: string): { success: true } {
  const list = getTagKeys()
  const i = list.findIndex((t) => t.id === id)
  if (i >= 0) list.splice(i, 1)
  return { success: true }
}

// Environments
let _environments: Environment[] | undefined
export function getEnvironments(): Environment[] {
  return (_environments ??= seedEnvironments())
}
export function createEnvironment(input: { name: string; slug: string }): Environment {
  const env: Environment = { id: nextId('env'), name: input.name, slug: input.slug, createdAt: nowIso() }
  getEnvironments().unshift(env)
  return env
}
export function deleteEnvironment(id: string): { success: true } {
  const list = getEnvironments()
  const i = list.findIndex((e) => e.id === id)
  if (i >= 0) list.splice(i, 1)
  return { success: true }
}

// Env variables + values
let _envVariables: EnvVariable[] | undefined
export function getEnvVariables(): EnvVariable[] {
  return (_envVariables ??= seedEnvVariables())
}
let _envVariableValues: EnvVariableValue[] | undefined
export function getEnvVariableValues(): EnvVariableValue[] {
  return (_envVariableValues ??= seedEnvVariableValues())
}
export function createEnvVariable(input: { name: string; description?: string; scope: EnvVariable['scope']; isSecret: boolean; value?: string }): EnvVariable {
  const v: EnvVariable = { id: nextId('var'), name: input.name, description: input.description, scope: input.scope, isSecret: input.isSecret, value: input.value, createdAt: nowIso() }
  getEnvVariables().unshift(v)
  return v
}
export function setEnvVariableValue(input: { variableId: string; environmentId?: string; value: string }): EnvVariableValue | EnvVariable {
  if (!input.environmentId) {
    // Global value lives on the variable itself.
    const v = getEnvVariables().find((x) => x.id === input.variableId)
    if (v) v.value = input.value
    return v ?? ({} as EnvVariable)
  }
  const values = getEnvVariableValues()
  let row = values.find((x) => x.variableId === input.variableId && x.environmentId === input.environmentId)
  if (row) {
    row.value = input.value
    row.updatedAt = nowIso()
  } else {
    row = { variableId: input.variableId, environmentId: input.environmentId, value: input.value, updatedAt: nowIso() }
    values.push(row)
  }
  return row
}
export function deleteEnvVariable(id: string): { success: true } {
  const list = getEnvVariables()
  const i = list.findIndex((v) => v.id === id)
  if (i >= 0) list.splice(i, 1)
  const values = getEnvVariableValues()
  for (let j = values.length - 1; j >= 0; j--) if (values[j].variableId === id) values.splice(j, 1)
  return { success: true }
}

// Teams
let _teams: Team[] | undefined
export function getTeams(): Team[] {
  return (_teams ??= seedTeams())
}
export function createTeam(input: { name: string; slug: string }): Team {
  const t: Team = { id: nextId('t'), name: input.name, slug: input.slug, source: 'internal', memberCount: 0 }
  getTeams().unshift(t)
  return t
}
export function deleteTeam(id: string): { success: true } {
  const list = getTeams()
  const i = list.findIndex((t) => t.id === id)
  if (i >= 0) list.splice(i, 1)
  return { success: true }
}

// Roles
let _roles: Role[] | undefined
export function getRoles(): Role[] {
  return (_roles ??= seedRoles())
}
export function createRole(input: { name: string; slug: string; description?: string; permissions?: Role['permissions']; workspaces?: string[]; environments?: string[] }): Role {
  const r: Role = { id: nextId('role'), name: input.name, slug: input.slug, description: input.description, isSystem: false, permissions: input.permissions ?? [], workspaces: input.workspaces ?? [], environments: input.environments ?? [] }
  getRoles().push(r)
  return r
}
export function updateRole(id: string, patch: Partial<Role>): Role | undefined {
  const r = getRoles().find((x) => x.id === id)
  if (r) Object.assign(r, patch)
  return r
}
export function deleteRole(id: string): { success: true } {
  const list = getRoles()
  const i = list.findIndex((r) => r.id === id)
  if (i >= 0) list.splice(i, 1)
  return { success: true }
}

// Role assignments
let _assignments: Assignment[] | undefined
export function getAssignments(): Assignment[] {
  return (_assignments ??= seedAssignments())
}
export function createAssignments(input: { subjects: string[]; role: string }): { success: true; count: number } {
  const list = getAssignments()
  for (const subject of input.subjects) {
    if (!list.some((a) => a.subject === subject && a.role === input.role)) list.push({ subject, role: input.role })
  }
  return { success: true, count: input.subjects.length }
}
export function deleteAssignment(subject: string, role: string): { success: true } {
  const list = getAssignments()
  const i = list.findIndex((a) => a.subject === subject && a.role === role)
  if (i >= 0) list.splice(i, 1)
  return { success: true }
}

// API keys
let _apiKeys: ApiKey[] | undefined
export function getApiKeys(): ApiKey[] {
  return (_apiKeys ??= seedApiKeys())
}
export function createApiKey(input: { name: string; role: string; workspaces?: string[]; environments?: string[]; expiresAt?: string }): ApiKey & { token: string } {
  const key: ApiKey = { id: nextId('ak'), name: input.name, role: input.role, workspaces: input.workspaces ?? [], environments: input.environments ?? [], expiresAt: input.expiresAt, createdBy: 'you', createdAt: nowIso() }
  getApiKeys().unshift(key)
  return { ...key, token: `flint_mock_${key.id}` }
}
export function deleteApiKey(id: string): { success: true } {
  const list = getApiKeys()
  const i = list.findIndex((k) => k.id === id)
  if (i >= 0) list.splice(i, 1)
  return { success: true }
}

// Personal tokens (per-user store; the backing record is already mutable)
export function createPersonalToken(input: { userId: string; name: string; expiresAt?: string }): PersonalToken & { token: string } {
  const pt: PersonalToken = { id: nextId('pt'), name: input.name, expiresAt: input.expiresAt, createdAt: nowIso() }
  ;(personalTokens[input.userId] ??= []).unshift(pt)
  return { ...pt, token: `flint_pat_mock_${pt.id}` }
}
export function deletePersonalToken(id: string): { success: true } {
  for (const userId of Object.keys(personalTokens)) {
    const list = personalTokens[userId]
    const i = list.findIndex((t) => t.id === id)
    if (i >= 0) { list.splice(i, 1); break }
  }
  return { success: true }
}
