<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS } from '@/features/auth/domain/adminPermission'
import { useBillingChannelStore } from '@/features/billing/application/billingChannelStore'
import { useBillingOrderStore } from '@/features/billing/application/billingOrderStore'
import {
  formatCents,
  billingPeriodLabel,
  billingStatusLabel,
  paymentChannelLabel,
  paymentStatusLabel,
  refundStatusLabel,
} from '@/features/billing/presentation/billingFormat'
import {
  billingErrorMessage,
  emptyStateCopy,
  readonlyNoticeFor,
} from '@/features/billing/presentation/billingShared'
import '@/features/billing/presentation/billingStyles.css'

const store = useBillingOrderStore()
const channelStore = useBillingChannelStore()
const authStore = useAuthStore()
const filterForm = reactive({
  status: store.filters.status,
  planCode: store.filters.planCode,
  from: store.filters.from,
  to: store.filters.to,
})

// 订单本身没有可用的写操作；这里的判断让详情面板不会在只有查看权限时
// 渲染出需要管理权限的内容。
const canManageBilling = computed(() => authStore.can(ADMIN_PERMISSIONS.billingManage))
const readonlyNotice = computed(() =>
  readonlyNoticeFor('订单、支付记录与退款记录', '后续的退款与套餐调整'),
)
const emptyCopy = computed(() =>
  emptyStateCopy(
    store.hasFilters,
    '还没有订单',
    '家长下单后会显示在这里。刚上线的套餐需要家长在应用里完成购买。',
  ),
)
const orderStatusOptions = [
  { value: '', label: '全部状态' },
  { value: 'pending', label: '待支付' },
  { value: 'paid', label: '已支付' },
  { value: 'cancelled', label: '已取消' },
  { value: 'expired', label: '已过期' },
  { value: 'refunded', label: '已退款' },
  { value: 'partially_refunded', label: '部分退款' },
]

onMounted(() => {
  void store.load()
  // Channel health loads alongside the orders because an empty order list is
  // ambiguous on its own: it means "nobody bought" only when a channel is
  // actually able to take money.
  void channelStore.load()
})

async function applyFilters(): Promise<void> {
  store.filters = {
    status: filterForm.status,
    planCode: filterForm.planCode.trim(),
    from: filterForm.from,
    to: filterForm.to,
  }
  await store.applyFilters()
}

async function clearFilters(): Promise<void> {
  await store.resetFilters()
  filterForm.status = store.filters.status
  filterForm.planCode = store.filters.planCode
  filterForm.from = store.filters.from
  filterForm.to = store.filters.to
}

async function changePageSize(event: Event): Promise<void> {
  const target = event.target
  if (!(target instanceof HTMLSelectElement)) {
    return
  }
  await store.setPageSize(Number(target.value))
}

function openOrder(orderNo: string): void {
  void store.loadDetail(orderNo)
}

function closeOrder(): void {
  store.closeDetail()
}

