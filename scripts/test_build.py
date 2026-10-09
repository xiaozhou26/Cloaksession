"""Packaging contracts that do not require platform build toolchains."""

import importlib.util
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("desktop_build", Path(__file__).with_name("build.py"))
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)


class BuildTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        paths = {
            "ROOT": self.root,
            "DESKTOP": self.root / "desktop",
            "FRONTEND": self.root / "desktop/frontend",
            "PLAYWRIGHT": self.root / "desktop/resources/playwright",
            "COMPANION": self.root / "desktop/resources/companion",
            "BIN": self.root / "desktop/build/bin",
            "DIST": self.root / "desktop/build/dist",
        }
        for name, path in paths.items():
            path.mkdir(parents=True, exist_ok=True)
            patcher = patch.object(build, name, path)
            patcher.start()
            self.addCleanup(patcher.stop)
        self.put("desktop/frontend/package.json", '{"version":"1.4.0"}')
        self.put("desktop/frontend/package-lock.json", '{"version":"1.4.0","packages":{"":{"version":"1.4.0"}}}')
        self.put("desktop/wails.json", '{"info":{"productVersion":"1.4.0"}}')
        self.put("desktop/go.mod", "module github.com/xiaozhou26/Cloaksession/desktop\n\nrequire github.com/wailsapp/wails/v2 v2.11.0\n")
        self.put("desktop/service.go", 'package main\nconst appVersion = "1.4.0"\n')
        self.put("LICENSE", "test license")
        self.put("desktop/resources/companion/manifest.json", "{}")
        self.put("desktop/resources/companion/cs.js", "// companion")
        dependencies = {"playwright-core": "1.63.0"}
        self.put("desktop/resources/playwright/package.json", json.dumps({"version": "1.4.0", "dependencies": dependencies}))
        self.put("desktop/resources/playwright/package-lock.json", json.dumps({"version": "1.4.0", "packages": {
            "": {"version": "1.4.0", "dependencies": dependencies}, "node_modules/playwright-core": {"version": "1.63.0"}}}))
        for name in ("bridge.mjs", "node_modules/playwright-core/package.json"):
            self.put(f"desktop/resources/playwright/{name}", "{}")

    def put(self, name, content):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return path

    def test_versions_match(self):
        self.assertEqual(build.verify_versions("v1.4.0"), "1.4.0")

    def test_wrong_tag_rejected(self):
        with self.assertRaisesRegex(RuntimeError, "Release tag"):
            build.verify_versions("v1.2.0")

    def test_stale_runtime_version_rejected(self):
        path = build.PLAYWRIGHT / "package.json"
        manifest = build.read_json(path)
        manifest["version"] = "1.3.0"
        path.write_text(json.dumps(manifest))
        with self.assertRaisesRegex(RuntimeError, "resources/playwright/package.json"):
            build.verify_versions()

    def test_stale_backend_version_rejected(self):
        self.put("desktop/service.go", 'package main\nconst appVersion = "1.3.0"\n')
        with self.assertRaisesRegex(RuntimeError, "service.go appVersion"):
            build.verify_versions()

    def test_missing_backend_version_rejected(self):
        self.put("desktop/service.go", "package main\n")
        with self.assertRaisesRegex(RuntimeError, "service.go appVersion"):
            build.verify_versions()

    def test_stale_frontend_lock_rejected(self):
        self.put("desktop/frontend/package-lock.json", '{"version":"1.4.0","packages":{"":{"version":"1.2.0"}}}')
        with self.assertRaisesRegex(RuntimeError, "packages root"):
            build.verify_versions()

    def test_wrong_wails_pin_rejected(self):
        self.put("desktop/go.mod", "module github.com/xiaozhou26/Cloaksession/desktop\nrequire github.com/wailsapp/wails/v2 v2.10.0\n")
        with self.assertRaisesRegex(RuntimeError, "must pin"):
            build.verify_versions()

    def test_linux_staging_and_archive(self):
        executable = self.put("desktop/build/bin/Cloaksession", "desktop")
        executable.chmod(0o755)
        self.put("desktop/build/bin/desktop-core", "obsolete")
        self.put("desktop/build/bin/resources/chromix/stale.js", "obsolete")
        with patch.object(build.sys, "platform", "linux"), patch.object(build.platform, "machine", return_value="x86_64"):
            build.stage_runtime()
            build.package("1.4.0", False)
        self.assertFalse((build.BIN / "desktop-core").exists())
        self.assertFalse((build.BIN / "resources/chromix").exists())
        self.assertEqual(executable.stat().st_mode & 0o777, 0o755)
        with tarfile.open(build.DIST / "Cloaksession-1.4.0-linux-amd64.tar.gz") as archive:
            names = archive.getnames()
        self.assertNotIn("Cloaksession/desktop-core", names)
        self.assertIn("Cloaksession/resources/playwright/node_modules/playwright-core/package.json", names)
        self.assertIn("Cloaksession/resources/companion/manifest.json", names)
        self.assertIn("Cloaksession/resources/LICENSE", names)

    def test_windows_staging(self):
        self.put("desktop/build/bin/Cloaksession.exe", "desktop")
        self.put("desktop/build/bin/desktop-core.exe", "obsolete")
        with patch.object(build.sys, "platform", "win32"):
            build.stage_runtime()
        self.assertFalse((build.BIN / "desktop-core.exe").exists())
        self.assertTrue((build.BIN / "resources/playwright/bridge.mjs").is_file())
        self.assertTrue((build.BIN / "resources/companion/cs.js").is_file())

    def test_macos_staging_resigns_after_copy(self):
        self.put("desktop/build/bin/Cloaksession.app/Contents/MacOS/Cloaksession", "desktop")
        with patch.object(build.sys, "platform", "darwin"), patch.object(build, "run") as run:
            build.stage_runtime()
        app = build.BIN / "Cloaksession.app"
        self.assertFalse((app / "Contents/MacOS/desktop-core").exists())
        self.assertTrue((app / "Contents/Resources/playwright/bridge.mjs").is_file())
        self.assertTrue((app / "Contents/Resources/companion/manifest.json").is_file())
        self.assertEqual(run.call_args_list[-1].args[0], ["codesign", "--verify", "--deep", "--strict", app])

    def test_missing_runtime_dependency_rejected(self):
        self.put("desktop/build/bin/Cloaksession", "desktop")
        (build.PLAYWRIGHT / "node_modules/playwright-core/package.json").unlink()
        with patch.object(build.sys, "platform", "linux"):
            with self.assertRaisesRegex(RuntimeError, "Missing packaged browser"):
                build.stage_runtime()

    def test_missing_companion_rejected(self):
        (build.COMPANION / "cs.js").unlink()
        with self.assertRaisesRegex(RuntimeError, "Missing companion"):
            build.stage_resources(build.BIN / "resources")

    def test_runtime_rejects_extra_dependencies(self):
        self.put("desktop/resources/playwright/package.json",
                 json.dumps({"dependencies": {"playwright-core": "1.63.0", "unexpected": "1.0.0"}}))
        with self.assertRaisesRegex(RuntimeError, "only on playwright-core"):
            build.verify_runtime_manifest()

    def test_runtime_rejects_stale_installed_packages(self):
        self.put("desktop/resources/playwright/node_modules/stale/package.json", "{}")
        with self.assertRaisesRegex(RuntimeError, "npm ci"):
            build.stage_browser_resources(build.BIN / "resources/playwright")

    def test_runtime_staging_excludes_unlisted_source_files(self):
        self.put("desktop/resources/playwright/obsolete/module.js", "obsolete")
        self.put("desktop/build/bin/resources/playwright/node_modules/stale/package.json", "{}")
        destination = build.BIN / "resources/playwright"
        build.stage_browser_resources(destination)
        self.assertFalse((destination / "obsolete").exists())
        self.assertFalse((destination / "node_modules/stale").exists())
        self.assertTrue((destination / "node_modules/playwright-core/package.json").is_file())

    def test_dependencies_are_reinstalled_from_lock(self):
        with patch.object(build, "run") as run:
            build.install_dependencies()
        self.assertEqual(run.call_args_list[-1].args[0], ["npm", "ci", "--omit=dev"])
        self.assertEqual(run.call_args_list[-1].kwargs["cwd"], build.PLAYWRIGHT)

    def test_universal_build_targets_go_only(self):
        self.put("desktop/icons/icon.png", "png")
        self.put("desktop/icons/icon.ico", "ico")
        executable = build.BIN / "Cloaksession.app/Contents/MacOS/Cloaksession"
        with patch.object(build.sys, "argv", ["build.py", "--universal"]), \
                patch.object(build.sys, "platform", "darwin"), \
                patch.object(build, "install_dependencies"), \
                patch.object(build, "stage_runtime", return_value=executable), \
                patch.object(build, "run") as run:
            build.main()
        commands = [call.args[0] for call in run.call_args_list]
        self.assertEqual([command[0] for command in commands], ["npm", "go", "lipo"])
        self.assertEqual(commands[1][-2:], ["-platform", "darwin/universal"])
        self.assertEqual(commands[-1], ["lipo", executable, "-verify_arch", "arm64", "x86_64"])

    def test_prepare_builds_embedded_frontend_and_stages_development_paths(self):
        self.put("desktop/icons/icon.png", "png")
        self.put("desktop/icons/icon.ico", "ico")
        with patch.object(build.sys, "argv", ["build.py", "--prepare"]), \
                patch.object(build.sys, "platform", "darwin"), \
                patch.object(build, "install_dependencies"), \
                patch.object(build, "run") as run:
            build.main()
        run.assert_called_once_with(["npm", "run", "build"], cwd=build.FRONTEND)
        self.assertFalse((build.BIN / "desktop-core").exists())
        self.assertTrue((build.BIN.parent / "Resources/playwright/bridge.mjs").is_file())
        self.assertTrue((build.DESKTOP / "build/appicon.png").is_file())

    def test_optional_go_race_tests_use_linux_webkit_tag(self):
        self.put("desktop/icons/icon.png", "png")
        self.put("desktop/icons/icon.ico", "ico")
        with patch.object(build.sys, "argv", ["build.py", "--prepare", "--test", "--webkit2-41"]), \
                patch.object(build.sys, "platform", "linux"), \
                patch.object(build, "install_dependencies"), \
                patch.object(build, "run") as run:
            build.main()
        commands = [call.args[0] for call in run.call_args_list]
        self.assertEqual(commands[1], ["go", "test", "-race", "-count=1", "-mod=readonly", "-tags", "webkit2_41", "./..."])
        self.assertTrue((build.BIN / "resources/playwright/bridge.mjs").is_file())

    def test_stale_runtime_lock_version_rejected(self):
        path = build.PLAYWRIGHT / "package-lock.json"
        manifest = build.read_json(path)
        manifest["packages"][""]["version"] = "1.3.0"
        path.write_text(json.dumps(manifest))
        with self.assertRaisesRegex(RuntimeError, "resources/playwright/package-lock.json packages root"):
            build.verify_versions()

    def test_windows_installer_payload_and_update_suffix(self):
        def fake_run(command):
            output = next(value.removeprefix("/DOUTPUT=") for value in command if value.startswith("/DOUTPUT="))
            Path(output).write_bytes(b"installer")
        with patch.object(build.sys, "platform", "win32"), patch.object(build.platform, "machine", return_value="AMD64"), \
                patch.object(build, "find_makensis", return_value="makensis"), patch.object(build, "run", side_effect=fake_run) as run:
            build.package("1.4.0", False)
        self.assertTrue((build.DIST / "Cloaksession-1.4.0-windows-amd64-setup.exe").is_file())
        self.assertIn(f"/DPAYLOAD={build.BIN}", run.call_args.args[0])


if __name__ == "__main__":
    unittest.main()
