import './style.css';
import {
  GetState, UIReady, CreateShare, StopShare, RemoveShare, OpenLink,
  AcceptTransfer, DeclineTransfer, CancelTransfer, DismissTransfer, ClearFinished,
  PickFiles, PickFolder, ChooseDownloadDir, Reveal, CopyText,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

const $ = (sel) => document.querySelector(sel);
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const icon = (name, cls = '') => `<svg class="i ${cls}"><use href="#i-${name}"/></svg>`;

// ---------- formatting ----------

function fmtBytes(n) {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), u.length - 1);
  const v = n / 1024 ** i;
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1).replace('.', ',')} ${u[i]}`;
}
const fmtSpeed = (bps) => (bps > 0 ? `${fmtBytes(bps)}/s` : '');
function fmtEta(t) {
  if (!(t.speed > 0) || t.done >= t.total) return '';
  const s = Math.round((t.total - t.done) / t.speed);
  if (s < 60) return `noch ${s} s`;
  if (s < 3600) return `noch ${Math.round(s / 60)} min`;
  return `noch ${Math.floor(s / 3600)} h ${Math.round((s % 3600) / 60)} min`;
}
const fileCount = (n) => (n === 1 ? '1 Datei' : `${n} Dateien`);
function ago(ms) {
  const s = Math.round((Date.now() - ms) / 1000);
  if (s < 60) return 'gerade eben';
  if (s < 3600) return `vor ${Math.round(s / 60)} min`;
  return `vor ${Math.round(s / 3600)} h`;
}

const baseName = (p) => (p || 'Downloads').replace(/[\\/]+$/, '').split(/[\\/]/).pop() || p;

// ---------- toasts ----------

function toast(text, kind = 'ok') {
  const el = document.createElement('div');
  el.className = `toast ${kind}`;
  el.innerHTML = `${icon(kind === 'error' ? 'x' : 'check')}<span>${esc(text)}</span>`;
  $('#toasts').append(el);
  setTimeout(() => { el.classList.add('out'); setTimeout(() => el.remove(), 300); }, kind === 'error' ? 5000 : 2400);
}
const guard = (fn) => async (...a) => {
  try { return await fn(...a); } catch (e) { toast(String(e?.message ?? e), 'error'); }
};

// ---------- keyed list rendering ----------
// Cards are kept across updates (so hover, focus and clicks survive the
// five-per-second state pushes); a card's markup is rebuilt only when its
// structural signature changes, and the fast-moving bits are patched.

function reconcile(container, items, { sig, build, patch, empty }) {
  const existing = new Map([...container.children].filter((c) => c.dataset.id).map((c) => [c.dataset.id, c]));
  const emptyEl = container.querySelector('.empty');
  if (!items.length) {
    existing.forEach((c) => c.remove());
    if (!emptyEl) container.insertAdjacentHTML('beforeend', empty);
    return;
  }
  emptyEl?.remove();
  let prev = null;
  for (const it of items) {
    let el = existing.get(it.id);
    existing.delete(it.id);
    const s = sig(it);
    if (!el) {
      el = document.createElement('div');
      el.dataset.id = it.id;
    }
    if (el.dataset.sig !== s) {
      el.dataset.sig = s;
      build(el, it);
    }
    patch?.(el, it);
    const want = prev ? prev.nextSibling : container.firstChild;
    if (el !== want) container.insertBefore(el, want);
    prev = el;
  }
  existing.forEach((c) => c.remove());
}

const set = (el, sel, text) => { const n = el.querySelector(sel); if (n && n.textContent !== text) n.textContent = text; };

// ---------- shares ----------

function shareStatus(s) {
  switch (s.status) {
    case 'starting': return `<span class="pill accent live">Startet</span>`;
    case 'ready':
      return s.active > 0
        ? `<span class="pill ok live">${s.active} aktiv</span>`
        : `<span class="pill ok live">Online</span>`;
    case 'error': return `<span class="pill bad">Fehler</span>`;
    default: return `<span class="pill">Beendet</span>`;
  }
}

function buildShare(el, s) {
  el.className = 'card';
  let body = '';
  if (s.status === 'starting') {
    body = `<div class="skeleton">Sicherer Tunnel wird aufgebaut …</div>`;
  } else if (s.status === 'ready') {
    body = `<div class="linkbox"><code title="${esc(s.link)}">${esc(s.link)}</code>
      <button class="btn primary small" data-act="copy">${icon('copy', 'sm')}<span>Link kopieren</span></button></div>`;
  } else if (s.status === 'error') {
    body = `<div class="card-sub" style="margin-top:10px"><span class="err">${esc(s.error)}</span></div>`;
  }
  const live = s.status === 'starting' || s.status === 'ready';
  el.innerHTML = `
    <div class="card-row">
      <div class="badge-icon">${icon(s.fileCount > 1 ? 'folder' : 'file')}</div>
      <div class="card-main">
        <div class="card-title" title="${esc(s.title)}">${esc(s.title)}</div>
        <div class="card-sub">
          <span>${fileCount(s.fileCount)} · ${fmtBytes(s.total)}</span><span class="sep">·</span>
          <span data-f="status">${shareStatus(s)}</span>
          ${s.oneTime ? `<span class="pill accent">Einmal-Link</span>` : ''}
          <span data-f="dl"></span>
        </div>
      </div>
      <div class="card-actions">
        ${live
          ? `<button class="icon-btn" data-act="stop" title="Freigabe beenden">${icon('stop')}</button>`
          : `<button class="icon-btn" data-act="remove" title="Entfernen">${icon('x')}</button>`}
      </div>
    </div>${body}`;
  el.querySelector('[data-act=copy]')?.addEventListener('click', guard(async (ev) => {
    await CopyText(s.link);
    const b = ev.currentTarget;
    b.classList.add('copied');
    b.querySelector('span').textContent = 'Kopiert!';
    setTimeout(() => { b.classList.remove('copied'); b.querySelector('span').textContent = 'Link kopieren'; }, 1600);
  }));
  el.querySelector('[data-act=stop]')?.addEventListener('click', guard(() => StopShare(s.id)));
  el.querySelector('[data-act=remove]')?.addEventListener('click', guard(() => RemoveShare(s.id)));
}

function patchShare(el, s) {
  const st = el.querySelector('[data-f=status]');
  const html = shareStatus(s);
  if (st && st.innerHTML !== html) st.innerHTML = html;
  set(el, '[data-f=dl]', s.downloads ? `· ${s.downloads}× heruntergeladen` : '');
}

// ---------- transfers ----------

const FINISHED = new Set(['done', 'declined', 'error', 'cancelled']);

function statusText(t) {
  const recv = t.direction === 'recv';
  switch (t.status) {
    case 'connecting': return recv ? 'Verbinde mit Absender …' : 'Verbindung wird aufgebaut …';
    case 'awaiting': return 'Möchtest du diese Dateien empfangen?';
    case 'waiting': return 'Empfänger entscheidet …';
    case 'transferring': return recv ? 'Empfange …' : 'Sende …';
    case 'verifying': return 'Wird abgeschlossen …';
    case 'done': return recv ? 'Gespeichert' : 'Zugestellt';
    case 'declined': return recv ? 'Abgelehnt' : 'Vom Empfänger abgelehnt';
    case 'cancelled': return t.error || 'Abgebrochen';
    case 'error': return t.error || 'Fehler';
    default: return t.status;
  }
}

function routePill(t) {
  if (!t.route || FINISHED.has(t.status)) return '';
  return t.route === 'direct'
    ? `<span class="pill ok" title="Direkte Peer-to-Peer-Verbindung (${esc(t.routeInfo)})">${icon('bolt')}Direkt</span>`
    : `<span class="pill warn" title="Über Relay ${esc(t.routeInfo)} – wird direkt, sobald möglich">${icon('relay')}Relay</span>`;
}

function buildTransfer(el, t) {
  const recv = t.direction === 'recv';
  const fin = FINISHED.has(t.status);
  el.className = `card${t.status === 'awaiting' ? ' attention' : ''}`;

  let extra = '';
  if (t.status === 'awaiting') {
    const rows = (t.files || []).map((f) => `<li><span title="${esc(f.path)}">${esc(f.path)}</span><span>${fmtBytes(f.size)}</span></li>`).join('');
    const more = t.fileCount > (t.files || []).length ? `<li class="more">… und ${t.fileCount - t.files.length} weitere</li>` : '';
    extra = `<ul class="files">${rows}${more}</ul>
      <div class="decide">
        <button class="btn ghost" data-act="decline">Ablehnen</button>
        <button class="btn primary" data-act="accept">${icon('download')}Annehmen</button>
      </div>`;
  } else if (!fin || t.status === 'done') {
    const indet = t.status === 'connecting' || t.status === 'waiting';
    extra = `<div class="progress${indet ? ' indeterminate' : ''}${t.status === 'done' ? ' done' : ''}"><div data-f="bar"></div></div>
      <div class="progress-meta"><span data-f="bytes"></span><span data-f="rate"></span></div>`;
  }

  let actions = '';
  if (t.status === 'done' && recv && t.savedTo) {
    actions += `<button class="btn ghost small" data-act="reveal">${icon('eye', 'sm')}Anzeigen</button>`;
  }
  actions += fin
    ? `<button class="icon-btn" data-act="dismiss" title="Aus der Liste entfernen">${icon('x')}</button>`
    : t.status !== 'awaiting' ? `<button class="icon-btn" data-act="cancel" title="Abbrechen">${icon('stop')}</button>` : '';

  const subCls = t.status === 'error' ? 'err' : '';
  el.innerHTML = `
    <div class="card-row">
      <div class="badge-icon ${recv ? 'recv' : ''}">${icon(recv ? 'download' : 'upload')}</div>
      <div class="card-main">
        <div class="card-title" title="${esc(t.title)}">${esc(t.title)}</div>
        <div class="card-sub">
          <span class="${subCls}" data-f="status">${esc(statusText(t))}</span>
          <span data-f="route"></span>
        </div>
      </div>
      <div class="card-actions">${actions}</div>
    </div>${extra}`;

  const on = (act, fn) => el.querySelector(`[data-act=${act}]`)?.addEventListener('click', guard(fn));
  on('accept', () => AcceptTransfer(t.id));
  on('decline', () => DeclineTransfer(t.id));
  on('cancel', () => CancelTransfer(t.id));
  on('dismiss', () => DismissTransfer(t.id));
  on('reveal', () => Reveal(t.savedTo));
}

function patchTransfer(el, t) {
  const pct = t.total > 0 ? Math.min(100, (t.done / t.total) * 100) : (t.status === 'done' ? 100 : 0);
  const bar = el.querySelector('[data-f=bar]');
  if (bar) bar.style.width = `${pct}%`;
  const fin = FINISHED.has(t.status);
  set(el, '[data-f=bytes]', t.total ? `${fmtBytes(t.done)} von ${fmtBytes(t.total)} · ${fileCount(t.fileCount)}` : '');
  set(el, '[data-f=rate]', fin
    ? ago(t.startedAt)
    : [fmtSpeed(t.speed), fmtEta(t)].filter(Boolean).join(' · '));
  const r = el.querySelector('[data-f=route]');
  const html = routePill(t);
  if (r && r.innerHTML !== html) r.innerHTML = html;
}

// ---------- state ----------

let state = { shares: [], transfers: [], settings: {} };
const seenAwaiting = new Set();

function render(st) {
  state = st;
  document.body.classList.add(`platform-${st.platform}`);
  set(document, '#dl-dir-label', baseName(st.settings.downloadDir));
  $('#dl-dir').title = `Empfangene Dateien landen in:\n${st.settings.downloadDir}\n\nKlicken zum Ändern`;

  reconcile($('#shares'), st.shares, {
    sig: (s) => `${s.status}|${s.link}|${s.active > 0}`,
    build: buildShare,
    patch: patchShare,
    empty: `<div class="empty"><strong>Noch nichts geteilt</strong>Zieh Dateien in das Feld oben, um einen Link zu erzeugen.</div>`,
  });
  reconcile($('#transfers'), st.transfers, {
    sig: (t) => `${t.status}|${t.title}|${!!t.savedTo}`,
    build: buildTransfer,
    patch: patchTransfer,
    empty: `<div class="empty"><strong>Keine Übertragungen</strong>Hier erscheinen Downloads deiner Freunde und Dateien, die du empfängst.</div>`,
  });
  $('#clear-finished').hidden = !st.transfers.some((t) => FINISHED.has(t.status));

  for (const t of st.transfers) {
    if (t.status === 'awaiting' && !seenAwaiting.has(t.id)) {
      seenAwaiting.add(t.id);
      document.querySelector(`#transfers [data-id="${t.id}"]`)?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }
  }
}

