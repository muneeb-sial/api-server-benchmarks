// pm2 cluster mode: one Node process per WORKERS, sharing the port.
// Run through pm2-runtime (no daemon) so every worker stays inside the
// runner's Job Object and is measured and memory-capped with it.
module.exports = {
  apps: [
    {
      name: 'bench',
      script: 'dist/main.js',
      exec_mode: 'cluster',
      instances: Number(process.env.WORKERS) || 'max',
      node_args: `--max-old-space-size=${process.env.WORKER_HEAP_MB || 1024}`,
      autorestart: false, // a crashed worker should show up as a crash, not be hidden
      merge_logs: true,
      env: { NODE_ENV: 'production' },
    },
  ],
};
