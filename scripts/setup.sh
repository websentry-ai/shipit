#!/bin/bash
set -e

echo "=== ShipIt Setup ==="

# Check if postgres is running
if ! docker compose ps | grep -q "postgres.*running"; then
    echo "Starting PostgreSQL..."
    docker compose up -d postgres
    sleep 3
fi

# Run migrations
echo "Running migrations..."
PGPASSWORD=shipit psql -h localhost -p 5433 -U shipit -d shipit -f migrations/001_initial.sql 2>/dev/null || true

# Generate encryption key if not set
if [ -z "$ENCRYPT_KEY" ]; then
    ENCRYPT_KEY=$(openssl rand -hex 32)
    echo ""
    echo "Generated ENCRYPT_KEY (save this!):"
    echo "$ENCRYPT_KEY"
    echo ""
    echo "export ENCRYPT_KEY=$ENCRYPT_KEY" >> .env
fi

# Generate initial API token
TOKEN=$(openssl rand -hex 32)
TOKEN_HASH=$(echo -n "$TOKEN" | shasum -a 256 | cut -d' ' -f1)

echo "Creating initial API token..."
PGPASSWORD=shipit psql -h localhost -p 5433 -U shipit -d shipit -c \
    "INSERT INTO api_tokens (name, token_hash) VALUES ('default', '$TOKEN_HASH') ON CONFLICT DO NOTHING;" 2>/dev/null || true

echo ""
echo "=== Setup Complete ==="
echo ""
echo "API Token (save this!):"
echo "$TOKEN"
echo ""
echo "Configure CLI:"
echo "  shipit config set-url http://localhost:8080"
echo "  shipit config set-token $TOKEN"
echo ""
echo "Start server:"
echo "  source .env && go run ./cmd/server"
