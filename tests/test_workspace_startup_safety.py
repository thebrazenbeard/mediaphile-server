"""Source-only guard against concealed VS Code startup execution.

No editor, Node process, or suspicious asset is executed. A passing source
check does not establish whether an older checkout already ran anything.
"""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
BLOCKED_GIT_BLOBS = {
    "31ce18eddf4c96c1a705dbceaec4304f953d4dc3",  # disguised .lkf
    "60e613b9bcbd179b2c27b96c2130cd7db2e4250b",  # earlier disguised .woff2
}


def read_jsonc(path: Path) -> object:
    raw = path.read_text(encoding="utf-8")
    return json.loads(re.sub(r",\s*(?=[}\]])", "", raw))


def has_folder_open(value: object) -> bool:
    if isinstance(value, dict):
        if value.get("runOn") == "folderOpen":
            return True
        return any(has_folder_open(item) for item in value.values())
    if isinstance(value, list):
        return any(has_folder_open(item) for item in value)
    return False


def git_blob_digest(data: bytes) -> str:
    header = b"blob " + str(len(data)).encode("ascii") + b"\0"
    return hashlib.sha1(header + data).hexdigest()


class WorkspaceStartupSafetyTests(unittest.TestCase):
    def test_no_folder_open_execution(self):
        tasks = read_jsonc(ROOT / ".vscode" / "tasks.json")
        self.assertFalse(has_folder_open(tasks))
        self.assertNotIn("fa-solid.lkf", json.dumps(tasks))
        self.assertNotIn("fa-solid-500.woff2", json.dumps(tasks))

    def test_no_ineffective_workspace_consent_override(self):
        settings = read_jsonc(ROOT / ".vscode" / "settings.json")
        self.assertIsInstance(settings, dict)
        self.assertNotIn("task.allowAutomaticTasks", settings)
        self.assertNotIn("tasks", settings)

    def test_disguised_executable_asset_absent(self):
        self.assertFalse((ROOT / "public/fonts/fa-solid.lkf").exists())
        self.assertFalse((ROOT / "public/fonts/fa-solid-500.woff2").exists())
        for path in ROOT.rglob("*"):
            if ".git" in path.parts or not path.is_file():
                continue
            if path.stat().st_size not in (12797, 27410):
                continue
            self.assertNotIn(git_blob_digest(path.read_bytes()), BLOCKED_GIT_BLOBS, str(path))


if __name__ == "__main__":
    unittest.main()
