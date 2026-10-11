<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useImagePrivacyStatsStore } from '@/features/image_privacy/application/imagePrivacyStatsStore'

const store = useImagePrivacyStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasModes = computed(() => (store.stats?.modeBuckets.length ?? 0) > 0)
const hasReasons = computed(() => (store.stats?.reasonBuckets.length ?? 0) > 0)
const hasReports = computed(() => (store.stats?.deviceCount ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function modeLabel(label: string): string {
  return (
    {
      off: '已关闭',
      metadata: '仅处理元数据',
      local_full: '本机完整处理',
    }[label] ?? label
  )
}

function reasonLabel(label: string): string {
  return (
    {
      none: '无拦截',
      disabled: '功能已关闭',
      consent: '缺少监护人同意',
      scope: '超出上传范围',
      quota: '达到配额上限',
      error: '处理异常',
    }[label] ?? label
  )
}

function generatedAtLabel(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString('zh-CN')
}
</script>

<template>
  <section class="image-privacy-page">
    <header class="image-privacy-header">
      <div>
        <h1>图片隐私</h1>
        <p class="image-privacy-hint">
          仅统计设备相机处理策略与上传拦截情况，不展示图片、缩略图或人脸模板。
        </p>
      </div>
      <div class="image-privacy-actions">
        <div class="image-privacy-windows" aria-label="统计时间范围">
          <button
            v-for="option in windowOptions"
            :key="option.value"
            type="button"
            :class="{ active: store.windowDays === option.value }"
            :disabled="store.isLoading"
            @click="store.load(option.value)"
          >
            {{ option.label }}
          </button>
        </div>
        <button
          class="image-privacy-refresh"
          type="button"
          :disabled="store.isLoading"
          @click="store.load()"
        >
          {{ store.isLoading ? '刷新中…' : '刷新' }}
        </button>
      </div>
    </header>

    <p v-if="store.error" class="image-privacy-error" role="alert">
      {{ store.error.message }}
    </p>

    <div
      v-if="store.isLoading && !store.stats"
      class="image-privacy-state"
      aria-live="polite"
    >
      正在加载图片隐私统计。
    </div>

    <div
      v-else-if="store.stats && !hasReports"
      class="image-privacy-state"
      aria-live="polite"
    >
      <strong>当前窗口没有设备上报记录</strong>
      <span>可以切换统计时间范围，或稍后刷新重试。</span>
    </div>

    <template v-else-if="store.stats">
      <div v-if="store.isLoading" class="image-privacy-refreshing" aria-live="polite">
        正在刷新统计，当前展示上一次结果。
      </div>

      <div class="image-privacy-cards">
        <article class="image-privacy-card">
          <span class="image-privacy-card-label">上报设备</span>
          <strong>{{ store.stats.deviceCount }}</strong>
        </article>
        <article class="image-privacy-card">
          <span class="image-privacy-card-label">支持相机</span>
          <strong>{{ store.stats.cameraCount }}</strong>
        </article>
        <article class="image-privacy-card">
          <span class="image-privacy-card-label">仅本地处理</span>
          <strong>{{ store.stats.localOnlyCount }}</strong>
        </article>
        <article class="image-privacy-card">
          <span class="image-privacy-card-label">允许上传</span>
          <strong>{{ store.stats.uploadAllowedCount }}</strong>
        </article>
        <article class="image-privacy-card">
          <span class="image-privacy-card-label">策略落后</span>
          <strong>{{ store.stats.outdatedCount }}</strong>
        </article>
        <article class="image-privacy-card">
          <span class="image-privacy-card-label">已处理</span>
          <strong>{{ store.stats.processedTotal }}</strong>
        </article>
        <article class="image-privacy-card">
          <span class="image-privacy-card-label">已上传</span>
          <strong>{{ store.stats.uploadedTotal }}</strong>
        </article>
        <article class="image-privacy-card">
          <span class="image-privacy-card-label">已拦截</span>
          <strong>{{ store.stats.blockedTotal }}</strong>
        </article>
      </div>

      <div class="image-privacy-panels">
        <article class="image-privacy-panel">
          <h2>处理模式</h2>
          <p v-if="!hasModes" class="image-privacy-empty">当前窗口没有处理模式记录。</p>
          <ul v-else>
            <li v-for="item in store.stats.modeBuckets" :key="item.label">
              <span>{{ modeLabel(item.label) }}</span>
              <strong>{{ item.count }}</strong>
            </li>
          </ul>
        </article>
        <article class="image-privacy-panel">
          <h2>上传拦截原因</h2>
          <p v-if="!hasReasons" class="image-privacy-empty">当前窗口没有拦截记录。</p>
          <ul v-else>
            <li v-for="item in store.stats.reasonBuckets" :key="item.label">
              <span>{{ reasonLabel(item.label) }}</span>
              <strong>{{ item.count }}</strong>
            </li>
          </ul>
        </article>
      </div>

      <p class="image-privacy-meta">
        上传占比 {{ store.uploadRatio }}% · 拦截占比 {{ store.blockedRatio }}%
        <span v-if="store.stats.generatedAt">
          · 统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
        </span>
      </p>
    </template>
  </section>
</template>

<style scoped>
.image-privacy-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
  color: var(--sprout-text, #4a2e3b);
}

.image-privacy-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  flex-wrap: wrap;
}

.image-privacy-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.image-privacy-hint {
  max-width: 680px;
  margin: 0;
  color: var(--sprout-text-muted, #6b4f5a);
  font-size: 13px;
  line-height: 1.6;
}

.image-privacy-actions,
.image-privacy-windows {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.image-privacy-actions button {
  min-height: 34px;
  border: 1px solid var(--sprout-outline, #f0bdcb);
  border-radius: var(--sprout-radius-control, 18px);
  background: rgb(255 255 255 / 88%);
  padding: 6px 13px;
  color: var(--sprout-text-muted, #6b4f5a);
  font: inherit;
  cursor: pointer;
  transition:
    border-color var(--sprout-duration-fast, 150ms) ease,
    background-color var(--sprout-duration-fast, 150ms) ease,
    color var(--sprout-duration-fast, 150ms) ease,
    box-shadow var(--sprout-duration-base, 220ms) ease;
}

.image-privacy-actions button:hover:not(:disabled) {
  border-color: var(--sprout-pink, #f7a8bf);
  background: #fff7fa;
  color: var(--sprout-pink-deep, #b23a68);
}

.image-privacy-actions button.active {
  border-color: var(--sprout-pink-strong, #d94f83);
  background: #fff0f4;
  color: #c94175;
  font-weight: 700;
}

.image-privacy-actions button:disabled {
  cursor: wait;
  opacity: 0.65;
}

.image-privacy-refresh {
  box-shadow: 0 8px 18px rgb(217 79 131 / 8%);
}

.image-privacy-error {
  margin: 0;
  border: 1px solid #f3b7c8;
  border-radius: var(--sprout-radius-control, 18px);
  background: #fff7f9;
  padding: 12px 14px;
  color: #a52b57;
}

.image-privacy-state {
  display: grid;
  gap: 6px;
  min-height: 180px;
  place-content: center;
  border: 1px dashed var(--sprout-outline, #f0bdcb);
  border-radius: var(--sprout-radius-card, 22px);
  background: rgb(255 255 255 / 70%);
  color: var(--sprout-text-muted, #6b4f5a);
  text-align: center;
}

.image-privacy-state strong {
  color: var(--sprout-text, #4a2e3b);
  font-size: 16px;
}

.image-privacy-refreshing {
  border-radius: var(--sprout-radius-control, 18px);
  background: #fff2f5;
  padding: 10px 14px;
  color: #a52b57;
  font-size: 13px;
}

.image-privacy-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
}

.image-privacy-card {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 8px;
  border: 1px solid var(--sprout-outline-soft, #f7d9e2);
  border-radius: var(--sprout-radius-card, 22px);
  background: rgb(255 255 255 / 88%);
  padding: 16px;
  box-shadow: var(--sprout-shadow-soft, 0 14px 34px rgb(194 91 128 / 10%));
}

.image-privacy-card-label {
  overflow-wrap: anywhere;
  color: var(--sprout-text-muted, #6b4f5a);
  font-size: 13px;
}

.image-privacy-card strong {
  overflow-wrap: anywhere;
  font-size: 26px;
}

.image-privacy-panels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 16px;
}

.image-privacy-panel {
  min-width: 0;
  border: 1px solid var(--sprout-outline, #f0bdcb);
  border-radius: var(--sprout-radius-card, 22px);
  background: rgb(255 255 255 / 88%);
  padding: 20px;
}

.image-privacy-panel h2 {
  margin: 0 0 14px;
  font-size: 16px;
}

.image-privacy-panel ul {
  display: flex;
  flex-direction: column;
  gap: 11px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.image-privacy-panel li {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 14px;
}

.image-privacy-panel li span {
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--sprout-text-muted, #6b4f5a);
}

.image-privacy-empty {
  margin: 0;
  color: var(--sprout-text-muted, #6b4f5a);
}

.image-privacy-meta {
  margin: 0;
  color: var(--sprout-text-muted, #6b4f5a);
  font-size: 13px;
}

@media (max-width: 640px) {
  .image-privacy-actions,
  .image-privacy-windows {
    width: 100%;
  }

  .image-privacy-windows {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .image-privacy-windows button {
    min-width: 0;
    padding-inline: 6px;
  }
}
</style>
