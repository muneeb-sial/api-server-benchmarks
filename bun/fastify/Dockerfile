# Linux image of the server for `bench run -docker`: node:cluster with WORKERS processes (cluster.js).
FROM oven/bun:1.3-slim
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY main.js cluster.js ./
RUN bun build main.js --target=bun --minify --outdir=dist
CMD ["bun", "cluster.js", "dist/main.js"]
