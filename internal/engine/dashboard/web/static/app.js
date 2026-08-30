(function (global) {
  const TOKEN_KEY = 'bcs_admin_token';

  function getToken() {
    return sessionStorage.getItem(TOKEN_KEY) || '';
  }

  function setToken(token) {
    if (token) {
      sessionStorage.setItem(TOKEN_KEY, token);
    } else {
      sessionStorage.removeItem(TOKEN_KEY);
    }
  }

  function ensureTokenUI() {
    let overlay = document.getElementById('auth-overlay');
    if (overlay) return overlay;
    overlay = document.createElement('div');
    overlay.id = 'auth-overlay';
    overlay.className = 'modal-overlay';
    overlay.innerHTML = `
      <div class="modal" role="dialog" aria-labelledby="auth-title">
        <h2 id="auth-title">Доступ к админке</h2>
        <p class="muted">Введите ADMIN_TOKEN бота</p>
        <label class="modal-label">Токен
          <input type="password" id="auth-token-input" autocomplete="current-password">
        </label>
        <div class="modal-actions">
          <button type="button" class="btn primary" id="auth-save">Войти</button>
        </div>
        <p class="modal-error muted" id="auth-error" hidden></p>
      </div>`;
    document.body.appendChild(overlay);
    return overlay;
  }

  function promptToken(force) {
    return new Promise((resolve) => {
      if (!force && getToken()) {
        resolve(getToken());
        return;
      }
      const overlay = ensureTokenUI();
      const input = document.getElementById('auth-token-input');
      const save = document.getElementById('auth-save');
      const err = document.getElementById('auth-error');
      err.hidden = true;
      overlay.hidden = false;
      input.value = getToken();
      input.focus();

      const finish = () => {
        const value = input.value.trim();
        if (!value) {
          err.textContent = 'Токен обязателен';
          err.hidden = false;
          return;
        }
        setToken(value);
        overlay.hidden = true;
        resolve(value);
      };
      save.onclick = finish;
      input.onkeydown = (e) => {
        if (e.key === 'Enter') finish();
      };
    });
  }

  async function api(url, opts = {}) {
    const headers = Object.assign({}, opts.headers || {});
    const token = getToken();
    if (token) {
      headers.Authorization = 'Bearer ' + token;
    }
    let res = await fetch(url, Object.assign({}, opts, { headers }));
    if (res.status === 401) {
      await promptToken(true);
      headers.Authorization = 'Bearer ' + getToken();
      res = await fetch(url, Object.assign({}, opts, { headers }));
    }
    return res;
  }

  function filterParamsFromURL() {
    return new URLSearchParams(window.location.search);
  }

  function withFilterQS(path, extra = {}) {
    const p = filterParamsFromURL();
    Object.entries(extra).forEach(([k, v]) => {
      if (v === undefined || v === null || v === '') p.delete(k);
      else p.set(k, String(v));
    });
    const s = p.toString();
    return s ? path + '?' + s : path;
  }

  function pnlClass(v) {
    if (v > 0) return 'positive';
    if (v < 0) return 'negative';
    return '';
  }

  function fmtMoney(v) {
    return Number(v).toFixed(2);
  }

  function fmtPct(v) {
    return Number(v).toFixed(1) + '%';
  }

  function fmtHold(sec) {
    sec = Math.round(Number(sec) || 0);
    if (sec <= 0) return '—';
    if (sec < 60) return sec + 'с';
    if (sec < 3600) return Math.floor(sec / 60) + 'м';
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    return m === 0 ? h + 'ч' : h + 'ч ' + String(m).padStart(2, '0') + 'м';
  }

  function fmtTime(iso) {
    if (!iso) return '—';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '—';
    const dd = String(d.getDate()).padStart(2, '0');
    const mm = String(d.getMonth() + 1).padStart(2, '0');
    const hh = String(d.getHours()).padStart(2, '0');
    const mi = String(d.getMinutes()).padStart(2, '0');
    return dd + '.' + mm + ' ' + hh + ':' + mi;
  }

  // Всегда Europe/Moscow (как ось графика сделок / open-chart), без зависимости от TZ браузера.
  function fmtTimeMSK(iso) {
    if (!iso) return '—';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '—';
    return d.toLocaleString('ru-RU', {
      timeZone: 'Europe/Moscow',
      day: '2-digit',
      month: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
  }

  function syncNavLinks() {
    const qs = window.location.search;
    document.querySelectorAll('.topbar nav a').forEach((a) => {
      const url = new URL(a.getAttribute('href'), window.location.origin);
      a.href = url.pathname + qs;
    });
  }

  async function downloadJSON(url, filename) {
    const res = await api(url);
    if (!res.ok) throw new Error(await res.text());
    const blob = await res.blob();
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(a.href);
  }

  global.AdminApp = {
    getToken,
    setToken,
    promptToken,
    api,
    filterParamsFromURL,
    withFilterQS,
    pnlClass,
    fmtMoney,
    fmtPct,
    fmtHold,
    fmtTime,
    fmtTimeMSK,
    syncNavLinks,
    downloadJSON,
  };

  syncNavLinks();
})(window);
