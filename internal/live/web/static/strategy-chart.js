(function () {
  const App = window.AdminApp;
  const strategyEl = document.getElementById('strategy-select');
  const summaryEl = document.getElementById('strategy-summary');
  const statusEl = document.getElementById('strategy-status');
  const errEl = document.getElementById('page-error');
  const itemsEl = document.getElementById('strategy-items');
  const countEl = document.getElementById('strategy-trade-count');
  const chartEl = document.getElementById('strategy-chart');
  const titleEl = document.getElementById('strategy-chart-title');
  const metaEl = document.getElementById('strategy-chart-meta');

  const MSK = 'Europe/Moscow';
  let chart = null;
  let series = null;
  let priceLines = [];
  let trades = [];
  let selectedTradeId = null;
  let selectedTicker = null;

  function setStatus(text, ok) {
    statusEl.textContent = text;
    statusEl.className = 'open-status ' + (ok ? 'ok' : 'error');
  }

  function showError(msg) {
    if (!msg) {
      errEl.hidden = true;
      errEl.textContent = '';
      return;
    }
    errEl.hidden = false;
    errEl.textContent = msg;
  }

  function toUnix(time) {
    if (typeof time === 'number') return time;
    if (!time || typeof time !== 'object') return 0;
    if ('timestamp' in time) return time.timestamp;
    if ('year' in time && 'month' in time && 'day' in time) {
      return Math.floor(Date.UTC(time.year, time.month - 1, time.day) / 1000);
    }
    return 0;
  }

  function formatMSKTime(timestamp) {
    return new Date(timestamp * 1000).toLocaleTimeString('ru-RU', {
      timeZone: MSK,
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
  }

  function formatMSKDateTime(timestamp) {
    return new Date(timestamp * 1000).toLocaleString('ru-RU', {
      timeZone: MSK,
      day: '2-digit',
      month: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
  }

  function formatMSKTick(time, tickMarkType) {
    const ts = toUnix(time);
    if (!ts) return '';
    const d = new Date(ts * 1000);
    if (tickMarkType === LightweightCharts.TickMarkType.Year) {
      return d.toLocaleDateString('ru-RU', { timeZone: MSK, year: 'numeric' });
    }
    if (tickMarkType === LightweightCharts.TickMarkType.Month) {
      return d.toLocaleDateString('ru-RU', { timeZone: MSK, month: 'short' });
    }
    if (tickMarkType === LightweightCharts.TickMarkType.DayOfMonth) {
      return d.toLocaleDateString('ru-RU', { timeZone: MSK, day: '2-digit', month: '2-digit' });
    }
    return formatMSKTime(ts);
  }

  function ensureChart() {
    if (chart) return;
    chart = LightweightCharts.createChart(chartEl, {
      layout: {
        background: { color: '#1a2332' },
        textColor: '#e7ecf3',
      },
      grid: {
        vertLines: { color: '#2d3a4f' },
        horzLines: { color: '#2d3a4f' },
      },
      rightPriceScale: { borderColor: '#2d3a4f' },
      timeScale: {
        borderColor: '#2d3a4f',
        timeVisible: true,
        secondsVisible: false,
        tickMarkFormatter: formatMSKTick,
      },
      localization: {
        locale: 'ru-RU',
        timeFormatter: (time) => formatMSKDateTime(toUnix(time)),
      },
    });
    series = chart.addCandlestickSeries({
      upColor: '#3dd68c',
      downColor: '#f07178',
      borderVisible: false,
      wickUpColor: '#3dd68c',
      wickDownColor: '#f07178',
    });
    new ResizeObserver(() => {
      chart.applyOptions({ width: chartEl.clientWidth, height: chartEl.clientHeight });
    }).observe(chartEl);
    chart.applyOptions({ width: chartEl.clientWidth, height: chartEl.clientHeight });
  }

  function clearLevels() {
    priceLines.forEach((line) => {
      try { series.removePriceLine(line); } catch (_) {}
    });
    priceLines = [];
  }

  async function loadStrategies() {
    const res = await App.api('/api/experiments');
    if (!res.ok) throw new Error(await res.text());
    const ids = await res.json();
    strategyEl.innerHTML = '';
    if (!ids || !ids.length) {
      const opt = document.createElement('option');
      opt.value = '';
      opt.textContent = 'Нет стратегий';
      strategyEl.appendChild(opt);
      strategyEl.disabled = true;
      return;
    }
    strategyEl.disabled = false;
    ids.forEach((id) => {
      const opt = document.createElement('option');
      opt.value = id;
      opt.textContent = id;
      strategyEl.appendChild(opt);
    });
  }

  function renderTrades() {
    countEl.textContent = String(trades.length);
    if (!trades.length) {
      itemsEl.innerHTML = '<p class="muted" style="padding:1rem">Нет сделок</p>';
      selectedTradeId = null;
      selectedTicker = null;
      return;
    }

    if (selectedTradeId && !trades.some((t) => t.tradeId === selectedTradeId)) {
      selectedTradeId = trades[0].tradeId;
      selectedTicker = trades[0].ticker;
    }
    if (!selectedTradeId) {
      selectedTradeId = trades[0].tradeId;
      selectedTicker = trades[0].ticker;
    }

    itemsEl.innerHTML = trades.map((t) => {
      const dirClass = t.direction === 'BUY' ? 'dir-buy' : 'dir-sell';
      const pnl = Number(t.pnl || 0);
      const pnlClass = pnl >= 0 ? 'positive' : 'negative';
      const active = t.tradeId === selectedTradeId ? 'active' : '';
      const date = t.tradingDate || '';
      return `
        <button type="button" class="open-item ${active}" data-trade-id="${t.tradeId}" data-ticker="${t.ticker}">
          <div class="open-item-header">
            <span>#${t.index} · ${t.ticker}</span>
            <span class="${dirClass}">${t.direction}</span>
          </div>
          <div class="open-item-meta">
            ${date} · ${t.entryLabel} → ${t.exitLabel}
            · <span class="${pnlClass}">${App.fmtMoney(pnl)}</span> (${Number(t.pnlR || 0).toFixed(2)} R)
            <br>${t.closeReasonLabel || t.closeReason || ''}
          </div>
        </button>`;
    }).join('');

    itemsEl.querySelectorAll('.open-item').forEach((btn) => {
      btn.addEventListener('click', () => {
        selectedTradeId = btn.dataset.tradeId;
        selectedTicker = btn.dataset.ticker;
        loadTradeChart();
        renderTrades();
      });
    });
  }

  async function loadStrategyTrades() {
    const experimentId = strategyEl.value;
    if (!experimentId) {
      trades = [];
      renderTrades();
      summaryEl.textContent = '';
      setStatus('Выберите стратегию', true);
      return;
    }

    setStatus('Загрузка сделок…', true);
    showError('');
    const res = await App.api(`/api/strategy-trades?experiment_id=${encodeURIComponent(experimentId)}`);
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    trades = data.trades || [];
    const pnl = Number(data.total_pnl || 0);
    summaryEl.textContent = trades.length
      ? `${data.trade_count} сделок · PnL ${App.fmtMoney(pnl)} · Win ${Number(data.win_rate || 0).toFixed(1)}% · Exp ${Number(data.expectancy_r || 0).toFixed(2)} R`
      : 'сделок нет';
    summaryEl.className = 'muted ' + (pnl >= 0 ? 'positive' : 'negative');
    renderTrades();
    setStatus(`Стратегия ${experimentId} · сделок: ${trades.length}`, true);
    if (selectedTradeId) await loadTradeChart();
  }

  async function loadTradeChart() {
    const experimentId = strategyEl.value;
    if (!experimentId || !selectedTradeId || !selectedTicker) {
      return;
    }
    ensureChart();
    try {
      const qs = new URLSearchParams({
        experiment_id: experimentId,
        ticker: selectedTicker,
        trade_id: selectedTradeId,
      });
      const res = await App.api(`/api/strategy-trade-chart?${qs}`);
      if (!res.ok) throw new Error(await res.text());
      const payload = await res.json();
      series.setData(payload.candles || []);
      series.setMarkers(payload.markers || []);
      clearLevels();
      (payload.levels || []).forEach((lvl) => {
        priceLines.push(series.createPriceLine({
          price: lvl.price,
          color: lvl.color,
          lineWidth: 1,
          lineStyle: LightweightCharts.LineStyle.Dashed,
          axisLabelVisible: true,
          title: lvl.title,
        }));
      });
      if (payload.candles && payload.candles.length) {
        chart.timeScale().fitContent();
      }
      const trade = payload.trade || {};
      titleEl.textContent = `${payload.ticker} · #${trade.index || ''} · ${trade.direction || ''}`;
      metaEl.textContent = `${payload.timeframe || 'M5'} · ${payload.trading_date || ''} · PnL ${App.fmtMoney(Number(trade.pnl || 0))}`;
    } catch (e) {
      showError(String(e.message || e));
      setStatus('График: ' + (e.message || e), false);
    }
  }

  async function init() {
    try {
      await loadStrategies();
      if (strategyEl.value) await loadStrategyTrades();
    } catch (e) {
      showError(String(e.message || e));
      setStatus('Ошибка: ' + (e.message || e), false);
    }
  }

  strategyEl.addEventListener('change', () => {
    selectedTradeId = null;
    selectedTicker = null;
    loadStrategyTrades().catch((e) => {
      showError(String(e.message || e));
      setStatus('Ошибка: ' + (e.message || e), false);
    });
  });

  init();
})();
