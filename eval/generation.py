"""Generate inspectable draft inputs with Ragas in a cancellable worker."""

from collections import Counter
from dataclasses import asdict, dataclass
import hashlib
import json
import math
import multiprocessing
import os
from pathlib import Path
import queue
import re
import signal
import time

from eval.api import validate_url
from eval.dataset import load_dataset
from eval.report import artifact, atomic_json


@dataclass(frozen=True)
class ModelEndpoint:
    base_url: str
    model: str
    api_key_env: str | None = None

    def validate(self):
        validate_url(self.base_url)
        if not self.model.strip():
            raise ValueError("explicit model is required")
        if self.api_key_env and not os.environ.get(self.api_key_env):
            raise ValueError(f"credential environment variable {self.api_key_env} is empty")


@dataclass(frozen=True)
class GenerationConfig:
    generator: ModelEndpoint
    embedding: ModelEndpoint
    size: int = 40
    seed: int = 42
    timeout: float = 300
    run_timeout: float = 7200
    reasoning_effort: str | None = None
    max_output_tokens: int = 4096
    temperature: float | None = None
    gpu_only: bool = False
    top_p: float | None = None
    cache_dir: Path | None = None
    max_workers: int = 1

    def validate(self):
        self.generator.validate()
        self.embedding.validate()
        if type(self.size) is not int or self.size <= 0:
            raise ValueError("size must be a positive integer")
        if type(self.seed) is not int or self.seed < 0:
            raise ValueError("seed must be a nonnegative integer")
        if type(self.max_workers) is not int or self.max_workers <= 0:
            raise ValueError("workers must be a positive integer")
        if type(self.max_output_tokens) is not int or self.max_output_tokens <= 0:
            raise ValueError("generator output token budget must be a positive integer")
        if self.temperature is not None and (not math.isfinite(self.temperature) or not 0 <= self.temperature <= 2):
            raise ValueError("generator temperature must be finite and between 0 and 2")
        if self.top_p is not None and (not math.isfinite(self.top_p) or not 0 < self.top_p <= 1):
            raise ValueError("generator top-p must be finite and greater than 0 and at most 1")
        for value in (self.timeout, self.run_timeout):
            if not math.isfinite(value) or value <= 0:
                raise ValueError("timeouts must be positive and finite")
        if self.reasoning_effort is not None and not self.reasoning_effort.strip():
            raise ValueError("generator reasoning effort must be nonempty when specified")


def document_snapshot(directory):
    root = Path(directory).resolve()
    if not root.is_dir():
        raise ValueError("documents must be an existing directory")
    documents = []
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.suffix.lower() != ".md":
            continue
        raw = path.read_bytes()
        try:
            text = raw.decode("utf-8")
        except UnicodeDecodeError as error:
            raise ValueError(f"invalid UTF-8 Markdown: {path}") from error
        if not text.strip():
            raise ValueError(f"blank Markdown: {path}")
        documents.append({"path": str(path.resolve()), "relative_path": str(path.relative_to(root)),
                          "sha256": hashlib.sha256(raw).hexdigest(), "bytes": len(raw), "text": text})
    if not documents:
        raise ValueError("documents directory contains no Markdown files")
    return documents


def prepare_chunks(documents):
    # Generation dependencies never enter dataset validation or benchmark imports.
    from langchain_core.documents import Document
    from langchain_text_splitters import RecursiveCharacterTextSplitter

    splitter = RecursiveCharacterTextSplitter.from_tiktoken_encoder(
        encoding_name="cl100k_base", chunk_size=1000, chunk_overlap=100, add_start_index=True)
    inputs = [Document(page_content=doc["text"], metadata={k: v for k, v in doc.items() if k != "text"})
              for doc in documents]
    return splitter.split_documents(inputs)


