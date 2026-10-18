<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import type { ApiError } from '@/api/apiError'
import type { AIModelOption } from '@/api/adminOperations'
import {
  useFamilyAccountStore,
  type AdminFamily,
  type AdminFamilyAiAccount,
  type CreateAdminFamilyInput,
  type FamilyMutationResult,
  type UpdateAdminFamilyAiAccountInput,
  type UpdateAdminFamilyProfileInput,
} from '@/features/family/application/familyAccountStore'

const store = useFamilyAccountStore()
const isCreateOpen = ref(false)
const selectedFamilyId = ref('')
const profileForm = reactive<UpdateAdminFamilyProfileInput>(
  emptyProfileForm(),
)
const aiForm = reactive<UpdateAdminFamilyAiAccountInput>(
  emptyAiAccountForm(),
)
const passwordForm = reactive({
  password: '',
  confirmation: '',
})
const isPasswordResetOpen = ref(false)
const feedbackBySection = reactive({
  profile: emptySectionFeedback(),
  ai: emptySectionFeedback(),
  password: emptySectionFeedback(),
})
const createForm = reactive<CreateAdminFamilyInput>({
  phone: '',
  password: '',
  guardianFamilyName: '',
  childNickname: '',
  childBirthday: '',
})

const selectedFamily = computed(
  () =>
    store.families.find(
      (family) => family.parentAccountId === selectedFamilyId.value,
    ) ?? null,
)
const selectedDevices = computed(() =>
  selectedFamily.value === null
    ? []
    : store.devicesFor(selectedFamily.value.parentAccountId),
)
const isDevicesLoading = computed(
  () =>
    selectedFamily.value !== null &&
    store.isFamilyDevicesLoading(selectedFamily.value.parentAccountId),
)
const deviceError = computed(
  () =>
    selectedFamily.value === null
      ? null
      : store.deviceErrorFor(selectedFamily.value.parentAccountId),
)
const availableModels = computed(() =>
  modelOptionsForAccount(store.modelOptions, aiForm.availableModels),
)
const aiUsage = computed(() =>
  selectedFamily.value === null
    ? null
    : store.aiUsageFor(selectedFamily.value.parentAccountId),
)
const isAIUsageLoading = computed(
  () =>
    selectedFamily.value !== null &&
    store.isFamilyAIUsageLoading(selectedFamily.value.parentAccountId),
)
const aiUsageError = computed(() =>
  selectedFamily.value === null
    ? null
    : store.aiUsageErrorFor(selectedFamily.value.parentAccountId),
)

onMounted(() => {
  void store.load()
})

function openCreate(): void {
  if (store.isLoading || store.isSubmitting) {
    return
  }
  isCreateOpen.value = true
  store.lastMessage = ''
}

function closeCreate(): void {
  if (store.isSubmitting) {
    return
  }
  isCreateOpen.value = false
}

async function createFamily(): Promise<void> {
  if (store.isSubmitting) {
    return
  }
  const succeeded = await store.create(createForm)
  if (!succeeded) {
    return
  }
  resetCreateForm()
  closeCreate()
}

function resetCreateForm(): void {
  createForm.phone = ''
  createForm.password = ''
  createForm.guardianFamilyName = ''
  createForm.childNickname = ''
  createForm.childBirthday = ''
}

async function openFamily(family: AdminFamily): Promise<void> {
  if (store.isLoading || store.isSubmitting) {
    return
  }
  selectedFamilyId.value = family.parentAccountId
  Object.assign(profileForm, {
    displayName: family.parentDisplayName,
    guardianFamilyName: family.guardianFamilyName,
    childNickname: family.childNickname,
    childBirthday: family.childBirthday,
  })
  if (family.aiAccount !== null) {
    Object.assign(aiForm, {
      providerAccountId: family.aiAccount.providerAccountId,
      status: family.aiAccount.status,
      balanceUsd: family.aiAccount.balanceUsd,
      concurrencyLimit: family.aiAccount.concurrencyLimit,
      availableModels: assignableModels(family.aiAccount, store.modelOptions),
    })
  }
  clearSectionFeedback()
  await Promise.all([
    store.loadFamilyDevices(family.parentAccountId),
    family.aiAccount === null
      ? Promise.resolve(null)
      : store.loadFamilyAIUsage(family.parentAccountId),
  ])
}

function closeFamily(): void {
  selectedFamilyId.value = ''
  isPasswordResetOpen.value = false
  passwordForm.password = ''
  passwordForm.confirmation = ''
  clearSectionFeedback()
}

async function saveProfile(): Promise<void> {
  if (selectedFamily.value === null) {
    return
  }
  const result = await store.updateParentProfile(
    selectedFamily.value.parentAccountId,
    profileForm,
  )
  handleMutationResult('profile', result, '家长资料已保存。')
}

async function saveAiAccount(): Promise<void> {
  if (selectedFamily.value === null || selectedFamily.value.aiAccount === null) {
    return
  }
  if (aiForm.availableModels.length === 0) {
    setSectionError('ai', {
      kind: 'validation',
      message: '请至少选择一个模型。',
      retryable: false,
    })
    return
  }
  const result = await store.updateAiAccount({
    providerAccountId: selectedFamily.value.aiAccount.providerAccountId,
    status: aiForm.status,
    balanceUsd: aiForm.balanceUsd,
    concurrencyLimit: aiForm.concurrencyLimit,
    availableModels: aiForm.availableModels,
  })
  if (result.succeeded) {
    synchronizeFromSelectedFamily()
  }
  handleMutationResult('ai', result, 'AI 设置已保存。')
}

function openPasswordReset(): void {
  if (
    store.isResettingPassword ||
    store.isUpdatingProfile ||
    store.isUpdatingAiAccount ||
    store.isSubmitting ||
    store.isLoading
  ) {
    return
  }
  isPasswordResetOpen.value = true
  passwordForm.password = ''
  passwordForm.confirmation = ''
  clearSectionFeedback()
}

