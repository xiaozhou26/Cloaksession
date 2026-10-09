#!/usr/bin/env python3
"""Build the Wails desktop, stage its browser resources, and optionally package it."""

import argparse
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tarfile

ROOT = Path(__file__).resolve().parents[1]
DESKTOP = ROOT / "desktop"
FRONTEND = DESKTOP / "frontend"
PLAYWRIGHT = DESKTOP / "resources/playwright"
COMPANION = DESKTOP / "resources/companion"
REVERSE = DESKTOP / "resources/reverse"
REVERSE_VERSION = "4.0.5"
PATCHRIGHT_VERSION = "1.61.1-mcp.2"
BIN = DESKTOP / "build/bin"
DIST = DESKTOP / "build/dist"
WAILS_VERSION = "v2.11.0"
WAILS_MODULE = "github.com/wailsapp/wails/v2"


def run(args, cwd=ROOT, capture=False):
    args = [str(arg) for arg in args]
    executable = shutil.which(args[0])
    if executable is None:
        raise RuntimeError(f"Required executable not found: {args[0]}")
    args[0] = executable
    print("+ " + subprocess.list2cmdline(args), flush=True)
    result = subprocess.run(args, cwd=cwd, check=True, text=True,
                            stdout=subprocess.PIPE if capture else None)
    return result.stdout if capture else None


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def verify_versions(tag=None):
    version = read_json(DESKTOP / "wails.json")["info"]["productVersion"]
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        raise RuntimeError("Desktop version must be a numeric major.minor.patch version")
    backend_version = re.search(r'(?m)^const appVersion = "([^"]+)"\s*$',
                                (DESKTOP / "service.go").read_text(encoding="utf-8"))
    if backend_version is None:
        raise RuntimeError("service.go appVersion is missing")
    lock = read_json(FRONTEND / "package-lock.json")
    versions = {
        "service.go appVersion": backend_version.group(1),
        "frontend/package.json": read_json(FRONTEND / "package.json")["version"],
        "frontend/package-lock.json": lock["version"],
        "frontend/package-lock.json packages root": lock["packages"][""]["version"],
        "resources/playwright/package.json": read_json(PLAYWRIGHT / "package.json")["version"],
        "resources/playwright/package-lock.json": read_json(PLAYWRIGHT / "package-lock.json")["version"],
        "resources/playwright/package-lock.json packages root": read_json(PLAYWRIGHT / "package-lock.json")["packages"][""]["version"],
        "resources/reverse/package.json": read_json(REVERSE / "package.json")["version"],
        "resources/reverse/package-lock.json": read_json(REVERSE / "package-lock.json")["version"],
        "resources/reverse/package-lock.json packages root": read_json(REVERSE / "package-lock.json")["packages"][""]["version"],
    }
    for source, actual in versions.items():
        if actual != version:
            raise RuntimeError(f"{source}: {actual!r} does not match Wails productVersion {version}")
    go_mod = (DESKTOP / "go.mod").read_text()
    if not re.search(r"(?m)^module github\.com/xiaozhou26/Cloaksession/desktop\s*$", go_mod):
        raise RuntimeError("Unexpected desktop Go module path")
    if not re.search(rf"(?m)^\s*(?:require\s+)?{re.escape(WAILS_MODULE)}\s+{re.escape(WAILS_VERSION)}\s*$", go_mod):
        raise RuntimeError(f"desktop/go.mod must pin {WAILS_MODULE} {WAILS_VERSION}")
    if tag is not None and tag != f"v{version}":
        raise RuntimeError(f"Release tag {tag!r} does not match v{version}")
    print(f"Verified desktop version {version}, Wails {WAILS_VERSION}", flush=True)
    return version


def verify_runtime_manifest():
    manifest = read_json(PLAYWRIGHT / "package.json")
    lock = read_json(PLAYWRIGHT / "package-lock.json")
    dependencies = manifest.get("dependencies", {})
    if set(dependencies) != {"playwright-core"} or manifest.get("devDependencies"):
        raise RuntimeError("Browser bridge must depend only on playwright-core")
    packages = lock.get("packages", {})
    if set(packages) != {"", "node_modules/playwright-core"}:
        raise RuntimeError("Browser bridge lock must contain only playwright-core")
    if packages[""].get("dependencies") != dependencies:
        raise RuntimeError("Browser bridge manifest and lock dependencies do not match")


def verify_reverse_manifest():
    manifest = read_json(REVERSE / "package.json")
    packages = read_json(REVERSE / "package-lock.json").get("packages", {})
    dependencies = {"js-reverse-mcp": REVERSE_VERSION}
    if manifest.get("dependencies") != dependencies or manifest.get("devDependencies"):
        raise RuntimeError(f"Reverse bridge must depend only on js-reverse-mcp {REVERSE_VERSION}")
    if packages.get("", {}).get("dependencies") != dependencies:
        raise RuntimeError("Reverse bridge manifest and lock dependencies do not match")
    expected = {
        "js-reverse-mcp": REVERSE_VERSION,
        "@zhizhuodemao/patchright": PATCHRIGHT_VERSION,
        "@zhizhuodemao/patchright-core": PATCHRIGHT_VERSION,
    }
    for name, version in expected.items():
        if packages.get(f"node_modules/{name}", {}).get("version") != version:
            raise RuntimeError(f"Reverse bridge lock must pin {name} {version}")
    upstream = packages["node_modules/js-reverse-mcp"].get("dependencies", {})
    if upstream.get("@zhizhuodemao/patchright") != PATCHRIGHT_VERSION:
        raise RuntimeError("Reverse bridge must use the upstream dedicated patchright pin")


