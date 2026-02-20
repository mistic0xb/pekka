#!/usr/bin/env bash
set -e

PEKKA_DIR="./pekka"

PURPLE='\033[0;35m'
MUTED='\033[0;2m'
RED='\033[0;31m'
ORANGE='\033[38;5;214m'
NC='\033[0m'

echo "Installing Pekka..."
echo

# create pekka directory in current dir
mkdir -p "$PEKKA_DIR"
cd "$PEKKA_DIR"

# detect OS & ARCH
raw_os=$(uname -s)
OS=$(echo "$raw_os" | tr '[:upper:]' '[:lower:]')
case "$raw_os" in
  Darwin*) OS="darwin" ;;
  Linux*)  OS="linux"  ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
  x86_64)        ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
esac

# On macOS x64, check for Rosetta
if [ "$OS" = "darwin" ] && [ "$ARCH" = "amd64" ]; then
  rosetta_flag=$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)
  if [ "$rosetta_flag" = "1" ]; then
    ARCH="arm64"
  fi
fi

BIN_NAME="pekka-${OS}-${ARCH}"
DOWNLOAD_URL="https://github.com/mistic0xb/pekka/releases/latest/download/$BIN_NAME"

# progress bar

unbuffered_sed() {
    if echo | sed -u -e "" >/dev/null 2>&1; then
        sed -nu "$@"
    elif echo | sed -l -e "" >/dev/null 2>&1; then
        sed -nl "$@"
    else
        local pad="$(printf "\n%512s" "")"
        sed -ne "s/$/\\${pad}/" "$@"
    fi
}

print_progress() {
    local bytes="$1"
    local length="$2"
    [ "$length" -gt 0 ] || return 0

    local width=50
    local percent=$(( bytes * 100 / length ))
    [ "$percent" -gt 100 ] && percent=100
    local on=$(( percent * width / 100 ))
    local off=$(( width - on ))

    local filled=$(printf "%*s" "$on" "")
    filled=${filled// /■}
    local empty=$(printf "%*s" "$off" "")
    empty=${empty// /･}

    printf "\r${ORANGE}%s%s %3d%%${NC}" "$filled" "$empty" "$percent" >&4
}

download_with_progress() {
    local url="$1"
    local output="$2"

    if [ -t 2 ]; then
        exec 4>&2
    else
        exec 4>/dev/null
    fi

    local tmp_dir=${TMPDIR:-/tmp}
    local tracefile="${tmp_dir}/pekka_install_$$.trace"

    rm -f "$tracefile"
    mkfifo "$tracefile"

    # Hide cursor
    printf "\033[?25l" >&4

    trap "trap - RETURN; rm -f \"$tracefile\"; printf '\033[?25h' >&4; exec 4>&-" RETURN

    (
        curl --trace-ascii "$tracefile" -s -L -o "$output" "$url"
    ) &
    local curl_pid=$!

    unbuffered_sed \
        -e 'y/ACDEGHLNORTV/acdeghlnortv/' \
        -e '/^0000: content-length:/p' \
        -e '/^<= recv data/p' \
        "$tracefile" | \
    {
        local length=0
        local bytes=0

        while IFS=" " read -r -a line; do
            [ "${#line[@]}" -lt 2 ] && continue
            local tag="${line[0]} ${line[1]}"

            if [ "$tag" = "0000: content-length:" ]; then
                length="${line[2]}"
                length=$(echo "$length" | tr -d '\r')
                bytes=0
            elif [ "$tag" = "<= recv" ]; then
                local size="${line[3]}"
                bytes=$(( bytes + size ))
                if [ "$length" -gt 0 ]; then
                    print_progress "$bytes" "$length"
                fi
            fi
        done
    }

    wait $curl_pid
    local ret=$?
    echo "" >&4
    return $ret
}

#####

echo -e "${MUTED}Downloading ${NC}pekka ${MUTED}(${OS}-${ARCH})...${NC}"

if [ -t 2 ]; then
    if ! download_with_progress "$DOWNLOAD_URL" pekka; then
        echo -e "${RED}Download failed.${NC}"
        exit 1
    fi
else
    # Non-TTY fallback (e.g. piped from curl | bash)
    curl -# -L -o pekka "$DOWNLOAD_URL"
fi

chmod +x pekka
echo -e "${MUTED}Binary installed to ${NC}$(pwd)/pekka"

# --- config prompts ---
echo
echo "Generating config.yml"
echo -e "${MUTED}Press Enter to accept defaults. * = required.${NC}"
echo

read -p "Bunker URL (*required): " bunker_url
[ -z "$bunker_url" ] && { echo -e "${RED}Bunker URL is required${NC}"; exit 1; }

read -p "Author npub (*required): " npub
[ -z "$npub" ] && { echo -e "${RED}Author npub is required${NC}"; exit 1; }

read -p "Daily limit (default: 10000): " daily_limit
daily_limit=${daily_limit:-10000}

read -p "Per npub limit (default: 1000): " per_npub_limit
per_npub_limit=${per_npub_limit:-1000}

read -p "NWC URL (*required): " nwc_url
[ -z "$nwc_url" ] && { echo -e "${RED}NWC URL is required${NC}"; exit 1; }

read -p "Enable reaction? (y/n, default: y): " reaction_enabled
reaction_enabled=${reaction_enabled:-y}

read -p "Reaction content (default: :catJAM:): " reaction_content
reaction_content=${reaction_content:-:catJAM:}

read -p "Emoji name (default: catJAM): " emoji_name
emoji_name=${emoji_name:-catJAM}

read -p "Emoji URL (default: https://cdn.betterttv.net/emote/5f1b0186cf6d2144653d2970/3x.webp): " emoji_url
emoji_url=${emoji_url:-https://cdn.betterttv.net/emote/5f1b0186cf6d2144653d2970/3x.webp}

echo
echo -e "${MUTED}Relays (defaults: wss://nos.lol, wss://relay.primal.net, wss://nostr.mom)${NC}"
read -p "Add extra relays (comma-separated): " extra_relays

read -p "Response delay in seconds (default: 10): " response_delay
response_delay=${response_delay:-10}

read -p "Zap amount (default: 3): " zap_amount
zap_amount=${zap_amount:-3}

read -p "Zap comment: " zap_comment

# write config.yml
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
echo -e "${MUTED}Configuration written to ${NC}$(pwd)/config.yml"
echo
echo -e "${PURPLE}Pekka is ready.${NC}"
echo
echo -e "cd pekka       ${MUTED}# enter directory${NC}"
echo -e "./pekka start  ${MUTED}# run${NC}"
echo