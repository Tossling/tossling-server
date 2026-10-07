#!/bin/bash
set -euo pipefail

URL="${1:?usage: smoke.sh <base-url> <settings.json>}"
SETTINGS="${2:?usage: smoke.sh <base-url> <settings.json>}"
TOKEN=$(sed -n 's/.*"token": *"\(tk_[A-Za-z0-9]*\)".*/\1/p' "$SETTINGS")
SECRET=$(sed -n 's/.*"setup_secret": *"\([A-Za-z0-9_-]*\)".*/\1/p' "$SETTINGS")
AUTH="Authorization: Bearer $TOKEN"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
FAILED=0

code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
check() {
    if [ "$2" = "$3" ]; then
        echo "ok   $1"
    else
        echo "FAIL $1: expected $2, got $3"
        FAILED=1
    fi
}

check "own page" 200 "$(code "$URL/")"
check "footer links the source" 1 "$(curl -s "$URL/" | grep -c 'github.com/tossling/tossling-server')"
check "own health" 200 "$(code "$URL/v1/tossling/health")"
check "health tells whether push works" 1 "$(curl -s "$URL/v1/tossling/health" | grep -c '"push":false')"
check "health names the server" 1 "$(curl -s "$URL/v1/tossling/health" | grep -c '"server":"tossling-server"')"
check "health gives the main address" 1 "$(curl -s "$URL/v1/tossling/health" | grep -c "\"url\":\"$URL\"")"
check "old api path still answers" 200 "$(code "$URL/v1/tossy/health")"
check "ntfy health" 200 "$(code "$URL/v1/health")"
check "token belongs to tossy" '"tossy"' "$(curl -s -H "$AUTH" "$URL/v1/account" | grep -o '"username":"[a-z]*"' | cut -d: -f2)"
check "publish to a room" 200 "$(code -H "$AUTH" -d hi "$URL/tossling-smoke")"
check "anonymous publish to a room" 403 "$(code -d hi "$URL/tossling-smoke")"
check "publish to an old room" 200 "$(code -H "$AUTH" -d hi "$URL/tossy-smoke")"
check "publish outside room channels" 403 "$(code -H "$AUTH" -d hi "$URL/other")"
check "publish to mac" 200 "$(code -H "$AUTH" -d hi "$URL/mac")"
check "JSON publish to root" 200 "$(code -H "$AUTH" -H 'Content-Type: application/json' -d '{"topic":"claude","message":"m"}' "$URL/")"
check "anonymous read of an invite" 200 "$(code "$URL/tossling-inv-SMOKE/json?poll=1")"
check "anonymous read of an old invite" 200 "$(code "$URL/tossy-inv-SMOKE/json?poll=1")"
check "anonymous write of an invite" 403 "$(code -d x "$URL/tossling-inv-SMOKE")"
check "uncached invite" 200 "$(code -H "$AUTH" -H 'X-Cache: no' -d x "$URL/tossling-inv-SMOKE")"

head -c 5000000 /dev/urandom > "$TMP/file.bin"
reply=$(curl -s -H "$AUTH" -T "$TMP/file.bin" -H 'X-Filename: file.bin' "$URL/tossling-smoke")
file_url=$(echo "$reply" | sed -n 's/.*"url":"\([^"]*\)".*/\1/p')
check "attachment link uses the base URL" "$URL" "${file_url%/file/*}"
curl -s -H "$AUTH" -o "$TMP/back.bin" "$URL/file/${file_url##*/file/}"
check "attachment round trip" same "$(cmp -s "$TMP/file.bin" "$TMP/back.bin" && echo same || echo different)"
check "poll a room" 2 "$(curl -s -H "$AUTH" "$URL/tossling-smoke/json?poll=1&since=all" | grep -c '"event":"message"')"

new=$(curl -s -H "$AUTH" -X POST -H 'Content-Type: application/json' -d '{"label":"smoke"}' "$URL/v1/account/token" | sed -n 's/.*"token":"\(tk_[A-Za-z0-9]*\)".*/\1/p')
check "new account token works" 200 "$(code -H "Authorization: Bearer $new" "$URL/v1/account")"
check "delete the token" 200 "$(code -H "Authorization: Bearer $new" -H "X-Token: $new" -X DELETE "$URL/v1/account/token")"
check "deleted token is refused" 401 "$(code -H "Authorization: Bearer $new" "$URL/v1/account")"
subscription='{"base_url":"'"$URL"'","topic":"smoke"}'
check "add a project channel" 200 "$(code -H "$AUTH" -X POST -H 'Content-Type: application/json' -d "$subscription" "$URL/v1/account/subscription")"

check "devices read any channel" 200 "$(code -H "$AUTH" "$URL/anything/json?poll=1")"
check "devices cannot write any channel" 403 "$(code -H "$AUTH" -d hi "$URL/anything")"

