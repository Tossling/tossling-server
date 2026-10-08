import base64
import html
import io
import json
import os
import socket
import sys
import threading
import time
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

SERVER = os.environ.get("TOSSLING_SERVER", "http://127.0.0.1:8090").rstrip("/")
TOKEN = os.environ.get("TOSSLING_TOKEN", "")
SETTINGS = os.environ.get("TOSSLING_SETTINGS", "")
PUBLIC_URL = os.environ.get("DEMO_PUBLIC_URL", SERVER).rstrip("/")
STATE = os.environ.get("DEMO_STATE", "/data/demo.json")
NAME = os.environ.get("DEMO_NAME", "Demo computer")
IMAGE = os.environ.get("DEMO_IMAGE", os.path.join(os.path.dirname(os.path.abspath(__file__)), "sample.png"))
PROJECT = os.environ.get("DEMO_PROJECT", "demo")
PROJECT_NAME = os.environ.get("DEMO_PROJECT_NAME", "Demo project")
PORT = int(os.environ.get("DEMO_PORT", "8080"))
LIFETIME = int(os.environ.get("DEMO_MINUTES", "60")) * 60
UNUSED = 10 * 60
MAX_ROOMS = int(os.environ.get("DEMO_MAX_ROOMS", "200"))
ROOMS_PER_ADDRESS = int(os.environ.get("DEMO_ROOMS_PER_HOUR", "10"))
REPLIES_PER_MINUTE = 12

lock = threading.RLock()
changed = threading.Event()
replies = {}
requests_by_address = {}
seen_events = []
streaming = {}
state = {}


def log(*parts):
    print(time.strftime("%Y-%m-%d %H:%M:%S"), *parts, file=sys.stderr, flush=True)


def request(path, data=None, method=None, headers=None, token=None, timeout=30):
    r = urllib.request.Request(path if path.startswith("http") else SERVER + path, data=data, method=method)
    r.add_header("Authorization", "Bearer " + (token or TOKEN))
    for key, value in (headers or {}).items():
        r.add_header(key, value)
    return urllib.request.urlopen(r, timeout=timeout)


def b64(data):
    return base64.b64encode(data).decode()


def save():
    with lock:
        tmp = STATE + ".tmp"
        with open(tmp, "w") as f:
            json.dump(state, f, indent=1, sort_keys=True)
        os.replace(tmp, STATE)


def setup():
    global state
    if os.path.exists(STATE):
        with open(STATE) as f:
            state = json.load(f)
    if "rooms" not in state:
        state = {key: state[key] for key in ("publisher",) if key in state}
        private = X25519PrivateKey.generate()
        state.update({
            "id": os.urandom(8).hex(),
            "private": b64(private.private_bytes(serialization.Encoding.Raw, serialization.PrivateFormat.Raw, serialization.NoEncryption())),
            "pk": b64(private.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)),
            "rooms": {},
        })
    state.setdefault("prefix", "tossling-" if json.load(request("/v1/tossling/health")).get("server") == "tossling-server" else "tossy-")
    if not state.get("publisher"):
        answer = json.load(request(
            "/v1/tossling/projects",
            data=json.dumps({"topic": PROJECT, "name": PROJECT_NAME, "publisher": PROJECT}).encode(),
            method="POST",
            headers={"Content-Type": "application/json"},
        ))
        state["publisher"] = answer.get("token", "")
    os.makedirs(os.path.dirname(os.path.abspath(STATE)), exist_ok=True)
    save()


def new_room():
    room = {"topic": state["prefix"] + os.urandom(12).hex(), "key": b64(os.urandom(32)), "created": time.time(), "devices": []}
    with lock:
        state["rooms"][room["topic"]] = room
        save()
    interrupt()
    code = json.dumps({
        "id": state["id"], "k": room["key"], "n": NAME, "o": state["id"], "pk": state["pk"],
        "r": room["topic"], "s": PUBLIC_URL, "t": TOKEN, "v": 2,
    }, sort_keys=True, separators=(",", ":"))
    log("room", room["topic"])
    return {
        "code": code,
        "link": "tossling://join?code=" + base64.urlsafe_b64encode(code.encode()).decode().rstrip("="),
        "expires": int(room["created"] + LIFETIME),
    }


def interrupt():
    changed.set()
    response = streaming.get("response")
    if response is not None:
        try:
            response.fp.raw._sock.shutdown(socket.SHUT_RDWR)
        except Exception:
            pass


def seal(key, plain):
    nonce = os.urandom(12)
    return nonce + AESGCM(key).encrypt(nonce, plain, None)


def open_sealed(key, sealed):
    return AESGCM(key).decrypt(sealed[:12], sealed[12:], None)


def envelope(room, meta):
    meta = {"src": "linux", "n": NAME, "id": state["id"], "m": "text/plain", **meta}
    return b64(seal(base64.b64decode(room["key"]), json.dumps(meta, sort_keys=True).encode()))


