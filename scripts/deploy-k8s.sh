#!/bin/bash
set -e

cd "$(dirname "$0")/.."

echo "🏗️  Building Docker images..."
docker build -t workflow-api:latest ./workflow-engine-go
docker build -t workflow-worker:latest ./workflow-engine-go -f workflow-engine-go/Dockerfile.worker
docker build -t workflow-dispatcher:latest ./workflow-engine-go -f workflow-engine-go/Dockerfile.dispatcher
docker build -t workflow-sweeper:latest ./workflow-engine-go -f workflow-engine-go/Dockerfile.sweeper
docker build -t workflow-frontend:latest ./frontend

echo "🔄 Loading images into kind/minikube (Optional: Skip if using external registry)"
# kind load docker-image workflow-api:latest workflow-frontend:latest workflow-worker:latest workflow-dispatcher:latest workflow-sweeper:latest

echo "🚀 Deploying to Kubernetes namespace 'workflow-engine'..."
kubectl apply -f k8s/

echo "⏳ Waiting for deployments to reach ready state..."
kubectl rollout status deployment/api -n workflow-engine --timeout=120s
kubectl rollout status deployment/frontend -n workflow-engine --timeout=120s

echo "📦 Running migrations inside Kubernetes..."
# Note: DATABASE_URL should match the secret in 02-secrets.yaml
kubectl run migrate --rm -i --restart=Never \
  --image=workflow-api:latest \
  --namespace=workflow-engine \
  --env="DATABASE_URL=postgres://workflow:workflow_password@postgres:5432/workflow?sslmode=disable" \
  --env="MIGRATIONS_DIR=/db/migrations" \
  --command -- /app/migrate up

echo "🌱 Seeding K8s database..."
cat workflow-engine-go/scripts/seed-database.sql | kubectl exec -i statefulset/postgres -n workflow-engine -- psql -U workflow -d workflow

echo "✅ Deployment complete!"
echo ""
echo "🌐 Access the app by port forwarding:"
echo "   kubectl port-forward svc/frontend 8080:80 -n workflow-engine"
echo "   Open: http://localhost:8080"
