<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS } from '@/features/auth/domain/adminPermission'
import { useBillingPlanStore } from '@/features/billing/application/billingPlanStore'
import type { BillingPlan, BillingPlanInput } from '@/features/billing/api/billingClient'
import {
  billingPeriodLabel,
  formatCents,
  isKnownPlanStatus,
  planStatusLabel,
} from '@/features/billing/presentation/billingFormat'
import {
  billingErrorMessage,
  emptyStateCopy,
  readonlyNoticeFor,
} from '@/features/billing/presentation/billingShared'
import '@/features/billing/presentation/billingStyles.css'

interface PlanFormState extends BillingPlanInput {
  id: string
  version: number | null
  // 价格以元输入，提交前换算成整数分，避免小数进入业务金额。
  priceYuan: string
  originalPriceYuan: string
  formError: string
}

const store = useBillingPlanStore()
const authStore = useAuthStore()
const isEditorOpen = ref(false)
const form = reactive<PlanFormState>(emptyForm())

const canManageBilling = computed(() => authStore.can(ADMIN_PERMISSIONS.billingManage))
const readonlyNotice = computed(() =>
  readonlyNoticeFor('套餐列表与价格', '新建或修改套餐'),
)
const emptyCopy = computed(() =>
  emptyStateCopy(false, '还没有套餐', '新建套餐后，家长就能在应用里看到可购买的选项。'),
)
const editorTitle = computed(() => (form.id === '' ? '新建套餐' : '修改套餐'))
const periodOptions = [
  { value: 'monthly', label: '按月' },
  { value: 'quarterly', label: '按季' },
  { value: 'yearly', label: '按年' },
  { value: 'one_time', label: '一次性' },
]
const statusOptions = [
  { value: 'draft', label: '草稿（家长看不到）' },
  { value: 'active', label: '已上架' },
  { value: 'archived', label: '已下架' },
]

onMounted(() => {
  void store.load()
})

function openCreate(): void {
  if (!canManageBilling.value) {
    return
  }
  Object.assign(form, emptyForm())
  isEditorOpen.value = true
}

function openEdit(plan: BillingPlan): void {
  if (!canManageBilling.value) {
    return
  }
  Object.assign(form, {
    id: plan.id,
    version: plan.version,
    code: plan.code,
    displayName: plan.displayName,
    description: plan.description,
    billingPeriod: plan.billingPeriod,
    periodMonths: plan.periodMonths,
    priceCnyCents: plan.priceCnyCents,
    originalPriceCnyCents: plan.originalPriceCnyCents,
    status: isKnownPlanStatus(plan.status) ? plan.status : 'draft',
    sortOrder: plan.sortOrder,
    priceYuan: (plan.priceCnyCents / 100).toFixed(2),
    originalPriceYuan:
      plan.originalPriceCnyCents === 0 ? '' : (plan.originalPriceCnyCents / 100).toFixed(2),
    formError: '',
  })
  isEditorOpen.value = true
}

function closeEditor(): void {
  if (store.isSaving) {
    return
  }
  isEditorOpen.value = false
}

async function savePlan(): Promise<void> {
  form.formError = ''
  if (!canManageBilling.value) {
    return
  }
  const validation = validateForm(form)
  if (validation !== '') {
    form.formError = validation
    return
  }
  const payload: BillingPlanInput = {
    code: form.code,
    displayName: form.displayName,
    description: form.description,
    billingPeriod: form.billingPeriod,
    periodMonths: form.periodMonths,
    priceCnyCents: yuanToCents(form.priceYuan),
    originalPriceCnyCents:
      form.originalPriceYuan.trim() === '' ? 0 : yuanToCents(form.originalPriceYuan),
    status: form.status,
    sortOrder: form.sortOrder,
  }
  const result =
    form.id === '' || form.version === null
      ? await store.create(payload)
      : await store.update({ ...payload, id: form.id, version: form.version })
  // 冲突时不关闭编辑器：管理员看到提示后可以重新加载最新数据再修改。
  if (result.succeeded) {
    isEditorOpen.value = false
  }
}

async function reloadAfterConflict(): Promise<void> {
  await store.reloadAfterConflict()
  const plan = form.id === '' ? null : store.planById(form.id)
  if (plan === null) {
    isEditorOpen.value = false
    return
  }
  openEdit(plan)
}

function validateForm(state: PlanFormState): string {
  if (state.code.trim() === '') {
    return '请填写套餐编号，例如 starter_monthly。'
  }
  if (!/^[a-z0-9_]+$/.test(state.code.trim())) {
    return '套餐编号只能使用小写字母、数字和下划线。'
  }
  if (state.displayName.trim() === '') {
    return '请填写套餐名称，家长会在应用里看到这个名字。'
  }
  if (yuanToCents(state.priceYuan) < 0) {
    return '价格请填写 0 或更大的金额，最多两位小数。'
  }
  if (state.originalPriceYuan.trim() !== '' && yuanToCents(state.originalPriceYuan) < 0) {
    return '划线价请填写 0 或更大的金额，最多两位小数。'
  }
  if (state.billingPeriod !== 'one_time' && state.periodMonths < 1) {
    return '按周期的套餐请填写大于 0 的周期月数。'
  }
  return ''
}

