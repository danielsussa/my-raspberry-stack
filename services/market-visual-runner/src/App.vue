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
        <div v-for="symbol in visibleSymbols" :key="symbol" class="chart-card">
          <div class="chart-header">
            <div class="label">Symbol</div>
            <div class="value">{{ symbol }}</div>
          </div>
          <div class="chart-canvas">
            <canvas :ref="setChartRef(symbol)"></canvas>
          </div>
        </div>
        <div v-if="!visibleSymbols.length" class="chart-empty">
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
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
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
const visibleSymbols = ref([]);
let renderToken = 0;
const dragState = { active: false, startIndex: 0, currentIndex: 0, chart: null };

const parseResolutionMinutes = (resolution) => {
  if (typeof resolution !== "string") return 1;
  const trimmed = resolution.trim().toLowerCase();
  const match = trimmed.match(/^(\d+)\s*([mh])$/);
  if (!match) return 1;
  const amount = Number(match[1]);
  if (!Number.isFinite(amount) || amount <= 0) return 1;
  return match[2] === "h" ? amount * 60 : amount;
};

const resolutionMinutes = computed(() => parseResolutionMinutes(timeframe.value?.resolution));

const minutesCount = computed(() => {
  const first = timeframe.value?.frame_quality?.[0];
  if (!first?.quality) return 0;
  return first.quality.length;
});

const symbols = computed(() => {
  const list = timeframe.value?.frame_quality ?? [];
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
  const current = new Date(base.getTime() + rangeStart.value * resolutionMinutes.value * 60_000);
  return formatDateTime(current);
});

