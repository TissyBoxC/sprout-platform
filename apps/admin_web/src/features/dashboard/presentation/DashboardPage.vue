<script setup lang="ts">
import { computed, onMounted } from 'vue'

import MetricCard from '@/components/MetricCard.vue'
import { useDashboardStore } from '@/features/dashboard/application/dashboardStore'

const store = useDashboardStore()
const overview = computed(() => store.overview)

onMounted(() => {
  void store.load()
})

function formatCount(value: number): string {
  return new Intl.NumberFormat('zh-CN').format(value)
}

function formatQuota(value: number): string {
  return value.toFixed(2)
}

function formatTime(value: string): string {
  if (!value) {
    return '时间待补充'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '时间待补充'
  }
  return date.toLocaleString('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function releaseKindLabel(value: string): string {
  return (
    {
      resource: '资源更新',
      client: '客户端更新',
      firmware: '设备固件',
    }[value] ?? '版本更新'
  )
}

function releaseStatusLabel(value: string): string {
  return (
    {
      draft: '待发布',
      published: '已发布',
      withdrawn: '已撤回',
    }[value] ?? '待确认'
  )
}
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">运营概览</p>
        <h1>平台状态</h1>
        <p class="page-description">集中查看家庭、设备和 AI 服务的关键运行状态。</p>
      </div>
      <button type="button" :disabled="store.isLoading" @click="store.load">刷新数据</button>
    </header>

    <Transition name="toast">
      <p v-if="store.error" class="error-message">{{ store.error.message }}</p>
    </Transition>

    <Transition name="page" mode="out-in">
      <div
        :key="
          store.isLoading && overview === null
            ? 'loading'
            : overview === null && store.error
              ? 'error'
              : overview === null
                ? 'empty'
                : 'content'
        "
      >
        <div v-if="store.isLoading && overview === null" class="state-panel">
          <span class="state-spinner" aria-hidden="true"></span>
          正在汇总平台数据…
        </div>
        <div v-else-if="overview === null && store.error" class="state-panel state-error">
          <p>{{ store.error.message }}</p>
          <button type="button" class="primary" @click="store.load">重新加载</button>
        </div>
        <div v-else-if="overview === null" class="state-panel">
          <span class="state-icon" aria-hidden="true">☆</span>
          <span>暂时没有可显示的数据，请稍后重新加载。</span>
        </div>
        <template v-else>
          <div class="metric-grid">
          <MetricCard
            label="家长账号"
            :value="formatCount(overview.parentAccountCount)"
            :detail="`${formatCount(overview.activeDeviceCount)} 台设备已绑定`"
          />
          <MetricCard
            label="在线设备"
            :value="formatCount(overview.onlineDeviceCount)"
            :detail="`共 ${formatCount(overview.activeDeviceCount)} 台可用设备`"
          />
          <MetricCard
            label="今日 AI 对话"
            :value="formatCount(overview.todayConversationCount)"
            :detail="`今日消耗 ${formatQuota(overview.todaySpentUsd)}`"
          />
          <MetricCard
            label="剩余总额度"
            :value="formatQuota(overview.totalBalanceUsd)"
            :detail="`${formatCount(overview.pendingReleaseCount)} 个更新待发布`"
          />
          </div>

          <section class="release-panel">
          <div class="panel-heading">
            <div>
              <h2>最近更新</h2>
              <p>这里会显示最近登记和发布的版本。</p>
            </div>
            <RouterLink to="/releases">查看全部版本</RouterLink>
          </div>
          <div v-if="overview.recentReleases.length === 0" class="release-empty">
            还没有更新记录。发布新版本后，会显示在这里。
          </div>
          <ul v-else class="release-list">
            <li
              v-for="release in overview.recentReleases"
              :key="`${release.version}-${release.kind}-${release.platform}`"
            >
              <span class="release-version">{{ release.version }}</span>
              <span>{{ releaseKindLabel(release.kind) }}</span>
              <span :class="['release-status', release.status]">
                {{ releaseStatusLabel(release.status) }}
              </span>
              <time>{{ formatTime(release.publishedAt) }}</time>
            </li>
          </ul>
          </section>
        </template>
      </div>
    </Transition>
  </section>
</template>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 24px;
  padding: 24px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 8px 24px rgb(194 91 128 / 5%);
}

