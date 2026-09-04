# Phased Development Plan

Budget assumed: **~3 days**, matching the earlier effort estimate. Adjust the hour counts if your actual window is shorter or longer — the phase order and dependencies stay the same regardless of team size.

## Team tracks (4-way split)

| Track | Owns | Primary skills needed |
|---|---|---|
| **A — Data & Contracts** | Event schema, synthetic data generator, ingestion service | Go, data modeling |
| **B — Classification** | Root-cause rule engine, classifier service | Go, some rules/ML if time allows |
| **C — Decision & Action** | Decision engine, stopping rules, action executor | Go, the "agent logic" itself |
| **D — Audit, Metrics & Demo** | Audit store, batch evaluator, dashboard/CLI, video + diagram | Go, some frontend/CLI, storytelling for the video |

If you have fewer than 4 people, merge A+B and C+D — the schema doc is what makes either split work.

---

## Phase 0 — Contracts & Setup (half day, all-hands)

Everyone in the room. Do not split up before this is done.

- Walk through and finalize `03-event-schema-contracts.md` as a group — this is the actual first deliverable.
- Agree on the root-cause taxonomy and stopping rules (these are product decisions, not just code).
- Set up the repo skeleton per `02-repo-structure.md`, shared `go.mod`, `docker-compose.yml` with Redis/Kafka running locally.
- Each track writes a **stub/mock** for what it will *consume* from the previous stage, so nobody blocks on anybody starting tomorrow.

**Done when:** schema doc is frozen, repo skeleton exists, everyone can run `docker-compose up` and get a working Redis/Kafka locally.

---

## Phase 1 — Parallel Build (day 1, full day)

Each track builds against the schema and their own mocks, independently.

- **A:** Data generator producing realistic event batches + ground truth; ingestion service validating/deduping against real generator output.
- **B:** Classifier consuming mocked normalized events (hand-written JSON fixtures matching the schema), producing classification records.
- **C:** Decision engine consuming mocked classification records; executor consuming mocked decision records. Stopping-rule logic gets unit tests here — this is the highest-value logic in the whole project, worth the extra hour.
- **D:** Audit store schema + writer; batch evaluator skeleton reading from audit store; start scaffolding the dashboard.

**Sync point (end of day 1, 30 min):** each track demos their piece running standalone against fixtures. Confirm nobody's actual output has drifted from the schema doc.

**Done when:** all four services run independently and produce output matching the schema, against mocked input.

---

## Phase 2 — Integration (day 2, morning–midday)

- Wire real streams between services: A → B → C → D, replacing mocked fixtures with the real upstream service's output.
- Run a small batch end-to-end (10–50 transactions) and watch it flow through the whole pipeline.
- Fix the inevitable schema-edge-case mismatches here — this is expected, not a sign anything went wrong in Phase 1.

**Sync point (midday):** full pipeline runs end-to-end on a small batch without manual intervention.

---

## Phase 3 — Hardening (day 2, afternoon–evening)

- Scale the synthetic batch up (hundreds of transactions), include edge cases: ambiguous causes, merchants that should trip the circuit breaker, transactions that exhaust their retry cap.
- Verify stopping rules actually fire correctly under the bigger batch — this is what the judges will scrutinize hardest ("strictly bounded," "compliant escalation").
- Batch evaluator produces real numbers: recovery rate, false-retry cost, time-to-recovery, classification precision/recall.
- Dashboard/CLI shows a legible walkthrough of a single transaction's journey (event → cause → decision → action → outcome) — this is what makes the 5-minute video land.

**Sync point (end of day 2):** metrics are real and reproducible; a second run on the same data gives the same numbers.

---

## Phase 4 — Demo Prep (day 3, half day)

- Architecture diagram cleanup (reuse `01-architecture.md`'s mermaid diagram, export if needed).
- Record the 5-minute video: feed a batch in, narrate what the classifier/decision engine are doing, show the metrics at the end, show one edge case (e.g. circuit breaker firing) explicitly since that's the "compliant escalation" proof point.
- README polish: how to run it in one command (`make demo-data && make run && make batch`).
- Final pass on the audit trail — make sure a judge could open it and independently verify a claimed recovery.

**Done when:** repo runs from a clean clone with one command, video is recorded, docs are current.

---

## Dependency graph

```
Phase 0 (all-hands)
   |
   v
Phase 1 (A, B, C, D build in parallel against mocks)
   |
   v
Phase 2 (integrate A→B→C→D on real streams)
   |
   v
Phase 3 (scale up, harden stopping rules, real metrics)
   |
   v
Phase 4 (video, diagram, README — all-hands again)
```

Only Phase 0 and Phase 4 need everyone in the same room at the same time. Phases 1–3 are where the parallelism actually pays off.
