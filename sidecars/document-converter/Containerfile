FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Pre-download docling models at build time so container startup is fast
RUN python -c "from docling.document_converter import DocumentConverter; DocumentConverter()"

COPY main.py .

# Dirs mounted at runtime via docker compose volumes
RUN mkdir -p /data/raw /data/converted

ENV DOCLING_INPUT_DIR=/data/raw
ENV DOCLING_OUTPUT_DIR=/data/converted

EXPOSE 5003
CMD ["python", "main.py"]
