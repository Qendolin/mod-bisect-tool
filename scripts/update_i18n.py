import argparse
from pathlib import Path
import re
import tomllib


def format_toml_value(key: str, val: dict | str | int | float | bool) -> str:
    """Formats a parsed TOML key-value item back into standard TOML string representation."""
    lines = []
    if isinstance(val, dict):
        lines.append(f"[{key}]")
        for sub_k, sub_v in val.items():
            if isinstance(sub_v, str):
                escaped_v = sub_v.replace("\n", r"\n").replace('"', '\\"')
                lines.append(f'{sub_k} = "{escaped_v}"')
            else:
                lines.append(f"{sub_k} = {sub_v}")
    elif isinstance(val, str):
        escaped_val = val.replace("\n", r"\n").replace('"', '\\"')
        lines.append(f'{key} = "{escaped_val}"')
    else:
        lines.append(f"{key} = {val}")
    return "\n".join(lines)


def serialize_toml_val(val: dict | str | int | float | bool) -> str:
    """Serializes a single primitive Python value into TOML syntax."""
    if isinstance(val, str):
        escaped = val.replace("\\", "\\\\").replace("\n", "\\n").replace('"', '\\"')
        return f'"{escaped}"'
    elif isinstance(val, bool):
        return "true" if val else "false"
    return str(val)


def dump_toml(data: dict) -> str:
    """Dumps a dictionary back to TOML formatted string."""
    lines = []
    for k, v in data.items():
        if not isinstance(v, dict):
            lines.append(f"{k} = {serialize_toml_val(v)}")

    for k, v in data.items():
        if isinstance(v, dict):
            if lines and lines[-1] != "":
                lines.append("")
            lines.append(f"[{k}]")
            for sub_k, sub_v in v.items():
                if not isinstance(sub_v, dict):
                    lines.append(f"{sub_k} = {serialize_toml_val(sub_v)}")
    return "\n".join(lines) + "\n"


def merge_dicts(target: dict, source: dict) -> None:
    """Recursively merges source dict updates into target dict."""
    for k, v in source.items():
        if isinstance(v, dict) and k in target and isinstance(target[k], dict):
            merge_dicts(target[k], v)
        else:
            target[k] = v


def extract_keys_to_file(
    directory: str | Path, keys: list[str], output_file: str | Path
) -> None:
    dir_path = Path(directory)
    out_path = Path(output_file)

    if not dir_path.exists():
        print(f"Error: Directory path '{dir_path}' does not exist.")
        return

    toml_files = sorted(dir_path.rglob("*.toml"))
    if not toml_files:
        print(f"No .toml files found in '{dir_path}'.")
        return

    blocks = []
    for file_path in toml_files:
        try:
            with open(file_path, "rb") as f:
                data = tomllib.load(f)
        except Exception as e:
            print(f"Error reading {file_path.name}: {e}")
            continue

        matches = []
        for key in keys:
            if key in data:
                matches.append(format_toml_value(key, data[key]))

        if matches:
            block = f"--- {file_path.name} ---\n" + "\n\n".join(matches)
            blocks.append(block)

    if not blocks:
        print("No matching keys found.")
        return

    out_path.write_text("\n\n".join(blocks) + "\n", encoding="utf-8")
    print(f"Successfully extracted {len(blocks)} file match(es) to '{out_path}'.")


def merge_file_to_toml(directory: str | Path, input_file: str | Path) -> None:
    dir_path = Path(directory)
    in_path = Path(input_file)

    if not in_path.exists():
        print(f"Error: Input file '{in_path}' does not exist.")
        return

    input_text = in_path.read_text(encoding="utf-8")
    pattern = re.compile(r"^---\s*(.*?)\s*---$", re.MULTILINE)
    matches = list(pattern.finditer(input_text))

    if not matches:
        print("No header matching '--- filename ---' found in input.")
        return

    file_updates = {}
    for i, match in enumerate(matches):
        filename = match.group(1).strip()
        start = match.end()
        end = matches[i + 1].start() if i + 1 < len(matches) else len(input_text)
        content = input_text[start:end].strip()
        if content:
            file_updates[filename] = content

    for filename, raw_toml in file_updates.items():
        target_path = dir_path / filename
        if not target_path.exists():
            matches_found = list(dir_path.rglob(filename))
            if matches_found:
                target_path = matches_found[0]
            else:
                print(
                    f"Warning: File '{filename}' not found in '{dir_path}'. Skipping."
                )
                continue

        try:
            updates_dict = tomllib.loads(raw_toml)
            existing_dict = {}
            if target_path.exists():
                with open(target_path, "rb") as f:
                    existing_dict = tomllib.load(f)

            merge_dicts(existing_dict, updates_dict)

            with open(target_path, "w", encoding="utf-8") as f:
                f.write(dump_toml(existing_dict))

            print(f"Updated: {target_path}")

        except Exception as e:
            print(f"Error updating {filename}: {e}")


def main():
    parser = argparse.ArgumentParser(description="Extract or merge TOML i18n entries.")

    mode_group = parser.add_mutually_exclusive_group(required=True)
    mode_group.add_argument(
        "-e",
        "--extract",
        nargs="+",
        metavar="KEY",
        help="Extract key(s) from TOML files into the updates file.",
    )
    mode_group.add_argument(
        "-m",
        "--merge",
        action="store_true",
        help="Merge entries from the updates file back into TOML files.",
    )

    parser.add_argument(
        "-f",
        "--file",
        default="i18n-updates.txt",
        help="Update file path (default: i18n-updates.txt)",
    )
    parser.add_argument(
        "-p",
        "--path",
        default=r"pkg\gui\i18n",
        help=r"Path to TOML directory (default: pkg\gui\i18n)",
    )

    args = parser.parse_args()

    if args.extract:
        extract_keys_to_file(args.path, args.extract, args.file)
    elif args.merge:
        merge_file_to_toml(args.path, args.file)


if __name__ == "__main__":
    main()
