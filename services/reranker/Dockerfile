FROM python:3.11-slim

WORKDIR /app

# Model is swappable at build time via --build-arg RERANKER_MODEL=... and at
# runtime via the RERANKER_MODEL env var (see main.py). The int8 export baked
# below only applies to the build-time model; a runtime-swapped model degrades
# to fp32 onnx unless the image is rebuilt with it.
ARG RERANKER_MODEL=BAAI/bge-reranker-v2-m3
ENV RERANKER_MODEL=${RERANKER_MODEL}

# GPU=1 installs the CUDA torch build instead of the CPU wheel (see
# requirements-gpu.txt); pair with the deploy/compose/docker-compose.gpu.yml override, which
# sets this from RERANKER_GPU=1 and reserves the host GPU.
ARG GPU=0
ARG BAKE_QUANTIZED=1
ARG TARGETARCH

COPY requirements.txt requirements-cpu.txt requirements-gpu.txt ./
RUN if [ "$GPU" = "1" ]; then \
        pip install --no-cache-dir -r requirements.txt -r requirements-gpu.txt; \
    else \
        pip install --no-cache-dir -r requirements.txt -r requirements-cpu.txt; \
    fi

# Pre-download the model so container start doesn't hit HuggingFace. An AVX2
# dynamic-int8 ONNX bake is optional and only runs for amd64 builds; portable
# CPU images use the torch backend by default.
COPY quantize.py .
RUN python -c "from sentence_transformers import CrossEncoder; CrossEncoder('$RERANKER_MODEL')" \
 && if [ "$BAKE_QUANTIZED" = "1" ] && [ "$TARGETARCH" = "amd64" ]; then \
        python quantize.py; \
    else \
        echo "Skipping AVX2 quantized bake (BAKE_QUANTIZED=$BAKE_QUANTIZED TARGETARCH=$TARGETARCH)"; \
    fi

COPY main.py .

EXPOSE 5002
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "5002"]
