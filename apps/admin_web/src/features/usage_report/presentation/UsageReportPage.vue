<script setup lang="ts">
import { computed, onMounted } from 'vue'

import type { UsageReportDay } from '@/api/adminUsageReports'
import {
  useUsageReportStore,
  type UsageReportRange,
} from '@/features/usage_report/application/usageReportStore'

const store = useUsageReportStore()

const rangeOptions: Array<{ value: UsageReportRange; label: string }> = [
  { value: 7, label: '最近 7 天' },
  { value: 30, label: '最近 30 天' },
  { value: 90, label: '最近 90 天' },
]

const sortedReports = computed(() =>
  [...store.reports].sort((left, right) => right.reportDate.localeCompare(left.reportDate)),
)
const hasBlockedEvents = computed(() => store.totalBlockedCount > 0)
const hasCategories = computed(() => store.categoryTotals.length > 0)
const hasDevices = computed(() => store.deviceTotals.length > 0)

onMounted(() => {
  void store.loadFamilies()
})

async function selectFamily(parentAccountId: string): Promise<void> {
  await store.selectFamily(parentAccountId)
}

async function selectRange(days: UsageReportRange): Promise<void> {
  await store.setDays(days)
}

function categoryLabel(value: string): string {
  return (
    {
      story: '故事',
      nursery_rhyme: '儿歌',
      poetry: '古诗',
      english: '英语启蒙',
      encyclopedia: '科普百科',
      bedtime: '睡前内容',
      music: '音乐',
      nature: '自然',
      science: '科学',
    }[value] ?? '其他内容'
  )
}

function reportDateLabel(value: string): string {
  const date = new Date(`${value}T00:00:00`)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleDateString('zh-CN', {
    month: 'long',
    day: 'numeric',
    weekday: 'short',
  })
}

function allowedText(report: UsageReportDay): string {
  if (report.dailyLimitMinutes === 0) {
    return '不限使用时长'
  }
  return `每日 ${report.dailyLimitMinutes} 分钟`
}

function limitText(report: UsageReportDay): string {
  if (report.limitReached) {
    return '今日额度已用完'
  }
  if (report.dailyLimitMinutes === 0) {
    return '未设置每日上限'
  }
  return `剩余 ${report.remainingMinutes} 分钟`
}

function blockedTotal(report: UsageReportDay): number {
  return (
    report.blocked.disabledPeriod +
    report.blocked.dailyLimit +
    report.blocked.categoryDenied +
    report.blocked.timeUntrusted
  )
}
</script>

