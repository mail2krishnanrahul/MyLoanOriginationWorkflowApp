# My Loan Origination Workflow App

Go-based workflow engine for loan origination case processing, backed by PostgreSQL and an outbox-driven worker model.

## Repository Layout

- `workflow-engine-go/`: main Go service
- `workflow-engine-go/cmd/workflow-engine/`: service entrypoint
- `workflow-engine-go/internal/engine/`: orchestration, workers, lifecycle logic
- `workflow-engine-go/internal/repository/`: data access layer
- `workflow-engine-go/db/migrations/`: SQL migrations
- `workflow-engine-go/db/seeds/`: seed data
- `workflow-engine-go/Dockerfile`: container build
- `workflow-engine-go/k8s-deployment.yaml`: Kubernetes deployment sample

## Prerequisites

- Go `1.25+` (module targets `go 1.25.0`)
- PostgreSQL (local default DB URL is `postgres://myappuser:password@localhost:5432/LoanOriginationDB`)
- `psql` client
- Optional: `golang-migrate` CLI for migrations

## Configuration

The service supports:

- `DB_URL` (default: `postgres://myappuser:password@localhost:5432/LoanOriginationDB`)
- `WORKER_COUNT` (default: `10`)

## Local Setup

1. Start PostgreSQL and create the target database.
2. Apply migrations:

```bash
migrate -path workflow-engine-go/db/migrations \
  -database "postgres://myappuser:password@localhost:5432/LoanOriginationDB?sslmode=disable" up
```

3. Seed a case type (example):

```bash
psql "postgres://myappuser:password@localhost:5432/LoanOriginationDB?sslmode=disable" \
  -f workflow-engine-go/db/seeds/home_loan_simple_v1.sql
```

4. Run the service:

```bash
cd workflow-engine-go
go run ./cmd/workflow-engine
```

## Service Endpoints

- `GET /healthz` -> liveness check
- `GET /readyz` -> readiness check (verifies DB connectivity)
- `POST /cases` -> create a new case

Example create request:

```bash
curl -X POST http://localhost:8080/cases \
  -H "Content-Type: application/json" \
  -d '{
    "case_type_code": "HOME_LOAN",
    "case_type_version": 0,
    "metadata": {"borrower_id": "B001", "product_id": "FIXED_30"},
    "requested_by": "api-user"
  }'
```

## Testing

Run unit/integration tests:

```bash
cd workflow-engine-go
go test ./...
```

Note: several tests are currently scaffolds and intentionally skipped until a Postgres test harness is wired in.

## Docker

Build and run:

```bash
cd workflow-engine-go
docker build -t workflow-engine:latest .
docker run --rm -p 8080:8080 \
  -e DB_URL="postgres://myappuser:password@host.docker.internal:5432/LoanOriginationDB?sslmode=disable" \
  -e WORKER_COUNT=20 \
  workflow-engine:latest
```

## Kubernetes

Sample manifest is available at:

- `workflow-engine-go/k8s-deployment.yaml`

It expects:

- image `workflow-engine:latest` (replace with your registry/tag)
- secret `workflow-db-secret` with key `connection-string`

## Notes

- Migration/versioning guidance is documented in `workflow-engine-go/db/VERSIONING_POLICY.md`.
- The worker runs background sweepers for SLA urgency, capacity, expiry, and archival cleanup.
