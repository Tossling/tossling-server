# Demo room

A computer that is not there: a bot in a room of its own server. App Store and Google Play reviewers, and anyone
without a desktop at hand, join it from a link and see the app work.

When a phone joins, the bot says hello, sends an image and posts a notification to the "Demo project". After that it
answers whatever the phone sends: text, images and files. Ask it for an "image" or an "alert" to get one again.
Replies go only to the device that sent the message, but everyone who joins shares the room and sees each other's
devices.

Run it on a server of its own. The join code carries the server's device token, and that token can read every topic of
its server.

```sh
cd demo
# set the public address in docker-compose.yml, put the server behind HTTPS
docker compose up -d
docker compose exec demo-bot python bot.py code
```

The last command prints the pairing code (for a QR code) and the `tossling://join?code=…` link that opens it in the
app. Both stay valid as long as `bot/demo.json` is kept.

| Variable | Default | |
|---|---|---|
| `TOSSLING_SERVER` | `http://127.0.0.1:8090` | Where the bot talks to the server |
| `TOSSLING_TOKEN` | | The device token, or |
| `TOSSLING_SETTINGS` | | the server's `tossling-server.json` to read it from |
| `DEMO_PUBLIC_URL` | `TOSSLING_SERVER` | The address phones use, goes into the code |
| `DEMO_STATE` | `/data/demo.json` | Room, keys and the last event |
| `DEMO_NAME` | `Demo computer` | |
| `DEMO_PROJECT`, `DEMO_PROJECT_NAME` | `demo`, `Demo project` | |
