<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useDisplayStatsStore } from '@/features/display/application/displayStatsStore'

const store = useDisplayStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasBuckets = computed(() => (store.stats?.screenSizeBuckets.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function bucketLabel(label: string): string {
  return (
    {
      small: '小屏（宽度 < 480 px）',
      medium: '中屏（480 - 1023 px）',
      large: '大屏（宽度 >= 1024 px）',
      unknown: '未知分辨率',
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
  <section class="display-page">
    <header class="display-header">
      <div>
        <h1>屏幕与护眼</h1>
        <p class="display-hint">
          仅统计设备屏幕数量与护眼策略覆盖情况，不展示屏幕画面、截图或单个设备的策略明细。
        </p>
      </div>
      <div class="display-actions">
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

    <p v-if="store.error" class="display-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="display-cards">
      <article class="display-card">
        <span class="display-card-label">屏幕设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="display-card">
        <span class="display-card-label">支持触摸</span>
        <strong>{{ store.stats.touchDeviceCount }}</strong>
      </article>
      <article class="display-card">
        <span class="display-card-label">策略待更新</span>
        <strong>{{ store.stats.outdatedCount }}</strong>
      </article>
      <article class="display-card">
        <span class="display-card-label">平均亮度</span>
        <strong>{{ store.stats.averageBrightness }}%</strong>
      </article>
    </div>

    <article v-if="store.stats" class="display-panel">
      <h2>屏幕尺寸分布</h2>
      <p v-if="!hasBuckets" class="display-empty">当前窗口没有在线屏幕设备。</p>
      <ul v-else>
        <li v-for="item in store.stats.screenSizeBuckets" :key="item.label">
          <span>{{ bucketLabel(item.label) }}</span>
          <strong>{{ item.count }}</strong>
        </li>
      </ul>
      <p class="display-meta">
        触摸占比 {{ store.touchRatio }}% ·
        <span v-if="store.stats.generatedAt">
          统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
        </span>
      </p>
    </article>

    <p v-else-if="store.isLoading" class="display-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.display-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.display-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.display-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.display-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.display-actions {
  display: flex;
  gap: 8px;
}

.display-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.display-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.display-error {
  color: #b42318;
}

.display-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.display-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.display-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.display-card strong {
  font-size: 26px;
}

.display-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.display-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.display-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.display-panel li {
  display: flex;
  justify-content: space-between;
}

.display-empty {
  color: var(--color-text-muted, #667085);
}

.display-meta {
  margin: 16px 0 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
