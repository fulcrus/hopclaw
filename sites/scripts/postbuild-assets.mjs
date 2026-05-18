import { chmod, copyFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const siteRoot = fileURLToPath(new URL('..', import.meta.url))
const repoRoot = fileURLToPath(new URL('../..', import.meta.url))
const distRoot = path.join(siteRoot, 'dist')

const assets = [
  { source: path.join(repoRoot, 'scripts', 'install.sh'), dest: path.join(distRoot, 'install.sh'), mode: 0o755 },
  { source: path.join(repoRoot, 'scripts', 'install.ps1'), dest: path.join(distRoot, 'install.ps1') },
]

for (const asset of assets) {
  await copyFile(asset.source, asset.dest)
  if (asset.mode) {
    await chmod(asset.dest, asset.mode)
  }
  console.log(`[postbuild] copied ${path.relative(repoRoot, asset.source)} -> ${path.relative(repoRoot, asset.dest)}`)
}