function yuanToCents(value: string): number {
  const amount = Number(value)
  if (!Number.isFinite(amount) || amount < 0) {
    return -1
  }
  return Math.round(amount * 100)
}

function emptyForm(): PlanFormState {
  return {
    id: '',
    version: null,
    code: '',
    displayName: '',
    description: '',
    billingPeriod: 'monthly',
    periodMonths: 1,
    priceCnyCents: 0,
    originalPriceCnyCents: 0,
    status: 'draft',
    sortOrder: 0,
    priceYuan: '',
    originalPriceYuan: '',
    formError: '',
  }
}

function planBadgeClass(status: string): string {
  if (status === 'active') {
    return 'billing-badge billing-badge-positive'
  }
  if (status === 'draft') {
    return 'billing-badge billing-badge-pending'
  }
  return 'billing-badge billing-badge-neutral'
}

function reloadPlans(): void {
  void store.load()
}
</script>

<template>
  <section class="billing-page">
    <header class="billing-page-header">
      <div>
        <p class="billing-eyebrow">计费与订单</p>
        <h1>套餐</h1>
        <p class="billing-description">
          维护家长可以购买的套餐。草稿状态的套餐不会在应用里出现，上架后家长即可下单。
        </p>
      </div>
      <div class="billing-actions">
        <button type="button" :disabled="store.isLoading" @click="reloadPlans">
          {{ store.isLoading ? '正在加载…' : '重新加载' }}
        </button>
        <button
          v-if="canManageBilling"
          type="button"
          class="billing-primary"
          :disabled="store.isLoading || store.isSaving"
          @click="openCreate"
        >
          新建套餐
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
    <Transition name="toast">
      <!-- 冲突提示只在编辑弹窗关闭时显示在页面上，避免同一条提示出现两次。 -->
      <div
        v-if="store.conflictMessage && !isEditorOpen"
        class="billing-notice billing-notice-warning conflict-box"
      >
        <span>{{ store.conflictMessage }}</span>
        <button type="button" class="billing-quiet" @click="reloadAfterConflict">
          重新加载套餐
        </button>
      </div>
    </Transition>

    <div v-if="store.isLoading && store.plans.length === 0" class="billing-state">
      <span class="billing-spinner" aria-hidden="true"></span>
      <p>正在加载套餐…</p>
    </div>

    <div v-else-if="store.error !== null && store.plans.length === 0" class="billing-state">
      <div class="billing-state-content">
        <strong>套餐暂时没有加载出来</strong>
        <p>{{ billingErrorMessage(store.error) }}</p>
      </div>
      <button type="button" class="billing-primary" @click="reloadPlans">重新加载套餐</button>
    </div>

    <div v-else-if="store.plans.length === 0" class="billing-state">
      <span class="billing-state-icon" aria-hidden="true">☆</span>
      <div class="billing-state-content">
        <strong>{{ emptyCopy.title }}</strong>
        <p>{{ emptyCopy.hint }}</p>
      </div>
      <div class="billing-actions">
        <button v-if="canManageBilling" type="button" class="billing-primary" @click="openCreate">
          新建套餐
        </button>
        <button v-else type="button" @click="reloadPlans">重新加载套餐</button>
      </div>
    </div>

    <template v-else>
      <p class="billing-hint">
        共 {{ store.plans.length }} 个套餐，其中 {{ store.activePlanCount }} 个已上架，
        {{ store.draftPlanCount }} 个草稿。
      </p>
      <ul class="billing-plan-list">
        <li v-for="plan in store.plans" :key="plan.id" class="billing-card billing-plan-card">
          <div class="billing-plan-main">
            <div class="billing-stack">
              <strong class="billing-plan-name">{{ plan.displayName || '未命名套餐' }}</strong>
              <span class="billing-muted">
                {{ plan.code || '编号未填写' }} ·
                {{ billingPeriodLabel(plan.billingPeriod, plan.periodMonths) }}
              </span>
              <span v-if="plan.description" class="billing-muted">{{ plan.description }}</span>
            </div>
            <div class="billing-plan-price">
              <strong class="billing-amount">{{ formatCents(plan.priceCnyCents) }}</strong>
              <span
                v-if="plan.originalPriceCnyCents > plan.priceCnyCents"
                class="billing-muted billing-original-price"
              >
                原价 {{ formatCents(plan.originalPriceCnyCents) }}
              </span>
            </div>
          </div>
          <div class="billing-plan-footer">
            <span :class="planBadgeClass(plan.status)">{{ planStatusLabel(plan.status) }}</span>
            <button
              v-if="canManageBilling"
              type="button"
              class="billing-quiet"
              :aria-label="`修改套餐 ${plan.displayName || plan.code}`"
              @click="openEdit(plan)"
            >
              修改套餐
            </button>
          </div>
        </li>
      </ul>
    </template>

    <Transition name="modal">
      <div v-if="isEditorOpen" class="billing-dialog-backdrop" @click.self="closeEditor">
        <form
          class="billing-dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="billing-plan-editor-title"
          @submit.prevent="savePlan"
        >
          <h2 id="billing-plan-editor-title">{{ editorTitle }}</h2>
          <p class="billing-hint">保存后家长看到的是上架套餐的价格和说明。</p>

          <label class="billing-field">
            <span>套餐编号</span>
            <input
              v-model="form.code"
              type="text"
              required
              :disabled="store.isSaving || form.id !== ''"
              placeholder="例如 starter_monthly"
            />
          </label>
          <p class="billing-hint">编号一经创建不能修改，历史订单会一直引用它。</p>

          <label class="billing-field">
            <span>套餐名称</span>
            <input
              v-model="form.displayName"
              type="text"
              required
              :disabled="store.isSaving"
              placeholder="例如 初芽体验包"
            />
          </label>

          <label class="billing-field">
            <span>家长看到的说明</span>
            <textarea v-model="form.description" rows="3" :disabled="store.isSaving" />
          </label>

          <div class="billing-editor-grid">
            <label class="billing-field">
              <span>计费周期</span>
              <select v-model="form.billingPeriod" :disabled="store.isSaving">
                <option v-for="option in periodOptions" :key="option.value" :value="option.value">
                  {{ option.label }}
                </option>
              </select>
            </label>
            <label class="billing-field">
              <span>周期月数</span>
              <input
                v-model.number="form.periodMonths"
                type="number"
                min="0"
                step="1"
                :disabled="store.isSaving || form.billingPeriod === 'one_time'"
              />
            </label>
            <label class="billing-field">
              <span>售价（元）</span>
              <input
                v-model="form.priceYuan"
                type="text"
                inputmode="decimal"
                required
                :disabled="store.isSaving"
                placeholder="例如 9.90"
              />
            </label>
            <label class="billing-field">
              <span>划线价（元，留空不展示）</span>
              <input
                v-model="form.originalPriceYuan"
                type="text"
                inputmode="decimal"
                :disabled="store.isSaving"
                placeholder="例如 19.90"
              />
            </label>
            <label class="billing-field">
              <span>状态</span>
              <select v-model="form.status" :disabled="store.isSaving">
                <option v-for="option in statusOptions" :key="option.value" :value="option.value">
                  {{ option.label }}
                </option>
              </select>
            </label>
            <label class="billing-field">
              <span>展示顺序</span>
              <input
                v-model.number="form.sortOrder"
                type="number"
                min="0"
                step="1"
                :disabled="store.isSaving"
              />
            </label>
          </div>

          <p v-if="form.formError" class="billing-notice billing-notice-error" role="alert">
            {{ form.formError }}
          </p>
          <p v-if="store.saveError" class="billing-notice billing-notice-error" role="alert">
            {{ billingErrorMessage(store.saveError) }}
          </p>
          <div v-if="store.conflictMessage" class="billing-notice billing-notice-warning conflict-box">
            <span>{{ store.conflictMessage }}</span>
            <button type="button" class="billing-quiet" @click="reloadAfterConflict">
              重新加载套餐
            </button>
          </div>

          <div class="billing-dialog-actions">
            <button type="button" :disabled="store.isSaving" @click="closeEditor">取消</button>
            <button type="submit" class="billing-primary" :disabled="store.isSaving">
              {{ store.isSaving ? '正在保存…' : '保存套餐' }}
            </button>
          </div>
        </form>
      </div>
    </Transition>
  </section>
</template>

<style scoped>
.billing-plan-list {
  display: grid;
  gap: 14px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.billing-plan-card {
  display: grid;
  gap: 14px;
}

.billing-plan-main {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.billing-plan-name {
  color: #4a2e3b;
  font-size: 18px;
}

.billing-plan-price {
  display: grid;
  justify-items: end;
  gap: 4px;
  text-align: right;
}

.billing-plan-price strong {
  color: #4a2e3b;
  font-size: 20px;
}

.billing-original-price {
  text-decoration: line-through;
}

.billing-plan-footer {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-top: 12px;
  border-top: 1px solid #f7d9e2;
}

.billing-editor-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.conflict-box {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

@media (max-width: 640px) {
  .billing-editor-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .billing-plan-price {
    justify-items: start;
    text-align: left;
  }
}
</style>
