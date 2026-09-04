# Event Schema & Contracts

**Freeze this doc before anyone writes service code.** Every team builds against these shapes and mocks whatever they don't own yet. Changes after Phase 0 are a group decision — they break someone else's mock.

---

## 1. Raw Payment Event (Data Generator → Ingestion)

```json
{
  "event_id": "uuid",
  "transaction_id": "string",
  "merchant_id": "string",
  "event_type": "payment_initiated | payment_failed | payment_success | payment_timeout | retry_attempted",
  "amount": 4999.00,
  "currency": "INR",
  "payment_method": "card | upi | netbanking | wallet | emandate",
  "gateway_response_code": "string",
  "gateway_message": "string",
  "timestamp": "2026-08-26T10:15:30Z",
  "attempt_number": 1,
  "customer_id": "string",
  "metadata": {}
}
```

## 2. Normalized Event (Ingestion → Classifier)

Same shape as above, plus:

```json
{
  "normalized_at": "ISO8601",
  "is_duplicate": false,
  "schema_version": "1.0"
}
```

## 3. Classification Record (Classifier → Decision Engine)

```json
{
  "event_id": "uuid (refs raw event)",
  "transaction_id": "string",
  "root_cause": "issuer_decline | insufficient_funds | expired_card | gateway_timeout | network_error | risk_block | invalid_credentials | unknown",
  "confidence": 0.87,
  "classified_at": "ISO8601",
  "classifier_version": "string"
}
```

### Root-cause taxonomy

| Cause | Typical signal | Auto-retryable? |
|---|---|---|
| `issuer_decline` | Gateway code = soft decline | Yes, with cooldown |
| `insufficient_funds` | Gateway code = NSF | Yes, with longer cooldown |
| `expired_card` | Gateway code = expired/invalid card | No — needs customer action |
| `gateway_timeout` | No response within SLA | Yes, immediate retry once |
| `network_error` | Connection-level failure | Yes, immediate retry once |
| `risk_block` | Fraud/risk engine flag | **No — always escalate** |
| `invalid_credentials` | Auth failure, bad OTP/mandate | **No — always escalate** |
| `unknown` | Doesn't match a rule | No — escalate for review |

## 4. Decision Record (Decision Engine → Executor)

```json
{
  "transaction_id": "string",
  "root_cause": "string",
  "action": "retry_immediate | retry_delayed | send_update_card_link | escalate_human | no_action | halt_merchant",
  "action_params": { "delay_seconds": 900, "retry_count": 1 },
  "decided_at": "ISO8601",
  "stopping_rule_triggered": "string | null"
}
```

### Stopping rules (owned by Decision Engine)

- **Max retries per transaction:** 3. Beyond that → `escalate_human`, never a 4th retry.
- **Cooldowns by cause:** `gateway_timeout`/`network_error` → retry once immediately, then 5 min; `issuer_decline` → 15 min; `insufficient_funds` → 60 min.
- **Never auto-retry:** `risk_block`, `invalid_credentials` → always `escalate_human`, regardless of attempt count.
- **Merchant circuit breaker:** if a merchant's failure rate exceeds 40% in a rolling 15-minute window → `halt_merchant` for all its transactions, escalate the merchant itself for human review. This is what "compliant escalation" in the brief is scoring.

## 5. Action Result (Executor → Audit)

```json
{
  "transaction_id": "string",
  "action": "string",
  "action_result": "success | failed | skipped",
  "amount_recovered": 4999.00,
  "executed_at": "ISO8601"
}
```

## 6. Audit Log Entry (append-only, everyone writes to this)

```json
{
  "audit_id": "uuid",
  "transaction_id": "string",
  "merchant_id": "string",
  "stage": "ingest | classify | decide | execute",
  "payload": { "...": "the record from that stage" },
  "timestamp": "ISO8601"
}
```

One row per stage per transaction — this is what makes the pipeline replayable and is the source of truth for both the demo dashboard and the metrics evaluator.

## 7. Ground Truth (Data Generator only — never fed to the pipeline)

```json
{
  "transaction_id": "string",
  "true_cause": "string",
  "was_recoverable": true,
  "true_recovery_amount": 4999.00
}
```

## 8. Metrics definitions (Batch Evaluator)

| Metric | Formula |
|---|---|
| Recovery rate | `sum(amount_recovered) / sum(amount at risk that was_recoverable=true)` |
| False-retry cost | `count(retries attempted where was_recoverable=false)` × assumed cost-per-retry |
| Time-to-recovery | `avg(executed_at - first payment_failed timestamp)` for recovered transactions |
| Classification precision/recall | Compare `root_cause` in classification records vs `true_cause` in ground truth, per bucket |
