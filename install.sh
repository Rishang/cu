#!/usr/bin/env bash
#
# Installer for cu (github.com/Rishang/cloudutil).
#
#   curl -fsSL https://raw.githubusercontent.com/Rishang/cloudutil/main/install.sh | bash
#
# Env overrides:
#   VERSION=v1.0.0   install a specific tag (default: latest)
#   INSTALL_DIR=...  install into this directory instead of the auto-detected one
#
set -euo pipefail

REPO="Rishang/cloudutil"
BINARY="cu"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-}"

info() { printf '\033[0;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[0;33mwarn:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[0;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"; }

# --- platform -----------------------------------------------------------------
# goreleaser names archives cu_<version>_<goos>_<goarch>.tar.gz, so map uname
# output onto GOOS/GOARCH rather than the raw kernel strings.
detect_platform() {
  local os arch
  case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) die "unsupported OS: $(uname -s) (this installer covers Linux and macOS)" ;;
  esac

  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac

  printf '%s %s' "$os" "$arch"
}

# --- install directory --------------------------------------------------------
# Prefer a user-level bin directory that is already on $PATH — installing there
# needs no sudo and lands somewhere the shell will actually find. Only fall back
# to a system directory when $PATH has no home-owned entry.
resolve_install_dir() {
  local dir
  local IFS=:
  for dir in $PATH; do
    [ -n "$dir" ] || continue
    case "$dir" in
      "$HOME"/*)
        # A writable existing directory wins outright; a missing one under $HOME
        # is still fine, we can create it.
        if [ -d "$dir" ] && [ -w "$dir" ]; then
          printf '%s' "$dir"
          return 0
        fi
        ;;
    esac
  done

  # Nothing user-level on $PATH: use the conventional system location.
  printf '%s' "/usr/local/bin"
}

# --- version ------------------------------------------------------------------
latest_version() {
  # The redirect target of /releases/latest carries the tag, which avoids
  # needing jq to parse the API response.
  curl -fsSLI -o /dev/null -w '%{url_effective}' \
    "https://github.com/${REPO}/releases/latest" | sed 's#.*/tag/##'
}

# --- main ---------------------------------------------------------------------
need curl
need tar

read -r OS ARCH <<<"$(detect_platform)"

if [ "$VERSION" = "latest" ]; then
  info "Resolving latest release..."
  VERSION="$(latest_version)"
  [ -n "$VERSION" ] || die "could not determine the latest release tag"
fi
# Archive names carry the bare version, tags carry the leading v.
VERSION_NUM="${VERSION#v}"

ARCHIVE="${BINARY}_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

[ -n "$INSTALL_DIR" ] || INSTALL_DIR="$(resolve_install_dir)"

info "Installing ${BINARY} ${VERSION} (${OS}/${ARCH})"

TMPDIR_="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_"' EXIT

info "Downloading ${ARCHIVE}"
curl -fsSL "${BASE_URL}/${ARCHIVE}" -o "${TMPDIR_}/${ARCHIVE}" ||
  die "download failed: ${BASE_URL}/${ARCHIVE}"

# Checksum verification is best-effort: skip it rather than fail the install if
# the release has no checksums.txt or the host lacks a sha256 tool.
if curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMPDIR_}/checksums.txt" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$TMPDIR_" && grep " ${ARCHIVE}\$" checksums.txt | sha256sum -c -) >/dev/null ||
      die "checksum mismatch for ${ARCHIVE}"
    info "Checksum verified"
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$TMPDIR_" && grep " ${ARCHIVE}\$" checksums.txt | shasum -a 256 -c -) >/dev/null ||
      die "checksum mismatch for ${ARCHIVE}"
    info "Checksum verified"
  else
    warn "no sha256sum/shasum found, skipping checksum verification"
  fi
else
  warn "no checksums.txt in the release, skipping checksum verification"
fi

tar -xzf "${TMPDIR_}/${ARCHIVE}" -C "$TMPDIR_"
[ -f "${TMPDIR_}/${BINARY}" ] || die "${BINARY} not found inside ${ARCHIVE}"
chmod +x "${TMPDIR_}/${BINARY}"

# Writing to /usr/local/bin (or any root-owned dir) needs sudo; a home-owned one
# does not, so only escalate when the target really is not writable.
mkdir -p "$INSTALL_DIR" 2>/dev/null || true
if [ -w "$INSTALL_DIR" ]; then
  mv "${TMPDIR_}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  info "${INSTALL_DIR} is not writable, escalating with sudo"
  need sudo
  sudo mkdir -p "$INSTALL_DIR"
  sudo mv "${TMPDIR_}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

printf '\n'
info "Installed ${BINARY} ${VERSION} to ${INSTALL_DIR}/${BINARY}"

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*)
    info "Run: ${BINARY} --help"
    ;;
  *)
    warn "${INSTALL_DIR} is not on your \$PATH"
    printf '     Add it with:  export PATH="%s:$PATH"\n' "$INSTALL_DIR"
    ;;
esac
