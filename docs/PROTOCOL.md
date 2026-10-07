# Tossling protocol

This is what a Tossling app has to do to share a clipboard with the other apps: the macOS app
([tossling-desktop](https://github.com/tossling/tossling-desktop)), the Android app
([tossling-mobile](https://github.com/tossling/tossling-mobile)) and anything written later. The
[test vectors](vectors.json) pin down every byte that is derived or encrypted; a new client should pass all of them.

Tossling Server is [ntfy](https://ntfy.sh) with a small API next to it, so most of the transport is plain ntfy:
publishing to a topic, subscribing to `/<topic>/json`, attachments. Everything a room carries is encrypted on the
devices; the server sees topic names, sizes and times, never the content.

Some names still say `tossy`: the protocol was born as Tossy, and the labels that go into hashes and key derivation
keep that name forever (changing them would break every existing room).

## Contents

- [Server](#server)
- [Rooms](#rooms)
- [Messages](#messages)
- [Files](#files)
- [Devices in a room](#devices-in-a-room)
- [Joining](#joining)
- [Removing a device](#removing-a-device)
- [Projects](#projects)
- [Moving the server](#moving-the-server)
- [Compatibility](#compatibility)

## Server

Every request carries the device token: `Authorization: Bearer <token>`. The token belongs to the server's device
user (`tossy`), which can read and write `tossling-*`, `tossy-*`, `mac` and `claude`, and read every other topic.
Anyone, without a token, can read `tossling-inv-*` and `tossy-inv-*` (invites, below).

### Health

`GET /v1/tossling/health` (no token):

```json
{"server": "tossling-server", "version": "0.3.2", "push": true, "url": "https://tossling.example.com"}
```

- `server` is `tossling-server`. Servers before 0.3 answered only on `/v1/tossy/health` with `tossy-server`.
- `push`: the server can wake Android through Firebase. Without it the phone keeps a connection of its own.
- `url`: the server's main address, see [Moving the server](#moving-the-server).

A client checks the server with `GET /v1/account` and the token: 200 means the token works, 401/403 that it does not.

### Tossling API

The API lives under `/v1/tossling`; `/v1/tossy` is the same API under its old name. A client that gets 404 with a
body that is not a JSON error from `/v1/tossling/…` retries under `/v1/tossy/…` (a server before 0.3).

| Request | Answer |
|---|---|
| `GET /v1/tossling/projects` | `[{"topic", "name", "publisher"}]` |
| `POST /v1/tossling/projects` with `{"topic", "name", "publisher"?}` | 201, `{"topic", "name", "publisher", "token"?, "example"?}`: the publisher token comes only when the publisher is new |
| `DELETE /v1/tossling/projects/<topic>` | 200 |

### ntfy, as the apps use it

| What | Request |
|---|---|
| Publish a short message | `POST /<topic>`, body is the message |
| Publish with an attachment | `PUT /<topic>`, body is the attachment, `X-Message: <message>`, `X-Filename: clip.bin` |
| Listen | `GET /<topic>[,<topic>…]/json?since=<id or duration>`: one JSON event per line, keep-alive events in between |
| Fetch what was missed | `GET /<topic>/json?poll=1&since=<id or duration>` |
| Download an attachment | `GET <attachment.url>` with the token |
| Subscriptions (projects) | `GET /v1/account` → `subscriptions: [{"base_url", "topic", "display_name"}]` |
| New token | `POST /v1/account/token` with `{"label": "tossling"}` → `{"token"}` |
| Retire a token | `DELETE /v1/account/token` with `X-Token: <old token>` |

The apps send `X-Priority: 4`. Only `"event": "message"` events matter; everything else is skipped. Attachments up to
520 MiB are kept for 3 hours.

## Rooms

A room is one ntfy topic plus a 32-byte AES key. Every device of the room publishes to the topic and listens to it.

- **Topic**: a prefix and 24 lowercase hex characters (12 random bytes), for example
  `tossling-0123456789abcdef01234567`. The prefix is `tossling-` when the server's health answers with
  `tossling-server`, otherwise `tossy-`. Both prefixes are rooms for every client.
- **Key**: 32 random bytes, sent around as standard Base64 with padding.
- **Device id**: 16 lowercase hex characters, chosen once per device and kept across rooms. The Mac picks 8 random
  bytes. Android uses the first 16 hex characters of `SHA-256("tossy-device:" + ANDROID_ID)`, so a reinstall keeps
  the id (vectors: `device`).
- **Identity**: an X25519 key pair per device. The public key goes into `hello` as `pk` and lets the room send the
  device a new key when another device is removed.
- **Owner**: the device id of whoever created the room. The owner cannot be removed from the room.

## Messages

Every message in a room is an **envelope**: a JSON object (the meta) encrypted with the room key.

```
envelope = Base64( nonce[12] || AES-256-GCM(room key, nonce, meta JSON) || tag[16] )
```

No associated data, a fresh random nonce for every message (vectors: `envelope`). The envelope is the ntfy message.
Content that does not fit in the meta (images, long text, old-style files) travels as the ntfy attachment, encrypted
the same way with the same key: `nonce || ciphertext || tag`, raw bytes, not Base64.

### Meta

| Key | Meaning |
|---|---|
| `k` | Kind: `text`, `image`, `file`, `hello`, `ping`, `bye`, `rekey`, `kick`, `move` |
| `m` | MIME type of the content, `text/plain` for control messages |
| `src` | The sender's platform: `mac`, `android` (see [Compatibility](#compatibility)) |
| `n` | The sender's device name |
| `id` | The sender's device id |
| `to` | Optional list of device ids: only they act on the message |
| `v` | Text content, when it is inline |
| `f` | File name |
| `x` | File format: `2` is the streamed format below; absent means the whole file is one encrypted attachment |
| `s` | File size before encryption, bytes |
| `pk` | The sender's X25519 public key, Base64 (on control messages) |
| `re` | `true` on the `hello` of a device that has just joined: its `pk` replaces the one the others knew |
| `inv` | On a `hello`: the invite topic it answers |
| `e`, `keys`, `r`, `key` | Fields of `rekey` and `move`, below |

Readers ignore keys they do not know.

### Content

- **Text**: `k=text`, `m=text/plain`. Up to 2,400 bytes of UTF-8 go inline as `v` with `POST`; longer text is the
  attachment. The Mac sends at most 1 MB of text.
- **Image**: `k=image` with its MIME type (`image/png`, `image/jpeg`, …), always an attachment. The Mac sends up to
  15 MB and scales bigger images down.
- **File**: `k=file`, `f`, `x=2`, `s`, streamed (next section). Files are sent only when the user asks; up to 500 MB.
  A folder goes as a `.zip`.

### Receiving

1. Open the envelope; a message that does not open is skipped (another room key).
2. Skip it if `id` is this device's id, or if `to` is present and does not include this device.
3. Control kinds are handled as described below.
4. Content older than 15 minutes (by the event's `time`) is not put on the clipboard (Android still lists it in its
   history). A file is still fetched for up to 3 hours, then it is gone from the server.
5. Echo protection: a device that has just put something on the clipboard, or just sent it, ignores the same content
   (by SHA-256) coming back within 60 seconds, and does not send what it has just received.

A client remembers the id of the last event it handled and asks for `since=<id>` after a reconnect; the first time it
asks for `since=15m` (the Mac uses `900s`).

## Files

Files use a chunked stream, so neither side holds the whole file in memory (vectors: `tsy2`).

```
header = "TSY2" || chunk size (uint32, big endian) || prefix[7] || 0x00          16 bytes
chunk i = AES-256-GCM(room key, nonce_i, plaintext_i, aad = header) || tag[16]
nonce_i = prefix[7] || i (uint32, big endian) || last (0x01 for the last chunk, else 0x00)
```

- The chunk size is 1 MiB when sending; a reader accepts anything from 1 byte to 16 MiB.
- The plaintext is cut into chunks of exactly the chunk size; the last chunk holds the rest and can be empty (a file
  whose size is a multiple of the chunk size ends with an empty last chunk).
- A reader reads `chunk size + 16` bytes at a time; a shorter read is the last chunk.
- Sealed size: `16 + size + 16 × (size / chunk size + 1)`, so the upload can carry `Content-Length`.
- Received files go to the user's Downloads. Desktops put them into a `Tossling` folder there and onto the clipboard
  as a file, ready to paste.

## Devices in a room

There is no member list on the server: every device builds its own from the messages it sees.

- Any message adds or refreshes its sender: `id`, `n`, `src`, `pk`, and the event time as "last seen". A device seen
  within 5 minutes counts as online.
- A stored `pk` changes only through a `hello` with `re: true`, so a device in the room cannot quietly swap another
  device's key.
- **hello**: "I am here", with `pk`. Sent when a device joins (`re: true`), changes its name, or answers a ping
  (`to: [the pinging device]`).
- **ping**: asks everyone to answer with a `hello`. The Mac sends `ping` on start when it does not know anyone yet,
  `hello` otherwise; Android pings at most once a minute, when its home or devices screen opens.
- **bye**: the sender leaves; the others forget it.
- Old devices without an `id` are known as `legacy-<src>-<name>`.

## Joining

### Phone: QR code

A desktop shows its room as a QR code. The payload is JSON with sorted keys:

```json
{"id": "a1b2c3d4e5f60718", "k": "<room key>", "n": "Studio", "o": "<owner id>", "pk": "<public key>",
 "r": "tossling-0123456789abcdef01234567", "s": "https://tossling.example.com", "t": "<token>", "v": 2}
```

`v=1` codes (before rooms) carry `in` and `out` topics instead of `r`; they are legacy. The phone checks the room
(`poll` with the token), saves it, adds the desktop from `id`, `n` and `pk`, and sends `hello` with `re: true`.

### Desktop: invite code

A device already in the room invites another one with a one-time code, without the server storing anything.

- The code is 8 characters from `0123456789ABCDEFGHJKMNPQRSTVWXYZ`, shown as `7KQ2-M9XD`. Before use it is
  normalized: upper case, letters and digits only.
- Topic: the room prefix, `inv-`, and the first 12 bytes of `SHA-256("tossy-invite-topic:" + code)` in hex.
- Key: `PBKDF2-HMAC-SHA256(password = code, salt = "tossy-invite-v1", 300,000 iterations, 32 bytes)`.
- The invite is the JSON `{"s", "t", "r", "k", "n", "exp", "o"?}` (server, token, room, room key, inviter's name,
  expiry in Unix seconds, owner) sealed with that key like an envelope (vectors: `invite`).
- The inviter publishes it with `X-Cache: no`, so ntfy delivers it only to whoever is listening right now, and
  repeats every 15 seconds for up to 10 minutes. It expires 10 minutes after it was made.
- The joiner listens on `tossling-inv-…` and `tossy-inv-…` at once (it cannot know which prefix the inviter's room
  uses), without a token, opens the first invite that decrypts, checks `exp`, saves the room and publishes a `hello`
  to it with `re: true`, its `id`, and `inv` set to the topic the invite came from. The inviter stops when it sees
  that `hello`.

## Removing a device

Any device can remove another one except the owner. Removal moves everyone else to a new room the removed device
does not know:

1. Make a new room topic and key. If every remaining device has a `pk`, also issue a new token
   (`POST /v1/account/token`).
2. Publish `kick` with `to: [removed id]`. That device forgets the room (the owner ignores a kick).
3. Publish `rekey` with `to` = the remaining ids, `e` = an ephemeral X25519 public key and `keys` = for each
   remaining id, the secret `{"key", "r", "t"?}` (JSON, sorted keys) sealed for that device (vectors: `rekey`):

   ```
   shared = X25519(ephemeral private, recipient public)
   KEK    = HKDF-SHA256(ikm = shared, salt = ephemeral public || recipient public, info = "tossy-rekey-v1", 32 bytes)
   box    = Base64( nonce[12] || AES-256-GCM(KEK, nonce, secret, aad = recipient id) || tag[16] )
   ```

   If some remaining device has no `pk` yet, `r` and `key` also go in the clear inside the envelope (still under the
   old room key) and no new token is issued.
4. Everyone, the sender included, switches to the new room and token, forgets members not in `to` plus the sender,
   and says `hello` there. The old token is deleted a day later (`DELETE /v1/account/token`).

**move** (`r`): sent by a desktop on the old pre-room phone channel to tell a phone that predates rooms where the room
is. Only legacy pairings act on it.

## Projects

Projects are ntfy topics that services publish to and every device shows as notifications. They are not rooms and not
encrypted: they are ordinary ntfy messages (`title`, `message`, `priority`, `click`, `tags`, `icon`, Markdown when
`content_type` is `text/markdown`).

- The list is the device user's ntfy subscriptions: `GET /v1/account`, every topic that is not a room, named by
  `display_name`. The server keeps them under its current `base_url`.
- A device listens to the topics the user has not muted (muting is local). The Mac mutes `mac` and `claude` by default.
- Devices create and delete projects through the [Tossling API](#tossling-api).

## Moving the server

When `health.url` names another address than the one a device uses, the device:

1. accepts it only if it is `https`, or has the same scheme as now;
2. checks that `<url>/v1/tossling/health` is a Tossling server and that `<url>/v1/account` accepts the token;
3. switches to it and keeps everything else (rooms, keys, ids).

If a check fails it stays where it is and tries again later (the Mac every 6 hours).

## Android push

With Firebase set up (`push: true`), ntfy wakes the phone through a Firebase topic named after the room. The push
carries no content: the phone then fetches the room with `poll=1`.

## Compatibility

- Unknown meta keys, unknown kinds and unknown events are ignored.
- Both topic prefixes are rooms; both invite topics are listened to; both API prefixes are tried.
- Labels inside hashes and key derivation stay as they are: `tossy-device:`, `tossy-invite-topic:`,
  `tossy-invite-v1`, `tossy-rekey-v1`.
- `src`: `mac`, `windows` and `linux` are computers; `android`, `ios` and any value an app does not know are phones.
  Apps before 0.4 treat only `mac` as a computer and show the others as phones.
