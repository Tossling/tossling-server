function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text);
  }
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.opacity = "0";
  document.body.appendChild(area);
  area.select();
  const ok = document.execCommand("copy");
  area.remove();
  return ok ? Promise.resolve() : Promise.reject(new Error("copy failed"));
}

document.querySelectorAll("button[data-copy]").forEach((button) => {
  const label = button.getAttribute("aria-label");
  button.addEventListener("click", () => {
    copyText(button.dataset.copy).then(() => {
      button.classList.add("done");
      button.setAttribute("aria-label", button.dataset.copied);
      button.title = button.dataset.copied;
      setTimeout(() => {
        button.classList.remove("done");
        button.setAttribute("aria-label", label);
        button.title = label;
      }, 1600);
    });
  });
});

document.querySelectorAll("time[datetime]").forEach((el) => {
  const date = new Date(el.getAttribute("datetime"));
  if (isNaN(date)) return;
  const now = new Date();
  const time = date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  el.textContent = date.toDateString() === now.toDateString() ? time : date.toLocaleDateString([], { day: "2-digit", month: "2-digit" }) + " " + time;
  el.title = date.toLocaleString();
});

document.querySelectorAll("form[data-confirm]").forEach((form) => {
  form.addEventListener("submit", (e) => {
    if (!window.confirm(form.dataset.confirm)) e.preventDefault();
  });
});

function localTime(date) {
  const now = new Date();
  const time = date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  return date.toDateString() === now.toDateString() ? time : date.toLocaleDateString([], { day: "2-digit", month: "2-digit" }) + " " + time;
}

const events = document.querySelector("#events[data-stream]");
if (events && window.EventSource) {
  const dot = events.querySelector(".live");
  const source = new EventSource(events.dataset.stream);
  source.addEventListener("open", () => dot && dot.classList.add("on"));
  source.addEventListener("error", () => dot && dot.classList.remove("on"));
  source.addEventListener("message", (message) => {
    const e = JSON.parse(message.data);
    const item = document.createElement("div");
    item.className = "event fresh p" + (e.priority || 3);
    const head = document.createElement("div");
    head.className = "event-head";
    const title = document.createElement("span");
    title.className = "event-title";
    title.textContent = e.title || events.dataset.name;
    const when = document.createElement("time");
    when.className = "item-time";
    const date = new Date(e.time * 1000);
    when.dateTime = date.toISOString();
    when.textContent = localTime(date);
    when.title = date.toLocaleString();
    head.append(title, when);
    item.append(head);
    if (e.message) {
      const text = document.createElement("div");
      text.className = "event-text";
      text.textContent = e.message;
      item.append(text);
    }
    const empty = events.querySelector("#no-events");
    if (empty) empty.remove();
    const header = events.querySelector(".row-head");
    const first = header.nextElementSibling;
    if (first && first.classList.contains("event")) {
      const divider = document.createElement("div");
      divider.className = "divider flat";
      header.after(item, divider);
    } else {
      header.after(item);
    }
  });
}
