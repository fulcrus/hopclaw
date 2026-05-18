#!/bin/sh

set -eu

binaries_input="${HOPCLAW_INSTALL_BINARY:-hopclaw}"
install_dir_input="${HOPCLAW_INSTALL_DIR:-}"
version_input="${HOPCLAW_INSTALL_VERSION:-latest}"
repo_override="${HOPCLAW_INSTALL_REPO:-}"
base_url_input="${HOPCLAW_INSTALL_BASE_URL:-}"
run_onboard_input="${HOPCLAW_INSTALL_RUN_ONBOARD:-}"
onboard_mode_input="${HOPCLAW_INSTALL_ONBOARD_MODE:-}"
install_lang_input="${HOPCLAW_INSTALL_LANG:-}"
default_github_repo="hopclaw/hopclaw"

supported_binaries="hopclaw openclaw hopclaw-browserd hopclaw-desktopd hopclaw-gateway"
install_dir=""
install_lang="en"

log_step() {
  printf '==> %s\n' "$1" >&2
}

log_detail() {
  printf '   %s\n' "$1" >&2
}

usage() {
  cat <<'EOF'
Install a HopClaw release binary from GitHub releases.

Environment variables:
  HOPCLAW_INSTALL_BINARY   Binary or comma-separated binaries to install.
                           Supported: hopclaw, openclaw, hopclaw-browserd,
                           hopclaw-desktopd, hopclaw-gateway, or "all"
  HOPCLAW_INSTALL_DIR      Destination directory. Default: auto
                           (/usr/local/bin when writable, otherwise ~/.local/bin)
  HOPCLAW_INSTALL_VERSION  Release tag or "latest". Default: latest
  HOPCLAW_INSTALL_REPO     Override the GitHub repo slug, e.g. hopclaw/hopclaw
                           Default: hopclaw/hopclaw
  HOPCLAW_INSTALL_BASE_URL Optional hosted release base URL override
  HOPCLAW_INSTALL_LANG     Installer/onboarding language: en or zh
  HOPCLAW_INSTALL_RUN_ONBOARD
                           When set to 1/true/yes, run
                           "hopclaw onboard" after installing a CLI binary
  HOPCLAW_INSTALL_ONBOARD_MODE
    Onboarding handoff mode when
    HOPCLAW_INSTALL_RUN_ONBOARD is set:
    interactive or web-first (default)

Examples:
  curl -fsSL https://get.hopclaw.ai | sh
  curl -fsSL https://get.hopclaw.ai | HOPCLAW_INSTALL_LANG=zh sh
  curl -fsSL https://get.hopclaw.ai | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh
  curl -fsSL https://get.hopclaw.ai | HOPCLAW_INSTALL_RUN_ONBOARD=1 HOPCLAW_INSTALL_ONBOARD_MODE=web-first sh
  HOPCLAW_INSTALL_BASE_URL=https://mirror.example.com/releases sh ./scripts/install.sh
  HOPCLAW_INSTALL_BINARY=all sh ./scripts/install.sh
  HOPCLAW_INSTALL_VERSION=2026.3.17 sh ./scripts/install.sh
EOF
}

normalize_install_lang() {
  raw="$(printf '%s\n' "$1" | tr '[:upper:]' '[:lower:]' | sed 's/_/-/g;s/^[[:space:]]*//;s/[[:space:]]*$//')"
  case "$raw" in
    zh|zh-*|cn|chinese)
      printf '%s\n' "zh"
      return 0
      ;;
    en|en-*|english)
      printf '%s\n' "en"
      return 0
      ;;
  esac
  return 1
}

default_install_lang() {
  locale_hint="${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}"
  if normalized="$(normalize_install_lang "$locale_hint" 2>/dev/null)"; then
    printf '%s\n' "$normalized"
    return
  fi
  printf '%s\n' "en"
}

resolve_prompt_tty() {
  if command -v tty >/dev/null 2>&1; then
    tty_path="$(tty 2>/dev/null || true)"
    if [ -n "$tty_path" ] && [ "$tty_path" != "not a tty" ] && tty_is_usable "$tty_path"; then
      printf '%s\n' "$tty_path"
      return
    fi
  fi
  if tty_is_usable /dev/tty; then
    printf '%s\n' "/dev/tty"
  fi
}

