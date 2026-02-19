#!/usr/bin/env bash
set -e

REPO_URL="https://github.com/mistic0xb/pekka"
REPO_DIR="pekka"
BIN_DIR="$HOME/.local/bin"

# detect piped vs sourced
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  SOURCED=true
else
  SOURCED=false
fi

echo "Installing Pekka"
echo

# clone repo if needed
if [ -d "./$REPO_DIR/.git" ]; then
  echo "Repo already exists"
else
  git clone "$REPO_URL" "$REPO_DIR"
fi

cd "$REPO_DIR"

echo
echo "Pekka configuration"
echo "This will generate config.yml"
echo "Press Enter to accept defaults"
echo "* indicates required fields"
echo

read -p "Bunker URL (*required): " bunker_url
[ -z "$bunker_url" ] && { echo "Bunker URL is required"; exit 1; }

read -p "Author npub (*required): " npub
[ -z "$npub" ] && { echo "Author npub is required"; exit 1; }

echo
echo "Budget"
read -p "Daily limit (default: 10000): " daily_limit
daily_limit=${daily_limit:-10000}

read -p "Per npub limit (default: 1000): " per_npub_limit
per_npub_limit=${per_npub_limit:-1000}

echo
read -p "NWC URL (*required): " nwc_url
if [ -z "$nwc_url" ]; then
  echo "NWC URL is required"
  exit 1
fi

echo
echo "Reaction"
read -p "Enable reaction? (y/n, default: y): " reaction_enabled
reaction_enabled=${reaction_enabled:-y}

read -p "Reaction content (default: :catJAM:): " reaction_content
reaction_content=${reaction_content:-:catJAM:}

read -p "Emoji name (default: catJAM): " emoji_name
emoji_name=${emoji_name:-catJAM}

read -p "Emoji URL (default: https://cdn.betterttv.net/emote/5f1b0186cf6d2144653d2970/3x.webp): " emoji_url
emoji_url=${emoji_url:-https://cdn.betterttv.net/emote/5f1b0186cf6d2144653d2970/3x.webp}

echo
echo "Relays"
echo "Defaults:"
echo "  wss://nos.lol"
echo "  wss://relay.primal.net"
echo "  wss://nostr.mom"
read -p "Add extra relays (comma-separated): " extra_relays

echo
read -p "Response delay in seconds (default: 10): " response_delay
response_delay=${response_delay:-10}

echo
echo "Zap"
read -p "Zap amount (default: 3): " zap_amount
zap_amount=${zap_amount:-3}

read -p "Zap comment: " zap_comment

echo
echo "Writing config.yml"

cat > config.yml <<EOF
author:
  bunker_url: "$bunker_url"
  npub: "$npub"

budget:
  daily_limit: $daily_limit
  per_npub_limit: $per_npub_limit

database:
  path: ./pekka.db

nwc_url: "$nwc_url"

reaction:
  enabled: $( [[ "$reaction_enabled" == "y" ]] && echo true || echo false )
  content: "$reaction_content"
  emoji_name: "$emoji_name"
  emoji_url: "$emoji_url"

relays:
  - wss://nos.lol
  - wss://relay.primal.net
  - wss://nostr.mom
EOF

if [ -n "$extra_relays" ]; then
  IFS=',' read -ra RELAYS <<< "$extra_relays"
  for r in "${RELAYS[@]}"; do
    echo "  - $(echo "$r" | xargs)" >> config.yml
  done
fi

cat >> config.yml <<EOF

response_delay: $response_delay
selected_list: ""

zap:
  amount: $zap_amount
  comment: "$zap_comment"
EOF

echo
echo "Building Pekka"
go build -o pekka

mkdir -p "$BIN_DIR"
install -m 755 pekka "$BIN_DIR/pekka"

echo
echo "Installation complete"

if $SOURCED; then
  echo "Staying in $(pwd)"
else
  echo
  echo "Next steps:"
  echo "  cd pekka"
  echo "  pekka start"
fi
