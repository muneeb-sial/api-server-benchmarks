// Docker only (Linux): runs WORKERS copies of the server with node:cluster.
// Deno's primary owns the listening socket and the workers accept from it.
// A worker that dies takes the server down, so a crash shows up as one, as with
// pm2's autorestart: false for Node.
import cluster from 'node:cluster';

const workers = Number(process.env.WORKERS) || 1;
if (cluster.isPrimary) {
  for (let i = 0; i < workers; i++) cluster.fork();
  cluster.on('exit', (worker, code, signal) => {
    console.error(`worker ${worker.process.pid} exited (${signal || code})`);
    process.exit(1);
  });
} else {
  await import(new URL(process.argv[2], import.meta.url).href);
}
