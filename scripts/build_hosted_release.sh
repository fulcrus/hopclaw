#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
version_input="${1:-${HOPCLAW_RELEASE_VERSION:-}}"
channel="${HOPCLAW_RELEASE_CHANNEL:-stable}"
website_url="${HOPCLAW_RELEASE_WEBSITE_URL:-https://hopclaw.com}"
out_dir="${HOPCLAW_RELEASE_OUT_DIR:-/tmp/hopclaw-release-site}"
published_at="${HOPCLAW_RELEASE_PUBLISHED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
git_commit="$(git -C "$repo_root" rev-parse --short=12 HEAD)"

if [ -z "$version_input" ]; then
  year="$(date -u +%Y)"
  month="$(date -u +%m)"
  day="$(date -u +%d)"
  month="${month#0}"
  day="${day#0}"
  [ -n "$month" ] || month="0"
  [ -n "$day" ] || day="0"
  version_input="$year.$month.$day"
fi

version="${version_input#v}"
download_dir="$out_dir/releases/download/$version"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT INT TERM

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  shasum -a 256 "$1" | awk '{print $1}'
}

binary_ldflags() {
  binary="$1"
  case "$binary" in
    hopclaw|openclaw)
      printf '%s' "-s -w -X github.com/fulcrus/hopclaw/internal/version.Version=$version -X github.com/fulcrus/hopclaw/internal/version.GitCommit=$git_commit -X github.com/fulcrus/hopclaw/internal/version.BuildDate=$published_at -X github.com/fulcrus/hopclaw/internal/version.Channel=$channel"
      ;;
    *)
      printf '%s' "-s -w -X main.version=$version"
      ;;
  esac
}

append_asset_json() {
  name="$1"
  os="$2"
  arch="$3"
  sha="$4"
  asset_url="$website_url/releases/download/$version/$name"
  if [ "${asset_first:-1}" = "1" ]; then
    asset_first=0
  else
    printf ',\n' >> "$manifest_path"
  fi
  printf '          {\n' >> "$manifest_path"
  printf '            "name": "%s",\n' "$name" >> "$manifest_path"
  printf '            "os": "%s",\n' "$os" >> "$manifest_path"
  printf '            "arch": "%s",\n' "$arch" >> "$manifest_path"
  printf '            "url": "%s",\n' "$asset_url" >> "$manifest_path"
  printf '            "sha256": "%s"\n' "$sha" >> "$manifest_path"
  printf '          }' >> "$manifest_path"
}

append_manifest_assets() {
  asset_first=1
  while read -r os arch; do
    [ -n "$os" ] || continue
    if [ "$os" = "windows" ]; then
      archive_name="hopclaw_${version}_${os}_${arch}.zip"
    else
      archive_name="hopclaw_${version}_${os}_${arch}.tar.gz"
    fi
    checksum="$(sha256_file "$download_dir/$archive_name")"
    append_asset_json "$archive_name" "$os" "$arch" "$checksum"
  done <<EOF
$targets
EOF
}

require_cmd go
require_cmd tar
require_cmd zip
require_cmd awk
require_cmd find
require_cmd sed
require_cmd git
require_cmd shasum

rm -rf "$out_dir"
mkdir -p "$download_dir"

printf '%s\n' "$version" > "$out_dir/releases/LATEST"
cp "$repo_root/scripts/install.sh" "$out_dir/install.sh"
cp "$repo_root/scripts/install.ps1" "$out_dir/install.ps1"
chmod 0755 "$out_dir/install.sh"

targets="
linux amd64
linux arm64
darwin amd64
darwin arm64
windows amd64
windows arm64
"

manifest_path="$out_dir/releases/manifest.json"

for binary_spec in \
  "hopclaw ./cmd/hopclaw" \
  "openclaw ./cmd/openclaw" \
  "hopclaw-browserd ./cmd/hopclaw-browserd" \
  "hopclaw-desktopd ./cmd/hopclaw-desktopd" \
  "hopclaw-gateway ./cmd/hopclaw-gateway"
do
  :
done

while read -r os arch; do
  [ -n "$os" ] || continue
  bundle_name="hopclaw_${version}_${os}_${arch}"
  bundle_dir="$work_dir/$bundle_name"
  mkdir -p "$bundle_dir"

  while read -r binary pkg; do
    [ -n "$binary" ] || continue
    output_name="$binary"
    if [ "$os" = "windows" ]; then
      output_name="${output_name}.exe"
    fi
    (
      cd "$repo_root"
      GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 \
        go build -trimpath -ldflags "$(binary_ldflags "$binary")" -o "$bundle_dir/$output_name" "$pkg"
    )
  done <<'EOF'
hopclaw ./cmd/hopclaw
openclaw ./cmd/openclaw
hopclaw-browserd ./cmd/hopclaw-browserd
hopclaw-desktopd ./cmd/hopclaw-desktopd
hopclaw-gateway ./cmd/hopclaw-gateway
EOF

  cp \
    "$repo_root/README.md" \
    "$repo_root/README.zh-CN.md" \
    "$repo_root/CHANGELOG.md" \
    "$repo_root/SECURITY.md" \
    "$repo_root/LICENSE" \
    "$repo_root/NOTICE" \
    "$bundle_dir/"

  if [ "$os" = "windows" ]; then
    archive_name="${bundle_name}.zip"
    (
      cd "$work_dir"
      zip -qr "$download_dir/$archive_name" "$bundle_name"
    )
  else
    archive_name="${bundle_name}.tar.gz"
    (
      cd "$work_dir"
      tar -czf "$download_dir/$archive_name" "$bundle_name"
    )
  fi