<template>
  <section class="usage-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">家长守护</p>
        <h1>使用报告</h1>
        <p class="page-description">
          查看家庭最近的使用时长、对话次数、内容播放和守护规则拦截情况。此页面只读，不会修改家长数据。
        </p>
      </div>
      <button
        type="button"
        class="refresh-button"
        :disabled="store.isLoadingFamilies || store.isLoadingReports"
        @click="store.refresh"
      >
        {{ store.isLoadingFamilies || store.isLoadingReports ? '正在刷新…' : '刷新报告' }}
      </button>
    </header>

    <Transition name="toast">
      <p v-if="store.error && store.families.length > 0" class="error-message" role="alert">
        {{ store.error.message }}
      </p>
    </Transition>

    <div
      v-if="store.isLoadingFamilies && store.families.length === 0"
      class="state-panel"
    >
      <span class="state-spinner" aria-hidden="true"></span>
      正在读取家长账号…
    </div>

    <div v-else-if="store.error" class="state-panel error-state">
      <span class="state-icon" aria-hidden="true">!</span>
      <div>
        <strong>{{ store.error.message }}</strong>
        <p v-if="store.error.kind === 'insufficient_permission'">
          请联系品牌管理员开通使用报告查看权限。
        </p>
      </div>
      <button type="button" class="text-button" @click="store.loadFamilies">重新加载</button>
    </div>

    <div v-else-if="store.families.length === 0" class="state-panel empty-state">
      <span class="state-icon" aria-hidden="true">☆</span>
      <div>
        <strong>还没有家长账号</strong>
        <p>创建家长账号并连接设备后，使用情况会显示在这里。</p>
      </div>
    </div>

    <div v-else class="report-layout">
      <aside class="family-panel">
        <div class="panel-heading">
          <div>
            <h2>家长账号</h2>
            <p>共 {{ store.families.length }} 个账号</p>
          </div>
        </div>
        <div class="family-list" role="list">
          <button
            v-for="family in store.families"
            :key="family.parentAccountId"
            type="button"
            :class="[
              'family-option',
              { selected: family.parentAccountId === store.selectedParentAccountId },
            ]"
            :aria-pressed="family.parentAccountId === store.selectedParentAccountId"
            @click="selectFamily(family.parentAccountId)"
          >
            <span class="family-avatar" aria-hidden="true">家</span>
            <span class="family-copy">
              <strong>{{ family.displayName }}</strong>
              <small>{{ family.phone }}</small>
            </span>
          </button>
        </div>
      </aside>

      <main class="report-content">
        <div class="content-heading">
          <div>
            <p class="eyebrow">当前家长</p>
            <h2>{{ store.selectedFamily?.displayName || '未选择家长' }}</h2>
            <p>{{ store.selectedFamily?.phone || '请选择家长账号' }}</p>
          </div>
          <div class="range-picker" aria-label="报告范围">
            <button
              v-for="option in rangeOptions"
              :key="option.value"
              type="button"
              :class="{ selected: store.selectedDays === option.value }"
              :aria-pressed="store.selectedDays === option.value"
              @click="selectRange(option.value)"
            >
              {{ option.label }}
            </button>
          </div>
        </div>

        <div v-if="store.isLoadingReports" class="state-panel compact-state">
          <span class="state-spinner" aria-hidden="true"></span>
          正在读取使用报告…
        </div>

        <div v-else-if="store.reportError" class="state-panel compact-state error-state">
          <span class="state-icon" aria-hidden="true">!</span>
          <div>
            <strong>{{ store.reportError.message }}</strong>
            <p>使用报告暂时无法读取，可以重新尝试。</p>
          </div>
          <button type="button" class="text-button" @click="store.loadReports">重新加载</button>
        </div>

        <template v-else>
          <section class="metric-grid" aria-label="使用概览">
            <article>
              <span>累计使用</span>
              <strong>{{ store.totalActiveMinutes }}</strong>
              <small>分钟</small>
            </article>
            <article>
              <span>对话次数</span>
              <strong>{{ store.totalConversationCount }}</strong>
              <small>次</small>
            </article>
            <article>
              <span>内容播放</span>
              <strong>{{ store.totalContentPlayCount }}</strong>
              <small>次</small>
            </article>
            <article :class="{ attention: hasBlockedEvents }">
              <span>守护规则拦截</span>
              <strong>{{ store.totalBlockedCount }}</strong>
              <small>次</small>
            </article>
          </section>

          <div v-if="!store.hasUsage" class="state-panel compact-state empty-state">
            <span class="state-icon" aria-hidden="true">☆</span>
            <div>
              <strong>这段时间还没有使用记录</strong>
              <p>设备连接并开始使用后，使用时长和内容记录会显示在这里。</p>
            </div>
          </div>

          <template v-else>
            <section class="report-panel">
              <div class="panel-heading">
                <div>
                  <h2>每日使用</h2>
                  <p>按设备所在时区统计，额度以家长设置的每日上限为准。</p>
                </div>
              </div>
              <div class="table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>日期</th>
                      <th>使用时长</th>
                      <th>对话</th>
                      <th>内容播放</th>
                      <th>额度状态</th>
                      <th>拦截</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="report in sortedReports" :key="report.reportDate">
                      <td>
                        <strong>{{ reportDateLabel(report.reportDate) }}</strong>
                        <small>{{ report.reportDate }}</small>
                      </td>
                      <td>
                        <strong>{{ report.activeMinutes }}</strong> 分钟
                      </td>
                      <td>{{ report.conversationCount }} 次</td>
                      <td>{{ report.contentPlayCount }} 次</td>
                      <td>
                        <span
                          :class="['status-pill', report.limitReached ? 'reached' : 'available']"
                        >
                          {{ limitText(report) }}
                        </span>
                        <small>{{ allowedText(report) }}</small>
                      </td>
                      <td>
                        <span v-if="blockedTotal(report) > 0">
                          {{ blockedTotal(report) }} 次
                        </span>
                        <span v-else>无</span>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </section>

            <section class="detail-grid">
              <article class="detail-panel">
                <div class="panel-heading">
                  <div>
                    <h2>内容分类</h2>
                    <p>这段时间播放最多的内容类型。</p>
                  </div>
                </div>
                <div v-if="!hasCategories" class="list-empty">
                  这段时间还没有内容播放记录。
                </div>
                <ul v-else class="breakdown-list">
                  <li v-for="item in store.categoryTotals" :key="item.category">
                    <span>{{ categoryLabel(item.category) }}</span>
                    <strong>{{ item.minutes }} 分钟 · {{ item.playCount }} 次</strong>
                  </li>
                </ul>
              </article>

              <article class="detail-panel">
                <div class="panel-heading">
                  <div>
                    <h2>守护规则拦截</h2>
                    <p>记录设备因守护规则没有开始的内容或对话。</p>
                  </div>
                </div>
                <div v-if="!hasBlockedEvents" class="list-empty">
                  这段时间没有触发守护规则拦截。
                </div>
                <ul v-else class="breakdown-list">
                  <li v-if="store.blockedTotals.disabledPeriod > 0">
                    <span>免打扰时段</span>
                    <strong>{{ store.blockedTotals.disabledPeriod }} 次</strong>
                  </li>
                  <li v-if="store.blockedTotals.dailyLimit > 0">
                    <span>每日额度用完</span>
                    <strong>{{ store.blockedTotals.dailyLimit }} 次</strong>
                  </li>
                  <li v-if="store.blockedTotals.categoryDenied > 0">
                    <span>内容分类限制</span>
                    <strong>{{ store.blockedTotals.categoryDenied }} 次</strong>
                  </li>
                  <li v-if="store.blockedTotals.timeUntrusted > 0">
                    <span>设备时间未同步</span>
                    <strong>{{ store.blockedTotals.timeUntrusted }} 次</strong>
                  </li>
                </ul>
              </article>
            </section>

            <section class="report-panel">
              <div class="panel-heading">
                <div>
                  <h2>设备分布</h2>
                  <p>按设备汇总这段时间的使用情况。</p>
                </div>
              </div>
              <div v-if="!hasDevices" class="list-empty">
                这段时间还没有设备使用记录。
              </div>
              <ul v-else class="device-breakdown">
                <li v-for="device in store.deviceTotals" :key="device.deviceId">
                  <div>
                    <strong>{{ device.deviceName || '未命名设备' }}</strong>
                    <small>设备编号 {{ device.deviceId }}</small>
                  </div>
                  <span>{{ device.activeMinutes }} 分钟</span>
                  <span>{{ device.conversationCount }} 次对话</span>
                  <span>{{ device.contentPlayCount }} 次播放</span>
                </li>
              </ul>
            </section>
          </template>
        </template>
      </main>
    </div>
  </section>
