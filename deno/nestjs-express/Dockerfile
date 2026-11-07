# Linux image of the server for `bench run -docker`: node:cluster with WORKERS processes (cluster.js).
# Workers do not inherit --v8-flags, so the per-worker heap cap goes through DENO_V8_FLAGS.
FROM denoland/deno:2.9.8
WORKDIR /app
COPY package.json deno.lock ./
RUN deno install
COPY tsconfig.json cluster.js ./
COPY src src
RUN deno task build
CMD exec env DENO_V8_FLAGS=--max-old-space-size=${WORKER_HEAP_MB} deno run -A --cached-only cluster.js dist/main.js