// ---------- actions ----------

async function share(paths) {
  if (!paths?.length) return;
  await CreateShare(paths, $('#one-time').checked);
  toast('Freigabe wird erstellt – der Link erscheint gleich');
}

$('#pick-files').addEventListener('click', guard(async () => share(await PickFiles())));
$('#pick-folder').addEventListener('click', guard(async () => {
  const p = await PickFolder();
  if (p) await share([p]);
}));
$('#dl-dir').addEventListener('click', guard(async () => {
  const s = await ChooseDownloadDir();
  state.settings = s;
  set(document, '#dl-dir-label', baseName(s.downloadDir));
  toast(`Empfangene Dateien landen jetzt in ${s.downloadDir}`);
}));
$('#clear-finished').addEventListener('click', guard(() => ClearFinished()));

$('#receive-form').addEventListener('submit', guard(async (ev) => {
  ev.preventDefault();
  const input = $('#link-input');
  if (!input.value.trim()) { input.focus(); return; }
  await OpenLink(input.value);
  input.value = '';
}));

// Pasting a link anywhere fills the receive field.
document.addEventListener('paste', (ev) => {
  if (document.activeElement === $('#link-input')) return;
  const text = ev.clipboardData?.getData('text') ?? '';
  if (/^\s*katzensprung:/i.test(text)) {
    $('#link-input').value = text.trim();
    $('#link-input').focus();
  }
});

// Drag & drop: Wails delivers the dropped paths; the classes are cosmetic.
const drop = $('#drop');
let dragDepth = 0;
window.addEventListener('dragenter', (e) => { e.preventDefault(); dragDepth++; drop.classList.add('over'); });
window.addEventListener('dragleave', () => { if (--dragDepth <= 0) { dragDepth = 0; drop.classList.remove('over'); } });
window.addEventListener('dragover', (e) => e.preventDefault());
window.addEventListener('drop', (e) => { e.preventDefault(); dragDepth = 0; drop.classList.remove('over'); });

EventsOn('files-dropped', guard((paths) => share(paths)));
EventsOn('state', render);
EventsOn('toast', (m) => toast(m.text, m.kind));

// Refresh the "vor x min" labels now and then.
setInterval(() => state.platform && render(state), 30000);

(async () => {
  render(await GetState());
  await UIReady();
})();
