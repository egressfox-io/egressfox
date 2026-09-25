"""Offline guard for the E2E runner's pre-infrastructure configuration gate."""

import os
from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parent.parent


class ParallelismConfigurationTest(unittest.TestCase):
    def test_invalid_values_fail_before_kind_setup(self):
        for value in ("", "0", "6", "abc", "1.5", "-1"):
            with self.subTest(value=value):
                environment = os.environ.copy()
                environment["EGRESSFOX_E2E_PARALLELISM"] = value
                result = subprocess.run(
                    ["sh", "./hack/e2e-kind.sh"],
                    cwd=ROOT,
                    env=environment,
                    capture_output=True,
                    text=True,
                    timeout=5,
                    check=False,
                )
                self.assertEqual(result.returncode, 2)
                self.assertIn("EGRESSFOX_E2E_PARALLELISM must be an integer from 1 to 5", result.stderr)


if __name__ == "__main__":
    unittest.main()
