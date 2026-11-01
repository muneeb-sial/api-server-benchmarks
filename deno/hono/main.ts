import { Hono } from 'hono';

const app = new Hono();
app.get('/text', (c) => c.text('hello world'));
app.get('/json', (c) => c.json({ message: 'hello world' }));

// Served by `deno serve`, which reads the default export's fetch handler.
export default app;
