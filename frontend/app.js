const API_BASE = window.SUPPLIER_API_BASE || "http://localhost:8082";

const state = {
  suppliers: [],
  categories: [],
  isAdmin: false,
};

// ---------- view switching ----------

function showView(name) {
  document.querySelectorAll(".view").forEach((el) => el.classList.add("hidden"));
  document.getElementById(`view-${name}`).classList.remove("hidden");
  document.querySelectorAll(".nav-link[data-view]").forEach((el) => {
    el.classList.toggle("active", el.dataset.view === name);
  });
  if (name === "suppliers") loadSuppliers();
}

document.querySelectorAll("[data-view]").forEach((el) => {
  el.addEventListener("click", () => showView(el.dataset.view));
});

// ---------- toast ----------

let toastTimer;
function showToast(message, type = "success") {
  const el = document.getElementById("toast");
  el.textContent = message;
  el.className = `toast ${type}`;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.add("hidden"), 3000);
}

// ---------- API helpers ----------

async function apiFetch(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (state.isAdmin) headers["X-User-Role"] = "ADMIN";
  const res = await fetch(`${API_BASE}${path}`, { ...options, headers });
  const body = await res.json().catch(() => null);
  if (!res.ok) {
    const message = (body && body.error) || `Request failed (${res.status})`;
    throw new Error(message);
  }
  return body;
}

// ---------- suppliers: list ----------

const statusEl = document.getElementById("supplier-status");
const gridEl = document.getElementById("supplier-grid");

function setStatus(message, type) {
  if (!message) {
    statusEl.classList.add("hidden");
    return;
  }
  statusEl.textContent = message;
  statusEl.className = `status ${type || ""}`;
  statusEl.classList.remove("hidden");
}

async function loadSuppliers() {
  setStatus("Loading suppliers…", "loading");
  const params = new URLSearchParams();
  const search = document.getElementById("search-input").value.trim();
  const category = document.getElementById("category-select").value;
  if (search) params.set("q", search);
  if (category) params.set("category", category);

  try {
    const suppliers = await apiFetch(`/suppliers${params.toString() ? "?" + params.toString() : ""}`);
    state.suppliers = suppliers || [];
    setStatus(state.suppliers.length ? "" : "No suppliers match your search.", "");
    renderCategoryOptions(state.suppliers);
    renderGrid(state.suppliers);
  } catch (err) {
    setStatus(`Couldn't load suppliers: ${err.message}. Is supplier-service running at ${API_BASE}?`, "error");
    gridEl.innerHTML = "";
  }
}

function renderCategoryOptions(suppliers) {
  const select = document.getElementById("category-select");
  const current = select.value;
  const categories = Array.from(new Set(state.allCategories || [])).sort();
  // Keep the full category list stable even while a filter/search narrows results.
  if (!state.allCategories) {
    state.allCategories = Array.from(new Set(suppliers.map((s) => s.type))).sort();
  }
  select.innerHTML = '<option value="">All categories</option>' +
    state.allCategories.map((c) => `<option value="${escapeHtml(c)}">${escapeHtml(c)}</option>`).join("");
  select.value = current;
}

function renderGrid(suppliers) {
  gridEl.innerHTML = suppliers.map(supplierCardHtml).join("");
  gridEl.querySelectorAll(".supplier-card").forEach((card) => {
    card.addEventListener("click", () => openDetail(card.dataset.id));
  });
}

function supplierCardHtml(s) {
  const img = s.image_url
    ? `<div class="supplier-card-img" style="background-image:url('${escapeAttr(s.image_url)}')"></div>`
    : `<div class="supplier-card-img">🏬</div>`;
  return `
    <div class="supplier-card" data-id="${escapeAttr(s.id)}">
      ${img}
      <div class="supplier-card-body">
        <span class="badge badge-type">${escapeHtml(s.type)}</span>
        <span class="supplier-card-name">${escapeHtml(s.name)}</span>
        <span class="supplier-card-meta">${escapeHtml(s.building)}${s.floor ? " · Floor " + escapeHtml(s.floor) : ""}</span>
        <span class="supplier-card-meta">${escapeHtml(s.location_description)}</span>
        <span class="badge ${s.is_available ? "badge-available" : "badge-unavailable"}">
          ${s.is_available ? "Available" : "Unavailable"}
        </span>
      </div>
    </div>`;
}

document.getElementById("search-input").addEventListener("input", debounce(loadSuppliers, 300));
document.getElementById("category-select").addEventListener("change", loadSuppliers);

document.getElementById("admin-toggle").addEventListener("change", (e) => {
  state.isAdmin = e.target.checked;
  document.getElementById("add-supplier-btn").classList.toggle("hidden", !state.isAdmin);
  renderGrid(state.suppliers); // no-op re-render, keeps things consistent
});

document.getElementById("add-supplier-btn").addEventListener("click", () => openForm());

// ---------- modal plumbing ----------

const overlay = document.getElementById("modal-overlay");
const modalBody = document.getElementById("modal-body");

function openModal(html) {
  modalBody.innerHTML = html;
  overlay.classList.remove("hidden");
}
function closeModal() {
  overlay.classList.add("hidden");
  modalBody.innerHTML = "";
}
document.getElementById("modal-close").addEventListener("click", closeModal);
overlay.addEventListener("click", (e) => { if (e.target === overlay) closeModal(); });

// ---------- detail view ----------

