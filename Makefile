.PHONY: setup dev build test clean snapshot release docker web

# Development
setup:
	chmod +x scripts/setup.sh
	./scripts/setup.sh
	cd web && npm install

dev:
	source .env 2>/dev/null || true && go run ./cmd/server

dev-web:
	cd web && npm run dev

# Build web dashboard
web:
	@echo "Building web dashboard..."
	cd web && npm run build
	@echo "Copying dist to internal/web..."
	rm -rf internal/web/dist
	cp -r web/dist internal/web/

# Build (includes web dashboard)
build: web
	go build -o bin/server ./cmd/server
	go build -o bin/shipit ./cmd/shipit

# Build without web (for CLI-only builds)
build-cli:
	go build -o bin/shipit ./cmd/shipit

# Install CLI locally
install-cli:
	go build -o $(GOPATH)/bin/shipit ./cmd/shipit

# Database
db-up:
	docker compose up -d postgres

db-down:
	docker compose down

db-reset:
	docker compose down -v
	docker compose up -d postgres
	sleep 3
	PGPASSWORD=shipit psql -h localhost -U shipit -d shipit -f migrations/001_initial.sql

# Testing
test:
	go test ./...

# Clean
clean:
	rm -rf bin/ dist/
	rm -rf web/node_modules web/dist
	rm -rf internal/web/dist
	docker compose down -v

# Release (requires goreleaser and GITHUB_TOKEN)
snapshot:
	goreleaser release --snapshot --clean

release:
	goreleaser release --clean

# Docker
docker: web
	docker build -t shipit:latest .
