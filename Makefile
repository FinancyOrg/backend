COMPOSE            ?= podman compose
COMPOSE_FILE       ?= compose.yaml
HTTP_PORT          ?= 8081
LOCAL_DATABASE_URL ?= postgresql://root@localhost:26257/defaultdb?sslmode=disable
LOCAL_MONGO_URI    ?= mongodb://127.0.0.1:27017/financy?directConnection=true

SHELL := /bin/bash

.PHONY: up down tests test clean cleans ps logs

up:
	$(COMPOSE) -f $(COMPOSE_FILE) up -d --build
	@echo "Waiting for backend..."
	@$(wait_for_backend)
	@echo ""
	@echo "Backend is up (live reload):"
	@echo "  API:     http://localhost:$(HTTP_PORT)"
	@echo "  SQL:     $(LOCAL_DATABASE_URL)"
	@echo "  Mongo:   $(LOCAL_MONGO_URI)"
	@echo "  CRDB UI: http://localhost:8080"

down:
	$(COMPOSE) -f $(COMPOSE_FILE) down

ps:
	$(COMPOSE) -f $(COMPOSE_FILE) ps

logs:
	$(COMPOSE) -f $(COMPOSE_FILE) logs -f

# Host go test against local CRDB/Mongo (same as v2-financy). Databases
# come up first; the live backend is not required.
tests test:
	$(COMPOSE) -f $(COMPOSE_FILE) up -d crdb mongo
	@echo "Waiting for CockroachDB..."
	@$(wait_for_crdb)
	@echo "Waiting for MongoDB..."
	@$(wait_for_mongo)
	DATABASE_URL="$(LOCAL_DATABASE_URL)" MONGO_URI="$(LOCAL_MONGO_URI)" go test ./...

clean cleans:
	$(COMPOSE) -f $(COMPOSE_FILE) down -v --remove-orphans
	-@podman rm -f financy-backend financy-crdb financy-mongo >/dev/null 2>&1 || true
	-@podman rmi localhost/financy-backend:dev >/dev/null 2>&1 || true

# First boot of a live backend downloads modules and compiles; allow 3 min.
define wait_for_backend
	i=0; until curl -sf http://localhost:$(HTTP_PORT)/api/health >/dev/null 2>&1; do \
		i=$$((i+1)); [ $$i -ge 180 ] && { echo "Backend did not become ready. See: $(COMPOSE) logs backend"; exit 1; }; sleep 1; \
	done
endef

define wait_for_crdb
	i=0; until podman exec financy-crdb ./cockroach sql --insecure --execute="SELECT 1" >/dev/null 2>&1; do \
		i=$$((i+1)); [ $$i -ge 60 ] && { echo "CockroachDB did not become ready."; exit 1; }; sleep 1; \
	done
endef

define wait_for_mongo
	i=0; until podman exec financy-mongo mongosh --quiet --eval "db.runCommand({ ping: 1 })" >/dev/null 2>&1; do \
		i=$$((i+1)); [ $$i -ge 60 ] && { echo "MongoDB did not become ready."; exit 1; }; sleep 1; \
	done
endef