function closePasswordReset(): void {
  if (store.isResettingPassword) {
    return
  }
  isPasswordResetOpen.value = false
  passwordForm.password = ''
  passwordForm.confirmation = ''
}

async function resetPassword(): Promise<void> {
  if (selectedFamily.value === null) {
    return
  }
  if (store.isResettingPassword) {
    return
  }
  if (passwordForm.password.length < 8) {
    setSectionError('password', {
      kind: 'validation',
      message: '新密码至少需要 8 位。',
      retryable: false,
    })
    return
  }
  if (passwordForm.password !== passwordForm.confirmation) {
    setSectionError('password', {
      kind: 'validation',
      message: '两次输入的密码不一致。',
      retryable: false,
    })
    return
  }
  const result = await store.resetParentPassword(
    selectedFamily.value.parentAccountId,
    passwordForm.password,
  )
  if (result.succeeded) {
    closePasswordReset()
  }
  handleMutationResult(
    'password',
    result,
    '密码已重置，该家长需要重新登录。',
    '密码已重置，请提醒家长使用新密码登录。',
  )
}

async function retryAiAccount(): Promise<void> {
  if (selectedFamily.value === null) {
    return
  }
  if (store.isSubmitting || store.isUpdatingAiAccount) {
    return
  }
  const result = await store.retryAiAccount(selectedFamily.value.parentAccountId)
  handleMutationResult('ai', result, '家长 AI 服务已准备完成。')
}

async function reloadDevices(): Promise<void> {
  if (selectedFamily.value === null) {
    return
  }
  await store.loadFamilyDevices(selectedFamily.value.parentAccountId)
}

async function reloadAIUsage(): Promise<void> {
  if (selectedFamily.value === null) {
    return
  }
  await store.loadFamilyAIUsage(selectedFamily.value.parentAccountId)
}

async function changePageSize(event: Event): Promise<void> {
  const target = event.target
  if (!(target instanceof HTMLSelectElement)) {
    return
  }
  if (store.isLoading || store.isSubmitting) {
    target.value = String(store.pagination.pageSize)
    return
  }
  await store.setPageSize(Number(target.value))
}

function toggleModel(model: AIModelOption): void {
  if (aiForm.availableModels.includes(model.id)) {
    aiForm.availableModels = aiForm.availableModels.filter(
      (modelId) => modelId !== model.id,
    )
    return
  }
  aiForm.availableModels = [...aiForm.availableModels, model.id]
}

function selectAllModels(): void {
  aiForm.availableModels = store.modelOptions.map((model) => model.id)
}

function synchronizeFromSelectedFamily(): void {
  const family = selectedFamily.value
  if (family === null || family.aiAccount === null) {
    return
  }
  Object.assign(aiForm, {
    providerAccountId: family.aiAccount.providerAccountId,
    status: family.aiAccount.status,
    balanceUsd: family.aiAccount.balanceUsd,
    concurrencyLimit: family.aiAccount.concurrencyLimit,
    availableModels: assignableModels(family.aiAccount, store.modelOptions),
  })
}

function handleMutationResult(
  section: SectionName,
  result: FamilyMutationResult,
  successMessage: string,
  refreshFailureMessage = '已保存，但列表暂时没有更新，请重新加载。',
): void {
  if (!result.succeeded) {
    setSectionError(section, result.error)
    return
  }
  if (result.refreshError !== null) {
    feedbackBySection[section].error = result.refreshError
    feedbackBySection[section].message = refreshFailureMessage
    return
  }
  feedbackBySection[section].error = null
  feedbackBySection[section].message = successMessage
}

function clearSectionFeedback(): void {
  resetSectionFeedback('profile')
  resetSectionFeedback('ai')
  resetSectionFeedback('password')
}

function setSectionError(section: SectionName, value: ApiError | null): void {
  feedbackBySection[section].error = value
  feedbackBySection[section].message = ''
}

function resetSectionFeedback(section: SectionName): void {
  feedbackBySection[section].error = null
  feedbackBySection[section].message = ''
}

function statusLabel(value: string): string {
  return value === 'active' ? '可用' : '已暂停'
}

function modelOptionsForAccount(
  models: AIModelOption[],
  selectedModels: string[],
): AIModelOption[] {
  const options = [...models]
  const knownIds = new Set(options.map((model) => model.id))
  for (const modelId of selectedModels) {
    if (!knownIds.has(modelId)) {
      options.push({ id: modelId, latencyMs: null })
    }
  }
  return options.sort((left, right) => {
    const leftLatency = left.latencyMs ?? Number.POSITIVE_INFINITY
    const rightLatency = right.latencyMs ?? Number.POSITIVE_INFINITY
    if (leftLatency !== rightLatency) {
      return leftLatency - rightLatency
    }
    return left.id.localeCompare(right.id)
  })
}

function assignableModels(
  account: AdminFamilyAiAccount,
  models: AIModelOption[],
): string[] {
  const accessible = account.availableModels.length
    ? account.availableModels
    : account.allowedModels
  if (accessible.length > 0) {
    return accessible
  }
  return models.map((model) => model.id)
}

function fastestModel(models: AIModelOption[]): AIModelOption | null {
  const measuredModels = models.filter(
    (model) => model.id && model.latencyMs !== null && model.latencyMs >= 0,
  )
  if (measuredModels.length === 0) {
    return models.find((model) => model.id) ?? null
  }
  return measuredModels.reduce((fastest, model) =>
    (model.latencyMs ?? Number.POSITIVE_INFINITY) <
    (fastest.latencyMs ?? Number.POSITIVE_INFINITY)
      ? model
      : fastest,
  )
}

function latencyLabel(model: AIModelOption): string {
  return model.latencyMs === null ? '延迟待测' : `约 ${model.latencyMs} ms`
}

function isModelSelected(modelId: string): boolean {
  return aiForm.availableModels.includes(modelId)
}

function isFastestModel(modelId: string): boolean {
  return fastestModel(store.modelOptions)?.id === modelId
}

