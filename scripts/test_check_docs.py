import tempfile
import unittest
from pathlib import Path

from check_docs import _check_task_board


class TaskBoardTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.tasks = self.root / "docs/audits/tasks"
        (self.tasks / "archive").mkdir(parents=True)
        (self.tasks.parent / "eval.md").write_text("# Eval\n", encoding="utf-8")
        (self.tasks.parent / "README.md").write_text(
            "| 评测 | [eval.md](eval.md) | 开工 | tests |\n", encoding="utf-8"
        )
        self.board = self.tasks / "README.md"
        self.archive = self.tasks / "archive/README.md"
        self.archive.write_text("# Archive\n", encoding="utf-8")
        self.write_issue()

    def write_issue(self, name="one.md", status="开放", owner="—", time="—", blocked=""):
        content = (
            f"# Task\n\n状态：{status}\n类型：证据\n认领者：{owner}\n认领于：{time}\n"
            "业务组：评测\n父账本：eval.md\n完成后可拆：无\n\n"
            f"## 阻塞与交接\n{blocked}\n\n## 证据\n"
        )
        (self.tasks / name).write_text(content, encoding="utf-8")
        self.sync_board()

    def sync_board(self):
        rows = []
        for path in sorted(self.tasks.glob("*.md")):
            if path.name == "README.md":
                continue
            metadata = dict(line.split("：", 1) for line in path.read_text(encoding="utf-8").splitlines() if "：" in line)
            cells = [metadata[key] for key in ("业务组", "类型", "状态", "认领者", "认领于")]
            rows.append(f"| [{path.name}]({path.name}) | " + " | ".join(cells) + " |")
        self.board.write_text("\n".join(rows) + "\n", encoding="utf-8")

    def errors(self):
        errors = []
        _check_task_board(errors, self.root)
        return "\n".join(errors)

    def test_open_board_passes(self):
        self.assertEqual(self.errors(), "")

    def test_metadata_drift_fails(self):
        self.board.write_text(self.board.read_text(encoding="utf-8").replace("开放", "认领"), encoding="utf-8")
        self.assertIn("out of sync", self.errors())

    def test_missing_and_duplicate_rows_fail(self):
        row = self.board.read_text(encoding="utf-8")
        self.board.write_text(row + row, encoding="utf-8")
        self.assertIn("duplicate issue board row", self.errors())
        self.board.write_text("", encoding="utf-8")
        self.assertIn("board/file mismatch", self.errors())

    def test_claim_needs_owner_and_zoned_time(self):
        self.write_issue(status="认领")
        self.assertIn("needs an owner", self.errors())
        self.write_issue(status="认领", owner="session-a", time="2026-09-05T03:00:00")
        self.assertIn("ISO with timezone", self.errors())
        self.write_issue(status="认领", owner="session-a", time="2026-09-05T03:00:00+08:00")
        self.assertEqual(self.errors(), "")

    def test_blocked_claim_still_occupies_owner(self):
        blocked = "- 原因：缺输入\n- 解除条件：提供输入\n- 跟进者：主代理\n- 交接：保留 diff 与占用"
        self.write_issue(status="阻塞", owner="session-a", time="2026-09-05T03:00:00Z", blocked=blocked)
        self.assertEqual(self.errors(), "")
        self.write_issue(name="two.md", status="认领", owner="session-a", time="2026-09-05T03:01:00Z")
        self.assertIn("occupies multiple issues", self.errors())

    def test_blocked_requires_actionable_handoff(self):
        self.write_issue(status="阻塞", blocked="missing credentials")
        self.assertIn("blocked issue needs", self.errors())
        self.write_issue(status="阻塞", blocked="- 原因：缺输入\n- 解除条件：提供输入\n- 跟进者：主代理\n- 交接：无占用")
        self.assertEqual(self.errors(), "")

    def test_group_must_match_parent(self):
        path = self.tasks / "one.md"
        path.write_text(path.read_text(encoding="utf-8").replace("父账本：eval.md", "父账本：missing.md"), encoding="utf-8")
        self.assertIn("parent ledger mismatch", self.errors())

    def test_archive_requires_closed_status_and_index(self):
        (self.tasks / "one.md").rename(self.tasks / "archive/one.md")
        self.board.write_text("", encoding="utf-8")
        self.assertIn("invalid status for archive", self.errors())
        self.assertIn("archive index/file mismatch", self.errors())
        path = self.tasks / "archive/one.md"
        path.write_text(path.read_text(encoding="utf-8").replace("状态：开放", "状态：取消"), encoding="utf-8")
        self.archive.write_text("| [one.md](one.md) | 取消 | split |\n", encoding="utf-8")
        self.assertEqual(self.errors(), "")
        self.write_issue()
        self.assertIn("ID reused", self.errors())

    def test_duplicate_metadata_fails(self):
        path = self.tasks / "one.md"
        path.write_text(path.read_text(encoding="utf-8").replace("状态：开放", "状态：开放\n状态：认领"), encoding="utf-8")
        self.assertIn("expected one nonempty 状态", self.errors())

    def test_open_issue_cannot_keep_claim(self):
        self.write_issue(owner="session-a", time="2026-09-05T03:00:00Z")
        self.assertIn("open issue cannot retain an owner", self.errors())


if __name__ == "__main__":
    unittest.main()
