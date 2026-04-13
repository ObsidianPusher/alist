#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ ! -f "$ROOT_DIR/go.mod" ] && [ -f "$ROOT_DIR/../go.mod" ]; then
  ROOT_DIR="$(cd "$ROOT_DIR/.." && pwd)"
fi
cd "$ROOT_DIR"

OUT_PATH="${1:-build/alist-linux-musl-arm64}"
TOOLCHAIN_DIR="${TOOLCHAIN_DIR:-$ROOT_DIR/build/musl-toolchain}"
CACHE_DIR="${CACHE_DIR:-$ROOT_DIR/build/cache}"
MUSL_MIRROR="${MUSL_MIRROR:-https://musl.cc/}"
MUSL_MIRRORS="${MUSL_MIRRORS:-https://more.musl.cc/x86_64-linux-musl/ https://musl.cc/}"
TOOLCHAIN_URL="${TOOLCHAIN_URL:-}"
GITHUB_RELEASE_URL="${GITHUB_RELEASE_URL:-}"
GITHUB_REPO="${GITHUB_REPO:-}"
GITHUB_TAG="${GITHUB_TAG:-}"
GITHUB_ASSET="${GITHUB_ASSET:-aarch64-linux-musl-cross.tgz}"
GITHUB_PROXY="${GITHUB_PROXY:-}"
TOOLCHAIN_TARBALL="${TOOLCHAIN_TARBALL:-$CACHE_DIR/aarch64-linux-musl-cross.tgz}"
ALLOW_DOWNLOAD="${ALLOW_DOWNLOAD:-1}"
USE_ZIG="${USE_ZIG:-0}"
ZIG_BIN="${ZIG_BIN:-zig}"
ZIG_TARBALL="${ZIG_TARBALL:-$CACHE_DIR/zig-macos-aarch64-0.13.0.tar.xz}"
ZIG_DIR="${ZIG_DIR:-$ROOT_DIR/build/zig}"
ZIG_VERSION="${ZIG_VERSION:-0.13.0}"
ZIG_URL="${ZIG_URL:-}"
DIST_TARBALL="${DIST_TARBALL:-$CACHE_DIR/dist.tar.gz}"
DIST_URL="${DIST_URL:-https://github.com/alist-org/alist-web/releases/latest/download/dist.tar.gz}"
DIST_MIRRORS="${DIST_MIRRORS:-https://mirror.ghproxy.com/ https://ghproxy.com/}"
WEB_DIR="${WEB_DIR:-$ROOT_DIR/web}"
WEB_BUILD="${WEB_BUILD:-1}"
WEB_BUILD_FALLBACK="${WEB_BUILD_FALLBACK:-0}"
FORCE_WEB_REBUILD="${FORCE_WEB_REBUILD:-1}"
REQUIRE_ZH="${REQUIRE_ZH:-1}"
ZH_CODE="${ZH_CODE:-zh-CN}"
CN_MIRROR="${CN_MIRROR:-1}"
NPM_REGISTRY="${NPM_REGISTRY:-}"
SOLID_ROUTER_DIR="${SOLID_ROUTER_DIR:-$WEB_DIR/solid-router}"
SOLID_ROUTER_URL="${SOLID_ROUTER_URL:-}"
SOLID_ROUTER_MIRROR="${SOLID_ROUTER_MIRROR:-}"
SOLID_ROUTER_TARBALL="${SOLID_ROUTER_TARBALL:-$CACHE_DIR/solid-router.tar.gz}"
SOLID_ROUTER_REF="${SOLID_ROUTER_REF:-main}"
SOLID_ROUTER_TARBALL_URL="${SOLID_ROUTER_TARBALL_URL:-https://files.m.daocloud.io/github.com/alist-org/solid-router/archive/refs/heads/${SOLID_ROUTER_REF}.tar.gz}"
SOLID_ROUTER_MIRRORS="${SOLID_ROUTER_MIRRORS:-https://files.m.daocloud.io/ https://mirror.ghproxy.com/ https://ghproxy.com/}"

