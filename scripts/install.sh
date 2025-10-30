#!/usr/bin/env bash
set -euo pipefail

REPO="kangu/CouchFusion"
INSTALL_DIR="${HOME}/.couchfusion/bin"
BINARY_NAME="couchfusion"

log() {
  echo "[couchfusion installer] $*"
}

detect_os() {
  local uname_out
  uname_out="$(uname -s)"
  case "${uname_out}" in
    Linux*)   echo "linux" ;;
    Darwin*)  echo "darwin" ;;
    *)        log "Unsupported operating system: ${uname_out}"; exit 1 ;;
  esac
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "${arch}" in
    x86_64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) log "Unsupported architecture: ${arch}"; exit 1 ;;
  esac
}

fetch_latest_version() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name":' \
    | cut -d '"' -f4
}

main() {
  local version os arch tmp archive_url archive_path extracted binary_path shell_rc

  os="$(detect_os)"
  arch="$(detect_arch)"

  version="${COUCHFUSION_VERSION:-}"
  if [[ -z "${version}" ]]; then
    log "Discovering latest release..."
    version="$(fetch_latest_version)"
  fi

  if [[ -z "${version}" ]]; then
    log "Unable to determine release version. Set COUCHFUSION_VERSION and retry."
    exit 1
  fi

  archive_url="https://github.com/${REPO}/releases/download/${version}/${BINARY_NAME}_${os}_${arch}.tar.gz"
  log "Installing ${BINARY_NAME} ${version} for ${os}/${arch}"

  tmp="$(mktemp -d)"
  archive_path="${tmp}/${BINARY_NAME}.tar.gz"

  curl -fsSL "${archive_url}" -o "${archive_path}"

  mkdir -p "${INSTALL_DIR}"
  tar -xzf "${archive_path}" -C "${tmp}"

  mv -f "${tmp}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
  chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

  binary_path="${INSTALL_DIR}/${BINARY_NAME}"
  log "Binary installed to ${binary_path}"

  if ! command -v "${BINARY_NAME}" >/dev/null 2>&1; then
    shell_rc="${HOME}/.bashrc"
    if [[ -n "${ZSH_VERSION:-}" ]]; then
      shell_rc="${HOME}/.zshrc"
    elif [[ -n "${FISH_VERSION:-}" ]]; then
      shell_rc="${HOME}/.config/fish/config.fish"
    fi

    if [[ "${shell_rc}" == *"config.fish" ]]; then
      if ! grep -Fq "${INSTALL_DIR}" "${shell_rc}" 2>/dev/null; then
        echo "set -gx PATH ${INSTALL_DIR} \$PATH" >> "${shell_rc}"
        log "Added ${INSTALL_DIR} to PATH in ${shell_rc}"
      fi
    else
      if ! grep -Fq "${INSTALL_DIR}" "${shell_rc}" 2>/dev/null; then
        echo "export PATH=\"${INSTALL_DIR}:\$PATH\"" >> "${shell_rc}"
        log "Added ${INSTALL_DIR} to PATH in ${shell_rc}"
      fi
    fi
  fi

  log "Done! Restart your shell or source your profile to use ${BINARY_NAME}."
}

main "$@"
