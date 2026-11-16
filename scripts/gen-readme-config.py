#!/usr/bin/env python3
"""Upsert a README ## Configuration section from ui/extension.json.

Markers:
  <!-- fleet-config:start -->
  ...
  <!-- fleet-config:end -->

Usage:
  python3 gen-readme-config.py /path/to/fleet-ext-foo
  python3 gen-readme-config.py --all /path/to/extensions
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

START = "<!-- fleet-config:start -->"
END = "<!-- fleet-config:end -->"
EXTRAS_DIR = Path(__file__).resolve().parent / "readme-config-extras"


def esc_cell(text: str) -> str:
    return text.replace("|", "\\|").replace("\n", " ").strip()


def field_type(field: dict) -> str:
    kind = str(field.get("kind") or "text")
    step = field.get("step")
    if kind == "number" and step is not None:
        return f"number (step={step})"
    return kind


def field_notes(field: dict) -> str:
    parts: list[str] = []
    help_text = (field.get("help") or "").strip()
    if help_text:
        parts.append(help_text)
    ph = (field.get("placeholder") or "").strip()
    if ph:
        parts.append(f"Placeholder: `{ph}`")
    if field.get("advanced"):
        parts.append("Advanced (under More options).")
    return " ".join(parts) if parts else "—"


def render_form(form: dict) -> str:
    title = form.get("title") or form.get("id") or "Settings"
    lines = [f"#### {title}", ""]
    desc = (form.get("description") or "").strip()
    if desc:
        lines.append(desc)
        lines.append("")
    fields = [
        f
        for f in (form.get("fields") or [])
        if not (f.get("name") == "enabled" and f.get("kind") == "boolean")
    ]
    if not fields:
        lines.append("_No user-facing fields in this form (Activate controls on/off)._")
        lines.append("")
        return "\n".join(lines)
    lines.append("| Parameter | Type | Required | Notes |")
    lines.append("| --- | --- | --- | --- |")
    for field in fields:
        label = field.get("label") or field.get("name") or "?"
        name = field.get("name") or ""
        param = f"{esc_cell(label)} (`{name}`)" if name else esc_cell(label)
        req = "yes" if field.get("required") else "no"
        lines.append(
            f"| {param} | {esc_cell(field_type(field))} | {req} | {esc_cell(field_notes(field))} |"
        )
    lines.append("")
    return "\n".join(lines)


def render_actions(actions: list) -> str:
    if not actions:
        return ""
    lines = [
        "### Actions",
        "",
        "| Action | Notes |",
        "| --- | --- |",
    ]
    for action in actions:
        label = action.get("label") or action.get("id") or "?"
        notes: list[str] = []
        if action.get("primary"):
            notes.append("Primary")
        if action.get("danger"):
            notes.append("Destructive")
        if action.get("confirm"):
            notes.append("Asks for confirmation")
        target = action.get("target")
        if target:
            notes.append(f"Target: `{target}`")
        lines.append(f"| {esc_cell(label)} | {esc_cell('; '.join(notes) if notes else '—')} |")
    lines.append("")
    return "\n".join(lines)


def load_extra(extension_id: str) -> str:
    path = EXTRAS_DIR / f"{extension_id}.md"
    if not path.is_file():
        return ""
    text = path.read_text(encoding="utf-8").strip()
    return text + "\n\n" if text else ""


def build_section(ui: dict) -> str:
    ext_id = ui.get("extensionId") or "unknown"
    parts = [
        START,
        "## Configuration",
        "",
        "Turn this extension **on/off** with **Extensions → Activate / Deactivate**. "
        "There is no separate Enabled checkbox in settings; Apply only saves options. "
        "After a Fleet host upgrade, use **Reload workers** if RPC returns "
        "`extension worker not active`.",
        "",
    ]
    forms = ui.get("forms") or []
    if forms:
        parts.append("### Parameters")
        parts.append("")
        for form in forms:
            parts.append(render_form(form).rstrip())
            parts.append("")
    else:
        parts.append("_This package has no declarative settings forms._")
        parts.append("")
    actions_block = render_actions(ui.get("actions") or [])
    if actions_block:
        parts.append(actions_block.rstrip())
        parts.append("")
    extra = load_extra(ext_id)
    if extra:
        parts.append("### Additional UI")
        parts.append("")
        parts.append(extra.rstrip())
        parts.append("")
    parts.append(END)
    return "\n".join(parts).rstrip() + "\n"


def ensure_toc_link(readme: str) -> str:
    """Add Configuration to the centered TOC strip when present."""
    # Pattern: <a href="#install">Install</a> · ...
    if re.search(r'href="#configuration"', readme, re.I):
        return readme

    def inject(match: re.Match[str]) -> str:
        inner = match.group(1)
        if "configuration" in inner.lower():
            return match.group(0)
        # Prefer inserting Configuration before Install if Install exists
        if re.search(r'href="#install"', inner, re.I):
            link = '<a href="#configuration">Configuration</a> ·\n  '
            inner2 = re.sub(
                r'(<a href="#install">Install</a>)',
                link + r"\1",
                inner,
                count=1,
                flags=re.I,
            )
            return f"<p align=\"center\">\n{inner2}\n</p>"
        return (
            f'<p align="center">\n'
            f'  <a href="#configuration">Configuration</a> ·\n'
            f"{inner}\n</p>"
        )

    updated, n = re.subn(
        r"<p align=\"center\">\s*\n((?:\s*<a href=.*?</a>\s*·?\s*\n?)+)\s*</p>",
        inject,
        readme,
        count=1,
        flags=re.I,
    )
    return updated if n else readme


def upsert_readme(readme_path: Path, section: str) -> None:
    text = readme_path.read_text(encoding="utf-8")
    text = ensure_toc_link(text)
    if START in text and END in text:
        pattern = re.compile(
            re.escape(START) + r".*?" + re.escape(END),
            re.DOTALL,
        )
        text = pattern.sub(section.rstrip(), text, count=1)
        if not text.endswith("\n"):
            text += "\n"
        readme_path.write_text(text, encoding="utf-8")
        return

    # Insert after centered id line, else before ## Install, else after first ---
    id_line = re.search(
        r'(<p align="center"><code>org\.[^<]+</code>[^<]*</p>\s*\n)',
        text,
    )
    if id_line:
        pos = id_line.end()
        text = text[:pos] + "\n" + section + "\n" + text[pos:]
    elif re.search(r"^## Install\b", text, re.M):
        text = re.sub(
            r"^## Install\b",
            section + "\n## Install",
            text,
            count=1,
            flags=re.M,
        )
    else:
        # After first horizontal rule
        m = re.search(r"^---\s*$", text, re.M)
        if m:
            pos = m.end()
            text = text[:pos] + "\n\n" + section + text[pos:]
        else:
            text = section + "\n" + text

    if not text.endswith("\n"):
        text += "\n"
    readme_path.write_text(text, encoding="utf-8")


def process_ext_dir(ext_dir: Path) -> None:
    ui_path = ext_dir / "ui" / "extension.json"
    readme_path = ext_dir / "README.md"
    if not ui_path.is_file():
        raise FileNotFoundError(f"missing {ui_path}")
    if not readme_path.is_file():
        raise FileNotFoundError(f"missing {readme_path}")
    ui = json.loads(ui_path.read_text(encoding="utf-8"))
    section = build_section(ui)
    upsert_readme(readme_path, section)
    print(f"updated {readme_path}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "path",
        nargs="?",
        help="Extension package directory (contains ui/extension.json + README.md)",
    )
    parser.add_argument(
        "--all",
        metavar="EXTENSIONS_ROOT",
        help="Process every fleet-ext-* under this directory",
    )
    args = parser.parse_args()
    if args.all:
        root = Path(args.all).resolve()
        dirs = sorted(p for p in root.glob("fleet-ext-*") if p.is_dir())
        if not dirs:
            print(f"no fleet-ext-* under {root}", file=sys.stderr)
            return 1
        errors = 0
        for d in dirs:
            try:
                process_ext_dir(d)
            except Exception as e:  # noqa: BLE001 — batch report
                errors += 1
                print(f"error {d.name}: {e}", file=sys.stderr)
        return 1 if errors else 0
    if not args.path:
        parser.error("path or --all is required")
    process_ext_dir(Path(args.path).resolve())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
