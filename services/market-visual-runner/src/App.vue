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
        <div>
          <div class="label">Ticks Extra</div>
          <div class="value">{{ ticksRequested || 0 }}</div>
        </div>
        <div>
          <div class="label">Resolution (s)</div>
          <div class="value">{{ customResolutionSeconds || "auto" }}</div>
        </div>
      </div>
      <div class="slider-row">
        <div ref="sliderEl" class="noui-slider"></div>
        <div class="slider-note" v-if="loading">Carregando timeframe…</div>
        <div class="slider-note error" v-else-if="error">{{ error }}</div>
        <div class="slider-note" v-else-if="!minutesCount">Sem dados.</div>
      </div>
      <div class="panel-actions">
        <button v-if="showAiPredict" type="button" class="ai-btn">AI PREDICT</button>
        <button
          v-if="hasSelection"
          type="button"
          class="increase-btn"
          @click="requestMoreTicks"
        >
          INCREASE RESOLUTION
        </button>
        <button
          v-if="hasSelection"
          type="button"
          class="zoom-btn"
          @click="zoomToSelection"
        >
          ZOOM SELECTION
        </button>
        <button type="button" class="reset-btn" @click="resetSession">RESET</button>
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

  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
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
const wsRef = ref(null);
const wsStatus = ref("disconnected");
const wsRequests = new Map();
let wsRequestSeq = 0;
let renderToken = 0;
const dragState = { active: false, startIndex: 0, currentIndex: 0, chart: null, didSelect: false };
const computeMarkers = new Map();
const selectionRanges = reactive(new Map());
const showAiPredict = ref(false);
const pendingState = ref(null);
const ticksRequested = ref(0);
const lastSymbol = ref("");
const lastSelectionSymbol = ref("");
const customResolutionSeconds = ref(0);
let wsReconnectTimer = null;
let wsReconnectAttempts = 0;
let stateSaveTimer = null;
let isApplyingState = false;
const hasPendingState = computed(() => !!pendingState.value);
const restoredRange = ref(false);
const hasSelection = computed(() => selectionRanges.size > 0);

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
  return formatDateTimeLabel(current);
});

const endMinuteLabel = computed(() => {
  if (!timeframe.value?.start || !minutesCount.value) return "";
  const base = new Date(timeframe.value.start);
  if (Number.isNaN(base.getTime())) return "";
  const current = new Date(base.getTime() + (rangeEnd.value + 1) * resolutionMinutes.value * 60_000 - 60_000);
  return formatDateTimeLabel(current);
});

const fetchTimeframe = async () => {
  loading.value = true;
  error.value = "";
  try {
    const data = await fetchTimeframeWS();
    timeframe.value = data;
    if (!pendingState.value) {
      rangeStart.value = 0;
      rangeEnd.value = endIndex.value;
      clampRange();
    }
    initOrUpdateSlider();
    renderCharts();
    applyPendingState();
  } catch (err) {
    error.value = err?.message ?? "Erro ao carregar timeframe.";
  } finally {
    loading.value = false;
  }
};

onMounted(() => {
  connectWebsocket();
  fetchTimeframe();
});

