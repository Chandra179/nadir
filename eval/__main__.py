"""Run from the repository root: python -m eval --help."""

import argparse
import asyncio
import math
from pathlib import Path
import signal

from eval.api import validate_url
from eval.dataset import load_capture, load_dataset
from eval.judge import JudgeConfig
from eval.runner import Run, collect, score


def positive(value):
    number = float(value)
    if not math.isfinite(number) or number <= 0:
        raise argparse.ArgumentTypeError("must be positive and finite")
    return number


def positive_int(value):
    number = int(value)
    if number <= 0:
        raise argparse.ArgumentTypeError("must be positive")
    return number


def parser():
    root = argparse.ArgumentParser(description="Collect Nadir answers and score immutable captures with Ragas")
    commands = root.add_subparsers(dest="command", required=True)
    for command in ("run", "collect", "score", "validate"):
        sub = commands.add_parser(command)
        if command in {"run", "collect", "validate"}:
            sub.add_argument("--dataset", type=Path, required=True)
        if command in {"run", "collect"}:
            sub.add_argument("--host", required=True)
            sub.add_argument("--top-k", type=positive_int, default=5)
            sub.add_argument("--repetitions", type=positive_int, default=1)
            sub.add_argument("--timeout", type=positive, default=120)
        if command == "score":
            sub.add_argument("--capture", type=Path, required=True)
        if command in {"run", "score"}:
            sub.add_argument("--judge-base-url", required=True)
            sub.add_argument("--judge-model", required=True)
            sub.add_argument("--judge-api-key-env")
            sub.add_argument("--judge-reasoning-effort", help="explicit endpoint-supported effort, e.g. none for Ollama Qwen")
            sub.add_argument("--metric-timeout", type=positive, default=300)
        if command != "validate":
            sub.add_argument("--output-dir", type=Path)
    return root


async def execute(options, run):
    loop = asyncio.get_running_loop()
    task = asyncio.current_task()
    previous = signal.getsignal(signal.SIGTERM)
    loop.add_signal_handler(signal.SIGTERM, task.cancel)
    interrupted = False
    try:
        path = options.capture if options.command == "score" else await collect(
            run, options.dataset, options.host, options.top_k, options.timeout, options.repetitions)
        if options.command in {"run", "score"}:
            await score(run, path, JudgeConfig(options.judge_base_url, options.judge_model,
                                             options.judge_api_key_env, options.metric_timeout, options.judge_reasoning_effort))
    except (asyncio.CancelledError, KeyboardInterrupt):
        interrupted = True
        run.record["errors"].append("evaluation interrupted")
    except Exception as error:
        run.record["errors"].append(f"{type(error).__name__}: {error}")
    finally:
        loop.remove_signal_handler(signal.SIGTERM)
        signal.signal(signal.SIGTERM, previous)
        run.finish(interrupted)
    return 130 if interrupted else 1 if run.record["errors"] else 0


def main(argv=None):
    arguments = parser()
    options = arguments.parse_args(argv)
    try:
        if options.command in {"run", "collect", "validate"}:
            samples = load_dataset(options.dataset)
        else:
            load_capture(options.capture)
        if options.command == "validate":
            print(f"Valid dataset: {len(samples)} samples")
            return 0
        if options.command in {"run", "collect"}:
            validate_url(options.host)
        if options.command in {"run", "score"}:
            JudgeConfig(options.judge_base_url, options.judge_model,
                        options.judge_api_key_env, options.metric_timeout, options.judge_reasoning_effort).validate()
        inputs = {k: str(v) if isinstance(v, Path) else v for k, v in vars(options).items() if k != "output_dir"}
        run = Run(options.command, inputs, options.output_dir)
    except (ValueError, OSError) as error:
        arguments.error(str(error))
    return asyncio.run(execute(options, run))


if __name__ == "__main__":
    raise SystemExit(main())
