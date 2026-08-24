#!/usr/bin/env python3
"""Generate the static Mod Bisect Tool download site."""

import argparse
import html
import json
import re
import shutil
import sys
import urllib.request
from pathlib import Path

REPO = "Qendolin/mod-bisect-tool"
API = f"https://api.github.com/repos/{REPO}/releases?per_page=100"
DIST = Path("dist/www")
STATIC = ("style.css", "favicon.svg", ".nojekyll", "GUI-User-Guide.html", "TUI-User-Guide.html")

OS_INFO = {
    "windows": ("Windows", 0),
    "darwin": ("macOS", 1),
    "linux": ("Linux", 2),
}
ARCH_INFO = {
    "amd64": ("x86_64 / amd64", 0),
    "arm64": ("arm64 / aarch64", 1),
    "386": ("x86 / 32-bit", 2),
}

ASSET_RE = re.compile(
    r"^(?P<tag>.+?)-(?P<os>windows|darwin|linux)-(?P<arch>amd64|arm64|386)"
    r"(?P<ext>\.[a-zA-Z0-9]+)?$"
)


def fetch_json(url):
    request = urllib.request.Request(url, headers={"User-Agent": "mod-bisect-tool-www"})
    with urllib.request.urlopen(request) as response:
        return json.load(response)


def parse_asset(name):
    """Return (kind, os, arch), or None for unsupported assets."""
    if name.endswith(".md5"):
        return None
    prefixes = (
        ("mod-bisect-gui-", "gui"),
        ("mod-bisect-tui-", "tui"),
        ("fabric-mod-bisect-tool-", "tui"),
    )
    for prefix, kind in prefixes:
        if name.startswith(prefix):
            match = ASSET_RE.match(name[len(prefix):])
            if match:
                return kind, match.group("os"), match.group("arch")
            return None
    return None


def release_assets(release):
    assets = []
    for asset in release.get("assets", []):
        parsed = parse_asset(asset["name"])
        if not parsed:
            continue
        kind, os_name, arch = parsed
        os_label = OS_INFO[os_name][0]
        arch_label = ARCH_INFO[arch][0]
        assets.append({
            "kind": kind,
            "os": os_name,
            "osLabel": os_label,
            "arch": arch,
            "archLabel": arch_label,
            "name": asset["name"],
            "url": asset.get("browser_download_url") or download_url(release["tag_name"], asset["name"]),
        })
    assets.sort(key=lambda item: (
        OS_INFO[item["os"]][1],
        ARCH_INFO[item["arch"]][1],
        item["kind"],
    ))
    return assets


def download_url(tag, name):
    return f"https://github.com/{REPO}/releases/download/{tag}/{name}"


def fmt_date(value):
    return value[:10] if value else ""


def channel_label(release):
    return "Pre-release" if release.get("prerelease") else "Stable"


def build_catalogue(releases):
    groups = []
    for release in releases:
        assets = release_assets(release)
        if not assets:
            continue
        rows = []
        for asset in assets:
            rows.append(
                "        <tr>\n"
                f"          <td>{html.escape(asset['osLabel'])}</td>\n"
                f"          <td>{'Graphical' if asset['kind'] == 'gui' else 'Terminal'}</td>\n"
                f"          <td>{html.escape(asset['archLabel'])}</td>\n"
                f"          <td><a href=\"{html.escape(asset['url'], quote=True)}\"><code>{html.escape(asset['name'])}</code></a></td>\n"
                "        </tr>"
            )
        if rows:
            tag = html.escape(release["tag_name"])
            groups.append(
                "      <details class=\"version-group\">\n"
                f"        <summary><span>{tag}</span> <small>{channel_label(release)}</small></summary>\n"
                "        <table class=\"catalogue\">\n"
                "          <thead><tr><th>Operating system</th><th>Interface</th><th>Architecture</th><th>File</th></tr></thead>\n"
                "          <tbody>\n"
                + "\n".join(rows)
                + "\n          </tbody>\n        </table>\n      </details>"
            )
    return "\n".join(groups)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--www", default="www", help="source site directory")
    args = parser.parse_args()
    source = Path(args.www)

    if DIST.exists():
        shutil.rmtree(DIST)
    DIST.mkdir(parents=True)

    releases = [release for release in fetch_json(API) if not release.get("draft")]
    release_data = []
    for release in releases:
        release_data.append({
            "tag": release["tag_name"],
            "prerelease": bool(release.get("prerelease")),
            "date": fmt_date(release.get("published_at")),
            "assets": release_assets(release),
        })

    template = (source / "index.html").read_text(encoding="utf-8")
    # Prevent a release name or filename from prematurely closing the script tag.
    data_json = json.dumps(release_data, ensure_ascii=True, separators=(",", ":"))
    data_json = data_json.replace("<", "\\u003c").replace(">", "\\u003e")
    output = template.replace("{{ALL_RELEASES_TABLE}}", build_catalogue(releases))
    output = output.replace("{{RELEASE_DATA}}", data_json)
    (DIST / "index.html").write_text(output, encoding="utf-8")

    for name in STATIC:
        path = source / name
        if path.exists():
            shutil.copy2(path, DIST / name)
    if (source / "img").exists():
        shutil.copytree(source / "img", DIST / "img")

    print(f"Wrote {DIST} ({len(release_data)} releases)")


if __name__ == "__main__":
    sys.exit(main())
