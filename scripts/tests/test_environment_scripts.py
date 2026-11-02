"""Offline configuration and failure-path tests; Docker Compose is required.

No containers are started. The lifecycle tests replace docker/curl with stubs.
"""

import base64
import importlib.util
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("production_config", ROOT / "scripts/validate-production-config.py")
validator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(validator)


def production_environment():
    return {
        "MIFOLYO_QUERY_IMAGE": "mifolyo-environment-query:smoke",
        "MIFOLYO_FORUM_IMAGE": "mifolyo-environment-forum:smoke",
        "MIFOLYO_PUBLIC_URL": "https://environment-smoke.example.ts.net",
        "MIFOLYO_HTTP_PORT": "18190",
        "QUERY_APP_KEY": "base64:" + base64.b64encode(secrets.token_bytes(32)).decode(),
        "FORUM_APP_KEY": "base64:" + base64.b64encode(secrets.token_bytes(32)).decode(),
        **{key: secrets.token_hex(32) for key in (
            "MONGO_ROOT_PASSWORD", "MONGO_QUERY_PASSWORD", "POSTGRES_PASSWORD", "REDIS_PASSWORD")},
        "MAIL_SCHEME": "smtp",
        "MAIL_HOST": "mifolyo-environment-smoke-mail",
        "MAIL_PORT": "1025",
        "MAIL_FROM_ADDRESS": "accounts@smoke.mifolyo.test",
    }


def clean_environment():
    return {key: value for key, value in os.environ.items()
            if not key.startswith(("MIFOLYO_", "COMPOSE_", "SEARCH_UI_STAGING_", "MAIL_"))
            and key not in production_environment()}


class EnvironmentScriptsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="mifolyo-env-tests-")
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.env = clean_environment()
        self.values = production_environment()
        self.env_file = self.directory / "production.env"
        self.env_file.write_text("".join(f"{key}={value}\n" for key, value in self.values.items()))
        self.env_file.chmod(0o600)

    def run_script(self, script, *arguments, **overrides):
        return subprocess.run(["bash", str(ROOT / "scripts" / script), *arguments],
                              env={**self.env, **overrides}, cwd=self.directory,
                              capture_output=True, text=True, timeout=30)

    def production_config(self):
        result = subprocess.run([
            "docker", "compose", "--env-file", str(self.env_file), "--file",
            str(ROOT / "scripts/docker/production.compose.yml"),
            "--profile", "tools", "config", "--format", "json"],
            env=self.env, capture_output=True, text=True, check=True)
        return json.loads(result.stdout)

    def fake_tools(self, fail_on):
        config = self.production_config()
        (self.directory / "config.json").write_text(json.dumps(config))
        docker = self.directory / "docker"
        docker.write_text(f"#!{sys.executable}\n" + '''
import json, os, pathlib, sys
directory = pathlib.Path(__file__).parent
args = sys.argv[1:]
with (directory / "calls.jsonl").open("a") as log:
    log.write(json.dumps(args) + "\\n")
if os.environ["FAIL_ON"] in " ".join(args):
    sys.exit(42)
if args[:2] == ["compose", "version"]:
    print(os.environ.get("COMPOSE_TEST_VERSION", "5.1.3"))
if "config" in args and "--format" in args:
    print((directory / "config.json").read_text())
if "config" in args and "--images" in args:
    print("mifolyo-environment-query:smoke")
if "ps" in args and "--quiet" in args:
    print("asset-" + args[-1])
if args[0] == "wait":
    print(os.environ.get("ASSET_EXIT", "0"))
''')
        docker.chmod(0o755)
        curl = self.directory / "curl"
        curl.write_text("#!/bin/sh\nexit 0\n")
        curl.chmod(0o755)
        self.env.update(PATH=f"{self.directory}:{self.env['PATH']}", FAIL_ON=fail_on)

    def calls(self):
        return [json.loads(line) for line in (self.directory / "calls.jsonl").read_text().splitlines()]

    def test_help_does_not_require_configuration(self):
        for script in ("test-env.sh", "production-env.sh"):
            result = self.run_script(script, "--help")
            self.assertEqual(result.returncode, 0, result.stderr)

    def test_test_configuration_ignores_ambient_compose_target(self):
        result = self.run_script("test-env.sh", "config", COMPOSE_FILE="/missing.yml",
                                 COMPOSE_PROJECT_NAME="production", COMPOSE_PROFILES="crawl,render")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("mifolyo-app-test", result.stdout)

    def test_test_project_cannot_select_retained_or_production_stack(self):
        for project in ("mifolyo-v1-baseline-test", "mifolyo-production", "mifolyo-search-ui-staging"):
            self.assertNotEqual(self.run_script("test-env.sh", "stop", MIFOLYO_TEST_PROJECT=project).returncode, 0)

    def test_invalid_ports_and_arguments_are_rejected(self):
        for port in ("0", "65536", "abc", "-1", "080", "18446744073709551617"):
            self.assertNotEqual(self.run_script("test-env.sh", "up", SEARCH_UI_STAGING_HTTP_PORT=port).returncode, 0)
        self.assertNotEqual(self.run_script("test-env.sh", "up", SEARCH_UI_STAGING_HTTP_PORT="18182").returncode, 0)
        for script in ("test-env.sh", "production-env.sh"):
            self.assertNotEqual(self.run_script(script, "logs", "--follow").returncode, 0)
            self.assertNotEqual(self.run_script(script, "up", "--volumes").returncode, 0)

    def test_production_requires_config_and_correct_project(self):
        self.assertNotEqual(self.run_script("production-env.sh", "--env-file", "/missing", "up").returncode, 0)
        self.assertNotEqual(self.run_script("production-env.sh", "--project", "mifolyo-app-test", "stop").returncode, 0)

    def test_valid_configuration_does_not_print_secrets(self):
        result = self.run_script("production-env.sh", "--env-file", str(self.env_file), "config")
        self.assertEqual(result.returncode, 0, result.stderr)
        for key in ("QUERY_APP_KEY", "POSTGRES_PASSWORD", "MONGO_ROOT_PASSWORD"):
            self.assertNotIn(self.values[key], result.stdout + result.stderr)

    def test_production_private_networks_and_persistence(self):
        config = self.production_config()
        self.assertEqual(validator.validate(config), (18190, self.values["MIFOLYO_PUBLIC_URL"]))
        services = config["services"]
        self.assertEqual(set(name for name, service in services.items() if service.get("ports")), {"caddy"})
        self.assertTrue(config["networks"]["data"]["internal"])
        self.assertTrue(config["networks"]["web"]["internal"])
        self.assertEqual(services["query-engine"]["environment"]["SESSION_DRIVER"], "redis")
        self.assertTrue(any(volume["target"] == "/data" for volume in services["redis"]["volumes"]))
        self.assertIn("--queue=moderation,default", services["forum-worker"]["command"])
        self.assertEqual(services["query-assets"]["image"], services["query-engine"]["image"])
        self.assertEqual(services["forum-assets"]["image"], services["forum-engine"]["image"])
        self.assertNotIn("spider", services)
        self.assertNotIn("mail", services)

    def test_unsafe_production_settings_rejected(self):
        config = self.production_config()
        cases = [
            ("query-engine", "APP_KEY", "base64:" + "A" * 43 + "="),
            ("query-engine", "APP_URL", "http://localhost"),
            ("query-engine", "APP_URL", "https://public.example.com"),
            ("query-engine", "REDIS_PASSWORD", "password"),
            ("forum-engine", "MAIL_PORT", "0"),
        ]
        for service, key, value in cases:
            altered = json.loads(json.dumps(config))
            altered["services"][service]["environment"][key] = value
            with self.subTest(service=service, key=key), self.assertRaises(ValueError):
                validator.validate(altered)
        config["services"]["query-engine"]["image"] = "query:latest"
        with self.assertRaises(ValueError):
            validator.validate(config)

    def test_failed_pull_does_not_stop_running_production(self):
        self.fake_tools("pull")
        result = self.run_script("production-env.sh", "--env-file", str(self.env_file), "up")
        self.assertEqual(result.returncode, 42)
        self.assertFalse(any("stop" in call or "up" in call or "run" in call for call in self.calls()))

    def test_failed_migration_does_not_start_app_or_assets(self):
        self.fake_tools("run --rm --no-deps --pull never forum-migrate")
        result = self.run_script("production-env.sh", "--env-file", str(self.env_file), "up", "--no-pull")
        self.assertEqual(result.returncode, 42)
        self.assertFalse(any("up" in call and "query-engine" in call for call in self.calls()))
        self.assertFalse(any("up" in call and "query-assets" in call for call in self.calls()))

    def test_failed_asset_copy_does_not_start_app(self):
        self.fake_tools("wait asset-forum-assets")
        result = self.run_script("production-env.sh", "--env-file", str(self.env_file), "up", "--no-pull")
        self.assertEqual(result.returncode, 42)
        self.assertFalse(any("up" in call and "query-engine" in call for call in self.calls()))

    def test_exited_asset_container_failure_is_not_a_successful_wait(self):
        self.fake_tools("never-matches")
        result = self.run_script("production-env.sh", "--env-file", str(self.env_file), "up", "--no-pull", ASSET_EXIT="1")
        self.assertEqual(result.returncode, 1)
        self.assertIn("query-assets failed with exit status 1", result.stderr)
        self.assertFalse(any("up" in call and "query-engine" in call for call in self.calls()))

    def test_missing_local_image_does_not_stop_production(self):
        self.fake_tools("image inspect")
        result = self.run_script("production-env.sh", "--env-file", str(self.env_file), "up", "--no-pull")
        self.assertEqual(result.returncode, 1)
        self.assertFalse(any("stop" in call or "up" in call for call in self.calls()))

    def test_unsupported_compose_fails_before_any_lifecycle_command(self):
        self.fake_tools("never-matches")
        result = self.run_script("production-env.sh", "--env-file", str(self.env_file), "up", COMPOSE_TEST_VERSION="2.23.0")
        self.assertEqual(result.returncode, 1)
        self.assertIn("2.24 or newer", result.stderr)
        self.assertEqual(len(self.calls()), 1)

    def test_failed_test_build_does_not_start_or_seed(self):
        self.fake_tools("build query-engine forum-engine")
        result = self.run_script("test-env.sh", "up")
        self.assertEqual(result.returncode, 42)
        self.assertFalse(any("up" in call or "exec" in call for call in self.calls()))


if __name__ == "__main__":
    unittest.main()
