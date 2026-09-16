#!/usr/bin/env python3
"""Build an ARQMath release-gate candidate and merge human reviews.

The script uses only the Python standard library.  It has two intentionally
separate stages:

1. ``build`` downloads/verifies the public ARQMath topics and qrels, selects a
   deterministic 40 topics from each 2020--2022 edition, and optionally
   normalizes the public Posts archive into Markdown documents.  Its output
   is a review pack with ``release_gate`` false.
2. ``merge`` combines two independently completed reviewer files, an explicit
   adjudication file, and a privacy-review record into the schema-v3 golden
   format.  It refuses to mark a pack as release-gate evidence unless every
   selected query has both reviewer judgments.

Raw ARQMath data is never written to the repository by this script.  Use an
ignored working directory such as ``var/evaluation/arqmath`` and commit only
the resulting metadata/report when the source license policy permits it.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import html
import json
import os
import re
import sys
import urllib.request
import zipfile
from dataclasses import dataclass
from html.parser import HTMLParser
from pathlib import Path
from typing import BinaryIO, Iterator, Mapping, Sequence
import xml.etree.ElementTree as ET


ARQMATH_HOME = "https://www.cs.rit.edu/~dprl/ARQMath/"
ARQMATH_BACKUP = "https://www.cs.rit.edu/~dprl/ARQMath-backup/"
POSTS_URL = ARQMATH_BACKUP + "Collection/Posts.V1.3.zip"
LICENSE_NOTICE_PATH = "LICENSE-NOTICE.md"


@dataclass(frozen=True)
class EditionSource:
    edition: str
    year: str
    topics_url: str
    topics_sha256: str
    qrels_url: str
    qrels_sha256: str


SOURCES = (
    EditionSource(
        "arqmath-1",
        "2020",
        ARQMATH_BACKUP + "Topics/Task1%263%20Topics/ARQMath-1/Topics/Topics_V2.0.xml",
        "7e88764849bec5901c4d90253b48df189232b01f581d905db41a0c1592ebaf5e",
        ARQMATH_BACKUP + "Evaluation/%20Scripts%20%26%20Qrels/ARQMath-1/Task%201%20/qrel_official_task1",
        "6b8c248e6a70a79bdec79f349b1e51b710338bd146c1cb0d4c9dc6e315ec8948",
    ),
    EditionSource(
        "arqmath-2",
        "2021",
        ARQMATH_BACKUP + "Topics/Task1%263%20Topics/ARQMath-2/Topics/Topics_Task1_2021_V1.1.xml",
        "2c7fec17c0a7f90a08f1cd85b2100a3b3371fad6030527a8766afa824fd3d063",
        ARQMATH_BACKUP + "Evaluation/%20Scripts%20%26%20Qrels/ARQMath-2/Task1/Qrel%20Files/qrel_task1_2021_all.tsv",
        "289f4ae6fb0f9a65fb8101da1a3c45b6c74180a0ccffb73eb5635db854193f38",
    ),
    EditionSource(
        "arqmath-3",
        "2022",
        ARQMATH_BACKUP + "Topics/Task1%263%20Topics/ARQMath-3/Topics/Topics_Task1_2022_V0.1.xml",
        "fe6e0e2e3af41d1b84de05fb708066a2f1f12470e23d1587ae353cfe30fa8c54",
        ARQMATH_BACKUP + "Evaluation/%20Scripts%20%26%20Qrels/ARQMath-3/Task%201/Qrel%20Files/qrel_task1_2022_all.tsv",
        "c8a9fc2020a251c8ecfbf8612760f6bd29433b1e854342c367e6aa0174515517",
    ),
)


class TextExtractor(HTMLParser):
    """Convert the HTML fragments stored inside ARQMath XML into text."""

    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.parts: list[str] = []

    def handle_data(self, data: str) -> None:
        self.parts.append(data)

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag in {"p", "div", "li", "br", "h1", "h2", "h3", "pre"}:
            self.parts.append("\n")

    def handle_endtag(self, tag: str) -> None:
        if tag in {"p", "div", "li", "h1", "h2", "h3", "pre"}:
            self.parts.append("\n")


def html_to_text(value: str) -> str:
    parser = TextExtractor()
    parser.feed(html.unescape(value or ""))
    parser.close()
    lines = [re.sub(r"\s+", " ", line).strip() for line in "".join(parser.parts).splitlines()]
    return "\n".join(line for line in lines if line)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify_sha256(path: Path, expected: str) -> str:
    actual = sha256_file(path)
    if actual.lower() != expected.lower():
        raise ValueError(f"SHA-256 mismatch for {path}: expected {expected}, got {actual}")
    return actual


def download_verified(url: str, destination: Path, expected_sha256: str) -> Path:
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.is_file():
        verify_sha256(destination, expected_sha256)
        return destination
    partial = destination.with_suffix(destination.suffix + ".part")
    digest = hashlib.sha256()
    with urllib.request.urlopen(url, timeout=120) as response, partial.open("wb") as output:
        while True:
            chunk = response.read(1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
            output.write(chunk)
    actual = digest.hexdigest()
    if actual.lower() != expected_sha256.lower():
        partial.unlink(missing_ok=True)
        raise ValueError(f"SHA-256 mismatch for {url}: expected {expected_sha256}, got {actual}")
    partial.replace(destination)
    return destination


def acquire_sources(source_dir: Path) -> dict[str, Path]:
    result: dict[str, Path] = {}
    for source in SOURCES:
        topic_path = download_verified(
            source.topics_url,
            source_dir / f"topics-{source.year}.xml",
            source.topics_sha256,
        )
        qrel_path = download_verified(
            source.qrels_url,
            source_dir / f"qrels-{source.year}.tsv",
            source.qrels_sha256,
        )
        result[f"topics-{source.year}"] = topic_path
        result[f"qrels-{source.year}"] = qrel_path
    return result


def parse_topics(path: Path, edition: str, year: str) -> list[dict[str, object]]:
    root = ET.parse(path).getroot()
    topics: list[dict[str, object]] = []
    for element in root.findall("Topic"):
        source_id = (element.get("number") or "").strip()
        title = html_to_text(element.findtext("Title", ""))
        question = html_to_text(element.findtext("Question", ""))
        tags = [tag.strip() for tag in element.findtext("Tags", "").split(",") if tag.strip()]
        if not source_id or not title or not question:
            raise ValueError(f"invalid ARQMath topic in {path}: missing number/title/question")
        topics.append(
            {
                "source_id": source_id,
                "edition": edition,
                "year": year,
                "query": f"{title}\n\n{question}",
                "tags": tags,
            }
        )
    if not topics:
        raise ValueError(f"no topics found in {path}")
    return topics


def parse_qrels(path: Path) -> dict[str, list[dict[str, object]]]:
    qrels: dict[str, list[dict[str, object]]] = {}
    seen: set[tuple[str, str]] = set()
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        fields = line.split()
        if len(fields) < 4:
            raise ValueError(f"invalid qrels row {path}:{line_number}")
        topic_id, document_id, grade_text = fields[0], fields[2], fields[3]
        try:
            grade = int(grade_text)
        except ValueError as exc:
            raise ValueError(f"invalid qrels grade {path}:{line_number}") from exc
        key = (topic_id, document_id)
        if key in seen:
            raise ValueError(f"duplicate qrels row {topic_id}/{document_id} in {path}")
        seen.add(key)
        qrels.setdefault(topic_id, []).append({"document_id": document_id, "grade": grade})
    if not qrels:
        raise ValueError(f"no qrels found in {path}")
    return qrels


def topic_sort_key(topic: Mapping[str, object]) -> tuple[int, str]:
    source_id = str(topic["source_id"])
    match = re.search(r"(\d+)$", source_id)
    return (int(match.group(1)) if match else 0, source_id)


def select_topics(
    topic_sets: Sequence[Sequence[dict[str, object]]],
    qrel_sets: Sequence[Mapping[str, Sequence[Mapping[str, object]]]],
    per_edition: int = 40,
) -> list[dict[str, object]]:
    if len(topic_sets) != len(qrel_sets):
        raise ValueError("topic and qrels edition counts differ")
    selected: list[dict[str, object]] = []
    for topics, qrels in zip(topic_sets, qrel_sets):
        eligible = [
            topic
            for topic in topics
            if topic["source_id"] in qrels and any(int(row["grade"]) > 0 for row in qrels[topic["source_id"]])
        ]
        eligible.sort(key=topic_sort_key)
        if len(eligible) < per_edition:
            raise ValueError(f"edition {topics[0]['edition']} has only {len(eligible)} eligible topics")
        for topic in eligible[:per_edition]:
            source_id = str(topic["source_id"])
            selected.append(
                {
                    "id": f"arqmath-{topic['year']}-{source_id}",
                    "source_query_id": source_id,
                    "source_edition": topic["edition"],
                    "source_year": topic["year"],
                    "query": topic["query"],
                    "tags": list(topic["tags"]),
                    "candidate_documents": [dict(row) for row in qrels[source_id]],
                }
            )
    ids = [str(query["id"]) for query in selected]
    if len(ids) != len(set(ids)):
        raise ValueError("selected ARQMath queries contain duplicate IDs")
    return selected


def iter_post_rows(path: Path) -> Iterator[dict[str, str]]:
    handles: list[BinaryIO] = []
    archive: zipfile.ZipFile | None = None
    try:
        if zipfile.is_zipfile(path):
            archive = zipfile.ZipFile(path)
            names = [name for name in archive.namelist() if name.lower().endswith("posts.xml")]
            if not names:
                raise ValueError(f"no Posts.xml member found in {path}")
            stream = archive.open(names[0], "r")
            handles.append(stream)
        else:
            stream = path.open("rb")
            handles.append(stream)
        for _, element in ET.iterparse(stream, events=("end",)):
            if element.tag.rsplit("}", 1)[-1] != "row":
                continue
            attributes = {key: value for key, value in element.attrib.items() if value is not None}
            element.clear()
            yield attributes
    finally:
        for handle in handles:
            handle.close()
        if archive is not None:
            archive.close()


def write_corpus(posts_path: Path, corpus_dir: Path, include_all_answers: bool = True, document_ids: set[str] | None = None) -> dict[str, object]:
    corpus_dir.mkdir(parents=True, exist_ok=True)
    posts_dir = corpus_dir / "posts"
    posts_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = corpus_dir / "manifest.jsonl"
    count = 0
    wanted = document_ids or set()
    with manifest_path.open("w", encoding="utf-8", newline="\n") as manifest:
        for row in iter_post_rows(posts_path):
            if row.get("PostTypeId") != "2":
                continue
            post_id = row.get("Id", "").strip()
            if not post_id or (not include_all_answers and post_id not in wanted):
                continue
            body = html_to_text(row.get("Body", ""))
            if not body:
                continue
            relative = Path("posts") / f"{post_id}.md"
            target = corpus_dir / relative
            content = (
                f"# Math Stack Exchange answer {post_id}\n\n"
                f"Source post ID: {post_id}\n"
                f"Parent question ID: {row.get('ParentId', '')}\n\n"
                f"{body}\n"
            )
            target.write_text(content, encoding="utf-8", newline="\n")
            manifest.write(json.dumps({
                "path": relative.as_posix(),
                "source_id": post_id,
                "sha256": sha256_file(target),
                "bytes": target.stat().st_size,
            }, sort_keys=True, ensure_ascii=False) + "\n")
            count += 1
    return {
        "id": "arqmath-math-stackexchange-2010-2018-v1.3",
        "documents": [],
        "document_count": count,
        "representative": include_all_answers,
        "manifest_path": manifest_path.as_posix(),
        "manifest_sha256": sha256_file(manifest_path),
    }


def source_metadata(source_paths: Mapping[str, Path], posts_path: Path | None) -> dict[str, object]:
    artifact_hashes = {path.name: sha256_file(path) for path in sorted(source_paths.values(), key=lambda item: item.name)}
    if posts_path is not None:
        artifact_hashes["Posts.V1.3.zip"] = sha256_file(posts_path)
    return {
        "name": "ARQMath public evaluation collection",
        "homepage": ARQMATH_HOME,
        "license": "Math Stack Exchange CC BY-SA with ARQMath non-commercial snapshot terms",
        "usage": "non-commercial evaluation with attribution",
        "snapshot": "ARQMath-1 through ARQMath-3 Task 1; collection Posts.V1.3",
        "attribution": "Mansouri et al., ARQMath; Math Stack Exchange contributors",
        "license_notice_path": LICENSE_NOTICE_PATH,
        "artifact_sha256": artifact_hashes,
    }


def write_license_notice(output_dir: Path) -> Path:
    path = output_dir / LICENSE_NOTICE_PATH
    path.write_text(
        """# ARQMath license notice