if [ "$CN_MIRROR" = "1" ] && [ -z "$NPM_REGISTRY" ]; then
  NPM_REGISTRY="https://registry.npmmirror.com"
fi

ensure_toolchain() {
  if command -v aarch64-linux-musl-gcc >/dev/null 2>&1; then
    return
  fi
  mkdir -p "$TOOLCHAIN_DIR"
  mkdir -p "$CACHE_DIR"
  if [ ! -x "$TOOLCHAIN_DIR/bin/aarch64-linux-musl-gcc" ]; then
    if [ -f "$TOOLCHAIN_TARBALL" ]; then
      if ! tar tzf "$TOOLCHAIN_TARBALL" >/dev/null 2>&1; then
        echo "Cached toolchain is corrupted, removing: $TOOLCHAIN_TARBALL"
        rm -f "$TOOLCHAIN_TARBALL"
        if [ "$ALLOW_DOWNLOAD" != "1" ]; then
          echo "Cache-only mode is enabled. Please re-download and place it at: $TOOLCHAIN_TARBALL"
          exit 1
        fi
      fi
    fi
    if [ ! -f "$TOOLCHAIN_TARBALL" ]; then
      echo "Cached toolchain not found, please put it here: $TOOLCHAIN_TARBALL"
      if [ "$ALLOW_DOWNLOAD" != "1" ]; then
        echo "Cache-only mode is enabled. Set ALLOW_DOWNLOAD=1 if you want to download automatically."
        exit 1
      fi
      echo "Downloading musl toolchain (aarch64-linux-musl-cross)..."
      if [ -n "$TOOLCHAIN_URL" ]; then
        download_url="$TOOLCHAIN_URL"
      elif [ -n "$GITHUB_RELEASE_URL" ]; then
        download_url="$GITHUB_RELEASE_URL"
        if [ -n "$GITHUB_PROXY" ]; then
          download_url="${GITHUB_PROXY%/}/$download_url"
        fi
      elif [ -n "$GITHUB_REPO" ] && [ -n "$GITHUB_TAG" ]; then
        github_url="https://github.com/${GITHUB_REPO}/releases/download/${GITHUB_TAG}/${GITHUB_ASSET}"
        if [ -n "$GITHUB_PROXY" ]; then
          download_url="${GITHUB_PROXY%/}/$github_url"
        else
          download_url="$github_url"
        fi
      elif [ -n "$MUSL_MIRRORS" ]; then
        download_url=""
        for m in $MUSL_MIRRORS; do
          if curl -I --connect-timeout 5 --max-time 8 "${m}aarch64-linux-musl-cross.tgz" >/dev/null 2>&1; then
            download_url="${m}aarch64-linux-musl-cross.tgz"
            break
          fi
        done
        if [ -z "$download_url" ]; then
          download_url="${MUSL_MIRROR}aarch64-linux-musl-cross.tgz"
        fi
      else
        download_url="${MUSL_MIRROR}aarch64-linux-musl-cross.tgz"
      fi
      echo "Using toolchain URL: $download_url"
      if command -v aria2c >/dev/null 2>&1; then
        aria2c -x 8 -s 8 -k 1M -o "$(basename "$TOOLCHAIN_TARBALL")" -d "$(dirname "$TOOLCHAIN_TARBALL")" "$download_url"
      else
        curl -L --retry 3 --retry-delay 2 --connect-timeout 10 --continue-at - "$download_url" -o "$TOOLCHAIN_TARBALL"
      fi
    else
      echo "Using cached musl toolchain: $TOOLCHAIN_TARBALL"
    fi
    if [ -d "$TOOLCHAIN_DIR" ] && [ ! -x "$TOOLCHAIN_DIR/bin/aarch64-linux-musl-gcc" ]; then
      rm -rf "$TOOLCHAIN_DIR"
      mkdir -p "$TOOLCHAIN_DIR"
    fi
    tar xf "$TOOLCHAIN_TARBALL" --strip-components 1 -C "$TOOLCHAIN_DIR"
  fi
  export PATH="$TOOLCHAIN_DIR/bin:$PATH"
}

