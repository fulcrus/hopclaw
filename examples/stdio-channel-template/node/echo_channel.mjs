#!/usr/bin/env node

import readline from 'node:readline'

const JSONRPC = '2.0'
const PROTOCOL_VERSION = '2025-03-15'

function writeMessage(message) {
  process.stdout.write(`${JSON.stringify(message)}\n`)
}

function respondOK(id, result) {
  writeMessage({ jsonrpc: JSONRPC, id, result })
}

function notify(method, params) {
  writeMessage({ jsonrpc: JSONRPC, method, params })
}

const rl = readline.createInterface({
  input: process.stdin,
  crlfDelay: Infinity,
})

rl.on('line', line => {
  if (!line.trim()) return
  const msg = JSON.parse(line)

  switch (msg.method) {
    case 'initialize':
      respondOK(msg.id, {
        protocol_version: PROTOCOL_VERSION,
        plugin_name: 'sample-echo-channel-node',
        plugin_version: '0.1.0',
        capabilities: {
          send_text: true,
          send_rich_text: false,
          send_file: false,
          edit: false,
          delete: false,
          react: false,
          history: false,
        },
      })
      break
    case 'connect':
      respondOK(msg.id, { ok: true })
      notify('channel/status', { status: 'connected', message: 'node template connected' })
      break
    case 'disconnect':
      respondOK(msg.id, { ok: true })
      notify('channel/status', { status: 'disconnected', message: 'node template disconnected' })
      process.exit(0)
      break
    case 'send': {
      const params = msg.params || {}
      respondOK(msg.id, { ok: true, message_id: 'echo-msg-node-1' })
      notify('channel/inbound', {
        channel_id: params.channel_id || 'sample-echo',
        sender_id: params.target_id || 'unknown',
        sender_name: 'Echo Template Node',
        content: `Echo: ${params.content || ''}`,
        raw_event: {
          template: true,
          language: 'node',
        },
      })
      break
    }
    default:
      writeMessage({
        jsonrpc: JSONRPC,
        id: msg.id,
        error: { code: -32601, message: 'method not found' },
      })
  }
})
