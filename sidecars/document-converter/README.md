# Document converter sidecar

The document converter accepts PDF files and converts them to Markdown using
Docling. Nadir uses it as an optional intake step before the normal indexing
pipeline. The original PDF remains the source identity for citations.

## Interface

### `POST /convert`

For one uploaded PDF, send the bytes with `Content-Type: application/pdf`.
Optionally provide the original filename in `X-Nadir-Filename`:

```bash
curl -X POST http://localhost:5003/convert \
  -H 'Content-Type: application/pdf' \
  -H 'X-Nadir-Filename: handbook.pdf' \
  --data-binary @handbook.pdf
```

The response is Markdown with `Content-Type: text/markdown`.

When the request is not an `application/pdf` upload, the sidecar scans
`DOCLING_INPUT_DIR`, converts pending PDFs, and returns JSON:

```json
{
  "converted": ["handbook.pdf"],
  "count": 1
}
```

### `GET /health`

Returns `{"status":"ok"}` with HTTP `200` while the HTTP process is running.
This is a process health check; it does not force-load or validate a Docling
model. Conversion errors are returned by the conversion request.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DOCLING_INPUT_DIR` | `pdfs/raw` | Directory scanned for pending PDFs |
| `DOCLING_OUTPUT_DIR` | `pdfs/converted` | Directory receiving generated Markdown |

The HTTP server listens on port `5003`. The Go API's `docling.addr` setting
points to this process; in Compose the service name remains `docling`.

## CPU, GPU, and platform behavior

- The current image is based on `python:3.11-slim` and installs no CUDA
  runtime or GPU reservation.
- Conversion therefore runs on CPU in the base Compose stack.
- The same image is suitable for Linux, Windows Docker Desktop, and macOS.
- Docker build downloads the Docling model in advance, so the image is large
  but startup does not need a model download.
- If GPU acceleration is needed later, add it as an explicit deployment
  variant rather than changing the HTTP contract; the current implementation
  has no GPU-selection configuration.

## Local run

From the repository root, install dependencies and run a one-shot conversion:

```bash
venv/bin/pip install -r sidecars/document-converter/requirements.txt
venv/bin/python sidecars/document-converter/main.py \
  --input pdfs/raw --output pdfs/converted
```

To run the HTTP sidecar instead:

```bash
venv/bin/uvicorn main:app --app-dir sidecars/document-converter \
  --host 0.0.0.0 --port 5003
curl http://localhost:5003/health
```

With Docker Compose, start this optional process with:

```bash
docker compose -f deploy/compose/docker-compose.yml \
  --profile pdf up -d --build docling
```
