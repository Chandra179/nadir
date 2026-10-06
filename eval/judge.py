"""Explicit OpenAI-compatible judge configuration and Ragas metric mapping."""

from dataclasses import dataclass
import math
import os

from eval.api import validate_url

METRICS = ("faithfulness", "factual_correctness", "context_precision", "context_recall")


@dataclass(frozen=True)
class JudgeConfig:
    base_url: str
    model: str
    api_key_env: str | None = None
    timeout: float = 300
    reasoning_effort: str | None = None

    def validate(self):
        validate_url(self.base_url)
        if not self.model.strip():
            raise ValueError("explicit judge model is required")
        if not math.isfinite(self.timeout) or self.timeout <= 0:
            raise ValueError("judge timeout must be positive and finite")
        if self.api_key_env and not os.environ.get(self.api_key_env):
            raise ValueError(f"judge credential environment variable {self.api_key_env} is empty")
        if self.reasoning_effort is not None and not self.reasoning_effort.strip():
            raise ValueError("judge reasoning effort must be nonempty when specified")

    def provenance(self):
        return {"base_url": self.base_url, "model": self.model,
                "api_key_env": self.api_key_env, "metric_timeout_seconds": self.timeout,
                "reasoning_effort": self.reasoning_effort}


class RagasJudge:
    def __init__(self, config, *, transport=None):
        config.validate()
        # Imports stay here so dataset validation, reports and Locust require no Ragas.
        from openai import AsyncOpenAI
        from ragas.llms import llm_factory
        from ragas.metrics.collections import Faithfulness, FactualCorrectness, ContextPrecision, ContextRecall

        import httpx
        self.client = AsyncOpenAI(
            base_url=config.base_url,
            api_key=os.environ[config.api_key_env] if config.api_key_env else "local",
            timeout=config.timeout, max_retries=0,
            http_client=httpx.AsyncClient(transport=transport, timeout=config.timeout, trust_env=transport is None),
        )
        model_options = {} if config.reasoning_effort is None else {"reasoning_effort": config.reasoning_effort}
        llm = llm_factory(config.model, client=self.client, provider="openai", **model_options)
        self.metrics = {
            "faithfulness": Faithfulness(llm=llm),
            "factual_correctness": FactualCorrectness(llm=llm, mode="f1"),
            "context_precision": ContextPrecision(llm=llm),
            "context_recall": ContextRecall(llm=llm),
        }

    async def score(self, name, sample):
        arguments = {
            "faithfulness": {"user_input": sample["user_input"], "response": sample["response"], "retrieved_contexts": sample["admitted_contexts"]},
            "factual_correctness": {"response": sample["response"], "reference": sample["reference"]},
            "context_precision": {"user_input": sample["user_input"], "reference": sample["reference"],
                                  "retrieved_contexts": sample["retrieved_contexts"]},
            "context_recall": {"user_input": sample["user_input"], "reference": sample["reference"],
                               "retrieved_contexts": sample["retrieved_contexts"]},
        }
        result = await self.metrics[name].ascore(**arguments[name])
        return float(result.value)

    async def close(self):
        await self.client.close()
