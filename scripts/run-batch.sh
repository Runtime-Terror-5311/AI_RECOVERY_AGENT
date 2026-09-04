#!/usr/bin/env bash
set -euo pipefail

echo "==> 1. Generating noisy synthetic batch and ground truth..."
go run ./cmd/datagen --count=100 --noise-rate=0.15 \
  --out-events=data/synthetic/batch-01.json \
  --out-ground-truth=data/ground-truth/batch-01.json

echo "==> 2. Ingesting, validating, and state-indexing events..."
go run ./cmd/ingest \
  --input=data/synthetic/batch-01.json \
  --output=data/synthetic/batch-01-normalized.json \
  --audit-log=data/audit.jsonl \
  --state-file=data/state-snapshot.json

echo "==> 3. Running ML root-cause classification with abstention threshold..."
go run ./cmd/classifier \
  --input=data/synthetic/batch-01-normalized.json \
  --output=data/synthetic/batch-01-classified.json \
  --model=ml \
  --abstain-threshold=0.45 \
  --audit-log=data/audit.jsonl

echo "==> 4. Optimizing Expected Net Value (ENV) decisions with safety policies..."
go run ./cmd/decision \
  --input=data/synthetic/batch-01-classified.json \
  --raw-events=data/synthetic/batch-01.json \
  --output=data/synthetic/batch-01-decisions.json \
  --audit-log=data/audit.jsonl \
  --state-file=data/state-snapshot.json

echo "==> 5. Executing calibrated probabilistic actions..."
go run ./cmd/executor \
  --input=data/synthetic/batch-01-decisions.json \
  --raw-events=data/synthetic/batch-01.json \
  --ground-truth=data/ground-truth/batch-01.json \
  --output=data/synthetic/batch-01-actions.json \
  --audit-log=data/audit.jsonl \
  --state-file=data/state-snapshot.json

echo "==> 6. Evaluating multi-dimensional honest metrics against ground truth..."
go run ./cmd/batch-eval \
  --ground-truth=data/ground-truth/batch-01.json \
  --actions=data/synthetic/batch-01-actions.json \
  --decisions=data/synthetic/batch-01-decisions.json \
  --classifications=data/synthetic/batch-01-classified.json \
  --raw-events=data/synthetic/batch-01.json \
  --out-report=data/evaluation-report.json

echo "==> Batch run completed successfully!"
