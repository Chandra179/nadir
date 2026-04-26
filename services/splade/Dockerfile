FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Pre-download model at build time so container startup is fast
RUN python -c "from fastembed import SparseTextEmbedding; SparseTextEmbedding(model_name='prithivida/Splade_PP_en_v1')"

COPY main.py .

EXPOSE 5001
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "5001"]
