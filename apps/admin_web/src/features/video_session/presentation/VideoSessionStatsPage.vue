<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useVideoSessionStatsStore } from '@/features/video_session/application/videoSessionStatsStore'

const store = useVideoSessionStatsStore()

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
      idle: '空闲',
      ringing: '响铃中',
      active: '通话中',
      ended: '已结束',
      rejected: '已拒绝',
      failed: '失败',
    }[label] ?? label
  )
}

function reasonLabel(label: string): string {
  return (
    {
      none: '无',
      hangup: '主动挂断',
      rejected: '拒接',
      timeout: '超时未接',
      network: '网络中断',
      permission_revoked: '授权被撤回',
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
  <section class="video-session-page">
    <header class="video-session-header">
      <div>
        <h1>视频会话</h1>
        <p class="video-session-hint">
          仅统计设备视频会话的能力、授权与结束原因，不展示任何音频或视频画面。
        </p>
      </div>
      <div class="video-session-actions">
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

    <p v-if="store.error" class="video-session-error">{{ store.error.message }}</p>

    <div v-if="store.stats" class="video-session-cards">
      <article class="video-session-card">
        <span class="video-session-card-label">会话设备</span>
        <strong>{{ store.stats.deviceCount }}</strong>
      </article>
      <article class="video-session-card">
        <span class="video-session-card-label">支持会话</span>
        <strong>{{ store.stats.supportedCount }}</strong>
      </article>
      <article class="video-session-card">
        <span class="video-session-card-label">已允许采集</span>
        <strong>{{ store.stats.captureAllowedCount }}</strong>
      </article>
      <article class="video-session-card">
        <span class="video-session-card-label">策略落后</span>
        <strong>{{ store.stats.outdatedCount }}</strong>
      </article>
      <article class="video-session-card">
        <span class="video-session-card-label">弱网</span>
        <strong>{{ store.stats.poorQualityCount }}</strong>
      </article>
    </div>

    <div v-if="store.stats" class="video-session-panels">
      <article class="video-session-panel">
        <h2>通话状态</h2>
        <p v-if="!hasStates" class="video-session-empty">当前窗口没有会话记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.stateBuckets" :key="item.label">
            <span>{{ stateLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
      <article class="video-session-panel">
        <h2>结束原因</h2>
        <p v-if="!hasReasons" class="video-session-empty">当前窗口没有结束记录。</p>
        <ul v-else>
          <li v-for="item in store.stats.reasonBuckets" :key="item.label">
            <span>{{ reasonLabel(item.label) }}</span>
            <strong>{{ item.count }}</strong>
          </li>
        </ul>
      </article>
    </div>

    <p v-if="store.stats" class="video-session-meta">
      采集授权占比 {{ store.captureRatio }}% ·
      <span v-if="store.stats.generatedAt">
        统计时间 {{ generatedAtLabel(store.stats.generatedAt) }}
      </span>
    </p>

    <p v-else-if="store.isLoading" class="video-session-empty">正在加载统计数据。</p>
  </section>
</template>

<style scoped>
.video-session-page {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.video-session-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.video-session-header h1 {
  margin: 0 0 6px;
  font-size: 22px;
}

.video-session-hint {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.video-session-actions {
  display: flex;
  gap: 8px;
}

.video-session-actions button {
  border: 1px solid var(--color-border, #d0d5dd);
  background: transparent;
  border-radius: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: inherit;
}

.video-session-actions button.active {
  border-color: var(--color-primary, #1f6feb);
  color: var(--color-primary, #1f6feb);
  font-weight: 600;
}

.video-session-error {
  color: #b42318;
}

.video-session-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.video-session-card {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.video-session-card-label {
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}

.video-session-card strong {
  font-size: 26px;
}

.video-session-panels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 16px;
}

.video-session-panel {
  border: 1px solid var(--color-border, #eaecf0);
  border-radius: 8px;
  padding: 20px;
}

.video-session-panel h2 {
  margin: 0 0 12px;
  font-size: 16px;
}

.video-session-panel ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.video-session-panel li {
  display: flex;
  justify-content: space-between;
}

.video-session-empty {
  color: var(--color-text-muted, #667085);
}

.video-session-meta {
  margin: 0;
  color: var(--color-text-muted, #667085);
  font-size: 13px;
}
</style>
