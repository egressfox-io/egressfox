import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("generate-changelog.py")
spec = importlib.util.spec_from_file_location("generate_changelog", SCRIPT)
generator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generator)
notes_spec = importlib.util.spec_from_file_location("release_notes", Path(__file__).with_name("release-notes.py"))
release_notes = importlib.util.module_from_spec(notes_spec)
notes_spec.loader.exec_module(release_notes)


def command(*args, env=None):
    return subprocess.run(args, check=True, text=True, capture_output=True, env=env).stdout.strip()


class ChangeEntryTests(unittest.TestCase):
    def test_groups_emoji_and_breaking_changes(self):
        self.assertEqual(generator.entry("✨ feat(core): ✨ add endpoints", ""), ("Added", "- ✨ Add endpoints."))
        self.assertEqual(generator.entry("🔒 fix(source): redact tokens", ""), ("Security", "- 🔒 Redact tokens."))
        self.assertEqual(generator.entry("🐛 fix(api)!: remove old field", ""), ("Changed", "- 🐛 Breaking: remove old field."))
        self.assertEqual(generator.entry("⚡ perf(state): reduce allocations", ""), ("Changed", "- ⚡ Reduce allocations."))

    def test_excludes_noise_and_rejects_malformed_user_change(self):
        for subject in ("📝 docs(plan): write plan", "✅ test(api): cover path", "🔧 ci(release): pin action", "⬆️ chore(deps): bump module", "♻️ refactor(core): move helper", "⚡ perf(state): benchmark append"):
            self.assertIsNone(generator.entry(subject, ""))
        self.assertIsNone(generator.entry("🐛 fix(core): repair bug", "Changelog: skip\n"))
        with self.assertRaisesRegex(ValueError, "leading emoji"):
            generator.entry("feat(core): add endpoint", "")

    def test_deduplicates_user_facing_entries(self):
        records = [("1", "✨ feat(core): add endpoints", ""), ("2", "✨ feat(api): add endpoints", ""), ("3", "🐛 fix(core): repair bug", "")]
        section = generator.release_section("v0.1.0-dev.1", records)
        self.assertEqual(section.count("- ✨ Add endpoints."), 1)
        self.assertIn("### Fixed\n\n- 🐛 Repair bug.", section)
        changelog = generator.render("", "v0.1.0-dev.1", section)
        self.assertEqual(release_notes.notes(changelog, "v0.1.0-dev.1"), section.split("\n", 1)[1].lstrip())

    def test_empty_preview_is_allowed_but_release_preparation_fails(self):
        self.assertEqual(generator.release_section("v0.1.0-dev.2", [], allow_empty=True), "## [v0.1.0-dev.2]\n")
        with self.assertRaisesRegex(ValueError, "no user-facing commits"):
            generator.release_section("v0.1.0-dev.2", [])


