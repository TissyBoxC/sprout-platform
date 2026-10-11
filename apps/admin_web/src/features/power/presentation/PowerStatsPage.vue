<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { usePowerStatsStore } from '@/features/power/application/powerStatsStore'

const store = usePowerStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasBuckets = computed(() => (store.stats?.percentageBuckets.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function bucketLabel(label: string): string {
  return (
    {
      high: '电量充足（>= 80%）',
      good: '良好（50% - 79%）',
      medium: '一般（20% - 49%）',
      low: '偏低（< 20%）',
      unknown: '未知电量',
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
  <section class="power-page">
    <header class="power-header">
      <div>
        <h1>电池与省电</h1>
        <p class="power-hint">仅统计设备电量与电源模式，不展示原始电量采样或单台设备的功耗明细。</p>
      </div>
      <div class="power-actions">
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

    <p v-if="store.error" class="power-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="power-cards">
      <article class="power-card">
        <span class="power-card-label">活跃设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="power-card">
        <span class="power-card-label">充电中</span>
        <strong>{{ store.stats.chargingCount }}</strong>
      </article>
      <article class="power-card">
        <span class="power-card-label">低电量</span>
        <strong>{{ store.stats.lowBatteryCount }}</strong>
      </article>
      <article class="power-card">
        <span class="power-card-label">低功耗</span>
        <strong>{{ store.stats.lowPowerCount }}</strong>
      </article>
      <article class="power-card">
        <span class="power-card-label">深度休眠</span>
        <strong>{{ store.stats.deepSleepCount }}</strong>
      </article>
      <article class="power-card">
        <span class="power-card-label">平均电量</span>
        <strong>{{ store.stats.averagePercent }}%</strong>
      </article>
    </div>

    <article v-if="store.stats" class="power-panel">
      <h2>电量分布</h2>
      <p v-if="!hasBuckets" class="power-empty">当前窗口没有上报电量的设备。</p>
      <ul v-else>
        <li v-for="item in store.stats.percentageBuckets" :key="item.label">
          <span>{{ bucketLabel(item.label) }}</span>
          <strong>{{ item.count }}</strong>
        </li>
      </ul>
      <p class="power-meta">
        低电量占比 {{ store.lowBatteryRatio }}% ·
        <span v-if="store.stats.generatedAt">
          统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
        </span>
      </p>
    </article>

    <p v-else-if="store.isLoading" class="power-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.power-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.power-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.power-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.power-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.power-actions {
  display: flex;
  gap: 8px;
}

.power-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.power-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.power-error {
  color: #b42318;
}

.power-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.power-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.power-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.power-card strong {
  font-size: 26px;
}

.power-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.power-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.power-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.power-panel li {
  display: flex;
  justify-content: space-between;
}

.power-empty {
  color: var(--color-text-muted, #667085);
}

.power-meta {
  margin: 16px 0 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
