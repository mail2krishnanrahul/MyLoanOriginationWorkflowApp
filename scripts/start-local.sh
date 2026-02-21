#!/bin/bash
set -e

# Change to project root directory
cd "$(dirname "$0")/.."

echo "🛑 Stopping existing containers..."
docker-compose down -v

echo "🏗️  Building images..."
docker-compose build --no-cache

echo "📦 Running database migrations..."
# Run the migration binary inside an ephemeral API container
docker-compose run --rm -e MIGRATIONS_DIR=/db/migrations api /app/migrate up

echo "🌱 Seeding database..."
# Pipe seed script into ephemeral postgres container pointing to host
cat workflow-engine-go/scripts/seed-database.sql | docker run -i --rm postgres:16-alpine psql "postgres://myappuser:password@host.docker.internal:5432/LoanOriginationDB"

echo "🚀 Starting remaining backend and frontend services..."
docker-compose up -d

echo "✅ System is running!"
echo ""
echo "🌐 Frontend URL: http://localhost:80"
echo "🔧 Backend API:  http://localhost:8080"
echo "📊 Postgres DB:  localhost:5432"
echo ""
echo "📋 View logs via: docker-compose logs -f"
