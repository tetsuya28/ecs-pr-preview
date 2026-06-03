# ecs-pr-preview

A CLI tool that creates and deletes per-PR preview environments on AWS ECS (Fargate) + ALB.

## How it works

On `create`:
1. Copies the base Task Definition and updates the app image and environment variables (e.g. `APP_URL`, `SESSION_DOMAIN`).
2. Creates an ALB Target Group for the PR.
3. Adds an ALB Listener Rule (Host header matching `pr-<N>.<BASE_DOMAIN>`) that forwards to the Target Group.
4. Creates (or updates) an ECS Service pointing to the new Task Definition and Target Group.
5. Upserts a Route53 A alias record pointing `pr-<N>.<BASE_DOMAIN>` to the ALB.

On `delete`, the above resources are removed in reverse order.

## Usage

### Prerequisites

- Go 1.26+
- AWS credentials with permissions for ECS, ELBV2, and Route53.
- An existing ECS cluster, ALB, and a Route53 hosted zone.
- ACM certificate covering `*.<BASE_DOMAIN>` attached to the ALB HTTPS listener.

### Install

```bash
go install github.com/tetsuya28/ecs-pr-preview/cmd/pr-preview@latest
```

### Commands

```
pr-preview create --pr-number <N> --image-tag <tag> --ecr-registry <registry>
pr-preview delete --pr-number <N>
```

### Environment variables

#### Required

| Variable | Description | Example |
|---|---|---|
| `ECS_CLUSTER_NAME` | ECS cluster name | `myapp` |
| `ALB_NAME` | Application Load Balancer name | `myapp` |
| `HOSTED_ZONE_ID` | Route53 hosted zone ID | `Z1234567890ABCDEF` |
| `BASE_TASK_DEF` | Task Definition family to copy | `myapp` |
| `PR_RESOURCE_PREFIX` | Prefix for all PR resources (TG / Service / TaskDef) | `myapp-pr` |
| `BASE_DOMAIN` | Base domain for PR URLs | `example.com` |
| `APP_ECR_REPOSITORY` | ECR repository name for the app image | `myapp-app` |

#### Optional (with defaults)

| Variable | Default | Description |
|---|---|---|
| `BASE_SERVICE` | `ECS_CLUSTER_NAME` | ECS Service used to inherit network configuration |
| `PR_SUBDOMAIN_PREFIX` | `pr` | Subdomain prefix (`pr-<N>.<BASE_DOMAIN>`) |
| `APP_CONTAINER_NAME` | `app` | Container name in the Task Definition to update the image |
| `LB_CONTAINER_NAME` | `nginx` | Container name registered to the ALB Target Group |
| `LB_CONTAINER_PORT` | `80` | Port of `LB_CONTAINER_NAME` |
| `HEALTH_CHECK_PATH` | `/healthz` | ALB health check path |
| `ENV_OVERRIDES` | _(none)_ | Comma-separated `KEY=template` pairs that rewrite Task Definition env vars. Placeholders: `{pr_url}` (PR URL with scheme), `{pr_domain}` (PR hostname only). Literal values are passed through as-is. Example: `APP_URL={pr_url},MY_DOMAIN={pr_domain},DEBUG=false` |

#### Notification (optional)

| Variable | Description |
|---|---|
| `SLACK_WEBHOOK_URL` | Slack Incoming Webhook URL for step-by-step notifications |
| `GITHUB_TOKEN` | GitHub token for PR comments (requires `pull_requests: write`) |
| `GITHUB_REPOSITORY` | Repository in `owner/repo` format |
| `PR_NUMBER` | PR number (used by the GitHub commenter) |

### GitHub Actions example

```yaml
jobs:
  create-preview:
    if: contains(github.event.pull_request.labels.*.name, 'preview')
    runs-on: ubuntu-latest
    environment: development
    env:
      AWS_REGION: ap-northeast-1
      PR_NUMBER: ${{ github.event.pull_request.number }}
      # Required config
      ECS_CLUSTER_NAME: myapp
      ALB_NAME: myapp
      HOSTED_ZONE_ID: ${{ secrets.ROUTE53_HOSTED_ZONE_ID }}
      BASE_TASK_DEF: myapp
      PR_RESOURCE_PREFIX: myapp-pr
      BASE_DOMAIN: example.com
      APP_ECR_REPOSITORY: myapp-app
      # Notification
      SLACK_WEBHOOK_URL: ${{ secrets.SLACK_WEBHOOK_URL }}
      GITHUB_REPOSITORY: ${{ github.repository }}
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - name: Set AWS credentials
        uses: aws-actions/configure-aws-credentials@v6
        with:
          role-to-assume: ${{ secrets.AWS_ROLE_ARN }}
          aws-region: ${{ env.AWS_REGION }}
      - name: Login to ECR
        id: login-ecr
        uses: aws-actions/amazon-ecr-login@v2
      - name: Create PR environment
        run: |
          go run github.com/tetsuya28/ecs-pr-preview/cmd/pr-preview@latest create \
            --pr-number "${{ env.PR_NUMBER }}" \
            --image-tag "${{ github.event.pull_request.head.sha }}" \
            --ecr-registry "${{ steps.login-ecr.outputs.registry }}"

  delete-preview:
    if: |
      github.event.action == 'closed' ||
      (github.event.action == 'unlabeled' && github.event.label.name == 'preview')
    runs-on: ubuntu-latest
    environment: development
    env:
      AWS_REGION: ap-northeast-1
      PR_NUMBER: ${{ github.event.pull_request.number }}
      ECS_CLUSTER_NAME: myapp
      ALB_NAME: myapp
      HOSTED_ZONE_ID: ${{ secrets.ROUTE53_HOSTED_ZONE_ID }}
      BASE_TASK_DEF: myapp
      PR_RESOURCE_PREFIX: myapp-pr
      BASE_DOMAIN: example.com
      APP_ECR_REPOSITORY: myapp-app
      SLACK_WEBHOOK_URL: ${{ secrets.SLACK_WEBHOOK_URL }}
      GITHUB_REPOSITORY: ${{ github.repository }}
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26"
      - name: Set AWS credentials
        uses: aws-actions/configure-aws-credentials@v6
        with:
          role-to-assume: ${{ secrets.AWS_ROLE_ARN }}
          aws-region: ${{ env.AWS_REGION }}
      - name: Delete PR environment
        run: |
          go run github.com/tetsuya28/ecs-pr-preview/cmd/pr-preview@latest delete \
            --pr-number "${{ env.PR_NUMBER }}"
```
