# Repo Structure

```
revenue-recovery-agent/
├── README.md                     # what the project is, how to run it, demo link
├── docs/
│   ├── 00-README.md
│   ├── 01-architecture.md
│   ├── 02-repo-structure.md
│   ├── 03-event-schema-contracts.md
│   └── 04-phased-development-plan.md
│
├── cmd/                           # one main package per runnable service
│   ├── datagen/                   # synthetic data generator CLI
│   ├── ingest/                    # ingestion service entrypoint
│   ├── classifier/                # classifier service entrypoint
│   ├── decision/                  # decision engine entrypoint
│   ├── executor/                  # action executor entrypoint
│   └── batch-eval/                # batch metrics evaluator CLI
│
├── internal/
│   ├── events/                    # canonical structs + JSON (de)serialization for every contract in 03-event-schema-contracts.md
│   ├── stream/                    # thin wrapper over Redis Streams (or Kafka) — Publisher/Consumer interfaces so the transport is swappable
│   ├── classify/                  # root-cause rule engine (start rule-based; model-backed classifier can implement the same interface later)
│   ├── decide/                    # decision engine logic + all stopping-rule implementations
│   ├── execute/                   # simulated action execution (retry/notify/escalate) + rate limiting
│   ├── audit/                     # audit log writer/reader (SQLite or JSONL backend)
│   └── metrics/                   # recovery rate, false-retry cost, time-to-recovery, precision/recall calculations
│
├── data/
│   ├── synthetic/                 # generated event batches (input to the pipeline)
│   └── ground-truth/              # true cause + true recoverability per transaction, used only by batch-eval
│
├── dashboard/                     # optional CLI/web view of the audit trail for the demo video
│
├── deploy/
│   ├── docker-compose.yml         # local Redis/Kafka + all services, one command to run everything
│   └── k8s/                       # optional, stretch goal
│
├── scripts/
│   └── run-batch.sh               # generate data → run pipeline → run batch-eval → print metrics, in one shot
│
├── go.mod
└── Makefile                       # make run, make test, make batch, make demo-data
```


