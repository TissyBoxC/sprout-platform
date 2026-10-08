<script setup lang="ts">
import { computed, onMounted } from 'vue'

import type {
  FeatureCenterFeature,
  FeatureConfigValue,
  FeatureHealthStatus,
  FeatureMetric,
} from '@/api/adminFeatureCenter'
import { useFeatureCenterStore } from '@/features/feature_center/application/featureCenterStore'
import FeatureCenterField from '@/features/feature_center/presentation/FeatureCenterField.vue'

const store = useFeatureCenterStore()

const selectedFeature = computed(() => store.selectedFeature)
const hasFeatures = computed(() => store.features.length > 0)
const hasVisibleFeatures = computed(() => store.filteredFeatures.length > 0)
const healthStatus = computed(
  () => selectedFeature.value?.health.status ?? 'unknown',
)

onMounted(() => {
  void store.load()
})

function onSearchInput(event: Event): void {
  const target = event.target
  if (target instanceof HTMLInputElement) {
    store.setFilters({ keyword: target.value })
  }
}

function onCategoryChange(event: Event): void {
  const target = event.target
  if (target instanceof HTMLSelectElement) {
    store.setFilters({ category: target.value })
  }
}

function selectFeature(feature: FeatureCenterFeature): void {
  if (feature.id === store.selectedFeatureId) {
    return
  }
  void store.selectFeature(feature.id)
}

function updateConfigValue(key: string, value: FeatureConfigValue): void {
  store.setConfigValue(key, value)
}

function featureStatusLabel(feature: FeatureCenterFeature): string {
  return (
    {
      enabled: '已启用',
      planned: '规划中',
      disabled: '已停用',
      maintenance: '维护中',
      unknown: '状态未明',
    }[feature.status] ?? '状态未明'
  )
}

function featureStatusClass(feature: FeatureCenterFeature): string {
  return feature.status
}

function healthLabel(value: FeatureHealthStatus): string {
  return (
    {
      healthy: '运行正常',
      degraded: '存在波动',
      unhealthy: '需要处理',
      checking: '检查中',
      unknown: '暂未接入健康检查',
    }[value] ?? '暂未接入健康检查'
  )
}

