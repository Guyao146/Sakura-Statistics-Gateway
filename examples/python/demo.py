"""Run with --endpoint and --app-id for an enabled desktop application."""
import argparse
from pathlib import Path

from sakura_sdk import Client


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", default="http://127.0.0.1:8080")
    parser.add_argument("--app-id", required=True)
    parser.add_argument("--state-dir", type=Path, default=Path.home() / ".sakura-example")
    args = parser.parse_args()
    with Client(args.endpoint, args.app_id, args.state_dir, app_version="1.0.0", platform="python") as client:
        print("A local startup event has been queued. Press Enter after actual interaction; q exits.")
        try:
            while input("> ").strip().lower() != "q":
                queued = client.mark_active()
                print(f"active queued={queued}; pending={client.pending}; error={client.last_error}")
        except (EOFError, KeyboardInterrupt):
            pass
    print("Exited promptly. Unsent events stay on disk for the next launch.")


if __name__ == "__main__":
    main()
