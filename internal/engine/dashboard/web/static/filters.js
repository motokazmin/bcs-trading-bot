(function (global) {
  const App = global.AdminApp;

  function todayISODate() {
    const now = new Date();
    const yyyy = now.getFullYear();
    const mm = String(now.getMonth() + 1).padStart(2, '0');
    const dd = String(now.getDate()).padStart(2, '0');
    return `${yyyy}-${mm}-${dd}`;
  }

  function mountFilters(root, meta, onApplied) {
    const params = App.filterParamsFromURL();
    root.innerHTML = `
<form class="filters" method="get" id="filters-form"
      data-range-from="${meta.dateRange.from || ''}"
      data-range-to="${meta.dateRange.to || ''}"
      data-trade-count="${meta.tradeCount || 0}">
  <input type="hidden" name="period" id="period-intent" value="">
  <label>Период
    <select id="period-select">
      <option value="all">Все</option>
      <option value="today">Сегодня</option>
    </select>
  </label>
  <label>Эксперимент
    <select name="experiment_id" id="filter-experiment">
      <option value="">все</option>
    </select>
  </label>
  <label>Тикер <input name="ticker" id="filter-ticker" value="${params.get('ticker') || ''}" placeholder="SBER"></label>
  <label>С <input type="date" name="date_from" id="date-from" value="${params.get('date_from') || ''}"></label>
  <label>По <input type="date" name="date_to" id="date-to" value="${params.get('date_to') || ''}"></label>
  <button type="button" class="btn secondary" id="archive-btn" disabled>Архивировать</button>
  <button type="submit">Применить</button>
</form>
<div class="modal-overlay" id="archive-modal" hidden>
  <div class="modal" role="dialog" aria-labelledby="archive-modal-title">
    <h2 id="archive-modal-title">Архивировать период</h2>
    <p class="muted" id="archive-modal-period"></p>
    <label class="modal-label">Комментарий
      <textarea id="archive-comment" rows="4" placeholder="Заметка к периоду…"></textarea>
    </label>
    <div class="modal-actions">
      <button type="button" class="btn" id="archive-cancel">Отмена</button>
      <button type="button" class="btn primary" id="archive-save">Сохранить</button>
    </div>
    <p class="modal-error muted" id="archive-error" hidden></p>
  </div>
</div>`;

    const form = document.getElementById('filters-form');
    const periodSelect = document.getElementById('period-select');
    const periodIntent = document.getElementById('period-intent');
    const dateFrom = document.getElementById('date-from');
    const dateTo = document.getElementById('date-to');
    const archiveBtn = document.getElementById('archive-btn');
    const modal = document.getElementById('archive-modal');
    const modalPeriod = document.getElementById('archive-modal-period');
    const commentInput = document.getElementById('archive-comment');
    const cancelBtn = document.getElementById('archive-cancel');
    const saveBtn = document.getElementById('archive-save');
    const errorEl = document.getElementById('archive-error');
    const expSelect = document.getElementById('filter-experiment');

    (meta.experiments || []).forEach((id) => {
      const opt = document.createElement('option');
      opt.value = id;
      opt.textContent = id;
      if (params.get('experiment_id') === id) opt.selected = true;
      expSelect.appendChild(opt);
    });

    let archives = [];

    function periodFromURL() {
      return new URLSearchParams(window.location.search).get('period') || '';
    }

    function setPeriodIntent(value) {
      periodIntent.value = value || '';
    }

    function effectiveArchiveRange() {
      if (dateFrom.value && dateTo.value) {
        return { from: dateFrom.value, to: dateTo.value };
      }
      return {
        from: form.dataset.rangeFrom || '',
        to: form.dataset.rangeTo || '',
      };
    }

    function tradeCountOnScreen() {
      const n = parseInt(form.dataset.tradeCount || '0', 10);
      return Number.isFinite(n) ? n : 0;
    }

    function isValidPeriodIntent(intent) {
      if (!intent) return false;
      if (intent === 'all' || intent === 'today') return true;
      if (archives.some((a) => a.id === intent)) return true;
      return archives.length === 0;
    }

    function currentPeriodValue() {
      const intent = periodFromURL() || periodIntent.value;
      if (isValidPeriodIntent(intent)) return intent;
      if (!dateFrom.value && !dateTo.value) return 'all';
      const today = todayISODate();
      if (dateFrom.value === today && dateTo.value === today) return 'today';
      const archive = archives.find((a) => a.date_from === dateFrom.value && a.date_to === dateTo.value);
      return archive ? archive.id : '';
    }

    function updateArchiveBtn() {
      const range = effectiveArchiveRange();
      archiveBtn.disabled = !(range.from && range.to && tradeCountOnScreen() > 0);
    }

    function renderPeriodOptions() {
      const selected = currentPeriodValue();
      periodSelect.innerHTML = '';
      const allOpt = document.createElement('option');
      allOpt.value = 'all';
      allOpt.textContent = 'Все';
      periodSelect.appendChild(allOpt);
      const todayOpt = document.createElement('option');
      todayOpt.value = 'today';
      todayOpt.textContent = 'Сегодня';
      periodSelect.appendChild(todayOpt);
      if (archives.length > 0) {
        const group = document.createElement('optgroup');
        group.label = 'Архивы';
        for (const a of archives) {
          const opt = document.createElement('option');
          opt.value = a.id;
          opt.textContent = a.name;
          group.appendChild(opt);
        }
        periodSelect.appendChild(group);
      }
      if (selected && [...periodSelect.options].some((o) => o.value === selected)) {
        periodSelect.value = selected;
        setPeriodIntent(selected);
      } else if (!dateFrom.value && !dateTo.value) {
        periodSelect.value = 'all';
        setPeriodIntent('all');
      } else {
        periodSelect.selectedIndex = -1;
        if (!periodFromURL()) setPeriodIntent('');
      }
      updateArchiveBtn();
    }

    async function loadArchives() {
      try {
        const res = await App.api('/api/archives');
        if (!res.ok) return;
        const data = await res.json();
        archives = Array.isArray(data) ? data : [];
      } catch (_) {
        archives = [];
      }
      renderPeriodOptions();
    }

    function openModal() {
      const range = effectiveArchiveRange();
      if (!range.from || !range.to || tradeCountOnScreen() <= 0) return;
      modalPeriod.textContent = range.from + ' — ' + range.to;
      commentInput.value = '';
      errorEl.hidden = true;
      errorEl.textContent = '';
      modal.hidden = false;
      commentInput.focus();
    }

    function closeModal() {
      modal.hidden = true;
    }

    function applyPeriodAndSubmit(period, from, to) {
      if ([...periodSelect.options].some((o) => o.value === period)) {
        periodSelect.value = period;
      }
      setPeriodIntent(period);
      dateFrom.value = from;
      dateTo.value = to;
      updateArchiveBtn();
      form.requestSubmit();
    }

    periodSelect.addEventListener('change', () => {
      const value = periodSelect.value;
      if (value === 'all') {
        applyPeriodAndSubmit('all', '', '');
        return;
      }
      if (value === 'today') {
        const today = todayISODate();
        applyPeriodAndSubmit('today', today, today);
        return;
      }
      const archive = archives.find((a) => a.id === value);
      if (!archive) return;
      applyPeriodAndSubmit(archive.id, archive.date_from, archive.date_to);
    });

    function onManualDateEdit() {
      setPeriodIntent('');
      periodSelect.selectedIndex = -1;
      updateArchiveBtn();
    }

    dateFrom.addEventListener('change', onManualDateEdit);
    dateTo.addEventListener('change', onManualDateEdit);
    dateFrom.addEventListener('input', updateArchiveBtn);
    dateTo.addEventListener('input', updateArchiveBtn);

    form.addEventListener('submit', (e) => {
      e.preventDefault();
      if (periodSelect.selectedIndex < 0) setPeriodIntent('');
      const next = new URLSearchParams();
      const fd = new FormData(form);
      for (const [k, v] of fd.entries()) {
        if (String(v).trim() !== '') next.set(k, String(v).trim());
      }
      // preserve limit/offset only if caller wants — clear paging on filter apply
      const url = window.location.pathname + (next.toString() ? '?' + next.toString() : '');
      window.history.replaceState({}, '', url);
      App.syncNavLinks();
      if (typeof onApplied === 'function') onApplied();
    });

    archiveBtn.addEventListener('click', openModal);
    cancelBtn.addEventListener('click', closeModal);
    modal.addEventListener('click', (e) => {
      if (e.target === modal) closeModal();
    });

    saveBtn.addEventListener('click', async () => {
      const range = effectiveArchiveRange();
      errorEl.hidden = true;
      saveBtn.disabled = true;
      try {
        const res = await App.api('/api/archives', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            date_from: range.from,
            date_to: range.to,
            comment: commentInput.value,
          }),
        });
        if (!res.ok) {
          errorEl.textContent = (await res.text()) || 'Ошибка сохранения';
          errorEl.hidden = false;
          return;
        }
        closeModal();
        await loadArchives();
        // Заархивированный период уезжает из общей выборки — возвращаемся на «Все»,
        // иначе на экране остаётся период, которого в «Все» уже нет.
        applyPeriodAndSubmit('all', '', '');
      } catch (e) {
        errorEl.textContent = 'Ошибка: ' + e;
        errorEl.hidden = false;
      } finally {
        saveBtn.disabled = false;
      }
    });

    setPeriodIntent(periodFromURL());
    updateArchiveBtn();
    renderPeriodOptions();
    loadArchives();

    return {
      updateMeta(next) {
        form.dataset.rangeFrom = next.dateRange?.from || '';
        form.dataset.rangeTo = next.dateRange?.to || '';
        form.dataset.tradeCount = String(next.tradeCount || 0);
        updateArchiveBtn();
      },
    };
  }

  global.AdminFilters = { mountFilters };
})(window);