.eyebrow {
  margin: 0 0 6px;
  color: var(--sprout-pink-strong);
  font-size: 13px;
  font-weight: 700;
}

h1 {
  margin: 0;
  color: var(--sprout-text);
  font-size: 28px;
}

.page-description {
  max-width: 680px;
  margin: 8px 0 0;
  color: var(--sprout-text-muted);
}

.page-header button {
  min-height: 38px;
  flex: 0 0 auto;
  padding: 0 16px;
  border: 1px solid #e9a5b8;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: #c94175;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

.page-header button:not(:disabled):hover {
  border-color: #d94f83;
  background: #fff7fa;
  box-shadow: 0 8px 18px rgb(217 79 131 / 10%);
}

.page-header button:disabled {
  cursor: wait;
  opacity: 0.55;
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}

.metric-grid > :nth-child(2) {
  animation-delay: 55ms;
}

.metric-grid > :nth-child(3) {
  animation-delay: 110ms;
}

.metric-grid > :nth-child(4) {
  animation-delay: 165ms;
}

.state-panel {
  display: flex;
  min-height: 168px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  color: var(--sprout-text-muted);
  text-align: center;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.state-error {
  flex-direction: column;
}

.state-error p {
  margin: 0;
}

.state-error button {
  min-height: 38px;
  padding: 0 16px;
  border: 1px solid #d94f83;
  border-radius: var(--sprout-radius-control);
  background: #d94f83;
  color: #ffffff;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

.state-spinner {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  border: 2px solid #f0bdcb;
  border-top-color: #d94f83;
  border-radius: 50%;
  animation: spin 700ms linear infinite;
}

.state-icon {
  display: grid;
  width: 34px;
  height: 34px;
  place-items: center;
  border-radius: 12px;
  background: #fff2f5;
  color: #d94f83;
  font-size: 20px;
}

.release-panel {
  margin-top: 22px;
  overflow: hidden;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.panel-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 20px 22px;
  border-bottom: 1px solid #f7d9e2;
}

.panel-heading h2,
.panel-heading p {
  margin: 0;
}

.panel-heading h2 {
  color: var(--sprout-text);
  font-size: 18px;
}

.panel-heading p {
  margin-top: 5px;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.panel-heading a {
  color: #c94175;
  font-weight: 700;
  text-decoration: none;
}

.panel-heading a:hover {
  color: #a12b4a;
}

.release-empty {
  padding: 24px 22px;
  color: var(--sprout-text-muted);
}

.release-list {
  display: grid;
  gap: 0;
  margin: 0;
  padding: 0;
  list-style: none;
}

.release-list li {
  display: grid;
  grid-template-columns: minmax(90px, 1fr) minmax(110px, 1fr) auto auto;
  align-items: center;
  gap: 16px;
  padding: 14px 22px;
  border-bottom: 1px solid #f7d9e2;
}

.release-list li:last-child {
  border-bottom: 0;
}

.release-version {
  color: var(--sprout-text);
  font-weight: 700;
}

.release-list time {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.release-status {
  justify-self: start;
  padding: 4px 10px;
  border-radius: 999px;
  background: #fff3cd;
  color: #755600;
  font-size: 13px;
}

.release-status.published {
  background: #e7f8ee;
  color: #1d6b3f;
}

.release-status.withdrawn {
  background: #fff0f2;
  color: #a12b4a;
}

.error-message {
  margin: 0 0 14px;
  color: #b3261e;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1080px) {
  .metric-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 760px) {
  .page-header {
    display: grid;
  }

  .release-list li {
    grid-template-columns: 1fr auto;
  }

  .release-list time {
    grid-column: 1 / -1;
  }
}

@media (max-width: 560px) {
  .metric-grid {
    grid-template-columns: 1fr;
  }
}
</style>
