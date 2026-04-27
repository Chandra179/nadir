"""
Marker PDF-to-markdown conversion sidecar.

Replaces Docling for PDF preprocessing. Marker produces accurate LaTeX math
($...$, $$...$$) where Docling emits <!-- formula-not-decoded --> placeholders.

Install:
    pip install -r services/marker/requirements.txt

Run (one-shot, converts all PDFs in input_dir):
    python services/marker/main.py --input pdfs/raw --output pdfs/converted

Run (watch via FastAPI trigger):
    uvicorn services.marker.main:app --port 5003
    curl -X POST http://localhost:5003/convert

API (when running as server):
    POST /convert            convert all pending PDFs in input_dir → output_dir
    GET  /health
"""

import argparse
import os
import sys
from pathlib import Path


def convert_dir(input_dir: Path, output_dir: Path) -> list[str]:
    from marker.converters.pdf import PdfConverter
    from marker.models import create_model_dict
    from marker.output import text_from_rendered

    output_dir.mkdir(parents=True, exist_ok=True)
    models = create_model_dict()
    converter = PdfConverter(artifact_dict=models)
    converted = []

    for pdf in sorted(input_dir.glob("*.pdf")):
        out_path = output_dir / (pdf.stem + ".md")
        # skip if output already newer than source
        if out_path.exists() and out_path.stat().st_mtime >= pdf.stat().st_mtime:
            continue
        rendered = converter(str(pdf))
        md, _, _ = text_from_rendered(rendered)
        out_path.write_text(md, encoding="utf-8")
        converted.append(pdf.name)
        print(f"converted: {pdf.name} -> {out_path.name}")

    return converted


def main_cli():
    parser = argparse.ArgumentParser(description="Convert PDFs to markdown via Marker")
    parser.add_argument("--input", required=True, help="dir containing PDF files")
    parser.add_argument("--output", required=True, help="dir to write .md files")
    args = parser.parse_args()

    converted = convert_dir(Path(args.input), Path(args.output))
    print(f"done: {len(converted)} file(s) converted")


# FastAPI server mode (optional — for trigger-based use)
try:
    from fastapi import FastAPI

    INPUT_DIR = Path(os.getenv("MARKER_INPUT_DIR", "pdfs/raw"))
    OUTPUT_DIR = Path(os.getenv("MARKER_OUTPUT_DIR", "pdfs/converted"))

    app = FastAPI()

    @app.post("/convert")
    def convert():
        converted = convert_dir(INPUT_DIR, OUTPUT_DIR)
        return {"converted": converted, "count": len(converted)}

    @app.get("/health")
    def health():
        return {"status": "ok"}

except ImportError:
    app = None


if __name__ == "__main__":
    if len(sys.argv) > 1:
        main_cli()
    elif app is not None:
        import uvicorn
        uvicorn.run(app, host="0.0.0.0", port=5003)
    else:
        print("install fastapi+uvicorn for server mode, or pass --input/--output for CLI mode")
        sys.exit(1)