tty_is_usable() {
  tty_candidate="$1"
  if [ -z "$tty_candidate" ]; then
    return 1
  fi
  if [ ! -r "$tty_candidate" ] || [ ! -w "$tty_candidate" ]; then
    return 1
  fi
  if ! ( : <"$tty_candidate" ) >/dev/null 2>&1; then
    return 1
  fi
  if ! ( : >"$tty_candidate" ) >/dev/null 2>&1; then
    return 1
  fi
  return 0
}

lang_is_zh() {
  [ "$install_lang" = "zh" ]
}

prompt_install_lang() {
  if normalized="$(normalize_install_lang "$install_lang_input" 2>/dev/null)"; then
    install_lang="$normalized"
    export HOPCLAW_INSTALL_LANG="$install_lang"
    return
  fi

  default_lang="$(default_install_lang)"
  tty_path="$(resolve_prompt_tty)"
  if [ -z "$tty_path" ]; then
    install_lang="$default_lang"
    export HOPCLAW_INSTALL_LANG="$install_lang"
    return
  fi

  {
    printf '\nChoose installer language / 选择安装语言\n'
    if [ "$default_lang" = "zh" ]; then
      printf '  1) 中文（默认）\n'
      printf '  2) English\n'
      printf '> '
    else
      printf '  1) 中文\n'
      printf '  2) English (default)\n'
      printf '> '
    fi
  } >"$tty_path"

  choice=""
  IFS= read -r choice <"$tty_path" || true
  case "$(printf '%s\n' "$choice" | tr '[:upper:]' '[:lower:]' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')" in
    "" )
      install_lang="$default_lang"
      ;;
    1|zh|cn|中文)
      install_lang="zh"
      ;;
    2|en|english)
      install_lang="en"
      ;;
    * )
      install_lang="$default_lang"
      ;;
  esac
  export HOPCLAW_INSTALL_LANG="$install_lang"
}

text_step_starting_installer() {
  if lang_is_zh; then
    printf '%s\n' "开始安装 HopClaw"
  else
    printf '%s\n' "Starting HopClaw installer"
  fi
}

text_detail_target() {
  if lang_is_zh; then
    printf '目标：%s\n' "$1"
  else
    printf 'target: %s\n' "$1"
  fi
}

text_detail_install_dir() {
  if lang_is_zh; then
    printf '安装目录：%s\n' "$1"
  else
    printf 'install dir: %s\n' "$1"
  fi
}

text_detail_source() {
  if lang_is_zh; then
    printf '来源：%s\n' "$1"
  else
    printf 'source: %s\n' "$1"
  fi
}

text_step_resolve_latest_github() {
  if lang_is_zh; then
    printf '解析 %s 的最新 GitHub 版本\n' "$1"
  else
    printf 'Resolving latest GitHub release for %s\n' "$1"
  fi
}

text_step_resolve_latest_hosted() {
  if lang_is_zh; then
    printf '%s\n' "解析最新托管版本"
  else
    printf '%s\n' "Resolving latest hosted release version"
  fi
}

text_step_downloading() {
  if lang_is_zh; then
    printf '下载 %s\n' "$1"
  else
    printf 'Downloading %s\n' "$1"
  fi
}

text_step_verifying() {
  if lang_is_zh; then
    printf '校验 %s 的校验和\n' "$1"
  else
    printf 'Verifying checksum for %s\n' "$1"
  fi
}

text_step_extracting() {
  if lang_is_zh; then
    printf '解压 %s\n' "$1"
  else
    printf 'Extracting %s\n' "$1"
  fi
}

text_step_installing_binaries() {
  if lang_is_zh; then
    printf '安装二进制到 %s\n' "$1"
  else
    printf 'Installing binaries into %s\n' "$1"
  fi
}

text_step_finalizing() {
  if lang_is_zh; then
    printf '%s\n' "完成安装收尾"
  else
    printf '%s\n' "Finalizing install"
  fi
}

