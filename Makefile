.PHONY: sync-data test-local stop-local test

sync-data:
	./scripts/sync_prod_data.sh

test-local:
	docker compose -f docker-compose.local.yml up --build -d
	@echo "Local container starting on http://localhost:8085"

stop-local:
	docker compose -f docker-compose.local.yml down

test:
	go test -v ./...
