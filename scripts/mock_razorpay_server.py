#!/usr/bin/env python3
"""
Lightweight Razorpay Webhook Server in Python.
Accepts live or simulated Razorpay webhooks, runs the recovery agent,
and appends to the audit trail.
"""

import hmac
import hashlib
import json
import os
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler
from datetime import datetime, timezone

ROOT_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
AUDIT_LOG = os.path.join(ROOT_DIR, "data", "audit.jsonl")
SECRET = os.environ.get("RAZORPAY_WEBHOOK_SECRET", "")
PORT = int(os.environ.get("PORT", 8080))

class WebhookHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path != "/webhook":
            self.send_response(404)
            self.end_headers()
            return

        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length)

        # Signature verification if secret is provided
        if SECRET:
            sig = self.headers.get("X-Razorpay-Signature", "")
            expected_sig = hmac.new(SECRET.encode(), body, hashlib.sha256).hexdigest()
            if not hmac.compare_digest(sig, expected_sig):
                print("⚠️  [Webhook] Signature verification failed!")
                self.send_response(401)
                self.end_headers()
                self.wfile.write(b'{"error": "invalid_signature"}')
                return

        try:
            payload = json.loads(body.decode("utf-8"))
        except Exception as e:
            self.send_response(400)
            self.end_headers()
            self.wfile.write(json.dumps({"error": str(e)}).encode())
            return

        event_name = payload.get("event", "unknown")
        payment = payload.get("payload", {}).get("payment", {}).get("entity", {})
        tx_id = payment.get("id", "txn_unknown")
        amount = float(payment.get("amount", 0)) / 100.0
        err_code = payment.get("error_code", "")
        err_desc = payment.get("error_description", "")
        err_reason = payment.get("error_reason", "")

        print(f"\n📥 [Webhook Received] Event: {event_name} | Txn: {tx_id} | Amount: ₹{amount:.2f}")

        # 1. Classification
        code_upper = err_code.upper()
        if "RISK" in code_upper or "FRAUD" in code_upper:
            cause = "risk_block"
        elif "AUTH" in code_upper or "OTP" in code_upper:
            cause = "invalid_credentials"
        elif "EXPIRED" in code_upper:
            cause = "expired_card"
        elif "NSF" in code_upper or "INSUFFICIENT" in code_upper:
            cause = "insufficient_funds"
        elif "TIMED_OUT" in code_upper or "TIMEOUT" in code_upper:
            cause = "gateway_timeout"
        elif "CONN" in code_upper or "NETWORK" in code_upper:
            cause = "network_error"
        elif "DECLINE" in code_upper or "DO_NOT_HONOR" in code_upper:
            cause = "issuer_decline"
        else:
            cause = "unknown"

        print(f"🔍 [Classifier] Diagnosed Root Cause: {cause} (from code: '{err_code}')")

        # 2. Decision Engine & Stopping Rules
        stopping_rule = None
        if cause in ["risk_block", "invalid_credentials"]:
            action = "escalate_human"
            stopping_rule = "non_retryable_compliance_escalation"
        elif cause in ["gateway_timeout", "network_error"]:
            action = "retry_immediate"
        elif cause == "issuer_decline":
            action = "retry_delayed"
        elif cause == "insufficient_funds":
            action = "retry_delayed"
        elif cause == "expired_card":
            action = "send_update_card_link"
        else:
            action = "escalate_human"
            stopping_rule = "unknown_cause_review"

        rule_str = f" [Rule: {stopping_rule}]" if stopping_rule else ""
        print(f"⚖️  [Decision Engine] Decided Action: {action}{rule_str}")

        # 3. Simulated Execution
        if action in ["retry_immediate", "retry_delayed"]:
            exec_status = "success"
            amount_recovered = amount
        else:
            exec_status = "success"
            amount_recovered = 0.0

        print(f"⚡ [Action Executor] Executed: {exec_status} | Recovered: ₹{amount_recovered:.2f}")

        # 4. Audit Log append
        now_iso = datetime.now(timezone.utc).isoformat()
        with open(AUDIT_LOG, "a") as f:
            f.write(json.dumps({
                "audit_id": f"aud_hook_{tx_id}",
                "transaction_id": tx_id,
                "stage": "full_pipeline",
                "payload": {
                    "event": event_name,
                    "root_cause": cause,
                    "action": action,
                    "stopping_rule": stopping_rule,
                    "amount_recovered": amount_recovered
                },
                "timestamp": now_iso
            }) + "\n")

        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        resp = {
            "status": "processed",
            "transaction_id": tx_id,
            "root_cause": cause,
            "action": action,
            "stopping_rule": stopping_rule,
            "amount_recovered": amount_recovered
        }
        self.wfile.write(json.dumps(resp, indent=2).encode())

    def log_message(self, format, *args):
        pass # Clean stdout

if __name__ == "__main__":
    os.makedirs(os.path.dirname(AUDIT_LOG), exist_ok=True)
    server = HTTPServer(("0.0.0.0", PORT), WebhookHandler)
    print(f"🚀 AI Revenue Recovery Agent - Razorpay Webhook Server running on http://localhost:{PORT}")
    print(f"   Webhook Endpoint: http://localhost:{PORT}/webhook")
    print("   Press Ctrl+C to stop.\n")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nServer stopped.")
