<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS } from '@/features/auth/domain/adminPermission'
import {
  useBillingRefundStore,
  type BillingRefundDecision,
} from '@/features/billing/application/billingRefundStore'
import type { BillingRefund } from '@/features/billing/api/billingClient'
import { formatCents, refundStatusLabel } from '@/features/billing/presentation/billingFormat'
import {
  billingErrorMessage,
  readonlyNoticeFor,
} from '@/features/billing/presentation/billingShared'
import '@/features/billing/presentation/billingStyles.css'

const store = useBillingRefundStore()
const authStore = useAuthStore()
const pendingDecision = ref<{ refund: BillingRefund; decision: BillingRefundDecision } | null>(
  null,
)

const canManageBilling = computed(() => authStore.can(ADMIN_PERMISSIONS.billingManage))
const readonlyNotice = computed(() =>
  readonlyNoticeFor('待审批的退款申请', '批准或拒绝退款'),
)
const decisionTitle = computed(() => {
  if (pendingDecision.value === null) {
    return ''
  }
  return pendingDecision.value.decision === 'approve' ? '批准退款申请' : '拒绝退款申请'
})

onMounted(() => {
  void store.load()
})

function requestDecision(refund: BillingRefund, decision: BillingRefundDecision): void {
  if (!canManageBilling.value || store.decidingRefundNo !== '') {
    return
  }
  pendingDecision.value = { refund, decision }
}

function cancelDecision(): void {
  if (store.decidingRefundNo !== '') {
    return
  }
  pendingDecision.value = null
}

async function confirmDecision(): Promise<void> {
  const pending = pendingDecision.value
  if (pending === null) {
    return
  }
  const result = await store.decide(pending.refund.refundNo, pending.decision)
  // 失败时保留弹窗，让管理员看到原因后决定是否重试。
  if (result.succeeded) {
    pendingDecision.value = null
  }
}

