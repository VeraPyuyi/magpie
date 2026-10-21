// Optional built-in page. Only explicit task/context text crosses this boundary.
magpie.plugin("dot", (ctx) => {
  let root, available = false, pending = null;
  let selected = null, revision = 0, shownJob = null, notice = "", status = "unverified", capability = "Task probe required in DotControl", items = [];
  const cancelling = new Set();
  let capacity = null;
  function drawCapacity() {
    const box = q("capacity"), c = capacity;
    box.textContent = c ? t("Dot capacity: {retained} retained of {limit}; {remaining} remaining. Probes also count.", c) : "";
    box.hidden = !c;
  }
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
  function note(code) { notice = code; q("notice").textContent = code ? say(code) : ""; }
  function select(id) {
    selected = id; revision++; shownJob = null; q("result").replaceChildren(); note("");
    q("identity").textContent = id || ""; q("lookup").disabled = !id;
    return revision;
  }
  function controls() { q("submit").disabled = !available || !!pending || !q("task").value.trim(); }
  function remember(value) {
    pending = value;
    try { if (value) localStorage.setItem("magpie.dot.pending", JSON.stringify({request_id:value.request_id, session_id:value.session_id})); else localStorage.removeItem("magpie.dot.pending"); } catch {}
    controls();
  }
  function result(job) {
    if (job.request_id !== selected) return;
    shownJob = job;
    const box = q("result"); box.replaceChildren();
    box.append(make("p", job.request_id + " · " + job.job_id + " · " + say(job.state)));
    if (typeof job.summary === "string" && job.summary) {
      const summary = make("pre", job.summary.slice(0, 4096)); summary.className = "dot-summary"; box.append(summary);
    }
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
    const token = select(id);
    try {
      const job = await api("requests/" + encodeURIComponent(id));
      if (token !== revision) return;
      if (job.request_id !== id) throw new Error("identity_mismatch");
      result(job); note("Dot request recovered");
    } catch { if (token === revision) note("outcome_unknown"); }
  }
  function drawHistory() {
    const box = q("history"); box.replaceChildren();
    for (const item of items.slice().reverse()) {
        const row = make("div"); row.className = "dot-request";
        row.append(make("span", item.request_id + " · " + say("Local acceptance record") + ": " + say(item.state)));
        const lookup = ctx.ui.button(say("Look up original request"), () => recover(item.request_id));
        const cancel = ctx.ui.button(say("Request cancellation"), async () => {
          if (cancelling.has(item.request_id)) return;
          if (selected !== item.request_id) select(item.request_id);
          const token = revision;
          cancelling.add(item.request_id); cancel.disabled = true;
          try {
            await api("requests/" + encodeURIComponent(item.request_id) + "/cancel", {});
            if (token === revision) note("Cancellation is intent only; refresh to inspect completion");
          } catch { if (token === revision) note("outcome_unknown"); }
          finally { cancelling.delete(item.request_id); drawHistory(); }
        });
        cancel.disabled = cancelling.has(item.request_id);
        row.append(lookup, cancel); box.append(row);
    }
  }
  async function history() {
    try { items = await api("requests"); drawHistory(); }
    catch { note("journal_unavailable"); }
  }
  async function refresh() {
    // Start the selected original request's GET immediately, clearing old output
    // before connection/history reads. Selection revisions reject late replies.
    const recovery = selected ? recover(selected) : Promise.resolve();
    available = false; controls();
    try {
      const st = await api("status"); available = st.task_available === true;
      const stateLabels = {offline:"DotControl is offline", unpaired:"Dot is not paired; pair in the terminal", bridge_paused:"Dot bridge is paused", access_revoked:"Dot access was revoked; pair again"};
      const c = st.capacity;
      capacity = c && [c.limit,c.retained,c.remaining].every(Number.isSafeInteger) && c.limit > 0 && c.retained >= 0 && c.remaining >= 0 && c.retained + c.remaining === c.limit ? c : null;
      status = stateLabels[st.code] || (st.code && st.code !== "ready" ? st.code : !st.enabled ? "disabled" : available ? "ready" : "probe_required");
      if (st.subscription?.renewal_state === "failed" || st.subscribe_failure) note("Dot subscription renewal failed; ChatGPT must refresh it before expiry");
      capability = st.capabilities?.tasks ? "Task capability verified" : "Task probe required in DotControl";
    } catch { status = "unverified"; capacity = null; }
    q("status").textContent = say(status); q("capability").textContent = say(capability); drawCapacity();
    controls(); await Promise.all([history(), recovery]);
  }
  function mount(main) {
    root = main;
    root.innerHTML = `<section class="dot-page"><h2 id="dot-title"></h2><p id="dot-status" role="status"></p><p id="dot-capability"></p><p id="dot-capacity" hidden></p><p id="dot-pair"></p><pre>magpie dot pair &lt;private-pairing-file&gt;\nmagpie dot status</pre><div class="dot-actions"><button type="button" id="dot-refresh"></button><button type="button" id="dot-off"></button></div><p id="dot-context-note"></p><label id="dot-task-label" for="dot-task"></label><textarea id="dot-task" rows="4" maxlength="131072"></textarea><label id="dot-context-label" for="dot-context"></label><textarea id="dot-context" rows="3" maxlength="65536"></textarea><div class="dot-actions"><button type="button" id="dot-submit"></button><button type="button" id="dot-lookup"></button><button type="button" id="dot-new"></button></div><p id="dot-identity"></p><p id="dot-notice" role="status" aria-live="polite"></p><div id="dot-result"></div><h3 id="dot-history-title"></h3><div id="dot-history"></div></section>`;
    try { const saved = JSON.parse(localStorage.getItem("magpie.dot.pending")); if (/^[A-Za-z0-9._:-]{1,128}$/.test(saved?.request_id)) pending = saved; } catch {}
    q("task").oninput = controls;
    q("refresh").onclick = refresh;
    q("off").onclick = async () => { try { await api("off", {}); await refresh(); } catch { note("unverified"); } };
    selected = pending?.request_id || null;
    q("lookup").onclick = () => selected && recover(selected);
    q("new").onclick = () => { remember(null); select(null); q("task").value = ""; q("context").value = ""; controls(); };
    q("submit").onclick = async () => {
      if (!available || pending) return;
      const id = "gui-" + uuid();
      const sub = {request_id: id, session_id: "session-" + uuid(), mode: "task", task: q("task").value, messages: q("context").value ? [{role:"user",content:q("context").value}] : []};
      remember(sub); const token = select(sub.request_id);
      q("identity").textContent = sub.request_id + " · " + sub.session_id;
      try {
        const job = await api("jobs", sub);
        if (token !== revision) return;
        if (job.request_id !== sub.request_id) throw new Error("identity_mismatch");
        result(job); note("Dot task accepted");
      } catch { if (token === revision) note("outcome_unknown"); }
      await history();
    };
    relang();
  }
  function relang() {
    if (!root) return;
    const labels = {title:"Dot background tasks", pair:"Pair using the private local file in the terminal, then refresh status. Never paste credentials here.", refresh:"Refresh Dot status", off:"Disable Dot locally", "context-note":"Only the task and explicit context below are sent. Request history stores identity, not conversation context.", "task-label":"Task for Dot", "context-label":"Explicit context (optional)", submit:"Submit background task", lookup:"Look up original request", new:"Start a separate task", "history-title":"Requests from this window"};
    for (const [id,key] of Object.entries(labels)) q(id).textContent = say(key);
    q("task").placeholder = say("Describe the background task"); q("context").placeholder = say("Add only context you intend to send");
    q("lookup").disabled = !selected;
    q("identity").textContent = selected || "";
    q("status").textContent = say(status); q("capability").textContent = say(capability); drawCapacity();
    note(notice); if (shownJob) result(shownJob); drawHistory();
    controls();
  }
  ctx.slots.add("page", {key:"dot", title:"Dot background tasks", mount, open:refresh, relang});
});