text_installed_binary() {
  if lang_is_zh; then
    printf '已安装 %s（来源 %s）到 %s\n' "$1" "$2" "$3"
  else
    printf 'installed %s from %s to %s\n' "$1" "$2" "$3"
  fi
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

http_get() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO- "$1"
    return
  fi
  echo "either curl or wget is required" >&2
  exit 1
}

http_download() {
  url="$1"
  path="$2"
  mode="${3:-quiet}"
  if command -v curl >/dev/null 2>&1; then
    if [ "$mode" = "progress" ] && [ -t 2 ]; then
      curl -fL --progress-bar "$url" -o "$path"
    else
      curl -fsSL "$url" -o "$path"
    fi
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    if [ "$mode" = "progress" ] && [ -t 2 ]; then
      wget -O "$path" "$url"
    else
      wget -qO "$path" "$url"
    fi
    return
  fi
  echo "either curl or wget is required" >&2
  exit 1
}

resolve_base_url() {
  value="$base_url_input"
  if [ -z "$value" ]; then
    value="https://hopclaw.com/releases"
  fi
  printf '%s\n' "$value" | sed 's:/*$::'
}

resolve_latest_hosted_version() {
  base_url="$1"
  latest="$(http_get "$base_url/LATEST" | tr -d '\r' | sed -n '1{s/[[:space:]]*$//;p;}')"
  latest="${latest#v}"
  if [ -z "$latest" ]; then
    echo "failed to resolve the latest hosted release version from $base_url/LATEST" >&2
    exit 1
  fi
  printf '%s\n' "$latest"
}

default_install_dir() {
  if [ -n "$install_dir_input" ]; then
    printf '%s\n' "$install_dir_input"
    return
  fi
  if [ "$os" = "windows" ]; then
    if [ -n "${HOME:-}" ]; then
      printf '%s\n' "$HOME/.local/bin"
      return
    fi
    printf '%s\n' "/usr/local/bin"
    return
  fi
  if [ -d /usr/local/bin ] || [ ! -e /usr/local/bin ]; then
    if [ -w /usr/local/bin ]; then
      printf '%s\n' "/usr/local/bin"
      return
    fi
  fi
  if [ -n "${HOME:-}" ]; then
    printf '%s\n' "$HOME/.local/bin"
    return
  fi
  printf '%s\n' "/usr/local/bin"
}

detect_os() {
  raw_os="$(uname -s 2>/dev/null || printf '%s' "${OS:-}")"
  case "$raw_os" in
    Linux) printf '%s\n' "linux" ;;
    Darwin) printf '%s\n' "darwin" ;;
    CYGWIN*|MINGW*|MSYS*|Windows_NT) printf '%s\n' "windows" ;;
    *)
      echo "unsupported operating system: $raw_os" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  raw_arch="$(uname -m 2>/dev/null || printf '%s' "${PROCESSOR_ARCHITECTURE:-}")"
  case "$raw_arch" in
    x86_64|amd64) printf '%s\n' "amd64" ;;
    arm64|aarch64) printf '%s\n' "arm64" ;;
    *)
      echo "unsupported architecture: $raw_arch" >&2
      exit 1
      ;;
  esac
}

validate_binary_name() {
  case "$1" in
    hopclaw|openclaw|hopclaw-browserd|hopclaw-desktopd|hopclaw-gateway) ;;
    *)
      echo "unsupported binary: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
}

resolve_binaries() {
  case "$binaries_input" in
    -h|--help)
      usage
      exit 0
      ;;
  esac

  raw="$(printf '%s' "$binaries_input" | tr ',\n\t' '   ')"
  raw="$(printf '%s\n' "$raw" | sed 's/[[:space:]][[:space:]]*/ /g; s/^ //; s/ $//')"
  if [ -z "$raw" ]; then
    echo "HOPCLAW_INSTALL_BINARY cannot be empty" >&2
    exit 1
  fi

  if [ "$raw" = "all" ]; then
    printf '%s\n' $supported_binaries
    return
  fi

  resolved=""
  for item in $raw; do
    validate_binary_name "$item"
    case " $resolved " in
      *" $item "*) ;;
      *) resolved="$resolved $item" ;;
    esac
  done
  printf '%s\n' "$resolved" | tr ' ' '\n' | sed '/^$/d'
}

