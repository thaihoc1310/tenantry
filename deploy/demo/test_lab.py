"""python3 -m unittest deploy/demo/test_lab.py: the retry of lab.retried and
what clean-e2e.py counts as a test organization."""
import os
import sys
import unittest
import urllib.error

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lab  # noqa: E402
import importlib.util  # noqa: E402

_spec = importlib.util.spec_from_file_location("clean_e2e", os.path.join(os.path.dirname(os.path.abspath(__file__)), "clean-e2e.py"))
clean_e2e = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(clean_e2e)
import purge  # noqa: E402


class Retried(unittest.TestCase):
    def run_with(self, outcomes, tries=3):
        calls, slept = [], []

        def call():
            calls.append(1)
            o = outcomes[len(calls) - 1]
            if isinstance(o, BaseException):
                raise o
            return o
        try:
            return lab.retried("GET /x", call, tries=tries, sleep=slept.append), len(calls), slept
        except BaseException as e:
            return e, len(calls), slept

    def test_timeout_then_answer(self):
        r, n, slept = self.run_with([TimeoutError("read timed out"), (200, {})])
        self.assertEqual((r, n, slept), ((200, {}), 2, [2.0]))

    def test_gives_up_after_three(self):
        r, n, slept = self.run_with([urllib.error.URLError("refused")] * 3)
        self.assertIsInstance(r, urllib.error.URLError)
        self.assertEqual((n, slept), (3, [2.0, 4.0]))

    def test_http_error_is_not_retried(self):
        e = urllib.error.HTTPError("u", 500, "x", {}, None)
        r, n, _ = self.run_with([e, (200, {})])
        self.assertIs(r, e)
        self.assertEqual(n, 1)


class TestOrganizations(unittest.TestCase):
    def test_only_e2e_and_optest(self):
        self.assertTrue(clean_e2e.is_test("e2eacmes1kaaj"))
        self.assertTrue(clean_e2e.is_test("optestwk1", "optest-1@tenantry.site"))
        for slug in ("acme", "solar", "e2", "xe2e", "", None):
            self.assertFalse(clean_e2e.is_test(slug), slug)

    def test_pending_needs_a_test_owner(self):
        self.assertFalse(clean_e2e.is_test("e2ereject1", "alice@tenantry.site"))
        self.assertTrue(clean_e2e.is_test("e2ereject1", "e2emwrswx2j16y@tenantry.site"))


class PurgeAccounts(unittest.TestCase):
    def test_test_accounts_only(self):
        self.assertTrue(purge.is_test_account("e2emwrswx2j16y", "e2emwrswx2j16y@tenantry.site"))
        self.assertTrue(purge.is_test_account("optest-1", ""))
        for u in ("admin", "alice", "joe", "bob", "mallory", "service-account-tenantry-sync", "carol"):
            self.assertFalse(purge.is_test_account(u, f"{u}@tenantry.site"), u)
        # A protected local part wins over a test-looking username.
        self.assertFalse(purge.is_test_account("e2e-alice", "alice@tenantry.site"))


if __name__ == "__main__":
    unittest.main()
