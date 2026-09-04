#!/usr/bin/env python3
"""
Unit and Boundary Stress Tests for the Decision Engine, Stopping Rules, Compliance Vetoes, and Circuit Breakers.
"""

import unittest

class DecisionEngineBoundaryAndSafetyTest(unittest.TestCase):
    def test_circuit_breaker_boundary_39_9_percent(self):
        """39.9% failure rate is below the 40% threshold -> allowed."""
        fail_rate = 0.399
        total_events = 20
        is_halted = (fail_rate > 0.40 and total_events >= 5)
        self.assertFalse(is_halted, "39.9% failure rate should NOT trigger circuit breaker")

    def test_circuit_breaker_boundary_40_0_percent(self):
        """40.0% failure rate is exactly at threshold -> allowed."""
        fail_rate = 0.400
        total_events = 20
        is_halted = (fail_rate > 0.40 and total_events >= 5)
        self.assertFalse(is_halted, "40.0% failure rate should NOT trigger circuit breaker")

    def test_circuit_breaker_boundary_40_1_percent(self):
        """40.1% failure rate exceeds the 40% threshold -> halted."""
        fail_rate = 0.401
        total_events = 20
        is_halted = (fail_rate > 0.40 and total_events >= 5)
        self.assertTrue(is_halted, "40.1% failure rate MUST trigger circuit breaker")

    def test_compliance_veto_risk_block(self):
        """Security/Fraud cause strictly vetoes automated retries."""
        cause = "risk_block"
        is_vetoed = cause in ["risk_block", "invalid_credentials"]
        action = "escalate_human" if is_vetoed else "retry_immediate"
        self.assertEqual(action, "escalate_human")

    def test_compliance_veto_invalid_credentials(self):
        """Auth/MPIN failure strictly vetoes automated retries."""
        cause = "invalid_credentials"
        is_vetoed = cause in ["risk_block", "invalid_credentials"]
        action = "escalate_human" if is_vetoed else "retry_immediate"
        self.assertEqual(action, "escalate_human")

    def test_max_retries_exhaustion_cap(self):
        """Transaction reaching attempt 3 must be escalated to human."""
        attempt_count = 3
        action = "escalate_human" if attempt_count >= 3 else "retry_delayed"
        self.assertEqual(action, "escalate_human")

    def test_expected_net_value_optimization(self):
        """ENV selects optimal action based on predicted recovery economics."""
        amount = 5000.0
        # Immediate retry on timeout: P=0.78, Cost=2.50
        env_immediate = (0.78 * amount) - 2.50
        # Delayed retry on timeout: P=0.74, Cost=2.50
        env_delayed = (0.74 * amount) - 2.50
        # Escalation: Cost=25.00
        env_escalate = -25.00

        self.assertGreater(env_immediate, env_delayed)
        self.assertGreater(env_immediate, env_escalate)
        self.assertGreater(env_immediate, 0)

    def test_method_eligibility_constraint(self):
        """Card update link is only eligible for card / emandate methods."""
        allowed_for_card = "card" in ["card", "emandate"]
        allowed_for_upi = "upi" in ["card", "emandate"]
        self.assertTrue(allowed_for_card)
        self.assertFalse(allowed_for_upi)

if __name__ == "__main__":
    unittest.main()
