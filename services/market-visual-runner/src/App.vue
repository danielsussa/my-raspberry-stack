<template>
  <div class="page">
    <header class="hero">
      <div class="title">Market Visual Runner</div>
      <div class="subtitle">Visualize streams and snapshots in one place.</div>
    </header>

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

<style scoped>
.list {
  margin: 0;
  padding-left: 1.1rem;
  color: #e6f4f1;
  line-height: 1.6;
}
</style>
