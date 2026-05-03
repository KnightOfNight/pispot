#!/usr/bin/env python3
"""
Pre-deploy secrets guard.

Verifies that all required config files and secret files exist before
allowing a deploy to proceed. Called by `make check-secrets`, which is
a dependency of ship-dashboard and restart-dashboard.

Exits 0 on success, 1 on any missing item (with a helpful error message).
"""

import os
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent

# .env values we read to find cert name
DASHBOARD_ENV = REPO_ROOT / "dashboard" / ".env"
INVENTORY = REPO_ROOT / "ansible" / "inventory" / "hosts"
GROUP_VARS = REPO_ROOT / "ansible" / "group_vars" / "all.yml"
SSL_DIR = REPO_ROOT / "ssl"


def load_env(path: Path) -> dict:
    """Parse a simple KEY=VALUE .env file, ignoring comments and blanks."""
    env = {}
    if not path.exists():
        return env
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if "=" in line:
            k, _, v = line.partition("=")
            env[k.strip()] = v.strip()
    return env


def check() -> list[str]:
    """Return a list of error strings; empty list means all good."""
    errors = []

    # --- dashboard/.env --------------------------------------------------
    if not DASHBOARD_ENV.exists():
        errors.append(
            f"Missing: dashboard/.env\n"
            f"  Run 'make init' to generate it."
        )
        return errors  # can't continue without env

    env = load_env(DASHBOARD_ENV)

    for key in ("PI_HOST", "PISPOT_TLS_CERT_NAME", "DASHBOARD_TITLE"):
        if not env.get(key):
            errors.append(
                f"Missing or empty: {key} in dashboard/.env\n"
                f"  Run 'make init' to regenerate."
            )

    cert_name = env.get("PISPOT_TLS_CERT_NAME", "")

    # --- ssl/ cert files --------------------------------------------------
    if cert_name:
        for ext in (".crt", ".key", ".ca"):
            p = SSL_DIR / f"{cert_name}{ext}"
            if not p.exists():
                errors.append(
                    f"Missing: ssl/{cert_name}{ext}\n"
                    f"  Place your TLS cert files in ssl/ or run 'make gen-certs'."
                )
    else:
        errors.append(
            "PISPOT_TLS_CERT_NAME is not set — cannot check TLS files."
        )

    # --- ansible/inventory/hosts -----------------------------------------
    if not INVENTORY.exists():
        errors.append(
            f"Missing: ansible/inventory/hosts\n"
            f"  Run 'make init' to generate it."
        )

    # --- ansible/group_vars/all.yml ---------------------------------------
    if not GROUP_VARS.exists():
        errors.append(
            f"Missing: ansible/group_vars/all.yml\n"
            f"  Run 'make init' to generate it."
        )

    return errors


def main() -> int:
    errors = check()
    if not errors:
        print("check-secrets: all required files present")
        return 0

    print("check-secrets: FAILED — missing required files:\n", file=sys.stderr)
    for e in errors:
        for line in e.splitlines():
            print(f"  {line}", file=sys.stderr)
        print(file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
