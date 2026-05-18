import http from 'node:http'

const server = http.createServer((req, res) => {
  if (req.method !== 'POST' || req.url !== '/invoke') {
    res.statusCode = 404
    res.end('not found')
    return
  }

  let body = ''
  req.on('data', chunk => {
    body += chunk
  })
  req.on('end', () => {
    const payload = body ? JSON.parse(body) : {}
    const text = String((payload.input || {}).text || '')

    const response = {
      protocol_version: 'hopclaw.tool/v1',
      ok: true,
      status: 'success',
      summary: 'Echoed input text',
      content: `Echo: ${text}`,
      data: {
        echoed_text: text,
        tool_name: payload.tool_name,
      },
    }

    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify(response))
  })
})

server.listen(18082, '127.0.0.1', () => {
  console.log('listening on http://127.0.0.1:18082/invoke')
})
