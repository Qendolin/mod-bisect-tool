#!/usr/bin/env python3
"""Build and package mod-bisect-gui for a target platform.

Uses the gogio tool for Windows (icon embedding, GUI subsystem) and macOS (.app
bundle + zip), and a plain `go build` for Linux (CGO), which is packaged as an
AppImage. Windows Authenticode signing and macOS Developer ID signing are
enabled by environment variables when credentials are available.

Usage:
    python3 scripts/build_gui.py --goos linux --goarch amd64 --tag v1.2.0
"""

from __future__ import annotations

import argparse
import base64
import contextlib
import os
import shutil
import stat
import subprocess
import tempfile
import uuid
from collections.abc import Generator
from pathlib import Path

# ── Helpers ───────────────────────────────────────────────────────────────────


def run(
    *cmd: str | Path,
    extra_env: dict[str, str] | None = None,
    cwd: Path | None = None,
) -> None:
    """Print and execute a command, merging extra_env into the current environment."""
    env = {**os.environ, **(extra_env or {})}
    print("+", " ".join(str(c) for c in cmd), flush=True)
    subprocess.run([str(c) for c in cmd], check=True, env=env, cwd=cwd)


def build_ldflags(distribution: str, git_tag: str, git_revision: str) -> str:
    return (
        f"-X github.com/Qendolin/mod-bisect-tool/pkg/app.AppDistribution={distribution} "
        f"-X github.com/Qendolin/mod-bisect-tool/pkg/app.AppVersion={git_tag} "
        f"-X github.com/Qendolin/mod-bisect-tool/pkg/app.AppRevision={git_revision}"
    )


def gogio_build(
    target: str,
    arch: str,
    icon: Path,
    out: str,
    project_dir: Path,
    app_id: str,
    ldflags: str | None = None,
    extra_env: dict[str, str] | None = None,
) -> None:
    """Invoke gogio for Windows/macOS packaging. Must run from project_dir."""
    args = [
        "gogio",
        "-target",
        target,
        "-arch",
        arch,
        "-icon",
        str(icon),
        "-appid",
        app_id,
        "-o",
        out,
    ]
    if ldflags:
        args.extend(["-ldflags", ldflags])
    args.append(".")
    run(*args, extra_env=extra_env, cwd=project_dir)


@contextlib.contextmanager
def windows_signing_certificate() -> Generator[str, None, None]:
    """Decode the optional PFX secret into a temporary file."""
    encoded = os.environ.get("WINDOWS_SIGNING_CERTIFICATE_BASE64")
    if not encoded:
        yield ""
        return

    try:
        certificate = base64.b64decode(encoded, validate=True)
    except ValueError as exc:
        raise RuntimeError(
            "WINDOWS_SIGNING_CERTIFICATE_BASE64 is not valid base64"
        ) from exc

    with tempfile.TemporaryDirectory() as temp_dir:
        path = Path(temp_dir) / "mod-bisect-signing.pfx"
        path.write_bytes(certificate)
        yield str(path)