resolve_latest_tag() {
  repo="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest" | sed 's#.*/##'
    return
  fi
  html="$(http_get "https://github.com/$repo/releases/latest")"
  printf '%s\n' "$html" | sed -n 's#.*releases/tag/\([^"]*\).*#\1#p' | head -n 1
}

install_file() {
  src="$1"
  dst="$2"

  if mkdir -p "$install_dir" 2>/dev/null && install -m 0755 "$src" "$dst" 2>/dev/null; then
    return
  fi
  if command -v sudo >/dev/null 2>&1; then
    sudo mkdir -p "$install_dir"
    sudo install -m 0755 "$src" "$dst"
    return
  fi
  echo "cannot write to $install_dir and sudo is not available" >&2
  exit 1
}

path_has_dir() {
  case ":${PATH:-}:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

calc_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  return 1
}

verify_archive_checksum() {
  checksums_url="$1"
  archive="$2"
  archive_path="$3"
  checksums_path="$tmpdir/checksums.txt"

  if ! http_download "$checksums_url" "$checksums_path" 2>/dev/null; then
    echo "warning: checksum manifest not found at $checksums_url; continuing without verification" >&2
    return
  fi

  expected="$(awk -v name="$archive" '
    {
      file=$2
      sub(/^\*/, "", file)
      if (file == name) {
        print $1
        exit
      }
    }
  ' "$checksums_path")"
  if [ -z "$expected" ]; then
    echo "warning: checksum for $archive not found; continuing without verification" >&2
    return
  fi

  actual="$(calc_sha256 "$archive_path" 2>/dev/null || true)"
  if [ -z "$actual" ]; then
    echo "warning: sha256sum/shasum not found; skipping checksum verification" >&2
    return
  fi

  if [ "$actual" != "$expected" ]; then
    echo "checksum verification failed for $archive" >&2
    exit 1
  fi
}

is_truthy() {
  case "$1" in
    1|y|Y|yes|YES|true|TRUE|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

resolve_onboard_mode() {
  mode="$(printf '%s\n' "$onboard_mode_input" | tr '[:upper:]' '[:lower:]' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  if [ -z "$mode" ]; then
    mode="web-first"
  fi
  case "$mode" in
    interactive|web-first)
      printf '%s\n' "$mode"
      ;;
    *)
      echo "unsupported HOPCLAW_INSTALL_ONBOARD_MODE: $onboard_mode_input (use interactive or web-first)" >&2
      exit 1
      ;;
  esac
}

resolve_onboard_tty() {
  resolve_prompt_tty
}

archive_name_for() {
  version="$1"
  os_name="$2"
  arch_name="$3"
  extension="tar.gz"
  if [ "$os_name" = "windows" ]; then
    extension="zip"
  fi
  printf 'hopclaw_%s_%s_%s.%s\n' "$version" "$os_name" "$arch_name" "$extension"
}

binary_path_name() {
  binary="$1"
  if [ "$os" = "windows" ]; then
    printf '%s.exe\n' "$binary"
    return
  fi
  printf '%s\n' "$binary"
}

extract_archive() {
  archive_path="$1"
  case "$archive_path" in
    *.zip)
      need_cmd unzip
      unzip -q "$archive_path" -d "$tmpdir"
      ;;
    *.tar.gz)
      tar -xzf "$archive_path" -C "$tmpdir"
      ;;
    *)
      echo "unsupported archive format: $archive_path" >&2
      exit 1
      ;;
  esac
}

verify_cli_installation() {
  cli_path="$1"
  cli_binary="$2"

  if [ -z "$cli_path" ] || [ -z "$cli_binary" ]; then
    return
  fi

  if lang_is_zh; then
    log_step "验证 $cli_binary version"
  else
    log_step "Verifying $cli_binary version"
  fi
  if ! "$cli_path" version >/dev/null 2>&1; then
    echo "failed to verify $cli_binary installation with '$cli_path version'" >&2
    exit 1
  fi
}

