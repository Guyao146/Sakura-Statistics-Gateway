#!/usr/bin/env python3
"""Create a PostgreSQL custom-format backup via Compose without reading .env."""
import argparse
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
COMPOSE = ["docker", "compose", "--project-directory", str(ROOT), "-f", str(ROOT / "compose.yaml")]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path, help="new backup file; existing files are never overwritten")
    args = parser.parse_args()
    output = args.output.resolve()
    try:
        # Exclusive creation prevents accidental overwrite, including through symlinks.
        with output.open("xb") as stream:
            try:
                subprocess.run(COMPOSE + ["exec", "-T", "postgres", "sh", "-ec",
                    'exec pg_dump --format=custom --no-owner --no-privileges -U "$POSTGRES_USER" -d "$POSTGRES_DB"'],
                    stdout=stream, check=True)
                stream.flush()
                os.fsync(stream.fileno())
            except BaseException:
                stream.close()
                output.unlink(missing_ok=True)
                raise
        print(f"Backup saved: {output}")
    except (OSError, subprocess.CalledProcessError) as exc:
        print(f"Backup failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
