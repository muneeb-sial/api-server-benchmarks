# Linux image of the server for `bench run -docker`: uvicorn with WORKERS processes.
FROM python:3.10-slim-bookworm
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -q -r requirements.txt
COPY main.py ./
CMD exec python -m uvicorn main:app --host ${HOST} --port ${PORT} --workers ${WORKERS} \
    --http httptools --no-access-log --log-level warning
