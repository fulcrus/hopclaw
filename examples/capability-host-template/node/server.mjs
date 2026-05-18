import http from 'node:http'

const token = (process.env.SAMPLE_HOST_TOKEN || '').trim()

const server = http.createServer((req, res) => {
  if (req.method === 'GET' && req.url === '/healthz') {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify({ ok: true }))
    return
  }

  if (req.method !== 'POST' || req.url !== '/sample-host/v1') {
    res.statusCode = 404
    res.end('not found')
    return
  }

  if (token && (req.headers.authorization || '').trim() !== `Bearer ${token}`) {
    res.statusCode = 401
    res.end('unauthorized')
    return
  }

  let body = ''
  req.on('data', chunk => {
    body += chunk
  })
  req.on('end', () => {
    const payload = body ? JSON.parse(body) : {}
    const action = String(payload.action || '').trim()
    const response = { ok: true }

    if (action === 'create_session') {
      response.session_id = 'sess-demo-node-1'
      response.data = { session_id: 'sess-demo-node-1' }
    } else if (action === 'ping') {
      response.data = { message: 'pong', language: 'node' }
    } else {
      response.ok = false
      response.error = 'unsupported action'
    }

    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify(response))
  })
})

server.listen(18083, '127.0.0.1', () => {
  console.log('listening on http://127.0.0.1:18083/sample-host/v1')
})
