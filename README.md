# My Loan Origination Workflow App

Production-grade Go-based workflow engine for loan origination case processing, backed by PostgreSQL and an outbox-driven worker model. Fully multi-tenant and secure.

## Features & Capabilities

- **Deal Ingestion Engine**: Snapshot-driven delta detection and deterministic algorithmic task generation for Business Lending deals based on material changes (OpenAPI integrated). Auto-generates case tags derived from deal structure (complexity, skills, VIP status).
- **Case List with Tags**: Card-based case list UI with color-coded tag chips (complexity, skill, VIP, document errors, exceptions), advanced filtering, SLA indicators, and pagination.
- **Deal 360 View**: Full hierarchical visualisation of deal structure — borrowing entities, facilities, interest/payment schedules, collateral packages, and asset details — loaded from the ingested deal snapshot.
- **GetNext Intelligent Work Distribution**: One-click case assignment engine that scores every allocatable case across 7 weighted factors (SLA risk, skill match, queue age, complexity, business value, affinity, workload) and atomically claims the highest-priority eligible case for the current operator. Includes real-time queue depth indicators, expandable top-3 preview panel, explainable score breakdowns, skip-reason capture, supervisor dashboard, and full Prometheus observability.
- **Strict Multi-Tenancy**: Data isolation at the row level with a mandatory `tenant_id` context scope across all interactions, ensuring enterprise-grade compartmentalisation.
- **Dynamic Task Orchestration**: Robust DAG-based engine orchestrating task lifecycles (Draft → Pending → In-Progress → Done/Failed).
- **Approval & Decision Gates**: Evaluation core supporting complex arithmetic and logic conditionals. Native handling of consensus, majority, and tier-based approval chains.
- **SLA & Escalation Management**: High-fidelity business calendar awareness calculating precise deadlines. Automated background sweepers to handle expiry and assignment escalation.
- **Event-Driven Integration**: Idempotent outbound Webhook engine with dynamic retry backoff policies for deep third-party service integration.
- **Automated Work Assignment**: Managers equipped with robust routing policies (`Round Robin`, `Least Loaded`, `Skill Score`) connecting human capital to impending tasks dynamically.
- **Omnichannel Notifications**: Modular alerting adapters for Email, SMS, and In-App persistent notifications.
- **Secure Document Management**: Adapter-driven persistence with AES-256-GCM encryption at rest capability. Built-in compliance mechanisms for Archival and Retention sweeps.
- **Document Verification Workflow**: Complete workflow for HOME_LOAN_DOC_VERIFICATION cases — intake, classification, QA review, allocation, checklists, error tags, and additional info requests.
- **Exception Sagas**: Automated exception handling, leveraging Saga Compensations and Retries for failure state recovery without manual intervention.
- **Admin Hub**: Admin panel with deal ingestion interface (editable JSON payload, one-click case creation), user management, and team configuration.
- **Custom Theme Switcher**: Six selectable UI themes (Light, Dark, Ocean Breeze, Sunset Ember, Forest Canopy, Arctic Frost) accessible from a topbar dropdown. Themes override CSS variables for backgrounds, panels, borders, sidebar gradients, and body gradients while preserving Tailwind dark-mode compatibility.

## Roles & Permissions

### System Roles

These are the core tenant-scoped roles available across the platform:

| Role Code | Display Name | Description |
|-----------|-------------|-------------|
| `LOAN_OFFICER` | Loan Officer | Frontline originations and document collection. Can create cases, view own cases, claim and complete tasks. |
| `UNDERWRITER` | Underwriter | Credit review and task rejections. Can view all cases, claim/complete/reject tasks. |
| `SENIOR_APPROVER` | Senior Approver | Underwriter with approval authority. Inherits Underwriter permissions plus approval approve/reject/escalate. |
| `SUPERVISOR` | Supervisor | Team oversight and queue management. Inherits Senior Approver permissions plus task/case reassignment, all approval visibility, and operational reporting. |
| `ADMIN` | Administrator | Full systemic governance over a tenant. All 28 permissions including user lifecycle management, role/team management, and tenant configuration. |

