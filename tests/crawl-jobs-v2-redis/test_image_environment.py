"""Target-kernel network-none observations; no container or network is started."""
import copy
import unittest

import executor
import harness


class ImageNetworkTests(unittest.TestCase):
    def setUp(self):
        self.interfaces = {"lo": {"flags": "0x9", "operstate": "unknown"},
                           "tunl0": {"flags": "0x80", "operstate": "down"},
                           "gretap0": {"flags": "0x1002", "operstate": "down"}}
        self.v4 = "Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\n"
        self.v6 = ("0" * 32 + " 00 " + "0" * 32 + " 00 " + "0" * 32 +
                   " ffffffff 00000001 00000000 00200200 lo\n")

    def test_only_down_fallbacks_and_reject_routes_are_accepted(self):
        result = executor.validate_network(self.interfaces, self.v4, self.v6)
        self.assertEqual(result["external_routes"], 0)
        self.assertEqual(result["inactive_fallbacks"], ["gretap0", "tunl0"])
        executor.validate_network({"lo": self.interfaces["lo"]}, self.v4, "")

    def test_real_interfaces_active_tunnels_and_external_routes_reject(self):
        for name, fields in (("eth0", {"flags": "0x0", "operstate": "down"}),
                             ("tunl0", {"flags": "0x81", "operstate": "down"}),
                             ("tunl0", {"flags": "0x80", "operstate": "up"})):
            changed = copy.deepcopy(self.interfaces)
            changed[name] = fields
            with self.assertRaises(harness.InvalidArtifact):
                executor.validate_network(changed, self.v4, self.v6)
        for v4, v6 in ((self.v4 + "eth0 00000000 0100000A\n", self.v6),
                       (self.v4, self.v6.replace("lo", "tunl0")),
                       (self.v4, self.v6.replace("00200200", "00000001")),
                       (self.v4, self.v6.replace("00 " + "0" * 32 + " ffffffff", "00 " + "0" * 31 + "1 ffffffff"))):
            with self.assertRaises(harness.InvalidArtifact):
                executor.validate_network(self.interfaces, v4, v6)


if __name__ == "__main__":
    unittest.main()