@contextlib.contextmanager
def macos_signing_keychain() -> Generator[None, None, None]:
    """Import the optional PKCS#12 secret into an ephemeral macOS keychain."""
    encoded = os.environ.get("MACOS_SIGNING_CERTIFICATE_BASE64")
    if not encoded:
        yield
        return

    password = os.environ.get("MACOS_SIGNING_CERTIFICATE_PASSWORD")
    identity = os.environ.get("MACOS_SIGNING_IDENTITY")
    if not password or not identity:
        raise RuntimeError(
            "MACOS_SIGNING_CERTIFICATE_PASSWORD and MACOS_SIGNING_IDENTITY "
            "are required when MACOS_SIGNING_CERTIFICATE_BASE64 is set"
        )
    try:
        certificate = base64.b64decode(encoded, validate=True)
    except ValueError as exc:
        raise RuntimeError(
            "MACOS_SIGNING_CERTIFICATE_BASE64 is not valid base64"
        ) from exc

    keychain = Path(tempfile.gettempdir()) / f"mod-bisect-{uuid.uuid4()}.keychain-db"
    keychain_password = uuid.uuid4().hex
    certificate_path = keychain.with_suffix(".p12")
    certificate_path.write_bytes(certificate)
    try:
        run("security", "create-keychain", "-p", keychain_password, str(keychain))
        run("security", "set-keychain-settings", "-lut", "21600", str(keychain))
        run("security", "unlock-keychain", "-p", keychain_password, str(keychain))
        run(
            "security",
            "import",
            str(certificate_path),
            "-P",
            password,
            "-A",
            "-t",
            "cert",
            "-f",
            "pkcs12",
            str(keychain),
        )
        run("security", "list-keychains", "-d", "user", "-s", str(keychain))
        run("security", "default-keychain", "-s", str(keychain))
        yield
    finally:
        subprocess.run(
            ["security", "delete-keychain", str(keychain)],
            check=False,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        certificate_path.unlink(missing_ok=True)


# ── Linux ─────────────────────────────────────────────────────────────────────


def build_linux(
    goarch: str, git_tag: str, project_dir: Path, dist: Path, icon: Path, app_id: str
) -> None:
    # Reconstruct a conformant AppDir from the built binary.
    appdir = project_dir / "AppDir"
    if appdir.exists():
        shutil.rmtree(appdir)
    bin_dir = appdir / "usr" / "bin"
    bin_dir.mkdir(parents=True)

    git_revision = (
        subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=project_dir)
        .decode()
        .strip()
    )
    ldflags = build_ldflags("linux-appimage", git_tag, git_revision)
    run(
        "go",
        "build",
        "-ldflags",
        ldflags,
        "-o",
        str(bin_dir / "mod-bisect-gui"),
        ".",
        cwd=project_dir,
    )

    # Write AppRun — resolves the binary path relative to the AppImage at runtime.
    apprun = appdir / "AppRun"
    apprun.write_text(
        "#!/bin/sh\n"
        'HERE="$(cd "$(dirname "$0")"; pwd)"\n'
        'exec "$HERE/usr/bin/mod-bisect-gui" "$@"\n'
    )
    apprun.chmod(apprun.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    # AppImage spec: .desktop and icon must live at the AppDir root. appimagetool
    # hard-errors if Categories= is absent, so it is written in up front.
    desktop = appdir / "mod-bisect-gui.desktop"
    desktop.write_text(
        "[Desktop Entry]\n"
        "Type=Application\n"
        "Name=Mod Bisect Tool\n"
        "Comment=Minecraft mod bisection tool\n"
        "Exec=mod-bisect-gui\n"
        "Icon=mod-bisect-gui\n"
        "Terminal=false\n"
        "Categories=Utility;\n"
    )
    shutil.copy2(icon, appdir / "mod-bisect-gui.png")

    tool_arch = "aarch64" if goarch == "arm64" else "x86_64"
    output = dist / f"mod-bisect-gui-{git_tag}-linux-{goarch}.AppImage"
    tool_search_dir = project_dir.parent.parent
    try:
        appimagetool_path = next(
            tool_search_dir.glob("appimagetool-*.AppImage")
        ).resolve()
    except StopIteration:
        raise FileNotFoundError(
            f"Could not find appimagetool-*.AppImage in {tool_search_dir}"
        )

    run(
        str(appimagetool_path),
        str(appdir),
        extra_env={"VERSION": git_tag, "ARCH": tool_arch},
        cwd=project_dir,
    )

    # Find the generated AppImage and move it to the expected output path.
    generated = list(project_dir.glob("*.AppImage"))
    if not generated:
        raise FileNotFoundError("appimagetool did not produce an .AppImage")
    shutil.move(generated[0], output)


# ── Windows ───────────────────────────────────────────────────────────────────


def build_windows(
    goarch: str, git_tag: str, project_dir: Path, dist: Path, icon: Path, app_id: str
) -> None:
    # Windows is built pure-Go (CGO_ENABLED=0): no cross-compiler needed. gogio
    # embeds the icon and links with -H windowsgui.
    exe = project_dir / "mod-bisect-gui.exe"
    git_revision = (
        subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=project_dir)
        .decode()
        .strip()
    )
    gogio_build(
        "windows",
        goarch,
        icon,
        str(exe),
        project_dir,
        app_id,
        ldflags=f"-H windowsgui {build_ldflags('windows-binary', git_tag, git_revision)}",
    )
    certificate_password = os.environ.get("WINDOWS_SIGNING_CERTIFICATE_PASSWORD")
    with windows_signing_certificate() as certificate:
        if certificate and not certificate_password:
            raise RuntimeError(
                "WINDOWS_SIGNING_CERTIFICATE_PASSWORD is required when "
                "WINDOWS_SIGNING_CERTIFICATE_BASE64 is set"
            )
        if certificate:
            run(
                os.environ.get("WINDOWS_SIGNTOOL", "signtool"),
                "sign",
                "/fd",
                "SHA256",
                "/f",
                certificate,
                "/p",
                certificate_password,
                "/tr",
                os.environ.get(
                    "WINDOWS_TIMESTAMP_URL", "http://timestamp.digicert.com"
                ),
                "/td",
                "SHA256",
                str(exe),
            )
        else:
            print(
                "WINDOWS_SIGNING_CERTIFICATE_BASE64 is not set; "
                "leaving executable unsigned"
            )

    shutil.move(exe, dist / f"mod-bisect-gui-{git_tag}-windows-{goarch}.exe")


