(async function () {
  const App = window.AdminApp;
  const errEl = document.getElementById('page-error');
  const cards = document.getElementById('kpi-cards');
  const cmpSection = document.getElementById('comparison-section');
  const cmpBody = document.getElementById('comparison-body');
  let filtersCtrl = null;

  function showError(msg) {
    errEl.hidden = !msg;
    errEl.textContent = msg || '';
  }

  async function load() {
    showError('');
    try {
      const qs = window.location.search;
      const [summaryRes, cmpRes, rangeRes, expRes, equityRes] = await Promise.all([
        App.api('/api/summary' + qs),
        App.api('/api/comparison' + qs),
        App.api('/api/date-range' + qs),
        App.api('/api/experiments'),
        App.api('/api/account-equity' + qs),
      ]);
      for (const res of [summaryRes, cmpRes, rangeRes, expRes, equityRes]) {
        if (!res.ok) throw new Error(await res.text());
      }
      const summary = await summaryRes.json();
      const comparison = await cmpRes.json();
      const dateRange = await rangeRes.json();
      const experiments = await expRes.json();
      const equity = await equityRes.json();

      if (!filtersCtrl) {
        filtersCtrl = AdminFilters.mountFilters(
          document.getElementById('filters-root'),
          { dateRange, tradeCount: summary.trade_count, experiments },
          load,
        );
      } else {
        filtersCtrl.updateMeta({ dateRange, tradeCount: summary.trade_count });
      }

      const deposit = equity.starting_deposit;
      const balance = equity.current_balance;
      cards.innerHTML = `
        <div class="card"><div class="label">Сделок</div><div class="value">${summary.trade_count}</div></div>
        <div class="card highlight"><div class="label">Expectancy (R)</div><div class="value ${App.pnlClass(summary.expectancy_r)}">${summary.expectancy_r >= 0 ? '+' : ''}${Number(summary.expectancy_r).toFixed(2)}</div></div>
        <div class="card"><div class="label">Expectancy (₽)</div><div class="value ${App.pnlClass(summary.expectancy)}">${App.fmtMoney(summary.expectancy)} ₽</div></div>
        <div class="card"><div class="label">Win rate</div><div class="value">${App.fmtPct(summary.win_rate)}</div></div>
        <div class="card"><div class="label">Total PnL</div><div class="value ${App.pnlClass(summary.total_pnl)}">${App.fmtMoney(summary.total_pnl)} ₽</div></div>
        <div class="card highlight"><div class="label">Депозит</div><div class="value">${deposit ? App.fmtMoney(deposit) + ' ₽' : '—'}</div></div>
        <div class="card highlight"><div class="label">Баланс</div><div class="value ${deposit ? App.pnlClass(balance) : ''}">${deposit ? App.fmtMoney(balance) + ' ₽' : '—'}</div></div>
        <div class="card"><div class="label">Profit factor</div><div class="value">${Number(summary.profit_factor).toFixed(2)}</div></div>
        <div class="card"><div class="label">Ср. удержание</div><div class="value">${App.fmtHold(summary.avg_hold_seconds)}</div></div>
        <div class="card"><div class="label">Период</div><div class="value small">${dateRange.from || '—'} — ${dateRange.to || '—'}</div></div>`;

      if (comparison && comparison.length) {
        cmpSection.hidden = false;
        cmpBody.innerHTML = comparison.map((row) => `
          <tr>
            <td><code>${row.key}</code></td>
            <td class="num">${row.trade_count}</td>
            <td class="num ${App.pnlClass(row.avg_pnl_r)}">${row.avg_pnl_r >= 0 ? '+' : ''}${Number(row.avg_pnl_r).toFixed(2)}</td>
            <td class="num">${App.fmtPct(row.win_rate)}</td>
            <td class="num ${App.pnlClass(row.total_pnl)}">${App.fmtMoney(row.total_pnl)}</td>
            <td class="num">${Number(row.profit_factor).toFixed(2)}</td>
          </tr>`).join('');
      } else {
        cmpSection.hidden = true;
        cmpBody.innerHTML = '';
      }
    } catch (e) {
      showError(String(e.message || e));
    }
  }

  await load();
})();