if [ -n "${TOSSLING_CLI:-}" ]; then
    $TOSSLING_CLI project add smoke-alerts Smoke Alerts > "$TMP/project.txt"
    PUB=$(grep -o 'tk_[A-Za-z0-9]*' "$TMP/project.txt" | head -1)
    check "project token printed" 1 "$([ -n "$PUB" ] && echo 1 || echo 0)"
    check "project token writes its channel" 200 "$(code -H "Authorization: Bearer $PUB" -d built "$URL/smoke-alerts")"
    check "project token cannot read" 403 "$(code -H "Authorization: Bearer $PUB" "$URL/smoke-alerts/json?poll=1")"
    check "project token cannot write a room" 403 "$(code -H "Authorization: Bearer $PUB" -d x "$URL/tossling-smoke")"
    check "project token cannot write other channels" 403 "$(code -H "Authorization: Bearer $PUB" -d x "$URL/mac")"
    check "devices read the project" 1 "$(curl -s -H "$AUTH" "$URL/smoke-alerts/json?poll=1&since=all" | grep -c '"event":"message"')"
    check "project reaches the devices" 1 "$(curl -s -H "$AUTH" "$URL/v1/account" | grep -c '"topic":"smoke-alerts","display_name":"Smoke Alerts"')"
    check "project list" 1 "$($TOSSLING_CLI project list | grep -c '^smoke-alerts')"
    check "room prefix is not a project" 1 "$($TOSSLING_CLI project add tossling-nope >/dev/null 2>&1 && echo 0 || echo 1)"
    check "old room prefix is not a project" 1 "$($TOSSLING_CLI project add tossy-nope >/dev/null 2>&1 && echo 0 || echo 1)"
    $TOSSLING_CLI project add smoke-more "Smoke More" --publisher smoke-alerts > "$TMP/more.txt"
    check "shared publisher gets no new token" 0 "$(grep -c 'tk_' "$TMP/more.txt")"
    check "shared publisher writes the second channel" 200 "$(code -H "Authorization: Bearer $PUB" -d x "$URL/smoke-more")"
    $TOSSLING_CLI project remove smoke-more > /dev/null
    check "publisher loses a removed channel" 403 "$(code -H "Authorization: Bearer $PUB" -d x "$URL/smoke-more")"
    check "publisher keeps its other channel" 200 "$(code -H "Authorization: Bearer $PUB" -d x "$URL/smoke-alerts")"
    $TOSSLING_CLI project remove smoke-alerts > /dev/null
    check "publisher without channels is removed" 401 "$(code -H "Authorization: Bearer $PUB" -d x "$URL/smoke-alerts")"
    check "removed project leaves the devices" 0 "$(curl -s -H "$AUTH" "$URL/v1/account" | grep -c '"topic":"smoke-alerts"')"
fi

