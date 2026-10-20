<script setup lang="ts">
import { computed, onMounted, reactive } from 'vue'

import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS } from '@/features/auth/domain/adminPermission'
import { useBillingReconciliationStore } from '@/features/billing/application/billingReconciliationStore'
import {
  formatCents,
  formatSignedCents,
  paymentChannelLabel,
  reconciliationStatusLabel,
} from '@/features/billing/presentation/billingFormat'
import {
  billingErrorMessage,
  readonlyNoticeFor,
} from '@/features/billing/presentation/billingShared'
import '@/features/billing/presentation/billingStyles.css'

type RunFilter = '' | 'running' | 'completed' | 'failed'

const store = useBillingReconciliationStore()
const authStore = useAuthStore()
const form = reactive({
  channel: 'wechat',
  businessDate: '',
  formError: '',
})
const runFilter = reactive<{ status: RunFilter }>({ status: '' })

const canManageBilling = computed(() => authStore.can(ADMIN_PERMISSIONS.billingManage))
const readonlyNotice = computed(() =>
  readonlyNoticeFor('历史对账记录', '发起新的对账'),
)
// 渠道清单与后端注册的渠道一致；未接入真实渠道时对账会明确失败，
// 而不是得到一个虚假的「全部匹配」。
const channelOptions = [
  { value: 'wechat', label: '微信支付' },
  { value: 'alipay', label: '支付宝' },
]
const filteredRuns = computed(() =>
  runFilter.status === ''
    ? store.runs
    : store.runs.filter((run) => run.status === runFilter.status),
)
const runningRunCount = computed(
  () => store.runs.filter((run) => run.status === 'running').length,
)
const latestRun = computed(() => store.runs[0] ?? null)

onMounted(() => {
  void store.load()
})

async function startRun(): Promise<void> {
  form.formError = ''
  if (!canManageBilling.value) {
    return
  }
  if (form.businessDate.trim() === '') {
    form.formError = '请选择要核对的营业日。'
    return
  }
  const started = await store.start({
    channel: form.channel,
    businessDate: form.businessDate,
  })
  if (started) {
    await store.load()
  }
}

function reloadRuns(): void {
  void store.load()
}

function formatBusinessDate(value: string): string {
  if (!value) {
    return '日期未记录'
  }
  const parsed = /^(\d{4})-(\d{2})-(\d{2})/.exec(value)
  if (parsed === null) {
    return value
  }
  return `${parsed[1]} 年 ${Number(parsed[2])} 月 ${Number(parsed[3])} 日`
}

