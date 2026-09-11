(function () {
  const App = window.AdminApp;
  const dateEl = document.getElementById('day-date');
  const tickerEl = document.getElementById('day-ticker');
  const loadBtn = document.getElementById('day-load');
  const summaryEl = document.getElementById('day-summary');
  const statusEl = document.getElementById('day-status');
  const errEl = document.getElementById('page-error');
  const chartEl = document.getElementById('day-chart');
  const titleEl = document.getElementById('day-chart-title');
  const metaEl = document.getElementById('day-chart-meta');
  const tradesBody = document.getElementById('day-trades-body');
  const tradeCountEl = document.getElementById('day-trade-count');

  const MSK = 'Europe/Moscow';
  let chart = null;
  let series = null;
  let dayTickers = [];

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

  function todayMSK() {
    return new Date().toLocaleDateString('en-CA', { timeZone: MSK });
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

  function fillTickers(tickers, preferred) {
    dayTickers = tickers || [];
    const prev = preferred || tickerEl.value;
    tickerEl.innerHTML = '';
    if (!dayTickers.length) {
      const opt = document.createElement('option');
      opt.value = '';
      opt.textContent = 'Нет сделок';
      tickerEl.appendChild(opt);
      tickerEl.disabled = true;
      return;
    }
    tickerEl.disabled = false;
    dayTickers.forEach((row) => {
      const opt = document.createElement('option');
      opt.value = row.ticker;
      opt.textContent = `${row.ticker} (${row.trade_count})`;
      tickerEl.appendChild(opt);
    });
    if (prev && dayTickers.some((t) => t.ticker === prev)) {
      tickerEl.value = prev;
    }
  }

  function renderTrades(trades) {
    tradeCountEl.textContent = String(trades.length);
    if (!trades.length) {
      tradesBody.innerHTML = '<tr><td colspan="9" class="muted">Нет сделок за день</td></tr>';
      return;
    }
    tradesBody.innerHTML = trades.map((t) => {
      const pnl = Number(t.pnl || 0);
      const dirClass = t.direction === 'BUY' ? 'dir-buy' : 'dir-sell';
      return `
        <tr>
          <td class="num muted">${t.index}</td>
          <td><code>${t.experimentId || ''}</code></td>
          <td class="${dirClass}">${t.direction || ''}</td>
          <td>${t.entryLabel || ''}</td>
          <td>${t.exitLabel || ''}</td>
          <td class="num">${Number(t.entryPrice || 0).toFixed(2)}</td>
          <td class="num">${Number(t.exitPrice || 0).toFixed(2)}</td>
          <td class="num ${App.pnlClass(pnl)}">${App.fmtMoney(pnl)}</td>
          <td>${t.closeReasonLabel || t.closeReason || ''}</td>
        </tr>`;
    }).join('');
  }

  async function loadDayMeta() {
    const date = dateEl.value;
    if (!date) return;
    showError('');
    const res = await App.api(`/api/day-trades?date=${encodeURIComponent(date)}`);
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    fillTickers(data.tickers || [], tickerEl.value);
    const pnl = Number(data.total_pnl || 0);
    summaryEl.textContent = data.trade_count
      ? `сделок: ${data.trade_count} · PnL ${App.fmtMoney(pnl)}`
      : 'сделок нет';
    summaryEl.className = 'muted ' + (pnl >= 0 ? 'positive' : 'negative');
    return data;
  }

  async function loadChart() {
    const date = dateEl.value;
    const ticker = tickerEl.value;
    if (!date) {
      setStatus('Укажите дату', false);
      return;
    }
    if (!ticker) {
      setStatus('Нет тикера со сделками за этот день', false);
      ensureChart();
      series.setData([]);
      series.setMarkers([]);
      renderTrades([]);
      titleEl.textContent = 'Нет данных';
      metaEl.textContent = '';
      return;
    }

    setStatus('Загрузка…', true);
    showError('');
    try {
      await loadDayMeta();
      const chartRes = await App.api(
        `/api/day-chart?date=${encodeURIComponent(date)}&ticker=${encodeURIComponent(ticker)}`
      );
      if (!chartRes.ok) throw new Error(await chartRes.text());
      const payload = await chartRes.json();
      ensureChart();
      series.setData(payload.candles || []);
      series.setMarkers(payload.markers || []);
      if (payload.candles && payload.candles.length) {
        chart.timeScale().fitContent();
      }
      const trades = payload.trades || [];
      renderTrades(trades);
      titleEl.textContent = `${payload.ticker} · ${payload.date}`;
      metaEl.textContent = `${payload.timeframe || 'M5'} · свечей: ${(payload.candles || []).length} · сделок: ${trades.length}`;
      setStatus(`Готово · ${ticker} · ${date}`, true);
    } catch (e) {
      showError(String(e.message || e));
      setStatus('Ошибка: ' + (e.message || e), false);
    }
  }

  async function init() {
    try {
      const rangeRes = await App.api('/api/date-range');
      let defaultDate = todayMSK();
      if (rangeRes.ok) {
        const range = await rangeRes.json();
        if (range.to || range.To) defaultDate = range.to || range.To;
        else if (range.from || range.From) defaultDate = range.from || range.From;
      }
      dateEl.value = defaultDate;
      await loadDayMeta();
      if (tickerEl.value) await loadChart();
      else setStatus('Выберите дату со сделками', true);
    } catch (e) {
      showError(String(e.message || e));
      setStatus('Ошибка: ' + (e.message || e), false);
    }
  }

  loadBtn.addEventListener('click', () => loadChart());
  dateEl.addEventListener('change', async () => {
    try {
      await loadDayMeta();
      if (tickerEl.value) await loadChart();
    } catch (e) {
      showError(String(e.message || e));
      setStatus('Ошибка: ' + (e.message || e), false);
    }
  });
  tickerEl.addEventListener('change', () => loadChart());

  init();
})();