function statusBadgeClass(status: string): string {
  if (status === 'paid') {
    return 'billing-badge billing-badge-positive'
  }
  if (status === 'pending') {
    return 'billing-badge billing-badge-pending'
  }
  if (status === 'cancelled' || status === 'expired') {
    return 'billing-badge billing-badge-neutral'
  }
  if (status === 'refunded' || status === 'partially_refunded') {
    return 'billing-badge billing-badge-negative'
  }
  return 'billing-badge billing-badge-neutral'
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

function orderTitle(orderNo: string): string {
  return `订单 ${orderNo}`
}

const detailOrder = computed(() => store.detail?.order ?? null)
</script>

<template>
  <section class="billing-page">
    <header class="billing-page-header">
      <div>
        <p class="billing-eyebrow">计费与订单</p>
        <h1>订单</h1>
        <p class="billing-description">
          查看家长购买的每一笔订单、支付尝试和退款记录。金额按分记账，这里统一显示为元。
        </p>
      </div>
      <div class="billing-actions">
        <button type="button" :disabled="store.isLoading" @click="store.load()">
          {{ store.isLoading ? '正在加载…' : '重新加载' }}
        </button>
      </div>
    </header>

    <p v-if="!canManageBilling" class="billing-readonly">{{ readonlyNotice }}</p>

    <p v-if="channelStore.isSellingUnavailable" class="billing-readonly">
      已登记 {{ channelStore.registeredCount }} 个支付渠道，但目前都还不能收款。在渠道配置完成前，家长无法下单，
      这里的空订单代表「收不了款」，不代表「没有人买」。
    </p>

    <form class="billing-card billing-filters" aria-label="订单筛选" @submit.prevent="applyFilters">
      <label class="billing-field">
        <span>订单状态</span>
        <select v-model="filterForm.status" :disabled="store.isLoading">
          <option v-for="option in orderStatusOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
      </label>
      <label class="billing-field">
        <span>套餐编号</span>
        <input
          v-model="filterForm.planCode"
          type="search"
          placeholder="例如 starter_monthly"
          :disabled="store.isLoading"
        />
      </label>
      <label class="billing-field">
        <span>下单开始日期</span>
        <input v-model="filterForm.from" type="date" :disabled="store.isLoading" />
      </label>
      <label class="billing-field">
        <span>下单结束日期</span>
        <input v-model="filterForm.to" type="date" :disabled="store.isLoading" />
      </label>
      <div class="billing-actions billing-filter-actions">
        <button type="submit" class="billing-primary" :disabled="store.isLoading">
          查询订单
        </button>
        <button type="button" :disabled="store.isLoading || !store.hasFilters" @click="clearFilters">
          清除筛选
        </button>
      </div>
    </form>

    <div v-if="store.isLoading && store.orders.length === 0" class="billing-state">
      <span class="billing-spinner" aria-hidden="true"></span>
      <p>正在加载订单…</p>
    </div>

    <div v-else-if="store.error !== null && store.orders.length === 0" class="billing-state">
      <div class="billing-state-content">
        <strong>订单暂时没有加载出来</strong>
        <p>{{ billingErrorMessage(store.error) }}</p>
      </div>
      <button type="button" class="billing-primary" @click="store.load()">重新加载订单</button>
    </div>

    <div v-else-if="store.orders.length === 0" class="billing-state">
      <span class="billing-state-icon" aria-hidden="true">☆</span>
      <div class="billing-state-content">
        <strong>{{ emptyCopy.title }}</strong>
        <p>{{ emptyCopy.hint }}</p>
      </div>
      <div class="billing-actions">
        <button v-if="store.pagination.page > 1" type="button" @click="store.goToPage(store.pagination.page - 1)">
          返回上一页
        </button>
        <button v-if="store.hasFilters" type="button" class="billing-primary" @click="clearFilters">
          清除筛选
        </button>
        <button v-else type="button" @click="store.load()">重新加载订单</button>
      </div>
    </div>

    <div v-else class="billing-table-shell">
      <p v-if="store.error !== null" class="billing-notice billing-notice-error billing-inline-error">
        {{ billingErrorMessage(store.error) }}可以点「重新加载」再试一次。
      </p>
      <table>
        <caption class="billing-caption">一共 {{ store.pagination.total }} 笔订单</caption>
        <thead>
          <tr>
            <th scope="col">订单号</th>
            <th scope="col">套餐</th>
            <th scope="col">状态</th>
            <th scope="col">应付</th>
            <th scope="col">实付</th>
            <th scope="col">已退</th>
            <th scope="col">下单时间</th>
            <th scope="col"><span class="billing-visually-hidden">操作</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="order in store.orders" :key="order.orderNo">
            <td>
              <span class="billing-stack">
                <strong>{{ order.orderNo }}</strong>
                <span class="billing-muted">{{ order.planCode || '套餐编号已变化' }}</span>
              </span>
            </td>
            <td>
              <span class="billing-stack">
                <span>{{ order.planDisplayName || '未命名套餐' }}</span>
                <span class="billing-muted">
                  {{ billingPeriodLabel(order.billingPeriod, order.periodMonths) }}
                </span>
              </span>
            </td>
            <td>
              <span :class="statusBadgeClass(order.status)">
                {{ billingStatusLabel(order.status) }}
              </span>
            </td>
            <td class="billing-amount">{{ formatCents(order.payableCnyCents) }}</td>
            <td class="billing-amount">{{ formatCents(order.paidCnyCents) }}</td>
            <td class="billing-amount">{{ formatCents(order.refundedCnyCents) }}</td>
            <td>{{ formatDateTime(order.createdAt) }}</td>
            <td>
              <button
                type="button"
                class="billing-quiet"
                :aria-label="`查看订单 ${order.orderNo}`"
                @click="openOrder(order.orderNo)"
              >
                查看详情
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <footer class="billing-pagination">
        <p>
          第 {{ store.pageStart }}–{{ store.pageEnd }} 笔，共 {{ store.pagination.total }} 笔
        </p>
        <div class="billing-pagination-controls">
          <label>
            <span>每页</span>
            <select
              :value="store.pagination.pageSize"
              :disabled="store.isLoading"
              @change="changePageSize"
            >
              <option :value="10">10 笔</option>
              <option :value="20">20 笔</option>
              <option :value="50">50 笔</option>
            </select>
          </label>
          <button
            type="button"
            :disabled="store.isLoading || store.pagination.page <= 1"
            @click="store.goToPage(store.pagination.page - 1)"
          >
            上一页
          </button>
          <span>第 {{ store.pagination.page }} / {{ Math.max(store.pagination.totalPages, 1) }} 页</span>
          <button
            type="button"
            :disabled="
              store.isLoading ||
              store.pagination.totalPages === 0 ||
              store.pagination.page >= store.pagination.totalPages
            "
            @click="store.goToPage(store.pagination.page + 1)"
          >
            下一页
          </button>
        </div>
      </footer>
    </div>

    <Transition name="modal">
      <div
        v-if="store.detailOrderNo !== ''"
        class="billing-drawer-backdrop"
        @click.self="closeOrder"
      >
        <aside
          class="billing-drawer"
          role="dialog"
          aria-modal="true"
          :aria-label="orderTitle(store.detailOrderNo)"
          @keydown.esc="closeOrder"
        >
          <header class="billing-drawer-header">
            <div>
              <p class="billing-eyebrow">订单详情</p>
              <h2>{{ store.detailOrderNo }}</h2>
              <p v-if="detailOrder" class="billing-hint">
                {{ detailOrder.planDisplayName || '未命名套餐' }} ·
                {{ billingStatusLabel(detailOrder.status) }}
              </p>
            </div>
            <button type="button" :aria-label="'关闭订单详情'" @click="closeOrder">关闭</button>
          </header>

          <div class="billing-drawer-body">
            <div v-if="store.isDetailLoading" class="billing-state">
              <span class="billing-spinner" aria-hidden="true"></span>
              <p>正在加载订单详情…</p>
            </div>
            <div v-else-if="store.detailError !== null" class="billing-state">
              <div class="billing-state-content">
                <strong>订单详情暂时没有加载出来</strong>
                <p>{{ billingErrorMessage(store.detailError) }}</p>
              </div>
              <button
                type="button"
                class="billing-primary"
                @click="store.loadDetail(store.detailOrderNo)"
              >
                重新加载详情
              </button>
            </div>
            <template v-else-if="detailOrder">
              <section class="billing-detail-card">
                <header>
                  <h3>套餐与金额</h3>
                  <span :class="statusBadgeClass(detailOrder.status)">
                    {{ billingStatusLabel(detailOrder.status) }}
                  </span>
                </header>
                <dl class="billing-facts">
                  <div>
                    <dt>套餐名称</dt>
                    <dd>{{ detailOrder.planDisplayName || '未命名套餐' }}</dd>
                  </div>
                  <div>
                    <dt>套餐编号</dt>
                    <dd>{{ detailOrder.planCode || '暂无' }}</dd>
                  </div>
                  <div>
                    <dt>计费周期</dt>
                    <dd>
                      {{ billingPeriodLabel(detailOrder.billingPeriod, detailOrder.periodMonths) }}
                    </dd>
                  </div>
                  <div>
                    <dt>单价</dt>
                    <dd>{{ formatCents(detailOrder.unitPriceCnyCents) }}</dd>
                  </div>
                  <div>
                    <dt>优惠</dt>
                    <dd>{{ formatCents(detailOrder.discountCnyCents) }}</dd>
                  </div>
                  <div>
                    <dt>应付</dt>
                    <dd>{{ formatCents(detailOrder.payableCnyCents) }}</dd>
                  </div>
                  <div>
                    <dt>实付</dt>
                    <dd>{{ formatCents(detailOrder.paidCnyCents) }}</dd>
                  </div>
                  <div>
                    <dt>已退</dt>
                    <dd>{{ formatCents(detailOrder.refundedCnyCents) }}</dd>
                  </div>
                </dl>
                <p class="billing-hint">
                  下单时间 {{ formatDateTime(detailOrder.createdAt) }} · 支付时间
                  {{ formatDateTime(detailOrder.paidAt) }} · 待支付截止
                  {{ formatDateTime(detailOrder.expiresAt) }}
                </p>
                <p v-if="detailOrder.remark" class="billing-hint">运营备注：{{ detailOrder.remark }}</p>
              </section>

              <section class="billing-detail-card">
                <header>
                  <h3>支付尝试</h3>
                  <span class="billing-muted">{{ store.detail?.payments.length ?? 0 }} 次</span>
                </header>
                <ul v-if="(store.detail?.payments.length ?? 0) > 0" class="billing-list">
                  <li v-for="payment in store.detail?.payments ?? []" :key="payment.id">
                    <strong>
                      {{ paymentChannelLabel(payment.channel) }} ·
                      {{ formatCents(payment.amountCnyCents) }}
                    </strong>
                    <span class="billing-muted">
                      {{ paymentStatusLabel(payment.status) }} ·
                      {{ formatDateTime(payment.createdAt) }}
                    </span>
                    <span v-if="payment.channelTradeNo" class="billing-muted">
                      渠道单号 {{ payment.channelTradeNo }}
                    </span>
                    <span v-if="payment.failureMessage" class="billing-muted">
                      未成功原因：{{ payment.failureMessage }}
                    </span>
                  </li>
                </ul>
                <p v-else class="billing-inline-empty">
                  这笔订单还没有支付尝试。家长发起支付后，每次尝试都会记录在这里。
                </p>
              </section>

              <section class="billing-detail-card">
                <header>
                  <h3>退款记录</h3>
                  <span class="billing-muted">{{ store.detail?.refunds.length ?? 0 }} 笔</span>
                </header>
                <ul v-if="(store.detail?.refunds.length ?? 0) > 0" class="billing-list">
                  <li v-for="refund in store.detail?.refunds ?? []" :key="refund.refundNo">
                    <strong>{{ refund.refundNo }} · {{ formatCents(refund.amountCnyCents) }}</strong>
                    <span class="billing-muted">
                      {{ refundStatusLabel(refund.status) }} · 申请于
                      {{ formatDateTime(refund.requestedAt) }}
                    </span>
                    <span v-if="refund.reason" class="billing-muted">
                      退款原因：{{ refund.reason }}
                    </span>
                  </li>
                </ul>
                <p v-else class="billing-inline-empty">
                  这笔订单没有退款记录。家长提交退款申请后，会在这里显示处理进度。
                </p>
              </section>
            </template>
          </div>

          <footer class="billing-drawer-footer">
            <button type="button" @click="closeOrder">关闭订单详情</button>
          </footer>
        </aside>
      </div>
    </Transition>
  </section>
</template>

<style scoped>
.billing-filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  align-items: end;
  gap: 14px;
}

.billing-filter-actions {
  grid-column: 1 / -1;
  justify-content: flex-end;
}

.billing-caption {
  padding: 14px 16px 4px;
  color: var(--billing-text-muted);
  font-size: 13px;
  text-align: left;
}

.billing-inline-error {
  margin: 14px 16px 0;
}

.billing-visually-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
</style>
