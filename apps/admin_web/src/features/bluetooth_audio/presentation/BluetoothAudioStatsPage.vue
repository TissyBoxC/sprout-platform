<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useBluetoothAudioStatsStore } from '@/features/bluetooth_audio/application/bluetoothAudioStatsStore'

const store = useBluetoothAudioStatsStore()

const windowOptions = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const hasStates = computed(() => (store.stats?.stateBuckets.length ?? 0) > 0)
const hasCodecs = computed(() => (store.stats?.codecBuckets.length ?? 0) > 0)

onMounted(() => {
  void store.load()
})

function stateLabel(label: string): string {
  return (
    {
      off: '已关闭',
      idle: '空闲',
      advertising: '广播中',
      connecting: '连接中',
      connected: '已连接',
      error: '异常',
    }[label] ?? label
  )
}

function codecLabel(label: string): string {
  return (
    {
      unknown: '未知',
      sbc: 'SBC',
      aac: 'AAC',
      ldac: 'LDAC',
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
  <section class="bluetooth-audio-page">
    <header class="bluetooth-audio-header">
      <div>
        <h1>蓝牙音箱</h1>
        <p class="bluetooth-audio-hint">
          仅统计设备蓝牙音频的能力、适配器状态与编解码分布，不展示配对密钥或任何音频。
        </p>
      </div>
      <div class="bluetooth-audio-actions">
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

    <p v-if="store.error" class="bluetooth-audio-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="bluetooth-audio-cards">
      <article class="bluetooth-audio-card">
        <span class="bluetooth-audio-card-label">上报设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="bluetooth-audio-card">
        <span class="bluetooth-audio-card-label">支持蓝牙</span>
        <strong>{{ store.stats.supportedCount }}</strong>
      </article>
      <article class="bluetooth-audio-card">
        <span class="bluetooth-audio-card-label">音箱模式</span>
        <strong>{{ store.stats.speakerModeCount }}</strong>
      </article>
      <article class="bluetooth-audio-card">
        <span class="bluetooth-audio-card-label">已连接</span>
        <strong>{{ store.stats.connectedCount }}</strong>
      </article>
      <article class="bluetooth-audio-card">
        <span class="bluetooth-audio-card-label">配对设备总数</span>
        <strong>{{ store.stats.pairedDeviceTotal }}</strong>
      </article>
      <article class="bluetooth-audio-card">
        <span class="bluetooth-audio-card-label">策略落后</span>
        <strong>{{ store.stats.outdatedCount }}</strong>
      </article>
    </div>

    <div v-if="store.stats" class="bluetooth-audio-panels">
      <article class="bluetooth-audio-panel">
        <h2>适配器状态</h2>
        <p v-if="!hasStates" class="bluetooth-audio-empty">当前窗口没有上报记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.stateBuckets" :key="item.label">
            <span>{{ stateLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
      <article class="bluetooth-audio-panel">
        <h2>活跃编解码</h2>
        <p v-if="!hasCodecs" class="bluetooth-audio-empty">当前窗口没有编解码记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.codecBuckets" :key="item.label">
            <span>{{ codecLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
    </div>

    <p v-if="store.stats" class="bluetooth-audio-meta">
      连接占比 {{ store.connectedRatio }}% ·
      <span v-if="store.stats.generatedAt">
        统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
      </span>
    </p>

    <p v-else-if="store.isLoading" class="bluetooth-audio-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.bluetooth-audio-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.bluetooth-audio-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.bluetooth-audio-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.bluetooth-audio-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.bluetooth-audio-actions {
  display: flex;
  gap: 8px;
}

.bluetooth-audio-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.bluetooth-audio-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.bluetooth-audio-error {
  color: #b42318;
}

.bluetooth-audio-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.bluetooth-audio-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.bluetooth-audio-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.bluetooth-audio-card strong {
  font-size: 26px;
}

.bluetooth-audio-panels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 16px;
}

.bluetooth-audio-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.bluetooth-audio-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.bluetooth-audio-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.bluetooth-audio-panel li {
  display: flex;
  justify-content: space-between;
}

.bluetooth-audio-empty {
  color: var(--color-text-muted, #667085);
}

.bluetooth-audio-meta {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