print_next_steps() {
  cli_binary="$1"
  if [ -z "$cli_binary" ]; then
    return
  fi

  echo
  if lang_is_zh; then
    echo "接下来可以这样做："
    echo "  $cli_binary onboard     # 继续安装向导"
    echo "  $cli_binary dashboard   # 查看本地控制台地址"
    echo "  $cli_binary update      # 检查新版本"
  else
    echo "next steps:"
    echo "  $cli_binary onboard     # guided first-time setup"
    echo "  $cli_binary dashboard   # open the local dashboard"
    echo "  $cli_binary update      # check for newer releases"
  fi

  if ! path_has_dir "$install_dir"; then
    echo
    if lang_is_zh; then
      echo "注意：$install_dir 还不在你的 PATH 里"
      echo "可以运行下面这行："
      echo "  export PATH=\"$install_dir:\$PATH\""
    else
      echo "note: $install_dir is not on your PATH"
      echo "add it with:"
      echo "  export PATH=\"$install_dir:\$PATH\""
    fi
  fi
}

prompt_run_onboard() {
  cli_binary="$1"
  tty_path="$(resolve_onboard_tty)"
  if [ -z "$tty_path" ]; then
    return 1
  fi

  if lang_is_zh; then
    printf '\n现在运行 %s onboard（web-first）吗？ [Y/n] ' "$cli_binary" >"$tty_path"
  else
    printf '\nRun %s onboard now (web-first)? [Y/n] ' "$cli_binary" >"$tty_path"
  fi

  choice=""
  IFS= read -r choice <"$tty_path" || true
  normalized="$(printf '%s\n' "$choice" | tr '[:upper:]' '[:lower:]' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  case "$normalized" in
    ""|y|yes)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

maybe_run_onboard() {
  cli_path="$1"
  cli_binary="$2"

  if [ -z "$cli_path" ] || [ -z "$cli_binary" ]; then
    if is_truthy "$run_onboard_input"; then
      echo "warning: HOPCLAW_INSTALL_RUN_ONBOARD was set, but no CLI binary was installed" >&2
    fi
    return
  fi

  should_run=1
  if [ -n "$run_onboard_input" ]; then
    if ! is_truthy "$run_onboard_input"; then
      should_run=0
    fi
  elif ! prompt_run_onboard "$cli_binary"; then
    should_run=0
  fi

  if [ "$should_run" -ne 1 ]; then
    return
  fi

  onboard_mode="$(resolve_onboard_mode)"

  echo
  case "$onboard_mode" in
    interactive)
      if lang_is_zh; then
        log_step "启动 $cli_binary onboard"
        echo "正在启动 $cli_binary onboard..."
      else
        log_step "Launching $cli_binary onboard"
        echo "launching $cli_binary onboard..."
      fi
      tty_path="$(resolve_onboard_tty)"
      if [ -n "$tty_path" ]; then
        HOPCLAW_INSTALL_LANG="$install_lang" "$cli_path" onboard <"$tty_path"
        return
      fi
      if [ -t 0 ]; then
        HOPCLAW_INSTALL_LANG="$install_lang" "$cli_path" onboard
        return
      fi
      if lang_is_zh; then
        echo "未检测到可用交互终端，改为启动 web-first onboarding。" >&2
      else
        echo "no interactive terminal detected; falling back to web-first onboarding." >&2
      fi
      HOPCLAW_INSTALL_LANG="$install_lang" "$cli_path" onboard --web-first
      return
      ;;
    web-first)
      if lang_is_zh; then
        log_step "启动 $cli_binary onboard --web-first"
        echo "正在启动 $cli_binary onboard --web-first..."
      else
        log_step "Launching $cli_binary onboard --web-first"
        echo "launching $cli_binary onboard --web-first..."
      fi
      HOPCLAW_INSTALL_LANG="$install_lang" "$cli_path" onboard --web-first
      ;;
  esac
}

case "$binaries_input" in
  -h|--help)
    usage
    exit 0
    ;;
esac

prompt_install_lang

binaries="$(resolve_binaries)"
need_cmd uname
need_cmd mktemp
need_cmd sed
need_cmd find
need_cmd install
need_cmd awk