function formatTime(value: string): string {
  if (!value) {
    return '暂未接入'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '暂未接入'
  }
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function metricGridClass(metrics: FeatureMetric[]): string {
  return metrics.length > 2 ? 'metric-grid metric-grid-wide' : 'metric-grid'
}

function metricValue(value: string): string {
  const normalized = value.trim()
  return normalized === '' || normalized === '暂无' ? '暂未接入' : value
}

function plannedReason(feature: FeatureCenterFeature): string {
  return (
    feature.readOnlyReason ||
    '该功能尚未实现，当前仅展示规划信息，完成实现后开放配置。'
  )
}
</script>

<template>
  <section class="feature-center-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">服务管理</p>
        <h1>功能中心</h1>
        <p class="page-description">
          查看和调整各功能的运行参数，检查服务健康状态。密钥只显示配置状态，不会在此处回显。
        </p>
      </div>
      <div class="header-actions">
        <button
          type="button"
          class="secondary"
          :disabled="store.isLoading || store.isRefreshing"
          @click="store.refresh"
        >
          {{ store.isRefreshing ? '正在刷新…' : '刷新功能列表' }}
        </button>
      </div>
    </header>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="notice success" role="status">
        {{ store.lastMessage }}
      </p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.conflictMessage" class="notice conflict" role="alert">
        <span>{{ store.conflictMessage }}</span>
        <button type="button" @click="store.refresh">重新加载</button>
      </p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.error || store.detailError" class="notice error" role="alert">
        {{ store.error?.message || store.detailError?.message }}
        <button
          v-if="(store.error || store.detailError)?.retryable"
          type="button"
          @click="store.load"
        >
          重新加载
        </button>
      </p>
    </Transition>

    <Transition name="page" mode="out-in">
      <div
        :key="
          store.isLoading
            ? 'loading'
            : !hasFeatures
              ? 'empty'
              : selectedFeature
                ? selectedFeature.id
                : 'list'
        "
      >
        <div v-if="store.isLoading" class="state-panel">
          <span class="state-spinner" aria-hidden="true"></span>
          <span>正在读取功能配置…</span>
        </div>

        <div v-else-if="!hasFeatures" class="state-panel state-panel-empty">
          <span class="state-mark" aria-hidden="true">☆</span>
          <strong>还没有可管理的功能</strong>
          <span>完成功能接入后，模块会按类别显示在这里。</span>
        </div>

        <div v-else class="feature-workspace">
          <aside class="feature-list-panel" aria-label="功能模块列表">
            <div class="list-heading">
              <div>
                <h2>功能模块</h2>
                <p>{{ store.filteredFeatures.length }} / {{ store.features.length }} 个功能</p>
              </div>
            </div>

            <div class="list-filters">
              <label>
                <span class="sr-only">搜索功能</span>
                <input
                  :value="store.filters.keyword"
                  type="search"
                  placeholder="搜索名称、说明或负责人"
                  @input="onSearchInput"
                />
              </label>
              <label>
                <span class="sr-only">按类别筛选</span>
                <select :value="store.filters.category" @change="onCategoryChange">
                  <option value="all">全部类别</option>
                  <option
                    v-for="category in store.categories"
                    :key="category.id"
                    :value="category.id"
                  >
                    {{ category.label }}
                  </option>
                </select>
              </label>
            </div>

            <div v-if="!hasVisibleFeatures" class="list-empty">
              <strong>没有匹配的功能</strong>
              <span>换一个关键词或类别再试。</span>
            </div>

            <div v-else class="feature-groups">
              <section
                v-for="group in store.groupedFeatures"
                :key="group.id"
                class="feature-group"
              >
                <p class="group-title">{{ group.label }}</p>
                <ul>
                  <li v-for="feature in group.features" :key="feature.id">
                    <button
                      type="button"
                      :class="{ active: feature.id === store.selectedFeatureId }"
                      @click="selectFeature(feature)"
                    >
                      <span class="feature-button-main">
                        <strong>{{ feature.name }}</strong>
                        <small>{{ feature.owner || '负责人待补充' }}</small>
                      </span>
                      <span
                        class="mini-status"
                        :class="featureStatusClass(feature)"
                      >
                        {{ featureStatusLabel(feature) }}
                      </span>
                    </button>
                  </li>
                </ul>
              </section>
            </div>
          </aside>

          <main class="feature-detail-panel">
            <div v-if="store.isDetailLoading" class="detail-loading">
              <span class="state-spinner" aria-hidden="true"></span>
              正在读取功能详情…
            </div>

            <template v-else-if="selectedFeature">
              <header class="detail-header">
                <div class="detail-title">
                  <p class="eyebrow">{{ selectedFeature.categoryLabel }}</p>
                  <h2>{{ selectedFeature.name }}</h2>
                  <p>{{ selectedFeature.description || '暂未填写功能说明。' }}</p>
                </div>
                <span
                  class="feature-status"
                  :class="featureStatusClass(selectedFeature)"
                >
                  {{ featureStatusLabel(selectedFeature) }}
                </span>
              </header>

              <dl class="feature-meta">
                <div>
                  <dt>负责人</dt>
                  <dd>{{ selectedFeature.owner || '待补充' }}</dd>
                </div>
                <div>
                  <dt>更新时间</dt>
                  <dd>{{ formatTime(selectedFeature.updatedAt) }}</dd>
                </div>
                <div>
                  <dt>配置版本</dt>
                  <dd>第 {{ selectedFeature.version }} 版</dd>
                </div>
              </dl>

              <section class="health-panel" :class="healthStatus">
                <div class="health-copy">
                  <span class="health-dot" aria-hidden="true"></span>
                  <div>
                    <strong>健康状态：{{ healthLabel(healthStatus) }}</strong>
                    <p v-if="selectedFeature.health.message">
                      {{ selectedFeature.health.message }}
                    </p>
                    <p v-else-if="healthStatus === 'unknown'">
                      这项功能还没有接入可用的健康检查。
                    </p>
                    <p v-else>最近一次检查未附带说明。</p>
                  </div>
                </div>
                <div class="health-actions">
                  <span>
                    最后检查：{{ formatTime(selectedFeature.health.lastCheckedAt) }}
                    <template v-if="selectedFeature.health.latencyMs !== null">
                      · {{ selectedFeature.health.latencyMs }} ms
                    </template>
                  </span>
                  <button
                    type="button"
                    class="secondary compact"
                    :disabled="store.isCheckingHealth"
                    @click="store.checkHealth()"
                  >
                    {{ store.isCheckingHealth ? '正在检查…' : '检查健康' }}
                  </button>
                </div>
              </section>

              <section
                v-if="selectedFeature.metrics.length > 0"
                class="metrics-section"
              >
                <div class="section-heading">
                  <div>
                    <h3>运行指标</h3>
                    <p>只读数据，由功能服务上报。</p>
                  </div>
                </div>
                <div :class="metricGridClass(selectedFeature.metrics)">
                  <article
                    v-for="metric in selectedFeature.metrics"
                    :key="metric.key"
                    class="metric-card"
                  >
                    <span>{{ metric.label }}</span>
                    <strong>{{ metricValue(metric.value) }}</strong>
                    <small v-if="metric.unit">{{ metric.unit }}</small>
                  </article>
                </div>
              </section>

              <section class="config-section">
                <div class="section-heading">
                  <div>
                    <h3>功能配置</h3>
                    <p v-if="selectedFeature.status === 'planned'">
                      {{ plannedReason(selectedFeature) }}
                    </p>
                    <p v-else-if="selectedFeature.readOnlyReason">
                      {{ selectedFeature.readOnlyReason }}
                    </p>
                    <p v-else>保存后会立即更新该功能的运行参数。</p>
                  </div>
                  <span v-if="selectedFeature.status === 'planned'" class="planned-badge">
                    规划中
                  </span>
                  <span v-else-if="selectedFeature.readOnlyReason" class="readonly-badge">
                    只读
                  </span>
                  <span v-else-if="store.isDirty" class="dirty-badge">
                    有未保存的修改
                  </span>
                </div>

                <div
                  v-if="selectedFeature.configSchema.length === 0"
                  class="config-empty"
                >
                  这项功能没有可调整的配置。
                </div>
                <div v-else class="config-grid">
                  <FeatureCenterField
                    v-for="field in selectedFeature.configSchema"
                    :key="field.key"
                    :field="field"
                    :value="store.draftConfig[field.key] ?? null"
                    :secret-draft="store.secretDrafts[field.key] ?? ''"
                    :secret-configured="
                      store.selectedFeature?.secretConfigured[field.key] ?? false
                    "
                    :locked="selectedFeature.status === 'planned'"
                    @update-value="updateConfigValue(field.key, $event)"
                    @update-secret="store.setSecretDraft(field.key, $event)"
                  />
                </div>

                <div class="form-actions">
                  <button
                    type="button"
                    class="secondary"
                    :disabled="
                      !store.isDirty ||
                      store.isSaving ||
                      !store.canEdit ||
                      selectedFeature.status === 'planned'
                    "
                    @click="store.discardDraft"
                  >
                    放弃修改
                  </button>
                  <button
                    type="button"
                    class="primary"
                    :disabled="!store.canSave"
                    @click="store.save"
                  >
                    {{ store.isSaving ? '正在保存…' : '保存配置' }}
                  </button>
                </div>
              </section>
            </template>

            <div v-else class="detail-empty">
              <span class="state-mark" aria-hidden="true">☆</span>
              <strong>请选择一个功能模块</strong>
              <span>右侧会显示功能说明、健康状态、运行指标和配置项。</span>
            </div>
          </main>
        </div>
      </div>
    </Transition>
  </section>
</template>

<style scoped>
.feature-center-page {
  display: grid;
  gap: 18px;
}

.page-header,
.feature-workspace,
.feature-detail-panel,
.feature-list-panel,
.health-panel,
.metrics-section,
.config-section {
  border: 1px solid var(--sprout-outline);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  padding: 24px;
  border-radius: var(--sprout-radius-card);
}

.eyebrow {
  margin: 0 0 6px;
  color: var(--sprout-pink-strong);
  font-size: 13px;
  font-weight: 700;
}

h1,
h2,
h3,
p,
dl,
dd {
  overflow-wrap: anywhere;
}

h1 {
  margin: 0;
  color: var(--sprout-text);
  font-size: 28px;
}

.page-description {
  max-width: 760px;
  margin: 10px 0 0;
  color: var(--sprout-text-muted);
  line-height: 1.7;
}

button {
  min-height: 40px;
  padding: 0 16px;
  border: 1px solid #e9a5b8;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: #b23a68;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

button.primary {
  border-color: #d94f83;
  background: #d94f83;
  color: #ffffff;
  box-shadow: 0 8px 18px rgb(217 79 131 / 18%);
}

button:not(:disabled):hover {
  border-color: #d94f83;
  background: #fff7fa;
  color: #b23a68;
  box-shadow: 0 9px 20px rgb(217 79 131 / 12%);
}

button.primary:not(:disabled):hover {
  background: #b23a68;
  color: #ffffff;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.54;
}

button.compact {
  min-height: 34px;
  padding: 0 12px;
  border-radius: 12px;
  font-size: 12px;
}

.header-actions {
  display: flex;
  flex: 0 0 auto;
  gap: 10px;
}

.notice {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 0;
  padding: 13px 16px;
  border-radius: 16px;
  line-height: 1.55;
}

.notice.success {
  border: 1px solid #a9dfbc;
  background: #edf9f1;
  color: #1d6b3f;
}

.notice.conflict {
  border: 1px solid #f3d18e;
  background: #fffaf0;
  color: #6b4f2a;
}

.notice.error {
  border: 1px solid #f0bdcb;
  background: #fff0f2;
  color: #a12b4a;
}

.notice button {
  min-height: 32px;
  flex: 0 0 auto;
  padding: 0 10px;
  border-radius: 10px;
  font-size: 12px;
}

.feature-workspace {
  display: grid;
  min-height: 680px;
  grid-template-columns: 310px minmax(0, 1fr);
  overflow: hidden;
  border-radius: var(--sprout-radius-card);
}

.feature-list-panel {
  min-width: 0;
  border: 0;
  border-right: 1px solid var(--sprout-outline-soft);
  border-radius: 0;
  box-shadow: none;
}

.list-heading {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 20px 18px 13px;
}

.list-heading h2,
.list-heading p {
  margin: 0;
}

.list-heading h2 {
  color: var(--sprout-text);
  font-size: 17px;
}

.list-heading p {
  margin-top: 4px;
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.list-filters {
  display: grid;
  gap: 9px;
  padding: 0 14px 14px;
  border-bottom: 1px solid var(--sprout-outline-soft);
}

.list-filters input,
.list-filters select {
  width: 100%;
  min-height: 40px;
  padding: 0 12px;
  border: 1px solid var(--sprout-outline);
  border-radius: 13px;
  background: #fffbfc;
  color: var(--sprout-text);
  font: inherit;
}

.list-filters input:focus,
.list-filters select:focus {
  border-color: var(--sprout-pink-strong);
  background: #ffffff;
  box-shadow: 0 0 0 3px rgb(217 79 131 / 10%);
  outline: none;
}

.feature-groups {
  display: grid;
  max-height: calc(680px - 148px);
  gap: 8px;
  overflow: auto;
  padding: 13px 10px 18px;
}

.feature-group {
  display: grid;
  gap: 6px;
}

.group-title {
  margin: 7px 8px 2px;
  color: var(--sprout-text-muted);
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.06em;
}

.feature-group ul {
  display: grid;
  gap: 5px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.feature-group li button {
  display: flex;
  width: 100%;
  min-height: 62px;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 9px 10px;
  border-color: transparent;
  border-radius: 15px;
  text-align: left;
}

.feature-group li button:hover {
  border-color: var(--sprout-outline-soft);
  background: #fffafb;
  box-shadow: none;
}

.feature-group li button.active {
  border-color: #f0bdcb;
  background: #fff0f4;
  box-shadow: inset 0 0 0 1px rgb(217 79 131 / 5%);
}

.feature-button-main {
  display: grid;
  min-width: 0;
  gap: 4px;
}

.feature-button-main strong,
.feature-button-main small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.feature-button-main strong {
  color: var(--sprout-text);
  font-size: 14px;
}

.feature-button-main small {
  color: var(--sprout-text-muted);
  font-size: 11px;
  font-weight: 400;
}

.mini-status,
.feature-status {
  display: inline-flex;
  align-items: center;
  flex: 0 0 auto;
  border-radius: 999px;
  font-weight: 700;
  white-space: nowrap;
}

.mini-status {
  padding: 4px 7px;
  font-size: 10px;
}

.feature-status {
  padding: 6px 11px;
  font-size: 12px;
}

.mini-status.enabled,
.feature-status.enabled {
  background: #e7f8ee;
  color: #1d6b3f;
}

.mini-status.disabled,
.feature-status.disabled {
  background: #f5eef1;
  color: #7a6370;
}

.mini-status.maintenance,
.feature-status.maintenance {
  background: #fff3cd;
  color: #755600;
}

.mini-status.planned,
.feature-status.planned {
  background: #f3ecff;
  color: #6542a3;
}

.mini-status.unknown,
.feature-status.unknown {
  background: #eaf5ff;
  color: #27627f;
}

.list-empty,
.detail-empty {
  display: grid;
  place-items: center;
  gap: 8px;
  padding: 28px 20px;
  color: var(--sprout-text-muted);
  text-align: center;
}

.list-empty strong,
.detail-empty strong {
  color: var(--sprout-text);
}

.feature-detail-panel {
  min-width: 0;
  border: 0;
  border-radius: 0;
  box-shadow: none;
}

.detail-loading {
  display: flex;
  min-height: 360px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--sprout-text-muted);
}

.detail-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 25px 26px 20px;
  border-bottom: 1px solid var(--sprout-outline-soft);
}

.detail-title h2 {
  margin: 0;
  color: var(--sprout-text);
  font-size: 23px;
}

.detail-title p:last-child {
  max-width: 780px;
  margin: 9px 0 0;
  color: var(--sprout-text-muted);
  line-height: 1.65;
}

.feature-meta {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin: 0;
  padding: 17px 26px;
  border-bottom: 1px solid var(--sprout-outline-soft);
}

.feature-meta div {
  display: grid;
  gap: 5px;
  padding: 12px 14px;
  border-radius: 14px;
  background: #fff9fb;
}

.feature-meta dt {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.feature-meta dd {
  margin: 0;
  color: var(--sprout-text);
  font-size: 14px;
  font-weight: 700;
}

.health-panel {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin: 20px 26px 0;
  padding: 17px 18px;
  border-radius: 18px;
  box-shadow: none;
}

.health-panel.healthy {
  border-color: #a9dfbc;
  background: #f2fbf5;
}

.health-panel.degraded {
  border-color: #f3d18e;
  background: #fffaf0;
}

.health-panel.unhealthy {
  border-color: #f0bdcb;
  background: #fff4f6;
}

.health-panel.checking,
.health-panel.unknown {
  border-color: #b7def2;
  background: #f3faff;
}

.health-copy {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 11px;
}

.health-dot {
  width: 11px;
  height: 11px;
  flex: 0 0 auto;
  margin-top: 4px;
  border-radius: 50%;
  background: #27627f;
  box-shadow: 0 0 0 5px rgb(102 204 255 / 15%);
}

.healthy .health-dot {
  background: #1d6b3f;
  box-shadow: 0 0 0 5px rgb(29 107 63 / 12%);
}

.degraded .health-dot {
  background: #9a6a00;
  box-shadow: 0 0 0 5px rgb(154 106 0 / 12%);
}

.unhealthy .health-dot {
  background: #a12b4a;
  box-shadow: 0 0 0 5px rgb(161 43 74 / 12%);
}

.health-copy strong {
  color: var(--sprout-text);
}

.health-copy p {
  margin: 5px 0 0;
  color: var(--sprout-text-muted);
  font-size: 13px;
  line-height: 1.55;
}

.health-actions {
  display: grid;
  flex: 0 0 auto;
  justify-items: end;
  gap: 8px;
}

.health-actions > span {
  color: var(--sprout-text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.metrics-section,
.config-section {
  margin: 20px 26px 0;
  border: 0;
  box-shadow: none;
}

.metrics-section {
  padding-bottom: 2px;
}

.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 11px;
}

.section-heading h3,
.section-heading p {
  margin: 0;
}

.section-heading h3 {
  color: var(--sprout-text);
  font-size: 17px;
}

.section-heading p {
  margin-top: 4px;
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.dirty-badge {
  padding: 4px 9px;
  border-radius: 999px;
  background: #fff3cd;
  color: #755600;
  font-size: 11px;
  font-weight: 700;
}

.readonly-badge {
  padding: 4px 9px;
  border-radius: 999px;
  background: #eaf5ff;
  color: #27627f;
  font-size: 11px;
  font-weight: 700;
}

.planned-badge {
  flex: 0 0 auto;
  padding: 4px 9px;
  border-radius: 999px;
  background: #f3ecff;
  color: #6542a3;
  font-size: 11px;
  font-weight: 700;
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 11px;
}

.metric-grid-wide {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.metric-card {
  display: grid;
  gap: 5px;
  padding: 15px 16px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 16px;
  background: #fff9fb;
}

.metric-card span {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.metric-card strong {
  color: var(--sprout-text);
  font-size: 22px;
}

.metric-card small {
  color: var(--sprout-text-muted);
}

.config-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.config-empty {
  padding: 18px;
  border: 1px dashed var(--sprout-outline);
  border-radius: 16px;
  background: #fffbfc;
  color: var(--sprout-text-muted);
}

.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin: 18px 0 26px;
  padding-top: 17px;
  border-top: 1px solid var(--sprout-outline-soft);
}

.state-panel {
  display: flex;
  min-height: 300px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 30px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  color: var(--sprout-text-muted);
  text-align: center;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.state-panel-empty {
  display: grid;
  align-content: center;
  gap: 7px;
}

.state-panel-empty strong {
  color: var(--sprout-text);
}

.state-mark {
  display: grid;
  width: 38px;
  height: 38px;
  place-items: center;
  border-radius: 14px;
  background: #fff2f5;
  color: var(--sprout-pink-strong);
  font-size: 22px;
}

.state-spinner {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  border: 2px solid #f0bdcb;
  border-top-color: #d94f83;
  border-radius: 50%;
  animation: spin 700ms linear infinite;
}

.detail-empty {
  min-height: 430px;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1120px) {
  .feature-workspace {
    grid-template-columns: 270px minmax(0, 1fr);
  }

  .metric-grid-wide {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 860px) {
  .feature-workspace {
    grid-template-columns: 1fr;
  }

  .feature-list-panel {
    border-right: 0;
    border-bottom: 1px solid var(--sprout-outline-soft);
  }

  .feature-groups {
    max-height: 290px;
  }

  .config-grid,
  .metric-grid,
  .metric-grid-wide {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 620px) {
  .page-header,
  .detail-header,
  .health-panel {
    display: grid;
  }

  .header-actions {
    justify-content: flex-start;
  }

  .feature-meta {
    grid-template-columns: 1fr;
  }

  .health-actions {
    justify-items: start;
  }

  .health-actions > span {
    white-space: normal;
  }

  .feature-meta,
  .detail-header {
    padding-right: 18px;
    padding-left: 18px;
  }

  .health-panel,
  .metrics-section,
  .config-section {
    margin-right: 18px;
    margin-left: 18px;
  }
}
</style>
