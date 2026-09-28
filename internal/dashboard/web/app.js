// dothaven dashboard. Plain DOM, no dependencies, no innerHTML with data:
// paths can contain any character, and this page must not be the way a
// crafted filename runs script.
"use strict";

const $ = (id) => document.getElementById(id);

function el(tag, props, ...children) {
  const n = document.createElement(tag);
  if (props) {
    for (const [k, v] of Object.entries(props)) {
      if (v == null) continue;
      if (k === "class") n.className = v;
      else if (k === "text") n.textContent = v;
      else n.setAttribute(k, v);
    }
  }
  for (const c of children.flat()) {
    if (c == null) continue;
    n.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return n;
}

const fmt = new Intl.NumberFormat();
function compact(n) {
  if (n >= 10000) return new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 }).format(n);
  return fmt.format(n);
}
function bytes(n) {
  if (n >= 1 << 30) return (n / (1 << 30)).toFixed(1) + " GB";
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n >= 1 << 10) return Math.round(n / (1 << 10)) + " KB";
  return n + " B";
}
function ago(iso) {
  const t = Date.parse(iso);
  if (!t) return "";
  const m = Math.round((Date.now() - t) / 60000);
  if (m < 1) return "just now";
  if (m < 60) return m + " min ago";
  if (m < 60 * 24) return Math.round(m / 60) + " h ago";
  return Math.round(m / 1440) + " days ago";
}
function when(iso) {
  const t = Date.parse(iso);
  if (!t) return "";
  const d = new Date(t);
  const opts = { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" };
  if (d.getFullYear() !== new Date().getFullYear()) opts.year = "numeric";
  return d.toLocaleString(undefined, opts);
}

async function api(name, fresh) {
  const r = await fetch("/api/" + name + (fresh ? "?fresh=1" : ""), { credentials: "same-origin" });
  const body = await r.json();
  if (!r.ok) throw new Error(body.error || r.statusText);
  return body;
}

// ---- tooltip: one element, positioned by the pointer ----
const tip = $("tip");
function attachTip(node, build) {
  node.addEventListener("pointermove", (e) => {
    tip.replaceChildren(...build());
    tip.hidden = false;
    const pad = 14, w = tip.offsetWidth, h = tip.offsetHeight;
    let x = e.clientX + pad, y = e.clientY + pad;
    if (x + w > innerWidth - 8) x = e.clientX - w - pad;
    if (y + h > innerHeight - 8) y = e.clientY - h - pad;
    tip.style.left = x + "px";
    tip.style.top = y + "px";
  });
  node.addEventListener("pointerleave", () => { tip.hidden = true; });
}

// ---- horizontal bars (single series: the title names it; no legend) ----
function bars(root, rows, value, label, detail) {
  root.replaceChildren();
  if (!rows.length) {
    root.append(el("p", { class: "empty", text: "Nothing here yet." }));
    return;
  }
  const maxV = Math.max(...rows.map(value), 1);
  for (const r of rows) {
    const v = value(r);
    const bar = el("div", { class: "bar" });
    bar.style.width = Math.max(0.4, (v / maxV) * 82) + "%";
    const row = el("div", { class: "bar-row", tabindex: "0", "aria-label": label(r) + ": " + fmt.format(v) },
      el("div", { class: "bar-label", text: label(r) }),
      el("div", { class: "bar-track" }, bar, el("span", { class: "bar-value", text: compact(v) })));
    attachTip(row, () => [el("b", { text: label(r) }), el("div", { text: detail(r) })]);
    root.append(row);
  }
}

function table(root, head, rows) {
  const t = el("table", null,
    el("thead", null, el("tr", null, head.map((h) => el("th", { class: h.num ? "num" : null, text: h.t })))),
    el("tbody", null, rows.map((r) => el("tr", null, r.map((c, i) => el("td", { class: head[i].num ? "num" : null, text: c }))))));
  root.replaceChildren(t);
}

function tile(label, value, note) {
  return el("div", { class: "tile" },
    el("div", { class: "label", text: label }),
    el("div", { class: "value", text: value }),
    note ? el("div", { class: "note", text: note }) : null);
}

function status(kind, text) {
  return el("span", { class: "status" }, el("span", { class: "dot " + kind, "aria-hidden": "true" }), text);
}

function fail(root, err) {
  root.classList.remove("lazy");
  root.replaceChildren(el("p", { class: "empty", text: "Could not load: " + err.message }));
}

// ---- panels ----
function renderSummary(s) {
  $("machine").textContent = `${s.machine.host} · ${s.machine.os} · dothaven ${s.machine.version}`;
  $("hero").textContent = compact(s.coverage.files);
  $("hero-sub").textContent = `${bytes(s.coverage.bytes)} across ${s.coverage.categories.length} categories` +
    (s.coverage.includes ? ` · ${s.coverage.includes} path(s) you added` : "");

  const newest = s.backups[0];
  $("tiles").replaceChildren(
    tile("Newest backup", newest ? ago(newest.modified) : "none",
      newest ? (newest.kind === "folder" ? "folder on this machine" : `${newest.kind}, ${bytes(newest.size)}`) : "run dothaven backup --encrypt"),
    tile("Backups found", fmt.format(s.backups.length), "on this machine and its drives"),
    tile("Not covered", fmt.format(s.coverage.uncovered.length), s.coverage.uncovered.length ? "see the list below" : "everything is covered"),
    tile("Applied here", fmt.format(s.ledger.applied), s.ledger.lastApplied ? "last " + ago(s.ledger.lastApplied) : "nothing restored yet"),
    tile("Credential files", fmt.format(s.coverage.credentials), "only in encrypted backups"),
  );

  const cats = [...s.coverage.categories].sort((a, b) => b.files - a.files);
  bars($("coverage"), cats, (c) => c.files, (c) => (c.credentials ? "🔑 " : "") + c.name,
    (c) => `${fmt.format(c.files)} files · ${bytes(c.bytes)}${c.about ? " — " + c.about : ""}`);
  table($("coverage-table"), [{ t: "Category" }, { t: "Files", num: true }, { t: "Size", num: true }, { t: "What" }],
    cats.map((c) => [c.name, fmt.format(c.files), bytes(c.bytes), c.about || ""]));

  const unc = $("uncovered");
  unc.replaceChildren();
  if (!s.coverage.uncovered.length) unc.append(el("li", null, el("span", { class: "ok", text: "✓ Everything that looks like config is covered." })));
  for (const p of s.coverage.uncovered) unc.append(el("li", null, el("span", { class: "path", text: p })));

  const b = $("backups");
  if (!s.backups.length) {
    b.replaceChildren(el("p", { class: "empty", text: "No backups yet. For a new machine: dothaven backup --encrypt" }));
  } else {
    table(b, [{ t: "When" }, { t: "Kind" }, { t: "Size", num: true }, { t: "Where" }],
      s.backups.slice(0, 12).map((x) => [when(x.modified), x.kind, x.kind === "folder" ? "—" : bytes(x.size), x.path]));
  }

  const l = $("ledger");
  l.replaceChildren(el("ul", { class: "rows" },
    el("li", null, status("good", "Applied"), el("span", { class: "meta", text: fmt.format(s.ledger.applied) + " files" })),
    el("li", null, status("warning", "Skipped on purpose"), el("span", { class: "meta", text: fmt.format(s.ledger.skipped) + " files" })),
    s.ledger.lastApplied ? el("li", null, el("span", { class: "path", text: "Last restore" }), el("span", { class: "meta", text: when(s.ledger.lastApplied) })) : null));

  const inv = s.inventory;
  $("inventory-sub").textContent = inv.source ? `From ${inv.source} (${when(inv.date) || inv.date}).` : "No inventory yet — run dothaven collect, or make a backup.";
  bars($("inventory"), inv.groups, (g) => g.count, (g) => g.label, (g) => `${fmt.format(g.count)} ${g.label}`);
  table($("inventory-table"), [{ t: "Group" }, { t: "Count", num: true }], inv.groups.map((g) => [g.label, fmt.format(g.count)]));
}

function renderSecrets(s) {
  const root = $("secrets");
  root.classList.remove("lazy");
  if (!s.files.length) {
    root.replaceChildren(el("p", { class: "ok", text: `✓ No secrets found in ${fmt.format(s.scanned)} tracked files.` }));
    return;
  }
  root.replaceChildren(
    el("p", { class: "sub", text: `${s.high} HIGH, ${s.medium} MEDIUM across ${fmt.format(s.scanned)} files. Encrypted backups keep them; plaintext ones redact them.` }),
    el("ul", { class: "rows" }, s.files.map((f) => el("li", null,
      status(f.severity === "HIGH" ? "critical" : "warning", f.severity),
      el("span", { class: "path", text: f.path }),
      el("span", { class: "meta", text: f.label + (f.count > 1 ? ` ×${f.count}` : "") })))));
}

function renderRepos(r) {
  const root = $("repos");
  root.classList.remove("lazy");
  if (!r.repos.length) {
    root.replaceChildren(el("p", { class: "ok", text: `✓ ${fmt.format(r.checked)} repositories checked — everything is on a remote.` }));
    return;
  }
  root.replaceChildren(
    el("p", { class: "sub", text: `${r.repos.length} of ${fmt.format(r.checked)} repositories hold work that exists nowhere else.` }),
    el("ul", { class: "rows" }, r.repos.map((x) => el("li", null,
      status(x.noRemote ? "critical" : "serious", x.noRemote ? "No remote" : "Unpushed"),
      el("span", { class: "path", text: x.path }),
      el("span", { class: "meta", text: x.detail })))));
}

function renderGitHub(g) {
  const root = $("github");
  root.classList.remove("lazy");
  if (!g.signedIn) {
    root.replaceChildren(el("p", null, "Not signed in. ", el("code", { text: "dothaven github login" })));
    return;
  }
  if (g.renewDue) {
    root.replaceChildren(el("p", null, "Your GitHub sign-in is due for renewal (they last 8 hours). Any GitHub command renews it: ", el("code", { text: "dothaven github status" })));
    return;
  }
  const items = [el("li", null, status("good", "Signed in"), el("span", { class: "path", text: g.login }), el("span", { class: "meta", text: "via " + g.source }))];
  if (!g.repo) {
    items.push(el("li", null, el("span", { class: "path", text: "No backup repository yet" }), el("span", { class: "meta", text: "dothaven github push" })));
  } else {
    items.push(el("li", null, status(g.private ? "good" : "critical", g.private ? "Private" : "PUBLIC"), el("span", { class: "path", text: g.repo })));
    for (const m of g.machines) {
      items.push(el("li", null, el("span", { class: "path", text: "machines/" + m.machine }), el("span", { class: "meta", text: `${m.mode} · ${when(m.created)}` })));
    }
  }
  root.replaceChildren(el("ul", { class: "rows" }, items));
}

async function load(fresh) {
  try { renderSummary(await api("summary", fresh)); } catch (e) { $("hero").textContent = "—"; $("hero-sub").textContent = "Could not load: " + e.message; }
  api("secrets", fresh).then(renderSecrets, (e) => fail($("secrets"), e));
  api("repos", fresh).then(renderRepos, (e) => fail($("repos"), e));
  api("github", fresh).then(renderGitHub, (e) => fail($("github"), e));
}

// ---- theme: follows the OS, a click overrides it (remembered if storage works) ----
function applyTheme(t) {
  if (t) document.documentElement.setAttribute("data-theme", t);
  else document.documentElement.removeAttribute("data-theme");
}
try { applyTheme(localStorage.getItem("dothaven-theme")); } catch (_) { /* private mode */ }
$("theme").addEventListener("click", () => {
  const dark = document.documentElement.getAttribute("data-theme") === "dark" ||
    (!document.documentElement.getAttribute("data-theme") && matchMedia("(prefers-color-scheme: dark)").matches);
  const next = dark ? "light" : "dark";
  applyTheme(next);
  try { localStorage.setItem("dothaven-theme", next); } catch (_) { /* fine */ }
});
$("refresh").addEventListener("click", () => {
  for (const id of ["secrets", "repos", "github"]) { $(id).classList.add("lazy"); $(id).textContent = "Refreshing…"; }
  load(true);
});

load(false);
