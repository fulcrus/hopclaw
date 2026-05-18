<script setup>
import { useI18n } from "../i18n";

const { t, ta } = useI18n();

const remoteConfig = `diagnostics:
  telemetry_enabled: true
  telemetry_endpoint: https://telemetry.example.com/api/v1/ingest/events
  telemetry_token: env:HOPCLAW_TELEMETRY_TOKEN
  telemetry_timeout: 5s
  telemetry_debug_log: false`;

const localCollectorConfig = `diagnostics:
  telemetry_enabled: false
  telemetry_collector_enabled: true
  telemetry_collector_dir: ./.hopclaw/telemetry-collector
  telemetry_collector_auth_token: env:HOPCLAW_TELEMETRY_COLLECTOR_TOKEN
  telemetry_collector_max_upload_bytes: 4194304

# local collector ingest path:
# POST /telemetry/events`;
</script>

<template>
  <div class="page-shell telemetry-page">
    <section class="page-intro">
      <div class="container">
        <div class="hero-stage">
          <div class="hero-grid telemetry-hero-grid">
            <div class="stack hero-copy">
              <span class="eyebrow">{{ t("telemetry.badge") }}</span>
              <h1 class="headline">{{ t("telemetry.title") }}</h1>
              <p class="lede">{{ t("telemetry.desc") }}</p>

              <ul class="check-list fact-list">
                <li v-for="item in ta('telemetry.facts')" :key="item">
                  {{ item }}
                </li>
              </ul>

              <div class="hero-actions">
                <router-link to="/docs" class="btn btn-primary">{{
                  t("telemetry.primaryCta")
                }}</router-link>
              </div>
            </div>

            <div class="surface surface-dark endpoint-panel">
              <div class="section-head compact-head">
                <span class="badge badge-accent">{{
                  t("telemetry.endpointBadge")
                }}</span>
                <p class="section-subtitle">
                  {{ t("telemetry.endpointDesc") }}
                </p>
              </div>

              <pre
                class="code-block"
              ><code>POST https://telemetry.example.com/api/v1/ingest/events</code></pre>

              <div class="endpoint-grid">
                <article
                  v-for="item in ta('telemetry.endpoints')"
                  :key="item.path"
                  class="endpoint-card"
                >
                  <span class="endpoint-label">{{ item.label }}</span>
                  <code class="endpoint-path">{{ item.path }}</code>
                  <p>{{ item.note }}</p>
                </article>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container split telemetry-split">
        <div class="section-frame list-frame">
          <div class="section-head">
            <span class="eyebrow">{{ t("telemetry.payloadEyebrow") }}</span>
            <h2 class="section-title">{{ t("telemetry.payloadTitle") }}</h2>
            <p class="section-subtitle">{{ t("telemetry.payloadDesc") }}</p>
          </div>

          <div class="grid-2 list-grid">
            <article class="card callout-card">
              <span class="badge badge-green">{{
                t("telemetry.sendsTitle")
              }}</span>
              <ul class="check-list">
                <li v-for="item in ta('telemetry.sends')" :key="item">
                  {{ item }}
                </li>
              </ul>
            </article>

            <article class="card callout-card">
              <span class="badge badge-orange">{{
                t("telemetry.neverTitle")
              }}</span>
              <ul class="check-list">
                <li v-for="item in ta('telemetry.never')" :key="item">
                  {{ item }}
                </li>
              </ul>
            </article>
          </div>
        </div>

        <div class="section-frame">
          <div class="section-head">
            <span class="eyebrow">{{ t("telemetry.eventsEyebrow") }}</span>
            <h2 class="section-title">{{ t("telemetry.eventsTitle") }}</h2>
            <p class="section-subtitle">{{ t("telemetry.eventsDesc") }}</p>
          </div>

          <div class="event-grid">
            <article
              v-for="item in ta('telemetry.events')"
              :key="item.name"
              class="card callout-card event-card"
            >
              <code class="event-name">{{ item.name }}</code>
              <p>{{ item.desc }}</p>
            </article>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-frame">
          <div class="section-head">
            <span class="eyebrow">{{ t("telemetry.behaviorEyebrow") }}</span>
            <h2 class="section-title">{{ t("telemetry.behaviorTitle") }}</h2>
            <p class="section-subtitle">{{ t("telemetry.behaviorDesc") }}</p>
          </div>

          <div class="grid-3">
            <article
              v-for="item in ta('telemetry.behavior')"
              :key="item.title"
              class="card callout-card"
            >
              <h3>{{ item.title }}</h3>
              <p>{{ item.desc }}</p>
            </article>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-frame">
          <div class="section-head">
            <span class="eyebrow">{{ t("telemetry.configEyebrow") }}</span>
            <h2 class="section-title">{{ t("telemetry.configTitle") }}</h2>
            <p class="section-subtitle">{{ t("telemetry.configDesc") }}</p>
          </div>

          <div class="grid-2 config-grid">
            <article class="surface config-card">
              <div class="section-head compact-head">
                <span class="badge badge-primary">{{
                  t("telemetry.remoteLabel")
                }}</span>
                <h3>{{ t("telemetry.remoteTitle") }}</h3>
                <p class="section-subtitle">
                  {{ t("telemetry.remoteDesc") }}
                </p>
              </div>
              <pre class="code-block"><code>{{ remoteConfig }}</code></pre>
            </article>

            <article class="surface config-card">
              <div class="section-head compact-head">
                <span class="badge badge-accent">{{
                  t("telemetry.localCollectorLabel")
                }}</span>
                <h3>{{ t("telemetry.localCollectorTitle") }}</h3>
                <p class="section-subtitle">
                  {{ t("telemetry.localCollectorDesc") }}
                </p>
              </div>
              <pre class="code-block"><code>{{ localCollectorConfig }}</code></pre>
            </article>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.telemetry-hero-grid,
.hero-copy,
.endpoint-panel,
.list-frame,
.config-card {
  display: grid;
  gap: 20px;
}

.compact-head {
  margin-bottom: 0;
}

.fact-list {
  max-width: 54rem;
}

.endpoint-grid,
.event-grid {
  display: grid;
  gap: 14px;
}

.endpoint-card {
  display: grid;
  gap: 8px;
  padding: 16px 18px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 22px;
  background: rgba(255, 255, 255, 0.04);
}

.endpoint-label {
  color: rgba(255, 244, 233, 0.66);
  font-size: 0.76rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.endpoint-path {
  color: var(--paper-strong);
  font-size: 0.84rem;
  word-break: break-word;
}

.event-name {
  color: var(--accent-strong);
  font-size: 0.84rem;
  word-break: break-word;
}

.endpoint-card p {
  margin: 0;
  color: rgba(255, 244, 233, 0.8);
}

.event-card p {
  margin: 0;
}

.list-grid,
.config-grid {
  align-items: stretch;
}

.config-card h3 {
  margin: 0;
  font-size: 1.2rem;
}

.event-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.event-card {
  gap: 10px;
}

@media (max-width: 980px) {
  .event-grid {
    grid-template-columns: 1fr;
  }
}
</style>
