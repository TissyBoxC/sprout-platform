<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useMultiDeviceStatsStore } from '@/features/multi_device/application/multiDeviceStatsStore'

const store = useMultiDeviceStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasScopes = computed(() => (store.stats?.scopeBuckets.length ?? 0) > 0)
const hasStates = computed(() => (store.stats?.stateBuckets.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function scopeLabel(label: string): string {
  return (
    {
      device: '单设备联动',
      group: '组内联动',
      family: '全家联动',
    }[label] ?? label
  )
}

function stateLabel(label: string): string {
  return (
    {
      pending: '待同步',
      applied: '已同步',
      conflict: '有冲突',
      failed: '同步失败',
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
  <section class="mesh-page">
    <header class="mesh-header">
      <div>
        <h1>多设备</h1>
        <p class="mesh-hint">
          仅统计设备分组、家庭联动消息与内容同步状态，不展示分组明细、消息正文或单台设备的同步内容。
        </p>
      </div>
      <div class="mesh-actions">
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

    <p v-if="store.error" class="mesh-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="mesh-cards">
      <article class="mesh-card">
        <span class="mesh-card-label">设备组</span>
        <strong>{{ store.stats.groupCount }}</strong>
      </article>
      <article class="mesh-card">
        <span class="mesh-card-label">联动设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="mesh-card">
        <span class="mesh-card-label">联动消息</span>
        <strong>{{ store.stats.messageCount }}</strong>
      </article>
      <article class="mesh-card">
        <span class="mesh-card-label">已同步内容</span>
        <strong>{{ store.stats.syncedContentCount }}</strong>
      </article>
      <article class="mesh-card">
        <span class="mesh-card-label">同步冲突</span>
        <strong>{{ store.stats.conflictCount }}</strong>
      </article>
    </div>

    <div v-if="store.stats" class="mesh-panels">
      <article class="mesh-panel">
        <h2>联动范围分布</h2>
        <p v-if="!hasScopes" class="mesh-empty">当前窗口没有联动记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.scopeBuckets" :key="item.label">
            <span>{{ scopeLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>

      <article class="mesh-panel">
        <h2>内容同步状态</h2>
        <p v-if="!hasStates" class="mesh-empty">当前窗口没有内容同步记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.stateBuckets" :key="item.label">
            <span>{{ stateLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
    </div>

    <p v-if="store.stats" class="mesh-meta">
      冲突占比 {{ store.conflictRatio }}% ·
      <span v-if="store.stats.generatedAt">
        统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
      </span>
    </p>

    <p v-else-if="store.isLoading" class="mesh-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.mesh-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.mesh-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.mesh-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.mesh-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.mesh-actions {
  display: flex;
  gap: 8px;
}

.mesh-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.mesh-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.mesh-error {
  color: #b42318;
}

.mesh-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.mesh-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.mesh-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.mesh-card strong {
  font-size: 26px;
}

.mesh-panels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 16px;
}

.mesh-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.mesh-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.mesh-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.mesh-panel li {
  display: flex;
  justify-content: space-between;
}

.mesh-empty {
  color: var(--color-text-muted, #667085);
}

.mesh-meta {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