HOST_OS="$(uname -s)"
if [ "$HOST_OS" = "Darwin" ]; then
  USE_ZIG=1
fi

is_tar_ok() {
  tar tf "$1" >/dev/null 2>&1
}

detect_zig_tarball() {
  if [ -n "$ZIG_TARBALL" ] && [ -f "$ZIG_TARBALL" ]; then
    echo "$ZIG_TARBALL"
    return
  fi
  local arch
  arch="$(uname -m)"
  local suffix="x86_64"
  if [ "$arch" = "arm64" ] || [ "$arch" = "aarch64" ]; then
    suffix="aarch64"
  fi
  local candidate="$CACHE_DIR/zig-macos-${suffix}-${ZIG_VERSION}.tar.xz"
  if [ -f "$candidate" ]; then
    echo "$candidate"
  fi
}

prepare_web_env() {
  mkdir -p "$CACHE_DIR"
  export HUSKY=0
  export HUSKY_SKIP_INSTALL=1
  export COREPACK_HOME="${COREPACK_HOME:-$CACHE_DIR/corepack}"
  export XDG_CACHE_HOME="${XDG_CACHE_HOME:-$CACHE_DIR/xdg-cache}"
  export XDG_DATA_HOME="${XDG_DATA_HOME:-$CACHE_DIR/xdg-data}"
  export XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$CACHE_DIR/xdg-config}"
  export NPM_CONFIG_CACHE="${NPM_CONFIG_CACHE:-$CACHE_DIR/npm-cache}"
  export YARN_CACHE_FOLDER="${YARN_CACHE_FOLDER:-$CACHE_DIR/yarn-cache}"
  export PNPM_STORE_DIR="${PNPM_STORE_DIR:-$CACHE_DIR/pnpm-store}"
  if [ -n "$NPM_REGISTRY" ]; then
    export NPM_CONFIG_REGISTRY="$NPM_REGISTRY"
  fi
}

read_solid_router_url() {
  local url=""
  if [ -n "$SOLID_ROUTER_URL" ]; then
    url="$SOLID_ROUTER_URL"
  elif [ -f "$WEB_DIR/.gitmodules" ]; then
    url="$(
      awk -F'= *' '
        /^\[submodule "solid-router"\]/{found=1; next}
        found && $1 ~ /url/ {gsub(/^[ \t]+|[ \t]+$/, "", $2); print $2; exit}
      ' "$WEB_DIR/.gitmodules"
    )"
  fi
  if [ -n "$SOLID_ROUTER_MIRROR" ] && [ -n "$url" ]; then
    url="${SOLID_ROUTER_MIRROR%/}/$url"
  fi
  echo "$url"
}

