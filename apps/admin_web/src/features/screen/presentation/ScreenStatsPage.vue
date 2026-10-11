<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useScreenStatsStore } from '@/features/screen/application/screenStatsStore'

const store = useScreenStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasStates = computed(() => (store.stats?.stateBuckets.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function stateLabel(label: string): string {
  return (
    {
      idle: '空闲',
      loading: '加载中',
      playing: '播放中',
      paused: '已暂停',
      ended: '已结束',
      failed: '播放失败',
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
  <section class="screen-page">
    <header class="screen-header">
      <div>
        <h1>屏幕</h1>
        <p class="screen-hint">
          仅统计设备屏幕、点读与视频播放状态，不展示表情画面、点读词条内容或任何屏幕截图。
        </p>
      </div>
      <div class="screen-actions">
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

    <p v-if="store.error" class="screen-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="screen-cards">
      <article class="screen-card">
        <span class="screen-card-label">屏幕设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="screen-card">
        <span class="screen-card-label">支持视频</span>
        <strong>{{ store.stats.videoDeviceCount }}</strong>
      </article>
      <article class="screen-card">
        <span class="screen-card-label">支持点读</span>
        <strong>{{ store.stats.pointReadDeviceCount }}</strong>
      </article>
      <article class="screen-card">
        <span class="screen-card-label">策略落后</span>
        <strong>{{ store.stats.outdatedCount }}</strong>
      </article>
    </div>

    <div v-if="store.stats" class="screen-panels">
      <article class="screen-panel">
        <h2>视频播放状态</h2>
        <p v-if="!hasStates" class="screen-empty">当前窗口没有视频播放记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.stateBuckets" :key="item.label">
            <span>{{ stateLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
    </div>

    <p v-if="store.stats" class="screen-meta">
      视频占比 {{ store.videoRatio }}% ·
      <span v-if="store.stats.generatedAt">
        统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
      </span>
    </p>

    <p v-else-if="store.isLoading" class="screen-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.screen-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.screen-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.screen-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.screen-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.screen-actions {
  display: flex;
  gap: 8px;
}

.screen-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.screen-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.screen-error {
  color: #b42318;
}

.screen-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.screen-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.screen-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.screen-card strong {
  font-size: 26px;
}

.screen-panels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 16px;
}

.screen-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.screen-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.screen-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.screen-panel li {
  display: flex;
  justify-content: space-between;
}

.screen-empty {
  color: var(--color-text-muted, #667085);
}

.screen-meta {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
