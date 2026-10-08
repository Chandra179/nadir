"""Run from the repository root: python -m eval --help."""

import argparse
import asyncio
import math
from pathlib import Path
import signal

from eval.api import validate_url
from eval.dataset import load_capture, load_dataset
from eval.generation import GenerationConfig, ModelEndpoint, document_snapshot, generate
from eval.judge import JudgeConfig
from eval.retrieval import run_command as run_retrieval
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


def nonnegative_int(value):
    number = int(value)
    if number < 0:
        raise argparse.ArgumentTypeError("must be nonnegative")
    return number


def parser():
    root = argparse.ArgumentParser(description="Generate draft questions, collect Nadir answers and score captures with Ragas")
    commands = root.add_subparsers(dest="command", required=True)
    for command in ("generate", "run", "collect", "score", "validate"):
        sub = commands.add_parser(command)
        if command == "generate":
            sub.add_argument("--documents", type=Path, required=True)
            sub.add_argument("--generator-base-url", required=True)
            sub.add_argument("--generator-model", required=True)
            sub.add_argument("--generator-api-key-env")
            sub.add_argument("--generator-reasoning-effort")
            sub.add_argument("--generator-max-output-tokens", type=positive_int, default=4096)
            sub.add_argument("--generator-temperature", type=float, help="explicit model sampling temperature (0 to 2)")
            sub.add_argument("--generator-top-p", type=float, help="explicit nucleus sampling probability (greater than 0 to 1)")
            sub.add_argument("--cache-dir", type=Path, help="optional native Ragas response cache; keep separate per model revision/context")
            sub.add_argument("--ollama-gpu-only", action="store_true",
                             help="check Ollama placement before and after each model request; fail on CPU fallback")
            sub.add_argument("--embedding-base-url", required=True)
            sub.add_argument("--embedding-model", required=True)
            sub.add_argument("--embedding-api-key-env")
            sub.add_argument("--size", type=positive_int, default=40)
            sub.add_argument("--workers", type=positive_int, default=1, help="native Ragas workers; increase only after GPU and throughput checks")
            sub.add_argument("--seed", type=nonnegative_int, default=42)
            sub.add_argument("--timeout", type=positive, default=300)
            sub.add_argument("--run-timeout", type=positive, default=7200)
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
    retrieval = commands.add_parser("retrieval", help="score retrieval only (Hit@k, Recall@k, MRR); no generation, no judge")
    retrieval.add_argument("--golden", type=Path, default=Path("eval/golden/retrieval.json"))
    retrieval.add_argument("--samples-dir", type=Path, default=Path("eval/samples"),
                           help="source documents used to check that every label snippet exists exactly once")
    retrieval.add_argument("--validate", action="store_true", help="check the golden set against the sources and exit; needs no API")
    retrieval.add_argument("--host")
    retrieval.add_argument("--top-k", type=positive_int, default=10)
    retrieval.add_argument("--probe-top-k", type=nonnegative_int, default=50,
                           help="re-query misses this deep to tell 'ranked low' from 'never retrieved'; 0 disables")
    retrieval.add_argument("--timeout", type=positive, default=120)
    retrieval.add_argument("--output-dir", type=Path)
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
    if options.command == "retrieval":
        try:
            if options.host:
                validate_url(options.host)
            return run_retrieval(options, Run)
        except (ValueError, OSError) as error:
            arguments.error(str(error))
    try:
        if options.command == "generate":
            documents = document_snapshot(options.documents)
            config = GenerationConfig(
                ModelEndpoint(options.generator_base_url, options.generator_model, options.generator_api_key_env),
                ModelEndpoint(options.embedding_base_url, options.embedding_model, options.embedding_api_key_env),
                options.size, options.seed, options.timeout, options.run_timeout,
                options.generator_reasoning_effort, options.generator_max_output_tokens,
                options.generator_temperature, options.ollama_gpu_only,
                options.generator_top_p, options.cache_dir, options.workers)
            config.validate()
        elif options.command in {"run", "collect", "validate"}:
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
    if options.command == "generate":
        return generate(run, documents, config)
    return asyncio.run(execute(options, run))


if __name__ == "__main__":
    raise SystemExit(main())
