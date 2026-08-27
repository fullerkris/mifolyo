import copy
import json
import unittest
from pathlib import Path

import jsonschema


REPO_ROOT = Path(__file__).resolve().parents[3]


class PolicyContractTests(unittest.TestCase):
    def test_render_schema_and_disabled_policy(self):
        schema = json.loads(
            (REPO_ROOT / "contracts" / "render-policy-v1.schema.json").read_text(
                encoding="utf-8"
            )
        )
        disabled = json.loads(
            (
                REPO_ROOT
                / "services"
                / "spider"
                / "config"
                / "render-policy-v1.disabled.json"
            ).read_text(encoding="utf-8")
        )
        validator = jsonschema.Draft202012Validator(schema)
        validator.check_schema(schema)
        validator.validate(disabled)

        rule = {
            "id": "fonts-shell",
            "enabled": False,
            "host_rule": {"host": "fonts.google.com", "match": "exact"},
            "allow_paths": ["/"],
            "allow_path_prefixes": [],
            "deny_path_prefixes": [],
            "mode": "brokered",
            "failure_action": "reject_page",
            "resource_rules": [
                {
                    "host_rule": {"host": "fonts.gstatic.com", "match": "exact"},
                    "allow_paths": [],
                    "allow_path_prefixes": ["/s/"],
                    "deny_path_prefixes": [],
                    "allowed_types": ["script", "stylesheet"],
                }
            ],
            "network_controls": {
                "allowed_methods": ["GET"],
                "robots_for_resources": True,
                "allow_cookies": False,
                "allow_service_workers": False,
                "allow_websockets": False,
                "allow_webrtc": False,
                "allow_downloads": False,
                "allow_popups": False,
                "allow_secondary_documents": False,
                "allow_javascript_navigation": False,
            },
            "limits": {
                "max_render_time_ms": 10000,
                "settle_time_ms": 500,
                "max_resource_requests": 16,
                "max_aggregate_resource_bytes": 8388608,
                "max_resource_body_bytes": 2097152,
                "max_rendered_dom_bytes": 5242880,
                "max_dom_nodes": 50000,
                "max_redirect_hops": 2,
                "max_console_bytes": 8192,
            },
        }
        policy = {"schema_version": 1, "default_action": "deny", "rules": [rule]}
        validator.validate(policy)

        inline_rule = copy.deepcopy(rule)
        inline_rule["id"] = "inline-shell"
        inline_rule["mode"] = "inline_only"
        inline_rule["resource_rules"] = []
        inline_rule["limits"]["max_resource_requests"] = 0
        inline_rule["limits"]["max_aggregate_resource_bytes"] = 0
        inline_rule["limits"]["max_resource_body_bytes"] = 0
        inline_rule["limits"]["max_redirect_hops"] = 0
        inline_policy = {
            "schema_version": 1,
            "default_action": "deny",
            "rules": [inline_rule],
        }
        validator.validate(inline_policy)

        invalid = copy.deepcopy(inline_policy)
        invalid["rules"][0]["limits"]["max_redirect_hops"] = 1
        self.assertTrue(list(validator.iter_errors(invalid)))

        invalid = copy.deepcopy(policy)
        invalid["rules"][0]["host_rule"]["match"] = "apex_and_subdomains"
        self.assertTrue(list(validator.iter_errors(invalid)))

        invalid = copy.deepcopy(policy)
        invalid["rules"][0]["allow_paths"] = []
        self.assertTrue(list(validator.iter_errors(invalid)))

        invalid = copy.deepcopy(policy)
        invalid["rules"][0]["allow_paths"] = []
        invalid["rules"][0]["allow_path_prefixes"] = ["/"]
        self.assertTrue(list(validator.iter_errors(invalid)))

        invalid = copy.deepcopy(policy)
        invalid["rules"][0]["resource_rules"][0]["allowed_types"] = ["fetch"]
        self.assertTrue(list(validator.iter_errors(invalid)))

        invalid = copy.deepcopy(policy)
        invalid["rules"][0]["resource_rules"].append(
            copy.deepcopy(invalid["rules"][0]["resource_rules"][0])
        )
        self.assertTrue(list(validator.iter_errors(invalid)))

        invalid = copy.deepcopy(policy)
        invalid["rules"][0]["resource_rules"][0]["allow_path_prefixes"] = []
        self.assertTrue(list(validator.iter_errors(invalid)))

    def test_reddit_export_fixture_matches_contract(self):
        schema = json.loads(
            (REPO_ROOT / "contracts" / "reddit-export-v1.schema.json").read_text(
                encoding="utf-8"
            )
        )
        bundled_schema = json.loads(
            (
                REPO_ROOT
                / "services"
                / "seed-importer"
                / "reddit-export-v1.schema.json"
            ).read_text(encoding="utf-8")
        )
        self.assertEqual(schema, bundled_schema)
        export = json.loads(
            (
                REPO_ROOT
                / "services"
                / "seed-importer"
                / "testdata"
                / "reddit-listing.json"
            ).read_text(encoding="utf-8")
        )
        validator = jsonschema.Draft202012Validator(schema)
        validator.check_schema(schema)
        validator.validate(export)


if __name__ == "__main__":
    unittest.main()
