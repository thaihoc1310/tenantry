"""python3 -m unittest deploy/keycloak/test_realm.py: what the realm file must
keep for logout to work end to end (ADR-0006, ADR-0022)."""
import json
import os
import unittest

REALM = json.load(open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "realm-tenantry.json")))


def client(client_id):
    return next(c for c in REALM["clients"] if c["clientId"] == client_id)


class Logout(unittest.TestCase):
    def test_headlamp_may_come_back_after_logout(self):
        # Headlamp (v0.46) ends the Keycloak session itself and asks to come
        # back to /encoded-clusters/<cluster>/login; anything else on the host
        # stays refused.
        uris = client("headlamp-cluster-a")["attributes"]["post.logout.redirect.uris"].split("##")
        self.assertEqual(uris, ["https://${CONSOLE_HOST}/", "https://${CONSOLE_HOST}/encoded-clusters/*"])

    def test_portal_comes_back_to_its_root(self):
        self.assertEqual(client("portal")["attributes"]["post.logout.redirect.uris"], "https://${PORTAL_HOST}/")


if __name__ == "__main__":
    unittest.main()
