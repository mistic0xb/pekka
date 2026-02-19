#!/usr/bin/env bash
set -e

REPO_URL="https://github.com/mistic0xb/pekka"
INSTALL_DIR="$HOME/.pekka"
BIN_DIR="$HOME/.local/bin"

echo "▶ Installing Pekka..."

# 1. Clone repo
if [ ! -d "$INSTALL_DIR" ]; then
  git clone "$REPO_URL" "$INSTALL_DIR"
else
  echo "✔ Pekka already cloned"
fi

cd "$INSTALL_DIR"

# 2. Ask user to fill config
echo
echo "📝 Please edit the config file:"
echo "   $INSTALL_DIR/config.yml"
echo
read -p "Have you filled the config file? (y/n): " confirm

if [[ "$confirm" != "y" ]]; then
  echo "❌ Aborting. Fill config.yml and re-run install.sh"
  exit 1
fi

# 3. Build binary
echo "🔨 Building Pekka..."
go build -o pekka

# 4. Install binary globally (no chmod needed later)
mkdir -p "$BIN_DIR"
mv pekka "$BIN_DIR/pekka"

# 5. Ensure PATH (bash + zsh)
if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
  for rc in "$HOME/.bashrc" "$HOME/.zshrc"; do
    if [ -f "$rc" ]; then
      echo "export PATH=\"$BIN_DIR:\$PATH\"" >> "$rc"
      echo "✔ Added Pekka to PATH ($rc)"
    fi
  done
fi


echo
echo "✅ Installation complete!"
echo "👉 Run with: pekka start"
