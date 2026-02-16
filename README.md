# Pekka - Quick Start

## 1. Setup
```bash
# Clone repo
git clone https://github.com/mistic0xb/pekka
cd pekka 

# Create config
cp config.example.yml config.yml
# Edit config.yml with your credentials
```

## 2. Run Bot
```bash
# Start bot (runs in background)
docker compose up -d

# Go inside the container 
docker exec -it pekka sh

# Run Command
./pekka start

# Stop bot
docker compose down
```

## Config Location

- `./config.yml` - Your credentials (DO NOT COMMIT!)
- `./pekka.db` - Bot database (persists across restarts)

## Updating
```bash
docker compose down
git pull
docker compose build
docker compose up -d
```