def write_samples(path, rows):
    questions = []
    for index, row in enumerate(rows, 1):
        if not isinstance(row, dict) or any(not isinstance(row.get(key), str) or not row[key].strip()
                                            for key in ("user_input", "reference")):
            raise ValueError("Ragas returned an invalid question or reference")
        contexts = row.get("reference_contexts")
        if not isinstance(contexts, list) or not contexts or not all(isinstance(x, str) and x.strip() for x in contexts):
            raise ValueError("Ragas returned missing reference contexts")
        if not isinstance(row.get("synthesizer_name"), str) or not row["synthesizer_name"]:
            raise ValueError("Ragas returned a missing synthesizer name")
        questions.append({"id": f"generated-{index:04d}", "user_input": row["user_input"], "reference": row["reference"]})
    atomic_json(path / "testset.json", rows)
    atomic_json(path / "questions.json", questions)


def generation_summary(documents, rows, requested, chunk_sources=()):
    coverage = Counter()
    samples = []
    duplicates = {}
    for index, row in enumerate(rows, 1):
        identifier = f"generated-{index:04d}"
        # Ragas prefixes multi-hop evidence with a hop label. Match its original
        # passage without changing the saved framework evidence.
        passages = [re.sub(r"^<\d+-hop>\n\n", "", context) for context in row["reference_contexts"]]
        passage_hashes = {hashlib.sha256(context.encode("utf-8")).hexdigest() for context in passages}
        sources = sorted({chunk["source_path"] for chunk in chunk_sources if chunk["sha256"] in passage_hashes}
                         | {doc["relative_path"] for doc in documents if any(context in doc["text"] for context in passages)})
        coverage.update(sources)
        samples.append({"id": identifier, "synthesizer_name": row["synthesizer_name"], "source_paths": sources})
        duplicates.setdefault(" ".join(row["user_input"].split()).casefold(), []).append(identifier)
    return ({"review_status": "unreviewed", "requested": requested, "generated": len(rows),
             "target_met": len(rows) >= requested,
             "question_types": dict(Counter(row["synthesizer_name"] for row in rows)),
             "document_coverage": {doc["relative_path"]: coverage[doc["relative_path"]] for doc in documents},
             "uncovered_documents": [doc["relative_path"] for doc in documents if not coverage[doc["relative_path"]]],
             "duplicate_questions": [ids for ids in duplicates.values() if len(ids) > 1],
             "unmapped_samples": [row["id"] for row in samples if not row["source_paths"]]}, samples)


def redact(message, config):
    for endpoint in (config.generator, config.embedding):
        if endpoint.api_key_env and (secret := os.environ.get(endpoint.api_key_env)):
            message = message.replace(secret, "[redacted]")
    return message


def cache_namespace(config):
    settings = asdict(config)
    for name in ("cache_dir", "size", "timeout", "run_timeout", "max_workers"):
        settings.pop(name)
    settings["framework"] = "ragas-0.4.3"
    return hashlib.sha256(json.dumps(settings, sort_keys=True).encode()).hexdigest()


