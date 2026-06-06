package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMockPipelines validates that the YAML examples we use in the frontend
// mock data are valid Flint pipelines. This catches drift between the
// parser/spec and the mock data.

func TestMockPipeline_GoMicroserviceCI(t *testing.T) {
	yaml := `
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
      key: "go-mod-placeholder"
      paths: [/go/pkg/mod]

  - name: lint
    dependsOn: [install-deps]
    run: golangci-lint run --timeout 5m ./...

  - name: unit-tests
    dependsOn: [install-deps]
    run: go test -race -count=1 -coverprofile=coverage.out ./...

  - name: integration-tests
    dependsOn: [install-deps]
    run: go test -tags=integration -timeout 10m ./...
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
    run: CGO_ENABLED=0 go build -trimpath -o bin/server ./cmd/server

  - name: docker-push
    dependsOn: [build]
    image: docker:24
    run: docker build -t ghcr.io/acme/server:latest .
    secrets:
      REGISTRY_TOKEN: ghcr-push-token

  - name: deploy-staging
    dependsOn: [docker-push]
    environments: [staging]
    run: helm upgrade --install server ./charts/server

  - name: smoke-tests
    dependsOn: [deploy-staging]
    run: ./scripts/smoke-test.sh
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
    run: helm upgrade --install server ./charts/server --set env=prod
`
	p, err := Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Len(t, p.Steps, 10)
	assert.Equal(t, "run", p.Steps[0].ExecType())
	assert.Equal(t, "gate", p.Steps[8].ExecType())

	result := Validate([]byte(yaml), ValidateOptions{})
	assert.True(t, result.Valid(), "errors: %v", result.Errors())
}

func TestMockPipeline_NestedSteps(t *testing.T) {
	yaml := `
image: node:20-alpine

triggers:
  push:
    branches: [main]

steps:
  - name: setup
    steps:
      - name: install
        run: npm ci
      - name: generate
        run: npm run codegen

  - name: quality
    dependsOn: [setup]
    steps:
      - name: lint
        run: npm run lint
      - name: typecheck
        run: npx tsc --noEmit
      - name: test
        run: npm test

  - name: build
    dependsOn: [quality]
    run: npm run build
    env:
      NODE_ENV: production

  - name: deploy
    dependsOn: [build]
    run: npx wrangler deploy
`
	p, err := Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Len(t, p.Steps, 4)
	assert.Equal(t, "steps", p.Steps[0].ExecType())
	assert.Len(t, p.Steps[0].Steps, 2)
	assert.Equal(t, "steps", p.Steps[1].ExecType())
	assert.Len(t, p.Steps[1].Steps, 3)

	result := Validate([]byte(yaml), ValidateOptions{})
	assert.True(t, result.Valid(), "errors: %v", result.Errors())
}

func TestMockPipeline_MatrixBuild(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
  pull_request:
    branches: [main]

steps:
  - name: test
    image: "node:20"
    run: npm test
    matrix:
      os: [ubuntu, alpine]
      node: ["18", "20", "22"]

  - name: build
    dependsOn: [test]
    run: npm run build
`
	p, err := Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Len(t, p.Steps, 2)
	assert.NotNil(t, p.Steps[0].Matrix)
	assert.Len(t, p.Steps[0].Matrix["os"], 2)
	assert.Len(t, p.Steps[0].Matrix["node"], 3)

	result := Validate([]byte(yaml), ValidateOptions{})
	assert.True(t, result.Valid(), "errors: %v", result.Errors())
}

func TestMockPipeline_UseTemplate(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]

steps:
  - name: setup-aws
    use: ./fragments/aws-login.yaml
    with:
      region: us-east-1
      role: deploy-role

  - name: deploy
    dependsOn: [setup-aws]
    run: cdk deploy --all
`
	p, err := Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Len(t, p.Steps, 2)
	assert.Equal(t, "use", p.Steps[0].ExecType())
	assert.Equal(t, "./fragments/aws-login.yaml", p.Steps[0].Use)
	assert.Equal(t, "us-east-1", p.Steps[0].With["region"])
}

