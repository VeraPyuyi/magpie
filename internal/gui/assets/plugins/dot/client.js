// Optional built-in page. Only explicit task/context text crosses this boundary.
magpie.plugin("dot", (ctx) => {
  let root, available = false, pending = null;
  const q = (id) => root.querySelector("#dot-" + id);
  const say = (key) => { const label = key.includes("_") || /^[a-z]+$/.test(key) ? "Dot state: " + key : key; const translated = t(label); return translated === label ? key.replaceAll("_", " ") : translated; };
  const uuid = () => Array.from(crypto.getRandomValues(new Uint8Array(16)), b => b.toString(16).padStart(2, "0")).join("");
  const make = (tag, text) => { const n = document.createElement(tag); if (text) n.textContent = text; return n; };
  async function api(path, body) {
    const response = await fetch("/api/dot/" + path, body === undefined ? {} : {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(body)});
    const data = await response.json();
    if (!response.ok) throw new Error(data.code || "unverified");
    return data;
  }
  function note(code) { q("notice").textContent = say(code); }
  function controls() { q("submit").disabled = !available || !!pending || !q("task").value.trim(); }
  function remember(value) {
    pending = value;
    try { if (value) localStorage.setItem("magpie.dot.pending", JSON.stringify({request_id:value.request_id, session_id:value.session_id})); else localStorage.removeItem("magpie.dot.pending"); } catch {}
    controls();
  }
  function result(job) {
    const box = q("result"); box.replaceChildren();
    box.append(make("p", job.request_id + " · " + job.job_id + " · " + say(job.state)));
    if (job.result) {
      box.append(make("pre", job.result.text));
      for (const item of (job.result.artifacts || [])) {
        try {
          const u = new URL(item.url);
          if (u.protocol !== "https:" || !u.hostname || u.username || u.password) continue;
          const a = make("a", item.label || u.hostname); a.href = u.href; a.target = "_blank"; a.rel = "noopener noreferrer"; box.append(a);
        } catch {}
      }
    }
  }
  async function recover(id) {
    try { result(await api("requests/" + encodeURIComponent(id))); note("Dot request recovered"); }
    catch { note("outcome_unknown"); }
  }
  async function history() {
    const box = q("history");
    try {
      const items = await api("requests"); box.replaceChildren();
      for (const item of items.slice().reverse()) {
        const row = make("div"); row.className = "dot-request";
        row.append(make("span", item.request_id + " · " + say(item.state)));
        const lookup = ctx.ui.button(say("Look up original request"), () => recover(item.request_id));
        const cancel = ctx.ui.button(say("Request cancellation"), async () => {
          cancel.disabled = true;
          try { await api("requests/" + encodeURIComponent(item.request_id) + "/cancel", {}); note("Cancellation is intent only; refresh to inspect completion"); }
          catch { note("outcome_unknown"); }
          finally { cancel.disabled = false; }
        });
        row.append(lookup, cancel); box.append(row);
      }
    } catch { note("journal_unavailable"); }
  }
  async function refresh() {
    available = false; controls();
    try {
      const st = await api("status"); available = st.task_available === true;
      q("status").textContent = say(!st.enabled ? "disabled" : available ? "ready" : !st.capabilities?.tasks ? "probe_required" : st.code && st.code !== "ready" ? st.code : "unverified");
      q("capability").textContent = say(st.capabilities?.tasks ? "Task capability verified" : "Task probe required in DotControl");
    } catch { q("status").textContent = say("unverified"); }
    controls(); await history();
  }
  function mount(main) {
    root = main;
    root.innerHTML = `<section class="dot-page"><h2 id="dot-title"></h2><p id="dot-status" role="status"></p><p id="dot-capability"></p><p id="dot-pair"></p><pre>magpie dot pair &lt;private-pairing-file&gt;\nmagpie dot status</pre><div class="dot-actions"><button type="button" id="dot-refresh"></button><button type="button" id="dot-off"></button></div><p id="dot-context-note"></p><label id="dot-task-label" for="dot-task"></label><textarea id="dot-task" rows="4" maxlength="131072"></textarea><label id="dot-context-label" for="dot-context"></label><textarea id="dot-context" rows="3" maxlength="65536"></textarea><div class="dot-actions"><button type="button" id="dot-submit"></button><button type="button" id="dot-lookup"></button><button type="button" id="dot-new"></button></div><p id="dot-identity"></p><p id="dot-notice" role="status" aria-live="polite"></p><div id="dot-result"></div><h3 id="dot-history-title"></h3><div id="dot-history"></div></section>`;
    try { const saved = JSON.parse(localStorage.getItem("magpie.dot.pending")); if (/^[A-Za-z0-9._:-]{1,128}$/.test(saved?.request_id)) pending = saved; } catch {}
    q("task").oninput = controls;
    q("refresh").onclick = refresh;
    q("off").onclick = async () => { try { await api("off", {}); await refresh(); } catch { note("unverified"); } };
    q("lookup").onclick = () => pending && recover(pending.request_id);
    q("new").onclick = () => { remember(null); q("identity").textContent = ""; q("lookup").disabled = true; };
    q("submit").onclick = async () => {
      if (!available || pending) return;
      const id = "gui-" + uuid();
      const sub = {request_id: id, session_id: "session-" + uuid(), mode: "task", task: q("task").value, messages: q("context").value ? [{role:"user",content:q("context").value}] : []};
      remember(sub); q("identity").textContent = sub.request_id + " · " + sub.session_id; q("lookup").disabled = false;
      try { result(await api("jobs", sub)); note("Dot task accepted"); }
      catch { note("outcome_unknown"); }
      await history();
    };
    relang();
  }
  function relang() {
    if (!root) return;
    const labels = {title:"Dot background tasks", pair:"Pair using the private local file in the terminal, then refresh status. Never paste credentials here.", refresh:"Refresh Dot status", off:"Disable Dot locally", "context-note":"Only the task and explicit context below are sent. Request history stores identity, not conversation context.", "task-label":"Task for Dot", "context-label":"Explicit context (optional)", submit:"Submit background task", lookup:"Look up original request", new:"Start a separate task", "history-title":"Requests from this window"};
    for (const [id,key] of Object.entries(labels)) q(id).textContent = say(key);
    q("task").placeholder = say("Describe the background task"); q("context").placeholder = say("Add only context you intend to send");
    q("lookup").disabled = !pending;
    if (pending) q("identity").textContent = pending.request_id + " · " + pending.session_id;
    controls();
  }
  ctx.slots.add("page", {key:"dot", title:"Dot background tasks", mount, open:refresh, relang});
});