def _worker(path, documents, config, events):
    # The worker owns framework event loops; terminating it also stops their threads.
    os.environ["RAGAS_DO_NOT_TRACK"] = "true"
    import random
    random.seed(config.seed)
    import asyncio
    import httpx
    from langchain_core.callbacks import BaseCallbackHandler
    from openai import AsyncOpenAI
    from ragas.embeddings import OpenAIEmbeddings
    from ragas.llms import llm_factory
    from ragas.run_config import RunConfig
    from ragas.testset import TestsetGenerator
    from ragas.testset.synthesizers import default_query_distribution

    class Checkpoint(BaseCallbackHandler):
        raise_error = True
        run_inline = True

        def __init__(self):
            self.names = {}
            self.rows = []

        def on_chain_start(self, serialized, inputs, *, run_id, name=None, **kwargs):
            self.names[run_id] = name or (serialized or {}).get("name")

        def on_chain_end(self, outputs, *, run_id, **kwargs):
            name = self.names.pop(run_id, None)
            if isinstance(outputs, dict) and "sample" in outputs:
                row = outputs["sample"].model_dump(exclude_none=True)
                row["synthesizer_name"] = name
                write_samples(path, self.rows + [row])
                self.rows.append(row)
                events.put({"event": "sample", "count": len(self.rows)})

    clients = []
    try:
        def client(endpoint, role):
            transport = None
            if config.gpu_only:
                from eval.ollama import GPUOnlyTransport
                transport = GPUOnlyTransport(endpoint, role, config.timeout, events)
            result = AsyncOpenAI(base_url=endpoint.base_url,
                                 api_key=os.environ[endpoint.api_key_env] if endpoint.api_key_env else "local",
                                 timeout=config.timeout, max_retries=0,
                                 # Ragas's synchronous stages create fresh loops.
                                 # Pooled async sockets cannot cross closed loops.
                                 http_client=httpx.AsyncClient(timeout=config.timeout, trust_env=False,
                                     transport=transport,
                                     limits=httpx.Limits(max_keepalive_connections=0)))
            clients.append(result)
            return result

        options = {"seed": config.seed, "max_tokens": config.max_output_tokens}
        if config.temperature is not None:
            options["temperature"] = config.temperature
        if config.reasoning_effort is not None:
            options["reasoning_effort"] = config.reasoning_effort
        if config.top_p is not None:
            options["top_p"] = config.top_p
        caches = {}
        if config.cache_dir is not None:
            from ragas.cache import DiskCacheBackend
            for role in ("generator", "embedding"):
                caches[role] = DiskCacheBackend(str(config.cache_dir / cache_namespace(config) / role))
        llm = llm_factory(config.generator.model, client=client(config.generator, "generator"), provider="openai",
                          cache=caches.get("generator"), **options)
        embeddings = OpenAIEmbeddings(client=client(config.embedding, "embedding"), model=config.embedding.model,
                                      cache=caches.get("embedding"))
        chunks = prepare_chunks(documents)
        events.put({"event": "chunks", "count": len(chunks), "sources": [
            {"source_path": chunk.metadata["relative_path"],
             "sha256": hashlib.sha256(chunk.page_content.encode("utf-8")).hexdigest()}
            for chunk in chunks]})
        checkpoint = Checkpoint()
        generator = TestsetGenerator(llm=llm, embedding_model=embeddings)
        testset = generator.generate_with_chunks(
            chunks, testset_size=config.size,
            run_config=RunConfig(timeout=config.timeout, max_workers=config.max_workers, max_retries=1, seed=config.seed),
            callbacks=[checkpoint], raise_exceptions=True)
        rows = testset.to_list()
        write_samples(path, rows)
        distribution = default_query_distribution(llm, generator.knowledge_graph)
        events.put({"event": "done", "distribution": [
            {"synthesizer_name": synth.name, "weight": weight} for synth, weight in distribution]})
    except Exception as error:
        events.put({"event": "error", "message": redact(f"{type(error).__name__}: {error}", config)})
    finally:
        async def close():
            for connection in clients:
                await connection.close()
        try:
            asyncio.run(close())
        except Exception as error:
            events.put({"event": "error", "message": redact(f"client cleanup: {error}", config)})