func TestMockPipeline_TerraformWithGate(t *testing.T) {
	yaml := `
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

steps:
  - name: init
    run: terraform init -backend-config=backend.hcl
    cache:
      key: "tf-providers-placeholder"
      paths: [/workspace/.terraform]

  - name: validate
    dependsOn: [init]
    run: terraform validate

  - name: plan
    dependsOn: [validate]
    run: terraform plan -out=tfplan -no-color

  - name: approve-apply
    dependsOn: [plan]
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
      AWS_SECRET_ACCESS_KEY: aws-infra-secret
`
	p, err := Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Len(t, p.Steps, 5)
	assert.Equal(t, "gate", p.Steps[3].ExecType())
	assert.Equal(t, 1, p.Steps[3].Gate.MinApprovals)

	result := Validate([]byte(yaml), ValidateOptions{})
	assert.True(t, result.Valid(), "errors: %v", result.Errors())
}

func TestMockPipeline_NestedWithMatrix(t *testing.T) {
	// Mirrors p-3 ci.yaml in the frontend mocks
	yaml := `
image: node:20-alpine

triggers:
  push:
    branches: [main]
  pull_request:
    branches: [main]

steps:
  - name: setup
    steps:
      - name: install
        run: npm ci --ignore-scripts
      - name: codegen
        run: npm run generate:api-types

  - name: quality
    dependsOn: [setup]
    steps:
      - name: lint
        run: npm run lint -- --max-warnings 0
      - name: typecheck
        run: npx tsc --noEmit --pretty
      - name: stylelint
        run: npx stylelint "src/**/*.css"

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

  - name: deploy-preview
    dependsOn: [build]
    environments: [staging]
    run: bunx wrangler pages deploy dist/
    secrets:
      CLOUDFLARE_API_TOKEN: cf-deploy-token
`
	p, err := Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Len(t, p.Steps, 5)
	assert.Equal(t, "steps", p.Steps[0].ExecType())
	assert.Equal(t, "steps", p.Steps[1].ExecType())
	assert.NotNil(t, p.Steps[2].Matrix) // test has matrix
	assert.Len(t, p.Steps[2].Matrix["browser"], 3)
}

func TestMockPipeline_UseTemplateWithPromotion(t *testing.T) {
	// Mirrors p-5 ci.yaml in the frontend mocks
	yaml := `
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
  - name: ecr-login
    use: ./fragments/ecr-login.yaml
    with:
      region: us-east-1
      registry: "123456789.dkr.ecr.us-east-1.amazonaws.com"

  - name: install-deps
    run: go mod download

  - name: checks
    dependsOn: [install-deps]
    steps:
      - name: lint
        run: golangci-lint run --timeout 5m ./...
      - name: gosec
        run: gosec -fmt json ./...

  - name: test
    dependsOn: [install-deps]
    run: go test -race ./...
    services:
      - name: postgres
        image: postgres:16-alpine
        env:
          POSTGRES_DB: auth_test
          POSTGRES_PASSWORD: test
    outputs:
      - path: /workspace/coverage.out

  - name: build
    dependsOn: [checks, test]
    run: go build -o bin/server ./cmd/server
    outputs:
      - path: /workspace/bin

  - name: docker-push
    dependsOn: [build, ecr-login]
    image: docker:24
    run: docker build -t server:latest .
    inputs:
      - from: build
        path: /workspace/bin

  - name: deploy
    dependsOn: [docker-push]
    run: helm upgrade --install server ./charts/server

  - name: e2e-tests
    dependsOn: [deploy]
    run: ./scripts/e2e-auth.sh
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
      minApprovals: 2
`
	p, err := Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Len(t, p.Steps, 9)
	assert.Equal(t, "use", p.Steps[0].ExecType())   // template
	assert.Equal(t, "steps", p.Steps[2].ExecType()) // nested
	assert.Len(t, p.Steps[3].Services, 1)           // postgres service
	assert.Len(t, p.Steps[3].Outputs, 1)            // artifact output
	assert.Len(t, p.Steps[5].Inputs, 1)             // artifact input
	assert.Equal(t, "gate", p.Steps[8].ExecType())  // approval gate
	assert.Equal(t, 2, p.Steps[8].Gate.MinApprovals)
	assert.Len(t, p.Triggers.Promotion, 1) // promotion trigger
}

func TestMockPipeline_PromotionTrigger(t *testing.T) {
	yaml := `
environments: [staging, production]

triggers:
  push:
    branches: [main]
    environments: [staging]
  promotion:
    - from: staging
      environments: [production]

steps:
  - name: deploy
    run: helm upgrade --install myapp ./charts/myapp

  - name: verify
    dependsOn: [deploy]
    run: ./scripts/health-check.sh
    timeout: 5m
`
	p, err := Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Len(t, p.Triggers.Promotion, 1)
	assert.Equal(t, "staging", p.Triggers.Promotion[0].From)
}