function formatDate(value: string): string {
  if (!value) {
    return '未填写'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '未填写'
  }
  return date.toLocaleDateString('zh-CN')
}

function formatTime(value: string): string {
  if (!value) {
    return '尚未上报'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '尚未上报'
  }
  return date.toLocaleString('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatCount(value: number): string {
  if (!Number.isFinite(value)) {
    return '0'
  }
  return value.toLocaleString('zh-CN')
}

function formatUsd(value: number): string {
  if (!Number.isFinite(value)) {
    return '$0.0000'
  }
  return `$${value.toFixed(4)}`
}

function formatDurationMs(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '暂无数据'
  }
  return `${Math.round(value)} ms`
}

function deviceStatusLabel(isOnline: boolean | null): string {
  if (isOnline === null) {
    return '状态未知'
  }
  return isOnline ? '在线' : '离线'
}

function deviceStatusClass(isOnline: boolean | null): string {
  if (isOnline === null) {
    return 'unknown'
  }
  return isOnline ? 'online' : 'offline'
}

function capabilityLabel(value: string): string {
  return (
    {
      camera: '摄像头',
      microphone: '麦克风',
      display: '显示屏',
      touch: '触摸屏',
      wifi: '无线网络',
      bluetooth: '蓝牙',
      battery: '电池',
      cellular: '移动网络',
    }[value] ?? value
  )
}

function emptyProfileForm(): UpdateAdminFamilyProfileInput {
  return {
    displayName: '',
    guardianFamilyName: '',
    childNickname: '',
    childBirthday: '',
  }
}

function emptyAiAccountForm(): UpdateAdminFamilyAiAccountInput {
  return {
    providerAccountId: '',
    status: 'active',
    balanceUsd: 0,
    concurrencyLimit: 1,
    availableModels: [],
  }
}

type SectionName = 'profile' | 'ai' | 'password'

