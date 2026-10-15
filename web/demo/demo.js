"use strict";
(async function () {
  const view = document.getElementById("view");
  const server = document.getElementById("server");
  const announcement = document.getElementById("announcement");
  let step = "schema", scenario = "match", data;
  const escape = value => String(value).replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
  const pretty = value => escape(JSON.stringify(value, null, 2));
  const artifact = name => `artifacts/postgres-${server.value}/${name}`;
  const download = (name, label) => `<a href="${artifact(name)}" download>${escape(label)}</a>`;
  const raw = (label, value) => `<details class="raw"><summary>${escape(label)}</summary><pre>${pretty(value)}</pre></details>`;
  function schemaCard(doc, title, badge) {
    return `<div class="card"><h3>${escape(title)} <span class="tag">${badge}</span></h3>${doc.tables.map(t => `<div class="schema-table"><div class="table-name">${escape(t.name)}</div>${t.columns.map(c => `<div class="column"><span>${escape(c.name)}</span><span>${escape(c.type)}${c.nullable ? " · NULL" : " · required"}</span></div>`).join("")}<p class="key">Primary key: ${escape((t.primary_key || []).join(", ") || "none")}<br>Indexes: ${escape((t.indexes || []).map(i => `${i.name} (${i.columns.join(", ")})`).join("; ") || "none")}</p></div>`).join("")}</div>`;
  }
  function rowsCard(rows, after) {
    const headers = after ? ["customer_id", "full_name", "email", "notes"] : ["id", "customer_name", "email"];
    return `<div class="card"><h3>${after ? "After migration" : "Before migration"}</h3><div class="table-scroll"><table><caption class="sr-only">${after ? "Preserved customer records after migration" : "Original customer records"}</caption><thead><tr>${headers.map(h => `<th scope="col">${h}</th>`).join("")}</tr></thead><tbody>${rows.map(r => `<tr>${[r.id,r.name,r.email,...(after ? [null] : [])].map(v => `<td>${v === null ? '<span class="null">NULL</span>' : escape(v)}</td>`).join("")}</tr>`).join("")}</tbody></table></div></div>`;
  }
  function render() {
    const d = data.runs[server.value];
    document.querySelectorAll("[data-step]").forEach(b => b.setAttribute("aria-pressed", String(b.dataset.step === step)));
    document.getElementById("provenance").textContent = `Recorded with ForgeSchema ${d.run.cli_version} on ${d.run.postgresql_version}. Isolated demo schema was removed after the run; history was retained until database teardown.`;
    if (step === "schema") {
      view.innerHTML = `<h2 id="view-title">Keep the intent. Change the shape.</h2><p class="lede">Two explicit renames, a nullable notes column, an events table and two new indexes. The original customers stay in place.</p><div class="grid">${schemaCard(d.current,"Inspected source","BEFORE")}${schemaCard(d.desired,"Desired schema","TARGET")}</div><div class="callout">customer_name → full_name and id → customer_id are explicit renames. The primary key and existing index references follow the renamed columns.</div><div class="actions">${download("current.json","Download source JSON")}${download("desired.json","Download target JSON")}${download("renames.yaml","Download explicit renames")}</div>${raw("Inspect the source / target JSON",{source:d.current,target:d.desired})}`;
    } else if (step === "plan") {
      view.innerHTML = `<h2 id="view-title">Six operations. Every SQL statement visible.</h2><p class="lede">This ready plan was produced by the CLI and validated again before apply. Open an operation to read its explanation and exact SQL.</p>${d.plan.operations.map((op,i) => `<details class="op"${i===0 ? " open" : ""}><summary>${i+1}. ${escape(op.kind.replaceAll("_"," "))}${op.table ? " · "+escape(op.table) : ""}</summary><p>${escape(op.explanation || "")}</p><pre>${escape(op.sql)}</pre></details>`).join("")}<div class="callout">All six statements were applied in one transaction. A blocked plan contains no executable operations. External DDL must be paused for a real apply.</div><div class="actions">${download("reviewed-plan.json","Download reviewed plan")}${download("review.txt","Download readable SQL review")}</div>${raw("Inspect the complete format 1 artifact",d.plan)}`;
    } else if (step === "apply") {
      view.innerHTML = `<h2 id="view-title">New names. The same customer records.</h2><p class="lede">The harness read its two synthetic rows before and after the real CLI migration and compared the original values. New notes values were checked separately as NULL.</p><div class="grid">${rowsCard(d.before,false)}${rowsCard(d.after,true)}</div><div class="callout"><span class="tag">APPLIED</span> ${d.apply.operations} operations, two original rows preserved, one matching history record. The migration ID is <span class="fingerprint">${escape(d.apply.migration_id)}</span>.</div><details class="op"><summary>What happens if the same plan is applied again?</summary><p>The original source no longer matches. The second apply was rejected with exit 1.</p><pre>${escape(d.replay)}</pre></details><div class="actions">${download("before-rows.json","Download original rows")}${download("after-rows.json","Download preserved rows")}${download("apply-result.json","Download apply result")}</div>${raw("Inspect the apply result",d.apply)}`;
    } else {
      const drift = scenario === "drift", report = drift ? d.drift : d.match;
      view.innerHTML = `<h2 id="view-title">Can verification spot an extra column?</h2><p class="lede">Switch between two recorded checks: the matching target and the same schema after the harness added external_note. Verification only reads structure.</p><div class="scenario" role="group" aria-label="Recorded verification scenario"><button type="button" data-scenario="match" aria-pressed="${!drift}">Matching target</button><button type="button" data-scenario="drift" aria-pressed="${drift}">Extra column added</button></div><div class="callout${drift ? " warning" : ""}" role="status"><span class="tag${drift ? " warning" : ""}">${drift ? "DRIFT · EXIT 4" : "MATCH · EXIT 0"}</span> ${drift ? "customers.external_note is present but absent from the reviewed target. The report contains one column_unexpected difference." : "The inspected structure matches the reviewed target. The report contains zero differences."}</div><p class="fingerprint">Expected fingerprint: ${escape(report.expected_fingerprint)}<br>Actual fingerprint: ${escape(report.actual_fingerprint)}</p><pre aria-label="Recorded verification report">${pretty(report)}</pre><p class="lede">The before/after inspection artifacts agree: verification left the extra column untouched. Afterwards, the harness removed its injected column and recorded a final match.</p><div class="actions">${download(drift ? "verify-drift.json" : "verify-match.json","Download this report")}${download("before-drift-check.json","Before verification")}${download("after-drift-check.json","After verification")}${download("verify-final.json","Final restored match")}</div>`;
      view.querySelectorAll("[data-scenario]").forEach(b => b.addEventListener("click", () => { scenario=b.dataset.scenario; render(); view.querySelector(`[data-scenario="${scenario}"]`).focus(); }));
    }
    announcement.textContent = `${server.options[server.selectedIndex].text}: ${document.getElementById("view-title").textContent}${step === "verify" ? ", " + (scenario === "drift" ? "drift detected" : "target matches") : ""}`;
  }
  try {
    const response = await fetch("data.json");
    if (!response.ok) throw new Error("Unable to load recorded artifacts");
    data = await response.json();
    document.querySelectorAll("[data-step]").forEach(b => b.addEventListener("click", () => { step=b.dataset.step; render(); }));
    server.addEventListener("change", render);
    render();
  } catch (error) {
    view.innerHTML = '<h2 id="view-title">The recorded demo could not load.</h2><p>Reload the page, or <a href="https://github.com/ForgeSchema/forgeschema/tree/main/examples/walkthrough">explore the same artifacts on GitHub</a>.</p>';
    announcement.textContent = "Demo could not load.";
  }
}());
