<template>
  <div class="page">
    <header class="hero">
      <div class="title">Market Visual Runner</div>
      <div class="subtitle">Visualize streams and snapshots in one place.</div>
    </header>

    <section class="panel slider-panel">
      <div class="panel-title">Timeframe</div>
      <div class="slider-meta">
        <div>
          <div class="label">Start (Data)</div>
          <div class="value">{{ timeframe?.start ?? "—" }}</div>
        </div>
        <div>
          <div class="label">Start (Selecao)</div>
          <div class="value">{{ startMinuteLabel ?? "—" }}</div>
        </div>
        <div>
          <div class="label">End (Selecao)</div>
          <div class="value">{{ endMinuteLabel ?? "—" }}</div>
        </div>
        <div>
          <div class="label">End (Data)</div>
          <div class="value">{{ timeframe?.end ?? "—" }}</div>
        </div>
      </div>
      <div class="slider-row">
        <div ref="sliderEl" class="noui-slider"></div>
        <div class="slider-note" v-if="loading">Carregando timeframe…</div>
        <div class="slider-note error" v-else-if="error">{{ error }}</div>
        <div class="slider-note" v-else-if="!minutesCount">Sem dados.</div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-title">Status</div>
      <div class="grid">
        <div class="card">
          <div class="label">Connection</div>
          <div class="value ok">Ready</div>
        </div>
        <div class="card">
          <div class="label">Last Refresh</div>
          <div class="value">Just now</div>
        </div>
        <div class="card">
          <div class="label">Instruments</div>
          <div class="value">0</div>
        </div>
        <div class="card">
          <div class="label">Ticks Today</div>
          <div class="value">0</div>
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-title">Price Charts</div>
      <div class="charts-grid">
        <div v-for="symbol in symbols" :key="symbol" class="chart-card">
          <div class="chart-header">
            <div class="label">Symbol</div>
            <div class="value">{{ symbol }}</div>
          </div>
          <div class="chart-canvas">
            <canvas :ref="setChartRef(symbol)"></canvas>
          </div>
        </div>
        <div v-if="!symbols.length" class="chart-empty">
          <div class="value">Sem ativos.</div>
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-title">Next</div>
      <ul class="list">
        <li>Connect to stream endpoints.</li>
        <li>Add filters for symbols and time ranges.</li>
        <li>Render live charts and replay views.</li>
      </ul>
    </section>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import Chart from "chart.js/auto";
import noUiSlider from "nouislider";
import "nouislider/dist/nouislider.css";

const timeframe = ref(null);
const loading = ref(false);
const error = ref("");
const rangeStart = ref(0);
const rangeEnd = ref(0);
const endIndex = computed(() => Math.max(minutesCount.value - 1, 0));
const sliderEl = ref(null);
const sliderApi = ref(null);
const chartRefs = new Map();
const chartInstances = new Map();
let renderToken = 0;

const minutesCount = computed(() => {
  const first = timeframe.value?.quality_per_symbol?.[0];
  if (!first?.frame_quality_per_minute) return 0;
  return first.frame_quality_per_minute.length;
});

const symbols = computed(() => {
  const list = timeframe.value?.quality_per_symbol ?? [];
  return list.map((entry) => entry.symbol);
});

const clampRange = () => {
  if (!minutesCount.value) {
    rangeStart.value = 0;
    rangeEnd.value = 0;
    return;
  }
  if (rangeStart.value > rangeEnd.value) {
    const tmp = rangeStart.value;
    rangeStart.value = rangeEnd.value;
    rangeEnd.value = tmp;
  }
};

const startMinuteLabel = computed(() => {
  if (!timeframe.value?.start || !minutesCount.value) return "";
  const base = new Date(timeframe.value.start);
  if (Number.isNaN(base.getTime())) return "";
  const current = new Date(base.getTime() + rangeStart.value * 60_000);
  return formatDateTime(current);
});

const endMinuteLabel = computed(() => {
  if (!timeframe.value?.start || !minutesCount.value) return "";
  const base = new Date(timeframe.value.start);
  if (Number.isNaN(base.getTime())) return "";
  const current = new Date(base.getTime() + rangeEnd.value * 60_000);
  return formatDateTime(current);
});

