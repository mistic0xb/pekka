# Zapbot - Quick Start

## 1. Setup
```bash
# Clone repo
git clone https://github.com/yourusername/zapbot
cd zapbot

# Create config
cp config.example.yml config.yml
# Edit config.yml with your credentials
```

## 2. Run Bot
```bash
# Start bot (runs in background)
docker compose up -d

# Go inside the container 
docker exec -it zapbot sh

# Run Command
./zapbot start

# Stop bot
docker compose down
```

## Config Location

- `./config.yml` - Your credentials (DO NOT COMMIT!)
- `./zapbot.db` - Bot database (persists across restarts)

## Updating
```bash
docker compose down
git pull
docker compose build
docker compose up -d
```