import Fastify from 'fastify';

const app = Fastify({ logger: false });
app.get('/text', (req, reply) => {
  reply.type('text/plain').send('hello world');
});
app.get('/json', () => ({ message: 'hello world' }));

const port = Number(process.env.PORT || 3000);
await app.listen({ port, host: process.env.HOST || '127.0.0.1' });