### Document Verification Workflow Roles

Specialised roles for the `HOME_LOAN_DOC_VERIFICATION` case type:

| Role Code | Display Name | Description |
|-----------|-------------|-------------|
| `LOAN_OFFICER` | Loan Officer | Handles document collection, case allocation, error tagging, deal structure review, credit memo review, additional info requests, ad-hoc task creation, and QA submission. |
| `QA_OFFICER` | QA Officer | Quality assurance review, approval, and rejection of cases. Can view documents and manage case tags. |
| `TEAM_LEAD` | Team Lead | Team oversight, case allocation to any officer, case prioritisation, document and QA visibility, and reporting access. |
| `BANKER` | Banker | Customer/banker liaison role. Can view own cases and submit additional information. |
| `SUPPORT_OFFICER` | Support Officer | Support team operations. Can view cases, manage tags, and request additional information. |

### Team Member Roles (Within-Team Hierarchy)

| Role | Description |
|------|-------------|
| `MEMBER` | Standard team member |
| `LEAD` | Team lead within a team |
| `MANAGER` | Team manager/administrator |

### Operational Teams

| Team Code | Display Name | Team Type |
|-----------|-------------|-----------|
| `SUPPORT_LEAD_TEAM` | Support Lead Team | PROCESSING |
| `DOC_PREP_TEAM` | Document Preparation Team | PROCESSING |
| `QA_TEAM` | Quality Assurance Team | UNDERWRITING |
| `BANKER_TEAM` | Banker Liaison Team | OPERATIONS |

### Permission Codes (28 total)

| Category | Permissions |
|----------|-------------|
| **Case** | `CASE_CREATE`, `CASE_VIEW_OWN`, `CASE_VIEW_ALL`, `CASE_CANCEL`, `CASE_REASSIGN` |
| **Task** | `TASK_CLAIM`, `TASK_COMPLETE`, `TASK_REJECT`, `TASK_REASSIGN`, `TASK_VIEW_OWN`, `TASK_VIEW_ALL` |
| **Approval** | `APPROVAL_APPROVE`, `APPROVAL_REJECT`, `APPROVAL_ESCALATE`, `APPROVAL_VIEW_ALL` |
| **Reporting** | `REPORT_VIEW`, `REPORT_EXPORT`, `REPORT_OPERATIONAL` |
| **Admin** | `USER_CREATE`, `USER_SUSPEND`, `USER_DEACTIVATE`, `USER_VIEW`, `ROLE_MANAGE`, `TEAM_MANAGE`, `CASETYPE_MANAGE`, `TENANT_CONFIG_MANAGE` |

## Repository Layout

- `frontend/`: React, TypeScript, and Vite-based responsive web interface.
- `workflow-engine-go/`: main Go service, background workers, and entry points.
- `workflow-engine-go/cmd/`: application bootstraps (api, worker, sweeper, dispatcher).
- `workflow-engine-go/internal/`: business logic encompassing auth, approval, documents, integration, orchestration, routing, etc.
- `workflow-engine-go/internal/getnext/`: GetNext intelligent work distribution engine (scoring CTE, claim/preview/skip functions, HTTP handler, Prometheus metrics, unit tests).
- `workflow-engine-go/internal/docverification/`: document verification workflow engine (intake, classification, QA, allocation).
- `workflow-engine-go/internal/integration/`: deal ingestion service with snapshot diffing and deterministic task/tag generation.
- `workflow-engine-go/pkg/model/`: shared core models.
- `workflow-engine-go/db/migrations/`: extensive SQL migration manifests (35 migrations, including GetNext schema).
- `workflow-engine-go/docs/`: OpenAPI specification and Swagger UI.
- `k8s/`: comprehensive Kubernetes deployment manifests (Database, API, Frontend, Workers, Routing).
- `scripts/`: utility scripts for seeding tenants, deals, and test data.

## Prerequisites

