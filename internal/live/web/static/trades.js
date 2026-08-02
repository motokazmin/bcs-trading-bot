(async function () {
  const App = window.AdminApp;
  const errEl = document.getElementById('page-error');
  const body = document.getElementById('trades-body');
  const pagerTop = document.getElementById('pager-top');
  const pagerBottom = document.getElementById('pager-bottom');
  let filtersCtrl = null;

  const perPageChoices = [25, 50, 100, 200];

  function showError(msg) {
    errEl.hidden = !msg;
    errEl.textContent = msg || '';
  }

  function tradesURL(limit, offset) {
    return App.withFilterQS('/trades', {
      limit,
      offset: offset > 0 ? offset : undefined,
    });
  }

  function renderPager(el, pag) {
    if (!pag.total) {
      el.innerHTML = '<p class="muted">Нет сделок</p>';
      return;
    }
    const pages = pag.pages.map((p) =>
      `<a class="pager-btn pager-num ${p.current ? 'current' : ''}" href="${p.href}" ${p.current ? 'aria-current="page"' : ''}>${p.page}</a>`
    ).join('');
    const sizeOpts = pag.perPage.map((o) =>
      `<option value="${o.href}" ${o.current ? 'selected' : ''}>${o.value}</option>`
    ).join('');
    el.innerHTML = `
<nav class="pager">
  <div class="pager-info">
    ${pag.empty
      ? `На странице ${pag.currentPage} нет записей — <a href="${pag.firstHref}">к первой странице</a>`
      : `${pag.from}–${pag.to} из ${pag.total}`}
  </div>
  <div class="pager-controls">
    <a class="pager-btn" href="${pag.firstHref}" ${!pag.hasPrev ? 'aria-disabled="true" tabindex="-1"' : ''}>«</a>
    <a class="pager-btn" href="${pag.prevHref}" ${!pag.hasPrev ? 'aria-disabled="true" tabindex="-1"' : ''}>‹</a>
    ${pages}
    <a class="pager-btn" href="${pag.nextHref}" ${!pag.hasNext ? 'aria-disabled="true" tabindex="-1"' : ''}>›</a>
    <a class="pager-btn" href="${pag.lastHref}" ${!pag.hasNext ? 'aria-disabled="true" tabindex="-1"' : ''}>»</a>
  </div>
  <label class="pager-size">
    На странице
    <select onchange="if(this.value) window.location.href=this.value">${sizeOpts}</select>
  </label>
</nav>`;
  }

  function buildPagination(limit, offset, result) {
    const total = result.total ?? result.Total ?? 0;
    const trades = result.trades || result.Trades || [];
    let totalPages = total > 0 ? Math.ceil(total / limit) : 0;
    let currentPage = Math.floor(offset / limit) + 1;
    if (totalPages > 0 && currentPage > totalPages) currentPage = totalPages;
    if (currentPage < 1) currentPage = 1;
    const from = trades.length ? offset + 1 : 0;
    const to = trades.length ? offset + trades.length : 0;
    const prevOffset = Math.max(0, offset - limit);
    const nextOffset = offset + limit;
    const lastOffset = Math.max(0, (totalPages - 1) * limit);
    const pages = [];
    if (totalPages > 0) {
      const window = 7;
      let start = currentPage - Math.floor(window / 2);
      if (start < 1) start = 1;
      let end = start + window - 1;
      if (end > totalPages) {
        end = totalPages;
        start = Math.max(1, end - window + 1);
      }
      for (let p = start; p <= end; p++) {
        const off = (p - 1) * limit;
        pages.push({ page: p, href: tradesURL(limit, off), current: p === currentPage });
      }
    }
    return {
      total, from, to, currentPage, totalPages,
      hasPrev: offset > 0,
      hasNext: offset + trades.length < total,
      empty: total > 0 && trades.length === 0,
      firstHref: tradesURL(limit, 0),
      prevHref: tradesURL(limit, prevOffset),
      nextHref: tradesURL(limit, nextOffset),
      lastHref: tradesURL(limit, lastOffset),
      pages,
      perPage: perPageChoices.map((v) => ({
        value: v,
        href: tradesURL(v, 0),
        current: v === limit,
      })),
    };
  }

  async function load() {
    showError('');
    const params = App.filterParamsFromURL();
    let limit = parseInt(params.get('limit') || '50', 10);
    let offset = parseInt(params.get('offset') || '0', 10);
    if (!Number.isFinite(limit) || limit <= 0) limit = 50;
    if (limit > 200) limit = 200;
    if (!Number.isFinite(offset) || offset < 0) offset = 0;

    try {
      const qs = window.location.search;
      const [tradesRes, summaryRes, rangeRes, expRes] = await Promise.all([
        App.api('/api/trades' + qs),
        App.api('/api/summary' + qs),
        App.api('/api/date-range' + qs),
        App.api('/api/experiments'),
      ]);
      for (const res of [tradesRes, summaryRes, rangeRes, expRes]) {
        if (!res.ok) throw new Error(await res.text());
      }
      const result = await tradesRes.json();
      const summary = await summaryRes.json();
      const dateRange = await rangeRes.json();
      const experiments = await expRes.json();

      if (!filtersCtrl) {
        filtersCtrl = AdminFilters.mountFilters(
          document.getElementById('filters-root'),
          { dateRange, tradeCount: summary.trade_count, experiments },
          () => { window.location.href = App.withFilterQS('/trades', { offset: undefined }); },
        );
      } else {
        filtersCtrl.updateMeta({ dateRange, tradeCount: summary.trade_count });
      }

      const pag = buildPagination(limit, offset, result);
      renderPager(pagerTop, pag);
      renderPager(pagerBottom, pag);

      const trades = result.trades || result.Trades || [];
      body.innerHTML = trades.map((t, i) => {
        const qty = t.Quantity ?? t.quantity ?? 0;
        const entry = Number(t.EntryPrice ?? t.entry_price ?? 0);
        const exit = Number(t.ExitPrice ?? t.exit_price ?? 0);
        const step = Number(t.StepPriceValue ?? t.step_price_value ?? 1) || 1;
        const lot = entry * step;
        const notional = lot * Number(qty);
        const pnl = Number(t.GrossPnL ?? t.gross_pnl ?? 0);
        const pnlR = Number(t.PnLR ?? t.pnl_r ?? 0);
        const mfe = Number(t.mfe_in_r ?? t.MFEinR ?? 0);
        const mae = Number(t.mae_in_r ?? t.MAEinR ?? 0);
        const upper = t.breakout_upper ?? t.BreakoutUpper;
        const lower = t.breakout_lower ?? t.BreakoutLower;
        return `
        <tr>
          <td class="num muted">${offset + i + 1}</td>
          <td><code>${t.ExperimentID || t.experiment_id || ''}</code></td>
          <td>${t.Ticker || t.ticker || ''}</td>
          <td>${t.Direction || t.direction || ''}</td>
          <td class="num">${qty}</td>
          <td class="num">${App.fmtMoney(lot)}</td>
          <td class="num">${App.fmtMoney(notional)}</td>
          <td>${App.fmtTime(t.OpenedAt || t.opened_at)}</td>
          <td>${App.fmtTime(t.ClosedAt || t.closed_at)}</td>
          <td>${App.fmtHold(t.HoldSeconds ?? t.hold_seconds)}</td>
          <td class="num">${entry.toFixed(2)}</td>
          <td class="num">${exit.toFixed(2)}</td>
          <td class="num ${App.pnlClass(pnl)}">${App.fmtMoney(pnl)}</td>
          <td class="num">${pnlR.toFixed(2)}</td>
          <td class="num">${mfe.toFixed(2)}</td>
          <td class="num">${mae.toFixed(2)}</td>
          <td class="num">${upper ? Number(upper).toFixed(2) : '—'}</td>
          <td class="num">${lower ? Number(lower).toFixed(2) : '—'}</td>
          <td>${t.CloseReason || t.close_reason || ''}</td>
        </tr>`;
      }).join('');
    } catch (e) {
      showError(String(e.message || e));
    }
  }

  await load();
})();