const endMinuteLabel = computed(() => {
  if (!timeframe.value?.start || !minutesCount.value) return "";
  const base = new Date(timeframe.value.start);
  if (Number.isNaN(base.getTime())) return "";
  const current = new Date(base.getTime() + (rangeEnd.value + 1) * resolutionMinutes.value * 60_000 - 60_000);
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
    sliderApi.value.on("change", () => {
      renderCharts();
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

const computeResolutionSeconds = (start, end) => {
  if (!(start instanceof Date) || !(end instanceof Date)) return 300;
  const diffMs = end.getTime() - start.getTime();
  if (!Number.isFinite(diffMs) || diffMs < 0) return 300;
  const minutes = Math.max(Math.round(diffMs / 60_000) + 1, 1);
  if (minutes < 10) return 1;
  if (minutes < 120) return 20;
  return 60;
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
  dragState.active = false;
  dragState.chart = null;
  const base = new Date(timeframe.value.start);
  if (Number.isNaN(base.getTime())) return;
  const startTime = new Date(base.getTime() + rangeStart.value * resolutionMinutes.value * 60_000);
  const endTime = new Date(base.getTime() + (rangeEnd.value + 1) * resolutionMinutes.value * 60_000 - 60_000);
  const resolutionSeconds = computeResolutionSeconds(startTime, endTime);
  const qualityMap = new Map(
    (timeframe.value?.frame_quality ?? []).map((entry) => [entry.symbol, entry.quality])
  );
  const token = (renderToken += 1);

  let payloads = [];
  try {
    payloads = await Promise.all(
      symbols.value.map(async (symbol) => {
        const result = await fetchPriceOverview(symbol, startTime, endTime, resolutionSeconds);
        return { symbol, result };
      })
    );
  } catch (err) {
    error.value = err?.message ?? "Erro ao carregar price overview.";
    return;
  }

  if (token !== renderToken) return;

  const availableSymbols = payloads
    .filter(({ result }) => result?.prices?.length)
    .map(({ symbol }) => symbol);
  visibleSymbols.value = availableSymbols;
  await nextTick();

  payloads.forEach(({ symbol, result }) => {
    if (!result || !result.prices?.length) {
      return;
    }
    const canvas = chartRefs.get(symbol);
    if (!canvas) return;
    if (chartInstances.has(symbol)) {
      const existing = chartInstances.get(symbol);
      if (existing?.canvas) {
        existing.canvas.__chartRef = null;
      }
      existing.destroy();
      chartInstances.delete(symbol);
    }
    const flags = qualityMap.get(symbol) ?? [];
    const data = result?.prices ?? [];
    const labels = result?.datetimes ?? [];
    const gaps = buildGapRanges(
      flags,
      rangeStart.value,
      rangeEnd.value,
      resolutionMinutes.value,
      resolutionSeconds,
      data.length - 1
    );
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
        {
          id: "hover-line",
          afterDatasetsDraw(chartInstance) {
            const { ctx, chartArea } = chartInstance;
            if (!chartArea) return;
            const active = chartInstance.getActiveElements
              ? chartInstance.getActiveElements()
              : chartInstance.tooltip?.getActiveElements?.() ?? [];
            if (!active.length) return;
            const { left, right, top, bottom } = chartArea;
            const x = active[0].element.x;
            if (x < left || x > right) return;
            ctx.save();
            ctx.strokeStyle = "rgba(123, 230, 207, 0.6)";
            ctx.lineWidth = 1;
            ctx.setLineDash([4, 4]);
            ctx.beginPath();
            ctx.moveTo(x, top);
            ctx.lineTo(x, bottom);
            ctx.stroke();
            ctx.restore();
          },
        },
        {
          id: "range-selection",
          beforeDatasetsDraw(chartInstance) {
            if (!dragState.active || dragState.chart !== chartInstance) return;
            const { ctx, chartArea, scales } = chartInstance;
            if (!chartArea || !scales?.x) return;
            const { top, bottom } = chartArea;
            const startIdx = Math.min(dragState.startIndex, dragState.currentIndex);
            const endIdx = Math.max(dragState.startIndex, dragState.currentIndex);
            const xStart = scales.x.getPixelForValue(startIdx);
            const xEnd = scales.x.getPixelForValue(endIdx + 1);
            ctx.save();
            ctx.fillStyle = "rgba(123, 230, 207, 0.18)";
            ctx.strokeStyle = "rgba(123, 230, 207, 0.7)";
            ctx.lineWidth = 1;
            ctx.fillRect(xStart, top, xEnd - xStart, bottom - top);
            ctx.strokeRect(xStart, top, xEnd - xStart, bottom - top);
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
        interaction: {
          mode: "index",
          intersect: false,
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
    chart.__rangeMeta = {
      baseMs: startTime.getTime(),
      resolutionSeconds,
      tfBaseMs: base.getTime(),
      bucketMs: resolutionMinutes.value * 60_000,
      maxIndex: Math.max(labels.length - 1, 0),
    };
    chartInstances.set(symbol, chart);
    canvas.__chartRef = chart;
    installHoverSync(canvas, chart);
    installRangeSelection(canvas, chart);
  });
};

const clearHoverSync = () => {
  chartInstances.forEach((chart) => {
    chart.setActiveElements([]);
    chart.tooltip?.setActiveElements?.([], { x: 0, y: 0 });
    chart.update("none");
  });
};

const installHoverSync = (canvas, chart) => {
  if (canvas.__hoverSyncInstalled) return;
  canvas.__hoverSyncInstalled = true;
  canvas.addEventListener("mouseleave", () => {
    clearHoverSync();
  });
  canvas.addEventListener("mousemove", (event) => {
    const current = canvas.__chartRef ?? chart;
    if (!current?.canvas) return;
    const points = current.getElementsAtEventForMode(event, "index", { intersect: false }, false);
    if (!points.length) {
      clearHoverSync();
      return;
    }
    const dataIndex = points[0].index;
    chartInstances.forEach((otherChart) => {
      const meta = otherChart.getDatasetMeta(0);
      const element = meta?.data?.[dataIndex];
      if (!element) {
        otherChart.setActiveElements([]);
        otherChart.tooltip?.setActiveElements?.([], { x: 0, y: 0 });
        otherChart.update("none");
        return;
      }
      const active = [{ datasetIndex: 0, index: dataIndex }];
      otherChart.setActiveElements(active);
      otherChart.tooltip?.setActiveElements?.(active, { x: element.x, y: element.y });
      otherChart.update("none");
    });
  });
};

const getIndexFromEvent = (chart, event) => {
  const points = chart.getElementsAtEventForMode(event, "index", { intersect: false }, false);
  if (points?.length) {
    return points[0].index;
  }
  const scale = chart.scales?.x;
  if (!scale) return null;
  const rect = chart.canvas.getBoundingClientRect();
  const x = event.clientX - rect.left;
  const minuteIndex = Math.round(scale.getValueForPixel(x));
  if (!Number.isFinite(minuteIndex)) return null;
  const maxIndex =
    typeof chart?.__rangeMeta?.maxIndex === "number"
      ? chart.__rangeMeta.maxIndex
      : Math.max((chart.data?.labels?.length ?? 1) - 1, 0);
  return Math.min(Math.max(minuteIndex, 0), maxIndex);
};

const finalizeRangeSelection = () => {
  if (!dragState.active || !dragState.chart) return;
  const startMinute = Math.min(dragState.startIndex, dragState.currentIndex);
  const endMinute = Math.max(dragState.startIndex, dragState.currentIndex);
  const meta = dragState.chart.__rangeMeta;
  let startBucket = 0;
  let endBucket = 0;
  if (meta?.baseMs != null && meta?.resolutionSeconds != null && meta?.tfBaseMs != null && meta?.bucketMs != null) {
    const startTimeMs = meta.baseMs + startMinute * meta.resolutionSeconds * 1000;
    const endTimeMs = meta.baseMs + endMinute * meta.resolutionSeconds * 1000;
    startBucket = Math.floor((startTimeMs - meta.tfBaseMs) / meta.bucketMs);
    endBucket = Math.floor((endTimeMs - meta.tfBaseMs) / meta.bucketMs);
  } else {
    const baseMinute = rangeStart.value * resolutionMinutes.value;
    startBucket = Math.floor((baseMinute + startMinute) / resolutionMinutes.value);
    endBucket = Math.floor((baseMinute + endMinute) / resolutionMinutes.value);
  }
  dragState.active = false;
  dragState.chart.update("none");
  dragState.chart = null;
  rangeStart.value = startBucket;
  rangeEnd.value = endBucket;
  clampRange();
  if (sliderApi.value) {
    sliderApi.value.set([rangeStart.value, rangeEnd.value]);
  }
  renderCharts();
};

const installRangeSelection = (canvas, chart) => {
  if (canvas.__rangeSelectInstalled) return;
  canvas.__rangeSelectInstalled = true;
  canvas.style.touchAction = "none";
  canvas.addEventListener("pointerdown", (event) => {
    if (event.pointerType === "mouse" && event.button !== 0) return;
    event.preventDefault();
    const current = canvas.__chartRef ?? chart;
    if (!current?.canvas) return;
    const index = getIndexFromEvent(current, event);
    if (index == null) return;
    dragState.active = true;
    dragState.startIndex = index;
    dragState.currentIndex = index;
    dragState.chart = current;
    canvas.setPointerCapture(event.pointerId);
    current.update("none");
  });
  canvas.addEventListener("pointermove", (event) => {
    const current = canvas.__chartRef ?? chart;
    if (!dragState.active || dragState.chart !== current) return;
    event.preventDefault();
    const index = getIndexFromEvent(current, event);
    if (index == null) return;
    dragState.currentIndex = index;
    current.update("none");
  });
  canvas.addEventListener("pointerup", (event) => {
    const current = canvas.__chartRef ?? chart;
    if (dragState.active && dragState.chart === current) {
      canvas.releasePointerCapture(event.pointerId);
      finalizeRangeSelection();
    }
  });
  canvas.addEventListener("pointerleave", (event) => {
    const current = canvas.__chartRef ?? chart;
    if (dragState.active && dragState.chart === current) {
      canvas.releasePointerCapture(event.pointerId);
      finalizeRangeSelection();
    }
  });
};

const fetchPriceOverview = async (symbol, start, end, resolutionSeconds) => {
  const params = new URLSearchParams({
    start: formatDateTime(start),
    end: formatDateTime(end),
    resolution: String(resolutionSeconds),
  });
  const response = await fetch(`/market-visual-runner-bff/symbols/${encodeURIComponent(symbol)}/price-overview?${params}`);
  if (response.status === 404) {
    return null;
  }
  if (!response.ok) {
    throw new Error("Falha ao carregar price overview.");
  }
  return response.json();
};

const buildGapRanges = (flags, startIdx, endIdx, stepMinutes, resolutionSeconds, maxIndex) => {
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
  if (stepMinutes <= 1) return ranges;
  const expanded = [];
  const maxOffset = (endIdx - startIdx + 1) * stepMinutes - 1;
  ranges.forEach(([startBucket, endBucket]) => {
    const startMinute = startBucket * stepMinutes;
    let endMinute = (endBucket + 1) * stepMinutes - 1;
    if (endMinute > maxOffset) endMinute = maxOffset;
    expanded.push([startMinute, endMinute]);
  });
  if (!resolutionSeconds || resolutionSeconds === 60) return expanded;
  const bucketSeconds = Math.max(resolutionSeconds, 1);
  const remapped = expanded
    .map(([startMinute, endMinute]) => {
      const startSec = startMinute * 60;
      const endSec = (endMinute + 1) * 60 - 1;
      let startBucket = Math.floor(startSec / bucketSeconds);
      let endBucket = Math.floor(endSec / bucketSeconds);
      if (typeof maxIndex === "number") {
        startBucket = Math.min(Math.max(startBucket, 0), maxIndex);
        endBucket = Math.min(Math.max(endBucket, 0), maxIndex);
      }
      if (endBucket < startBucket) return null;
      return [startBucket, endBucket];
    })
    .filter(Boolean);
  return remapped;
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
  grid-template-columns: 1fr;
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