function formatDateTime(value: string): string {
  if (!value) {
    return '尚未完成'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '尚未完成'
  }
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function runBadgeClass(status: string): string {
  if (status === 'completed') {
    return 'billing-badge billing-badge-positive'
  }
  if (status === 'running') {
    return 'billing-badge billing-badge-pending'
  }
  return 'billing-badge billing-badge-negative'
}

function differenceBadgeClass(difference: number): string {
  return difference === 0
    ? 'billing-badge billing-badge-positive'
    : 'billing-badge billing-badge-negative'
}
</script>

<template>
  <section class="billing-page">
    <header class="billing-page-header">
      <div>
        <p class="billing-eyebrow">计费与订单</p>
        <h1>对账</h1>
        <p class="billing-description">
          按营业日核对渠道账单与平台入账。差异不为零时，先确认渠道是否已结算，再联系支付渠道处理。
        </p>
      </div>
      <div class="billing-actions">
        <button type="button" :disabled="store.isLoading" @click="reloadRuns">
          {{ store.isLoading ? '正在加载…' : '重新加载' }}
        </button>
      </div>
    </header>

    <p v-if="!canManageBilling" class="billing-readonly">{{ readonlyNotice }}</p>

    <form
      v-if="canManageBilling"
      class="billing-card billing-start-form"
      aria-label="发起对账"
      @submit.prevent="startRun"
    >
      <div>
        <h2 class="billing-section-title">发起对账</h2>
        <p class="billing-hint">选择一个渠道和营业日。同一天同一渠道只能发起一次。</p>
      </div>
      <div class="billing-start-fields">
        <label class="billing-field">
          <span>支付渠道</span>
          <select v-model="form.channel" :disabled="store.isStarting">
            <option v-for="option in channelOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </option>
          </select>
        </label>
        <label class="billing-field">
          <span>营业日</span>
          <input v-model="form.businessDate" type="date" :disabled="store.isStarting" />
        </label>
        <button type="submit" class="billing-primary" :disabled="store.isStarting">
          {{ store.isStarting ? '正在发起…' : '开始对账' }}
        </button>
      </div>
      <p v-if="form.formError" class="billing-notice billing-notice-error" role="alert">
        {{ form.formError }}
      </p>
      <p v-if="store.startError" class="billing-notice billing-notice-error" role="alert">
        {{ billingErrorMessage(store.startError) }}
      </p>
      <p v-if="store.lastWarning" class="billing-notice billing-notice-warning" role="alert">
        {{ store.lastWarning }}
      </p>
    </form>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="billing-notice billing-notice-success" role="status">
        {{ store.lastMessage }}
      </p>
    </Transition>

    <section v-if="latestRun" class="billing-summary" aria-label="最近一次对账">
      <article class="billing-summary-card">
        <span class="billing-muted">最近一次对账</span>
        <strong>{{ paymentChannelLabel(latestRun.channel) }}</strong>
        <span class="billing-muted">{{ formatBusinessDate(latestRun.businessDate) }}</span>
      </article>
      <article class="billing-summary-card">
        <span class="billing-muted">平台入账</span>
        <strong class="billing-amount">{{ formatCents(latestRun.platformTotalCnyCents) }}</strong>
      </article>
      <article class="billing-summary-card">
        <span class="billing-muted">渠道账单</span>
        <strong class="billing-amount">{{ formatCents(latestRun.channelTotalCnyCents) }}</strong>
      </article>
      <article class="billing-summary-card">
        <span class="billing-muted">差异</span>
        <strong :class="differenceBadgeClass(latestRun.differenceCnyCents)">
          {{ formatSignedCents(latestRun.differenceCnyCents) }}
        </strong>
      </article>
    </section>

    <div class="billing-card billing-run-toolbar">
      <label class="billing-field">
        <span>按状态筛选</span>
        <select v-model="runFilter.status">
          <option value="">全部记录</option>
          <option value="running">进行中</option>
          <option value="completed">已完成</option>
          <option value="failed">未能完成</option>
        </select>
      </label>
      <p class="billing-hint">
        共 {{ store.runs.length }} 次记录，其中 {{ runningRunCount }} 次仍在进行。
      </p>
    </div>

    <div v-if="store.isLoading && store.runs.length === 0" class="billing-state">
      <span class="billing-spinner" aria-hidden="true"></span>
      <p>正在加载对账记录…</p>
    </div>

    <div v-else-if="store.error !== null && store.runs.length === 0" class="billing-state">
      <div class="billing-state-content">
        <strong>对账记录暂时没有加载出来</strong>
        <p>{{ billingErrorMessage(store.error) }}</p>
      </div>
      <button type="button" class="billing-primary" @click="reloadRuns">重新加载对账记录</button>
    </div>

    <div v-else-if="filteredRuns.length === 0" class="billing-state">
      <span class="billing-state-icon" aria-hidden="true">☆</span>
      <div class="billing-state-content">
        <strong>{{ runFilter.status === '' ? '还没有对账记录' : '没有符合条件的对账记录' }}</strong>
        <p>
          {{
            runFilter.status === ''
              ? '选择一个渠道和营业日发起对账，结果会显示在这里。'
              : '换一个状态再查看，或者查看全部记录。'
          }}
        </p>
      </div>
      <button
        v-if="runFilter.status !== ''"
        type="button"
        @click="runFilter.status = ''"
      >
        查看全部记录
      </button>
    </div>

    <div v-else class="billing-table-shell">
      <table>
        <thead>
          <tr>
            <th scope="col">营业日</th>
            <th scope="col">渠道</th>
            <th scope="col">状态</th>
            <th scope="col">平台入账</th>
            <th scope="col">渠道账单</th>
            <th scope="col">差异</th>
            <th scope="col">匹配</th>
            <th scope="col">平台缺失</th>
            <th scope="col">渠道缺失</th>
            <th scope="col">完成时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="run in filteredRuns" :key="run.id">
            <td>{{ formatBusinessDate(run.businessDate) }}</td>
            <td>{{ paymentChannelLabel(run.channel) }}</td>
            <td>
              <span :class="runBadgeClass(run.status)">
                {{ reconciliationStatusLabel(run.status) }}
              </span>
            </td>
            <td class="billing-amount">{{ formatCents(run.platformTotalCnyCents) }}</td>
            <td class="billing-amount">{{ formatCents(run.channelTotalCnyCents) }}</td>
            <td>
              <span :class="differenceBadgeClass(run.differenceCnyCents)">
                {{ formatSignedCents(run.differenceCnyCents) }}
              </span>
            </td>
            <td>{{ run.matchedCount }}</td>
            <td>{{ run.missingCount }}</td>
            <td>{{ run.extraCount }}</td>
            <td>{{ formatDateTime(run.finishedAt) }}</td>
          </tr>
        </tbody>
      </table>
      <p class="billing-hint billing-run-hint">
        「平台缺失」表示渠道账单里有、平台没有入账的订单；「渠道缺失」表示平台已入账、渠道账单里没有的订单。
      </p>
    </div>
  </section>
</template>

<style scoped>
.billing-section-title {
  margin: 0;
  color: #4a2e3b;
  font-size: 17px;
}

.billing-start-form {
  display: grid;
  gap: 14px;
}

.billing-start-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  align-items: end;
  gap: 14px;
}

.billing-start-fields button {
  justify-self: start;
}

.billing-summary {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}

.billing-summary-card {
  display: grid;
  gap: 6px;
  padding: 16px 18px;
  border: 1px solid #f0bdcb;
  border-radius: 20px;
  background: #ffffff;
  box-shadow: 0 8px 22px rgb(194 91 128 / 6%);
}

.billing-summary-card strong {
  color: #4a2e3b;
  font-size: 18px;
}

.billing-run-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: space-between;
  gap: 14px;
}

.billing-run-toolbar .billing-field {
  min-width: 180px;
}

.billing-run-hint {
  padding: 12px 16px 16px;
}
</style>