class GitHistoryTests(unittest.TestCase):
    def setUp(self):
        self.old_cwd = Path.cwd()
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        os.chdir(self.folder.name)
        self.addCleanup(os.chdir, self.old_cwd)
        command("git", "init", "-q", "-b", "codex/test")
        command("git", "config", "user.name", "Test Maintainer")
        command("git", "config", "user.email", "maintainer@example.test")
        Path("release").mkdir()
        Path("release/manifest.json").write_text(json.dumps({"release": {"developmentVersion": "v0.1.0-dev.1"}}))
        Path("CHANGELOG.md").write_text("# Changelog\n\n## [Unreleased]\n")
        self.commit("✨ feat(core): add first capability")

    def commit(self, subject):
        marker = Path("marker.txt")
        marker.write_text(marker.read_text() + "x" if marker.exists() else "x")
        command("git", "add", ".")
        command("git", "commit", "-q", "-m", subject)

    def test_first_release_uses_repository_root_and_ignores_noise(self):
        self.commit("📝 docs(plan): record implementation")
        self.assertIsNone(generator.previous_tag("v0.1.0-dev.1"))
        generated, tag = generator.generate("v0.1.0-dev.1", Path("CHANGELOG.md").read_text())
        self.assertIsNone(tag)
        self.assertEqual(generated.count("- ✨ Add first capability."), 1)
        self.assertNotIn("Record implementation", generated)

    def test_next_release_only_uses_commits_after_previous_tag(self):
        command("git", "tag", "v0.1.0-dev.1")
        Path("release/manifest.json").write_text(json.dumps({"release": {"developmentVersion": "v0.1.0-dev.2"}}))
        self.commit("🐛 fix(core): repair second capability")
        generated, tag = generator.generate("v0.1.0-dev.2", "# Changelog\n\n## [Unreleased]\n")
        self.assertEqual(tag, "v0.1.0-dev.1")
        self.assertIn("- 🐛 Repair second capability.", generated)
        self.assertNotIn("Add first capability", generated)
        with self.assertRaisesRegex(ValueError, "advance"):
            generator.previous_tag("v0.0.9")

    def test_preparation_commits_once_and_check_is_stable(self):
        Path("hack").mkdir()
        shutil.copyfile(SCRIPT, "hack/generate-changelog.py")
        shutil.copyfile(Path(__file__).with_name("release-prepare.sh"), "hack/release-prepare.sh")
        command("git", "add", "hack")
        command("git", "commit", "-q", "-m", "🔧 chore(release): add preparation tooling")
        env = dict(os.environ, VERSION="v0.1.0-dev.1", PYTHONDONTWRITEBYTECODE="1")
        command("bash", "hack/release-prepare.sh", env=env)
        self.assertEqual(command("git", "log", "-1", "--format=%s"), "📝 docs(changelog): prepare v0.1.0-dev.1 release notes")
        self.assertIn("- ✨ Add first capability.", Path("CHANGELOG.md").read_text())
        count = command("git", "rev-list", "--count", "HEAD")
        command("bash", "hack/release-prepare.sh", env=env)
        self.assertEqual(command("git", "rev-list", "--count", "HEAD"), count)
        command("python3", "hack/generate-changelog.py", "--version", "v0.1.0-dev.1", "--check", env=env)
        command("git", "tag", "v0.1.0-dev.1")
        command("python3", "hack/generate-changelog.py", "--version", "v0.1.0-dev.1", "--check", env=env)
        with self.assertRaises(subprocess.CalledProcessError):
            command("bash", "hack/release-prepare.sh", env=env)

    def test_historical_section_is_preserved(self):
        current = "# Changelog\n\n## [Unreleased]\n\n## [v0.1.0-dev.1]\n\n### Added\n\n- ✨ Historic.\n"
        result = generator.render(current, "v0.1.0-dev.2", "## [v0.1.0-dev.2]\n\n### Fixed\n\n- 🐛 Current.\n")
        self.assertIn("- ✨ Historic.", result)
        self.assertLess(result.index("v0.1.0-dev.2"), result.index("v0.1.0-dev.1"))

    def test_first_release_check_fails_shallow_and_passes_with_full_history(self):
        self.commit("🐛 fix(core): repair first capability")
        generated, _ = generator.generate("v0.1.0-dev.1", Path("CHANGELOG.md").read_text())
        Path("CHANGELOG.md").write_text(generated)
        self.commit("📝 docs(changelog): prepare v0.1.0-dev.1 release notes")
        self.assert_checkout_history("v0.1.0-dev.1")

    def test_later_release_checkout_needs_history_and_previous_tag(self):
        generated, _ = generator.generate("v0.1.0-dev.1", Path("CHANGELOG.md").read_text())
        Path("CHANGELOG.md").write_text(generated)
        self.commit("📝 docs(changelog): prepare v0.1.0-dev.1 release notes")
        command("git", "tag", "v0.1.0-dev.1")
        Path("release/manifest.json").write_text(json.dumps({"release": {"developmentVersion": "v0.1.0-dev.2"}}))
        self.commit("🐛 fix(core): repair second capability")
        generated, _ = generator.generate("v0.1.0-dev.2", Path("CHANGELOG.md").read_text())
        Path("CHANGELOG.md").write_text(generated)
        self.commit("📝 docs(changelog): prepare v0.1.0-dev.2 release notes")
        self.assert_checkout_history("v0.1.0-dev.2", previous_tag="v0.1.0-dev.1")

    def assert_checkout_history(self, version, previous_tag=None):
        # A file:// clone honors --depth, like the default Actions checkout.
        with tempfile.TemporaryDirectory() as destination:
            checkout = Path(destination) / "checkout"
            command("git", "clone", "-q", "--depth", "1", "--no-tags", "--branch", "codex/test", Path(self.folder.name).as_uri(), str(checkout))
            args = ["python3", str(SCRIPT), "--version", version, "--check"]
            env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
            self.assertEqual(command("git", "-C", str(checkout), "rev-parse", "--is-shallow-repository"), "true")
            shallow = subprocess.run(args, cwd=checkout, env=env, capture_output=True, text=True)
            self.assertNotEqual(shallow.returncode, 0, "a depth-one checkout must not verify incomplete history")
            command("git", "-C", str(checkout), "fetch", "-q", "--unshallow", "--tags")
            self.assertEqual(command("git", "-C", str(checkout), "rev-parse", "--is-shallow-repository"), "false")
            if previous_tag:
                self.assertIn(previous_tag, command("git", "-C", str(checkout), "tag", "--list").splitlines())
            complete = subprocess.run(args, cwd=checkout, env=env, capture_output=True, text=True)
            self.assertEqual(complete.returncode, 0, complete.stderr)


if __name__ == "__main__":
    unittest.main()