- Go `1.24+`
- Node.js `20+` for Frontend Development
- PostgreSQL (local default DB URL is `postgres://myappuser:password@localhost:5432/LoanOriginationDB`)
- `docker` and `docker-compose`
- `kubectl` and a local Kubernetes cluster (e.g. Docker Desktop K8s or Minikube)

## Quickstart (Docker Compose)

The easiest way to stand up the complete stack locally is via Docker Compose, which automatically builds the images and spins up the API, Frontend, and distributed background workers. Make sure PostgreSQL is running locally and is accessible via `host.docker.internal`.

```bash
docker-compose up --build
```

You can then view the health status at:
```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```
And access the Web UI at:
```bash
http://localhost/
```

## Local Development Setup

To run outside of containers:

1. Start PostgreSQL and create the target database.
2. Apply migrations:
```bash
cd workflow-engine-go
migrate -path db/migrations \
  -database "postgres://myappuser:password@localhost:5432/LoanOriginationDB?sslmode=disable" up
```
3. Seed the default tenant:
```bash
go run scripts/seed_tenant.go
```
4. Run the Go service:
```bash
go run ./cmd/api
```
5. Run the Frontend:
```bash
cd frontend
npm install
npm run dev
```
6. (Optional) Seed sample case tags:
```bash
cd workflow-engine-go
go run scripts/seed_case_tags.go
```

## Deal Ingestion

The deal ingestion engine accepts Business Lending deal snapshots and automatically:
- Creates `DOCUMENT_VERIFICATION` cases when the deal status is `DOCUMENT_VERIFICATION`
- Generates deterministic verification tasks based on deal structure (KYC, trust deeds, financials, collateral, guarantees)
- Auto-generates case tags derived from deal characteristics (complexity, required skills, VIP status)
- Detects material changes on subsequent snapshots and generates additional tasks only for changed elements

To ingest a deal via the Admin Hub, navigate to `http://localhost:5173/admin` and use the Deal Ingestion tab. Alternatively, use the API directly:

```bash
curl -X POST http://localhost:8080/api/ingest/deals \
  -H "Content-Type: application/json" \
  -H "X-Tenant-Code: DEFAULT" \
  -H "X-Idempotency-Key: unique-key-here" \
  -d @IngestDealRequirements/SampleJSON.json
```

## GetNext — Intelligent Work Distribution

The **GetNext engine** eliminates manual queue-picking by scoring every `ALLOCATABLE` case in the queue and atomically assigning the highest-priority eligible case to the requesting operator.

### Scoring Algorithm (7 factors)

| Factor | Weight (default) | Range | Description |
|---|---|---|---|
| SLA Risk | 35% | 0–100 | Urgency based on time remaining to `case_due_at` |
| Skill Match | 25% | 0–100 | Overlap between operator's `worker_skills` and case `required_skills` |
| Queue Age | 10% | 0–50 | Hours the case has been in `ALLOCATABLE` status |
| Complexity | 10% | 0–50 | Case complexity tier (`SIMPLE` → `NON_STANDARD`) |
| Business Value | 10% | 0–50 | VIP borrower flag + loan amount bands |
| Affinity | 5% | 0–30 | Whether the operator previously worked this case |
| Workload Penalty | 5% | –50–0 | Penalises operators approaching their capacity limit |

Weights are tenant-configurable via the `getnext_weights` table and loaded with a 5-minute cache.

### API Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/getnext/claim` | Claim the highest-scored eligible case |
| `GET` | `/api/v1/getnext/preview` | Preview top-N cases without claiming (read-only) |
| `POST` | `/api/v1/getnext/skip` | Record a skip with reason (FREE_TEXT, CONFLICT_OF_INTEREST, TOO_COMPLEX, WRONG_SKILL, OTHER) |
| `GET` | `/api/v1/getnext/queue` | Live queue depth, SLA breach count, avg wait time |
| `GET` | `/api/v1/getnext/supervisor` | Full supervisor dashboard (team workloads, stalled cases, priority ranking) |
| `POST` | `/api/v1/getnext/refresh-affinity` | Admin: refresh `case_user_affinity` materialised view |
| `POST` | `/api/v1/getnext/snapshot` | Admin: record a point-in-time queue snapshot |

