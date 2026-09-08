import unittest
from unittest.mock import patch

import collect_metrics


class RedisMetricsTest(unittest.TestCase):
    def query(self, clients):
        responses = {
            "memory": "used_memory:1024\n",
            "stats": "evicted_keys:0\n",
            "clients": clients,
            "commandstats": "cmdstat_ping:calls=10,usec=20,usec_per_call=2.0\n",
            "LATEST": "",
        }
        with patch.object(
            collect_metrics.subprocess, "check_output",
            side_effect=lambda command, **kwargs: responses[command[-1]],
        ):
            return collect_metrics.redis_info(30184)

    def test_blocked_clients_are_observed(self):
        metrics, error = self.query("blocked_clients:2\n")
        self.assertIsNone(error)
        self.assertEqual(metrics["blocked_clients"], 2)

    def test_missing_clients_is_not_a_complete_sample(self):
        metrics, error = self.query("")
        self.assertNotIn("blocked_clients", metrics)
        self.assertIn("blocked_clients", error)


class ResourceContractTest(unittest.TestCase):
    def test_cgroup_v1_sample_requires_bounded_limits_and_throttle_counters(self):
        sample = collect_metrics.parse_cgroup_sample(
            "total_rss 4096\ntotal_cache 1024\n",
            "1048576\n",
            "50000\n",
            "100000\n",
            "nr_periods 10\nnr_throttled 2\nthrottled_time 3000000\n",
        )
        self.assertEqual(sample["rss_bytes"], 4096)
        self.assertEqual(sample["memory_limit_bytes"], 1048576)
        self.assertEqual(sample["cpu_limit_cores"], 0.5)
        self.assertEqual(sample["cpu_throttled_usec"], 3000)

    def test_cgroup_v1_sample_rejects_unbounded_or_incomplete_metrics(self):
        with self.assertRaisesRegex(RuntimeError, "bounded RSS"):
            collect_metrics.parse_cgroup_sample(
                "total_rss 4096\n",
                "-1\n",
                "-1\n",
                "100000\n",
                "nr_throttled 0\nthrottled_time 0\n",
            )
        with self.assertRaisesRegex(RuntimeError, "throttling counters"):
            collect_metrics.parse_cgroup_sample(
                "total_rss 4096\n",
                "1048576\n",
                "50000\n",
                "100000\n",
                "nr_periods 10\n",
            )

    def test_window_coverage_requires_both_edges(self):
        covered = collect_metrics.evaluate_window_coverage([99.0, 100.5, 101.5, 102.0], 100.0, 102.0, 1.0)
        self.assertEqual(covered["status"], "covered")
        incomplete = collect_metrics.evaluate_window_coverage([100.5, 101.5], 100.0, 110.0, 1.0)
        self.assertEqual(incomplete["status"], "unmeasurable")

    def test_window_coverage_rejects_middle_sampling_gap(self):
        sparse = collect_metrics.evaluate_window_coverage([100.0, 110.0], 100.0, 110.0, 1.0)
        self.assertEqual(sparse["status"], "unmeasurable")
        self.assertEqual(sparse["max_gap_seconds"], 10.0)
        self.assertIn("gap", sparse["reason"])

    def test_docker_limits_rejects_production_image_drift(self):
        docker_output = "\n".join(
            f"{name}\t123\t100000000\t1048576\tpf-capacity-image\tsha256:frozen"
            for name in collect_metrics.CONTAINERS
        )
        with patch.object(collect_metrics.subprocess, "check_output", return_value=docker_output):
            limits, error = collect_metrics.docker_limits("pf-capacity-image", "sha256:changed")
        self.assertEqual(len(limits), len(collect_metrics.CONTAINERS))
        self.assertIn("image id differs", error)


if __name__ == "__main__":
    unittest.main()