</template>

<style scoped>
.usage-page {
  display: grid;
  gap: 20px;
}

.page-header,
.family-panel,
.report-content,
.metric-grid article,
.report-panel,
.detail-panel,
.state-panel {
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  padding: 24px;
}

.eyebrow {
  margin: 0 0 6px;
  color: var(--sprout-pink-strong);
  font-size: 13px;
  font-weight: 700;
}

h1,
h2 {
  margin: 0;
  color: var(--sprout-text);
}

h1 {
  font-size: 28px;
}

h2 {
  font-size: 20px;
}

.page-description,
.panel-heading p,
.content-heading p,
.state-panel p {
  margin: 8px 0 0;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

.refresh-button,
.range-picker button {
  min-height: 38px;
  padding: 0 15px;
  border: 1px solid #e9a5b8;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: #c94175;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
  transition:
    transform var(--sprout-duration-fast) var(--sprout-ease-out),
    border-color var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease,
    box-shadow var(--sprout-duration-base) ease;
}

.refresh-button:hover,
.range-picker button:hover {
  border-color: #d94f83;
  background: #fff7fa;
  box-shadow: 0 8px 18px rgb(217 79 131 / 10%);
}

.refresh-button:disabled {
  cursor: wait;
  opacity: 0.55;
}

.refresh-button:not(:disabled):active,
.range-picker button:active {
  transform: scale(0.98);
}

.text-button {
  min-height: auto;
  padding: 2px 0;
  border: 0;
  background: transparent;
  box-shadow: none;
  color: #c94175;
}

.text-button:hover {
  border: 0;
  background: transparent;
  box-shadow: none;
}

.report-layout {
  display: grid;
  grid-template-columns: 286px minmax(0, 1fr);
  gap: 20px;
  align-items: start;
}

.family-panel {
  display: grid;
  gap: 14px;
  padding: 18px;
}

.panel-heading,
.content-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.panel-heading {
  padding: 18px 20px;
  border-bottom: 1px solid #f7d9e2;
}

.family-panel .panel-heading {
  padding: 2px 2px 14px;
  border-bottom: 0;
}

.panel-heading h2,
.panel-heading p {
  margin: 0;
}

.panel-heading p {
  margin-top: 4px;
  font-size: 13px;
}

.family-list {
  display: grid;
  gap: 8px;
  max-height: min(620px, calc(100vh - 220px));
  overflow: auto;
}

.family-option {
  display: flex;
  align-items: center;
  gap: 11px;
  width: 100%;
  padding: 11px;
  border: 1px solid transparent;
  border-radius: 16px;
  background: #fffbfc;
  color: var(--sprout-text);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition:
    transform var(--sprout-duration-fast) var(--sprout-ease-out),
    border-color var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease,
    box-shadow var(--sprout-duration-base) ease;
}

.family-option:hover {
  border-color: #f0bdcb;
  background: #fff7fa;
  transform: translateX(2px);
}

.family-option.selected {
  border-color: #f7a8bf;
  background: #fff0f4;
  box-shadow: inset 0 0 0 1px rgb(240 189 203 / 55%);
}

.family-avatar {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 14px;
  background: #fff0f4;
  color: #c94175;
  font-weight: 800;
}

.family-copy {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.family-copy strong,
.family-copy small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.family-copy small {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.report-content {
  display: grid;
  min-width: 0;
  gap: 16px;
  padding: 22px;
}

.content-heading {
  gap: 18px;
  padding-bottom: 16px;
  border-bottom: 1px solid #f7d9e2;
}

.range-picker {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

.range-picker button.selected {
  border-color: #d94f83;
  background: #d94f83;
  color: #ffffff;
  box-shadow: 0 8px 18px rgb(217 79 131 / 18%);
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
}

.metric-grid article {
  display: grid;
  gap: 3px;
  padding: 17px 18px;
}

.metric-grid article.attention {
  border-color: #efb4c5;
  background: #fff7fa;
}

.metric-grid span,
.metric-grid small {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.metric-grid strong {
  color: var(--sprout-text);
  font-size: 26px;
}

.report-panel {
  overflow: hidden;
}

.table-scroll {
  overflow-x: auto;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  padding: 14px 16px;
  border-bottom: 1px solid #f7d9e2;
  text-align: left;
  vertical-align: top;
}

th {
  color: var(--sprout-text-muted);
  font-size: 13px;
  white-space: nowrap;
}

td {
  color: var(--sprout-text);
}

tbody tr {
  transition: background-color var(--sprout-duration-fast) ease;
}

tbody tr:hover {
  background: #fff8fa;
}

tbody tr:last-child td {
  border-bottom: 0;
}

td small {
  display: block;
  margin-top: 4px;
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.status-pill {
  display: inline-flex;
  padding: 4px 9px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
}

.status-pill.available {
  background: #e7f8ee;
  color: #1d6b3f;
}

.status-pill.reached {
  background: #fff0f2;
  color: #a12b4a;
}

.detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.detail-panel {
  min-width: 0;
  overflow: hidden;
}

.breakdown-list,
.device-breakdown {
  display: grid;
  gap: 0;
  margin: 0;
  padding: 0;
  list-style: none;
}

.breakdown-list li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 13px 18px;
  border-bottom: 1px solid #f7d9e2;
  color: var(--sprout-text-muted);
}

.breakdown-list li:last-child {
  border-bottom: 0;
}

.breakdown-list strong {
  color: var(--sprout-text);
}

.device-breakdown li {
  display: grid;
  grid-template-columns: minmax(0, 1.6fr) auto auto auto;
  align-items: center;
  gap: 14px;
  padding: 14px 18px;
  border-bottom: 1px solid #f7d9e2;
}

.device-breakdown li:last-child {
  border-bottom: 0;
}

.device-breakdown div {
  display: grid;
  min-width: 0;
  gap: 4px;
}

.device-breakdown strong {
  overflow-wrap: anywhere;
}

.device-breakdown small,
.device-breakdown span {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.list-empty {
  padding: 22px 20px;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

.state-panel {
  display: flex;
  min-height: 132px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 28px;
  color: var(--sprout-text-muted);
  text-align: center;
}

.compact-state {
  min-height: 220px;
}

.state-panel > div {
  text-align: left;
}

.state-panel strong {
  color: var(--sprout-text);
}

.error-state {
  color: #a12b4a;
}

.error-state strong {
  color: #a12b4a;
}

.empty-state {
  flex-wrap: wrap;
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
  flex: 0 0 auto;
  place-items: center;
  border-radius: 12px;
  background: #fff2f5;
  color: #d94f83;
  font-size: 20px;
}

.error-message {
  margin: 0;
  color: #a12b4a;
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

  .detail-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 900px) {
  .report-layout {
    grid-template-columns: 1fr;
  }

  .family-list {
    display: flex;
    max-height: none;
    overflow-x: auto;
    padding-bottom: 2px;
  }

  .family-option {
    min-width: 210px;
  }

  table {
    min-width: 860px;
  }
}

@media (max-width: 720px) {
  .page-header,
  .content-heading,
  .panel-heading {
    align-items: stretch;
    flex-direction: column;
  }

  .refresh-button {
    width: 100%;
  }

  .range-picker {
    justify-content: flex-start;
  }

  .device-breakdown li {
    grid-template-columns: 1fr auto;
  }

  .state-panel {
    align-items: flex-start;
    flex-direction: column;
  }
}

@media (max-width: 520px) {
  .metric-grid {
    grid-template-columns: 1fr;
  }

  .range-picker button {
    flex: 1;
  }
}
</style>