def generate(run, documents, config, *, worker_target=_worker):
    """Parent owns the run report; child writes atomic draft checkpoints only."""
    config.validate()
    run.record["provenance"].update(
        documents=[{k: v for k, v in doc.items() if k != "text"} for doc in documents],
        generator={"base_url": config.generator.base_url, "model": config.generator.model,
                   "api_key_env": config.generator.api_key_env, "reasoning_effort": config.reasoning_effort,
                   "temperature": config.temperature, "top_p": config.top_p},
        embedding={"base_url": config.embedding.base_url, "model": config.embedding.model,
                   "api_key_env": config.embedding.api_key_env},
        generation={"seed": config.seed, "max_workers": config.max_workers, "request_timeout_seconds": config.timeout,
                    "run_timeout_seconds": config.run_timeout, "chunk_tokens": 1000,
                    "chunk_overlap_tokens": 100, "token_encoding": "cl100k_base",
                    "generator_max_output_tokens": config.max_output_tokens, "telemetry_enabled": False})
    if config.gpu_only:
        run.record["provenance"]["gpu"] = {"required": True, "placement_checks": 0,
            "completed_requests": {}, "request_seconds": {}, "placements": []}
    if config.cache_dir is not None:
        run.record["provenance"]["generation"]["response_cache"] = {
            "backend": "ragas.DiskCacheBackend", "path": str(config.cache_dir.resolve()),
            "namespace": cache_namespace(config)}
    run.record["summary"] = {"review_status": "unreviewed", "requested": config.size, "generated": 0}
    write_samples(run.path, [])
    run.save()
    context = multiprocessing.get_context("spawn")
    events = context.Queue()
    process = context.Process(target=worker_target, args=(run.path.resolve(), documents, config, events))
    interrupted = False
    complete = False
    previous = signal.getsignal(signal.SIGTERM)

    def interrupt(signum, frame):
        raise KeyboardInterrupt

    def drain():
        nonlocal complete
        while True:
            try:
                event = events.get_nowait()
            except queue.Empty:
                break
            if event["event"] == "sample":
                run.record["summary"]["generated"] = event["count"]
                print(f"Draft questions: {event['count']}", flush=True)
            elif event["event"] == "chunks":
                run.record["provenance"]["generation"]["prepared_chunks"] = event["count"]
                run.record["provenance"]["generation"]["chunk_sources"] = event["sources"]
                print(f"Preparing Ragas testset from {event['count']} chunks", flush=True)
            elif event["event"] == "done":
                complete = True
                run.record["provenance"]["generation"]["query_distribution"] = event["distribution"]
            elif event["event"] == "error":
                run.record["errors"].append(redact(event["message"], config))
            elif event["event"] == "gpu":
                gpu = run.record["provenance"]["gpu"]
                gpu["placement_checks"] += 1
                placement = [{key: model.get(key) for key in (
                    "name", "digest", "size", "size_vram", "context_length")} for model in event["models"]]
                if placement not in gpu["placements"]:
                    gpu["placements"].append(placement)
                if event["request_seconds"] is not None:
                    role = event["role"]
                    gpu["completed_requests"][role] = gpu["completed_requests"].get(role, 0) + 1
                    gpu["request_seconds"][role] = gpu["request_seconds"].get(role, 0) + event["request_seconds"]
            run.save()

    try:
        signal.signal(signal.SIGTERM, interrupt)
        deadline = time.monotonic() + config.run_timeout
        process.start()
        while process.is_alive():
            drain()
            if time.monotonic() >= deadline:
                run.record["errors"].append("generation exceeded its overall deadline")
                break
            process.join(timeout=.1)
        drain()
        if not complete and not run.record["errors"]:
            run.record["errors"].append(f"generation worker exited without completion (exit {process.exitcode})")
    except KeyboardInterrupt:
        interrupted = True
        run.record["errors"].append("generation interrupted")
    except Exception as error:
        run.record["errors"].append(redact(f"{type(error).__name__}: {error}", config))
    finally:
        if process.pid is not None:
            if process.is_alive():
                process.terminate()
                process.join(timeout=5)
            if process.is_alive():
                process.kill()
                process.join(timeout=5)
            drain()
        signal.signal(signal.SIGTERM, previous)
        events.close()
        events.join_thread()
        rows = json.loads((run.path / "testset.json").read_text(encoding="utf-8"))
        # Cancellation can land between the two atomic artifact replacements.
        # Rebuild the normalized input from the canonical native checkpoint.
        write_samples(run.path, rows)
        for temporary in run.path.glob(".report-*"):
            temporary.unlink(missing_ok=True)
        summary, samples = generation_summary(documents, rows, config.size,
            run.record["provenance"]["generation"].get("chunk_sources", []))
        run.record["summary"] = summary
        run.record["results"] = {"samples": samples, "dataset": str((run.path / "questions.json").resolve()),
                                 "testset": str((run.path / "testset.json").resolve())}
        run.record["artifacts"] = [artifact(run.path / name, run.path) for name in ("testset.json", "questions.json")]
        if complete and len(rows) < config.size:
            run.record["errors"].append("Ragas returned fewer samples than requested")
        if rows:
            load_dataset(run.path / "questions.json")
        run.finish(interrupted)
    return 130 if interrupted else 1 if run.record["errors"] else 0
