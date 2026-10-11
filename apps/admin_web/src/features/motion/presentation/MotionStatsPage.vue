<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useMotionStatsStore } from '@/features/motion/application/motionStatsStore'

const store = useMotionStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasActions = computed(() => (store.stats?.actionBuckets.length ?? 0) > 0)
const hasStates = computed(() => (store.stats?.stateBuckets.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function actionLabel(label: string): string {
  return (
    {
      idle: '静止',
      walk: '行走',
      dance: '舞蹈',
      tail_wag: '尾巴动作',
      gesture: '手势',
    }[label] ?? label
  )
}

function stateLabel(label: string): string {
  return (
    {
      idle: '空闲',
      running: '运行中',
      stopped: '已停止',
      timeout: '超时停止',
      fault: '故障',
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
  <section class="motion-page">
    <header class="motion-header">
      <div>
        <h1>动作与电机</h1>
        <p class="motion-hint">
          仅统计设备动作状态与执行结果，不展示原始电机采样或单台设备的运动轨迹。
        </p>
      </div>
      <div class="motion-actions">
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

    <p v-if="store.error" class="motion-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="motion-cards">
      <article class="motion-card">
        <span class="motion-card-label">有动作能力的设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="motion-card">
        <span class="motion-card-label">运行中</span>
        <strong>{{ store.stats.runningCount }}</strong>
      </article>
      <article class="motion-card">
        <span class="motion-card-label">空闲</span>
        <strong>{{ store.stats.idleCount }}</strong>
      </article>
      <article class="motion-card">
        <span class="motion-card-label">故障</span>
        <strong>{{ store.stats.faultedCount }}</strong>
      </article>
    </div>

    <div v-if="store.stats" class="motion-panels">
      <article class="motion-panel">
        <h2>动作分布</h2>
        <p v-if="!hasActions" class="motion-empty">当前窗口没有上报动作的设备。</p>
        <ul v-else>
          <li v-for="item in store.stats.actionBuckets" :key="item.label">
            <span>{{ actionLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>

      <article class="motion-panel">
        <h2>状态分布</h2>
        <p v-if="!hasStates" class="motion-empty">当前窗口没有上报状态的设备。</p>
        <ul v-else>
          <li v-for="item in store.stats.stateBuckets" :key="item.label">
            <span>{{ stateLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
    </div>

    <p v-if="store.stats" class="motion-meta">
      故障占比 {{ store.faultedRatio }}% ·
      <span v-if="store.stats.generatedAt">
        统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
      </span>
    </p>

    <p v-else-if="store.isLoading" class="motion-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.motion-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.motion-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.motion-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.motion-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.motion-actions {
  display: flex;
  gap: 8px;
}

.motion-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.motion-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.motion-error {
  color: #b42318;
}

.motion-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.motion-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.motion-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.motion-card strong {
  font-size: 26px;
}

.motion-panels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 16px;
}

.motion-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.motion-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.motion-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.motion-panel li {
  display: flex;
  justify-content: space-between;
}

.motion-empty {
  color: var(--color-text-muted, #667085);
}

.motion-meta {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