def install_dependencies():
    verify_runtime_manifest()
    verify_reverse_manifest()
    run(["npm", "ci", "--legacy-peer-deps"], cwd=FRONTEND)
    run(["npm", "ci", "--omit=dev"], cwd=PLAYWRIGHT)
    run(["npm", "ci", "--omit=dev", "--omit=optional"], cwd=REVERSE)


def replace_tree(source, destination):
    if destination.exists():
        shutil.rmtree(destination)
    shutil.copytree(source, destination, symlinks=False,
                    ignore=shutil.ignore_patterns(".git", ".DS_Store", "npm-debug.log"))


def stage_browser_resources(destination):
    verify_runtime_manifest()
    required = ["bridge.mjs", "package.json", "package-lock.json", "node_modules/playwright-core/package.json"]
    for name in required:
        if not (PLAYWRIGHT / name).is_file():
            raise RuntimeError(f"Missing packaged browser resource: {name}")
    installed = {path.name for path in (PLAYWRIGHT / "node_modules").iterdir()}
    if installed - {"playwright-core", ".bin", ".package-lock.json"}:
        raise RuntimeError("Unexpected browser dependencies; run npm ci --omit=dev before packaging")
    if destination.exists():
        shutil.rmtree(destination)
    destination.mkdir(parents=True)
    for name in ("bridge.mjs", "package.json", "package-lock.json"):
        shutil.copy2(PLAYWRIGHT / name, destination / name)
    replace_tree(PLAYWRIGHT / "node_modules", destination / "node_modules")


def stage_reverse_resources(destination):
    verify_reverse_manifest()
    for name in ("bridge.mjs", "windows.mjs", "package.json", "package-lock.json"):
        if not (REVERSE / name).is_file():
            raise RuntimeError(f"Missing packaged reverse resource: {name}")
    packages = read_json(REVERSE / "package-lock.json")["packages"]
    for name, package in packages.items():
        if not name:
            continue
        installed = REVERSE / name
        if package.get("optional") or package.get("dev"):
            if installed.exists():
                raise RuntimeError(f"Unexpected reverse dependency {name}; run npm ci --omit=dev --omit=optional")
            continue
        manifest = installed / "package.json"
        if not manifest.is_file():
            raise RuntimeError(f"Missing packaged reverse dependency: {name}")
        if read_json(manifest).get("version") != package["version"]:
            raise RuntimeError(f"Installed reverse dependency does not match lock: {name}")
    modules = REVERSE / "node_modules"
    pending = [modules]
    while pending:
        current = pending.pop()
        for path in current.iterdir():
            if path.name.startswith("."):
                continue
            if path.name.startswith("@"):
                pending.append(path)
                continue
            if path.relative_to(REVERSE).as_posix() not in packages:
                raise RuntimeError(f"Unexpected reverse dependency {path.name}; run npm ci --omit=dev --omit=optional")
            if (path / "node_modules").is_dir():
                pending.append(path / "node_modules")
    upstream_dir = modules / "js-reverse-mcp"
    upstream = read_json(upstream_dir / "package.json")
    declared_bin = upstream.get("bin")
    entry = declared_bin.get("js-reverse-mcp") if isinstance(declared_bin, dict) else declared_bin
    if not isinstance(entry, str) or not entry:
        raise RuntimeError("Reverse package must declare its CLI bin")
    entry_path = (upstream_dir / entry).resolve()
    if not entry_path.is_relative_to(upstream_dir.resolve()) or not entry_path.is_file():
        raise RuntimeError("Missing or invalid packaged reverse CLI bin")
    if destination.exists():
        shutil.rmtree(destination)
    destination.mkdir(parents=True)
    for name in ("bridge.mjs", "windows.mjs", "package.json", "package-lock.json"):
        shutil.copy2(REVERSE / name, destination / name)
    replace_tree(modules, destination / "node_modules")


def stage_resources(resources):
    stage_browser_resources(resources / "playwright")
    stage_reverse_resources(resources / "reverse")
    for name in ("manifest.json", "cs.js"):
        if not (COMPANION / name).is_file():
            raise RuntimeError(f"Missing companion resource: {name}")
    replace_tree(COMPANION, resources / "companion")
    shutil.copy2(ROOT / "LICENSE", resources / "LICENSE")
    legacy = resources / "chromix"
    if legacy.exists():
        shutil.rmtree(legacy)


