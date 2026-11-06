# 0004 — `torch-int8` bypasses sentence-transformers' predict plumbing

- **Status:** Accepted
- **Date:** 2026-09-05
- **Deciders:** Chandra, ZCode session

## Context

The `torch-int8` reranker backend should have swapped a `CrossEncoder`'s inner
model for a dynamically quantized copy:

```python
model = CrossEncoder(name, max_length=512)
model.model = quantize_dynamic(model.model, {torch.nn.Linear}, dtype=torch.qint8)
```

This fails on both ms-marco-MiniLM and bge-reranker-v2-m3. `predict` crashes in
the transformers forward with `input_ids` arriving as a `BatchEncoding`
instead of a tensor: sentence-transformers 6 dispatches on module wiring that
`quantize_dynamic`'s deepcopy invalidates, so the quantized model receives the
tokenized features positionally.

## Decision

`torch-int8` does not use `CrossEncoder`. `TorchInt8Reranker` (in
`sidecars/reranker/main.py`) is a minimal pair scorer:

```python
model = AutoModelForSequenceClassification.from_pretrained(name)
model = quantize_dynamic(model, {torch.nn.Linear}, dtype=torch.qint8)
# predict: tokenizer(pairs, padding=True, truncation=True,
#                    max_length=MAX_LENGTH, return_tensors="pt")
#          → model(**inputs).logits → sigmoid
```

It matches `CrossEncoder` for single-label rerankers (sigmoid over the one
logit when `num_labels == 1`) and depends only on transformers and torch. The
`/rerank` handler is unchanged.

## Consequences

- The route survives model swaps and sentence-transformers upgrades; there is
  no private wiring to track.
- If `CrossEncoder` changes its activation contract, parity must be restored by
  hand. Rerank A/Bs and an fp32-vs-int8 score check guard this.
- The fp32 `torch` backend still uses `CrossEncoder`.
- Measured on 34 queries (bge-v2-m3): MRR@10 0.793 → 0.765, p50 4358 ms →
  3372 ms (1.29×), about 1.5 GB less RAM.
- The ONNX and OpenVINO backends were later removed (see 0031), leaving `torch`
  and `torch-int8`.