function formatDateTime(value: string): string {
  if (!value) {
    return '暂无记录'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '暂无记录'
  }
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function decisionErrorText(): string {
  if (pendingDecision.value === null) {
    return ''
  }
  return billingErrorMessage(
    store.decisionErrorFor(pendingDecision.value.refund.refundNo),
  )
}
</script>

<template>
  <section class="billing-page">
    <header class="billing-page-header">
      <div>
        <p class="billing-eyebrow">计费与订单</p>
        <h1>退款审批</h1>
        <p class="billing-description">
          家长提交的退款申请会先出现在这里。批准后由支付渠道原路退回，金额以申请记录为准。
        </p>
      </div>
      <div class="billing-actions">
        <button type="button" :disabled="store.isLoading" @click="store.load()">
          {{ store.isLoading ? '正在加载…' : '重新加载' }}
        </button>
      </div>
    </header>

    <p v-if="!canManageBilling" class="billing-readonly">{{ readonlyNotice }}</p>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="billing-notice billing-notice-success" role="status">
        {{ store.lastMessage }}
      </p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.lastWarning" class="billing-notice billing-notice-warning" role="alert">
        {{ store.lastWarning }}
      </p>
    </Transition>

    <div v-if="store.isLoading && store.refunds.length === 0" class="billing-state">
      <span class="billing-spinner" aria-hidden="true"></span>
      <p>正在加载退款申请…</p>
    </div>

    <div v-else-if="store.error !== null && store.refunds.length === 0" class="billing-state">
      <div class="billing-state-content">
        <strong>退款申请暂时没有加载出来</strong>
        <p>{{ billingErrorMessage(store.error) }}</p>
      </div>
      <button type="button" class="billing-primary" @click="store.load()">重新加载退款申请</button>
    </div>

    <div v-else-if="store.refunds.length === 0" class="billing-state">
      <span class="billing-state-icon" aria-hidden="true">☆</span>
      <div class="billing-state-content">
        <strong>没有待审批的退款申请</strong>
        <p>家长在应用里提交退款申请后，会出现在这里等待处理。</p>
      </div>
      <button type="button" @click="store.load()">重新加载退款申请</button>
    </div>

    <ul v-else class="billing-refund-list">
      <li v-for="refund in store.refunds" :key="refund.refundNo" class="billing-card">
        <div class="billing-refund-main">
          <div class="billing-stack">
            <strong class="billing-refund-amount">{{ formatCents(refund.amountCnyCents) }}</strong>
            <span class="billing-muted">退款单号 {{ refund.refundNo }}</span>
          </div>
          <span class="billing-badge billing-badge-pending">
            {{ refundStatusLabel(refund.status) }}
          </span>
        </div>
        <dl class="billing-facts">
          <div>
            <dt>关联订单</dt>
            <dd>{{ refund.orderNo || '等待关联' }}</dd>
          </div>
          <div>
            <dt>申请时间</dt>
            <dd>{{ formatDateTime(refund.requestedAt) }}</dd>
          </div>
          <div>
            <dt>退款原因</dt>
            <dd>{{ refund.reason || '家长没有填写原因' }}</dd>
          </div>
        </dl>
        <p v-if="store.decisionErrorFor(refund.refundNo)" class="billing-notice billing-notice-error">
          {{ billingErrorMessage(store.decisionErrorFor(refund.refundNo)) }}
          请确认后再试一次，或者稍后重新加载。
        </p>
        <div v-if="canManageBilling" class="billing-actions billing-refund-actions">
          <button
            type="button"
            class="billing-primary"
            :disabled="store.decidingRefundNo !== ''"
            :aria-label="`批准退款 ${refund.refundNo}`"
            @click="requestDecision(refund, 'approve')"
          >
            {{ store.decidingRefundNo === refund.refundNo ? '正在处理…' : '批准退款' }}
          </button>
          <button
            type="button"
            :disabled="store.decidingRefundNo !== ''"
            :aria-label="`拒绝退款 ${refund.refundNo}`"
            @click="requestDecision(refund, 'reject')"
          >
            拒绝退款
          </button>
        </div>
        <p v-else class="billing-hint">当前账号不能审批退款，需要「处理退款与套餐」权限。</p>
      </li>
    </ul>

    <footer v-if="store.totalPages > 1" class="billing-card billing-refund-pager">
      <p>
        共 {{ store.total }} 笔待审批申请，当前第 {{ store.page }} / {{ store.totalPages }} 页。
      </p>
      <div class="billing-actions">
        <button
          type="button"
          :disabled="store.isLoading || store.page <= 1"
          @click="store.goToPage(store.page - 1)"
        >
          查看上一页
        </button>
        <button
          type="button"
          :disabled="store.isLoading || !store.hasPendingOnOtherPages"
          @click="store.goToPage(store.page + 1)"
        >
          查看下一页
        </button>
      </div>
    </footer>

    <Transition name="modal">
      <div v-if="pendingDecision" class="billing-dialog-backdrop" @click.self="cancelDecision">
        <div
          class="billing-dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="billing-refund-decision-title"
        >
          <h2 id="billing-refund-decision-title">{{ decisionTitle }}</h2>
          <p class="billing-hint">
            退款单 {{ pendingDecision.refund.refundNo }}，金额
            {{ formatCents(pendingDecision.refund.amountCnyCents) }}。
            {{
              pendingDecision.decision === 'approve'
                ? '批准后这笔钱会按原支付渠道退回给家长，操作无法撤回。'
                : '拒绝后家长会看到申请未通过，这笔订单的金额不会变化。'
            }}
          </p>
          <p v-if="decisionErrorText()" class="billing-notice billing-notice-error" role="alert">
            {{ decisionErrorText() }}
          </p>
          <div class="billing-dialog-actions">
            <button
              type="button"
              :disabled="store.decidingRefundNo !== ''"
              @click="cancelDecision"
            >
              暂不处理
            </button>
            <button
              type="button"
              :class="pendingDecision.decision === 'approve' ? 'billing-primary' : ''"
              :disabled="store.decidingRefundNo !== ''"
              @click="confirmDecision"
            >
              {{
                store.decidingRefundNo !== ''
                  ? '正在处理…'
                  : pendingDecision.decision === 'approve'
                    ? '批准退款'
                    : '拒绝退款'
              }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </section>
</template>

<style scoped>
.billing-refund-list {
  display: grid;
  gap: 14px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.billing-refund-list > li {
  display: grid;
  gap: 14px;
}

.billing-refund-main {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
}

.billing-refund-amount {
  color: #4a2e3b;
  font-size: 22px;
  font-variant-numeric: tabular-nums;
}

.billing-refund-actions {
  justify-content: flex-end;
}

.billing-refund-pager {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.billing-refund-pager p {
  margin: 0;
  color: var(--billing-text-muted);
  font-size: 13px;
}
</style>
