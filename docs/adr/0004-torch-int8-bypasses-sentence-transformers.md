# 0004 — `torch-int8` bypasses sentence-transformers' predict plumbing

- **Status:** Accepted
- **Date:** 2026-09-05
- **Deciders:** Chandra, ZCode session

## Context

The natural implementation of the `torch-int8` backend (ADR 0003) was to
load a `CrossEncoder` and swap its inner transformers model for a
dynamically quantized copy:

```python
model = CrossEncoder(name, max_length=512)
model.model = quantize_dynamic(model.model, {torch.nn.Linear}, dtype=torch.qint8)
```

This fails systematically — reproduced on both ms-marco-MiniLM and
bge-reranker-v2-m3. `predict` crashes inside the transformers forward with
`input_ids` arriving as a `BatchEncoding` object instead of a tensor:
sentence-transformers 6's `Transformer.forward` dispatches on
`self.model_forward_params` / module wiring that `quantize_dynamic`'s
deepcopy invalidates, so the quantized model's forward gets called through a
path that passes the tokenized features positionally. Verified by spying on
the forward: the fp32 model receives correct kwargs; the quantized copy does
not.

## Decision

The `torch-int8` backend does **not** use `CrossEncoder` at all.
`TorchInt8Reranker` (in `services/reranker/main.py`) is a minimal pair
scorer that owns the whole loop:

```python
model = AutoModelForSequenceClassification.from_pretrained(name)
model = quantize_dynamic(model, {torch.nn.Linear}, dtype=torch.qint8)
# predict: tokenizer(pairs, padding=True, truncation=True,
#                    max_length=MAX_LENGTH, return_tensors="pt")
#          → model(**inputs).logits → sigmoid → tolist()
```

It matches `CrossEncoder` semantics for single-label rerankers (the whole
sidecar API surface is `predict(pairs) → scores.tolist()`, sigmoid on the
one logit for `num_labels == 1`) while depending only on transformers +
torch for inference. The `/rerank` handler is unchanged.

## Consequences

- The torch-int8 route is robust across model swaps and sentence-transformers
  version bumps — there is no private ST wiring to track.
- Semantics parity must be maintained by hand if `CrossEncoder` ever changes
  its activation contract (currently: sigmoid when `num_labels == 1`); the
  evalbench A/B plus the fp32-vs-int8 score check guard this.
- The fp32 `torch` backend and the ONNX backends keep using `CrossEncoder`;
  only the torch-int8 route uses the custom scorer.
- A similar bypass is the template if future quantization approaches (e.g.
  torch.export-based) hit the same dispatch incompatibility.
