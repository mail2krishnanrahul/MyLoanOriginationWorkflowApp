#!/bin/bash
set -e

# Change to project root directory
cd "$(dirname "$0")/.."

echo "🛑 Stopping Workflow Engine and removing containers..."
docker-compose down -v

echo "🧹 Cleaning up orphaned volumes and networks..."
docker volume prune -f
docker network prune -f

echo "✅ Local environment stopped and cleaned."
