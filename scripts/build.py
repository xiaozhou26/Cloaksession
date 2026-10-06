#!/usr/bin/env python3
"""Build the Wails desktop, stage its Rust runtime, and optionally package it."""

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
import tomllib

ROOT = Path(__file__).resolve().parents[1]
DESKTOP = ROOT / "desktop"
FRONTEND = DESKTOP / "frontend"
CHROMIX = ROOT / "crates/desktop-core/resources/chromix"
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
    cargo = tomllib.loads((ROOT / "crates/desktop-core/Cargo.toml").read_text())
    version = cargo["package"]["version"]
    if cargo["package"]["name"] != "desktop-core":
        raise RuntimeError("Rust desktop package must be named desktop-core")
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        raise RuntimeError("Desktop version must be a numeric major.minor.patch version")
    lock = read_json(FRONTEND / "package-lock.json")
    versions = {
        "frontend/package.json": read_json(FRONTEND / "package.json")["version"],
        "frontend/package-lock.json": lock["version"],
        "frontend/package-lock.json packages root": lock["packages"][""]["version"],
        "wails.json info.productVersion": read_json(DESKTOP / "wails.json")["info"]["productVersion"],
    }
    cargo_lock = tomllib.loads((ROOT / "Cargo.lock").read_text())
    core_versions = [p["version"] for p in cargo_lock["package"] if p["name"] == "desktop-core"]
    if core_versions != [version]:
        raise RuntimeError(f"Cargo.lock desktop-core version mismatch: {core_versions}")
    for source, actual in versions.items():
        if actual != version:
            raise RuntimeError(f"{source}: {actual!r} does not match desktop-core {version}")
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
    manifest = read_json(CHROMIX / "package.json")
    lock = read_json(CHROMIX / "package-lock.json")
    dependencies = manifest.get("dependencies", {})
    if set(dependencies) != {"playwright-core"} or manifest.get("devDependencies"):
        raise RuntimeError("Browser bridge must depend only on playwright-core")
    packages = lock.get("packages", {})
    if set(packages) != {"", "node_modules/playwright-core"}:
        raise RuntimeError("Browser bridge lock must contain only playwright-core")
    if packages[""].get("dependencies") != dependencies:
        raise RuntimeError("Browser bridge manifest and lock dependencies do not match")


def install_dependencies():
    verify_runtime_manifest()
    run(["npm", "ci", "--legacy-peer-deps"], cwd=FRONTEND)
    run(["npm", "ci", "--omit=dev"], cwd=CHROMIX)


def rust_sidecar(universal):
    metadata = json.loads(run(["cargo", "metadata", "--no-deps", "--format-version", "1", "--locked"], capture=True))
    target = Path(metadata["target_directory"])
    if universal:
        targets = ["aarch64-apple-darwin", "x86_64-apple-darwin"]
        for triple in targets:
            run(["rustup", "target", "add", triple])
            run(["cargo", "build", "--release", "--locked", "-p", "desktop-core", "--target", triple])
        output = target / "universal-apple-darwin/release/desktop-core"
        output.parent.mkdir(parents=True, exist_ok=True)
        run(["lipo", "-create", *[target / triple / "release/desktop-core" for triple in targets], "-output", output])
        run(["lipo", output, "-verify_arch", "arm64", "x86_64"])
        return output
    # An explicit host target avoids accidentally packaging a configured cross target.
    host = next(line.split(": ", 1)[1] for line in run(["rustc", "-vV"], capture=True).splitlines() if line.startswith("host: "))
    run(["cargo", "build", "--release", "--locked", "-p", "desktop-core", "--target", host])
    return target / host / "release" / ("desktop-core.exe" if sys.platform == "win32" else "desktop-core")


def replace_tree(source, destination):
    if destination.exists():
        shutil.rmtree(destination)
    shutil.copytree(source, destination, symlinks=False,
                    ignore=shutil.ignore_patterns(".git", ".DS_Store", "npm-debug.log"))


