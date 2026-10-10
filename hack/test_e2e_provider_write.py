"""Offline regression coverage for lost stdin during fixture uploads."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


class ProviderWriteTests(unittest.TestCase):
    def run_transfer(self, failures, always=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            destination = root / "body"
            destination.write_text("previous fixture\n")
            mock = root / "kubectl"
            mock.write_text("""#!/usr/bin/env python3
import os, pathlib, subprocess, sys
args = sys.argv[sys.argv.index('--') + 1:]
if args[:2] == ['sh', '-c']:
    count_file = pathlib.Path(os.environ['TRANSFER_COUNT'])
    count = int(count_file.read_text()) + 1 if count_file.exists() else 1
    count_file.write_text(str(count))
    body = sys.stdin.buffer.read()
    if os.environ['ALWAYS_LOST'] == '1' or count <= int(os.environ['LOST_ATTEMPTS']):
        body = b''  # successful exec with lost stdin
    sys.exit(subprocess.run(args, input=body).returncode)
sys.exit(subprocess.run(args).returncode)
""")
            mock.chmod(0o755)
            environment = dict(os.environ, PATH=str(root) + os.pathsep + os.environ['PATH'],
                               TRANSFER_COUNT=str(root / "count"), LOST_ATTEMPTS=str(failures),
                               ALWAYS_LOST='1' if always else '0')
            result = subprocess.run(
                ['sh', '-c', '. ./hack/e2e-provider-write.sh; namespace=fixture; provider_pod=provider; provider_write "$1"',
                 'sh', str(destination)], cwd=ROOT, env=environment,
                input='synthetic subscription\n', text=True, capture_output=True, timeout=10)
            return result, destination.read_text(), int((root / "count").read_text()), Path(str(destination) + '.upload').exists()

    def test_lost_stdin_is_retried_before_publication(self):
        result, body, attempts, temporary = self.run_transfer(1)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(body, 'synthetic subscription\n')
        self.assertEqual(attempts, 2)
        self.assertFalse(temporary)

    def test_repeated_loss_fails_without_replacing_fixture(self):
        result, body, attempts, temporary = self.run_transfer(0, always=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(body, 'previous fixture\n')
        self.assertEqual(attempts, 3)
        self.assertFalse(temporary)
        self.assertNotIn('synthetic subscription', result.stderr)


if __name__ == '__main__':
    unittest.main()
