FROM python:3.12-slim AS runtime

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends fonts-wqy-zenhei \
    && rm -rf /var/lib/apt/lists/*

COPY README.md /README.md
COPY backend/pyproject.toml backend/alembic.ini ./
COPY backend/alembic ./alembic
COPY backend/src ./src

RUN pip install --upgrade pip \
    && pip install .

RUN mkdir -p /app/storage/logs

EXPOSE 29280

CMD ["uvicorn", "productflow_backend.main:app", "--host", "0.0.0.0", "--port", "29280"]
