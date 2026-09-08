#!/usr/bin/env python3
"""Focused tests for contention evidence binding and aggregation boundaries."""

import sys
import unittest
from decimal import Decimal
from datetime import datetime, timezone


sys.path.insert(0, __file__.rsplit("/", 1)[0])

from run_contention import phase_metrics, quota_report, reconcile_submissions, task_ids_for_label  # noqa: E402


class FakeDBProbe:
    def __init__(self, candidates):
        self.candidates = candidates

    def tasks_by_session_prompts(self, pairs):
        return {pair: self.candidates.get(pair, []) for pair in pairs if pair in self.candidates}


class ContentionEvidenceTests(unittest.TestCase):
    merchants = [{"image_session_id": "session-a"}]

    def test_redispatch_timestamp_is_not_reported_as_first_dispatch(self):
        result = phase_metrics(
            ["task"],
            {"task": {"prompt": "queued", "sent_at": datetime.fromtimestamp(120, timezone.utc), "dispatch_attempts": 4}},
            {"request": {"prompt": "Current user request:\nqueued\n", "started_epoch": 122}},
            {"task": {"accepted_at": 100}},
        )[0]
        self.assertEqual(result["dispatch_attempts"], 4)
        self.assertEqual(result["accept_to_last_dispatch_ms"], 20000)
        self.assertEqual(result["last_dispatch_to_provider_ms"], 2000)
        self.assertEqual(result["accept_to_provider_ms"], 22000)
        self.assertTrue(result["first_dispatch_observation"].startswith("unmeasured"))
        self.assertNotIn("accept_to_dispatch_ms", result)
        self.assertNotIn("dispatch_to_provider_ms", result)

    def test_quota_account_accepts_postgres_numeric_aggregates(self):
        account = {
            "merchant_id": "merchant-a",
            "available_units": 1_009_157,
            "reserved_units": 0,
            "adjust_event_count": 10_000,
            "adjust_units": Decimal("10000"),
            "consumed_units": Decimal("843"),
            "outstanding_units": Decimal("0"),
        }
        report = quota_report([], [], [account], 1_000_000, 10_000, expected_merchant_ids=["merchant-a"])
        self.assertTrue(report["account_reconciliation"]["pass"])
        account["available_units"] += 1
        report = quota_report([], [], [account], 1_000_000, 10_000, expected_merchant_ids=["merchant-a"])
        self.assertFalse(report["account_reconciliation"]["pass"])

    def test_accepted_snapshot_gap_is_reconciled_without_overwriting_raw_id(self):
        submissions = [
            {"label": "A", "merchant_index": 0, "prompt": "direct", "task_id": "task-direct", "status": 202},
            {
                "label": "A",
                "merchant_index": 0,
                "prompt": "omitted-from-snapshot",
                "task_id": None,
                "status": 202,
                "error": "accepted response had unique prompt matches=0",
            },
        ]
        counts = reconcile_submissions(
            FakeDBProbe({("session-a", "direct"): ["task-direct"], ("session-a", "omitted-from-snapshot"): ["task-omitted"]}),
            submissions,
            self.merchants,
        )
        self.assertEqual(counts, {"accepted": 2, "response_snapshot_task_ids": 1, "reconciled": 2, "unresolved": 0, "ambiguous": 0})
        self.assertIsNone(submissions[1]["api_response_task_id"])
        self.assertEqual(submissions[1]["reconciled_task_id"], "task-omitted")
        self.assertEqual(submissions[1]["error"], "accepted response had unique prompt matches=0")
        self.assertEqual(task_ids_for_label(submissions, "A"), ["task-direct", "task-omitted"])

    def test_ambiguous_or_missing_db_binding_stays_unresolved(self):
        submissions = [
            {"label": "A", "merchant_index": 0, "prompt": "ambiguous", "task_id": None, "status": 202},
            {"label": "A", "merchant_index": 0, "prompt": "missing", "task_id": None, "status": 202},
        ]
        counts = reconcile_submissions(
            FakeDBProbe({("session-a", "ambiguous"): ["task-1", "task-2"]}),
            submissions,
            self.merchants,
        )
        self.assertEqual(counts["unresolved"], 2)
        self.assertEqual(counts["ambiguous"], 1)
        self.assertEqual(submissions[0]["reconciliation"], "accepted_db_task_ambiguous")
        self.assertEqual(submissions[1]["reconciliation"], "accepted_db_task_missing")
        self.assertEqual(task_ids_for_label(submissions, "A"), [])

    def test_quota_report_requires_observed_rows_and_matching_terminal_event(self):
        rows = [
            {
                "id": "task-complete",
                "status": "succeeded",
                "reserve_count": 1,
                "terminal_event_count": 1,
                "terminal_event_types": ["settle"],
                "hold_count": 1,
                "invalid_hold_count": 0,
            }
        ]
        report = quota_report(["task-complete", "task-missing"], rows)
        self.assertEqual(report["observed_task_count"], 1)
        self.assertEqual(report["semantic_complete_task_count"], 0)
        self.assertEqual(report["incomplete_task_ids"], ["task-complete", "task-missing"])
        self.assertFalse(report["pass"])

    def test_quota_report_reconciles_exact_amount_hold_and_account_balance(self):
        rows = [
            {
                "id": "task-complete",
                "status": "succeeded",
                "quota_events": [
                    {
                        "event_type": "reserve",
                        "amount_units": 1,
                        "available_after": 1009,
                        "reserved_after": 1,
                        "hold_id": "hold-1",
                    },
                    {
                        "event_type": "settle",
                        "amount_units": 1,
                        "available_after": 1009,
                        "reserved_after": 0,
                        "hold_id": "hold-1",
                    },
                ],
                "quota_holds": [{"id": "hold-1", "amount_units": 1, "status": "settled", "settled_units": 1}],
            }
        ]
        accounts = [
            {
                "merchant_id": "merchant-1",
                "available_units": 1009,
                "reserved_units": 0,
                "adjust_event_count": 10,
                "adjust_units": 10,
                "consumed_units": 1,
                "outstanding_units": 0,
            }
        ]
        report = quota_report(
            ["task-complete"],
            rows,
            accounts,
            opening_balance_units=1000,
            expected_adjust_units_per_merchant=10,
            expected_merchant_ids=["merchant-1"],
        )
        self.assertTrue(report["pass"])
        self.assertTrue(report["account_reconciliation"]["pass"])

    def test_quota_report_rejects_wrong_amount_hold_or_missing_account(self):
        rows = [
            {
                "id": "task-complete",
                "status": "succeeded",
                "quota_events": [
                    {"event_type": "reserve", "amount_units": 2, "available_after": 998, "reserved_after": 2, "hold_id": "hold-1"},
                    {"event_type": "settle", "amount_units": 2, "available_after": 998, "reserved_after": 0, "hold_id": "hold-1"},
                ],
                "quota_holds": [{"id": "hold-1", "amount_units": 2, "status": "settled", "settled_units": 2}],
            }
        ]
        report = quota_report(
            ["task-complete"],
            rows,
            [],
            opening_balance_units=1000,
            expected_adjust_units_per_merchant=10,
            expected_merchant_ids=["merchant-1"],
        )
        self.assertFalse(report["pass"])
        self.assertIn("merchant-1", report["account_reconciliation"]["incomplete_merchant_ids"])

    def test_quota_report_accepts_cancelled_unknown_hold_state(self):
        rows = [
            {
                "id": "task-cancelled",
                "status": "cancelled",
                "quota_events": [
                    {
                        "event_type": "reserve",
                        "amount_units": 1,
                        "available_after": 999,
                        "reserved_after": 1,
                        "hold_id": "hold-unknown",
                    },
                    {
                        "event_type": "mark_unknown",
                        "amount_units": 1,
                        "available_after": 999,
                        "reserved_after": 1,
                        "hold_id": "hold-unknown",
                    },
                ],
                "quota_holds": [
                    {
                        "id": "hold-unknown",
                        "amount_units": 1,
                        "status": "pending_reconciliation",
                        "settled_units": None,
                    }
                ],
            }
        ]
        report = quota_report(["task-cancelled"], rows)
        self.assertTrue(report["pass"])

    def test_quota_report_rejects_settled_units_on_non_settle_terminal(self):
        rows = [
            {
                "id": "task-released",
                "status": "cancelled",
                "quota_events": [
                    {
                        "event_type": "reserve",
                        "amount_units": 1,
                        "available_after": 999,
                        "reserved_after": 1,
                        "hold_id": "hold-released",
                    },
                    {
                        "event_type": "release",
                        "amount_units": 1,
                        "available_after": 1000,
                        "reserved_after": 0,
                        "hold_id": "hold-released",
                    },
                ],
                "quota_holds": [
                    {
                        "id": "hold-released",
                        "amount_units": 1,
                        "status": "released",
                        "settled_units": 1,
                    }
                ],
            },
            {
                "id": "task-unknown",
                "status": "cancelled",
                "quota_events": [
                    {
                        "event_type": "reserve",
                        "amount_units": 1,
                        "available_after": 999,
                        "reserved_after": 1,
                        "hold_id": "hold-unknown",
                    },
                    {
                        "event_type": "mark_unknown",
                        "amount_units": 1,
                        "available_after": 999,
                        "reserved_after": 1,
                        "hold_id": "hold-unknown",
                    },
                ],
                "quota_holds": [
                    {
                        "id": "hold-unknown",
                        "amount_units": 1,
                        "status": "pending_reconciliation",
                        "settled_units": 1,
                    }
                ],
            },
        ]
        report = quota_report(["task-released", "task-unknown"], rows)
        self.assertFalse(report["pass"])
        self.assertEqual(report["incomplete_task_ids"], ["task-released", "task-unknown"])


if __name__ == "__main__":
    unittest.main()
