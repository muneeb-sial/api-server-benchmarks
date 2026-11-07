# Linux image of the server for `bench run -docker`: waitress with WORKERS threads, as on Windows.
# Docker's port proxy holds extra connections open, so lift waitress's default limit of 100 to
# keep it from refusing new connections at 100 VUs.
FROM python:3.10-slim-bookworm
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -q -r requirements.txt
COPY main.py ./
CMD exec python -m waitress --host=${HOST} --port=${PORT} --threads=${WORKERS} --connection-limit=1000 main:app