# ── macOS ─────────────────────────────────────────────────────────────────────

# Oldest macOS the built app claims to support. Used both as the compiler's
# MACOSX_DEPLOYMENT_TARGET and as Info.plist's LSMinimumSystemVersion, so the
# two stay in sync. 11.0 (Big Sur) is the oldest version that supports Apple
# Silicon and Gio's Metal backend.
MACOS_MIN_VERSION = "11.0"


def build_darwin(
    goarch: str, git_tag: str, project_dir: Path, dist: Path, icon: Path, app_id: str
) -> None:
    # gogio produces <name>.app in project_dir; its intermediate zip lives in a
    # temp dir that is deleted. Package the .app ourselves with ditto (preserves
    # resource forks / extended attributes that plain zip drops).
    #
    # MACOSX_DEPLOYMENT_TARGET is set explicitly below because, without it,
    # clang (invoked by gogio for the cgo parts) defaults the deployment
    # target to whatever SDK ships on the runner (e.g. "26.5"), which stamps
    # LC_BUILD_VERSION's minos as that same version. Any user not on that
    # exact-or-newer macOS then gets a misleading "You can't use this version
    # of the application ... with this version of macOS" error even though
    # the binary itself is otherwise fine.
    app = project_dir / "Mod-Bisect-Tool.app"
    git_revision = (
        subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=project_dir)
        .decode()
        .strip()
    )
    gogio_build(
        "macos",
        goarch,
        icon,
        str(app),
        project_dir,
        app_id,
        ldflags=build_ldflags("darwin-app", git_tag, git_revision),
        extra_env={"MACOSX_DEPLOYMENT_TARGET": MACOS_MIN_VERSION},
    )
    if not app.exists():
        raise FileNotFoundError(f"gogio did not produce the expected .app {app}")

    # gogio's Info.plist template only writes CFBundleExecutable,
    # CFBundleIconFile, CFBundleIdentifier, NSHighResolutionCapable, and a
    # wrong CFBundlePackageType ("BNDL" instead of "APPL" — BNDL means
    # loadable bundle/plugin, not launchable app, and is what causes macOS to
    # refuse to open it). Fill in the rest of the conventional fields too.
    # `plutil -replace` adds a key if it's absent and overwrites it if
    # present, so this is safe regardless of what gogio already wrote.
    version = git_tag.lstrip("v") or "0.0.0"
    plist_fields = {
        "CFBundlePackageType": "APPL",
        "CFBundleName": "Mod Bisect Tool",
        "CFBundleDisplayName": "Mod Bisect Tool",
        "CFBundleVersion": version,
        "CFBundleShortVersionString": version,
        "CFBundleInfoDictionaryVersion": "6.0",
        "CFBundleSignature": "????",
        "CFBundleDevelopmentRegion": "en",
        "NSHumanReadableCopyright": "Copyright (c) Qendolin 2026",
        # Matches MACOSX_DEPLOYMENT_TARGET above, so Finder/Gatekeeper agree
        # with what the binary itself was actually built to support.
        "LSMinimumSystemVersion": MACOS_MIN_VERSION,
        "LSApplicationCategoryType": "public.app-category.utilities",
    }
    plist_path = app / "Contents" / "Info.plist"
    for key, value in plist_fields.items():
        run("plutil", "-replace", key, "-string", value, str(plist_path))

    signing_identity = os.environ.get("MACOS_SIGNING_IDENTITY")
    with macos_signing_keychain():
        if signing_identity:
            run(
                "codesign",
                "--force",
                "--deep",
                "--options",
                "runtime",
                "--timestamp",
                "--sign",
                signing_identity,
                str(app),
            )
        else:
            # Ad-hoc signing keeps local and unsigned CI builds launchable for testing.
            print("MACOS_SIGNING_IDENTITY is not set; using an ad-hoc signature")
            run("codesign", "--force", "--deep", "--sign", "-", str(app))

    output = dist / f"mod-bisect-gui-{git_tag}-darwin-{goarch}.zip"
    run("ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", str(app), str(output))

    apple_id = os.environ.get("APPLE_ID")
    team_id = os.environ.get("APPLE_TEAM_ID")
    app_password = os.environ.get("APPLE_APP_SPECIFIC_PASSWORD")
    if any((apple_id, team_id, app_password)):
        if not all((apple_id, team_id, app_password)):
            raise RuntimeError(
                "APPLE_ID, APPLE_TEAM_ID, and APPLE_APP_SPECIFIC_PASSWORD "
                "must all be set for notarization"
            )
        run(
            "xcrun",
            "notarytool",
            "submit",
            str(output),
            "--apple-id",
            apple_id,
            "--team-id",
            team_id,
            "--password",
            app_password,
            "--wait",
        )
        run("xcrun", "stapler", "staple", str(app))
        run("ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", str(app), str(output))
    elif signing_identity:
        print("Notarization credentials are not set; leaving the signed app unstapled")