watch(rangeStart, () => {
  clampRange();
});
watch(rangeEnd, () => {
  clampRange();
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
    sliderApi.value.on("change", () => {
      renderCharts();
      sendRangeSelectionNow();
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
    sliderApi.value.enable();
  }
};

watch(minutesCount, () => {
  if (!minutesCount.value) return;
  if (hasPendingState.value || isApplyingState || restoredRange.value) {
    initOrUpdateSlider();
    renderCharts();
    return;
  }
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
  closeWebsocket();
});

const formatDateTimeLabel = (date) => {
  if (!(date instanceof Date)) return "";
  const year = date.getUTCFullYear();
  const month = String(date.getUTCMonth() + 1).padStart(2, "0");
  const day = String(date.getUTCDate()).padStart(2, "0");
  const hours = String(date.getUTCHours()).padStart(2, "0");
  const minutes = String(date.getUTCMinutes()).padStart(2, "0");
  const seconds = String(date.getUTCSeconds()).padStart(2, "0");
  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds} UTC`;
};

const formatDateTimeParam = (date) => {
  if (!(date instanceof Date)) return "";
  return date.toISOString();
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

const renderChartsWithPayloads = async (payloads, startTime, endTime, resolutionSeconds) => {
  if (!timeframe.value?.start) return;
  dragState.active = false;
  dragState.chart = null;
  const base = new Date(timeframe.value.start);
  if (Number.isNaN(base.getTime())) return;
  const qualityMap = new Map(
    (timeframe.value?.frame_quality ?? []).map((entry) => [entry.symbol, entry.quality])
  );

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
          id: "compute-marker",
          afterDatasetsDraw(chartInstance) {
            const { ctx, chartArea, scales } = chartInstance;
            if (!chartArea || !scales?.x) return;
            const symbol = chartInstance.__computeSymbol;
            if (!symbol) return;
            const markerIndex = computeMarkers.get(symbol);
            if (markerIndex == null) return;
            const { top, bottom, left, right } = chartArea;
            const x = scales.x.getPixelForValue(markerIndex);
            if (!Number.isFinite(x) || x < left || x > right) return;
            ctx.save();
            ctx.strokeStyle = "rgba(255, 209, 102, 0.85)";
            ctx.lineWidth = 2;
            ctx.setLineDash([]);
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
        {
          id: "selection-overlay",
          afterDatasetsDraw(chartInstance) {
            const { ctx, chartArea, scales } = chartInstance;
            if (!chartArea || !scales?.x) return;
            const symbol = chartInstance.__computeSymbol;
            if (!symbol) return;
            const selection = selectionRanges.get(symbol);
            if (!selection) return;
            const { top, bottom } = chartArea;
            const xStart = scales.x.getPixelForValue(selection.startIdx);
            const xEnd = scales.x.getPixelForValue(selection.endIdx + 1);
            if (!Number.isFinite(xStart) || !Number.isFinite(xEnd)) return;
            ctx.save();
            ctx.fillStyle = "rgba(255, 209, 102, 0.12)";
            ctx.strokeStyle = "rgba(255, 209, 102, 0.6)";
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
    chart.__computeSymbol = symbol;
    chartInstances.set(symbol, chart);
    canvas.__chartRef = chart;
    installHoverSync(canvas, chart);
    installRangeSelection(canvas, chart);
    installComputeMarker(canvas, chart, symbol);
  });
};

const renderCharts = async () => {
  if (!symbols.value.length) return;
  if (!timeframe.value?.start) return;
  const base = new Date(timeframe.value.start);
  if (Number.isNaN(base.getTime())) return;
  const startTime = new Date(base.getTime() + rangeStart.value * resolutionMinutes.value * 60_000);
  const endTime = new Date(base.getTime() + (rangeEnd.value + 1) * resolutionMinutes.value * 60_000 - 60_000);
  const resolutionSeconds = customResolutionSeconds.value > 0
    ? customResolutionSeconds.value
    : computeResolutionSeconds(startTime, endTime);
  const token = (renderToken += 1);

  let payloads = [];
  try {
    payloads = await fetchPriceOverviewBatch(symbols.value, startTime, endTime, resolutionSeconds);
  } catch (err) {
    error.value = err?.message ?? "Erro ao carregar price overview.";
    return;
  }

  if (token !== renderToken) return;
  renderChartsWithPayloads(payloads, startTime, endTime, resolutionSeconds);
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
  const symbol = dragState.chart.__computeSymbol;
  if (symbol) {
    selectionRanges.set(symbol, {
      startIdx: startMinute,
      endIdx: endMinute,
      startBucket,
      endBucket,
    });
    lastSelectionSymbol.value = symbol;
  }
  dragState.active = false;
  dragState.didSelect = true;
  dragState.chart.update("none");
  dragState.chart = null;
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

const installComputeMarker = (canvas, chart, symbol) => {
  if (canvas.__computeMarkerInstalled) return;
  canvas.__computeMarkerInstalled = true;
  canvas.addEventListener("click", (event) => {
    if (dragState.didSelect) {
      dragState.didSelect = false;
      return;
    }
    const current = canvas.__chartRef ?? chart;
    if (!current?.canvas) return;
    const index = getIndexFromEvent(current, event);
    if (index == null) return;
    computeMarkers.set(symbol, index);
    lastSymbol.value = symbol;
    showAiPredict.value = true;
    saveComputeState();
    current.update("none");
  });
};

const buildWsUrl = () => {
  const protocol = window.location.protocol === "https:" ? "wss" : "ws";
  return `${protocol}://${window.location.host}/market-visual-runner-bff/ws`;
};

const connectWebsocket = () => {
  const existing = wsRef.value;
  if (existing && (existing.readyState === WebSocket.OPEN || existing.readyState === WebSocket.CONNECTING)) {
    return;
  }
  if (wsReconnectTimer) {
    clearTimeout(wsReconnectTimer);
    wsReconnectTimer = null;
  }
  const ws = new WebSocket(buildWsUrl());
  wsRef.value = ws;
  wsStatus.value = "connecting";

  ws.addEventListener("open", () => {
    wsStatus.value = "open";
    wsReconnectAttempts = 0;
    requestState();
  });

  ws.addEventListener("close", () => {
    wsStatus.value = "closed";
    scheduleReconnect();
  });

  ws.addEventListener("error", () => {
    wsStatus.value = "error";
    scheduleReconnect();
  });

  ws.addEventListener("message", (event) => {
    let payload;
    try {
      payload = JSON.parse(event.data);
    } catch {
      return;
    }
    const requestId = payload?.request_id;
    if (!requestId || !wsRequests.has(requestId)) return;
    const entry = wsRequests.get(requestId);
    wsRequests.delete(requestId);
    clearTimeout(entry.timeout);
    if (payload?.type === "error") {
      entry.reject(new Error(payload?.message || "Erro no WebSocket."));
      return;
    }
    entry.resolve(payload);
  });
};

const closeWebsocket = () => {
  if (wsReconnectTimer) {
    clearTimeout(wsReconnectTimer);
    wsReconnectTimer = null;
  }
  wsReconnectAttempts = 0;
  wsRequests.forEach((entry) => {
    clearTimeout(entry.timeout);
    entry.reject(new Error("WebSocket encerrado."));
  });
  wsRequests.clear();
  if (wsRef.value) {
    wsRef.value.close();
  }
  wsRef.value = null;
};

const scheduleReconnect = () => {
  if (wsReconnectTimer) return;
  wsReconnectAttempts += 1;
  const delay = Math.min(30000, 1000 * 2 ** Math.min(wsReconnectAttempts, 5));
  wsReconnectTimer = setTimeout(() => {
    wsReconnectTimer = null;
    connectWebsocket();
  }, delay);
};

const waitForWsOpen = () =>
  new Promise((resolve, reject) => {
    const ws = wsRef.value;
    if (!ws) {
      reject(new Error("WebSocket indisponivel."));
      return;
    }
    if (ws.readyState === WebSocket.OPEN) {
      resolve(ws);
      return;
    }
    if (ws.readyState !== WebSocket.CONNECTING) {
      reject(new Error("WebSocket indisponivel."));
      return;
    }
    const timer = setTimeout(() => {
      cleanup();
      reject(new Error("WebSocket indisponivel."));
    }, 5000);
    const cleanup = () => {
      clearTimeout(timer);
      ws.removeEventListener("open", handleOpen);
      ws.removeEventListener("error", handleError);
      ws.removeEventListener("close", handleError);
    };
    const handleOpen = () => {
      cleanup();
      resolve(ws);
    };
    const handleError = () => {
      cleanup();
      reject(new Error("WebSocket indisponivel."));
    };
    ws.addEventListener("open", handleOpen);
    ws.addEventListener("error", handleError);
    ws.addEventListener("close", handleError);
  });

const sendWsRequest = (type, payload) =>
  new Promise(async (resolve, reject) => {
    let ws;
    try {
      ws = await waitForWsOpen();
    } catch (err) {
      reject(err);
      return;
    }
    const requestId = `req_${Date.now()}_${wsRequestSeq++}`;
    const timeout = setTimeout(() => {
      wsRequests.delete(requestId);
      reject(new Error("WebSocket timeout."));
    }, 15000);
    wsRequests.set(requestId, { resolve, reject, timeout });
    ws.send(JSON.stringify({ type, request_id: requestId, ...payload }));
  });

const fetchTimeframeWS = async () => {
  const response = await sendWsRequest("timeframe", {});
  if (response?.data) {
    return response.data;
  }
  throw new Error("Falha ao carregar timeframe.");
};

const fetchPriceOverviewBatch = async (symbolsList, start, end, resolutionSeconds) => {
  if (!symbolsList?.length) return [];
  const response = await sendWsRequest("price_overview_batch", {
    symbols: symbolsList,
    start: formatDateTimeParam(start),
    end: formatDateTimeParam(end),
    resolution: resolutionSeconds,
  });
  const items = Array.isArray(response?.data) ? response.data : [];
  return items.map((item) => ({ symbol: item.symbol, result: item.data ?? null }));
};

const requestState = async () => {
  try {
    const response = await sendWsRequest("state_get", {});
    if (response?.data) {
      pendingState.value = response.data;
      applyPendingState();
    }
  } catch {
    // ignore
  }
};

const applyPendingState = () => {
  const state = pendingState.value;
  if (!state || !timeframe.value || !minutesCount.value) return;
  pendingState.value = null;
  isApplyingState = true;
  let didApplyRange = false;
  const base = new Date(timeframe.value.start);
  if (!Number.isNaN(base.getTime()) && state.range_start_time && state.range_end_time) {
    const startTime = new Date(state.range_start_time);
    const endTime = new Date(state.range_end_time);
    if (!Number.isNaN(startTime.getTime()) && !Number.isNaN(endTime.getTime())) {
      const bucketMs = resolutionMinutes.value * 60_000;
      const startOffset = startTime.getTime() - base.getTime();
      const endOffset = endTime.getTime() - base.getTime();
      rangeStart.value = Math.max(0, Math.min(Math.floor(startOffset / bucketMs), endIndex.value));
      rangeEnd.value = Math.max(0, Math.min(Math.floor(endOffset / bucketMs), endIndex.value));
      didApplyRange = true;
    }
  } else if (typeof state.range_start === "number" && typeof state.range_end === "number") {
    rangeStart.value = Math.max(0, Math.min(state.range_start, endIndex.value));
    rangeEnd.value = Math.max(0, Math.min(state.range_end, endIndex.value));
    didApplyRange = true;
  }
  clampRange();
  if (sliderApi.value) {
    sliderApi.value.set([rangeStart.value, rangeEnd.value]);
  }
  if (state.markers && typeof state.markers === "object") {
    computeMarkers.clear();
    Object.entries(state.markers).forEach(([symbol, index]) => {
      if (Number.isFinite(index)) {
        computeMarkers.set(symbol, index);
      }
    });
    showAiPredict.value = computeMarkers.size > 0;
  }
  if (typeof state.resolution === "string" && state.resolution) {
    // Keep for session display/diagnostics; timeframe resolution still drives rendering.
  }
  if (Number.isFinite(state.ticks_requested)) {
    ticksRequested.value = state.ticks_requested;
  }
  if (typeof state.last_symbol === "string") {
    lastSymbol.value = state.last_symbol;
  }
  if (Number.isFinite(state.custom_resolution_seconds)) {
    customResolutionSeconds.value = state.custom_resolution_seconds;
  }
  if (sliderApi.value) {
    sliderApi.value.enable();
  }
  chartInstances.forEach((chart) => chart.update("none"));
  if (didApplyRange) {
    restoredRange.value = true;
  }
  isApplyingState = false;
};

const buildComputeStatePayload = () => ({
  range_start: rangeStart.value,
  range_end: rangeEnd.value,
  markers: Object.fromEntries(computeMarkers.entries()),
  ticks_requested: ticksRequested.value,
  last_symbol: lastSymbol.value,
  resolution: timeframe.value?.resolution ?? "",
  custom_resolution_seconds: customResolutionSeconds.value,
});

const saveComputeState = async () => {
  try {
    await sendWsRequest("state_update", { state: buildComputeStatePayload() });
  } catch {
    // ignore
  }
};

const scheduleSaveComputeState = () => {
  if (isApplyingState) return;
  if (stateSaveTimer) clearTimeout(stateSaveTimer);
  stateSaveTimer = setTimeout(() => {
    stateSaveTimer = null;
    saveComputeState();
  }, 300);
};

const sendRangeSelectionNow = async () => {
  if (isApplyingState) return;
  const range = getSelectedRangeTimes();
  if (!range) return;
  try {
    await sendWsRequest("range_selection", {
      start: formatDateTimeParam(range.startTime),
      end: formatDateTimeParam(range.endTime),
      range_start: rangeStart.value,
      range_end: rangeEnd.value,
    });
  } catch {
    // ignore
  }
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

const getSelectedRangeTimes = () => {
  if (!timeframe.value?.start || !minutesCount.value) return null;
  const base = new Date(timeframe.value.start);
  if (Number.isNaN(base.getTime())) return null;
  const startTime = new Date(base.getTime() + rangeStart.value * resolutionMinutes.value * 60_000);
  const endTime = new Date(base.getTime() + (rangeEnd.value + 1) * resolutionMinutes.value * 60_000 - 60_000);
  return { startTime, endTime };
};

const getSelectionRangeTimes = () => {
  if (!timeframe.value?.start || !minutesCount.value) return null;
  const symbol = lastSelectionSymbol.value || [...selectionRanges.keys()][0];
  if (!symbol) return null;
  const selection = selectionRanges.get(symbol);
  if (!selection) return null;
  const base = new Date(timeframe.value.start);
  if (Number.isNaN(base.getTime())) return null;
  const startTime = new Date(base.getTime() + selection.startBucket * resolutionMinutes.value * 60_000);
  const endTime = new Date(base.getTime() + (selection.endBucket + 1) * resolutionMinutes.value * 60_000 - 60_000);
  return { startTime, endTime, selection };
};

const requestMoreTicks = async () => {
  const selectionRange = getSelectionRangeTimes();
  const range = selectionRange ?? getSelectedRangeTimes();
  if (!range) return;
  loading.value = true;
  error.value = "";
  try {
    connectWebsocket();
    const response = await sendWsRequest("increase_resolution", {
      ticks: 5000,
      start: formatDateTimeParam(range.startTime),
      end: formatDateTimeParam(range.endTime),
      symbols: symbols.value,
    });
    const items = Array.isArray(response?.data?.items) ? response.data.items : [];
    const resolutionSeconds =
      typeof response?.data?.resolution_seconds === "number" && response.data.resolution_seconds > 0
        ? response.data.resolution_seconds
        : computeResolutionSeconds(range.startTime, range.endTime);
    const payloads = items.map((item) => ({ symbol: item.symbol, result: item.data ?? null }));
    customResolutionSeconds.value = resolutionSeconds;
    renderChartsWithPayloads(payloads, range.startTime, range.endTime, resolutionSeconds);
    ticksRequested.value += 5000;
    await saveComputeState();
  } catch (err) {
    error.value = err?.message ?? "Erro ao pedir mais ticks.";
  } finally {
    loading.value = false;
  }
};

const zoomToSelection = () => {
  const selectionRange = getSelectionRangeTimes();
  if (!selectionRange?.selection) return;
  rangeStart.value = Math.max(0, Math.min(selectionRange.selection.startBucket, endIndex.value));
  rangeEnd.value = Math.max(0, Math.min(selectionRange.selection.endBucket, endIndex.value));
  clampRange();
  if (sliderApi.value) {
    sliderApi.value.set([rangeStart.value, rangeEnd.value]);
  }
  renderCharts();
  sendRangeSelectionNow();
};

const resetSession = async () => {
  loading.value = true;
  error.value = "";
  let resetState = null;
  try {
    connectWebsocket();
    const response = await sendWsRequest("state_reset", {});
    resetState = response?.data ?? null;
  } catch (err) {
    error.value = err?.message ?? "Erro ao resetar sessao.";
  } finally {
    loading.value = false;
  }
  if (resetState) {
    pendingState.value = resetState;
    applyPendingState();
  }
  showAiPredict.value = false;
  computeMarkers.clear();
  ticksRequested.value = 0;
  lastSymbol.value = "";
  customResolutionSeconds.value = 0;
  selectionRanges.clear();
  lastSelectionSymbol.value = "";
  rangeStart.value = 0;
  rangeEnd.value = endIndex.value;
  clampRange();
  if (sliderApi.value) {
    sliderApi.value.set([rangeStart.value, rangeEnd.value]);
    sliderApi.value.enable();
  }
  restoredRange.value = false;
  pendingState.value = null;
  if (sliderApi.value) {
    sliderApi.value.updateOptions(
      {
        range: { min: 0, max: endIndex.value },
        step: 1,
        start: [rangeStart.value, rangeEnd.value],
      },
      true
    );
  }
  chartInstances.forEach((chart) => chart.update("none"));
  renderCharts();
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

.panel-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.8rem;
}

.ai-btn {
  border: 1px solid rgba(123, 230, 207, 0.45);
  border-radius: 999px;
  padding: 0.7rem 1.4rem;
  font-size: 0.8rem;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #e7faf7;
  background: rgba(8, 18, 19, 0.7);
  box-shadow: 0 10px 20px rgba(4, 11, 12, 0.35);
  cursor: pointer;
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
}

.ai-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 14px 24px rgba(4, 11, 12, 0.45);
  border-color: rgba(123, 230, 207, 0.75);
}

.increase-btn {
  border: 1px solid rgba(255, 209, 102, 0.6);
  border-radius: 999px;
  padding: 0.7rem 1.4rem;
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: #fff1c2;
  background: rgba(26, 20, 10, 0.7);
  box-shadow: 0 10px 20px rgba(4, 11, 12, 0.35);
  cursor: pointer;
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
}

.increase-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 14px 24px rgba(4, 11, 12, 0.45);
  border-color: rgba(255, 209, 102, 0.9);
}

.reset-btn {
  border: 1px solid rgba(255, 122, 122, 0.6);
  border-radius: 999px;
  padding: 0.7rem 1.4rem;
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: #ffd6d6;
  background: rgba(26, 10, 10, 0.7);
  box-shadow: 0 10px 20px rgba(4, 11, 12, 0.35);
  cursor: pointer;
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
}

.reset-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 14px 24px rgba(4, 11, 12, 0.45);
  border-color: rgba(255, 122, 122, 0.9);
}

.zoom-btn {
  border: 1px solid rgba(123, 190, 255, 0.6);
  border-radius: 999px;
  padding: 0.7rem 1.4rem;
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: #d6ebff;
  background: rgba(10, 16, 26, 0.7);
  box-shadow: 0 10px 20px rgba(4, 11, 12, 0.35);
  cursor: pointer;
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
}

.zoom-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 14px 24px rgba(4, 11, 12, 0.45);
  border-color: rgba(123, 190, 255, 0.9);
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
