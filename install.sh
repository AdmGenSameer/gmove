#!/usr/bin/env bash
# ==============================================================================
# GMOVE Installer
# Safe Media Migration Manager
# https://github.com/AdmGenSameer/gmove
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/AdmGenSameer/gmove/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/AdmGenSameer/gmove/main/install.sh | bash -s -- --install-go
# ==============================================================================

set -euo pipefail

REPO="AdmGenSameer/gmove"
APP_NAME="gmove"

# Setup terminal colors if interactive
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    BOLD="\033[1m"
    GREEN="\033[32m"
    CYAN="\033[36m"
    YELLOW="\033[33m"
    RED="\033[31m"
    RESET="\033[0m"
else
    BOLD=""
    GREEN=""
    CYAN=""
    YELLOW=""
    RED=""
    RESET=""
fi

log_info() {
    printf "${CYAN}==>${RESET} ${BOLD}%s${RESET}\n" "$1"
}

log_success() {
    printf "${GREEN}✓${RESET} %s\n" "$1"
}

log_warn() {
    printf "${YELLOW}!${RESET} %s\n" "$1"
}

log_error() {
    printf "${RED}✗${RESET} %s\n" "$1" >&2
}

printf "\n"
printf "${BOLD}${CYAN}╭────────────────────────────────────────────────────────────╮${RESET}\n"
printf "${BOLD}${CYAN}│                         GMOVE                              │${RESET}\n"
printf "${BOLD}${CYAN}│              Safe Media Migration Manager                  │${RESET}\n"
printf "${BOLD}${CYAN}╰────────────────────────────────────────────────────────────╯${RESET}\n"
printf "\n"

INSTALL_GO_REQUESTED="${INSTALL_GO:-0}"
FORCE_BUILD="${FORCE_BUILD:-0}"
GO_ONLY=false

for arg in "$@"; do
    case "$arg" in
        --install-go)
            INSTALL_GO_REQUESTED=1
            ;;
        --go-only)
            GO_ONLY=true
            INSTALL_GO_REQUESTED=1
            ;;
        --force-build)
            FORCE_BUILD=1
            ;;
        --help|-h)
            printf "GMOVE Installer\n\nUsage: install.sh [options]\n\n"
            printf "Options:\n"
            printf "  --install-go    Install official Go toolchain\n"
            printf "  --go-only       Install Go toolchain only and exit\n"
            printf "  --force-build   Build from source even if pre-built binary is found\n"
            printf "  --help, -h      Show this help message\n\n"
            exit 0
            ;;
    esac
done

# 1. Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    linux)  TARGET_OS="linux" ;;
    darwin) TARGET_OS="darwin" ;;
    *)
        log_error "Unsupported Operating System: $OS. GMOVE supports Linux and macOS."
        exit 1
        ;;
esac

# 2. Detect Architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64)   TARGET_ARCH="amd64" ;;
    aarch64|arm64)  TARGET_ARCH="arm64" ;;
    armv7l|armv6l)  TARGET_ARCH="arm" ;;
    *)
        log_error "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

log_info "Detected environment: ${TARGET_OS}/${TARGET_ARCH}"

# 3. Determine installation destination
if [ -n "${GMOVE_INSTALL_DIR:-}" ]; then
    INSTALL_DIR="$GMOVE_INSTALL_DIR"
