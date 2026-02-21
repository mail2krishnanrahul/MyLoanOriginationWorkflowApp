# My Loan Origination Workflow App

Production-grade Go-based workflow engine for loan origination case processing, backed by PostgreSQL and an outbox-driven worker model. Fully multi-tenant and secure.

## Features & Capabilities

- **Deal Ingestion Engine**: Snapshots-driven delta detection and deterministic algorithmic task generation for Business Lending deals based on material changes (OpenAPI integrated).
- **Strict Multi-Tenancy**: Data isolation at the row level with a mandatory `tenant_id` context scope across all interactions, ensuring enterprise-grade compartmentalization.
- **Dynamic Task Orchestration**: Robust DAG-based engine orchestrating task lifecycles (Draft → Pending → In-Progress → Done/Failed).
- **Approval & Decision Gates**: Evaluation core supporting complex arithmetic and logic conditionals. Native handling of consensus, majority, and tier-based approval chains.
- **SLA & Escalation Management**: High-fidelity business calendar awareness calculating precise deadlines. Automated background sweepers to handle expiry and assignment escalating.
- **Event-Driven Integration**: Idempotent outbound Webhook engine with dynamic retry backoff policies for deep third-party service integration.
- **Automated Work Assignment**: Managers equipped with robust routing policies (`Round Robin`, `Least Loaded`, `Skill Score`) connecting human capital to impending tasks dynamically.
- **Omnichannel Notifications**: Modular alerting adapters for Email, SMS, and In-App persistent notifications.
- **Secure Document Management**: Adapter-driven persistence with AES-256-GCM encryption at rest capability. Built-in compliance mechanisms for Archival and Retention sweeps.
- **Exception Sagas**: Automated exception handling, leveraging Saga Compensations and Retries for failure state recovery without manual intervention.

## Repository Layout

- `frontend/`: React, TypeScript, and Vite-based responsive web interface.
- `workflow-engine-go/`: main Go service, background workers, and entry points.
- `workflow-engine-go/cmd/`: application bootstraps (api, worker, sweeper, dispatcher).
- `workflow-engine-go/internal/`: business logic encompassing auth, approval, documents, integration, orchestration, routing, etc.
- `workflow-engine-go/pkg/model/`: shared core models.
- `workflow-engine-go/db/migrations/`: extensive SQL migration manifests.
- `k8s/`: comprehensive Kubernetes deployment manifests (Database, API, Frontend, Workers, Routing).

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
2. Apply migrations mapping:
```bash
cd workflow-engine-go
migrate -path db/migrations \
  -database "postgres://myappuser:password@localhost:5432/LoanOriginationDB?sslmode=disable" up
```
3. Run the Go service:
```bash
go run ./cmd/api
```
4. Run the Frontend:
```bash
cd frontend
npm install
npm run dev
```

## Testing

The system boasts passing tests across unit and functional logic flows, securely mock-wired via `go-sqlmock`:

```bash
cd workflow-engine-go
go test ./... -v
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
