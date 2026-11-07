# Linux image of the server for `bench run -docker`: node:cluster with WORKERS processes (cluster.js).
FROM oven/bun:1.3-slim
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY tsconfig.json cluster.js ./
COPY src src
RUN bun run build
CMD ["bun", "cluster.js", "dist/main.js"]
