# Linux image of the server for `bench run -docker`: pm2 cluster with WORKERS processes.
FROM node:22-bookworm-slim
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY main.js ecosystem.config.cjs ./
CMD ["node", "node_modules/pm2/bin/pm2-runtime", "start", "ecosystem.config.cjs"]
