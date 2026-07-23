(function () {
  const statusEl = document.getElementById('open-status');
  const itemsEl = document.getElementById('open-items');
  const countEl = document.getElementById('open-count');
  const chartEl = document.getElementById('open-chart');
  const titleEl = document.getElementById('open-chart-title');
  const metaEl = document.getElementById('open-chart-meta');

  let selectedId = null;
  let chart = null;
  let series = null;
  let priceLines = [];

  function setStatus(text, ok) {
    statusEl.textContent = text;
    statusEl.className = 'open-status ' + (ok ? 'ok' : 'error');
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
      timeScale: { borderColor: '#2d3a4f', timeVisible: true, secondsVisible: false },
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
      const res = await fetch(`/live/chart?ticker=${encodeURIComponent(ticker)}&id=${encodeURIComponent(id || '')}`);
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
      const pos = payload.position;
      if (pos) {
        titleEl.textContent = `${pos.experiment_id} / ${pos.ticker} · ${pos.direction}`;
        metaEl.textContent = `вход ${Number(pos.entry_price).toFixed(2)} · uPnL ${Number(pos.unrealized_pnl).toFixed(2)} ₽`;
      }
    } catch (e) {
      setStatus('График: ' + e.message, false);
    }
  }

  async function tick() {
    try {
      const res = await fetch('/live/positions');
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