download_solid_router_tarball() {
  local url="${1:-$SOLID_ROUTER_TARBALL_URL}"
  local urls=()
  if [[ "$url" == https://github.com/* ]] || [[ "$url" == https://codeload.github.com/* ]]; then
    if [ -n "$SOLID_ROUTER_MIRRORS" ]; then
      for m in $SOLID_ROUTER_MIRRORS; do
        urls+=("${m%/}/$url")
      done
    fi
  fi
  urls+=("$url")
  local downloaded=0
  for u in "${urls[@]}"; do
    echo "Downloading solid-router tarball: $u"
    if command -v aria2c >/dev/null 2>&1; then
      if aria2c -x 8 -s 8 -k 1M -o "$(basename "$SOLID_ROUTER_TARBALL")" -d "$(dirname "$SOLID_ROUTER_TARBALL")" "$u"; then
        downloaded=1
        break
      fi
    else
      if curl -L --retry 3 --retry-delay 2 --connect-timeout 10 --continue-at - "$u" -o "$SOLID_ROUTER_TARBALL"; then
        downloaded=1
        break
      fi
    fi
  done
  if [ "$downloaded" != "1" ]; then
    return 1
  fi
  return 0
}

ensure_solid_router() {
  if [ -f "$SOLID_ROUTER_DIR/src/index.tsx" ] || [ -d "$SOLID_ROUTER_DIR/src" ]; then
    return 0
  fi
  if [ -f "$SOLID_ROUTER_TARBALL" ]; then
    if tar tzf "$SOLID_ROUTER_TARBALL" >/dev/null 2>&1; then
      rm -rf "$SOLID_ROUTER_DIR"
      mkdir -p "$SOLID_ROUTER_DIR"
      tar xf "$SOLID_ROUTER_TARBALL" -C "$SOLID_ROUTER_DIR" --strip-components 1
      if [ -d "$SOLID_ROUTER_DIR/src" ]; then
        return 0
      fi
    else
      echo "Cached solid-router tarball is corrupted, removing: $SOLID_ROUTER_TARBALL"
      rm -f "$SOLID_ROUTER_TARBALL"
    fi
  fi
  if [ "$ALLOW_DOWNLOAD" != "1" ]; then
    echo "solid-router source is missing. Please provide it at: $SOLID_ROUTER_DIR"
    echo "Or place a tarball at: $SOLID_ROUTER_TARBALL"
    return 1
  fi
  if download_solid_router_tarball "$SOLID_ROUTER_TARBALL_URL"; then
    if tar tzf "$SOLID_ROUTER_TARBALL" >/dev/null 2>&1; then
      rm -rf "$SOLID_ROUTER_DIR"
      mkdir -p "$SOLID_ROUTER_DIR"
      tar xf "$SOLID_ROUTER_TARBALL" -C "$SOLID_ROUTER_DIR" --strip-components 1
      if [ -d "$SOLID_ROUTER_DIR/src" ]; then
        return 0
      fi
    fi
    echo "solid-router tarball extraction failed: $SOLID_ROUTER_TARBALL"
  fi
  local url
  url="$(read_solid_router_url)"
  if [ -z "$url" ]; then
    echo "solid-router source is missing and URL not found."
    echo "Set SOLID_ROUTER_URL to a git URL or set SOLID_ROUTER_TARBALL_URL to a tarball URL."
    return 1
  fi
  if [[ "$url" == *.tar.gz ]] || [[ "$url" == *.tgz ]]; then
    if download_solid_router_tarball "$url"; then
      if tar tzf "$SOLID_ROUTER_TARBALL" >/dev/null 2>&1; then
        rm -rf "$SOLID_ROUTER_DIR"
        mkdir -p "$SOLID_ROUTER_DIR"
        tar xf "$SOLID_ROUTER_TARBALL" -C "$SOLID_ROUTER_DIR" --strip-components 1
        if [ -d "$SOLID_ROUTER_DIR/src" ]; then
          return 0
        fi
      fi
    fi
    echo "Failed to fetch solid-router tarball from: $url"
    return 1
  fi
  if ! command -v git >/dev/null 2>&1; then
    echo "git not found. Cannot fetch solid-router. Please install git or provide a local copy."
    return 1
  fi
  echo "Fetching solid-router from: $url"
  rm -rf "$SOLID_ROUTER_DIR"
  if git clone --depth 1 "$url" "$SOLID_ROUTER_DIR"; then
    return 0
  fi
  echo "Failed to fetch solid-router. You can set SOLID_ROUTER_URL or provide a local copy at: $SOLID_ROUTER_DIR"
  return 1
}

build_web_dist() {
  if [ "$WEB_BUILD" != "1" ]; then
    return 1
  fi
  if [ ! -f "$WEB_DIR/package.json" ]; then
    return 1
  fi
  if ! command -v node >/dev/null 2>&1; then
    echo "node not found. Please install Node.js or set WEB_BUILD=0 to use a prebuilt dist."
    return 1
  fi
  if ! command -v corepack >/dev/null 2>&1; then
    echo "corepack not found. Please install Node.js >= 16.10 or set WEB_BUILD=0 to use a prebuilt dist."
  fi

  local pkg_mgr="npm"
  if [ -f "$WEB_DIR/pnpm-lock.yaml" ] && command -v pnpm >/dev/null 2>&1; then
    pkg_mgr="pnpm"
  elif [ -f "$WEB_DIR/yarn.lock" ] && command -v yarn >/dev/null 2>&1; then
    pkg_mgr="yarn"
  elif [ -f "$WEB_DIR/package-lock.json" ]; then
    pkg_mgr="npm"
  fi

  echo "Building frontend from $WEB_DIR using $pkg_mgr"
  prepare_web_env
  if ! ensure_solid_router; then
    return 1
  fi
  pushd "$WEB_DIR" >/dev/null
  case "$pkg_mgr" in
    pnpm)
      if [ -f "pnpm-lock.yaml" ]; then
        pnpm install --frozen-lockfile --prefer-offline || return 1
      else
        pnpm install --prefer-offline || return 1
      fi
      pnpm build || return 1
      ;;
    yarn)
      if [ -f "yarn.lock" ]; then
        yarn install --frozen-lockfile --prefer-offline || return 1
      else
        yarn install --prefer-offline || return 1
      fi
      yarn build || return 1
      ;;
    npm)
      if [ -f "package-lock.json" ]; then
        npm ci --no-audit --fund=false || return 1
      else
        npm install --no-audit --fund=false || return 1
      fi
      npm run build || return 1
      ;;
  esac
  popd >/dev/null
  return 0
}

