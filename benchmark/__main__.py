"""Set local reporting defaults, then delegate execution to Locust."""

from __future__ import annotations

import argparse
import csv
import datetime as dt
from pathlib import Path
import sys
import uuid


def main() -> None:
    parser = argparse.ArgumentParser(add_help=False, allow_abbrev=False)
    parser.add_argument("--ui", action="store_true")
    parser.add_argument("--output-dir", type=Path)
    options, arguments = parser.parse_known_args()

    def supplied(*flags: str) -> bool:
        return any(argument == flag or argument.startswith(flag + "=")
                   or (len(flag) == 2 and argument.startswith(flag) and len(argument) > 2)
                   for argument in arguments for flag in flags)

    output = None
    if not supplied("--help", "-h", "--version", "-V"):
        output = options.output_dir or Path(".local/benchmark") / (
            dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "-" + uuid.uuid4().hex[:8]
        )
        output.mkdir(parents=True, exist_ok=False)
        arguments.extend(["--report-dir", str(output)])
        if not supplied("--csv"):
            arguments.extend(["--csv", str(output / "locust")])
        if not supplied("--html"):
            arguments.extend(["--html", str(output / "locust.html")])
        print(f"Benchmark reports: {output}", flush=True)
    defaults = [("--stop-timeout", None, "120")]
    if options.ui:
        defaults.append(("--web-host", None, "127.0.0.1"))
    else:
        if not supplied("--headless"):
            arguments.append("--headless")
        defaults.extend([("--users", "-u", "1"), ("--spawn-rate", "-r", "1"),
                         ("--run-time", "-t", "60s")])
    for flag, short, value in defaults:
        if not supplied(*([flag, short] if short else [flag])):
            arguments.extend([flag, value])
    sys.argv = ["locust", "-f", str(Path(__file__).with_name("locustfile.py")), *arguments]
    if supplied("--help", "-h"):
        # Locust prints help before loading the entrypoint. Register its options first.
        from benchmark import locustfile  # noqa: F401
    from locust.main import main as locust_main

    from locust import events
    from locust.html import get_html_report, process_html_filename
    from locust.stats import PERCENTILES_TO_REPORT, StatsCSV

    environments = []
    events.init.add_listener(lambda environment, **kwargs: environments.append(environment))
    failure = None
    try:
        locust_main()
    except BaseException as error:
        failure = error
        raise
    finally:
        # Native periodic CSVs can miss the last samples, especially in short runs.
        # Refresh them using Locust's serializer after its file handles are closed.
        if environments:
            environment = environments[0]
            from benchmark.locustfile import finish
            finish(environment)
            if environment.parsed_options.csv_prefix:
                writer = StatsCSV(environment, PERCENTILES_TO_REPORT)
                prefix = environment.parsed_options.csv_prefix
                for suffix, write in [("stats", writer.requests_csv), ("failures", writer.failures_csv),
                                      ("exceptions", writer.exceptions_csv)]:
                    with open(f"{prefix}_{suffix}.csv", "w", newline="") as handle:
                        write(csv.writer(handle))
            if environment.parsed_options.html_file:
                # Also preserve the native HTML report on SIGTERM shutdown.
                process_html_filename(environment.parsed_options)
                Path(environment.parsed_options.html_file).write_text(
                    get_html_report(environment, show_download_link=False), encoding="utf-8")
            # UI shutdown can finish stopping users after Locust chooses its exit code.
        if output is not None and not (output / "report.json").exists():
            from benchmark.report import startup_failure
            startup_failure(output, arguments, str(failure or "no workload was started"),
                            empty=failure is None or isinstance(failure, SystemExit) and failure.code == 0)
        if environments:
            environment = environments[0]
            if environment.process_exit_code:
                raise SystemExit(environment.process_exit_code)
            if environment.stats.total.num_failures:
                raise SystemExit(1)


if __name__ == "__main__":
    main()
