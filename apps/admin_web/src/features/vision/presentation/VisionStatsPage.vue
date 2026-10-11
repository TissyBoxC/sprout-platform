<script setup lang="ts">
import { computed, onMounted } from 'vue'

import type { VisionTaskType } from '@/api/adminVision'
import { useVisionStatsStore } from '@/features/vision/application/visionStatsStore'

const store = useVisionStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasTaskTypes = computed(() => (store.stats?.taskTypes.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function taskTypeLabel(taskType: VisionTaskType): string {
  return (
    {
      object_recognition: '识物',
      picture_book_recognition: '绘本识别',
      photo_qa: '拍照问答',
      capture: '拍照',
    }[taskType] ?? '其他'
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
  <section class="vision-page">
    <header class="vision-header">
      <div>
        <h1>摄像头与视觉</h1>
        <p class="vision-hint">仅统计任务数量与结果数量，不展示照片、识别标签或摘要内容。</p>
      </div>
      <div class="vision-actions">
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

    <p v-if="store.error" class="vision-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="vision-cards">
      <article class="vision-card">
        <span class="vision-card-label">任务总数</span>
        <strong>{{ store.stats.taskCount }}</strong>
      </article>
      <article class="vision-card">
        <span class="vision-card-label">已完成</span>
        <strong>{{ store.stats.completedCount }}</strong>
      </article>
      <article class="vision-card">
        <span class="vision-card-label">未完成</span>
        <strong>{{ store.stats.failedCount }}</strong>
      </article>
      <article class="vision-card">
        <span class="vision-card-label">超时</span>
        <strong>{{ store.stats.expiredCount }}</strong>
      </article>
      <article class="vision-card">
        <span class="vision-card-label">结果数量</span>
        <strong>{{ store.stats.resultCount }}</strong>
      </article>
      <article class="vision-card">
        <span class="vision-card-label">活跃设备</span>
        <strong>{{ store.stats.activeDeviceCount }}</strong>
      </article>
    </div>

    <article v-if="store.stats" class="vision-panel">
      <h2>任务类型分布</h2>
      <p v-if="!hasTaskTypes" class="vision-empty">当前窗口没有任务。</p>
      <ul v-else>
        <li v-for="item in store.stats.taskTypes" :key="item.taskType">
          <span>{{ taskTypeLabel(item.taskType) }}</span>
          <strong>{{ item.count }}</strong>
        </li>
      </ul>
      <p class="vision-meta">
        未完成占比 {{ store.failureRate }}% ·
        <span v-if="store.stats.generatedAt">
          统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
        </span>
      </p>
    </article>

    <p v-else-if="store.isLoading" class="vision-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.vision-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.vision-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.vision-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.vision-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.vision-actions {
  display: flex;
  gap: 8px;
}

.vision-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.vision-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.vision-error {
  color: #b42318;
}

.vision-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.vision-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.vision-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.vision-card strong {
  font-size: 26px;
}

.vision-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.vision-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.vision-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.vision-panel li {
  display: flex;
  justify-content: space-between;
}

.vision-empty {
  color: var(--color-text-muted, #667085);
}

.vision-meta {
  margin: 16px 0 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
