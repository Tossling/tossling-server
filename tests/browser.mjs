import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const [chrome, url] = process.argv.slice(2);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const profile = mkdtempSync(join(tmpdir(), "tossling-browser-"));
const port = 19000 + Math.floor(Math.random() * 1000);
const browser = spawn(chrome, ["--headless=new", "--disable-gpu", "--no-sandbox", `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, "about:blank"], { stdio: "ignore" });
let failed = 0;
const check = (name, ok, detail) => { console.log(`${ok ? "ok  " : "FAIL"} ${name}${ok ? "" : `: ${detail}`}`); if (!ok) failed = 1; };

try {
  let page;
  for (let i = 0; i < 80 && !page; i++) {
    try { page = (await (await fetch(`http://127.0.0.1:${port}/json`)).json()).find((t) => t.type === "page"); } catch {}
    if (!page) await sleep(250);
  }
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((r) => ws.addEventListener("open", r));
  let id = 0;
  const pending = new Map();
  ws.addEventListener("message", (e) => { const m = JSON.parse(e.data); if (pending.has(m.id)) { pending.get(m.id)(m.result); pending.delete(m.id); } });
  const send = (method, params = {}) => new Promise((r) => { const n = ++id; pending.set(n, r); ws.send(JSON.stringify({ id: n, method, params })); });
  const evaluate = async (expression) => (await send("Runtime.evaluate", { expression, returnByValue: true })).result.value;
  const open = async (target) => { await send("Page.navigate", { url: target }); await sleep(1500); };
  const submit = async (selector, values) => {
    await evaluate(`(() => { const f = document.querySelector(${JSON.stringify(selector)}); ${Object.entries(values).map(([k, v]) => `f.elements[${JSON.stringify(k)}].value = ${JSON.stringify(v)};`).join(" ")} f.requestSubmit(); })()`);
    await sleep(1500);
  };
  await send("Page.enable");
  await open(url);
  check("setup page styles load", await evaluate(`getComputedStyle(document.querySelector('.field input')).borderRadius !== '0px'`), "unstyled form");
  await submit('form[action$="/admin"]', { password: "browser-pass-123", repeat: "browser-pass-123" });
  check("browser saves the panel password", await evaluate(`!!document.querySelector('a[href="/admin/"]') && !document.querySelector('form[action$="/admin"]')`), await evaluate("document.body.innerText.slice(0, 120)"));
  await open(new URL("/admin/login", url).href);
  await submit("form", { password: "browser-pass-123" });
  check("browser signs in", await evaluate("location.pathname") === "/admin/", await evaluate("location.pathname + ' ' + document.body.innerText.slice(0, 80)"));
  await open(new URL("/admin/projects", url).href);
  await submit('form[action="/admin/projects"]', { topic: "browser-alerts", name: "Browser" });
  const created = await evaluate("document.body.innerText");
  check("browser creates a project", /tk_[A-Za-z0-9]+/.test(created), created.slice(0, 120));
  const token = (created.match(/tk_[A-Za-z0-9]+/) || [""])[0];
  await open(new URL("/admin/projects/browser-alerts", url).href);
  await sleep(1000);
  check("live indicator turns on", await evaluate(`document.querySelector(".live").classList.contains("on")`), "EventSource did not open");
  await fetch(new URL("/browser-alerts", url), { method: "POST", headers: { Authorization: `Bearer ${token}`, Title: "Live in the browser" }, body: "no reload needed" });
  await sleep(1500);
  check("event appears without a reload", await evaluate(`document.querySelector("#events").innerText.includes("Live in the browser")`), await evaluate(`document.querySelector("#events").innerText.slice(0, 120)`));
  await open(url);
  await submit('form[action$="/done"]', {});
  check("browser finishes the setup", await evaluate("location.pathname") === "/", await evaluate("location.pathname"));
  ws.close();
} catch (e) {
  check("browser test ran", false, e.message);
} finally {
  browser.kill();
  await sleep(500);
  rmSync(profile, { recursive: true, force: true });
}
process.exit(failed);
