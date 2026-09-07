"""
Minecraft Classpath Analyzer and Library Aggregator

This script inspects a running Minecraft process to identify top-level Java package
structures or consolidates package sets across multiple game instances and versions.

Key Features:
  1. Capture Mode (Default):
     - Locates a running Minecraft process ('javaw.exe' containing 'minecraft' in command line).
     - Inspects its classpath and extracts unique root packages containing class files.
     - Auto-detects the mod loader (Vanilla, Fabric, Quilt, Forge, or NeoForge).
     - Auto-detects the Minecraft version from the 'com/mojang/minecraft/<version>/' entry.
     - Outputs the detected packages to 'libs-<loader>-<version>.txt'.

  2. Aggregation Mode (-a / --aggregate):
     - Scans the current directory for 'libs-<loader>-<version>.txt' files.
     - Unions package sets across all recorded Minecraft versions.
     - Performs set subtraction against Vanilla to output:
         * libs-vanilla.txt    : Combined Vanilla packages across versions.
         * libs-fabriclike.txt : Combined (Fabric + Quilt) packages minus Vanilla.
         * libs-forgelike.txt  : Combined (Forge + NeoForge) packages minus Vanilla.
"""

import os
import sys
import re
import argparse
import zipfile
import subprocess
from collections import defaultdict
import psutil


def log(msg):
    """Prints status messages to stderr."""
    print(msg, file=sys.stderr)


def load_set(filepath):
    """Loads a text file into a set, trying common encodings."""
    if not os.path.exists(filepath):
        return set()

    encodings_to_try = ["utf-8", "utf-8-sig", "utf-16", "utf-16-le", "cp1252"]
    for enc in encodings_to_try:
        try:
            with open(filepath, "r", encoding=enc) as f:
                return {line.strip() for line in f if line.strip()}
        except (UnicodeDecodeError, UnicodeError):
            continue

    with open(filepath, "r", encoding="utf-8", errors="ignore") as f:
        return {line.strip() for line in f if line.strip()}


def save_set(filepath, data_set):
    """Saves a set to a text file with items sorted alphabetically."""
    with open(filepath, "w", encoding="utf-8") as f:
        for item in sorted(data_set):
            f.write(f"{item}\n")


def find_minecraft_pid():
    """Finds the PID of the Minecraft process by matching 'javaw.exe' with 'minecraft' in command line."""
    for proc in psutil.process_iter(["pid", "name", "cmdline"]):
        try:
            name = proc.info["name"]
            cmdline = proc.info["cmdline"]

            if name and "javaw.exe" in name.lower() and cmdline:
                full_cmd = " ".join(cmdline).lower()
                if "minecraft" in full_cmd:
                    return proc.info["pid"], cmdline
        except (psutil.NoSuchProcess, psutil.AccessDenied, psutil.ZombieProcess):
            pass

    # Fallback search using jps
    try:
        res = subprocess.run(["jps", "-lv"], capture_output=True, text=True)
        if res.returncode == 0:
            for line in res.stdout.splitlines():
                if "minecraft" in line.lower():
                    parts = line.strip().split(maxsplit=1)
                    if parts:
                        return int(parts[0]), line.split()
    except Exception:
        pass

    return None, None


def get_classpath(pid, cmdline):
    """Extracts classpath entries from command line arguments or via jcmd."""
    classpath_entries = []

    if cmdline:
        for i, arg in enumerate(cmdline):
            if arg in ("-cp", "-classpath", "--class-path") and i + 1 < len(cmdline):
                classpath_entries = cmdline[i + 1].split(os.pathsep)
                break

    if not classpath_entries and pid:
        try:
            res = subprocess.run(
                ["jcmd", str(pid), "VM.system_properties"],
                capture_output=True,
                text=True,
            )
            if res.returncode == 0:
                for line in res.stdout.splitlines():
                    if line.startswith("java.class.path="):
                        raw_cp = line.split("=", 1)[1]
                        classpath_entries = raw_cp.split(os.pathsep)
                        break
        except Exception:
            pass

    return classpath_entries


def detect_loader(classpath_entries):
    """Detects mod loader from the classpath."""
    full_cp = " ".join(classpath_entries).lower()

    if "neoforge" in full_cp:
        return "neoforge"
    if "forge" in full_cp:
        return "forge"
    if "quilt" in full_cp:
        return "quilt"
    if "fabric" in full_cp:
        return "fabric"

    return "vanilla"


def detect_version(classpath_entries):
    """Detects Minecraft version from com/mojang/minecraft/<version>/ entries."""
    pattern = re.compile(r"com[/\\]mojang[/\\]minecraft[/\\]([^/\\]+)", re.IGNORECASE)
    for entry in classpath_entries:
        match = pattern.search(entry)
        if match:
            return match.group(1)
    return "unknown"


