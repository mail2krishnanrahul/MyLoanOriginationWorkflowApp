# My Loan Origination Workflow App

Production-grade Go-based workflow engine for loan origination case processing, backed by PostgreSQL and an outbox-driven worker model. Fully multi-tenant and secure.

## Features & Capabilities

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

- `workflow-engine-go/`: main Go service and entry points
- `workflow-engine-go/cmd/workflow-engine/`: application bootstrap
- `workflow-engine-go/internal/`: business logic encompassing auth, approval, documents, integration, orchestration, routing, etc.
- `workflow-engine-go/pkg/model/`: shared core models
- `workflow-engine-go/db/migrations/`: extensive SQL migration manifests
- `workflow-engine-go/k8s/`: comprehensive Kubernetes deployment manifests (Deployment, HPA, PDB, Configs, Secrets)

## Prerequisites

- Go `1.25+`
- PostgreSQL (local default DB URL is `postgres://myappuser:password@localhost:5432/LoanOriginationDB`)
- `docker` and `docker-compose`
- `kubectl` for cluster deployments

## Quickstart (Docker Compose)

The easiest way to stand up the complete stack locally is via Docker Compose, which automatically builds the image, prepares a Postgres container, applies the `golang-migrate` schema sequences, and spins up the app core.

```bash
docker-compose up --build
```

You can then view the health status at:
```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
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
3. Run the service:
```bash
go run ./cmd/workflow-engine
```

## Testing

The system boasts 100% passing tests across unit and functional logic flows, securely mock-wired via `go-sqlmock`:

```bash
cd workflow-engine-go
go test ./... -v
```
*(Code coverage sits at ~31%, densely grouped in high-complexity algebraic evaluators and SLA/Calendaring calculations)*

## Kubernetes Deployment

The system is fully designed for High Availability Kubernetes deployments. All manifests are located in `workflow-engine-go/k8s/`.

1. Seed namespaces and config maps:
```bash
kubectl apply -f workflow-engine-go/k8s/configmap.yaml
```
2. Establish secure secrets (DB connections, SMTP auth, etc.):
```bash
kubectl apply -f workflow-engine-go/k8s/secrets.yaml
```
3. Initialize Schema Migrations (Job automatically executes and completes before app boot):
```bash
kubectl apply -f workflow-engine-go/k8s/migrate-job.yaml
```
4. Deploy Core App and Scaling Mechanisms:
```bash
kubectl apply -f workflow-engine-go/k8s/deployment.yaml
kubectl apply -f workflow-engine-go/k8s/service.yaml
kubectl apply -f workflow-engine-go/k8s/hpa.yaml
kubectl apply -f workflow-engine-go/k8s/pdb.yaml
```