function emptySectionFeedback(): {
  error: ApiError | null
  message: string
} {
  return { error: null, message: '' }
}
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">家庭与 AI 服务</p>
        <h1>家长账号</h1>
        <p class="page-description">
          查看家庭和绑定设备，统一维护家长资料、AI 服务与登录安全。
        </p>
      </div>
      <div class="header-actions">
        <button
          type="button"
          class="secondary"
          :disabled="store.isLoading"
          @click="store.load()"
        >
          重新加载
        </button>
        <button
          type="button"
          class="primary"
          :disabled="store.isLoading || store.isSubmitting"
          @click="openCreate"
        >
          新建家长账号
        </button>
      </div>
    </header>

    <Transition name="toast">
      <p v-if="store.lastMessage && !selectedFamily" class="success-message">
        {{ store.lastMessage }}
      </p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.error && !selectedFamily" class="error-message">
        {{ store.error.message }}
      </p>
    </Transition>

    <Transition name="page" mode="out-in">
      <div
        v-if="store.isLoading && store.families.length === 0"
        key="loading"
        class="state-panel"
      >
        <span class="state-spinner" aria-hidden="true"></span>
        正在读取家长账号…
      </div>
      <div
        v-else-if="store.families.length === 0 && store.error"
        key="error"
        class="state-panel state-error"
      >
        <p>{{ store.error.message }}</p>
        <button type="button" class="primary" @click="store.retryCurrentPage">
          重新加载这一页
        </button>
      </div>
      <div v-else-if="store.families.length === 0" key="empty" class="state-panel">
        <span class="state-icon" aria-hidden="true">☆</span>
        <div>
          <strong>
            {{
              store.pagination.page > 1
                ? '这一页没有家长账号'
                : '还没有家长账号'
            }}
          </strong>
          <p>
          {{
            store.pagination.page > 1
              ? '列表可能刚刚发生变化，请返回上一页或重新加载。'
              : '新建后，家长可以直接登录应用使用。'
          }}
          </p>
        </div>
        <div class="empty-actions">
          <button
            v-if="store.pagination.page > 1"
            type="button"
            class="secondary"
            :disabled="store.isLoading || store.isSubmitting"
            @click="store.goToPage(store.pagination.page - 1)"
          >
            返回上一页
          </button>
          <button
            type="button"
            class="secondary"
            :disabled="store.isLoading || store.isSubmitting"
            @click="store.retryCurrentPage"
          >
            重新加载
          </button>
        </div>
        <footer class="pagination pagination-empty">
          <p>
            第 {{ store.pageStart }}–{{ store.pageEnd }} 个账号，共
            {{ store.pagination.total }} 个
          </p>
          <div class="pagination-controls">
            <label>
              <span>每页</span>
              <select
                :value="
                  store.pagination.pendingPageSize ??
                  store.pagination.pageSize
                "
                :disabled="store.isLoading || store.isSubmitting"
                @change="changePageSize"
              >
                <option :value="10">10</option>
                <option :value="20">20</option>
                <option :value="50">50</option>
              </select>
            </label>
            <button
              type="button"
              class="secondary"
              :disabled="
                store.isLoading ||
                store.isSubmitting ||
                !store.pagination.hasPrevious
              "
              @click="store.goToPage(store.pagination.page - 1)"
            >
              上一页
            </button>
            <span>
              第 {{ store.pagination.page }} /
              {{ Math.max(store.pagination.totalPages, 1) }} 页
            </span>
            <button
              type="button"
              class="secondary"
              :disabled="
                store.isLoading ||
                store.isSubmitting ||
                !store.pagination.hasMore
              "
              @click="store.goToPage(store.pagination.page + 1)"
            >
              下一页
            </button>
          </div>
        </footer>
      </div>
      <div v-else key="table" class="table-shell">
        <table>
          <thead>
            <tr>
              <th>家长</th>
              <th>宝贝信息</th>
              <th>AI 服务</th>
              <th>剩余额度</th>
              <th>可用模型</th>
              <th>绑定设备</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="family in store.families" :key="family.parentAccountId">
              <td>
                <span class="parent-name">
                  {{
                    family.guardianFamilyName ||
                    family.parentDisplayName ||
                    '未填写称呼'
                  }}
                </span>
                <span class="parent-meta">{{ family.phone || '未绑定手机号' }}</span>
              </td>
              <td>
                <span>{{ family.childNickname || '未填写宝贝姓名' }}</span>
                <span class="parent-meta">
                  {{ formatDate(family.childBirthday) }}
                </span>
              </td>
              <td>
                <span
                  v-if="family.aiAccount"
                  :class="['status', family.aiAccount.status]"
                >
                  {{ statusLabel(family.aiAccount.status) }}
                </span>
                <span v-else class="status missing">待开通</span>
                <span v-if="family.aiAccount" class="parent-meta">
                  {{ family.aiAccount.credentialReady ? '服务已就绪' : '服务待修复' }}
                </span>
              </td>
              <td>
                <span v-if="family.aiAccount">
                  {{ family.aiAccount.balanceUsd.toFixed(2) }}
                </span>
                <span v-else>未开通</span>
              </td>
              <td>
                <span v-if="family.aiAccount">
                  {{
                    assignableModels(family.aiAccount, store.modelOptions).join('、') ||
                    '等待模型'
                  }}
                </span>
                <span v-else>未开通</span>
              </td>
              <td>
                <button
                  type="button"
                  class="link-button"
                  :disabled="store.isLoading || store.isSubmitting"
                  @click="openFamily(family)"
                >
                  查看设备
                </button>
              </td>
              <td>
                <button
                  type="button"
                  :disabled="store.isLoading || store.isSubmitting"
                  @click="openFamily(family)"
                >
                  管理
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <footer class="pagination">
          <p>
            第 {{ store.pageStart }}–{{ store.pageEnd }} 个账号，共
            {{ store.pagination.total }} 个
          </p>
          <div class="pagination-controls">
            <label>
              <span>每页</span>
              <select
                :value="
                  store.pagination.pendingPageSize ??
                  store.pagination.pageSize
                "
                :disabled="store.isLoading || store.isSubmitting"
                @change="changePageSize"
              >
                <option :value="10">10</option>
                <option :value="20">20</option>
                <option :value="50">50</option>
              </select>
            </label>
            <button
              type="button"
              class="secondary"
              :disabled="
                store.isLoading ||
                store.isSubmitting ||
                !store.pagination.hasPrevious
              "
              @click="store.goToPage(store.pagination.page - 1)"
            >
              上一页
            </button>
            <span>
              第 {{ store.pagination.page }} /
              {{ Math.max(store.pagination.totalPages, 1) }} 页
            </span>
            <button
              type="button"
              class="secondary"
              :disabled="
                store.isLoading ||
                store.isSubmitting ||
                !store.pagination.hasMore
              "
              @click="store.goToPage(store.pagination.page + 1)"
            >
              下一页
            </button>
          </div>
        </footer>
      </div>
    </Transition>

    <Transition name="modal">
      <div
        v-if="isCreateOpen"
        class="dialog-backdrop"
        @click.self="closeCreate"
      >
        <form
          class="dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="create-family-title"
          @submit.prevent="createFamily"
        >
          <h2 id="create-family-title">新建家长账号</h2>
          <p class="dialog-hint">
            创建后会自动开通家长 AI 服务，家长可直接登录应用。
          </p>
          <label>
            <span>手机号</span>
            <input
              v-model.trim="createForm.phone"
              type="tel"
              inputmode="tel"
              required
              placeholder="请输入家长手机号"
            />
          </label>
          <label>
            <span>初始密码</span>
            <input
              v-model="createForm.password"
              type="password"
              required
              minlength="8"
              placeholder="至少 8 位"
            />
          </label>
          <label>
            <span>家长姓氏（可选）</span>
            <input v-model.trim="createForm.guardianFamilyName" type="text" />
          </label>
          <label>
            <span>宝贝姓名（可选）</span>
            <input v-model.trim="createForm.childNickname" type="text" />
          </label>
          <label>
            <span>宝贝生日（可选）</span>
            <input v-model="createForm.childBirthday" type="date" />
          </label>
          <div class="dialog-actions">
            <button
              type="button"
              class="secondary"
              :disabled="store.isSubmitting"
              @click="closeCreate"
            >
              取消
            </button>
            <button
              type="submit"
              class="primary"
              :disabled="store.isSubmitting || store.isLoading"
            >
              {{ store.isSubmitting ? '正在创建…' : '创建账号' }}
            </button>
          </div>
        </form>
      </div>
    </Transition>

    <Transition name="drawer">
      <div
        v-if="selectedFamily"
        class="drawer-backdrop"
        @click.self="closeFamily"
      >
        <aside
          class="family-drawer"
          role="dialog"
          aria-modal="true"
          aria-labelledby="family-detail-title"
        >
          <header class="drawer-header">
            <div>
              <p class="eyebrow">家长账号</p>
              <h2 id="family-detail-title">
                {{
                  selectedFamily.guardianFamilyName ||
                  selectedFamily.parentDisplayName ||
                  selectedFamily.phone ||
                  '家长账号'
                }}
              </h2>
              <p class="drawer-subtitle">
                {{ selectedFamily.phone || '未绑定手机号' }} ·
                {{ selectedFamily.status === 'active' ? '账号可用' : '账号已停用' }}
              </p>
            </div>
            <button
              type="button"
              class="icon-button"
              aria-label="关闭家长账号详情"
              @click="closeFamily"
            >
              ×
            </button>
          </header>

          <div class="drawer-body">
            <Transition name="toast">
              <p
                v-if="feedbackBySection.profile.message"
                class="success-message"
                role="status"
              >
                {{ feedbackBySection.profile.message }}
              </p>
            </Transition>
            <Transition name="toast">
              <p
                v-if="feedbackBySection.profile.error"
                class="error-message"
                role="alert"
              >
                {{ feedbackBySection.profile.error.message }}
              </p>
            </Transition>

            <section class="detail-card">
              <div class="section-heading">
                <div>
                  <h3>家长资料</h3>
                  <p>这些信息用于家长端展示和孩子生日提醒。</p>
                </div>
              </div>
              <div class="form-grid">
                <label>
                  <span>家长称呼</span>
                  <input
                    v-model.trim="profileForm.displayName"
                    type="text"
                    autocomplete="name"
                    placeholder="例如：萌屋妈妈"
                  />
                </label>
                <label>
                  <span>家长姓氏</span>
                  <input
                    v-model.trim="profileForm.guardianFamilyName"
                    type="text"
                    placeholder="可选"
                  />
                </label>
                <label>
                  <span>宝贝姓名</span>
                  <input
                    v-model.trim="profileForm.childNickname"
                    type="text"
                    placeholder="可选"
                  />
                </label>
                <label>
                  <span>宝贝生日</span>
                  <input v-model="profileForm.childBirthday" type="date" />
                </label>
              </div>
              <div class="section-actions">
                <button
                  type="button"
                  class="primary"
                  :disabled="
                    store.isUpdatingProfile ||
                    store.isUpdatingAiAccount ||
                    store.isSubmitting
                  "
                  @click="saveProfile"
                >
                  {{ store.isUpdatingProfile ? '正在保存…' : '保存家长资料' }}
                </button>
              </div>
            </section>

            <section class="detail-card">
              <div class="section-heading">
                <div>
                  <h3>AI 服务</h3>
                  <p>额度、并发与模型会直接影响家长端的 AI 使用。</p>
                </div>
                <span
                  v-if="selectedFamily.aiAccount"
                  :class="['status', selectedFamily.aiAccount.status]"
                >
                  {{ statusLabel(selectedFamily.aiAccount.status) }}
                </span>
              </div>

              <div v-if="selectedFamily.aiAccount" class="ai-editor">
                <Transition name="toast">
                  <p
                    v-if="feedbackBySection.ai.message"
                    class="success-message"
                    role="status"
                  >
                    {{ feedbackBySection.ai.message }}
                  </p>
                </Transition>
                <Transition name="toast">
                  <p
                    v-if="feedbackBySection.ai.error"
                    class="error-message"
                    role="alert"
                  >
                    {{ feedbackBySection.ai.error.message }}
                  </p>
                </Transition>
                <div class="form-grid">
                  <label>
                    <span>服务状态</span>
                    <select v-model="aiForm.status">
                      <option value="active">可用</option>
                      <option value="suspended">暂停使用</option>
                    </select>
                  </label>
                  <label>
                    <span>剩余额度</span>
                    <input
                      v-model.number="aiForm.balanceUsd"
                      type="number"
                      min="0"
                      step="0.01"
                    />
                  </label>
                  <label>
                    <span>同时对话数量</span>
                    <input
                      v-model.number="aiForm.concurrencyLimit"
                      type="number"
                      min="1"
                    />
                  </label>
                </div>

                <div class="model-picker">
                  <div class="model-picker-heading">
                    <div>
                      <span class="field-label">可分配模型</span>
                      <p class="field-hint">
                        家长只能使用勾选的模型，列表按当前延迟从低到高排列。
                      </p>
                    </div>
                    <div class="model-picker-actions">
                      <button type="button" class="text-button" @click="selectAllModels">
                        全部选择
                      </button>
                    </div>
                  </div>

                  <div v-if="store.isLoadingModels" class="inline-state">
                    正在读取可用模型…
                  </div>
                  <div v-else-if="store.modelError" class="inline-state error">
                    {{ store.modelError.message }}
                    <button type="button" class="text-button" @click="store.load()">
                      重新加载模型
                    </button>
                  </div>
                  <div v-else-if="availableModels.length === 0" class="inline-state">
                    暂时没有可分配的模型。请稍后重新加载。
                  </div>
                  <ul v-else class="model-list">
                    <li v-for="model in availableModels" :key="model.id">
                      <label class="model-option">
                        <input
                          type="checkbox"
                          :checked="isModelSelected(model.id)"
                          @change="toggleModel(model)"
                        />
                        <span class="model-option-main">
                          <span class="model-name">
                            <strong>{{ model.id }}</strong>
                            <em v-if="isFastestModel(model.id)">默认最快</em>
                          </span>
                          <small>{{ latencyLabel(model) }}</small>
                        </span>
                      </label>
                    </li>
                  </ul>
                </div>

                <div class="section-actions">
                  <button
                    type="button"
                    class="primary"
                    :disabled="
                      store.isUpdatingAiAccount ||
                      store.isUpdatingProfile ||
                      store.isSubmitting
                    "
                    @click="saveAiAccount"
                  >
                    {{
                      store.isUpdatingAiAccount ? '正在保存…' : '保存 AI 设置'
                    }}
                  </button>
                </div>
              </div>

              <div v-else class="empty-section">
                <p>这个家长还没有 AI 服务。</p>
                <button
                  type="button"
                  class="primary"
                  :disabled="
                    store.isSubmitting ||
                    store.isUpdatingAiAccount ||
                    store.isUpdatingProfile
                  "
                  @click="retryAiAccount"
                >
                  {{ store.isSubmitting ? '正在开通…' : '重新开通 AI 服务' }}
                </button>
                <p v-if="feedbackBySection.ai.message" class="success-message">
                  {{ feedbackBySection.ai.message }}
                </p>
                <p v-if="feedbackBySection.ai.error" class="error-message">
                  {{ feedbackBySection.ai.error.message }}
                </p>
              </div>
            </section>

            <section v-if="selectedFamily.aiAccount" class="detail-card">
              <div class="section-heading">
                <div>
                  <h3>AI 用量核对</h3>
                  <p>与网关用量对账，默认显示最近 24 小时的请求与费用。</p>
                </div>
                <button
                  type="button"
                  class="text-button"
                  :disabled="isAIUsageLoading"
                  @click="reloadAIUsage"
                >
                  {{ isAIUsageLoading ? '正在读取…' : '更新用量' }}
                </button>
              </div>

              <div v-if="isAIUsageLoading" class="inline-state">
                正在读取 AI 用量…
              </div>
              <div v-else-if="aiUsageError" class="inline-state error">
                {{ aiUsageError.message }}
                <button
                  type="button"
                  class="text-button"
                  @click="reloadAIUsage"
                >
                  重新加载用量
                </button>
              </div>
              <div v-else-if="!aiUsage" class="empty-section">
                <p>还没有读取到用量数据。点击“更新用量”重新核对。</p>
              </div>
              <template v-else>
                <div class="usage-window">
                  <span>{{ formatTime(aiUsage.startTime) }}</span>
                  <span aria-hidden="true">→</span>
                  <span>{{ formatTime(aiUsage.endTime) }}</span>
                </div>
                <dl class="usage-totals">
                  <div>
                    <dt>请求数</dt>
                    <dd>{{ formatCount(aiUsage.requests) }}</dd>
                  </div>
                  <div>
                    <dt>输入 Token</dt>
                    <dd>{{ formatCount(aiUsage.inputTokens) }}</dd>
                  </div>
                  <div>
                    <dt>输出 Token</dt>
                    <dd>{{ formatCount(aiUsage.outputTokens) }}</dd>
                  </div>
                  <div>
                    <dt>合计 Token</dt>
                    <dd>{{ formatCount(aiUsage.totalTokens) }}</dd>
                  </div>
                  <div>
                    <dt>预估费用</dt>
                    <dd>{{ formatUsd(aiUsage.costUsd) }}</dd>
                  </div>
                  <div>
                    <dt>实际费用</dt>
                    <dd>{{ formatUsd(aiUsage.actualCostUsd) }}</dd>
                  </div>
                  <div>
                    <dt>平均耗时</dt>
                    <dd>{{ formatDurationMs(aiUsage.averageDurationMs) }}</dd>
                  </div>
                </dl>

                <ul v-if="aiUsage.models.length" class="usage-model-list">
                  <li
                    v-for="model in aiUsage.models"
                    :key="model.model"
                  >
                    <div class="usage-model-header">
                      <strong>{{ model.model }}</strong>
                      <span>{{ formatCount(model.requests) }} 次</span>
                    </div>
                    <p class="usage-model-meta">
                      Token {{ formatCount(model.totalTokens) }} ·
                      预估 {{ formatUsd(model.costUsd) }} ·
                      实际 {{ formatUsd(model.actualCostUsd) }}
                    </p>
                  </li>
                </ul>
                <p v-else class="field-hint">这段时间没有按模型的调用记录。</p>
              </template>
            </section>

            <section class="detail-card">
              <div class="section-heading">
                <div>
                  <h3>绑定设备</h3>
                  <p>查看这个家庭已经连接的设备与在线状态。</p>
                </div>
                <button
                  type="button"
                  class="text-button"
                  :disabled="isDevicesLoading"
                  @click="reloadDevices"
                >
                  {{ isDevicesLoading ? '正在更新…' : '更新状态' }}
                </button>
              </div>

              <div v-if="isDevicesLoading" class="inline-state">
                正在读取绑定设备…
              </div>
              <div v-else-if="deviceError" class="inline-state error">
                {{ deviceError.message }}
                <button type="button" class="text-button" @click="reloadDevices">
                  重新加载设备
                </button>
              </div>
              <div v-else-if="selectedDevices.length === 0" class="empty-section">
                <p>这个家长还没有绑定设备。家长端连接设备后会自动显示在这里。</p>
              </div>
              <ul v-else class="device-list">
                <li v-for="device in selectedDevices" :key="device.deviceId">
                  <div class="device-list-header">
                    <strong>{{ device.deviceName || '未命名设备' }}</strong>
                    <span
                      :class="[
                        'status',
                        deviceStatusClass(device.runtime?.isOnline ?? null),
                      ]"
                    >
                      {{ deviceStatusLabel(device.runtime?.isOnline ?? null) }}
                    </span>
                  </div>
                  <p class="device-meta">
                    {{ device.hardwareModel || '初芽' }} ·
                    {{ device.firmwareVersion || '版本未知' }}
                  </p>
                  <p v-if="device.capabilities.length" class="device-meta">
                    {{ device.capabilities.map(capabilityLabel).join('、') }}
                  </p>
                  <p class="device-meta">
                    绑定于 {{ formatDate(device.boundAt) }} ·
                    最近上报 {{ formatTime(device.runtime?.receivedAt ?? '') }}
                  </p>
                </li>
              </ul>
            </section>

            <section class="detail-card">
              <div class="section-heading">
                <div>
                  <h3>账号安全</h3>
                  <p>重置后，家长当前在所有设备上的登录会失效。</p>
                </div>
              </div>
              <div class="security-row">
                <div>
                  <strong>登录密码</strong>
                  <p>家长可以用手机号和密码登录应用。</p>
                </div>
                <button
                  type="button"
                  :disabled="
                    store.isResettingPassword ||
                    store.isUpdatingProfile ||
                    store.isUpdatingAiAccount ||
                    store.isSubmitting ||
                    store.isLoading
                  "
                  @click="openPasswordReset"
                >
                  重置密码
                </button>
              </div>
              <p v-if="feedbackBySection.password.message" class="success-message">
                {{ feedbackBySection.password.message }}
              </p>
              <p
                v-if="feedbackBySection.password.error"
                class="error-message"
              >
                {{ feedbackBySection.password.error.message }}
              </p>
            </section>
          </div>

          <footer class="drawer-footer">
            <button type="button" class="secondary" @click="closeFamily">
              关闭
            </button>
          </footer>
        </aside>
      </div>
    </Transition>

    <Transition name="modal">
      <div
        v-if="isPasswordResetOpen && selectedFamily"
        class="dialog-backdrop"
        @click.self="closePasswordReset"
      >
        <form
          class="dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="reset-password-title"
          @submit.prevent="resetPassword"
        >
          <h2 id="reset-password-title">重置家长密码</h2>
          <p class="dialog-hint">
            重置后该家长需要重新登录。请通过可靠方式把新密码交给家长。
          </p>
          <label>
            <span>新密码</span>
            <input
              v-model="passwordForm.password"
              type="password"
              minlength="8"
              required
              autocomplete="new-password"
              placeholder="至少 8 位"
            />
          </label>
          <label>
            <span>再次输入新密码</span>
            <input
              v-model="passwordForm.confirmation"
              type="password"
              minlength="8"
              required
              autocomplete="new-password"
              placeholder="再次输入新密码"
            />
          </label>
          <div class="dialog-actions">
            <button
              type="button"
              class="secondary"
              :disabled="store.isResettingPassword"
              @click="closePasswordReset"
            >
              取消
            </button>
            <button
              type="submit"
              class="primary"
              :disabled="
                store.isResettingPassword ||
                store.isUpdatingProfile ||
                store.isUpdatingAiAccount ||
                store.isSubmitting
              "
            >
              {{ store.isResettingPassword ? '正在重置…' : '重置密码' }}
            </button>
          </div>
        </form>
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
  margin-bottom: 22px;
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

