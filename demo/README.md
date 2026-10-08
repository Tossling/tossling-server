# Demo room

A computer that is not there: a bot in rooms of its own server. Anyone without a desktop at hand, and App Store and
Google Play reviewers, can see the app work.

Every request gets a room of its own for an hour. When the phone joins, the bot says hello, sends an image and posts a
notification to the "Demo project". After that it answers whatever the phone sends: text, images and files. Ask it for
an "image" or an "alert" to get one again. When the hour is up the bot says goodbye and disconnects the phone, which
goes back to its welcome screen.

| Address | What it gives |
|---|---|
| `GET /demo` | A page with a QR code for the app's scanner and an "Open in Tossling" button |
| `GET /demo/room` | JSON with `code` (the pairing code), `link` (`tossling://join?code=…`) and `expires` |

The apps ask `/demo/room` when the user taps "Try without a computer". Put both paths in front of the bot (port 8080
in the container) and everything else in front of the server.

Run the server with `TOSSLING_DEMO=true`. Every guest uses the same device token, so with the usual limits a few guests
at once would run out of requests. Demo mode raises the request, subscription and topic limits, keeps files up to
10 MB and deletes messages and files after an hour.

Run it on a server of its own. The code carries the server's device token, and that token reads every topic of its
server.

```sh
cd demo
# set the public address in docker-compose.yml, put the server behind HTTPS
docker compose up -d
```

| Variable | Default | |
|---|---|---|
| `TOSSLING_SERVER` | `http://127.0.0.1:8090` | Where the bot talks to the server |
| `TOSSLING_TOKEN` | | The device token, or |
| `TOSSLING_SETTINGS` | | the server's `tossling-server.json` to read it from |
| `DEMO_PUBLIC_URL` | `TOSSLING_SERVER` | The address phones use, goes into the code |
| `DEMO_STATE` | `/data/demo.json` | Keys and open rooms |
| `DEMO_PORT` | `8080` | |
| `DEMO_MINUTES` | `60` | How long a room lives |
| `DEMO_ROOMS_PER_HOUR` | `10` | New rooms per address and hour |
| `DEMO_MAX_ROOMS` | `200` | Open rooms at once |
| `DEMO_NAME` | `Demo computer` | |
| `DEMO_PROJECT`, `DEMO_PROJECT_NAME` | `demo`, `Demo project` | |
