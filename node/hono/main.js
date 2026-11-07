import { serve } from '@hono/node-server';
import { Hono } from 'hono';

const app = new Hono();
app.get('/text', (c) => c.text('hello world'));
app.get('/json', (c) => c.json({ message: 'hello world' }));

const port = Number(process.env.PORT || 3000);
serve({ fetch: app.fetch, port, hostname: process.env.HOST || '127.0.0.1' });
