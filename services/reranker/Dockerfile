FROM python:3.11-slim

WORKDIR /app

# Model is swappable at build time via --build-arg RERANKER_MODEL=... and at
# runtime via the RERANKER_MODEL env var (see main.py). The int8 export baked
# below only applies to the build-time model; a runtime-swapped model degrades
# to fp32 onnx unless the image is rebuilt with it.
ARG RERANKER_MODEL=BAAI/bge-reranker-v2-m3
ENV RERANKER_MODEL=${RERANKER_MODEL}

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Pre-download the model and bake its dynamic-int8 ONNX export at build time
# so container start doesn't hit HuggingFace or pay the export cost per boot.
COPY quantize.py .
RUN python -c "from sentence_transformers import CrossEncoder; CrossEncoder('$RERANKER_MODEL')" \
 && python quantize.py

COPY main.py .

EXPOSE 5002
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "5002"]