def publish(room, meta):
    request("/" + room["topic"], data=envelope(room, meta).encode(), method="POST", headers={"X-Priority": "4"})


def say(room, text, to):
    publish(room, {"k": "text", "v": text, "to": [to]})


def send_image(room, to):
    with open(IMAGE, "rb") as f:
        body = seal(base64.b64decode(room["key"]), f.read())
    request(
        "/" + room["topic"], data=body, method="PUT",
        headers={"X-Message": envelope(room, {"k": "image", "m": "image/png", "to": [to]}), "X-Filename": "clip.bin", "X-Priority": "4"},
        timeout=120,
    )


def alert(title, message):
    with lock:
        if not state.get("publisher") or time.time() - streaming.get("alerted", 0) < 60:
            return False
        streaming["alerted"] = time.time()
    request("/" + PROJECT, data=message.encode(), method="POST", token=state["publisher"], headers={"Title": title, "Priority": "3", "Tags": "white_check_mark"})
    return True


def allowed(sender):
    now = time.time()
    with lock:
        recent = [t for t in replies.get(sender, []) if now - t < 60]
        replies[sender] = recent + ([now] if len(recent) < REPLIES_PER_MINUTE else [])
        return len(recent) < REPLIES_PER_MINUTE


def size_label(size):
    return f"{size / (1 << 20):.1f} MB" if size >= 1 << 20 else f"{max(1, round(size / 1024))} KB"


def welcome(room, sender, name):
    minutes = max(1, round((room["created"] + LIFETIME - time.time()) / 60))
    time.sleep(1.5)
    say(room, f"Hi {name}! This is a demo computer, and this room is yours for {minutes} minutes. Copy some text on your phone and tap To Computer, I will answer.", sender)
    time.sleep(2)
    send_image(room, sender)
    time.sleep(2)
    alert("Deploy finished", "The demo project sends notifications like this one. Real projects are your servers, builds and scripts.")


def handle(room, event):
    try:
        meta = json.loads(open_sealed(base64.b64decode(room["key"]), base64.b64decode(event["message"])))
    except Exception:
        return
    sender = meta.get("id")
    if not sender or sender == state["id"] or ("to" in meta and state["id"] not in meta["to"]):
        return
    kind = meta.get("k")
    name = meta.get("n") or "there"
    with lock:
        if sender not in room["devices"]:
            room["devices"].append(sender)
            save()
    if kind == "ping":
        publish(room, {"k": "hello", "pk": state["pk"], "to": [sender]})
    elif kind == "hello" and meta.get("re"):
        log("joined", room["topic"], meta.get("src"))
        threading.Thread(target=welcome, args=(room, sender, name), daemon=True).start()
    elif kind in ("text", "image", "file") and allowed(sender):
        if kind == "text":
            text = meta.get("v")
            if text is None and event.get("attachment"):
                sealed = request(event["attachment"]["url"], timeout=120).read()
                text = open_sealed(base64.b64decode(room["key"]), sealed).decode("utf-8", "replace")
            text = (text or "").strip()
            lowered = text.lower()
            if any(word in lowered for word in ("image", "picture", "photo", "картин", "фото")):
                send_image(room, sender)
            elif any(word in lowered for word in ("alert", "notification", "уведом")):
                if not alert("Demo alert", "Someone in the demo asked for a notification. Your own servers and scripts send these."):
                    say(room, "A notification went out less than a minute ago, the Notifications tab has it.", sender)
            else:
                quoted = text if len(text) <= 80 else text[:79] + "…"
                say(room, f"The demo computer got \"{quoted}\". On a real computer it is on the clipboard now, ready to paste.", sender)
        elif kind == "image":
            say(room, f"The demo computer got your image, {size_label(event.get('attachment', {}).get('size', 0))}. On a real computer you could paste it now.", sender)
        else:
            say(room, f"The demo computer got {meta.get('f', 'a file')}, {size_label(meta.get('s', 0))}. A real computer puts it into Downloads.", sender)


def close(room):
    devices = list(room["devices"])
    if devices:
        try:
            publish(room, {"k": "text", "v": "The demo hour is over, so this room closes now. Set up Tossling on your computer to keep going: github.com/Tossling", "to": devices})
            time.sleep(2)
            publish(room, {"k": "kick", "to": devices})
        except Exception as error:
            log("close failed", room["topic"], repr(error))
    with lock:
        state["rooms"].pop(room["topic"], None)
        save()
    interrupt()
    log("closed", room["topic"], len(devices))


def janitor():
    while True:
        time.sleep(20)
        now = time.time()
        with lock:
            rooms = list(state["rooms"].values())
        for room in rooms:
            if now - room["created"] > LIFETIME or (not room["devices"] and now - room["created"] > UNUSED):
                close(room)


