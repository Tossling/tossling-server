import base64
import json
import os
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

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
REPLIES_PER_MINUTE = 12

lock = threading.Lock()
replies = {}


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


def load():
    with open(STATE) as f:
        return json.load(f)


def save(state):
    tmp = STATE + ".tmp"
    with open(tmp, "w") as f:
        json.dump(state, f, indent=1, sort_keys=True)
    os.replace(tmp, STATE)


def setup():
    if os.path.exists(STATE):
        return load()
    prefix = "tossling-" if json.load(request("/v1/tossling/health")).get("server") == "tossling-server" else "tossy-"
    private = X25519PrivateKey.generate()
    state = {
        "id": os.urandom(8).hex(),
        "room": prefix + os.urandom(12).hex(),
        "key": b64(os.urandom(32)),
        "private": b64(private.private_bytes(serialization.Encoding.Raw, serialization.PrivateFormat.Raw, serialization.NoEncryption())),
        "pk": b64(private.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)),
        "since": "1m",
    }
    answer = json.load(request(
        "/v1/tossling/projects",
        data=json.dumps({"topic": PROJECT, "name": PROJECT_NAME, "publisher": PROJECT}).encode(),
        method="POST",
        headers={"Content-Type": "application/json"},
    ))
    state["publisher"] = answer.get("token", "")
    os.makedirs(os.path.dirname(os.path.abspath(STATE)), exist_ok=True)
    save(state)
    log("new room", state["room"], "project", PROJECT)
    return state


def pairing_code(state):
    return json.dumps({
        "id": state["id"], "k": state["key"], "n": NAME, "o": state["id"], "pk": state["pk"],
        "r": state["room"], "s": PUBLIC_URL, "t": TOKEN, "v": 2,
    }, sort_keys=True, separators=(",", ":"))


def seal(key, plain):
    nonce = os.urandom(12)
    return nonce + AESGCM(key).encrypt(nonce, plain, None)


def open_sealed(key, sealed):
    return AESGCM(key).decrypt(sealed[:12], sealed[12:], None)


def envelope(state, meta):
    meta = {"src": "linux", "n": NAME, "id": state["id"], "m": "text/plain", **meta}
    return b64(seal(base64.b64decode(state["key"]), json.dumps(meta, sort_keys=True).encode()))


def publish(state, meta):
    request("/" + state["room"], data=envelope(state, meta).encode(), method="POST", headers={"X-Priority": "4"})


def say(state, text, to):
    publish(state, {"k": "text", "v": text, "to": [to]})


def hello(state, to=None):
    meta = {"k": "hello", "pk": state["pk"]}
    if to:
        meta["to"] = [to]
    publish(state, meta)


def send_image(state, to):
    with open(IMAGE, "rb") as f:
        body = seal(base64.b64decode(state["key"]), f.read())
    request(
        "/" + state["room"], data=body, method="PUT",
        headers={"X-Message": envelope(state, {"k": "image", "m": "image/png", "to": [to]}), "X-Filename": "clip.bin", "X-Priority": "4"},
        timeout=120,
    )


def alert(state, title, message, priority=3, tags="white_check_mark"):
    if not state.get("publisher"):
        return
    request(
        "/" + PROJECT, data=message.encode(), method="POST", token=state["publisher"],
        headers={"Title": title, "Priority": str(priority), "Tags": tags},
    )


def allowed(sender):
    now = time.time()
    with lock:
        recent = [t for t in replies.get(sender, []) if now - t < 60]
        if len(recent) >= REPLIES_PER_MINUTE:
            replies[sender] = recent
            return False
        replies[sender] = recent + [now]
        return True


def size_label(size):
    if size >= 1 << 20:
        return f"{size / (1 << 20):.1f} MB"
    return f"{max(1, round(size / 1024))} KB"


def welcome(state, sender, name):
    time.sleep(1.5)
    say(state, f"Hi {name}! This is a demo computer in a shared demo room. Copy some text on your phone and tap To Computer, I will answer.", sender)
    time.sleep(2)
    send_image(state, sender)
    time.sleep(2)
    alert(state, "Deploy finished", "The demo project sends notifications like this one. Real projects are your servers, builds and scripts.")


def handle(state, event):
    try:
        meta = json.loads(open_sealed(base64.b64decode(state["key"]), base64.b64decode(event["message"])))
    except Exception:
        return
    sender = meta.get("id")
    if not sender or sender == state["id"]:
        return
    if "to" in meta and state["id"] not in meta["to"]:
        return
    kind = meta.get("k")
    name = meta.get("n") or "there"
    if kind == "ping":
        hello(state, to=sender)
        return
    if kind == "hello":
        if meta.get("re"):
            log("joined", sender, meta.get("src"))
            threading.Thread(target=welcome, args=(state, sender, name), daemon=True).start()
        return
    if kind not in ("text", "image", "file") or not allowed(sender):
        return
    if kind == "text":
        text = meta.get("v")
        if text is None and event.get("attachment"):
            sealed = request(event["attachment"]["url"], timeout=120).read()
            text = open_sealed(base64.b64decode(state["key"]), sealed).decode("utf-8", "replace")
        text = (text or "").strip()
        lowered = text.lower()
        if any(word in lowered for word in ("image", "picture", "photo", "картин", "фото")):
            send_image(state, sender)
            return
        if any(word in lowered for word in ("alert", "notification", "уведом")):
            alert(state, "Demo alert", f"{name} asked for a notification.")
            return
        quoted = text if len(text) <= 80 else text[:79] + "…"
        say(state, f"The demo computer got \"{quoted}\". On a real computer it is on the clipboard now, ready to paste.", sender)
    elif kind == "image":
        size = event.get("attachment", {}).get("size", 0)
        say(state, f"The demo computer got your image, {size_label(size)}. On a real computer you could paste it now.", sender)
    else:
        say(state, f"The demo computer got {meta.get('f', 'a file')}, {size_label(meta.get('s', 0))}. A real computer puts it into Downloads.", sender)


def listen(state):
    delay = 1
    while True:
        try:
            since = urllib.parse.quote(state.get("since") or "1m")
            with request(f"/{state['room']}/json?since={since}", timeout=90) as stream:
                delay = 1
                for line in stream:
                    event = json.loads(line)
                    if event.get("event") != "message":
                        continue
                    state["since"] = event["id"]
                    save(state)
                    try:
                        handle(state, event)
                    except Exception as error:
                        log("message failed", repr(error))
        except Exception as error:
            log("stream", repr(error))
            time.sleep(delay)
            delay = min(delay * 2, 60)


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
    state = setup()
    command = sys.argv[1] if len(sys.argv) > 1 else "run"
    if command == "code":
        code = pairing_code(state)
        print(code)
        print("tossling://join?code=" + base64.urlsafe_b64encode(code.encode()).decode().rstrip("="))
        return
    log("demo computer in", state["room"])
    hello(state)
    listen(state)


if __name__ == "__main__":
    main()