done <<EOF
$targets
EOF

(
  cd "$download_dir"
  : > checksums.txt
  for artifact in hopclaw_${version}_*; do
    [ -f "$artifact" ] || continue
    printf '%s *%s\n' "$(sha256_file "$artifact")" "$artifact" >> checksums.txt
  done
)

cat > "$manifest_path" <<EOF
{
  "product": "HopClaw",
  "channels": {
    "$channel": {
      "latest": "$version",
      "releases": [
        {
          "version": "$version",
          "channel": "$channel",
          "url": "$website_url/releases/",
          "notes": "Hosted release bundles for HopClaw.",
          "published_at": "$published_at",
          "assets": [
EOF
append_manifest_assets
printf '\n' >> "$manifest_path"
printf '          ]\n' >> "$manifest_path"
printf '        }\n' >> "$manifest_path"
printf '      ]\n' >> "$manifest_path"
printf '    }\n' >> "$manifest_path"
printf '  },\n' >> "$manifest_path"
printf '  "releases": [\n' >> "$manifest_path"
printf '    {\n' >> "$manifest_path"
printf '      "version": "%s",\n' "$version" >> "$manifest_path"
printf '      "channel": "%s",\n' "$channel" >> "$manifest_path"
printf '      "url": "%s",\n' "$website_url/releases/" >> "$manifest_path"
printf '      "notes": "Hosted release bundles for HopClaw.",\n' >> "$manifest_path"
printf '      "published_at": "%s",\n' "$published_at" >> "$manifest_path"
printf '      "assets": [\n' >> "$manifest_path"
append_manifest_assets
printf '\n' >> "$manifest_path"
printf '      ]\n' >> "$manifest_path"
printf '    }\n' >> "$manifest_path"
printf '  ]\n' >> "$manifest_path"
printf '}\n' >> "$manifest_path"

index_path="$out_dir/releases/index.html"
cat > "$index_path" <<EOF
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>HopClaw Releases</title>
    <style>
      :root {
        color-scheme: dark;
        --bg: #09090b;
        --panel: #111113;
        --text: #fafafa;
        --muted: #a1a1aa;
        --border: rgba(255, 255, 255, 0.08);
        --accent: #ff7a45;
      }

      * { box-sizing: border-box; }
      body {
        margin: 0;
        padding: 48px 24px 72px;
        background: var(--bg);
        color: var(--text);
        font: 16px/1.6 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      }

      main {
        width: min(960px, 100%);
        margin: 0 auto;
        display: grid;
        gap: 24px;
      }

      .panel {
        border: 1px solid var(--border);
        border-radius: 14px;
        background: var(--panel);
        padding: 24px;
      }

      h1, h2 {
        margin: 0 0 12px;
      }

      p {
        margin: 0;
        color: var(--muted);
      }

      code, pre {
        font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      }

      pre {
        margin: 0;
        padding: 16px;
        border-radius: 10px;
        background: rgba(0, 0, 0, 0.28);
        overflow-x: auto;
      }

      ul {
        margin: 0;
        padding-left: 20px;
      }

      a {
        color: var(--accent);
        text-decoration: none;
      }

      a:hover {
        text-decoration: underline;
      }

      .meta {
        display: grid;
        gap: 8px;
      }
    </style>
  </head>
  <body>
    <main>
      <section class="panel">
        <h1>HopClaw Releases</h1>
        <div class="meta">
          <p>Latest stable version: <strong>$version</strong></p>
          <p>Published at: <strong>$published_at</strong></p>
          <p>Install on macOS / Linux:</p>
          <pre><code>curl -fsSL $website_url/install.sh | sh</code></pre>
          <p>Install on Windows PowerShell:</p>
          <pre><code>irm $website_url/install.ps1 | iex</code></pre>
        </div>
      </section>

      <section class="panel">
        <h2>Artifacts</h2>
        <ul>
EOF

while read -r os arch; do
  [ -n "$os" ] || continue
  if [ "$os" = "windows" ]; then
    archive_name="hopclaw_${version}_${os}_${arch}.zip"
  else
    archive_name="hopclaw_${version}_${os}_${arch}.tar.gz"
  fi
  printf '          <li><a href="/releases/download/%s/%s">%s</a></li>\n' "$version" "$archive_name" "$archive_name" >> "$index_path"
done <<EOF
$targets
EOF

cat >> "$index_path" <<EOF
          <li><a href="/releases/download/$version/checksums.txt">checksums.txt</a></li>
          <li><a href="/releases/manifest.json">manifest.json</a></li>
        </ul>
      </section>
    </main>
  </body>
</html>
EOF

echo "Hosted release surface written to $out_dir"
echo "Version: $version"
echo "Manifest: $manifest_path"
