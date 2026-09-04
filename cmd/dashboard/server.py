#!/usr/bin/env python3
"""
Lightweight Web Server for AI Revenue Recovery Agent Operator Dashboard.
Serves static dashboard and REST API for live evaluation reports and audit trail.
Includes auto-port fallback and SO_REUSEADDR to prevent 'Address already in use' errors.
"""

import argparse
import json
import os
import sys
import subprocess
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlparse

ROOT_DIR = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
DATA_DIR = os.path.join(ROOT_DIR, "data")
DASHBOARD_DIR = os.path.join(ROOT_DIR, "dashboard")
REPORT_PATH = os.path.join(DATA_DIR, "evaluation-report.json")
AUDIT_PATH = os.path.join(DATA_DIR, "audit.jsonl")

class DashboardHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        parsed = urlparse(self.path)

        if parsed.path == "/" or parsed.path == "/index.html":
            html_path = os.path.join(DASHBOARD_DIR, "index.html")
            if os.path.exists(html_path):
                with open(html_path, "rb") as f:
                    content = f.read()
                self.send_response(200)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.end_headers()
                self.wfile.write(content)
            else:
                self.send_error(404, "Dashboard HTML not found")
            return

        elif parsed.path == "/api/data":
            report = {}
            if os.path.exists(REPORT_PATH):
                with open(REPORT_PATH) as f:
                    report = json.load(f)

            audit_rows = []
            if os.path.exists(AUDIT_PATH):
                tx_stages = {}
                with open(AUDIT_PATH) as f:
                    for line in f:
                        if not line.strip():
                            continue
                        try:
                            entry = json.loads(line)
                            tx_id = entry.get("transaction_id")
                            stage = entry.get("stage")
                            payload = entry.get("payload", {})
                            if tx_id not in tx_stages:
                                tx_stages[tx_id] = {
                                    "transaction_id": tx_id,
                                    "merchant_id": entry.get("merchant_id"),
                                    "root_cause": "unknown",
                                    "confidence": 0.0,
                                    "decision_type": "AUTOMATED",
                                    "action": "no_action",
                                    "outcome": "pending",
                                    "net_recovered": 0.0
                                }
                            if stage == "classify":
                                tx_stages[tx_id]["root_cause"] = payload.get("root_cause")
                                tx_stages[tx_id]["confidence"] = payload.get("confidence")
                            elif stage == "decide":
                                tx_stages[tx_id]["action"] = payload.get("action")
                                tx_stages[tx_id]["decision_type"] = payload.get("decision_type", "AUTOMATED")
                            elif stage == "execute":
                                tx_stages[tx_id]["outcome"] = payload.get("action_result")
                                tx_stages[tx_id]["net_recovered"] = payload.get("net_recovered", 0.0)
                        except Exception:
                            continue
                audit_rows = list(tx_stages.values())

            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"report": report, "audit": audit_rows}).encode())
            return

        self.send_error(404, "Not Found")

    def do_POST(self):
        parsed = urlparse(self.path)
        if parsed.path == "/api/run-batch":
            verify_script = os.path.join(ROOT_DIR, "scripts", "verify_pipeline.py")
            subprocess.run([sys.executable, verify_script], check=True)
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"status": "success", "message": "Batch re-evaluated successfully"}')
            return

        self.send_error(404, "Not Found")

    def log_message(self, format, *args):
        pass # Suppress HTTP logs to keep terminal output clean


class ReusableHTTPServer(HTTPServer):
    allow_reuse_address = True


def start_server(initial_port=8080):
    parser = argparse.ArgumentParser(description="AI Revenue Recovery Dashboard Server")
    parser.add_argument("--port", type=int, default=initial_port, help="Port to listen on (default: 8080)")
    args, _ = parser.parse_known_args()

    port = args.port
    max_attempts = 10

    for attempt in range(max_attempts):
        try:
            server = ReusableHTTPServer(("0.0.0.0", port), DashboardHandler)
            print(f"\n🚀 AI Revenue Recovery Agent — Operator Dashboard Server")
            print(f"   👉 Open in browser: http://localhost:{port}")
            print(f"   Press Ctrl+C to stop.\n")
            try:
                server.serve_forever()
            except KeyboardInterrupt:
                print("\nDashboard server stopped.")
            finally:
                server.server_close()
            return
        except OSError as e:
            if e.errno == 48: # Address already in use
                print(f"⚠️  Port {port} is already in use. Trying port {port + 1}...")
                port += 1
            else:
                raise e

    print(f"❌ Failed to find an open port after {max_attempts} attempts.")
    sys.exit(1)


if __name__ == "__main__":
    start_server()
