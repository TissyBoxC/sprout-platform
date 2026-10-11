<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useFormFactorStatsStore } from '@/features/form_factor/application/formFactorStatsStore'

const store = useFormFactorStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasNames = computed(() => (store.stats?.nameBuckets.length ?? 0) > 0)
const hasCapabilities = computed(() => (store.stats?.capabilityCounts.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function nameLabel(label: string): string {
  return (
    {
      desktop: '桌面形态',
      moving_body: '可移动形态',
      wearable: '可穿戴形态',
    }[label] ?? label
  )
}

function capabilityLabel(label: string): string {
  return (
    {
      display: '屏幕',
      camera: '摄像头',
      microphone: '麦克风',
      speaker: '扬声器',
      touch: '触摸',
      locomotion: '行走能力',
      servo: '舵机',
      cellular: '蜂窝网络',
      battery: '电池',
      wearable: '可穿戴',
      unknown: '未知能力',
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
  <section class="form-factor-page">
    <header class="form-factor-header">
      <div>
        <h1>形态能力</h1>
        <p class="form-factor-hint">
          仅统计设备上报的硬件能力与组合结果，形态使用技术名称，不绑定外观或角色。
        </p>
      </div>
      <div class="form-factor-actions">
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

    <p v-if="store.error" class="form-factor-error">
      {{ store.error.message }}
    </p>

    <div v-if="store.stats" class="form-factor-cards">
      <article class="form-factor-card">
        <span class="form-factor-card-label">上报形态的设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="form-factor-card">
        <span class="form-factor-card-label">可移动</span>
        <strong>{{ store.stats.locomotionCount }}</strong>
      </article>
      <article class="form-factor-card">
        <span class="form-factor-card-label">语音对话</span>
        <strong>{{ store.stats.voiceCount }}</strong>
      </article>
      <article class="form-factor-card">
        <span class="form-factor-card-label">视频通话</span>
        <strong>{{ store.stats.videoCallCount }}</strong>
      </article>
      <article class="form-factor-card">
        <span class="form-factor-card-label">可穿戴</span>
        <strong>{{ store.stats.wearableCount }}</strong>
      </article>
    </div>

    <div v-if="store.stats" class="form-factor-panels">
      <article class="form-factor-panel">
        <h2>形态分布</h2>
        <p v-if="!hasNames" class="form-factor-empty">当前窗口没有上报形态的设备。</p>
        <ul v-else>
          <li v-for="item in store.stats.nameBuckets" :key="item.label">
            <span>{{ nameLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>

      <article class="form-factor-panel">
        <h2>硬件能力分布</h2>
        <p v-if="!hasCapabilities" class="form-factor-empty">当前窗口没有上报硬件能力的设备。</p>
        <ul v-else>
          <li v-for="item in store.stats.capabilityCounts" :key="item.label">
            <span>{{ capabilityLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
    </div>

    <p v-if="store.stats" class="form-factor-meta">
      可移动占比 {{ store.locomotionRatio }}% ·
      <span v-if="store.stats.generatedAt">
        统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
      </span>
    </p>

    <p v-else-if="store.isLoading" class="form-factor-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.form-factor-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.form-factor-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.form-factor-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.form-factor-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.form-factor-actions {
  display: flex;
  gap: 8px;
}

.form-factor-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.form-factor-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.form-factor-error {
  color: #b42318;
}

.form-factor-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.form-factor-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.form-factor-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.form-factor-card strong {
  font-size: 26px;
}

.form-factor-panels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 16px;
}

.form-factor-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.form-factor-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.form-factor-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.form-factor-panel li {
  display: flex;
  justify-content: space-between;
}

.form-factor-empty {
  color: var(--color-text-muted, #667085);
}

.form-factor-meta {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
