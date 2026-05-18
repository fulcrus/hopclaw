#!/usr/bin/env node

const chunks = [];

process.stdin.on('data', (chunk) => {
  chunks.push(chunk);
});

process.stdin.on('end', () => {
  const raw = Buffer.concat(chunks).toString('utf8').trim();
  const payload = raw ? JSON.parse(raw) : {};
  const hookContext = payload._hook_context || {};
  const summary = {
    ok: true,
    language: 'node',
    event_type: payload.event_type || '',
    run_id: payload.run_id || '',
    phase: hookContext.phase || '',
    message: `Handled ${payload.event_type || 'unknown'} for run ${payload.run_id || 'unknown'}`,
  };
  process.stdout.write(`${JSON.stringify(summary, null, 2)}\n`);
});

process.stdin.resume();
