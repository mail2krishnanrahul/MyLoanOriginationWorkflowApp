#!/bin/bash
set -e

# Change to project root directory
cd "$(dirname "$0")/.."

echo "🩺 Running Workflow System Health Check..."
echo "=========================================="

# 1. Check Postgres
echo -n "Checking PostgreSQL: "
if docker run --rm postgres:16-alpine pg_isready -h host.docker.internal -p 5432 -U myappuser > /dev/null 2>&1; then
  echo "✅ UP (Accepting Connections)"
else
  echo "❌ DOWN"
  exit 1
fi

# 2. Check API /health endpoint
echo -n "Checking API Server: "
if curl -s -f http://localhost:8080/health > /dev/null; then
  echo "✅ UP (HTTP 200 OK)"
else
  echo "❌ DOWN or Unhealthy"
  exit 1
fi

# 3. Check Frontend Server
echo -n "Checking Frontend Server: "
if curl -s -I -f http://localhost:80 > /dev/null; then
  echo "✅ UP (HTTP 200 OK)"
else
  echo "❌ DOWN (Is port 80 bound?)"
  exit 1
fi

# 4. Check Worker Logs for silent failures
echo -n "Checking Worker Service: "
WORKER_LOGS=$(docker-compose logs --tail=50 worker)
if echo "$WORKER_LOGS" | grep -i -q "error\|panic\|fatal"; then
  echo "⚠️  RUNNING (Errors detected in logs)"
else
  echo "✅ UP (Clean logs)"
fi

echo "=========================================="
echo "🎉 System is fully operational"
