import type { PipelineStep } from '#/components/pipeline/dag-view'

export interface Project {
  id: string
  name: string
  repo: string
  workspace: string
  lastRun?: {
    id: string
    status: 'succeeded' | 'failed' | 'running' | 'pending'
    branch: string
    duration: string
    triggeredBy: string
    startedAt: string
  }
  tags: string[]
  colour: string
}

export interface PipelineRun {
  id: string
  projectName: string
  projectColour: string
  repo: string
  status: 'succeeded' | 'failed' | 'running' | 'pending' | 'cancelled'
  branch: string
  commitSha: string
  commitMessage: string
  triggeredBy: string
  triggerType: 'push' | 'pull_request' | 'manual' | 'schedule'
  duration: string
  startedAt: string
  workflowFile: string
}

export const mockProjects: Project[] = [
  {
    id: 'p-1',
    name: 'API Gateway',
    repo: 'acme/api-gateway',
    workspace: 'production',
    lastRun: {
      id: 'r-101',
      status: 'succeeded',
      branch: 'main',
      duration: '2m 34s',
      triggeredBy: 'alice',
      startedAt: '3 min ago',
    },
    tags: ['backend', 'critical'],
    colour: '#3b82f6',
  },
  {
    id: 'p-2',
    name: 'Payment Service',
    repo: 'acme/payment-service',
    workspace: 'production',
    lastRun: {
      id: 'r-102',
      status: 'failed',
      branch: 'feat/stripe-v3',
      duration: '1m 12s',
      triggeredBy: 'bob',
      startedAt: '8 min ago',
    },
    tags: ['backend', 'pci'],
    colour: '#ef4444',
  },
  {
    id: 'p-3',
    name: 'Web Dashboard',
    repo: 'acme/web-dashboard',
    workspace: 'staging',
    lastRun: {
      id: 'r-103',
      status: 'running',
      branch: 'main',
      duration: '1m 45s',
      triggeredBy: 'carol',
      startedAt: '1 min ago',
    },
    tags: ['frontend'],
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
    tags: ['iac', 'platform'],
    colour: '#22c55e',
  },
  {
    id: 'p-5',
    name: 'Auth Service',
    repo: 'acme/auth-service',
    workspace: 'production',
    lastRun: {
      id: 'r-105',
      status: 'succeeded',
      branch: 'release/2.1',
      duration: '3m 18s',
      triggeredBy: 'eve',
      startedAt: '1 hour ago',
    },
    tags: ['backend', 'security'],
    colour: '#f59e0b',
  },
  {
    id: 'p-6',
    name: 'Helm Charts',
    repo: 'acme/helm-charts',
    workspace: 'platform',
    tags: ['iac'],
    colour: '#06b6d4',
  },
]

export const mockRuns: PipelineRun[] = [
  {
    id: 'r-101',
    projectName: 'API Gateway',
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
    projectName: 'Payment Service',
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
    projectName: 'Web Dashboard',
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
    projectName: 'API Gateway',
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
]

export const mockPipelineSteps: PipelineStep[] = [
  {
    name: 'checkout',
    status: 'succeeded',
    execType: 'run',
    wave: 0,
    startedAt: '2024-01-15T10:00:00Z',
    finishedAt: '2024-01-15T10:00:05Z',
  },
  {
    name: 'install-deps',
    status: 'succeeded',
    execType: 'run',
    wave: 1,
    dependsOn: ['checkout'],
    startedAt: '2024-01-15T10:00:05Z',
    finishedAt: '2024-01-15T10:00:32Z',
  },
  {
    name: 'lint',
    status: 'succeeded',
    execType: 'run',
    wave: 2,
    dependsOn: ['install-deps'],
    startedAt: '2024-01-15T10:00:32Z',
    finishedAt: '2024-01-15T10:00:48Z',
  },
  {
    name: 'unit-tests',
    status: 'succeeded',
    execType: 'run',
    wave: 2,
    dependsOn: ['install-deps'],
    startedAt: '2024-01-15T10:00:32Z',
    finishedAt: '2024-01-15T10:01:15Z',
  },
  {
    name: 'integration-tests',
    status: 'running',
    execType: 'run',
    wave: 2,
    dependsOn: ['install-deps'],
    startedAt: '2024-01-15T10:00:32Z',
  },
  {
    name: 'build',
    status: 'pending',
    execType: 'run',
    wave: 3,
    dependsOn: ['lint', 'unit-tests', 'integration-tests'],
  },
  {
    name: 'docker-push',
    status: 'pending',
    execType: 'run',
    wave: 4,
    dependsOn: ['build'],
  },
  {
    name: 'deploy-staging',
    status: 'pending',
    execType: 'run',
    wave: 5,
    dependsOn: ['docker-push'],
  },
  {
    name: 'smoke-tests',
    status: 'pending',
    execType: 'run',
    wave: 6,
    dependsOn: ['deploy-staging'],
  },
  {
    name: 'gate-prod',
    status: 'pending',
    execType: 'gate',
    wave: 7,
    dependsOn: ['smoke-tests'],
  },
  {
    name: 'deploy-prod',
    status: 'pending',
    execType: 'run',
    wave: 8,
    dependsOn: ['gate-prod'],
  },
]

export const mockStepLogs: Record<string, string> = {
  checkout: `[10:00:00] Cloning repository acme/api-gateway...
[10:00:01] Fetching ref main (a3f8c21)
[10:00:03] Submodule init: 0 submodules
[10:00:04] Checkout complete in 4.2s
[10:00:05] HEAD is now at a3f8c21 fix: rate limiter race condition`,
  'install-deps': `[10:00:05] Running: go mod download
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
[10:01:15] PASS (43.1s) — 142 tests, 0 failures`,
  'integration-tests': `[10:00:32] Starting postgres:16-alpine...
[10:00:38] Starting redis:7-alpine...
[10:00:42] Waiting for services to be healthy...
[10:00:48] Running: go test -tags=integration ./...
[10:01:02] ok  	acme/api-gateway/tests/integration  14.2s
[10:01:15] `,
}

export const dashboardStats = {
  totalRuns: 1247,
  successRate: 94.2,
  pendingGates: 3,
  activeProjects: 6,
  runsToday: 23,
  avgDuration: '2m 48s',
}
