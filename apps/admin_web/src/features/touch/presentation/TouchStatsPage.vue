<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useTouchStatsStore } from '@/features/touch/application/touchStatsStore'

const store = useTouchStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasPresets = computed(() => (store.stats?.presetCoverage.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function presetLabel(preset: string): string {
  return (
    {
      tap: '点击',
      double_tap: '双击',
      long_press: '长按',
      swipe: '滑动',
    }[preset] ?? preset
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
  <section class="touch-page">
    <header class="touch-header">
      <div>
        <h1>触摸交互</h1>
        <p class="touch-hint">
          仅统计设备触摸能力与手势分布，不展示触摸坐标、原始采样或单个设备的配置明细。
        </p>
      </div>
      <div class="touch-actions">
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

    <p v-if="store.error" class="touch-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="touch-cards">
      <article class="touch-card">
        <span class="touch-card-label">活跃设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="touch-card">
        <span class="touch-card-label">支持触摸</span>
        <strong>{{ store.stats.touchDeviceCount }}</strong>
      </article>
      <article class="touch-card">
        <span class="touch-card-label">支持触觉反馈</span>
        <strong>{{ store.stats.hapticDeviceCount }}</strong>
      </article>
      <article class="touch-card">
        <span class="touch-card-label">配置待更新</span>
        <strong>{{ store.stats.outdatedCount }}</strong>
      </article>
      <article class="touch-card">
        <span class="touch-card-label">平均灵敏度</span>
        <strong>{{ store.stats.averageSensitivity }}%</strong>
      </article>
    </div>

    <article v-if="store.stats" class="touch-panel">
      <h2>手势配置分布</h2>
      <p v-if="!hasPresets" class="touch-empty">当前窗口没有触摸设备。</p>
      <ul v-else>
        <li v-for="item in store.stats.presetCoverage" :key="item.preset">
          <span>{{ presetLabel(item.preset) }}</span>
          <strong>{{ item.count }}</strong>
        </li>
      </ul>
      <p class="touch-meta">
        触摸占比 {{ store.touchRatio }}% ·
        <span v-if="store.stats.generatedAt">
          统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
        </span>
      </p>
    </article>

    <p v-else-if="store.isLoading" class="touch-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.touch-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.touch-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.touch-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.touch-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.touch-actions {
  display: flex;
  gap: 8px;
}

.touch-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.touch-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.touch-error {
  color: #b42318;
}

.touch-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.touch-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.touch-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.touch-card strong {
  font-size: 26px;
}

.touch-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.touch-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.touch-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.touch-panel li {
  display: flex;
  justify-content: space-between;
}

.touch-empty {
  color: var(--color-text-muted, #667085);
}

.touch-meta {
  margin: 16px 0 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