def stage_runtime():
    if sys.platform == "darwin":
        app = BIN / "Cloaksession.app"
        executable = app / "Contents/MacOS/Cloaksession"
        resources = app / "Contents/Resources"
    else:
        executable = BIN / ("Cloaksession.exe" if sys.platform == "win32" else "Cloaksession")
        resources = BIN / "resources"
    if not executable.is_file():
        raise RuntimeError(f"Wails output missing: {executable}")
    for legacy in ("desktop-core", "desktop-core.exe"):
        (executable.parent / legacy).unlink(missing_ok=True)
    stage_resources(resources)
    if sys.platform == "darwin":
        # Adding resources invalidates Wails' initial ad-hoc signature.
        run(["codesign", "--force", "--deep", "--sign", "-", app])
        run(["codesign", "--verify", "--deep", "--strict", app])
    return executable


def find_makensis():
    found = shutil.which("makensis")
    if found:
        return found
    for variable in ("ProgramFiles(x86)", "ProgramFiles"):
        candidate = Path(os.environ.get(variable, "C:/Program Files (x86)")) / "NSIS/makensis.exe"
        if candidate.is_file():
            return str(candidate)
    raise RuntimeError("NSIS not found; install NSIS 3 and add makensis to PATH")


def package(version, universal):
    DIST.mkdir(parents=True, exist_ok=True)
    arch = {"x86_64": "amd64", "AMD64": "amd64", "aarch64": "arm64"}.get(platform.machine(), platform.machine())
    if sys.platform == "win32":
        if arch != "amd64":
            raise RuntimeError("The Windows NSIS installer currently supports x64 hosts only")
        asset = DIST / f"Cloaksession-{version}-windows-amd64-setup.exe"
        run([find_makensis(), "/WX", f"/DVERSION={version}", f"/DPAYLOAD={BIN}", f"/DOUTPUT={asset}",
             DESKTOP / "build/windows/installer.nsi"])
    elif sys.platform == "darwin":
        asset = DIST / f"Cloaksession-{version}-macos-{'universal' if universal else arch}.zip"
        run(["ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", BIN / "Cloaksession.app", asset])
    else:
        asset = DIST / f"Cloaksession-{version}-linux-{arch}.tar.gz"
        with tarfile.open(asset, "w:gz") as archive:
            for name in ("Cloaksession", "resources"):
                archive.add(BIN / name, arcname=f"Cloaksession/{name}")
    if not asset.is_file():
        raise RuntimeError(f"Package was not created: {asset}")
    print(f"Release asset: {asset}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--package", action="store_true", help="also create a platform release asset")
    parser.add_argument("--universal", action="store_true", help="build both macOS architectures and combine them")
    parser.add_argument("--webkit2-41", action="store_true", help="Linux only: use WebKitGTK 4.1 instead of 4.0")
    parser.add_argument("--prepare", action="store_true", help="install npm dependencies, build frontend assets, and stage resources for wails dev")
    parser.add_argument("--test", action="store_true", help="run Go tests with the race detector before building")
    parser.add_argument("--check-version", action="store_true", help="only verify synchronized application/tool versions")
    parser.add_argument("--tag", help="require this exact release tag, e.g. v1.4.0")
    args = parser.parse_args()
    if args.universal and sys.platform != "darwin":
        parser.error("--universal requires macOS")
    if args.webkit2_41 and sys.platform != "linux":
        parser.error("--webkit2-41 requires Linux")
    if args.prepare and args.package:
        parser.error("--prepare and --package are mutually exclusive")
    version = verify_versions(args.tag)
    verify_runtime_manifest()
    verify_reverse_manifest()
    if args.check_version:
        return
    install_dependencies()
    BIN.mkdir(parents=True, exist_ok=True)
    shutil.copy2(DESKTOP / "icons/icon.png", DESKTOP / "build/appicon.png")
    (DESKTOP / "build/windows").mkdir(parents=True, exist_ok=True)
    shutil.copy2(DESKTOP / "icons/icon.ico", DESKTOP / "build/windows/icon.ico")
    # The Go embed requires dist even before the development watcher starts.
    run(["npm", "run", "build"], cwd=FRONTEND)
    if args.test:
        test_command = ["go", "test", "-race", "-count=1", "-mod=readonly"]
        if args.webkit2_41:
            test_command += ["-tags", "webkit2_41"]
        run([*test_command, "./..."], cwd=DESKTOP)
    if args.prepare:
        resources = BIN.parent / "Resources" if sys.platform == "darwin" else BIN / "resources"
        stage_resources(resources)
        print(f"Development runtime staged in {BIN}")
        return
    # The frontend uses the typed window.go bridge, not generated wailsjs files.
    command = ["go", "run", f"{WAILS_MODULE}/cmd/wails@{WAILS_VERSION}", "build", "-s", "-skipbindings", "-clean", "-m"]
    if args.universal:
        command += ["-platform", "darwin/universal"]
    if args.webkit2_41:
        command += ["-tags", "webkit2_41"]
    if sys.platform == "win32":
        command += ["-webview2", "embed"]
    run(command, cwd=DESKTOP)
    executable = stage_runtime()
    if args.universal:
        run(["lipo", executable, "-verify_arch", "arm64", "x86_64"])
    if args.package:
        package(version, args.universal)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.CalledProcessError, OSError, ValueError) as error:
        print(f"Build failed: {error}", file=sys.stderr)
        sys.exit(1)
