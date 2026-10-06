"""Current evaluation inputs and immutable collected sample validation."""

import hashlib
import json
from pathlib import Path


def load_dataset(path):
    return dataset_snapshot(path)[0]


def dataset_snapshot(path):
    path = Path(path).resolve()
    raw = path.read_bytes()
    rows = json.loads(raw.decode("utf-8"))
    if not isinstance(rows, list) or not rows:
        raise ValueError("dataset must be a nonempty JSON array")
    ids = set()
    normalized = []
    for index, row in enumerate(rows):
        if not isinstance(row, dict) or set(row) - {"id", "user_input", "reference"}:
            raise ValueError(f"sample {index}: expected user_input, reference and optional id")
        for field in ("user_input", "reference"):
            if not isinstance(row.get(field), str) or not row[field].strip():
                raise ValueError(f"sample {index}: {field} must be a nonempty string")
        identifier = row.get("id", f"sample-{index + 1}")
        if not isinstance(identifier, str) or not identifier.strip() or identifier in ids:
            raise ValueError(f"sample {index}: id must be nonempty and unique")
        ids.add(identifier)
        normalized.append({"id": identifier, "user_input": row["user_input"], "reference": row["reference"]})
    return normalized, {"path": str(path), "sha256": hashlib.sha256(raw).hexdigest(),
                        "bytes": len(raw), "kind": "json"}


def load_capture(path):
    capture = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(capture, dict) or capture.get("schema_version") != 1 or capture.get("producer") != "nadir-eval":
        raise ValueError("unsupported capture")
    if not isinstance(capture.get("samples"), list) or not isinstance(capture.get("provenance"), dict):
        raise ValueError("invalid capture")
    identifiers = set()
    for row in capture["samples"]:
        if not isinstance(row, dict) or not isinstance(row.get("sample_id"), str) or row["sample_id"] in identifiers:
            raise ValueError("invalid or duplicate captured sample")
        identifiers.add(row["sample_id"])
        for key in ("user_input", "reference"):
            if not isinstance(row.get(key), str) or not row[key].strip():
                raise ValueError(f"capture requires {key}")
        if row.get("status") not in {"completed", "failed"} or not isinstance(row.get("errors"), list):
            raise ValueError("invalid captured status")
        if row["status"] == "completed":
            if not isinstance(row.get("response"), str) or not row["response"].strip():
                raise ValueError("completed capture requires response")
            for key in ("retrieved_contexts", "admitted_contexts"):
                if not isinstance(row.get(key), list) or not all(isinstance(x, str) for x in row[key]):
                    raise ValueError(f"capture requires string array {key}")
    return capture
