import importlib.util
import os
from pathlib import Path
import unittest
from unittest.mock import patch


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(file))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


preflight = load("release_preflight", "release-preflight.py")
release_notes = load("release_notes", "release-notes.py")


class ReleasePreflightTests(unittest.TestCase):
    def setUp(self):
        self.env = patch.dict(os.environ, {
            "VERSION": "v0.1.0-dev.1",
            "GITHUB_REPOSITORY": "egressfox-io/egressfox",
            "GITHUB_SHA": "a" * 40,
            "GITHUB_REF_TYPE": "tag",
            "GITHUB_REF_NAME": "v0.1.0-dev.1",
            "GH_TOKEN": "synthetic-test-token",
        })
        self.env.start()
        self.addCleanup(self.env.stop)
        git = patch.object(preflight.subprocess, "check_output", return_value="a" * 40 + "\n")
        git.start()
        self.addCleanup(git.stop)

    def check(self, release=None, tags=(), packages=True):
        def api(path, missing_ok=False):
            if path == "/repos/egressfox-io/egressfox":
                return {"full_name": "egressfox-io/egressfox"}
            if path.startswith("/repos/"):
                return release
            if "/versions" in path:
                return [{"metadata": {"container": {"tags": list(tags)}}}]
            if "/packages?" in path:
                return [{"name": "egressfox"}] if packages else []
            raise AssertionError(path)
        with patch.object(preflight, "api", side_effect=api):
            preflight.preflight()

    def test_new_version(self):
        self.check()
        self.check(packages=False)

    def test_existing_release(self):
        with self.assertRaisesRegex(ValueError, "GitHub Release already exists"):
            self.check(release={"id": 1})

    def test_existing_or_partially_published_image(self):
        with self.assertRaisesRegex(ValueError, "GHCR version tag already exists"):
            self.check(tags=("0.1.0-dev.1",))

    def test_invalid_tag_or_source(self):
        os.environ["GITHUB_REF_NAME"] = "v0.1.0-dev.2"
        with self.assertRaisesRegex(ValueError, "exact version tag"):
            self.check()

    def test_remote_error_fails_closed(self):
        with patch.object(preflight, "api", side_effect=RuntimeError("HTTP 403")):
            with self.assertRaisesRegex(RuntimeError, "HTTP 403"):
                preflight.preflight()

    def test_repeated_invocation_fails_after_first_publish(self):
        self.check()
        with self.assertRaisesRegex(ValueError, "GHCR version tag already exists"):
            self.check(tags=("0.1.0-dev.1",))


class ReleaseNotesTests(unittest.TestCase):
    def test_extracts_only_requested_version(self):
        changelog = "## [Unreleased]\n\n### Fixed\n\n- 🐛 Future.\n\n## [v0.1.0-dev.1]\n\n### Added\n\n- ✨ First.\n\n## [v0.0.1]\n\n### Added\n\n- ✨ Old.\n"
        self.assertEqual(release_notes.notes(changelog, "v0.1.0-dev.1"), "### Added\n\n- ✨ First.\n")

    def test_missing_duplicate_or_empty_section_fails(self):
        for changelog in ("## [Unreleased]\n", "## [v0.1.0-dev.1]\n### Added\n", "## [v0.1.0-dev.1]\n### Added\n- ✨ One.\n## [v0.1.0-dev.1]\n### Fixed\n- 🐛 Two.\n"):
            with self.subTest(changelog=changelog), self.assertRaises(ValueError):
                release_notes.notes(changelog, "v0.1.0-dev.1")

    def test_repository_section_is_extractable(self):
        result = release_notes.notes(Path("CHANGELOG.md").read_text(), "v0.1.0-dev.1")
        self.assertIn("🔒 Prevented", result)
        self.assertNotIn("Unreleased", result)


if __name__ == "__main__":
    unittest.main()