def analyze_classpath(classpath_entries):
    """Extracts unique top-level root packages containing class files."""
    classes_per_package = defaultdict(set)

    for entry in classpath_entries:
        if not os.path.exists(entry):
            continue

        entries_to_check = []

        if os.path.isfile(entry) and (entry.endswith(".jar") or entry.endswith(".zip")):
            try:
                with zipfile.ZipFile(entry, "r") as z:
                    entries_to_check = z.namelist()
            except zipfile.BadZipFile:
                continue
        elif os.path.isdir(entry):
            for root, _, files in os.walk(entry):
                for f in files:
                    rel_dir = os.path.relpath(root, entry)
                    rel_path = f if rel_dir == "." else os.path.join(rel_dir, f)
                    entries_to_check.append(rel_path.replace(os.sep, "/"))

        for item in entries_to_check:
            norm_item = item.replace("\\", "/")

            if norm_item.startswith("META-INF/") or norm_item == "META-INF":
                continue

            if norm_item.endswith(".class"):
                filename = os.path.basename(norm_item)
                if filename == "package-info.class":
                    continue

                dir_path = os.path.dirname(norm_item)
                pkg_name = dir_path.replace("/", ".") if dir_path else ""

                if pkg_name and not pkg_name.startswith("META-INF"):
                    classes_per_package[pkg_name].add(filename)

    packages_with_classes = set(classes_per_package.keys())
    root_packages = []

    for pkg in sorted(packages_with_classes):
        parts = pkg.split(".")
        has_ancestor_with_classes = False

        for i in range(1, len(parts)):
            ancestor = ".".join(parts[:i])
            if ancestor in packages_with_classes:
                has_ancestor_with_classes = True
                break

        if not has_ancestor_with_classes:
            root_packages.append(pkg)

    return root_packages


def capture_instance():
    """Captures classpath from running Minecraft instance and saves to libs-<loader>-<version>.txt."""
    log("Searching for running Minecraft process (javaw.exe)...")
    pid, cmdline = find_minecraft_pid()

    if not pid:
        log(
            "Error: Could not find a running 'javaw.exe' process containing 'minecraft' in its arguments."
        )
        sys.exit(1)

    log(f"Found process PID: {pid}")

    classpath = get_classpath(pid, cmdline)
    if not classpath:
        log("Error: Could not retrieve classpath from process.")
        sys.exit(1)

    loader = detect_loader(classpath)
    version = detect_version(classpath)
    out_filename = f"libs-{loader}-{version}.txt"

    log(f"Detected Loader: {loader}")
    log(f"Detected Version: {version}")
    log(f"Analyzing {len(classpath)} classpath entries...")

    root_packages = analyze_classpath(classpath)
    save_set(out_filename, root_packages)

    log(f"Saved {len(root_packages)} root packages to '{out_filename}'.")


def aggregate_libraries():
    """Aggregates all libs-<loader>-<version>.txt files into combined categories."""
    pattern = re.compile(
        r"^libs-(vanilla|fabric|quilt|forge|neoforge)-(.+)\.txt$", re.IGNORECASE
    )

    vanilla_all = set()
    fabric_like_all = set()
    forge_like_all = set()

    found_files = 0
    for filename in os.listdir("."):
        match = pattern.match(filename)
        if not match or not os.path.isfile(filename):
            continue

        loader = match.group(1).lower()
        found_files += 1
        pkgs = load_set(filename)

        if loader == "vanilla":
            vanilla_all.update(pkgs)
        elif loader in ("fabric", "quilt"):
            fabric_like_all.update(pkgs)
        elif loader in ("forge", "neoforge"):
            forge_like_all.update(pkgs)

    if found_files == 0:
        log("No matching files found (expected format: libs-<loader>-<version>.txt).")
        return

    fabric_like_clean = fabric_like_all - vanilla_all
    forge_like_clean = forge_like_all - vanilla_all

    save_set("libs-vanilla.txt", vanilla_all)
    save_set("libs-fabriclike.txt", fabric_like_clean)
    save_set("libs-forgelike.txt", forge_like_clean)

    log(f"Processed {found_files} version files:")
    log(f"  - libs-vanilla.txt ({len(vanilla_all)} packages)")
    log(f"  - libs-fabriclike.txt ({len(fabric_like_clean)} packages)")
    log(f"  - libs-forgelike.txt ({len(forge_like_clean)} packages)")


def main():
    parser = argparse.ArgumentParser(
        description="Analyze Minecraft instance classpaths or aggregate captured library lists."
    )
    parser.add_argument(
        "-a",
        "--aggregate",
        action="store_true",
        help="Combine all libs-<loader>-<version>.txt files into libs-vanilla.txt, libs-fabriclike.txt, and libs-forgelike.txt",
    )

    args = parser.parse_args()

    if args.aggregate:
        aggregate_libraries()
    else:
        capture_instance()


if __name__ == "__main__":
    main()
