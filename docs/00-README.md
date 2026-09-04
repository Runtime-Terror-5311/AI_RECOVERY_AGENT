# AI Revenue Recovery — Payment Degradation → Root Cause → Recovery Action

Razorpay Buildathon, Track 03 (AI Revenue Recovery). This is the planning doc set — read this file first, then jump to whichever doc your team is working from.

## Doc set

| Doc | Purpose | Owner audience |
|---|---|---|
| `01-architecture.md` | System diagram, components, data flow, tech choices | Everyone (read once, first) |
| `02-repo-structure.md` | Folder layout, what lives where | Everyone |
| `03-event-schema-contracts.md` | The exact JSON contracts every service must speak | Everyone — **freeze this before writing code** |
| `04-phased-development-plan.md` | Team split, phases, timeline, sync points | Team leads / whoever's coordinating |

## One-line pitch

An agent pipeline that ingests payment events, diagnoses *why* a payment failed, decides a bounded recovery action, executes it, and proves it worked with measured recovery-rate / false-retry-cost metrics and a full audit trail.

## The bar we're building to (from the track brief)

- **Honest metrics** — recovery rate, false-retry cost, time-to-recovery, computed against a synthetic batch with known ground truth. Not claimed, *measured*.
- **Bounded workflow** — capped retries, cooldowns, a merchant-level circuit breaker. This is explicitly not "retry forever."
- **Compliant escalation** — causes that should never be auto-retried (risk block, invalid credentials) always go to a human queue, never looped.
- **Audit trail** — every decision, every action, every outcome logged and replayable.

## Why the schema doc matters most

The reason four people can build this at once without stepping on each other is that everyone codes *against the contracts in `03-event-schema-contracts.md`*, not against each other's code. Nobody waits on anybody — you mock the input you don't own yet, and swap the mock for the real thing at integration time (Phase 2). If the schema needs to change mid-build, that's a group decision, not a solo one — it breaks someone else's mock.
