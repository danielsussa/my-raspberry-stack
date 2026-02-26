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

const minutesCount = computed(() => {
  const first = timeframe.value?.quality_per_symbol?.[0];
  if (!first?.frame_quality_per_minute) return 0;
  return first.frame_quality_per_minute.length;
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
});

onBeforeUnmount(() => {
  if (sliderApi.value) {
    sliderApi.value.destroy();
    sliderApi.value = null;
  }
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
</style>
