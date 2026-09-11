(function () {
  const App = window.AdminApp;
  const statusEl = document.getElementById('open-status');
  const itemsEl = document.getElementById('open-items');
  const countEl = document.getElementById('open-count');
  const chartEl = document.getElementById('open-chart');
  const titleEl = document.getElementById('open-chart-title');
  const metaEl = document.getElementById('open-chart-meta');

  const MSK = 'Europe/Moscow';

  let selectedId = null;
  let chart = null;
  let series = null;
  let priceLines = [];

  function setStatus(text, ok) {
    statusEl.textContent = text;
    statusEl.className = 'open-status ' + (ok ? 'ok' : 'error');
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

  function renderPositions(positions) {
    countEl.textContent = String(positions.length);
    if (!positions.length) {
      itemsEl.innerHTML = '<p class="muted" style="padding:1rem">Нет открытых позиций</p>';
      if (!selectedId) {
        titleEl.textContent = 'Нет открытых позиций';
        metaEl.textContent = '';
      }
      return;
    }

    if (selectedId && !positions.some((p) => p.id === selectedId)) {
      selectedId = positions[0].id;
    }
    if (!selectedId) selectedId = positions[0].id;

    itemsEl.innerHTML = positions.map((p) => {
      const dirClass = p.direction === 'BUY' ? 'dir-buy' : 'dir-sell';
      const pnlClass = p.unrealized_pnl >= 0 ? 'positive' : 'negative';
      const active = p.id === selectedId ? 'active' : '';
      const opened = App.fmtTimeMSK(p.opened_at || p.OpenedAt);
      return `
        <button type="button" class="open-item ${active}" data-id="${p.id}" data-ticker="${p.ticker}">
          <div class="open-item-header">
            <span><code>${p.experiment_id}</code> / ${p.ticker}</span>
            <span class="${dirClass}">${p.direction}</span>
          </div>
          <div class="open-item-meta">
            x${p.quantity} @ ${Number(p.entry_price).toFixed(2)}
            · last ${Number(p.last_price).toFixed(2)}
            · uPnL <span class="${pnlClass}">${Number(p.unrealized_pnl).toFixed(2)}</span>
            <br>SL ${Number(p.stop_loss).toFixed(2)} · TP ${Number(p.take_profit).toFixed(2)}
            · вход ${opened}
          </div>
        </button>`;
    }).join('');

    itemsEl.querySelectorAll('.open-item').forEach((btn) => {
      btn.addEventListener('click', () => {
        selectedId = btn.dataset.id;
        loadChart(btn.dataset.ticker, selectedId);
        renderPositions(positions);
      });
    });
  }

  async function loadChart(ticker, id) {
    ensureChart();
    try {
      const chartRes = await App.api(`/chart?ticker=${encodeURIComponent(ticker)}&id=${encodeURIComponent(id || '')}`);
      if (!chartRes.ok) throw new Error(await chartRes.text());
      const payload = await chartRes.json();
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
      const pos = payload.position;
      if (pos) {
        titleEl.textContent = `${pos.experiment_id} / ${pos.ticker} · ${pos.direction}`;
        const opened = App.fmtTimeMSK(pos.opened_at || pos.OpenedAt);
        metaEl.textContent = `вход ${Number(pos.entry_price).toFixed(2)} · ${opened} · uPnL ${Number(pos.unrealized_pnl).toFixed(2)} ₽`;
      }
    } catch (e) {
      setStatus('График: ' + e.message, false);
    }
  }

  async function tick() {
    try {
      const res = await App.api('/positions');
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      const positions = data.positions || [];
      setStatus(`Онлайн · позиций: ${positions.length}`, true);
      renderPositions(positions);
      if (selectedId) {
        const sel = positions.find((p) => p.id === selectedId);
        if (sel) await loadChart(sel.ticker, sel.id);
      }
    } catch (e) {
      setStatus('Бот недоступен: ' + e.message, false);
    }
  }

  tick();
  setInterval(tick, 3000);
})();
