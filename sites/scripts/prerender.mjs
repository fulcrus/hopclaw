import { spawn, spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'node:net'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const rootDir = fileURLToPath(new URL('..', import.meta.url))
const distDir = path.join(rootDir, 'dist')
const routes = ['/', '/how-it-works', '/features', '/use-cases', '/docs', '/telemetry', '/clawhub']
const renderLang = process.env.HOPCLAW_PRERENDER_LANG || 'zh-CN'

if (process.env.HOPCLAW_PRERENDER_SKIP === '1') {
  console.log('[prerender] skipped via HOPCLAW_PRERENDER_SKIP=1')
  process.exit(0)
}

const chromeBin = resolveChromeBinary()
if (!chromeBin) {
  throw new Error(
    'No Chrome/Chromium binary found for prerender. Set HOPCLAW_PRERENDER_CHROME_BIN or HOPCLAW_PRERENDER_SKIP=1.',
  )
}

if (!canRun('python3')) {
  throw new Error('python3 is required for prerender but was not found in PATH.')
}

const shellHtml = await readFile(path.join(distDir, 'index.html'))
for (const route of routes) {
  if (route === '/') continue
  const shellPath = path.join(distDir, route.slice(1), 'index.html')
  await mkdir(path.dirname(shellPath), { recursive: true })
  await writeFile(shellPath, shellHtml)
}

const port = await reservePort()
const serverUrl = `http://127.0.0.1:${port}`
const server = spawn('python3', ['-m', 'http.server', String(port), '--bind', '127.0.0.1', '--directory', distDir], {
  cwd: rootDir,
  stdio: ['ignore', 'pipe', 'pipe'],
})

server.stdout.on('data', () => {})
server.stderr.on('data', (chunk) => {
  const text = String(chunk).trim()
  if (text) {
    console.warn(`[prerender] python server: ${text}`)
  }
})

try {
  await waitForServer(`${serverUrl}/`)
  console.log(`[prerender] using ${chromeBin}`)
  console.log(`[prerender] rendering ${routes.length} routes with lang=${renderLang}`)

  for (const route of routes) {
    console.log(`[prerender] rendering ${route}`)
    const html = dumpDom(chromeBin, `${serverUrl}${route}`)
    const filePath =
      route === '/' ? path.join(distDir, 'index.html') : path.join(distDir, route.slice(1), 'index.html')

    await mkdir(path.dirname(filePath), { recursive: true })
    await writeFile(filePath, html)
    console.log(`[prerender] wrote ${route}`)
  }
} finally {
  server.kill('SIGTERM')
}

function resolveChromeBinary() {
  const envCandidates = [process.env.HOPCLAW_PRERENDER_CHROME_BIN, process.env.CHROME_BIN].filter(Boolean)
  const fileCandidates = [
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/Applications/Chromium.app/Contents/MacOS/Chromium',
    '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
    '/usr/bin/google-chrome',
    '/usr/bin/google-chrome-stable',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
    '/snap/bin/chromium',
  ]
  const commandCandidates = [
    'google-chrome',
    'google-chrome-stable',
    'chromium',
    'chromium-browser',
    'chrome',
    'msedge',
    'microsoft-edge',
  ]

  for (const candidate of [...envCandidates, ...fileCandidates, ...commandCandidates]) {
    if (!candidate) continue
    if (!existsSync(candidate) && candidate.includes(path.sep)) continue
    if (canRun(candidate)) return candidate
  }

  return null
}

function canRun(command) {
  const result = spawnSync(command, ['--version'], {
    encoding: 'utf8',
    maxBuffer: 1024 * 1024,
  })
  return result.status === 0
}

function dumpDom(chromePath, url) {
  const args = [
    '--headless=new',
    '--disable-gpu',
    '--disable-dev-shm-usage',
    '--disable-extensions',
    '--disable-background-networking',
    '--hide-scrollbars',
    '--no-first-run',
    '--no-default-browser-check',
    '--no-sandbox',
    '--run-all-compositor-stages-before-draw',
    '--virtual-time-budget=5000',
    `--lang=${renderLang}`,
    '--dump-dom',
    url,
  ]

  let result = spawnSync(chromePath, args, {
    encoding: 'utf8',
    maxBuffer: 24 * 1024 * 1024,
  })

  if (result.status !== 0) {
    result = spawnSync(chromePath, ['--headless', ...args.slice(1)], {
      encoding: 'utf8',
      maxBuffer: 24 * 1024 * 1024,
    })
  }

  const html = result.stdout?.trim() || ''
  if (result.status !== 0 || !html.includes('data-v-app')) {
    const stderr = result.stderr?.trim() || 'unknown error'
    throw new Error(`Chrome prerender failed for ${url}: ${stderr}`)
  }

  return result.stdout
}

async function reservePort() {
  const probe = createServer()

  await new Promise((resolve, reject) => {
    probe.once('error', reject)
    probe.listen(0, '127.0.0.1', resolve)
  })

  const address = probe.address()
  const port = typeof address === 'object' && address ? address.port : null

  await new Promise((resolve, reject) => {
    probe.close((error) => {
      if (error) {
        reject(error)
        return
      }
      resolve()
    })
  })

  if (!port) {
    throw new Error('Unable to reserve a port for prerender server.')
  }

  return port
}

async function waitForServer(url) {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    try {
      const response = await fetch(url)
      if (response.ok) return
    } catch {}

    await sleep(200)
  }

  throw new Error(`Timed out waiting for prerender server at ${url}`)
}

function sleep(ms) {
  return new Promise((resolve) => {
    setTimeout(resolve, ms)
  })
}