def stage_browser_resources(destination):
    verify_runtime_manifest()
    required = ["bridge.mjs", "package.json", "package-lock.json", "node_modules/playwright-core/package.json"]
    for name in required:
        if not (CHROMIX / name).is_file():
            raise RuntimeError(f"Missing packaged browser resource: {name}")
    installed = {path.name for path in (CHROMIX / "node_modules").iterdir()}
    if installed - {"playwright-core", ".bin", ".package-lock.json"}:
        raise RuntimeError("Unexpected browser dependencies; run npm ci --omit=dev before packaging")
    if destination.exists():
        shutil.rmtree(destination)
    destination.mkdir(parents=True)
    for name in ("bridge.mjs", "package.json", "package-lock.json"):
        shutil.copy2(CHROMIX / name, destination / name)
    replace_tree(CHROMIX / "node_modules", destination / "node_modules")


def stage_runtime(sidecar):
    if sys.platform == "darwin":
        app = BIN / "Cloaksession.app"
        executable = app / "Contents/MacOS/Cloaksession"
        resources = app / "Contents/Resources"
    else:
        executable = BIN / ("Cloaksession.exe" if sys.platform == "win32" else "Cloaksession")
        resources = BIN / "resources"
    if not executable.is_file():
        raise RuntimeError(f"Wails output missing: {executable}")
    shutil.copy2(sidecar, executable.parent / sidecar.name)
    stage_browser_resources(resources / "chromix")
    shutil.copy2(ROOT / "LICENSE", resources / "LICENSE")
    if sys.platform == "darwin":
        # Adding the sidecar/resources invalidates Wails' initial ad-hoc signature.
        run(["codesign", "--force", "--sign", "-", executable.parent / sidecar.name])
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
            for name in ("Cloaksession", "desktop-core", "resources"):
                archive.add(BIN / name, arcname=f"Cloaksession/{name}")
    if not asset.is_file():
        raise RuntimeError(f"Package was not created: {asset}")
    print(f"Release asset: {asset}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--package", action="store_true", help="also create a platform release asset")
    parser.add_argument("--universal", action="store_true", help="build both macOS architectures and combine them")
    parser.add_argument("--webkit2-41", action="store_true", help="Linux only: use WebKitGTK 4.1 instead of 4.0")
    parser.add_argument("--prepare", action="store_true", help="install npm dependencies and stage the release sidecar for wails dev")
    parser.add_argument("--check-version", action="store_true", help="only verify synchronized application/tool versions")
    parser.add_argument("--tag", help="require this exact release tag, e.g. v1.3.0")
    args = parser.parse_args()
    if args.universal and sys.platform != "darwin":
        parser.error("--universal requires macOS")
    if args.webkit2_41 and sys.platform != "linux":
        parser.error("--webkit2-41 requires Linux")
    if args.prepare and args.package:
        parser.error("--prepare and --package are mutually exclusive")
    version = verify_versions(args.tag)
    if args.check_version:
        return
    install_dependencies()
    sidecar = rust_sidecar(args.universal)
    BIN.mkdir(parents=True, exist_ok=True)
    shutil.copy2(DESKTOP / "icons/icon.png", DESKTOP / "build/appicon.png")
    (DESKTOP / "build/windows").mkdir(parents=True, exist_ok=True)
    shutil.copy2(DESKTOP / "icons/icon.ico", DESKTOP / "build/windows/icon.ico")
    # The Go embed requires dist even before the development watcher starts.
    run(["npm", "run", "build"], cwd=FRONTEND)
    if args.prepare:
        shutil.copy2(sidecar, BIN / sidecar.name)
        resources = BIN.parent / "Resources" if sys.platform == "darwin" else BIN / "resources"
        stage_browser_resources(resources / "chromix")
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
    executable = stage_runtime(sidecar)
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
