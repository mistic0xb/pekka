<p align="center">
  <img src="assets/pekka.png" alt="Pekka" width="100"/>
</p>
<h1 align="center">pekka</h1>
<p align="center">
  A <i>nostr</i> bot that automatically zaps & reacts to posts from a curated private or public follow list.
</p>

## Install
Enter this in your temrinal
```bash
curl -fsSL https://pekka.mistic.xyz/install -o ./install.sh && bash install.sh
```

## Config

Fill the Prompts OR Edit `pekka/config.yml` with your credentials after install.

- `pekka/config.yml` — your credentials (DO NOT COMMIT!)
- `pekka/pekka.db` — bot database

## Run
```bash
cd pekka
./pekka start
```

## Other Helpful Commands
```
pekka start    start the bot
pekka show     display current configuration
pekka stats    show zapping statistics
pekka help     help about any command
```