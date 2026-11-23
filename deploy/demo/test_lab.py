"""python3 -m unittest deploy/demo/test_lab.py: the retry of lab.retried."""
import os
import sys
import unittest
import urllib.error

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lab  # noqa: E402


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


if __name__ == "__main__":
    unittest.main()