def listen():
    since = int(time.time()) - 2
    while True:
        changed.clear()
        since = max(since, int(time.time()) - 600)
        with lock:
            topics = list(state["rooms"])
        if not topics:
            changed.wait(60)
            continue
        try:
            with request(f"/{','.join(topics)}/json?since={since}", timeout=90) as stream:
                streaming["response"] = stream
                if changed.is_set():
                    continue
                for line in stream:
                    event = json.loads(line)
                    if changed.is_set():
                        break
                    if event.get("event") != "message" or event["id"] in seen_events:
                        continue
                    seen_events.append(event["id"])
                    del seen_events[:-5000]
                    since = max(since, event.get("time", int(time.time())) - 2)
                    with lock:
                        room = state["rooms"].get(event.get("topic"))
                    if room:
                        try:
                            handle(room, event)
                        except Exception as error:
                            log("message failed", repr(error))
        except Exception as error:
            if not changed.is_set():
                log("stream", repr(error))
                time.sleep(3)
        finally:
            streaming["response"] = None


def address_allowed(address):
    now = time.time()
    with lock:
        recent = [t for t in requests_by_address.get(address, []) if now - t < 3600]
        ok = len(recent) < ROOMS_PER_ADDRESS and len(state["rooms"]) < MAX_ROOMS
        requests_by_address[address] = recent + ([now] if ok else [])
        return ok


def qr_svg(text):
    import qrcode
    import qrcode.image.svg
    image = qrcode.make(text, image_factory=qrcode.image.svg.SvgPathImage, box_size=8, border=2)
    out = io.BytesIO()
    image.save(out)
    return out.getvalue().decode()


PAGE = """<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Tossling demo</title><style>
:root{color-scheme:light dark;--bg:#f3f4f8;--card:#fff;--ink:#1d2030;--ink2:#5b6072;--accent:#3067b8}
@media (prefers-color-scheme:dark){:root{--bg:#14161d;--card:#1e212b;--ink:#eef0f6;--ink2:#9aa0b2;--accent:#7aa6f0}}
body{margin:0;background:var(--bg);color:var(--ink);font:16px/1.5 -apple-system,system-ui,sans-serif}
main{max-width:520px;margin:0 auto;padding:32px 16px}
.card{background:var(--card);border-radius:24px;padding:24px;margin-top:20px}
.qr{background:#fff;border-radius:16px;padding:12px;display:flex;justify-content:center}.qr svg{width:100%;max-width:300px;height:auto}
a.button{display:block;text-align:center;background:var(--accent);color:#fff;text-decoration:none;border-radius:999px;padding:14px;font-weight:600;margin-top:16px}
p{color:var(--ink2)}h1{margin:0 0 8px}</style></head><body><main>
<h1>Try Tossling</h1><p>This is a room of your own with a demo computer in it. It lasts {minutes} minutes.</p>
<div class="card"><div class="qr">{svg}</div>
<p>In Tossling tap Pair with a computer and scan the code. On the phone itself, tap the button.</p>
<a class="button" href="{link}">Open in Tossling</a></div>
<p>No app yet? Get it at <a href="https://github.com/Tossling">github.com/Tossling</a>. Reload the page for a new room.</p>
</main></body></html>"""


class Handler(BaseHTTPRequestHandler):

    def log_message(self, *args):
        pass

    def address(self):
        forwarded = self.headers.get("X-Forwarded-For", "")
        return forwarded.split(",")[0].strip() or self.client_address[0]

    def answer(self, status, body, content_type):
        data = body.encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        path = urllib.parse.urlparse(self.path).path.rstrip("/")
        if path not in ("/demo", "/demo/room"):
            self.answer(404, "Not found\n", "text/plain")
            return
        if not address_allowed(self.address()):
            self.answer(429, json.dumps({"error": "Too many demo rooms, try again later"}), "application/json")
            return
        room = new_room()
        if path == "/demo/room":
            self.answer(200, json.dumps(room), "application/json")
        else:
            page = PAGE.replace("{svg}", qr_svg(room["code"])).replace("{link}", html.escape(room["link"])).replace("{minutes}", str(LIFETIME // 60))
            self.answer(200, page, "text/html; charset=utf-8")


def server_token():
    if TOKEN or not SETTINGS:
        return TOKEN
    for _ in range(60):
        try:
            with open(SETTINGS) as f:
                token = json.load(f).get("token", "")
            if token:
                return token
        except (OSError, ValueError):
            pass
        time.sleep(2)
    return ""


def wait_for_server():
    for _ in range(60):
        try:
            request("/v1/tossling/health", timeout=5)
            return
        except Exception:
            time.sleep(2)


def main():
    global TOKEN
    TOKEN = server_token()
    if not TOKEN:
        sys.exit("TOSSLING_TOKEN or TOSSLING_SETTINGS is required")
    wait_for_server()
    setup()
    if len(sys.argv) > 1 and sys.argv[1] == "room":
        print(json.dumps(new_room(), indent=1))
        return
    threading.Thread(target=listen, daemon=True).start()
    threading.Thread(target=janitor, daemon=True).start()
    log("demo computer on port", PORT, "rooms last", LIFETIME // 60, "minutes")
    ThreadingHTTPServer(("", PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()
