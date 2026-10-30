#!/usr/bin/env python3
"""Restore a backup into an empty database, with explicit confirmation."""
import argparse
from pathlib import Path
import subprocess
import sys

from backup import COMPOSE


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("backup", type=Path)
    parser.add_argument("--confirm-empty-database", required=True, metavar="DATABASE_NAME",
                        help="must exactly match the running postgres container's POSTGRES_DB")
    args = parser.parse_args()
    try:
        if not args.backup.is_file():
            raise ValueError("Backup file does not exist")
        running = subprocess.run(COMPOSE + ["ps", "--status", "running", "-q", "app"],
                                 capture_output=True, text=True, check=True).stdout.strip()
        if running:
            raise ValueError("Stop app first: docker compose stop app")
        # Compare within container; .env is never parsed or sourced by this script.
        subprocess.run(COMPOSE + ["exec", "-T", "postgres", "sh", "-ec",
            '[ "$1" = "$POSTGRES_DB" ] || { printf "%s\\n" "Database confirmation mismatch" >&2; exit 1; }',
            "restore-check", args.confirm_empty_database], check=True)
        sql = """SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%'
AND c.relkind IN ('r','p','v','m','S','f');"""
        count = subprocess.run(COMPOSE + ["exec", "-T", "postgres", "sh", "-ec",
            'exec psql -X -v ON_ERROR_STOP=1 -At -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "$1"',
            "restore-check", sql], capture_output=True, text=True, check=True).stdout.strip()
        if count != "0":
            raise ValueError("Target database is not empty; restore refuses to delete or replace existing objects")
        with args.backup.open("rb") as stream:
            subprocess.run(COMPOSE + ["exec", "-T", "postgres", "sh", "-ec",
                'exec pg_restore --single-transaction --exit-on-error --no-owner --no-privileges '
                '-U "$POSTGRES_USER" -d "$POSTGRES_DB"'], stdin=stream, check=True)
        print("Restore completed. Verify the database and application version before starting app.")
    except (OSError, ValueError, subprocess.CalledProcessError) as exc:
        print(f"Restore refused or failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