os="$(detect_os)"
arch="$(detect_arch)"
if [ "$os" != "windows" ]; then
  need_cmd tar
fi
install_dir="$(default_install_dir)"
base_url=""
if [ -n "$base_url_input" ]; then
  base_url="$(resolve_base_url)"
fi
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

log_step "$(text_step_starting_installer)"
log_detail "$(text_detail_target "$os/$arch")"
log_detail "$(text_detail_install_dir "$install_dir")"

archive_path=""
tag=""
install_source_desc=""
repo="$repo_override"
if [ -z "$repo" ]; then
  repo="$default_github_repo"
fi
if [ -z "$base_url" ]; then
  log_detail "$(text_detail_source "github.com/$repo/releases")"
  tag_candidates=""
  if [ "$version_input" = "latest" ]; then
    log_step "$(text_step_resolve_latest_github "$repo")"
    latest_tag="$(resolve_latest_tag "$repo")"
    if [ -z "$latest_tag" ]; then
      echo "failed to resolve the latest release tag for $repo" >&2
      exit 1
    fi
    tag_candidates="$latest_tag"
  else
    tag_candidates="$version_input"
    case "$version_input" in
      v*) ;;
      *) tag_candidates="$tag_candidates v$version_input" ;;
    esac
  fi

  for candidate in $tag_candidates; do
    version="${candidate#v}"
    archive="$(archive_name_for "$version" "$os" "$arch")"
    url="https://github.com/$repo/releases/download/$candidate/$archive"
    log_step "$(text_step_downloading "$archive")"
    if http_download "$url" "$tmpdir/$archive" progress; then
      archive_path="$tmpdir/$archive"
      tag="$candidate"
      install_source_desc="$repo release $tag"
      break
    fi
  done

  if [ -z "$archive_path" ] || [ -z "$tag" ]; then
    echo "failed to download a matching release archive for $repo ($os/$arch)" >&2
    exit 1
  fi

  log_step "$(text_step_verifying "$archive")"
  verify_archive_checksum "https://github.com/$repo/releases/download/$tag/checksums.txt" "$archive" "$archive_path"
else
  log_detail "$(text_detail_source "$base_url")"
  if [ "$version_input" = "latest" ]; then
    log_step "$(text_step_resolve_latest_hosted)"
    version="$(resolve_latest_hosted_version "$base_url")"
  else
    version="${version_input#v}"
  fi
  archive="$(archive_name_for "$version" "$os" "$arch")"
  archive_path="$tmpdir/$archive"
  url="$base_url/download/$version/$archive"
  log_step "$(text_step_downloading "$archive")"
  if ! http_download "$url" "$archive_path" progress; then
    echo "failed to download a matching release archive from $base_url ($os/$arch, version $version)" >&2
    exit 1
  fi
  tag="$version"
  install_source_desc="$base_url release $tag"
  log_step "$(text_step_verifying "$archive")"
  verify_archive_checksum "$base_url/download/$version/checksums.txt" "$archive" "$archive_path"
fi

log_step "$(text_step_extracting "$archive")"
extract_archive "$archive_path"

primary_cli_binary=""
primary_cli_path=""
log_step "$(text_step_installing_binaries "$install_dir")"
for binary in $binaries; do
  archive_binary="$(binary_path_name "$binary")"
  source_path="$(find "$tmpdir" -type f -name "$archive_binary" | head -n 1)"
  if [ -z "$source_path" ]; then
    echo "binary $binary was not found in the downloaded archive" >&2
    exit 1
  fi

  destination="$install_dir/$archive_binary"
  install_file "$source_path" "$destination"
  text_installed_binary "$binary" "$install_source_desc" "$destination"

  case "$binary" in
    hopclaw|openclaw)
      if [ -z "$primary_cli_binary" ]; then
        primary_cli_binary="$binary"
        primary_cli_path="$destination"
      fi
      ;;
  esac
done

log_step "$(text_step_finalizing)"
verify_cli_installation "$primary_cli_path" "$primary_cli_binary"
print_next_steps "$primary_cli_binary"
maybe_run_onboard "$primary_cli_path" "$primary_cli_binary"