This evaluation pack uses public ARQMath Task 1 topics, qrels, and (when
supplied) the ARQMath Posts snapshot. ARQMath distribution terms require
non-commercial use and attribution. The underlying Math Stack Exchange
contributions are subject to the applicable CC BY-SA terms.

Sources and attribution:

- ARQMath resources: https://www.cs.rit.edu/~dprl/ARQMath/arqmath-resources.html
- ARQMath guidelines: https://www.cs.rit.edu/~dprl/ARQMath-backup/ARQMath-Guidlines-v5.3.pdf
- Stack Exchange licensing: https://opendata.stackexchange.com/help/licensing
- ARQMath authors and Math Stack Exchange contributors

This notice is informational. Operators must confirm that their intended use
complies with the source terms before distributing raw or normalized data.
""",
        encoding="utf-8",
        newline="\n",
    )
    return path


def validate_review_pack(pack: Mapping[str, object], minimum_queries: int = 100) -> None:
    """Validate the acquisition-stage pack before it becomes an artifact."""

    if pack.get("schema_version") != 1:
        raise ValueError("review pack schema_version must be 1")
    if pack.get("status") != "awaiting-independent-human-review":
        raise ValueError("review pack must be awaiting independent human review")
    metadata = pack.get("metadata")
    queries = pack.get("queries")
    if not isinstance(metadata, dict) or not isinstance(queries, list):
        raise ValueError("review pack requires metadata and queries")
    if metadata.get("release_gate") is not False:
        raise ValueError("review pack must set release_gate=false")
    source = metadata.get("source")
    if not isinstance(source, dict) or "arqmath" not in str(source.get("name", "")).casefold():
        raise ValueError("review pack requires ARQMath source metadata")
    for field in ("homepage", "license", "usage", "snapshot", "attribution", "license_notice_path"):
        if not str(source.get(field, "")).strip():
            raise ValueError(f"review pack source requires {field}")
    artifacts = source.get("artifact_sha256")
    if not isinstance(artifacts, dict) or not artifacts:
        raise ValueError("review pack requires source artifact hashes")
    for name, digest in artifacts.items():
        if not str(name).strip() or not re.fullmatch(r"[0-9a-fA-F]{64}", str(digest)):
            raise ValueError("review pack source artifact hashes must be named SHA-256 values")
    if len(queries) < minimum_queries:
        raise ValueError(f"review pack requires at least {minimum_queries} queries")
    seen_query_ids: set[str] = set()
    for query in queries:
        if not isinstance(query, dict):
            raise ValueError("review pack queries must be objects")
        query_id = str(query.get("id", "")).strip()
        if not query_id or query_id in seen_query_ids:
            raise ValueError("review pack contains missing or duplicate query IDs")
        if not str(query.get("query", "")).strip() or not str(query.get("source_query_id", "")).strip():
            raise ValueError(f"review pack query {query_id} requires source and query text")
        candidate_rows = query.get("candidate_documents")
        if not isinstance(candidate_rows, list) or not candidate_rows:
            raise ValueError(f"review pack query {query_id} requires candidate documents")
        seen_document_ids: set[str] = set()
        for candidate in candidate_rows:
            if not isinstance(candidate, dict):
                raise ValueError(f"review pack query {query_id} has an invalid candidate")
            document_id = str(candidate.get("document_id", "")).strip()
            grade = candidate.get("grade")
            if not document_id.isdigit() or isinstance(grade, bool) or not isinstance(grade, int) or grade < 0:
                raise ValueError(f"review pack query {query_id} has an invalid candidate document")
            if document_id in seen_document_ids:
                raise ValueError(f"review pack query {query_id} has duplicate candidate document {document_id}")
            seen_document_ids.add(document_id)
        seen_query_ids.add(query_id)


def write_review_report(pack: Mapping[str, object], output_dir: Path) -> Path:
    metadata = pack["metadata"]
    queries = pack["queries"]
    assert isinstance(metadata, dict)
    assert isinstance(queries, list)
    years = sorted({str(query["source_year"]) for query in queries if isinstance(query, dict)})
    report = {
        "report_type": "arqmath_review_pack_acquisition",
        "dataset": metadata["dataset"],
        "status": pack["status"],
        "release_gate": metadata["release_gate"],
        "query_count": len(queries),
        "queries_per_source_year": {year: sum(isinstance(query, dict) and str(query.get("source_year")) == year for query in queries) for year in years},
        "source": metadata["source"],
        "corpus": metadata["corpus"],
        "review_requirements": metadata["review_requirements"],
        "retrieval_evaluation": {
            "status": "not_run",
            "reason": "The representative ARQMath Posts corpus and independent human review artifacts are pending; no quality score is recorded.",
        },
        "limitations": [
            "The checked-in candidate contains public ARQMath topics/qrels, not consented Nadir user telemetry.",
            "The full Posts.V1.3 snapshot is not committed and was not available during this build.",
            "ARQMath qrels are candidate-pool evidence, not a substitute for two new independent reviews.",
        ],
    }
    path = output_dir / "arqmath-review-report.json"
    path.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    return path


def build_review_pack(
    source_dir: Path,
    output_dir: Path,
    posts_path: Path | None,
    posts_url: str | None,
    posts_sha256: str | None,
    per_edition: int,
) -> Path:
    source_paths = acquire_sources(source_dir)
    topic_sets: list[list[dict[str, object]]] = []
    qrel_sets: list[dict[str, list[dict[str, object]]]] = []
    for source in SOURCES:
        topic_sets.append(parse_topics(source_paths[f"topics-{source.year}"], source.edition, source.year))
        qrel_sets.append(parse_qrels(source_paths[f"qrels-{source.year}"]))
    queries = select_topics(topic_sets, qrel_sets, per_edition=per_edition)
    output_dir.mkdir(parents=True, exist_ok=True)
    write_license_notice(output_dir)
    corpus: dict[str, object]
    if posts_path is None and posts_sha256:
        if not re.fullmatch(r"[0-9a-fA-F]{64}", posts_sha256):
            raise ValueError("--posts-sha256 must be a hexadecimal SHA-256")
        posts_path = download_verified(posts_url or POSTS_URL, source_dir / "Posts.V1.3.zip", posts_sha256)
    if posts_path is None:
        corpus = {
            "id": "arqmath-math-stackexchange-2010-2018-v1.3",
            "documents": [],
            "document_count": 0,
            "representative": False,
            "manifest_path": "",
            "manifest_sha256": "",
        }
    else:
        if not posts_sha256 or not re.fullmatch(r"[0-9a-fA-F]{64}", posts_sha256):
            raise ValueError("--posts-sha256 is required when --posts is supplied")
        verify_sha256(posts_path, posts_sha256)
        corpus = write_corpus(posts_path, output_dir / "corpus", include_all_answers=True)
        manifest_path = Path(str(corpus["manifest_path"]))
        corpus["manifest_path"] = manifest_path.relative_to(output_dir).as_posix()
    pack = {
        "schema_version": 1,
        "status": "awaiting-independent-human-review",
        "metadata": {
            "dataset": "arqmath-task1-release-gate-candidate",
            "provenance": "Public ARQMath Task 1 topics and qrels selected deterministically across editions 1, 2, and 3.",
            "consent": "publicly licensed source; non-commercial snapshot terms; privacy/legal review pending",
            "judgment": "ARQMath source qrels are preserved as candidate-pool evidence; two new independent human reviews are required",
            "release_gate": False,
            "source": source_metadata(source_paths, posts_path),
            "corpus": corpus,
            "review_requirements": {
                "annotators": 2,
                "judgments_per_query": 2,
                "adjudication_required": True,
                "expected_answer_and_claims_required": True,
            },
        },
        "queries": queries,
    }
    validate_review_pack(pack)
    output_path = output_dir / "arqmath-review-pack.json"
    output_path.write_text(json.dumps(pack, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    write_review_report(pack, output_dir)
    return output_path


def load_json(path: Path) -> dict[str, object]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"read JSON {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise ValueError(f"{path} must contain a JSON object")
    return value


def review_rows(path: Path) -> tuple[dict[str, object], dict[str, dict[str, object]]]:
    value = load_json(path)
    reviewer = value.get("reviewer")
    judgments = value.get("judgments")
    if not isinstance(reviewer, dict) or not isinstance(judgments, list):
        raise ValueError(f"{path} requires reviewer and judgments")
    reviewer_id = str(reviewer.get("id", "")).strip()
    if (
        not reviewer_id
        or not str(reviewer.get("role", "")).strip()
        or reviewer.get("human") is not True
        or reviewer.get("independent") is not True
        or not str(reviewer.get("verification_ref", "")).strip()
    ):
        raise ValueError(f"{path} reviewer must be a verified independent human with a role")
    result: dict[str, dict[str, object]] = {}
    for row in judgments:
        if not isinstance(row, dict):
            raise ValueError(f"{path} contains a non-object judgment")
        query_id = str(row.get("query_id", "")).strip()
        if not query_id or query_id in result:
            raise ValueError(f"{path} contains a missing or duplicate query_id")
        relevant = row.get("relevant")
        if not isinstance(relevant, list) or not relevant:
            raise ValueError(f"{path} judgment {query_id} needs relevant labels")
        result[query_id] = row
    reviewer = dict(reviewer)
    reviewer["id"] = reviewer_id
    return reviewer, result


def map_labels(labels: object, corpus_root: Path, candidate_ids: set[str]) -> list[dict[str, object]]:
    if not isinstance(labels, list):
        raise ValueError("labels must be a list")
    mapped: list[dict[str, object]] = []
    seen: set[str] = set()
    for label in labels:
        if not isinstance(label, dict):
            raise ValueError("relevance labels must be objects")
        document_id = str(label.get("document_id", "")).strip()
        if not document_id or not document_id.isdigit():
            raise ValueError("relevance label needs a numeric document_id")
        if document_id not in candidate_ids:
            raise ValueError(f"relevance label references document outside the fixed candidate pool: {document_id}")
        if document_id in seen:
            raise ValueError(f"relevance labels contain duplicate document_id {document_id}")
        seen.add(document_id)
        grade = label.get("grade")
        if isinstance(grade, bool) or not isinstance(grade, int) or grade < 0:
            raise ValueError(f"relevance label {document_id} needs an integer grade")
        relative = (Path("posts") / f"{document_id}.md").as_posix()
        if not (corpus_root / "posts" / f"{document_id}.md").is_file():
            raise ValueError(f"review references missing corpus document {document_id}")
        mapped.append({"file": relative, "contains": f"Source post ID: {document_id}", "grade": grade})
    return mapped


def validate_complete_label_set(row: Mapping[str, object], candidate_ids: set[str], context: str) -> None:
    """Require one explicit positive/negative label for every candidate."""

    relevant = row.get("relevant")
    distractors = row.get("distractors", [])
    if not isinstance(relevant, list) or not relevant:
        raise ValueError(f"{context} requires at least one relevant label")
    if not isinstance(distractors, list):
        raise ValueError(f"{context} distractors must be a list")
    seen: set[str] = set()
    for labels, positive in ((relevant, True), (distractors, False)):
        for label in labels:
            if not isinstance(label, dict):
                raise ValueError(f"{context} contains a non-object label")
            document_id = str(label.get("document_id", "")).strip()
            grade = label.get("grade")
            if not document_id.isdigit() or isinstance(grade, bool) or not isinstance(grade, int) or grade < 0:
                raise ValueError(f"{context} contains an invalid document label")
            if document_id not in candidate_ids:
                raise ValueError(f"{context} references document outside the fixed candidate pool: {document_id}")
            if document_id in seen:
                raise ValueError(f"{context} labels document {document_id} more than once")
            if positive and grade == 0:
                raise ValueError(f"{context} marks document {document_id} relevant with grade 0")
            if not positive and grade != 0:
                raise ValueError(f"{context} marks document {document_id} as a distractor with non-zero grade")
            seen.add(document_id)
    if seen != candidate_ids:
        missing = sorted(candidate_ids - seen)
        raise ValueError(f"{context} must label every candidate; missing {', '.join(missing[:5])}")


def merge_reviews(review_pack_path: Path, reviewer_paths: Sequence[Path], adjudication_path: Path, privacy_path: Path, output_path: Path) -> Path:
    if len(reviewer_paths) != 2:
        raise ValueError("exactly two independent reviewer files are required")
    pack = load_json(review_pack_path)
    metadata = pack.get("metadata")
    pack_queries = pack.get("queries")
    if not isinstance(metadata, dict) or not isinstance(pack_queries, list):
        raise ValueError("review pack requires metadata and queries")
    if metadata.get("release_gate"):
        raise ValueError("review pack must start with release_gate=false")
    reviewers: list[dict[str, object]] = []
    judgments: list[dict[str, dict[str, object]]] = []
    for path in reviewer_paths:
        reviewer, rows = review_rows(path)
        reviewers.append(reviewer)
        judgments.append(rows)
    if reviewers[0]["id"] == reviewers[1]["id"]:
        raise ValueError("reviewer IDs must be distinct")
    query_ids = {str(query.get("id", "")) for query in pack_queries if isinstance(query, dict)}
    if len(query_ids) != len(pack_queries):
        raise ValueError("review pack contains missing or duplicate query IDs")
    if set(judgments[0]) != query_ids or set(judgments[1]) != query_ids:
        raise ValueError("each reviewer must judge every selected query")
    adjudication = load_json(adjudication_path)
    adjudicated_rows = adjudication.get("queries")
    if not isinstance(adjudicated_rows, list):
        raise ValueError("adjudication file requires queries")
    adjudicated = {str(row.get("query_id", "")): row for row in adjudicated_rows if isinstance(row, dict)}
    if len(adjudicated) != len(adjudicated_rows) or set(adjudicated) != query_ids:
        raise ValueError("adjudication must contain every selected query exactly once")
    privacy = load_json(privacy_path)
    for key in ("status", "reviewer", "reviewed_at", "evidence_path", "evidence_sha256"):
        if not str(privacy.get(key, "")).strip():
            raise ValueError(f"privacy review requires {key}")
    if str(privacy["status"]).lower() != "approved" or not re.fullmatch(r"[0-9a-fA-F]{64}", str(privacy["evidence_sha256"])):
        raise ValueError("privacy review must be approved and include a SHA-256 evidence hash")
    try:
        dt.datetime.fromisoformat(str(privacy["reviewed_at"]).replace("Z", "+00:00"))
    except ValueError as exc:
        raise ValueError("privacy review reviewed_at must be an ISO-8601 timestamp") from exc
    source = metadata.get("source")
    if not isinstance(source, dict) or "arqmath" not in str(source.get("name", "")).strip().lower():
        raise ValueError("review pack must identify ARQMath source metadata")
    source_hashes = source.get("artifact_sha256")
    if not isinstance(source_hashes, dict) or not re.fullmatch(r"[0-9a-fA-F]{64}", str(source_hashes.get("Posts.V1.3.zip", ""))):
        raise ValueError("review pack must include the verified Posts.V1.3.zip hash")
    corpus = metadata.get("corpus")
    if not isinstance(corpus, dict) or not corpus.get("representative") or not str(corpus.get("manifest_path", "")).strip() or not re.fullmatch(r"[0-9a-fA-F]{64}", str(corpus.get("manifest_sha256", ""))):
        raise ValueError("review pack corpus must be representative and have a verified manifest")
    manifest_ref = Path(str(corpus["manifest_path"]))
    manifest_path = manifest_ref if manifest_ref.is_absolute() else (review_pack_path.parent / manifest_ref).resolve()
    corpus_root = manifest_path.parent
    if not manifest_path.is_file():
        raise ValueError(f"review pack corpus manifest is missing: {manifest_path}")
    if sha256_file(manifest_path).lower() != str(corpus["manifest_sha256"]).lower():
        raise ValueError("review pack corpus manifest hash does not match the manifest")
    final_queries: list[dict[str, object]] = []
    artifact_hash = hashlib.sha256()
    for path in [*reviewer_paths, adjudication_path]:
        artifact_hash.update(path.read_bytes())
    for query in pack_queries:
        query_id = str(query["id"])
        candidate_rows = query.get("candidate_documents")
        if not isinstance(candidate_rows, list) or not candidate_rows:
            raise ValueError(f"review pack query {query_id} has no fixed candidate pool")
        candidate_ids: set[str] = set()
        final_candidates: list[dict[str, object]] = []
        for candidate in candidate_rows:
            if not isinstance(candidate, dict):
                raise ValueError(f"review pack query {query_id} has an invalid candidate")
            document_id = str(candidate.get("document_id", "")).strip()
            grade = candidate.get("grade")
            if not document_id.isdigit() or isinstance(grade, bool) or not isinstance(grade, int) or grade < 0:
                raise ValueError(f"review pack query {query_id} has an invalid candidate document")
            if document_id in candidate_ids:
                raise ValueError(f"review pack query {query_id} has duplicate candidate document {document_id}")
            candidate_ids.add(document_id)
            final_candidates.append({"document_id": document_id, "source_grade": grade})
        canonical = adjudicated[query_id]
        if not str(canonical.get("expected_answer", "")).strip() or not isinstance(canonical.get("required_claims"), list) or not canonical["required_claims"]:
            raise ValueError(f"adjudication {query_id} needs expected answer and required claims")
        validate_complete_label_set(canonical, candidate_ids, f"adjudication {query_id}")
        relevant = map_labels(canonical.get("relevant"), corpus_root, candidate_ids)
        distractors = map_labels(canonical.get("distractors", []), corpus_root, candidate_ids)
        recorded: list[dict[str, object]] = []
        for reviewer, rows in zip(reviewers, judgments):
            row = rows[query_id]
            validate_complete_label_set(row, candidate_ids, f"reviewer {reviewer['id']} query {query_id}")
            recorded.append({
                "annotator_id": reviewer["id"],
                "relevant": map_labels(row.get("relevant"), corpus_root, candidate_ids),
                "distractors": map_labels(row.get("distractors", []), corpus_root, candidate_ids),
            })
        final_queries.append({
            "id": query_id,
            "query": query["query"],
            "type": canonical.get("type", "factoid"),
            "faithfulness_label": canonical.get("faithfulness_label", "fully_supported"),
            "expected_answer": canonical["expected_answer"],
            "required_claims": canonical["required_claims"],
            "tags": query.get("tags", []),
            "source_query_id": query.get("source_query_id", ""),
            "source_edition": query.get("source_edition", ""),
            "source_year": query.get("source_year", ""),
            "candidate_documents": final_candidates,
            "relevant": relevant,
            "distractors": distractors,
            "judgments": recorded,
            "adjudication": {
                "method": canonical.get("method", "independent human judgments with explicit adjudication"),
                "reviewer_ids": [reviewer["id"] for reviewer in reviewers],
            },
        })
    final_metadata = dict(metadata)
    final_metadata.update({
        "consent": "publicly licensed ARQMath source; non-commercial snapshot terms; privacy review approved",
        "judgment": "two independent human math-capable reviewers with explicit per-query adjudication",
        "release_gate": True,
        "judgment_artifact_path": os.path.relpath(adjudication_path, output_path.parent),
        "judgment_artifact_sha256": artifact_hash.hexdigest(),
        "privacy_review": privacy,
        "annotators": reviewers,
    })
    output = {"schema_version": 3, "metadata": final_metadata, "queries": final_queries}
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(output, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    return output_path


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    build = subparsers.add_parser("build", help="download source artifacts and build the human-review pack")
    build.add_argument("--source-dir", type=Path, required=True)
    build.add_argument("--output-dir", type=Path, required=True)
    build.add_argument("--posts", type=Path, help="verified Posts.V1.3.zip or Posts.xml; omit only to create a pending query pack")
    build.add_argument("--posts-url", default=POSTS_URL, help="URL for the pinned Posts archive when --posts is omitted")
    build.add_argument("--posts-sha256", help="trusted SHA-256 for --posts; required when --posts is supplied")
    build.add_argument("--per-edition", type=int, default=40)
    merge = subparsers.add_parser("merge", help="merge two human reviews into a schema-v3 golden set")
    merge.add_argument("--review-pack", type=Path, required=True)
    merge.add_argument("--reviewer", type=Path, action="append", required=True)
    merge.add_argument("--adjudication", type=Path, required=True)
    merge.add_argument("--privacy-review", type=Path, required=True)
    merge.add_argument("--output", type=Path, required=True)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        if args.command == "build":
            if args.per_edition < 1:
                raise ValueError("--per-edition must be positive")
            output = build_review_pack(args.source_dir, args.output_dir, args.posts, args.posts_url, args.posts_sha256, args.per_edition)
        else:
            output = merge_reviews(args.review_pack, args.reviewer, args.adjudication, args.privacy_review, args.output)
    except (OSError, ValueError, ET.ParseError, zipfile.BadZipFile) as exc:
        print(f"arqmath importer: {exc}", file=sys.stderr)
        return 1
    print(output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
