"""
Docling PDF-to-markdown conversion sidecar.

Install:
    pip install docling

Run (one-shot, converts all PDFs in input_dir):
    python sidecars/document-converter/main.py --input pdfs/raw --output pdfs/converted

Run (watch via FastAPI trigger):
    uvicorn main:app --app-dir sidecars/document-converter --port 5003
    curl -X POST http://localhost:5003/convert

API (when running as server):
    POST /convert            convert raw application/pdf bytes or all pending PDFs
    GET  /health
"""

import argparse
import os
import sys
import tempfile
from pathlib import Path


def convert_dir(input_dir: Path, output_dir: Path) -> list[str]:
    from docling.document_converter import DocumentConverter

    output_dir.mkdir(parents=True, exist_ok=True)
    converter = DocumentConverter()
    converted = []

    for pdf in sorted(input_dir.glob("*.pdf")):
        out_path = output_dir / (pdf.stem + ".md")
        # skip if output already newer than source
        if out_path.exists() and out_path.stat().st_mtime >= pdf.stat().st_mtime:
            continue
        result = converter.convert(str(pdf))
        md = result.document.export_to_markdown()
        out_path.write_text(md, encoding="utf-8")
        converted.append(pdf.name)
        print(f"converted: {pdf.name} -> {out_path.name}")

    return converted


def convert_bytes(filename: str, data: bytes) -> str:
    """Convert one uploaded PDF without requiring a shared filesystem."""
    from docling.document_converter import DocumentConverter

    suffix = Path(filename).suffix or ".pdf"
    with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as source:
        source.write(data)
        source_path = Path(source.name)
    try:
        result = DocumentConverter().convert(str(source_path))
        return result.document.export_to_markdown()
    finally:
        source_path.unlink(missing_ok=True)


def main_cli():
    parser = argparse.ArgumentParser(description="Convert PDFs to markdown via Docling")
    parser.add_argument("--input", required=True, help="dir containing PDF files")
    parser.add_argument("--output", required=True, help="dir to write .md files")
    args = parser.parse_args()

    converted = convert_dir(Path(args.input), Path(args.output))
    print(f"done: {len(converted)} file(s) converted")


# FastAPI server mode (optional — for trigger-based use)
try:
    from fastapi import FastAPI, Request
    from fastapi.responses import PlainTextResponse
    import uvicorn

    INPUT_DIR = Path(os.getenv("DOCLING_INPUT_DIR", "pdfs/raw"))
    OUTPUT_DIR = Path(os.getenv("DOCLING_OUTPUT_DIR", "pdfs/converted"))

    app = FastAPI()

    @app.post("/convert")
    async def convert(request: Request):
        if request.headers.get("content-type", "").startswith("application/pdf"):
            data = await request.body()
            filename = request.headers.get("x-nadir-filename", "document.pdf")
            return PlainTextResponse(convert_bytes(filename, data), media_type="text/markdown")
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