### Concurrency Safety

Claiming uses `SELECT … FOR UPDATE SKIP LOCKED` inside a transaction, followed by an `UPDATE cases WHERE assigned_user_id IS NULL` optimistic lock. If two operators race for the same case, exactly one wins — the other receives an `ErrCaseAlreadyClaimed` and can retry.

### Prometheus Metrics

| Metric | Type | Description |
|---|---|---|
| `workflow_getnext_claims_total` | Counter | Successful claim operations |
| `workflow_getnext_skips_total` | Counter | Skip operations (labelled by reason) |
| `workflow_getnext_no_eligible_total` | Counter | Empty-queue call count |
| `workflow_getnext_capacity_blocked_total` | Counter | Capacity-blocked call count |
| `workflow_getnext_composite_score` | Histogram | Distribution of scores for claimed cases |
| `workflow_getnext_duration_seconds` | Histogram | End-to-end API call latency |
| `workflow_getnext_queue_depth` | Gauge | Live allocatable unassigned case count |
| `workflow_getnext_queue_sla_breached` | Gauge | SLA-breached cases waiting in queue |

### Frontend UI Components

| Component | Location | Description |
|---|---|---|
| `GetNextButton` | Cases list page header | One-click claim with loading state and error feedback |
| `QueueDepthIndicator` | Cases list page header | Live queue badge; pulses red on SLA breach |
| `GetNextPreviewPanel` | Cases list page | Expandable top-3 scored case preview with per-case claim/skip |
| `ScoreBreakdownTooltip` | Inside preview panel | Expandable 7-factor bar chart + Plain-English explanations |
| `SkipReasonModal` | Inside preview panel | 5-reason radio group with optional notes |
| `CapacityModal` | Triggered on capacity block | Visual gauge showing current vs max active cases |
| `SupervisorQueuePanel` | Supervisor views | Team workloads, stalled cases, highest-priority case ranking |

### Schema Changes (Migration 000035)

```sql
-- New status added to cases.status
ALLOCATABLE

-- New tables
getnext_weights             -- per-tenant scoring configuration
getnext_claims              -- append-only audit log
case_allocation_transitions -- tracks queue entry/exit timestamps
getnext_queue_snapshots     -- point-in-time analytics

-- New materialised view
case_user_affinity          -- pre-computed operator↔case affinity scores
```

Apply: `migrate -path workflow-engine-go/db/migrations -database $DATABASE_URL up`

## API Documentation

OpenAPI specification is available at `/swagger/openapi.yaml` and the Swagger UI at `/swagger/`.

## Testing

The system has passing tests across unit and functional logic flows:

```bash
cd workflow-engine-go
go test ./... -v
```

Frontend tests:
```bash
cd frontend
npx vitest run
```

## Kubernetes Deployment

The system is fully designed for High Availability Kubernetes deployments. All manifests are located in the repository root `k8s/` directory and configure the application (including PostgreSQL) into a deployable topology behind standard ingress controllers.

1. Set your image pull policy inside deployment manifests dependent on whether you intend to build locally or push the images to a container registry.
2. Build the images:
```bash
docker build -t workflow-engine:latest workflow-engine-go
docker build -t workflow-dispatcher:latest -f workflow-engine-go/Dockerfile.dispatcher workflow-engine-go
docker build -t workflow-sweeper:latest -f workflow-engine-go/Dockerfile.sweeper workflow-engine-go
docker build -t workflow-worker:latest -f workflow-engine-go/Dockerfile.worker workflow-engine-go
docker build -t workflow-frontend:latest frontend
```
3. Apply the full manifest suite:
```bash
kubectl apply -f k8s/
```
4. Access the application:
- Frontend UI: `http://localhost/`
- API Backend: `http://localhost/api/`
