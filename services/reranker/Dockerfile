FROM python:3.11-slim

WORKDIR /app

# Model is swappable at build time via --build-arg RERANKER_MODEL=... and at
# runtime via the RERANKER_MODEL env var (see main.py). The int8 export baked
# below only applies to the build-time model; a runtime-swapped model degrades
# to fp32 onnx unless the image is rebuilt with it.
ARG RERANKER_MODEL=BAAI/bge-reranker-v2-m3
ENV RERANKER_MODEL=${RERANKER_MODEL}

# GPU=1 installs the CUDA torch build instead of the CPU wheel (see
# requirements-gpu.txt); pair with the docker-compose.gpu.yml override, which
# sets this from RERANKER_GPU=1 and reserves the host GPU.
ARG GPU=0

COPY requirements.txt requirements-cpu.txt requirements-gpu.txt ./
RUN if [ "$GPU" = "1" ]; then \
        pip install --no-cache-dir -r requirements.txt -r requirements-gpu.txt; \
    else \
        pip install --no-cache-dir -r requirements.txt -r requirements-cpu.txt; \
    fi

# Pre-download the model and bake its dynamic-int8 ONNX export at build time
# so container start doesn't hit HuggingFace or pay the export cost per boot.
# The bake runs on CPU even in GPU images: the int8 artifact stays available
# for RERANKER_DEVICE=cpu on that image.
COPY quantize.py .
RUN python -c "from sentence_transformers import CrossEncoder; CrossEncoder('$RERANKER_MODEL')" \
 && python quantize.py

COPY main.py .

EXPOSE 5002
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "5002"]