const fetchTimeframe = async () => {
  loading.value = true;
  error.value = "";
  try {
    const response = await fetch("/market-visual-runner-bff/timeframe");
    if (!response.ok) {
      throw new Error("Falha ao carregar timeframe.");
    }
    const data = await response.json();
    timeframe.value = data;
    rangeStart.value = 0;
    rangeEnd.value = endIndex.value;
    clampRange();
    initOrUpdateSlider();
    renderCharts();
  } catch (err) {
    error.value = err?.message ?? "Erro ao carregar timeframe.";
  } finally {
    loading.value = false;
  }
};

onMounted(fetchTimeframe);

watch(rangeStart, clampRange);
watch(rangeEnd, clampRange);
watch([rangeStart, rangeEnd], () => {
  renderCharts();
});

const initOrUpdateSlider = () => {
  if (!sliderEl.value || !minutesCount.value) return;
  const max = Math.max(minutesCount.value - 1, 0);
  if (!sliderApi.value) {
    noUiSlider.create(sliderEl.value, {
      start: [rangeStart.value, rangeEnd.value],
      step: 1,
      connect: true,
      behaviour: "tap-drag",
      range: { min: 0, max },
    });
    sliderApi.value = sliderEl.value.noUiSlider;
    sliderApi.value.on("update", (values) => {
      const startVal = Math.round(Number(values[0]));
      const endVal = Math.round(Number(values[1]));
      if (rangeStart.value !== startVal) rangeStart.value = startVal;
      if (rangeEnd.value !== endVal) rangeEnd.value = endVal;
    });
  } else {
    sliderApi.value.updateOptions(
      {
        range: { min: 0, max },
        step: 1,
        start: [rangeStart.value, rangeEnd.value],
      },
      true
    );
  }
};

watch(minutesCount, () => {
  if (!minutesCount.value) return;
  rangeEnd.value = endIndex.value;
  clampRange();
  initOrUpdateSlider();
  renderCharts();
});

onBeforeUnmount(() => {
  if (sliderApi.value) {
    sliderApi.value.destroy();
    sliderApi.value = null;
  }
  chartInstances.forEach((chart) => chart.destroy());
  chartInstances.clear();
});

const formatDateTime = (date) => {
  if (!(date instanceof Date)) return "";
  const year = date.getUTCFullYear();
  const month = String(date.getUTCMonth() + 1).padStart(2, "0");
  const day = String(date.getUTCDate()).padStart(2, "0");
  const hours = String(date.getUTCHours()).padStart(2, "0");
  const minutes = String(date.getUTCMinutes()).padStart(2, "0");
  const seconds = String(date.getUTCSeconds()).padStart(2, "0");
  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
};

const setChartRef = (symbol) => (el) => {
  if (el) {
    chartRefs.set(symbol, el);
  } else {
    chartRefs.delete(symbol);
  }
};

