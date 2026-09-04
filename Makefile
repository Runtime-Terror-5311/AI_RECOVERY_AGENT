.PHONY: all build test demo-data run batch clean

BIN_DIR := bin
SERVICES := datagen ingest classifier decision executor batch-eval

all: build

build:
	@mkdir -p $(BIN_DIR)
	@for svc in $(SERVICES); do \
		echo "Building $$svc..."; \
		go build -o $(BIN_DIR)/$$svc ./cmd/$$svc || true; \
	done

test:
	go test -v ./internal/...

demo-data:
	@go run ./cmd/datagen --count=50 --out-events=data/synthetic/batch-01.json --out-ground-truth=data/ground-truth/batch-01.json

run:
	docker-compose -f deploy/docker-compose.yml up --build

batch:
	@./scripts/run-batch.sh

clean:
	rm -rf $(BIN_DIR)
	rm -f audit.db audit.db-journal
