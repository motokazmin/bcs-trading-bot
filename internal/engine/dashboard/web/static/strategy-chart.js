(function () {
  const App = window.AdminApp;
  const strategyEl = document.getElementById('strategy-select');
  const summaryEl = document.getElementById('strategy-summary');
  const statusEl = document.getElementById('strategy-status');
  const errEl = document.getElementById('page-error');
  const tradesBody = document.getElementById('strategy-trades-body');
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

  function tradeIdOf(t) {
    const id = t.tradeId ?? t.trade_id;
    if (id != null && id !== '') return String(id);
    const et = t.entryTime ?? t.entry_time;
    if (et != null) return String(et);
    return '';
  }

  function tickerOf(t) {
    return String(t.ticker ?? t.Ticker ?? '').toUpperCase();
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
      tradesBody.innerHTML = '<tr><td colspan="10" class="muted">Нет сделок</td></tr>';
      selectedTradeId = null;
      selectedTicker = null;
      return;
    }

    if (selectedTradeId && !trades.some((t) => tradeIdOf(t) === selectedTradeId)) {
      selectedTradeId = tradeIdOf(trades[0]);
      selectedTicker = tickerOf(trades[0]);
    }
    if (!selectedTradeId) {
      selectedTradeId = tradeIdOf(trades[0]);
      selectedTicker = tickerOf(trades[0]);
    }

    tradesBody.innerHTML = trades.map((t) => {
      const tid = tradeIdOf(t);
      const tk = tickerOf(t);
      const dirClass = t.direction === 'BUY' ? 'dir-buy' : 'dir-sell';
      const pnl = Number(t.pnl ?? t.PnL ?? 0);
      const pnlR = Number(t.pnlR ?? t.pnl_r ?? 0);
      const active = tid === selectedTradeId ? 'active' : '';
      return `
        <tr class="row-selectable ${active}" data-trade-id="${tid}" data-ticker="${tk}">
          <td class="num muted">${t.index}</td>
          <td>${tk}</td>
          <td class="${dirClass}">${t.direction || ''}</td>
          <td>${t.entryLabel || ''}</td>
          <td>${t.exitLabel || ''}</td>
          <td class="num">${Number(t.entryPrice || 0).toFixed(2)}</td>
          <td class="num">${Number(t.exitPrice || 0).toFixed(2)}</td>
          <td class="num ${App.pnlClass(pnl)}">${App.fmtMoney(pnl)}</td>
          <td class="num ${App.pnlClass(pnlR)}">${pnlR.toFixed(2)}</td>
          <td>${t.closeReasonLabel || t.closeReason || ''}</td>
        </tr>`;
    }).join('');

    tradesBody.querySelectorAll('tr.row-selectable').forEach((row) => {
      row.addEventListener('click', async () => {
        selectedTradeId = row.dataset.tradeId;
        selectedTicker = row.dataset.ticker;
        await loadTradeChart();
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
    if (trades.length) {
      await loadTradeChart();
    }
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
      const candleData = (payload.candles || []).map((c) => ({
        time: Number(c.time),
        open: Number(c.open),
        high: Number(c.high),
        low: Number(c.low),
        close: Number(c.close),
      }));
      series.setData(candleData);
      series.setMarkers(payload.markers || []);
      clearLevels();
      (payload.levels || []).forEach((lvl) => {
        priceLines.push(series.createPriceLine({
          price: Number(lvl.price),
          color: lvl.color,
          lineWidth: 1,
          lineStyle: LightweightCharts.LineStyle.Dashed,
          axisLabelVisible: true,
          title: lvl.title,
        }));
      });
      chart.applyOptions({ width: chartEl.clientWidth, height: chartEl.clientHeight });
      const trade = payload.trade || {};
      titleEl.textContent = `${payload.ticker || selectedTicker} · #${trade.index || ''} · ${trade.direction || ''}`;
      let meta = `${payload.timeframe || 'M5'} · ${payload.trading_date || ''} · PnL ${App.fmtMoney(Number(trade.pnl || 0))} · свечей: ${candleData.length}`;
      if (!candleData.length) {
        meta += ' · BCS не вернул свечи для окна сделки';
      }
      metaEl.textContent = meta;
      if (candleData.length) {
        chart.timeScale().fitContent();
      }
      showError('');
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