function openDetail(id) {
  const s = state.suppliers.find((x) => x.id === id);
  if (!s) return;

  const image = s.image_url
    ? `<img class="detail-img" src="${escapeAttr(s.image_url)}" alt="${escapeAttr(s.name)}" />`
    : `<div class="detail-img detail-img-placeholder">🏬</div>`;

  openModal(`
    ${image}
    <h2 class="detail-title">${escapeHtml(s.name)}</h2>
    <span class="badge badge-type">${escapeHtml(s.type)}</span>
    <div class="detail-row"><strong>Location:</strong> ${escapeHtml(s.building)}${s.floor ? ", Floor " + escapeHtml(s.floor) : ""} — ${escapeHtml(s.location_description)}</div>
    <div class="detail-row"><strong>Hours:</strong> ${escapeHtml(s.opening_time) || "–"} to ${escapeHtml(s.closing_time) || "–"}</div>
    <div class="detail-row"><strong>Coordinates:</strong> ${s.latitude}, ${s.longitude}</div>
    ${s.description ? `<div class="detail-row"><strong>Description:</strong> ${escapeHtml(s.description)}</div>` : ""}
    <div class="detail-row"><strong>Status:</strong> <span class="badge ${s.is_available ? "badge-available" : "badge-unavailable"}">${s.is_available ? "Available" : "Unavailable"}</span></div>
    ${state.isAdmin ? `
      <div class="modal-actions">
        <button class="btn btn-secondary" id="edit-btn">Edit</button>
        <button class="btn btn-danger" id="delete-btn">Delete</button>
      </div>` : ""}
  `);

  if (state.isAdmin) {
    document.getElementById("edit-btn").addEventListener("click", () => openForm(s));
    document.getElementById("delete-btn").addEventListener("click", () => deleteSupplier(s));
  }
}

// ---------- create / edit form ----------

function openForm(existing) {
  const s = existing || {
    name: "", type: "", building: "", floor: "", location_description: "",
    latitude: "", longitude: "", opening_time: "", closing_time: "",
    image_url: "", description: "", is_available: true,
  };

  openModal(`
    <h2 class="detail-title">${existing ? "Edit Supplier" : "Add Supplier"}</h2>
    <form id="supplier-form">
      <div class="form-row"><label>Name</label><input name="name" required value="${escapeAttr(s.name)}" /></div>
      <div class="form-row-grid">
        <div class="form-row"><label>Type / category</label><input name="type" required value="${escapeAttr(s.type)}" /></div>
        <div class="form-row"><label>Building</label><input name="building" required value="${escapeAttr(s.building)}" /></div>
      </div>
      <div class="form-row-grid">
        <div class="form-row"><label>Floor</label><input name="floor" value="${escapeAttr(s.floor)}" /></div>
        <div class="form-row"><label>Location description</label><input name="location_description" required value="${escapeAttr(s.location_description)}" /></div>
      </div>
      <div class="form-row-grid">
        <div class="form-row"><label>Latitude</label><input name="latitude" type="number" step="any" required value="${escapeAttr(s.latitude)}" /></div>
        <div class="form-row"><label>Longitude</label><input name="longitude" type="number" step="any" required value="${escapeAttr(s.longitude)}" /></div>
      </div>
      <div class="form-row-grid">
        <div class="form-row"><label>Opening time (HH:MM)</label><input name="opening_time" placeholder="09:00" value="${escapeAttr(s.opening_time)}" /></div>
        <div class="form-row"><label>Closing time (HH:MM)</label><input name="closing_time" placeholder="18:00" value="${escapeAttr(s.closing_time)}" /></div>
      </div>
      <div class="form-row"><label>Image URL</label><input name="image_url" value="${escapeAttr(s.image_url)}" /></div>
      <div class="form-row"><label>Description</label><input name="description" value="${escapeAttr(s.description)}" /></div>
      <div class="form-row checkbox-row">
        <input type="checkbox" name="is_available" id="is_available" ${s.is_available ? "checked" : ""} />
        <label for="is_available" style="margin:0;">Available</label>
      </div>
      <div class="modal-actions">
        <button type="submit" class="btn btn-primary">${existing ? "Save changes" : "Create supplier"}</button>
        <button type="button" class="btn btn-secondary" id="cancel-form">Cancel</button>
      </div>
    </form>
  `);

  document.getElementById("cancel-form").addEventListener("click", closeModal);
  document.getElementById("supplier-form").addEventListener("submit", (e) => {
    e.preventDefault();
    submitForm(e.target, existing);
  });
}

async function submitForm(form, existing) {
  const data = new FormData(form);
  const payload = {
    name: data.get("name"),
    type: data.get("type"),
    building: data.get("building"),
    floor: data.get("floor"),
    location_description: data.get("location_description"),
    latitude: parseFloat(data.get("latitude")),
    longitude: parseFloat(data.get("longitude")),
    opening_time: data.get("opening_time"),
    closing_time: data.get("closing_time"),
    image_url: data.get("image_url"),
    description: data.get("description"),
    is_available: data.get("is_available") === "on",
  };

  try {
    if (existing) {
      await apiFetch(`/suppliers/${existing.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      showToast("Supplier updated");
    } else {
      await apiFetch("/suppliers", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      showToast("Supplier created");
    }
    closeModal();
    loadSuppliers();
  } catch (err) {
    showToast(err.message, "error");
  }
}

async function deleteSupplier(s) {
  if (!confirm(`Delete "${s.name}"? This soft-deletes the record.`)) return;
  try {
    await apiFetch(`/suppliers/${s.id}`, { method: "DELETE" });
    showToast("Supplier deleted");
    closeModal();
    loadSuppliers();
  } catch (err) {
    showToast(err.message, "error");
  }
}

// ---------- utils ----------

function debounce(fn, delay) {
  let t;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), delay);
  };
}

function escapeHtml(v) {
  return String(v ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}
function escapeAttr(v) {
  return escapeHtml(v);
}

// ---------- init ----------

showView("home");