check "setup page" 200 "$(code "$URL/setup/$SECRET")"
check "setup page shows the token" 1 "$(curl -s "$URL/setup/$SECRET" | grep -c "$TOKEN")"
check "wrong setup link" 404 "$(code "$URL/setup/wrong")"
check "topic setup stays out of ntfy" 404 "$(code "$URL/setup/json")"
check "static files" 200 "$(code "$URL/static/tossling.css")"
CSS=$(curl -s "$URL/" | grep -o '/static/tossling.css?v=[a-f0-9]*' | head -1)
check "pages link versioned styles" 1 "$([ -n "$CSS" ] && echo 1 || echo 0)"
check "versioned styles are cached for long" 1 "$(curl -sI "$URL$CSS" | grep -ci 'immutable')"
JAR="$TMP/cookies"
O="Origin: $URL"
check "panel without a password" 1 "$(curl -s "$URL/admin/" | grep -c 'admin-password')"
check "short panel password refused" 400 "$(code -H "$O" -d password=short -d repeat=short "$URL/setup/$SECRET/admin")"
check "panel passwords must match" 400 "$(code -H "$O" -d password=smoke-password-1 -d repeat=smoke-password-2 "$URL/setup/$SECRET/admin")"
check "set the panel password" 303 "$(code -H "$O" -d password=smoke-password-1 -d repeat=smoke-password-1 "$URL/setup/$SECRET/admin")"
check "panel asks to sign in" 303 "$(code "$URL/admin/")"
check "wrong panel password" 401 "$(code -H "$O" -d password=nope-nope-nope "$URL/admin/login")"
check "sign in" 303 "$(code -c "$JAR" -H "$O" -d password=smoke-password-1 "$URL/admin/login")"
check "session cookie is HttpOnly" 1 "$(grep -c '#HttpOnly_' "$JAR")"
check "overview" 200 "$(code -b "$JAR" "$URL/admin/")"
check "cross-site panel request refused" 403 "$(code -b "$JAR" -d topic=evil "$URL/admin/projects")"
curl -s -b "$JAR" -H "$O" -d topic=panel-alerts -d "name=Panel Alerts" "$URL/admin/projects" > "$TMP/created.html"
PT=$(grep -o 'tk_[A-Za-z0-9]*' "$TMP/created.html" | head -1)
check "panel creates a project with a token" 1 "$([ -n "$PT" ] && echo 1 || echo 0)"
check "panel token publishes" 200 "$(code -H "Authorization: Bearer $PT" -d hello "$URL/panel-alerts")"
curl -s -N -m 4 -b "$JAR" "$URL/admin/projects/panel-alerts/events" > "$TMP/live.txt" &
LIVE=$!
sleep 1
code -H "Authorization: Bearer $PT" -H "Title: Live check" -d "arrives without a reload" "$URL/panel-alerts" > /dev/null
wait $LIVE || true
check "live events reach the panel" 1 "$(grep -c '^data: .*arrives without a reload' "$TMP/live.txt")"
check "live events need a session" 303 "$(code "$URL/admin/projects/panel-alerts/events")"
check "project page" 1 "$(curl -s -b "$JAR" "$URL/admin/projects/panel-alerts" | grep -c '<h1>Panel Alerts</h1>')"
check "test event" 200 "$(code -b "$JAR" -H "$O" -X POST "$URL/admin/projects/panel-alerts/test")"
check "events on the project page" 1 "$(curl -s -b "$JAR" "$URL/admin/projects/panel-alerts" | grep -c 'Tossling Server panel')"
check "rename" 303 "$(code -b "$JAR" -H "$O" -d name=Renamed "$URL/admin/projects/panel-alerts/rename")"
check "renamed for the devices" 1 "$(curl -s -H "$AUTH" "$URL/v1/account" | grep -c '"topic":"panel-alerts","display_name":"Renamed"')"
NT=$(curl -s -b "$JAR" -H "$O" -X POST "$URL/admin/projects/panel-alerts/token" | grep -o 'tk_[A-Za-z0-9]*' | head -1)
check "old token stops after a new one" 401 "$(code -H "Authorization: Bearer $PT" -d x "$URL/panel-alerts")"
check "new token publishes" 200 "$(code -H "Authorization: Bearer $NT" -d x "$URL/panel-alerts")"
check "delete from the panel" 303 "$(code -b "$JAR" -H "$O" -X POST "$URL/admin/projects/panel-alerts/delete")"
check "deleted project token refused" 401 "$(code -H "Authorization: Bearer $NT" -d x "$URL/panel-alerts")"
check "security page" 200 "$(code -b "$JAR" "$URL/admin/security")"
check "api needs a device token" 401 "$(code "$URL/v1/tossling/projects")"
check "api lists projects" 1 "$(curl -s -H "$AUTH" "$URL/v1/tossling/projects" | grep -c '"topic":"mac"\|"topic":"claude"\|^\[')"
AT=$(curl -s -H "$AUTH" -H 'Content-Type: application/json' -d '{"topic":"api-alerts","name":"API"}' "$URL/v1/tossling/projects" | grep -o 'tk_[A-Za-z0-9]*' | head -1)
check "api creates a project with a token" 200 "$(code -H "Authorization: Bearer $AT" -d x "$URL/api-alerts")"
check "api deletes a project" 204 "$(code -H "$AUTH" -X DELETE "$URL/v1/tossling/projects/api-alerts")"
check "project tokens cannot use the api" 401 "$(code -H "Authorization: Bearer $AT" "$URL/v1/tossling/projects")"

check "cross-site finish refused" 403 "$(code -X POST "$URL/setup/$SECRET/done")"
check "finish the setup" 303 "$(code -H "Origin: $URL" -X POST "$URL/setup/$SECRET/done")"
check "used setup link" 404 "$(code "$URL/setup/$SECRET")"
LINK=$(curl -s -b "$JAR" -H "$O" -X POST "$URL/admin/security/setup-link" | grep -o "$URL/setup/[A-Za-z0-9_-]*" | head -1)
check "panel issues a working setup link" 200 "$(code "$LINK")"
check "sign out everywhere" 303 "$(code -b "$JAR" -H "$O" -X POST "$URL/admin/security/logout-all")"
check "old session ends" 303 "$(code -b "$JAR" "$URL/admin/")"
for i in 1 2 3 4 5; do code -H "$O" -d password=wrong-wrong-$i "$URL/admin/login" > /dev/null; done
check "sign-in attempts are limited" 429 "$(code -H "$O" -d password=smoke-password-1 "$URL/admin/login")"

limited=0
for i in $(seq 1 90); do
    c=$(code -H "X-Tossling-Client-Ip: 10.1.0.$i" -H "X-Forwarded-For: 10.2.0.$i" "$URL/tossling-inv-SMOKE/json?poll=1")
    [ "$c" = 429 ] && limited=1 && break
done
check "spoofed client addresses share one rate limit" 1 "$limited"

exit "$FAILED"
