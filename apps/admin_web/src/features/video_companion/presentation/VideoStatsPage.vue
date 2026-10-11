<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useVideoStatsStore } from '@/features/video_companion/application/videoStatsStore'

const store = useVideoStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasStates = computed(() => (store.stats?.stateBuckets.length ?? 0) > 0)
const hasReasons = computed(() => (store.stats?.reasonBuckets.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function stateLabel(label: string): string {
  return (
    {
      ringing: '振铃中',
      active: '通话中',
      ended: '已结束',
      missed: '未接听',
      rejected: '已拒绝',
      failed: '失败',
    }[label] ?? label
  )
}

function reasonLabel(label: string): string {
  return (
    {
      completed: '正常挂断',
      timeout: '超时未接',
      declined: '对方拒绝',
      cancelled: '主叫取消',
      busy: '忙线',
      network_error: '网络异常',
      device_error: '设备异常',
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
  <section class="video-page">
    <header class="video-header">
      <div>
        <h1>视频陪伴</h1>
        <p class="video-hint">
          仅统计设备通话状态与结束原因，不展示音视频内容、联系人身份或单台设备的通话明细。
        </p>
      </div>
      <div class="video-actions">
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

    <p v-if="store.error" class="video-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="video-cards">
      <article class="video-card">
        <span class="video-card-label">通话总数</span>
        <strong>{{ store.stats.callCount }}</strong>
      </article>
      <article class="video-card">
        <span class="video-card-label">通话中</span>
        <strong>{{ store.stats.activeCount }}</strong>
      </article>
      <article class="video-card">
        <span class="video-card-label">已结束</span>
        <strong>{{ store.stats.endedCount }}</strong>
      </article>
      <article class="video-card">
        <span class="video-card-label">未接听</span>
        <strong>{{ store.stats.missedCount }}</strong>
      </article>
      <article class="video-card">
        <span class="video-card-label">已拒绝</span>
        <strong>{{ store.stats.rejectedCount }}</strong>
      </article>
      <article class="video-card">
        <span class="video-card-label">呼叫失败</span>
        <strong>{{ store.stats.failedCount }}</strong>
      </article>
      <article class="video-card">
        <span class="video-card-label">画质不佳</span>
        <strong>{{ store.stats.poorQualityCount }}</strong>
      </article>
    </div>

    <div v-if="store.stats" class="video-panels">
      <article class="video-panel">
        <h2>状态分布</h2>
        <p v-if="!hasStates" class="video-empty">当前窗口没有通话记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.stateBuckets" :key="item.label">
            <span>{{ stateLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>

      <article class="video-panel">
        <h2>结束原因分布</h2>
        <p v-if="!hasReasons" class="video-empty">当前窗口没有结束原因记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.reasonBuckets" :key="item.label">
            <span>{{ reasonLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
    </div>

    <p v-if="store.stats" class="video-meta">
      正常接通占比 {{ store.completedRatio }}% ·
      <span v-if="store.stats.generatedAt">
        统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
      </span>
    </p>

    <p v-else-if="store.isLoading" class="video-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.video-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.video-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.video-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.video-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.video-actions {
  display: flex;
  gap: 8px;
}

.video-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.video-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.video-error {
  color: #b42318;
}

.video-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.video-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.video-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.video-card strong {
  font-size: 26px;
}

.video-panels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 16px;
}

.video-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.video-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.video-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.video-panel li {
  display: flex;
  justify-content: space-between;
}

.video-empty {
  color: var(--color-text-muted, #667085);
}

.video-meta {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
