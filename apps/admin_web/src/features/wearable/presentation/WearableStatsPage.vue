<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useWearableStatsStore } from '@/features/wearable/application/wearableStatsStore'

const store = useWearableStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasPrecisions = computed(() => (store.stats?.precisionBuckets.length ?? 0) > 0)
const hasStates = computed(() => (store.stats?.stateBuckets.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function precisionLabel(label: string): string {
  return (
    {
      coarse: '粗略定位',
      balanced: '平衡定位',
      precise: '精确定位',
    }[label] ?? label
  )
}

function stateLabel(label: string): string {
  return (
    {
      inside: '围栏内',
      outside: '越界',
      unknown: '未知',
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
  <section class="wearable-page">
    <header class="wearable-header">
      <div>
        <h1>穿戴与定位</h1>
        <p class="wearable-hint">
          仅统计设备定位权限、精度、围栏与 SOS
          状态，不展示具体坐标、联系人身份或单台设备的定位轨迹。
        </p>
      </div>
      <div class="wearable-actions">
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

    <p v-if="store.error" class="wearable-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="wearable-cards">
      <article class="wearable-card">
        <span class="wearable-card-label">定位设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="wearable-card">
        <span class="wearable-card-label">已开启定位</span>
        <strong>{{ store.stats.locationEnabledCount }}</strong>
      </article>
      <article class="wearable-card">
        <span class="wearable-card-label">已授权</span>
        <strong>{{ store.stats.permissionCount }}</strong>
      </article>
      <article class="wearable-card">
        <span class="wearable-card-label">SOS 触发</span>
        <strong>{{ store.stats.sosTriggeredCount }}</strong>
      </article>
      <article class="wearable-card">
        <span class="wearable-card-label">SOS 已解除</span>
        <strong>{{ store.stats.sosCancelledCount }}</strong>
      </article>
      <article class="wearable-card">
        <span class="wearable-card-label">越界设备</span>
        <strong>{{ store.stats.geofenceOutsideCount }}</strong>
      </article>
    </div>

    <div v-if="store.stats" class="wearable-panels">
      <article class="wearable-panel">
        <h2>定位精度分布</h2>
        <p v-if="!hasPrecisions" class="wearable-empty">当前窗口没有上报定位的设备。</p>
        <ul v-else>
          <li v-for="item in store.stats.precisionBuckets" :key="item.label">
            <span>{{ precisionLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>

      <article class="wearable-panel">
        <h2>围栏状态分布</h2>
        <p v-if="!hasStates" class="wearable-empty">当前窗口没有围栏状态记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.stateBuckets" :key="item.label">
            <span>{{ stateLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
    </div>

    <p v-if="store.stats" class="wearable-meta">
      越界占比 {{ store.geofenceOutsideRatio }}% ·
      <span v-if="store.stats.generatedAt">
        统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
      </span>
    </p>

    <p v-else-if="store.isLoading" class="wearable-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.wearable-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.wearable-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.wearable-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.wearable-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.wearable-actions {
  display: flex;
  gap: 8px;
}

.wearable-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.wearable-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.wearable-error {
  color: #b42318;
}

.wearable-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.wearable-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.wearable-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.wearable-card strong {
  font-size: 26px;
}

.wearable-panels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 16px;
}

.wearable-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.wearable-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.wearable-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.wearable-panel li {
  display: flex;
  justify-content: space-between;
}

.wearable-empty {
  color: var(--color-text-muted, #667085);
}

.wearable-meta {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
