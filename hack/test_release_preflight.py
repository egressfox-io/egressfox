import importlib.util
import io
import os
from pathlib import Path
import unittest
import urllib.error
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

    def check(self, release=None, version_pages=(), package_missing=False):
        calls = []

        def api(operation, path, missing_ok=False):
            calls.append((operation, path, missing_ok))
            if path == "/repos/egressfox-io/egressfox":
                return {"full_name": "egressfox-io/egressfox"}
            if path.startswith("/repos/egressfox-io/egressfox/releases/tags/"):
                return release
            if path.startswith("/orgs/egressfox-io/packages/container/egressfox/versions?"):
                if package_missing:
                    self.assertTrue(missing_ok)
                    return None
                page = int(path.rsplit("page=", 1)[1])
                return version_pages[page - 1] if page <= len(version_pages) else []
            raise AssertionError(path)
        with patch.object(preflight, "api", side_effect=api):
            preflight.preflight()
        return calls

    def test_first_release_when_package_does_not_exist(self):
        calls = self.check(package_missing=True)
        self.assertEqual(calls[-1], (
            "GHCR package version lookup page 1",
            "/orgs/egressfox-io/packages/container/egressfox/versions?per_page=100&page=1",
            True,
        ))

    def test_existing_package_with_no_matching_version(self):
        self.check(version_pages=[[{"metadata": {"container": {"tags": ["latest"]}}}]])

    def test_new_version(self):
        self.check()

    def test_existing_release(self):
        with self.assertRaisesRegex(ValueError, "GitHub Release already exists"):
            self.check(release={"id": 1})

    def test_existing_or_partially_published_image(self):
        with self.assertRaisesRegex(ValueError, "GHCR version tag already exists"):
            self.check(version_pages=[[{"metadata": {"container": {"tags": ["0.1.0-dev.1"]}}}]])

    def test_invalid_tag_or_source(self):
        os.environ["GITHUB_REF_NAME"] = "v0.1.0-dev.2"
        with self.assertRaisesRegex(ValueError, "exact version tag"):
            self.check()

    def test_package_versions_pagination(self):
        full_page = [{"metadata": {"container": {"tags": []}}}] * 100
        calls = self.check(version_pages=[full_page, [{"metadata": {"container": {"tags": ["latest"]}}}]])
        self.assertEqual(calls[-1][1], "/orgs/egressfox-io/packages/container/egressfox/versions?per_page=100&page=2")

    def test_pagination_preserves_existing_query_parameters(self):
        calls = []

        def api(operation, path, missing_ok=False):
            calls.append((operation, path, missing_ok))
            return []

        with patch.object(preflight, "api", side_effect=api):
            self.assertEqual(list(preflight.pages("synthetic lookup", "/synthetic?state=active")), [])
        self.assertEqual(calls, [("synthetic lookup page 1", "/synthetic?state=active&per_page=100&page=1", False)])

    def test_remote_http_errors_fail_closed_with_safe_diagnostics(self):
        token = os.environ["GH_TOKEN"]
        for status in (400, 401, 403, 429, 500, 503):
            with self.subTest(status=status):
                error = urllib.error.HTTPError(
                    "https://api.github.com/synthetic",
                    status,
                    f"synthetic reason containing {token}",
                    None,
                    io.BytesIO(f"synthetic response containing {token}".encode()),
                )
                with patch.object(preflight.urllib.request, "urlopen", side_effect=error):
                    with self.assertRaisesRegex(RuntimeError, rf"GHCR package version lookup failed with HTTP {status}") as raised:
                        preflight.api("GHCR package version lookup", "/synthetic", missing_ok=True)
                self.assertNotIn(token, str(raised.exception))
                self.assertNotIn("synthetic response", str(raised.exception))

    def test_network_errors_identify_the_operation_without_leaking_details(self):
        token = os.environ["GH_TOKEN"]
        error = urllib.error.URLError(f"synthetic reason containing {token}")
        with patch.object(preflight.urllib.request, "urlopen", side_effect=error):
            with self.assertRaisesRegex(RuntimeError, "GHCR package version lookup request failed") as raised:
                preflight.api("GHCR package version lookup", "/synthetic")
        self.assertNotIn(token, str(raised.exception))

    def test_malformed_api_responses_fail_closed(self):
        malformed = (
            ("repository", [], "repository identity mismatch"),
            ("versions page", {"unexpected": "object"}, "expected an array"),
            ("version item", ["unexpected"], "expected an object"),
            ("metadata", [{"metadata": []}], "expected tag array"),
            ("tags", [{"metadata": {"container": {"tags": "unexpected"}}}], "expected tag array"),
        )
        for kind, result, message in malformed:
            with self.subTest(kind=kind):
                def api(operation, path, missing_ok=False):
                    if path == "/repos/egressfox-io/egressfox":
                        return result if kind == "repository" else {"full_name": "egressfox-io/egressfox"}
                    if path.startswith("/repos/"):
                        return None
                    if "/versions" in path:
                        return result
                    raise AssertionError(path)
                with patch.object(preflight, "api", side_effect=api):
                    with self.assertRaisesRegex(ValueError, message):
                        preflight.preflight()

    def test_malformed_json_is_reported_without_response_contents(self):
        class Response:
            def __enter__(self):
                return self

            def __exit__(self, *args):
                return False

            def read(self, *args):
                return b"not-json"

        with patch.object(preflight.urllib.request, "urlopen", return_value=Response()):
            with self.assertRaisesRegex(ValueError, "repository identity lookup returned malformed JSON"):
                preflight.api("repository identity lookup", "/synthetic")

    def test_repeated_invocation_fails_after_first_publish(self):
        self.check()
        with self.assertRaisesRegex(ValueError, "GHCR version tag already exists"):
            self.check(version_pages=[[{"metadata": {"container": {"tags": ["0.1.0-dev.1"]}}}]])


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
        self.assertIn("### Security", result)
        self.assertIn("🔒", result)
        self.assertNotIn("Unreleased", result)


if __name__ == "__main__":
    unittest.main()