copy_web_dist() {
  local src_dir="$WEB_DIR/dist"
  local dst_dir="$ROOT_DIR/public/dist"
  if [ ! -f "$src_dir/index.html" ]; then
    return 1
  fi
  rm -rf "$dst_dir"
  mkdir -p "$ROOT_DIR/public"
  cp -R "$src_dir" "$dst_dir"
  return 0
}

dist_has_lang() {
  local dist_dir="$ROOT_DIR/public/dist"
  if [ ! -d "$dist_dir/assets" ]; then
    return 1
  fi
  local pattern1="$ZH_CODE"
  local pattern2="简体中文"
  if command -v rg >/dev/null 2>&1; then
    if rg --text -n "$pattern1|$pattern2" "$dist_dir/assets" >/dev/null 2>&1; then
      return 0
    fi
    return 1
  fi
  if grep -R -n -E "$pattern1|$pattern2" "$dist_dir/assets" >/dev/null 2>&1; then
    return 0
  fi
  return 1
}

ensure_dist() {
  local index_file="$ROOT_DIR/public/dist/index.html"
  if [ "$FORCE_WEB_REBUILD" = "1" ]; then
    rm -rf "$ROOT_DIR/public/dist"
    if [ -d "$WEB_DIR/dist" ]; then
      rm -rf "$WEB_DIR/dist"
    fi
  fi
  if [ -f "$index_file" ]; then
    if [ "$REQUIRE_ZH" = "1" ]; then
      if ! dist_has_lang; then
        echo "Existing dist does not contain $ZH_CODE, rebuilding frontend..."
        rm -rf "$ROOT_DIR/public/dist"
      else
        return 0
      fi
    else
      return 0
    fi
  fi
  if [ -d "$WEB_DIR" ]; then
    if copy_web_dist; then
      if [ "$REQUIRE_ZH" != "1" ] || dist_has_lang; then
        return 0
      fi
      echo "Existing web/dist does not contain $ZH_CODE, rebuilding frontend..."
      rm -rf "$WEB_DIR/dist" "$ROOT_DIR/public/dist"
    fi
    if build_web_dist; then
      if copy_web_dist; then
        if [ "$REQUIRE_ZH" = "1" ] && ! dist_has_lang; then
          echo "Frontend build completed but $ZH_CODE is still missing in dist."
          exit 1
        fi
        return 0
      fi
    else
      if [ "$WEB_BUILD_FALLBACK" != "1" ]; then
        echo "Frontend build failed or skipped. Place web/dist manually or set WEB_BUILD_FALLBACK=1 to use dist tarball."
        exit 1
      fi
    fi
  fi
  mkdir -p "$CACHE_DIR"
  if [ -f "$DIST_TARBALL" ]; then
    if ! tar tzf "$DIST_TARBALL" >/dev/null 2>&1; then
      echo "Cached dist tarball is corrupted, removing: $DIST_TARBALL"
      rm -f "$DIST_TARBALL"
    fi
  fi
  if [ ! -f "$DIST_TARBALL" ]; then
    if [ "$ALLOW_DOWNLOAD" != "1" ]; then
      echo "Frontend dist not found. Please place it at: $DIST_TARBALL"
      exit 1
    fi
    local urls=()
    if [[ "$DIST_URL" == https://github.com/* ]] || [[ "$DIST_URL" == https://raw.githubusercontent.com/* ]]; then
      if [ -n "$GITHUB_PROXY" ]; then
        urls+=("${GITHUB_PROXY%/}/$DIST_URL")
      fi
      if [ -n "$DIST_MIRRORS" ]; then
        for m in $DIST_MIRRORS; do
          urls+=("${m%/}/$DIST_URL")
        done
      fi
    fi
    urls+=("$DIST_URL")
    local downloaded=0
    for url in "${urls[@]}"; do
      echo "Downloading frontend dist: $url"
      if command -v aria2c >/dev/null 2>&1; then
        if aria2c -x 8 -s 8 -k 1M -o "$(basename "$DIST_TARBALL")" -d "$(dirname "$DIST_TARBALL")" "$url"; then
          downloaded=1
          break
        fi
      else
        if curl -L --retry 3 --retry-delay 2 --connect-timeout 10 --continue-at - "$url" -o "$DIST_TARBALL"; then
          downloaded=1
          break
        fi
      fi
    done
    if [ "$downloaded" != "1" ]; then
      echo "Failed to download frontend dist. You can put it manually at: $DIST_TARBALL"
      exit 1
    fi
  fi
  local tmp_dir="$ROOT_DIR/build/dist_tmp"
  rm -rf "$tmp_dir"
  mkdir -p "$tmp_dir"
  tar xf "$DIST_TARBALL" -C "$tmp_dir"
  rm -rf "$ROOT_DIR/public/dist"
  mkdir -p "$ROOT_DIR/public"
  if [ -d "$tmp_dir/dist" ]; then
    mv "$tmp_dir/dist" "$ROOT_DIR/public/dist"
  else
    mkdir -p "$ROOT_DIR/public/dist"
    mv "$tmp_dir"/* "$ROOT_DIR/public/dist/" || true
  fi
}

if [ "$USE_ZIG" != "1" ]; then
  ensure_toolchain
else
  if ! command -v "$ZIG_BIN" >/dev/null 2>&1; then
    zig_tarball="$(detect_zig_tarball || true)"
    if [ -n "$zig_tarball" ] && [ -f "$zig_tarball" ]; then
      mkdir -p "$ZIG_DIR"
      if [ ! -x "$ZIG_DIR/zig" ]; then
        rm -rf "$ZIG_DIR"
        mkdir -p "$ZIG_DIR"
        if ! is_tar_ok "$zig_tarball"; then
          echo "Cached zig tarball is corrupted, removing: $zig_tarball"
          rm -f "$zig_tarball"
        else
          tar xf "$zig_tarball" -C "$ZIG_DIR" --strip-components 1
        fi
      fi
      if [ -x "$ZIG_DIR/zig" ]; then
        ZIG_BIN="$ZIG_DIR/zig"
      fi
    fi
    if ! command -v "$ZIG_BIN" >/dev/null 2>&1 && [ "$ALLOW_DOWNLOAD" = "1" ]; then
      mkdir -p "$CACHE_DIR"
      arch="$(uname -m)"
      suffix="x86_64"
      if [ "$arch" = "arm64" ] || [ "$arch" = "aarch64" ]; then
        suffix="aarch64"
      fi
      if [ -n "$ZIG_URL" ]; then
        zig_url="$ZIG_URL"
      else
        zig_url="https://ziglang.org/download/${ZIG_VERSION}/zig-macos-${suffix}-${ZIG_VERSION}.tar.xz"
      fi
      zig_tarball="$CACHE_DIR/zig-macos-${suffix}-${ZIG_VERSION}.tar.xz"
      echo "Downloading zig: $zig_url"
      if command -v aria2c >/dev/null 2>&1; then
        aria2c -x 8 -s 8 -k 1M -o "$(basename "$zig_tarball")" -d "$(dirname "$zig_tarball")" "$zig_url"
      else
        curl -L --retry 3 --retry-delay 2 --connect-timeout 10 --continue-at - "$zig_url" -o "$zig_tarball"
      fi
      if is_tar_ok "$zig_tarball"; then
        rm -rf "$ZIG_DIR"
        mkdir -p "$ZIG_DIR"
        tar xf "$zig_tarball" -C "$ZIG_DIR" --strip-components 1
        if [ -x "$ZIG_DIR/zig" ]; then
          ZIG_BIN="$ZIG_DIR/zig"
        fi
      else
        echo "Downloaded zig tarball is corrupted: $zig_tarball"
      fi
    fi
    if ! command -v "$ZIG_BIN" >/dev/null 2>&1; then
      echo "zig not found. On macOS you must use zig to cross-compile musl."
      echo "Install zig and retry, or set ZIG_BIN to the zig executable path."
      echo "You can also set ZIG_TARBALL to a local zig tarball to auto-extract."
      echo "Set ALLOW_DOWNLOAD=1 to let the script download zig automatically."
      exit 1
    fi
  fi
fi

ensure_dist

mkdir -p "$(dirname "$OUT_PATH")"

built_at="$(date +'%F %T %z')"
go_version="$(go version | sed 's/go version //')"
git_author="$(git log -1 --pretty=format:'%an <%ae>' 2>/dev/null || echo 'unknown')"
git_commit="$(git log --pretty=format:'%h' -1 2>/dev/null || echo 'unknown')"
version="$(git describe --abbrev=0 --tags 2>/dev/null || echo 'dev')"

ldflags="\
-w -s \
-X 'github.com/alist-org/alist/v3/internal/conf.BuiltAt=$built_at' \
-X 'github.com/alist-org/alist/v3/internal/conf.GoVersion=$go_version' \
-X 'github.com/alist-org/alist/v3/internal/conf.GitAuthor=$git_author' \
-X 'github.com/alist-org/alist/v3/internal/conf.GitCommit=$git_commit' \
-X 'github.com/alist-org/alist/v3/internal/conf.Version=$version' \
--extldflags '-static -fpic' \
"

export GOOS=linux
export GOARCH=arm64
export CGO_ENABLED=1
if [ "$USE_ZIG" = "1" ]; then
  export ZIG_GLOBAL_CACHE_DIR="${ZIG_GLOBAL_CACHE_DIR:-$ROOT_DIR/build/zig-cache}"
  export ZIG_LOCAL_CACHE_DIR="${ZIG_LOCAL_CACHE_DIR:-$ROOT_DIR/build/zig-cache-local}"
  mkdir -p "$ZIG_GLOBAL_CACHE_DIR" "$ZIG_LOCAL_CACHE_DIR"
  export CC="$ZIG_BIN cc -target aarch64-linux-musl"
  export CXX="$ZIG_BIN c++ -target aarch64-linux-musl"
else
  export CC=aarch64-linux-musl-gcc
fi
if [ -z "${GOPROXY:-}" ]; then
  export GOPROXY="https://goproxy.cn,direct"
fi
if [ -d "$ROOT_DIR/vendor" ]; then
  export GOFLAGS="${GOFLAGS:-} -mod=vendor"
fi

echo "Building musl arm64 binary -> $OUT_PATH"
go build -o "$OUT_PATH" -ldflags="$ldflags" -tags=jsoniter .