const renderCharts = async () => {
  if (!symbols.value.length) return;
  if (!timeframe.value?.start) return;
  const base = new Date(timeframe.value.start);
  if (Number.isNaN(base.getTime())) return;
  const startTime = new Date(base.getTime() + rangeStart.value * 60_000);
  const endTime = new Date(base.getTime() + rangeEnd.value * 60_000);
  const qualityMap = new Map(
    (timeframe.value?.quality_per_symbol ?? []).map((entry) => [entry.symbol, entry.frame_quality_per_minute])
  );
  const token = (renderToken += 1);

  const payloads = await Promise.all(
    symbols.value.map(async (symbol) => {
      const result = await fetchPriceOverview(symbol, startTime, endTime);
      return { symbol, result };
    })
  );

  if (token !== renderToken) return;

  payloads.forEach(({ symbol, result }) => {
    const canvas = chartRefs.get(symbol);
    if (!canvas) return;
    if (chartInstances.has(symbol)) {
      chartInstances.get(symbol).destroy();
      chartInstances.delete(symbol);
    }
    const flags = qualityMap.get(symbol) ?? [];
    const data = result?.prices ?? [];
    const labels = result?.datetimes ?? [];
    const gaps = buildGapRanges(flags, rangeStart.value, rangeEnd.value);
    const chart = new Chart(canvas, {
      plugins: [
        {
          id: "gap-highlighter",
          beforeDatasetsDraw(chartInstance) {
            if (!gaps.length) return;
            const { ctx, chartArea, scales } = chartInstance;
            if (!chartArea) return;
            const { top, bottom } = chartArea;
            ctx.save();
            ctx.fillStyle = "rgba(255, 105, 105, 0.12)";
            gaps.forEach(([startIdx, endIdx]) => {
              const xStart = scales.x.getPixelForValue(startIdx);
              const xEnd = scales.x.getPixelForValue(endIdx + 1);
              ctx.fillRect(xStart, top, xEnd - xStart, bottom - top);
            });
            ctx.restore();
          },
        },
      ],
      type: "line",
      data: {
        labels,
        datasets: [
          {
            label: symbol,
            data,
            borderColor: "#7be6cf",
            backgroundColor: "rgba(123, 230, 207, 0.15)",
            borderWidth: 2,
            tension: 0.35,
            pointRadius: 0,
            fill: true,
            spanGaps: false,
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: { display: false },
          tooltip: {
            callbacks: {
              label: (ctx) => (ctx.parsed.y == null ? `${symbol}: sem dado` : `${symbol}: ${ctx.parsed.y.toFixed(2)}`),
            },
          },
        },
        scales: {
          x: {
            ticks: { color: "#9ad7cf", maxTicksLimit: 5 },
            grid: { color: "rgba(126, 210, 204, 0.08)" },
          },
          y: {
            ticks: { color: "#9ad7cf" },
            grid: { color: "rgba(126, 210, 204, 0.08)" },
          },
        },
      },
    });
    chartInstances.set(symbol, chart);
  });
};

const fetchPriceOverview = async (symbol, start, end) => {
  const params = new URLSearchParams({
    start: formatDateTime(start),
    end: formatDateTime(end),
  });
  const response = await fetch(`/market-visual-runner-bff/symbols/${encodeURIComponent(symbol)}/price-overview?${params}`);
  if (!response.ok) {
    return null;
  }
  return response.json();
};

const buildGapRanges = (flags, startIdx, endIdx) => {
  const ranges = [];
  let inGap = false;
  let gapStart = startIdx;
  for (let i = startIdx; i <= endIdx; i += 1) {
    const hasData = flags[i] === 1;
    if (!hasData && !inGap) {
      inGap = true;
      gapStart = i;
    }
    if (hasData && inGap) {
      ranges.push([gapStart - startIdx, i - 1 - startIdx]);
      inGap = false;
    }
  }
  if (inGap) {
    ranges.push([gapStart - startIdx, endIdx - startIdx]);
  }
  return ranges;
};

</script>

<style scoped>
.list {
  margin: 0;
  padding-left: 1.1rem;
  color: #e6f4f1;
  line-height: 1.6;
}

.slider-panel {
  display: flex;
  flex-direction: column;
  gap: 1.1rem;
}

.slider-meta {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 1rem;
}

.slider-row {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}

.noui-slider {
  margin: 0.4rem 0 0.2rem;
}

:deep(.noUi-target) {
  background: rgba(10, 22, 23, 0.9);
  border: 1px solid rgba(126, 210, 204, 0.25);
  box-shadow: inset 0 0 0 1px rgba(4, 12, 13, 0.7);
  border-radius: 999px;
  height: 10px;
}

:deep(.noUi-connect) {
  background: linear-gradient(90deg, rgba(123, 230, 207, 0.2), rgba(123, 230, 207, 0.7));
}

:deep(.noUi-handle) {
  width: 18px;
  height: 18px;
  border-radius: 999px;
  background: #e7faf7;
  border: 2px solid rgba(123, 230, 207, 0.9);
  box-shadow: 0 6px 14px rgba(0, 0, 0, 0.35);
  top: -5px;
  right: -9px;
}

:deep(.noUi-handle::before),
:deep(.noUi-handle::after) {
  display: none;
}

.slider-note {
  font-size: 0.9rem;
  color: #9ad7cf;
}

.slider-note.error {
  color: #ff9f9f;
}

.charts-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 1.2rem;
}

.chart-card {
  background: rgba(8, 18, 19, 0.8);
  border-radius: 16px;
  border: 1px solid rgba(91, 180, 173, 0.2);
  padding: 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.8rem;
}

.chart-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.6rem;
}

.chart-canvas {
  position: relative;
  height: 220px;
}

.chart-empty {
  padding: 1rem;
  border: 1px dashed rgba(126, 210, 204, 0.25);
  border-radius: 14px;
  text-align: center;
}
</style>
