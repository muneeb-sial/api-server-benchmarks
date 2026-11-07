# Linux image of the server for `bench run -docker`: deno serve --parallel, DENO_JOBS = WORKERS.
FROM denoland/deno:2.9.8
WORKDIR /app
COPY package.json deno.lock ./
RUN deno install
COPY main.ts ./
CMD exec deno serve -A --cached-only --parallel --host ${HOST} --port ${PORT} \
    --v8-flags=--max-old-space-size=${WORKER_HEAP_MB} main.ts