h1,
h2,
h3 {
  margin: 0;
  color: var(--sprout-text);
}

h1 {
  font-size: 28px;
}

h2 {
  font-size: 22px;
}

h3 {
  font-size: 17px;
}

.page-description,
.drawer-subtitle,
.section-heading p,
.security-row p {
  margin: 8px 0 0;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

.header-actions,
.dialog-actions,
.section-actions,
.model-picker-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

button {
  min-height: 38px;
  padding: 0 16px;
  border: 1px solid #e9a5b8;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: #c94175;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

button:not(:disabled):hover {
  border-color: #d94f83;
  background: #fff7fa;
  box-shadow: 0 8px 18px rgb(217 79 131 / 10%);
}

button.primary {
  border-color: #d94f83;
  background: #d94f83;
  color: #ffffff;
}

button.primary:not(:disabled):hover {
  background: #c94175;
}

button:disabled {
  cursor: wait;
  opacity: 0.55;
}

.link-button,
.text-button {
  min-height: auto;
  padding: 2px 0;
  border: 0;
  border-radius: 0;
  background: transparent;
  color: #c94175;
  box-shadow: none;
}

.link-button:not(:disabled):hover,
.text-button:not(:disabled):hover {
  border: 0;
  background: transparent;
  color: #a82d5a;
  box-shadow: none;
}

.table-shell,
.state-panel {
  overflow: hidden;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 16px;
  border-top: 1px solid #f7d9e2;
  background: #fffbfc;
}

.pagination-empty {
  width: 100%;
}

.empty-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 10px;
}

.pagination p,
.pagination span {
  margin: 0;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.pagination-controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.pagination-controls label {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.pagination-controls select {
  min-height: 38px;
  padding: 0 10px;
  border: 1px solid #f0bdcb;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: var(--sprout-text);
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
  white-space: nowrap;
}

th {
  color: #6b4f5a;
  font-size: 13px;
}

td {
  color: #4a2e3b;
}

tbody tr:last-child td {
  border-bottom: 0;
}

tbody tr {
  transition: background-color var(--sprout-duration-fast) ease;
}

tbody tr:hover {
  background: #fff8fa;
}

.parent-name,
.parent-meta {
  display: block;
}

.parent-name {
  font-weight: 600;
}

.parent-meta,
.device-meta {
  margin-top: 3px;
  color: #6b4f5a;
  font-size: 12px;
}

.status {
  display: inline-flex;
  padding: 4px 10px;
  border-radius: 999px;
  background: #fff3cd;
  color: #755600;
  font-size: 13px;
}

.status.active,
.status.online {
  background: #e7f8ee;
  color: #1d6b3f;
}

.status.suspended,
.status.disabled,
.status.missing,
.status.offline {
  background: #fff0f2;
  color: #a12b4a;
}

.status.unknown {
  background: #fff3cd;
  color: #755600;
}

.state-panel {
  display: flex;
  min-height: 132px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 28px;
  color: #6b4f5a;
  text-align: center;
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
  color: #b3261e;
}

.success-message {
  color: #1d6b3f;
}

.dialog-backdrop,
.drawer-backdrop {
  position: fixed;
  inset: 0;
  z-index: 30;
  background: rgb(74 46 59 / 35%);
  backdrop-filter: blur(3px);
}

.dialog-backdrop {
  display: grid;
  place-items: center;
  padding: 20px;
}

.dialog {
  display: grid;
  width: min(100%, 480px);
  max-height: min(760px, calc(100vh - 40px));
  gap: 16px;
  overflow: auto;
  padding: 28px;
  border: 1px solid #f0bdcb;
  border-radius: 26px;
  background: #ffffff;
  box-shadow: 0 28px 72px rgb(74 46 59 / 22%);
}

.dialog-hint {
  margin: -6px 0 0;
  color: #6b4f5a;
  line-height: 1.6;
}

.dialog label,
.detail-card label {
  display: grid;
  gap: 7px;
  color: #4a2e3b;
  font-weight: 600;
}

.dialog input,
.dialog select,
.detail-card input,
.detail-card select {
  min-height: 44px;
  padding: 0 14px;
  border: 1px solid #f0bdcb;
  border-radius: 14px;
  background: #fff8fa;
  color: #4a2e3b;
  font: inherit;
  transition:
    border-color var(--sprout-duration-fast) ease,
    box-shadow var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease;
}

.dialog input:focus,
.dialog select:focus,
.detail-card input:focus,
.detail-card select:focus {
  border-color: #d94f83;
  background: #ffffff;
  box-shadow: 0 0 0 3px rgb(217 79 131 / 12%);
  outline: none;
}

.dialog-actions {
  justify-content: flex-end;
  margin-top: 6px;
}

.drawer-backdrop {
  display: flex;
  justify-content: flex-end;
}

.family-drawer {
  display: grid;
  width: min(100%, 760px);
  height: 100%;
  grid-template-rows: auto minmax(0, 1fr) auto;
  border-left: 1px solid #f0bdcb;
  background:
    radial-gradient(circle at 96% 2%, rgb(255 224 102 / 14%), transparent 16rem),
    #fffbfc;
  box-shadow: -24px 0 64px rgb(74 46 59 / 18%);
}

.drawer-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  padding: 26px 28px 20px;
  border-bottom: 1px solid #f7d9e2;
  background: rgb(255 255 255 / 88%);
}

.icon-button {
  width: 38px;
  min-width: 38px;
  height: 38px;
  min-height: 38px;
  padding: 0;
  border-radius: 50%;
  font-size: 22px;
  line-height: 1;
}

.drawer-body {
  display: grid;
  align-content: start;
  gap: 16px;
  overflow-y: auto;
  padding: 22px 28px 28px;
}

.detail-card {
  display: grid;
  gap: 18px;
  padding: 20px;
  border: 1px solid var(--sprout-outline);
  border-radius: 20px;
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.section-actions {
  justify-content: flex-end;
}

.ai-editor {
  display: grid;
  gap: 18px;
}

.model-picker {
  display: grid;
  gap: 12px;
}

.model-picker-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
}

.field-label {
  color: var(--sprout-text);
  font-weight: 700;
}

.field-hint {
  margin: 5px 0 0;
  color: var(--sprout-text-muted);
  font-size: 13px;
  line-height: 1.5;
}

.model-list,
.device-list {
  display: grid;
  gap: 9px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.model-option {
  display: flex !important;
  grid-template-columns: none !important;
  align-items: center;
  gap: 11px !important;
  padding: 11px 13px;
  border: 1px solid #f7d9e2;
  border-radius: 14px;
  background: #fffbfc;
  cursor: pointer;
  transition:
    border-color var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease,
    transform var(--sprout-duration-fast) var(--sprout-ease-out);
}

.model-option:hover {
  border-color: #e9a5b8;
  background: #fff7fa;
  transform: translateX(2px);
}

.model-option input {
  width: 18px;
  min-height: auto;
  height: 18px;
  flex: 0 0 auto;
  padding: 0;
  accent-color: #d94f83;
}

.model-option-main {
  display: flex;
  min-width: 0;
  flex: 1;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.model-option-main strong {
  overflow-wrap: anywhere;
}

.model-name {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 7px;
}

.model-name em {
  flex: 0 0 auto;
  padding: 2px 7px;
  border-radius: 999px;
  background: #fff3cd;
  color: #755600;
  font-size: 11px;
  font-style: normal;
  font-weight: 700;
}

.model-option-main small {
  flex: 0 0 auto;
  color: #6b4f5a;
}

.inline-state,
.empty-section {
  padding: 14px 16px;
  border-radius: 15px;
  background: #fff7fa;
  color: #6b4f5a;
  line-height: 1.6;
}

.inline-state.error {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  background: #fff0f2;
  color: #a12b4a;
}

.empty-section {
  display: grid;
  justify-items: start;
  gap: 12px;
}

.empty-section p {
  margin: 0;
}

.device-list li {
  display: grid;
  gap: 3px;
  padding: 14px 15px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #fffbfc;
}

.device-list-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.usage-window {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.usage-totals {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 10px;
  margin: 0;
}

.usage-totals > div {
  display: grid;
  gap: 4px;
  padding: 12px 14px;
  border: 1px solid #f7d9e2;
  border-radius: 14px;
  background: #fffbfc;
}

.usage-totals dt {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.usage-totals dd {
  margin: 0;
  color: var(--sprout-text);
  font-size: 16px;
  font-weight: 700;
  overflow-wrap: anywhere;
}

.usage-model-list {
  display: grid;
  gap: 9px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.usage-model-list li {
  display: grid;
  gap: 3px;
  padding: 12px 14px;
  border: 1px solid #f7d9e2;
  border-radius: 14px;
  background: #fffbfc;
}

.usage-model-header {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.usage-model-header strong {
  overflow-wrap: anywhere;
}

.usage-model-header span {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.usage-model-meta {
  margin: 0;
  color: #6b4f5a;
  font-size: 13px;
}

.security-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
}

.security-row p {
  margin-top: 4px;
}

.drawer-footer {
  display: flex;
  justify-content: flex-end;
  padding: 16px 28px;
  border-top: 1px solid #f7d9e2;
  background: rgb(255 255 255 / 92%);
}

.drawer-enter-active,
.drawer-leave-active {
  transition: opacity var(--sprout-duration-base) ease;
}

.drawer-enter-active .family-drawer,
.drawer-leave-active .family-drawer {
  transition: transform var(--sprout-duration-slow) var(--sprout-ease-out);
}

.drawer-enter-from,
.drawer-leave-to {
  opacity: 0;
}

.drawer-enter-from .family-drawer,
.drawer-leave-to .family-drawer {
  transform: translateX(24px);
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 900px) {
  .page-header {
    display: grid;
  }

  .table-shell {
    overflow-x: auto;
  }

  table {
    min-width: 1120px;
  }

  .form-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 720px) {
  .pagination {
    align-items: stretch;
    flex-direction: column;
  }

  .pagination-controls {
    justify-content: space-between;
  }
}

@media (max-width: 640px) {
  .dialog {
    gap: 14px;
    padding: 22px 18px;
    border-radius: 22px;
  }

  .drawer-header,
  .drawer-body,
  .drawer-footer {
    padding-right: 18px;
    padding-left: 18px;
  }

  .model-picker-heading,
  .section-heading,
  .security-row {
    align-items: stretch;
    flex-direction: column;
  }

  .security-row button,
  .section-actions button {
    width: 100%;
  }
}
</style>
