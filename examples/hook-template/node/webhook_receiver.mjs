#!/usr/bin/env node

import http from 'node:http';

const server = http.createServer((req, res) => {
  if (req.method !== 'POST') {
    res.statusCode = 405;
    res.end('method not allowed');
    return;
  }

  const chunks = [];
  req.on('data', (chunk) => {
    chunks.push(chunk);
  });
  req.on('end', () => {
    const raw = Buffer.concat(chunks).toString('utf8').trim();
    const payload = raw ? JSON.parse(raw) : {};
    console.log('=== HopClaw hook received ===');
    console.log('path:', req.url || '/');
    console.log('headers:', req.headers);
    console.log(JSON.stringify(payload, null, 2));
    res.statusCode = 204;
    res.end();
  });
});

server.listen(18084, '127.0.0.1', () => {
  console.log('Hook receiver listening on http://127.0.0.1:18084');
});
