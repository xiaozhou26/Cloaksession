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
            "CHROMIX": self.root / "crates/desktop-core/resources/chromix",
            "BIN": self.root / "desktop/build/bin",
            "DIST": self.root / "desktop/build/dist",
        }
        for name, path in paths.items():
            path.mkdir(parents=True, exist_ok=True)
            patcher = patch.object(build, name, path)
            patcher.start()
            self.addCleanup(patcher.stop)
        self.put("crates/desktop-core/Cargo.toml", '[package]\nname = "desktop-core"\nversion = "1.3.0"\n')
        self.put("Cargo.lock", '[[package]]\nname = "desktop-core"\nversion = "1.3.0"\n')
        self.put("desktop/frontend/package.json", '{"version":"1.3.0"}')
        self.put("desktop/frontend/package-lock.json", '{"version":"1.3.0","packages":{"":{"version":"1.3.0"}}}')
        self.put("desktop/wails.json", '{"info":{"productVersion":"1.3.0"}}')
        self.put("desktop/go.mod", "module github.com/xiaozhou26/Cloaksession/desktop\n\nrequire github.com/wailsapp/wails/v2 v2.11.0\n")
        self.put("LICENSE", "test license")
        dependencies = {"playwright-core": "1.63.0"}
        self.put("crates/desktop-core/resources/chromix/package.json", json.dumps({"dependencies": dependencies}))
        self.put("crates/desktop-core/resources/chromix/package-lock.json", json.dumps({"packages": {
            "": {"dependencies": dependencies}, "node_modules/playwright-core": {"version": "1.63.0"}}}))
        for name in ("bridge.mjs", "node_modules/playwright-core/package.json"):
            self.put(f"crates/desktop-core/resources/chromix/{name}", "{}")

    def put(self, name, content):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return path

    def test_versions_match(self):
        self.assertEqual(build.verify_versions("v1.3.0"), "1.3.0")

    def test_wrong_tag_rejected(self):
        with self.assertRaisesRegex(RuntimeError, "Release tag"):
            build.verify_versions("v1.2.0")

    def test_stale_cargo_lock_rejected(self):
        self.put("Cargo.lock", '[[package]]\nname = "desktop-core"\nversion = "1.2.0"\n')
        with self.assertRaisesRegex(RuntimeError, "Cargo.lock"):
            build.verify_versions()

    def test_stale_frontend_lock_rejected(self):
        self.put("desktop/frontend/package-lock.json", '{"version":"1.3.0","packages":{"":{"version":"1.2.0"}}}')
        with self.assertRaisesRegex(RuntimeError, "packages root"):
            build.verify_versions()

    def test_wrong_wails_pin_rejected(self):
        self.put("desktop/go.mod", "module github.com/xiaozhou26/Cloaksession/desktop\nrequire github.com/wailsapp/wails/v2 v2.10.0\n")
        with self.assertRaisesRegex(RuntimeError, "must pin"):
            build.verify_versions()

    def test_linux_staging_and_archive(self):
        self.put("desktop/build/bin/Cloaksession", "desktop")
        sidecar = self.put("target/release/desktop-core", "core")
        sidecar.chmod(0o755)
        self.put("desktop/build/bin/resources/chromix/stale.js", "stale")
        with patch.object(build.sys, "platform", "linux"), patch.object(build.platform, "machine", return_value="x86_64"):
            build.stage_runtime(sidecar)
            build.package("1.3.0", False)
        self.assertFalse((build.BIN / "resources/chromix/stale.js").exists())
        self.assertEqual((build.BIN / "desktop-core").stat().st_mode & 0o777, 0o755)
        with tarfile.open(build.DIST / "Cloaksession-1.3.0-linux-amd64.tar.gz") as archive:
            names = archive.getnames()
        self.assertIn("Cloaksession/desktop-core", names)
        self.assertIn("Cloaksession/resources/chromix/node_modules/playwright-core/package.json", names)
        self.assertIn("Cloaksession/resources/LICENSE", names)

    def test_windows_staging(self):
        self.put("desktop/build/bin/Cloaksession.exe", "desktop")
        sidecar = self.put("target/release/desktop-core.exe", "core")
        with patch.object(build.sys, "platform", "win32"):
            build.stage_runtime(sidecar)
        self.assertTrue((build.BIN / "desktop-core.exe").is_file())
        self.assertTrue((build.BIN / "resources/chromix/bridge.mjs").is_file())

    def test_macos_staging_resigns_after_copy(self):
        self.put("desktop/build/bin/Cloaksession.app/Contents/MacOS/Cloaksession", "desktop")
        sidecar = self.put("target/release/desktop-core", "core")
        with patch.object(build.sys, "platform", "darwin"), patch.object(build, "run") as run:
            build.stage_runtime(sidecar)
        app = build.BIN / "Cloaksession.app"
        self.assertTrue((app / "Contents/MacOS/desktop-core").is_file())
        self.assertTrue((app / "Contents/Resources/chromix/bridge.mjs").is_file())
        self.assertEqual(run.call_args_list[-1].args[0], ["codesign", "--verify", "--deep", "--strict", app])

    def test_missing_runtime_dependency_rejected(self):
        self.put("desktop/build/bin/Cloaksession", "desktop")
        sidecar = self.put("target/release/desktop-core", "core")
        (build.CHROMIX / "node_modules/playwright-core/package.json").unlink()
        with patch.object(build.sys, "platform", "linux"):
            with self.assertRaisesRegex(RuntimeError, "Missing packaged browser"):
                build.stage_runtime(sidecar)

    def test_runtime_rejects_extra_dependencies(self):
        self.put("crates/desktop-core/resources/chromix/package.json",
                 json.dumps({"dependencies": {"playwright-core": "1.63.0", "unexpected": "1.0.0"}}))
        with self.assertRaisesRegex(RuntimeError, "only on playwright-core"):
            build.verify_runtime_manifest()

    def test_runtime_rejects_stale_installed_packages(self):
        self.put("crates/desktop-core/resources/chromix/node_modules/stale/package.json", "{}")
        with self.assertRaisesRegex(RuntimeError, "npm ci"):
            build.stage_browser_resources(build.BIN / "resources/chromix")

    def test_runtime_staging_excludes_unlisted_source_files(self):
        self.put("crates/desktop-core/resources/chromix/obsolete/module.js", "obsolete")
        self.put("desktop/build/bin/resources/chromix/node_modules/stale/package.json", "{}")
        destination = build.BIN / "resources/chromix"
        build.stage_browser_resources(destination)
        self.assertFalse((destination / "obsolete").exists())
        self.assertFalse((destination / "node_modules/stale").exists())
        self.assertTrue((destination / "node_modules/playwright-core/package.json").is_file())

    def test_dependencies_are_reinstalled_from_lock(self):
        with patch.object(build, "run") as run:
            build.install_dependencies()
        self.assertEqual(run.call_args_list[-1].args[0], ["npm", "ci", "--omit=dev"])
        self.assertEqual(run.call_args_list[-1].kwargs["cwd"], build.CHROMIX)

    def test_universal_build_uses_both_rust_targets(self):
        target = self.root / "custom-target"
        with patch.object(build, "run", return_value=json.dumps({"target_directory": str(target)})) as run:
            result = build.rust_sidecar(True)
        commands = [call.args[0] for call in run.call_args_list]
        for triple in ("aarch64-apple-darwin", "x86_64-apple-darwin"):
            self.assertIn(["cargo", "build", "--release", "--locked", "-p", "desktop-core", "--target", triple], commands)
        self.assertEqual(result, target / "universal-apple-darwin/release/desktop-core")
        self.assertEqual(commands[-1], ["lipo", result, "-verify_arch", "arm64", "x86_64"])

    def test_prepare_builds_embedded_frontend_and_stages_development_paths(self):
        sidecar = self.put("target/release/desktop-core", "core")
        self.put("desktop/icons/icon.png", "png")
        self.put("desktop/icons/icon.ico", "ico")
        with patch.object(build.sys, "argv", ["build.py", "--prepare"]), \
                patch.object(build.sys, "platform", "darwin"), \
                patch.object(build, "install_dependencies"), \
                patch.object(build, "rust_sidecar", return_value=sidecar), \
                patch.object(build, "run") as run:
            build.main()
        run.assert_called_once_with(["npm", "run", "build"], cwd=build.FRONTEND)
        self.assertTrue((build.BIN / "desktop-core").is_file())
        self.assertTrue((build.BIN.parent / "Resources/chromix/bridge.mjs").is_file())
        self.assertTrue((build.DESKTOP / "build/appicon.png").is_file())

    def test_windows_installer_payload_and_update_suffix(self):
        def fake_run(command):
            output = next(value.removeprefix("/DOUTPUT=") for value in command if value.startswith("/DOUTPUT="))
            Path(output).write_bytes(b"installer")
        with patch.object(build.sys, "platform", "win32"), patch.object(build.platform, "machine", return_value="AMD64"), \
                patch.object(build, "find_makensis", return_value="makensis"), patch.object(build, "run", side_effect=fake_run) as run:
            build.package("1.3.0", False)
        self.assertTrue((build.DIST / "Cloaksession-1.3.0-windows-amd64-setup.exe").is_file())
        self.assertIn(f"/DPAYLOAD={build.BIN}", run.call_args.args[0])


if __name__ == "__main__":
    unittest.main()
