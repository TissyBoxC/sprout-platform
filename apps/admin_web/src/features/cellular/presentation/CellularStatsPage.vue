<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useCellularStatsStore } from '@/features/cellular/application/cellularStatsStore'

const store = useCellularStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasBearers = computed(() => (store.stats?.bearerBuckets.length ?? 0) > 0)
const hasEsim = computed(() => (store.stats?.esimBuckets.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function bearerLabel(label: string): string {
  return (
    {
      wifi: 'Wi-Fi',
      cellular: '蜂窝网络',
      none: '未连接',
    }[label] ?? label
  )
}

function esimLabel(label: string): string {
  return (
    {
      absent: '未安装',
      installed: '已安装',
      active: '已激活',
      disabled: '已停用',
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
  <section class="cellular-page">
    <header class="cellular-header">
      <div>
        <h1>蜂窝网络</h1>
        <p class="cellular-hint">
          仅统计设备承载、eSIM 状态与流量用量，不展示号码、通话明细或单台设备的流量明细。
        </p>
      </div>
      <div class="cellular-actions">
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
    </header>

    <p v-if="store.error" class="cellular-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="cellular-cards">
      <article class="cellular-card">
        <span class="cellular-card-label">活跃设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="cellular-card">
        <span class="cellular-card-label">蜂窝在线</span>
        <strong>{{ store.stats.cellularCount }}</strong>
      </article>
      <article class="cellular-card">
        <span class="cellular-card-label">漫游中</span>
        <strong>{{ store.stats.roamingCount }}</strong>
      </article>
      <article class="cellular-card">
        <span class="cellular-card-label">流量已用尽</span>
        <strong>{{ store.stats.quotaExhaustedCount }}</strong>
      </article>
      <article class="cellular-card">
        <span class="cellular-card-label">平均信号</span>
        <strong>{{ store.stats.averageSignal }}%</strong>
      </article>
    </div>

    <article v-if="store.stats" class="cellular-panel">
      <h2>承载分布</h2>
      <p v-if="!hasBearers" class="cellular-empty">当前窗口没有连接设备。</p>
      <ul v-else>
        <li v-for="item in store.stats.bearerBuckets" :key="item.label">
          <span>{{ bearerLabel(item.label) }}</span>
          <strong>{{ item.count }}</strong>
        </li>
      </ul>
    </article>

    <article v-if="store.stats" class="cellular-panel">
      <h2>eSIM 状态分布</h2>
      <p v-if="!hasEsim" class="cellular-empty">当前窗口没有蜂窝设备。</p>
      <ul v-else>
        <li v-for="item in store.stats.esimBuckets" :key="item.label">
          <span>{{ esimLabel(item.label) }}</span>
          <strong>{{ item.count }}</strong>
        </li>
      </ul>
      <p class="cellular-meta">
        蜂窝占比 {{ store.cellularRatio }}% ·
        <span v-if="store.stats.generatedAt">
          统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
        </span>
      </p>
    </article>

    <p v-else-if="store.isLoading" class="cellular-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.cellular-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.cellular-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.cellular-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.cellular-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.cellular-actions {
  display: flex;
  gap: 8px;
}

.cellular-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.cellular-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.cellular-error {
  color: #b42318;
}

.cellular-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.cellular-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.cellular-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.cellular-card strong {
  font-size: 26px;
}

.cellular-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.cellular-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.cellular-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.cellular-panel li {
  display: flex;
  justify-content: space-between;
}

.cellular-empty {
  color: var(--color-text-muted, #667085);
}

.cellular-meta {
  margin: 16px 0 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
