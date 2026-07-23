(async function () {
  const App = window.AdminApp;
  let filtersCtrl = null;

  async function loadMeta() {
    const qs = window.location.search;
    const [summaryRes, rangeRes, expRes] = await Promise.all([
      App.api('/api/summary' + qs),
      App.api('/api/date-range' + qs),
      App.api('/api/experiments'),
    ]);
    for (const res of [summaryRes, rangeRes, expRes]) {
      if (!res.ok) throw new Error(await res.text());
    }
    const summary = await summaryRes.json();
    const dateRange = await rangeRes.json();
    const experiments = await expRes.json();
    if (!filtersCtrl) {
      filtersCtrl = AdminFilters.mountFilters(
        document.getElementById('filters-root'),
        { dateRange, tradeCount: summary.trade_count, experiments },
        () => { loadMeta(); loadPrompts(); },
      );
    } else {
      filtersCtrl.updateMeta({ dateRange, tradeCount: summary.trade_count });
    }
  }

  function joinQS(mode) {
    const p = App.filterParamsFromURL();
    p.set('mode', mode);
    const s = p.toString();
    return s ? '?' + s : '';
  }

  async function copyText(text) {
    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(text);
        return true;
      } catch (_) {}
    }
    const ta = document.createElement('textarea');
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    try {
      return document.execCommand('copy');
    } catch (_) {
      return false;
    } finally {
      document.body.removeChild(ta);
    }
  }

  function flashButton(btn, okText, ms = 2000) {
    const orig = btn.textContent;
    btn.textContent = okText;
    btn.disabled = true;
    setTimeout(() => {
      btn.textContent = orig;
      btn.disabled = false;
    }, ms);
  }

  async function loadPrompts() {
    for (const mode of ['summary', 'detailed']) {
      const preview = document.querySelector('.prompt-preview[data-mode="' + mode + '"]');
      try {
        const res = await App.api('/api/prompt' + joinQS(mode));
        const data = await res.json();
        preview.value = data.prompt || '';
      } catch (e) {
        preview.value = 'Ошибка загрузки: ' + e;
      }
    }
  }

  document.querySelectorAll('.copy-prompt-btn').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const mode = btn.dataset.mode;
      const preview = document.querySelector('.prompt-preview[data-mode="' + mode + '"]');
      const ok = await copyText(preview.value);
      flashButton(btn, ok ? '✓ Скопировано' : 'Ошибка');
    });
  });

  document.querySelectorAll('.download-data-btn').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const mode = btn.dataset.mode;
      const filename = mode === 'detailed' ? 'data-trades.json' : 'data-summary.json';
      try {
        await App.downloadJSON('/api/export/data' + joinQS(mode), filename);
      } catch (e) {
        flashButton(btn, 'Ошибка');
        console.error(e);
      }
    });
  });

  try {
    await loadMeta();
    await loadPrompts();
  } catch (e) {
    console.error(e);
  }
})();
