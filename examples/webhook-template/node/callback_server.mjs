import http from 'node:http'

const server = http.createServer((req, res) => {
  if (req.method !== 'POST' || req.url !== '/callback') {
    res.statusCode = 404
    res.end('not found')
    return
  }

  let body = ''
  req.on('data', chunk => {
    body += chunk
  })
  req.on('end', () => {
    let payload = body
    try {
      payload = body ? JSON.parse(body) : null
    } catch {}

    console.log('=== HopClaw callback received ===')
    console.log('path:', req.url)
    console.log('headers:', req.headers)
    console.log('payload:', typeof payload === 'string' ? payload : JSON.stringify(payload, null, 2))

    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ ok: true }))
  })
})

server.listen(18081, '127.0.0.1', () => {
  console.log('listening on http://127.0.0.1:18081/callback')
})
