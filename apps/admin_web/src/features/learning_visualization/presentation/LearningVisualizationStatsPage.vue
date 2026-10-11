<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useLearningVisualizationStatsStore } from '@/features/learning_visualization/application/learningVisualizationStatsStore'

const store = useLearningVisualizationStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasStates = computed(() => (store.stats?.stateBuckets.length ?? 0) > 0)
const hasStyles = computed(() => (store.stats?.styleBuckets.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function stateLabel(label: string): string {
  return (
    {
      idle: '空闲',
      lesson: '学习中',
      feedback: '反馈中',
      reward: '奖励中',
      paused: '已暂停',
      error: '异常',
    }[label] ?? label
  )
}

function styleLabel(label: string): string {
  return (
    {
      neutral: '中性',
      encouraging: '鼓励',
      playful: '活泼',
      celebration: '庆祝',
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
  <section class="learning-visualization-page">
    <header class="learning-visualization-header">
      <div>
        <h1>学习可视化</h1>
        <p class="learning-visualization-hint">
          仅统计设备学习可视化的能力、触屏状态与反馈风格分布，不展示任何画面或触摸轨迹。
        </p>
      </div>
      <div class="learning-visualization-actions">
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

    <p v-if="store.error" class="learning-visualization-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="learning-visualization-cards">
      <article class="learning-visualization-card">
        <span class="learning-visualization-card-label">上报设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="learning-visualization-card">
        <span class="learning-visualization-card-label">支持可视化</span>
        <strong>{{ store.stats.supportedCount }}</strong>
      </article>
      <article class="learning-visualization-card">
        <span class="learning-visualization-card-label">触屏设备</span>
        <strong>{{ store.stats.touchCount }}</strong>
      </article>
      <article class="learning-visualization-card">
        <span class="learning-visualization-card-label">课程联动</span>
        <strong>{{ store.stats.lessonLinkedCount }}</strong>
      </article>
      <article class="learning-visualization-card">
        <span class="learning-visualization-card-label">策略落后</span>
        <strong>{{ store.stats.outdatedCount }}</strong>
      </article>
      <article class="learning-visualization-card">
        <span class="learning-visualization-card-label">反馈次数</span>
        <strong>{{ store.stats.feedbackEvents }}</strong>
      </article>
    </div>

    <div v-if="store.stats" class="learning-visualization-panels">
      <article class="learning-visualization-panel">
        <h2>触屏状态</h2>
        <p v-if="!hasStates" class="learning-visualization-empty">当前窗口没有上报记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.stateBuckets" :key="item.label">
            <span>{{ stateLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
      <article class="learning-visualization-panel">
        <h2>反馈风格</h2>
        <p v-if="!hasStyles" class="learning-visualization-empty">当前窗口没有风格记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.styleBuckets" :key="item.label">
            <span>{{ styleLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
    </div>

    <p v-if="store.stats" class="learning-visualization-meta">
      学习中占比 {{ store.runningRatio }}% ·
      <span v-if="store.stats.generatedAt">
        统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
      </span>
    </p>

    <p v-else-if="store.isLoading" class="learning-visualization-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.learning-visualization-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.learning-visualization-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.learning-visualization-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.learning-visualization-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.learning-visualization-actions {
  display: flex;
  gap: 8px;
}

.learning-visualization-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.learning-visualization-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.learning-visualization-error {
  color: #b42318;
}

.learning-visualization-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.learning-visualization-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.learning-visualization-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.learning-visualization-card strong {
  font-size: 26px;
}

.learning-visualization-panels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 16px;
}

.learning-visualization-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.learning-visualization-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.learning-visualization-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.learning-visualization-panel li {
  display: flex;
  justify-content: space-between;
}

.learning-visualization-empty {
  color: var(--color-text-muted, #667085);
}

.learning-visualization-meta {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
