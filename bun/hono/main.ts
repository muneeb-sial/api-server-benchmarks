import { Hono } from 'hono';

const app = new Hono();
app.get('/text', (c) => c.text('hello world'));
app.get('/json', (c) => c.json({ message: 'hello world' }));

Bun.serve({
  port: Number(process.env.PORT || 3000),
  hostname: process.env.HOST || '127.0.0.1',
  fetch: app.fetch,
});
