import express from 'express';

const app = express();
app.get('/text', (req, res) => {
  res.type('text/plain').send('hello world');
});
app.get('/json', (req, res) => {
  res.json({ message: 'hello world' });
});

const port = Number(process.env.PORT || 3000);
app.listen(port, '127.0.0.1');
