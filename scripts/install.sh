#!/usr/bin/env bash
# Nessy installer — detecta SO/arch, baixa binário do GitHub Releases.
#
# Uso:
#   curl -fsSL https://github.com/Felipeness/nessy/releases/latest/download/install.sh | bash
#   curl -fsSL https://github.com/Felipeness/nessy/releases/latest/download/install.sh | bash -s -- v0.1.0
#
# Vars opcionais:
#   NESSY_VERSION  — força versão específica (default: latest)
#   NESSY_INSTALL_DIR — onde instalar (default: ~/.local/bin)

set -euo pipefail

REPO="Felipeness/nessy"
VERSION="${1:-${NESSY_VERSION:-latest}}"
INSTALL_DIR="${NESSY_INSTALL_DIR:-$HOME/.local/bin}"

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  darwin|linux) ;;
  msys*|mingw*|cygwin*)
    OS="windows"
    ;;
  *)
    echo "Erro: SO não suportado: $OS" >&2
    exit 1
    ;;
esac

# Detect arch
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Erro: arch não suportada: $ARCH" >&2
    exit 1
    ;;
esac

# Resolve "latest" pra tag real
if [[ "$VERSION" == "latest" ]]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -o '"tag_name": *"[^"]*"' \
    | head -n1 \
    | sed 's/"tag_name": *"\(.*\)"/\1/')"
  if [[ -z "$VERSION" ]]; then
    echo "Erro: não consegui resolver versão latest" >&2
    exit 1
  fi
fi

# Strip leading 'v' do version pra nome do arquivo (GoReleaser usa Version sem v)
VERSION_NUM="${VERSION#v}"
EXT="tar.gz"
[[ "$OS" == "windows" ]] && EXT="zip"

ARCHIVE="nessy_${VERSION_NUM}_${OS}_${ARCH}.${EXT}"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"

echo "→ Baixando $ARCHIVE..."
TMPDIR="$(mktemp -d)"
trap "rm -rf '$TMPDIR'" EXIT

cd "$TMPDIR"
if ! curl -fsSL -o "$ARCHIVE" "$URL"; then
  echo "Erro: não consegui baixar $URL" >&2
  echo "Versões disponíveis: https://github.com/${REPO}/releases" >&2
  exit 1
fi

# Extrai
if [[ "$EXT" == "tar.gz" ]]; then
  tar -xzf "$ARCHIVE"
else
  unzip -q "$ARCHIVE"
fi

# Localiza binário (nome varia: nessy ou nessy.exe)
BIN="nessy"
[[ "$OS" == "windows" ]] && BIN="nessy.exe"
if [[ ! -f "$BIN" ]]; then
  echo "Erro: binário '$BIN' não encontrado no archive" >&2
  exit 1
fi

# Instala
mkdir -p "$INSTALL_DIR"
mv "$BIN" "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/$BIN"

echo ""
echo "✓ Nessy $VERSION instalado em $INSTALL_DIR/$BIN"
echo ""

# Checa PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  echo "⚠ $INSTALL_DIR não está no seu PATH."
  echo "  Adiciona no teu shell rc (~/.zshrc, ~/.bashrc):"
  echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
  echo ""
fi

echo "Pra começar:"
echo "  nessy tui      # explora no terminal"
echo "  nessy serve    # Studio web em http://localhost:5555"
echo "  nessy --help   # todos os comandos"