elif [ "$EUID" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
elif [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
else
    INSTALL_DIR="$HOME/.local/bin"
fi

mkdir -p "$INSTALL_DIR"
log_info "Installation destination: ${INSTALL_DIR}"

TMP_DIR="$(mktemp -d)"
cleanup() {
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# Helper: Install official Go toolchain directly from go.dev
install_go() {
    log_info "Downloading and installing official Go toolchain..."
    
    # Query latest stable version from go.dev or fallback
    GO_VERSION=$(curl -fsSL "https://go.dev/VERSION?m=text" 2>/dev/null | head -n 1 || echo "go1.23.4")
    if [[ ! "$GO_VERSION" =~ ^go[0-9] ]]; then
        GO_VERSION="go1.23.4"
    fi
    
    # Destination directory:
    # If root, install into /usr/local/go; otherwise install into $HOME/.local/go
    if [ "$EUID" -eq 0 ]; then
        GO_PARENT_DIR="/usr/local"
    else
        GO_PARENT_DIR="$HOME/.local"
    fi
    GO_ROOT="${GO_PARENT_DIR}/go"
    
    GO_TAR="$TMP_DIR/go.tar.gz"
    GO_URL="https://go.dev/dl/${GO_VERSION}.${TARGET_OS}-${TARGET_ARCH}.tar.gz"
    
    log_info "Fetching ${GO_VERSION} (${TARGET_OS}-${TARGET_ARCH}) from ${GO_URL}..."
    if ! curl -fsSL "$GO_URL" -o "$GO_TAR"; then
        log_error "Failed to download Go from ${GO_URL}"
        return 1
    fi
    
    log_info "Unpacking Go into ${GO_ROOT}..."
    rm -rf "$GO_ROOT"
    mkdir -p "$GO_PARENT_DIR"
    tar -xzf "$GO_TAR" -C "$GO_PARENT_DIR"
    
    export GOROOT="$GO_ROOT"
    export PATH="${GO_ROOT}/bin:$PATH"
    
    # Symlink go into INSTALL_DIR so it is immediately usable in PATH
    if [ -x "${GO_ROOT}/bin/go" ] && [ -d "$INSTALL_DIR" ]; then
        ln -sf "${GO_ROOT}/bin/go" "$INSTALL_DIR/go" 2>/dev/null || true
    fi
    
    GO_INSTALLED_BY_SCRIPT=true
    log_success "Go installed successfully: $(${GO_ROOT}/bin/go version)"
    return 0
}

# Optional explicit Go installation
if [ "$INSTALL_GO_REQUESTED" = "1" ]; then
    if ! command -v go >/dev/null 2>&1; then
        install_go
    else
        log_info "Go is already available: $(go version)"
    fi
    if [ "$GO_ONLY" = true ]; then
        log_success "Go toolchain installation completed."
        exit 0
    fi
fi

# 4. Download Release Binary or Build from Source
INSTALLED=false

# First attempt: Try to fetch pre-built release binary from GitHub (unless FORCE_BUILD=1)
if [ "$FORCE_BUILD" != "1" ]; then
    RELEASE_URL="https://github.com/${REPO}/releases/latest/download/${APP_NAME}-${TARGET_OS}-${TARGET_ARCH}.tar.gz"
    log_info "Checking for pre-built release binary..."

    HTTP_STATUS=$(curl -sL -w "%{http_code}" -o "$TMP_DIR/gmove.tar.gz" "$RELEASE_URL" || true)

    if [ "$HTTP_STATUS" = "200" ] && [ -s "$TMP_DIR/gmove.tar.gz" ]; then
        log_info "Downloading and extracting release binary..."
        tar -xzf "$TMP_DIR/gmove.tar.gz" -C "$TMP_DIR"
        # Binary can be in root or inside extracted folder
        BIN_PATH=""
        if [ -f "$TMP_DIR/gmove" ]; then
            BIN_PATH="$TMP_DIR/gmove"
        elif [ -f "$TMP_DIR/gmove-${TARGET_OS}-${TARGET_ARCH}/gmove" ]; then
            BIN_PATH="$TMP_DIR/gmove-${TARGET_OS}-${TARGET_ARCH}/gmove"
        fi

        if [ -n "$BIN_PATH" ] && [ -f "$BIN_PATH" ]; then
            mv "$BIN_PATH" "$INSTALL_DIR/gmove"
            chmod +x "$INSTALL_DIR/gmove"
            INSTALLED=true
            log_success "Downloaded and installed release binary."
        fi
    fi
fi

# Second attempt: If release binary was not found or if force build requested, build via Go
if [ "$INSTALLED" = false ] || [ "$FORCE_BUILD" = "1" ]; then
    log_info "Pre-built binary not yet published or source build requested. Checking for Go toolchain..."
    if ! command -v go >/dev/null 2>&1; then
        log_info "Go is not found in PATH. Initiating automatic Go installation..."
        if ! install_go; then
            log_error "Automatic Go installation failed. Please install Go manually from https://go.dev"
            exit 1
        fi
    fi

    GO_VER=$(go version | awk '{print $3}')
    log_info "Using Go ($GO_VER). Building latest GMOVE from source..."
    
    if [ -f "./cmd/gmove/main.go" ]; then
        log_info "Detected local repository clone. Compiling from current directory..."
        CGO_ENABLED=0 go build -ldflags="-s -w" -o "$TMP_DIR/gmove" ./cmd/gmove
    else
        log_info "Fetching latest source code from GitHub..."
        SRC_TAR="$TMP_DIR/src.tar.gz"
        curl -fsSL "https://github.com/${REPO}/archive/refs/heads/main.tar.gz" -o "$SRC_TAR"
        mkdir -p "$TMP_DIR/src"
        tar -xzf "$SRC_TAR" -C "$TMP_DIR/src" --strip-components=1
        (
            cd "$TMP_DIR/src"
            CGO_ENABLED=0 go build -ldflags="-s -w" -o "$TMP_DIR/gmove" ./cmd/gmove
        )
    fi
    
    mv "$TMP_DIR/gmove" "$INSTALL_DIR/gmove"
    chmod +x "$INSTALL_DIR/gmove"
    INSTALLED=true
    log_success "Successfully compiled and installed GMOVE."
fi

# 5. Initialize user configuration if not present
CONFIG_DIR="$HOME/.config/gmove"
CONFIG_FILE="$CONFIG_DIR/config.toml"
if [ ! -f "$CONFIG_FILE" ]; then
    mkdir -p "$CONFIG_DIR"
    log_info "Initializing default configuration at ${CONFIG_FILE}..."
    curl -fsSL "https://raw.githubusercontent.com/${REPO}/main/config.example.toml" -o "$CONFIG_FILE" || true
    if [ -f "$CONFIG_FILE" ]; then
        log_success "Created initial configuration template."
    fi
else
    log_info "Existing configuration found at ${CONFIG_FILE} (preserved)."
fi

# 6. Check for rclone dependency
if ! command -v rclone >/dev/null 2>&1; then
    printf "\n"
    log_warn "rclone is not installed or not in your PATH."
    log_warn "GMOVE requires rclone to communicate with Google Drive."
    log_warn "You can install rclone quickly by running:"
    printf "      ${BOLD}curl https://rclone.org/install.sh | sudo bash${RESET}\n\n"
else
    RCLONE_VER=$(rclone version 2>/dev/null | head -n 1 || echo "installed")
    log_success "rclone is available: ${RCLONE_VER}"
fi

# 7. Check PATH
PATH_OK=false
case ":$PATH:" in
    *":$INSTALL_DIR:"*) PATH_OK=true ;;
esac

if [ "$PATH_OK" = false ]; then
    printf "\n"
    log_warn "${INSTALL_DIR} is not in your PATH."
    log_warn "Add the following line to your ~/.bashrc or ~/.zshrc:"
    printf "      ${BOLD}export PATH=\"%s:\$PATH\"${RESET}\n" "$INSTALL_DIR"
    printf "      Then run: ${BOLD}source ~/.bashrc${RESET}\n\n"
fi

# 8. Verify binary execution
if [ -x "$INSTALL_DIR/gmove" ]; then
    VER_STR=$("$INSTALL_DIR/gmove" version 2>/dev/null || echo "GMOVE")
    printf "\n"
    printf "${GREEN}${BOLD}============================================================${RESET}\n"
    printf "${GREEN}${BOLD}  %s installed successfully!${RESET}\n" "$VER_STR"
    printf "${GREEN}${BOLD}============================================================${RESET}\n\n"
    if [ "${GO_INSTALLED_BY_SCRIPT:-false}" = true ]; then
        printf "  ${CYAN}✓${RESET} Go compiler installed at: ${BOLD}%s${RESET}\n" "${GO_ROOT}/bin/go"
    fi
    printf "Quickstart commands:\n"
    printf "  1. Test remote connection:    ${CYAN}gmove config check${RESET}\n"
    printf "  2. Perform a read-only scan:  ${CYAN}gmove scan${RESET}\n"
    printf "  3. Launch interactive UI:     ${CYAN}gmove${RESET}\n\n"
fi