# ── Entry point ───────────────────────────────────────────────────────────────

BUILDERS = {
    "linux": build_linux,
    "windows": build_windows,
    "darwin": build_darwin,
}


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--goos", required=True, choices=BUILDERS.keys())
    p.add_argument("--goarch", required=True, choices=["amd64", "arm64"])
    p.add_argument("--tag", required=True, help="Release tag, e.g. v1.2.0")
    p.add_argument(
        "--project-dir",
        default="cmd/mod-bisect-gui",
        help="Path to the GUI module directory (default: cmd/mod-bisect-gui)",
    )
    p.add_argument(
        "--dist",
        default="dist",
        help="Output directory for built artifacts (default: dist)",
    )
    p.add_argument(
        "--icon",
        default=None,
        help="Path to the icon (default: <project-dir>/Icon-Small.png)",
    )
    p.add_argument(
        "--appid",
        default="dev.qendolin.modbisecttool",
        help="Application/bundle ID for gogio (Windows/macOS)",
    )
    args = p.parse_args()

    project_dir = Path(args.project_dir).resolve()
    dist = Path(args.dist).resolve()
    dist.mkdir(parents=True, exist_ok=True)

    icon = Path(args.icon).resolve() if args.icon else project_dir / "Icon-Small.png"

    BUILDERS[args.goos](args.goarch, args.tag, project_dir, dist, icon, args.appid)
    print(f"\nDone. Output in {dist}", flush=True)


if __name__ == "__main__":
    main()
