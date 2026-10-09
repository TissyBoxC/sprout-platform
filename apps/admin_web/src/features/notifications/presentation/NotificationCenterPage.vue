<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import type {
  AdminNotification,
  CreateAdminNotificationInput,
  NotificationAudience,
  NotificationCategory,
  NotificationSeverity,
  NotificationSource,
} from '@/api/adminNotifications'
import {
  useNotificationCenterStore,
  type NotificationFormState,
} from '@/features/notifications/application/notificationCenterStore'

const store = useNotificationCenterStore()
const showComposer = ref(false)
const pendingDelete = ref<AdminNotification | null>(null)
const form = reactive<NotificationFormState>(emptyForm())

const categoryOptions: Array<{ value: NotificationCategory; label: string }> = [
  { value: 'system_announcement', label: '系统公告' },
  { value: 'content_release', label: '内容更新' },
  { value: 'service_update', label: '服务更新' },
  { value: 'device_status', label: '设备状态' },
  { value: 'account_security', label: '账号安全' },
  { value: 'usage_report', label: '使用报告' },
]

const severityOptions: Array<{ value: NotificationSeverity; label: string }> = [
  { value: 'info', label: '提示' },
  { value: 'success', label: '好消息' },
  { value: 'warning', label: '注意事项' },
  { value: 'critical', label: '重要提醒' },
]

const audienceOptions: Array<{ value: NotificationAudience; label: string }> = [
  { value: 'all_parents', label: '全部家长' },
  { value: 'parent', label: '指定家长' },
  { value: 'family_devices', label: '家庭设备' },
]

const channelOptions: Array<{ value: string; label: string }> = [
  { value: 'in_app', label: '家长端消息' },
  { value: 'push', label: '手机通知' },
  { value: 'device', label: '设备屏幕' },
]

const categoryFilterOptions = [
  { value: 'all', label: '全部类型' },
  ...categoryOptions,
]
const audienceFilterOptions = [
  { value: 'all', label: '全部对象' },
  ...audienceOptions,
]
const sourceFilterOptions: Array<{ value: NotificationSource | 'all'; label: string }> = [
  { value: 'all', label: '全部来源' },
  { value: 'administrator', label: '品牌运营' },
  { value: 'guardian', label: '家长消息' },
  { value: 'system', label: '系统自动' },
]

const hasStats = computed(() => store.stats !== null)
const selectedFamilyName = computed(
  () =>
    store.families.find(
      (family) => family.parentAccountId === form.parentAccountId,
    )?.displayName ?? '',
)

onMounted(() => {
  void store.load()
})

function emptyForm(): NotificationFormState {
  return {
    category: 'system_announcement',
    severity: 'info',
    title: '',
    body: '',
    actionPath: '',
    actionLabel: '',
    audience: 'all_parents',
    channels: ['in_app'],
    parentAccountId: '',
    deviceId: '',
    displayDurationSeconds: 0,
    expiresAt: '',
  }
}

function openComposer(): void {
  Object.assign(form, emptyForm())
  showComposer.value = true
  void store.loadFamilies()
}

function closeComposer(): void {
  showComposer.value = false
}

function resetFilters(): void {
  void store.setFilters({
    category: 'all',
    audience: 'all',
    source: 'all',
    query: '',
    page: 1,
  })
}

function toggleChannel(channel: string): void {
  const existing = form.channels.includes(channel)
  if (existing) {
    form.channels = form.channels.filter((item) => item !== channel)
  } else {
    form.channels = [...form.channels, channel]
  }
}

const formError = computed(() => {
  if (form.title.trim().length === 0) {
    return '请填写通知标题'
  }
  if (form.title.trim().length > 60) {
    return '通知标题不要超过 60 个字'
  }
  if (form.body.trim().length === 0) {
    return '请填写通知内容'
  }
  if (form.body.trim().length > 1000) {
    return '通知内容不要超过 1000 个字'
  }
  if (form.channels.length === 0) {
    return '请至少选择一种送达方式'
  }
  if (form.audience === 'parent' && form.parentAccountId.trim() === '') {
    return '请选择要接收通知的家长'
  }
  if (form.audience === 'family_devices' && form.deviceId.trim() === '') {
    return '请选择要送达的设备'
  }
  return ''
})

async function submit(): Promise<void> {
  if (formError.value !== '') {
    return
  }
  const input: CreateAdminNotificationInput = {
    category: form.category,
    severity: form.severity,
    title: form.title,
    body: form.body,
    actionPath: form.actionPath,
    actionLabel: form.actionLabel,
    audience: form.audience,
    channels: form.channels as CreateAdminNotificationInput['channels'],
    parentAccountId: form.parentAccountId,
    deviceId: form.deviceId,
    displayDurationSeconds: form.displayDurationSeconds,
    expiresAt: form.expiresAt,
  }
  const succeeded = await store.create(input)
  if (succeeded) {
    showComposer.value = false
  }
}

async function confirmDelete(): Promise<void> {
  const target = pendingDelete.value
  if (target === null) {
    return
  }
  const succeeded = await store.remove(target.id)
  if (succeeded) {
    pendingDelete.value = null
  }
}

function categoryLabel(value: NotificationCategory): string {
  return categoryOptions.find((option) => option.value === value)?.label ?? '系统公告'
}

function severityLabel(value: NotificationSeverity): string {
  return severityOptions.find((option) => option.value === value)?.label ?? '提示'
}

function audienceLabel(value: NotificationAudience): string {
  return audienceOptions.find((option) => option.value === value)?.label ?? '全部家长'
}

function sourceLabel(value: NotificationSource): string {
  return (
    {
      administrator: '品牌运营',
      guardian: '家长消息',
      system: '系统自动',
    }[value] ?? '系统自动'
  )
}

function channelLabel(value: string): string {
  return channelOptions.find((option) => option.value === value)?.label ?? value
}

function formatTime(value: string): string {
  if (value === '') {
    return '—'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}
</script>

<template>
  <section class="notification-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">家长守护</p>
        <h1>消息通知</h1>
        <p class="page-description">
          向家长发布公告、提醒和内容更新，并查看每条通知的送达情况。家长看到的文字都会经过内容安全审核。
        </p>
      </div>
      <div class="header-actions">
        <button type="button" class="ghost-button" :disabled="store.isLoading" @click="store.load">
          {{ store.isLoading ? '正在刷新…' : '刷新' }}
        </button>
        <button type="button" class="primary-button" @click="openComposer">
          发布通知
        </button>
      </div>
    </header>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="success-message" role="status">
        {{ store.lastMessage }}
      </p>
    </Transition>

    <section v-if="hasStats && store.stats" class="metric-grid" aria-label="通知概览">
      <article>
        <span>全部通知</span>
        <strong>{{ store.stats.total }}</strong>
        <small>条</small>
      </article>
      <article>
        <span>群发通知</span>
        <strong>{{ store.stats.broadcastCount }}</strong>
        <small>条</small>
      </article>
      <article>
        <span>等待送达</span>
        <strong>{{ store.stats.pendingDeliveryCount }}</strong>
        <small>条</small>
      </article>
      <article :class="{ attention: store.stats.failedDeliveryCount > 0 }">
        <span>送达失败</span>
        <strong>{{ store.stats.failedDeliveryCount }}</strong>
        <small>条</small>
      </article>
    </section>

    <section class="filter-bar">
      <label class="search-field">
        <span class="visually-hidden">搜索通知</span>
        <input
          :value="store.filters.query"
          type="search"
          placeholder="搜索标题或内容"
          @input="store.setFilters({ query: ($event.target as HTMLInputElement).value })"
        />
      </label>
      <select
        :value="store.filters.category"
        aria-label="按类型筛选"
        @change="store.setFilters({ category: ($event.target as HTMLSelectElement).value as NotificationCategory | 'all' })"
      >
        <option v-for="option in categoryFilterOptions" :key="option.value" :value="option.value">
          {{ option.label }}
        </option>
      </select>
      <select
        :value="store.filters.audience"
        aria-label="按接收对象筛选"
        @change="store.setFilters({ audience: ($event.target as HTMLSelectElement).value as NotificationAudience | 'all' })"
      >
        <option v-for="option in audienceFilterOptions" :key="option.value" :value="option.value">
          {{ option.label }}
        </option>
      </select>
      <select
        :value="store.filters.source"
        aria-label="按来源筛选"
        @change="store.setFilters({ source: ($event.target as HTMLSelectElement).value as NotificationSource | 'all' })"
      >
        <option v-for="option in sourceFilterOptions" :key="option.value" :value="option.value">
          {{ option.label }}
        </option>
      </select>
      <button type="button" class="ghost-button" @click="resetFilters">重置</button>
    </section>

    <div v-if="store.isLoading && !store.hasNotifications" class="state-panel">
      <span class="state-spinner" aria-hidden="true"></span>
      正在读取通知…
    </div>

    <div v-else-if="store.error" class="state-panel error-state">
      <span class="state-icon" aria-hidden="true">!</span>
      <div>
        <strong>{{ store.error.message }}</strong>
        <p>通知列表暂时无法读取，可以重新尝试。</p>
      </div>
      <button type="button" class="text-button" @click="store.loadPage">重新加载</button>
    </div>

    <div v-else-if="!store.hasNotifications" class="state-panel empty-state">
      <span class="state-icon" aria-hidden="true">☆</span>
      <div>
        <strong>还没有发布任何通知</strong>
        <p>发布第一条公告后，家长端的消息列表会同步显示。</p>
      </div>
      <button type="button" class="primary-button" @click="openComposer">发布通知</button>
    </div>

    <template v-else>
      <section class="list-card">
        <div class="table-scroll">
          <table>
            <thead>
              <tr>
                <th>通知</th>
                <th>接收对象</th>
                <th>送达方式</th>
                <th>送达情况</th>
                <th>发布时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="notification in store.notifications" :key="notification.id">
                <td>
                  <div class="title-cell">
                    <span :class="['severity-dot', notification.severity]"></span>
                    <div>
                      <strong>{{ notification.title }}</strong>
                      <small>{{ categoryLabel(notification.category) }} · {{ severityLabel(notification.severity) }}</small>
                    </div>
                  </div>
                  <p class="body-preview">{{ notification.body }}</p>
                </td>
                <td>
                  <span>{{ audienceLabel(notification.audience) }}</span>
                  <small>{{ sourceLabel(notification.source) }}</small>
                </td>
                <td>
                  <span v-if="notification.channels.length === 0">家长端消息</span>
                  <span v-else>{{ notification.channels.map(channelLabel).join('、') }}</span>
                </td>
                <td>
                  <span>{{ notification.deliveryDelivered }} / {{ notification.deliveryTotal }}</span>
                  <small v-if="notification.deliveryFailed > 0" class="failed-text">
                    {{ notification.deliveryFailed }} 条送达失败
                  </small>
                  <small v-else>{{ notification.deliveryRead }} 条已读</small>
                </td>
                <td>{{ formatTime(notification.publishAt) }}</td>
                <td>
                  <button
                    type="button"
                    class="text-button danger"
                    :disabled="store.deletingId === notification.id"
                    @click="pendingDelete = notification"
                  >
                    {{ store.deletingId === notification.id ? '正在删除…' : '删除' }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="pagination">
          <span>共 {{ store.total }} 条 · 第 {{ store.filters.page }} / {{ store.totalPages }} 页</span>
          <div>
            <button
              type="button"
              class="ghost-button"
              :disabled="!store.canGoPrevious"
              @click="store.goToPage(store.filters.page - 1)"
            >
              上一页
            </button>
            <button
              type="button"
              class="ghost-button"
              :disabled="!store.canGoNext"
              @click="store.goToPage(store.filters.page + 1)"
            >
              下一页
            </button>
          </div>
        </div>
      </section>
    </template>

    <Transition name="fade">
      <div v-if="showComposer" class="modal-backdrop" @click.self="closeComposer">
        <div class="modal" role="dialog" aria-modal="true" aria-label="发布通知">
          <header class="modal-header">
            <h2>发布通知</h2>
            <button type="button" class="icon-button" aria-label="关闭" @click="closeComposer">×</button>
          </header>
          <form class="composer" @submit.prevent="submit">
            <div class="field-grid">
              <label>
                <span>通知类型</span>
                <select v-model="form.category">
                  <option v-for="option in categoryOptions" :key="option.value" :value="option.value">
                    {{ option.label }}
                  </option>
                </select>
              </label>
              <label>
                <span>重要程度</span>
                <select v-model="form.severity">
                  <option v-for="option in severityOptions" :key="option.value" :value="option.value">
                    {{ option.label }}
                  </option>
                </select>
              </label>
            </div>

            <label>
              <span>标题</span>
              <input v-model="form.title" type="text" maxlength="60" placeholder="例如：新故事已经上线" />
            </label>

            <label>
              <span>内容</span>
              <textarea v-model="form.body" rows="4" maxlength="1000" placeholder="写清楚发生了什么、家长需要做什么"></textarea>
            </label>

            <div class="field-grid">
              <label>
                <span>跳转页面（可选）</span>
                <input v-model="form.actionPath" type="text" placeholder="例如 /me/usage-reports" />
              </label>
              <label>
                <span>按钮文字（可选）</span>
                <input v-model="form.actionLabel" type="text" maxlength="24" placeholder="例如 查看详情" />
              </label>
            </div>

            <fieldset class="choice-group">
              <legend>接收对象</legend>
              <div class="chip-row">
                <button
                  v-for="option in audienceOptions"
                  :key="option.value"
                  type="button"
                  :class="['chip', { selected: form.audience === option.value }]"
                  @click="form.audience = option.value"
                >
                  {{ option.label }}
                </button>
              </div>
            </fieldset>

            <label v-if="form.audience === 'parent'">
              <span>指定家长</span>
              <select v-model="form.parentAccountId">
                <option value="">请选择家长</option>
                <option
                  v-for="family in store.families"
                  :key="family.parentAccountId"
                  :value="family.parentAccountId"
                >
                  {{ family.displayName }} · {{ family.phone }}
                </option>
              </select>
              <small v-if="store.isLoadingFamilies">正在读取家长账号…</small>
              <small v-else-if="store.families.length === 0">还没有家长账号。</small>
            </label>

            <label v-if="form.audience === 'family_devices'">
              <span>设备编号</span>
              <input v-model="form.deviceId" type="text" placeholder="填写家长绑定的设备编号" />
              <small v-if="selectedFamilyName">将发送给 {{ selectedFamilyName }} 家庭下的设备。</small>
            </label>

            <div v-if="form.audience === 'family_devices'" class="field-grid">
              <label>
                <span>设备显示时长（秒）</span>
                <input v-model.number="form.displayDurationSeconds" type="number" min="0" max="300" />
              </label>
              <label>
                <span>失效时间（可选）</span>
                <input v-model="form.expiresAt" type="datetime-local" />
              </label>
            </div>

            <fieldset class="choice-group">
              <legend>送达方式</legend>
              <div class="chip-row">
                <button
                  v-for="option in channelOptions"
                  :key="option.value"
                  type="button"
                  :class="['chip', { selected: form.channels.includes(option.value) }]"
                  @click="toggleChannel(option.value)"
                >
                  {{ option.label }}
                </button>
              </div>
            </fieldset>

            <p v-if="formError !== ''" class="form-hint">{{ formError }}</p>
            <p v-else class="form-hint hint-muted">发布后家长会立刻在家长端看到这条通知。</p>

            <footer class="modal-footer">
              <button type="button" class="ghost-button" @click="closeComposer">取消</button>
              <button
                type="submit"
                class="primary-button"
                :disabled="store.isSubmitting || formError !== ''"
              >
                {{ store.isSubmitting ? '正在发布…' : '发布通知' }}
              </button>
            </footer>
          </form>
        </div>
      </div>
    </Transition>

    <Transition name="fade">
      <div v-if="pendingDelete" class="modal-backdrop" @click.self="pendingDelete = null">
        <div class="modal narrow" role="dialog" aria-modal="true" aria-label="删除通知">
          <header class="modal-header">
            <h2>删除这条通知？</h2>
          </header>
          <p class="confirm-copy">
            {{ pendingDelete.title }}<br />
            删除后家长端的消息列表里也不会再显示，且无法恢复。
          </p>
          <footer class="modal-footer">
            <button type="button" class="ghost-button" @click="pendingDelete = null">保留</button>
            <button
              type="button"
              class="danger-button"
              :disabled="store.deletingId !== ''"
              @click="confirmDelete"
            >
              {{ store.deletingId !== '' ? '正在删除…' : '删除通知' }}
            </button>
          </footer>
        </div>
      </div>
    </Transition>
  </section>
</template>

<style scoped>
.notification-page {
  display: grid;
  gap: 20px;
}

.page-header,
.metric-grid article,
.filter-bar,
.list-card,
.state-panel,
.modal {
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
.state-panel p {
  margin: 8px 0 0;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

.header-actions {
  display: flex;
  gap: 10px;
}

.primary-button,
.ghost-button,
.danger-button {
  min-height: 40px;
  padding: 0 16px;
  border-radius: var(--sprout-radius-control);
  font: inherit;
  font-weight: 700;
  cursor: pointer;
  transition:
    transform var(--sprout-duration-fast) var(--sprout-ease-out),
    border-color var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease,
    box-shadow var(--sprout-duration-base) ease;
}

.primary-button {
  border: 0;
  background: #d94f83;
  color: #ffffff;
  box-shadow: 0 10px 20px rgb(217 79 131 / 20%);
}

.primary-button:hover {
  background: #c94175;
  box-shadow: 0 12px 24px rgb(217 79 131 / 26%);
}

.ghost-button {
  border: 1px solid #e9a5b8;
  background: #ffffff;
  color: #c94175;
}

.ghost-button:hover {
  border-color: #d94f83;
  background: #fff7fa;
}

.danger-button {
  border: 0;
  background: #c02b53;
  color: #ffffff;
}

.danger-button:hover {
  background: #a5244a;
}

.primary-button:disabled,
.ghost-button:disabled,
.danger-button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.primary-button:not(:disabled):active,
.ghost-button:not(:disabled):active,
.danger-button:not(:disabled):active {
  transform: scale(0.98);
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

.filter-bar {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) repeat(3, minmax(120px, auto)) auto;
  gap: 10px;
  align-items: center;
  padding: 14px 16px;
}

.filter-bar input,
.filter-bar select,
.composer input,
.composer select,
.composer textarea {
  width: 100%;
  min-height: 40px;
  padding: 9px 12px;
  border: 1px solid #f0bdcb;
  border-radius: 14px;
  background: #fffbfc;
  color: var(--sprout-text);
  font: inherit;
}

.filter-bar input:focus,
.filter-bar select:focus,
.composer input:focus,
.composer select:focus,
.composer textarea:focus {
  border-color: #e98aa9;
  outline: none;
  box-shadow: 0 0 0 3px rgb(233 138 169 / 18%);
}

.list-card {
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

.title-cell {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.severity-dot {
  width: 9px;
  height: 9px;
  margin-top: 6px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: #b7a3ac;
}

.severity-dot.success {
  background: #2e9e6a;
}

.severity-dot.warning {
  background: #e0a02a;
}

.severity-dot.critical {
  background: #d1425f;
}

.body-preview {
  margin: 8px 0 0;
  color: var(--sprout-text-muted);
  font-size: 13px;
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.failed-text {
  color: #b12848;
}

.pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-top: 1px solid #f7d9e2;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.pagination div {
  display: flex;
  gap: 8px;
}

.text-button {
  min-height: auto;
  padding: 2px 0;
  border: 0;
  background: transparent;
  box-shadow: none;
  color: #c94175;
  font: inherit;
  cursor: pointer;
}

.text-button.danger {
  color: #b12848;
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

.state-panel > div {
  text-align: left;
}

.state-panel strong {
  color: var(--sprout-text);
}

.state-panel .primary-button {
  margin-left: 8px;
}

.error-state strong {
  color: #a12b4a;
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

.state-spinner {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  border: 2px solid #f0bdcb;
  border-top-color: #d94f83;
  border-radius: 50%;
  animation: spin 700ms linear infinite;
}

.success-message {
  margin: 0;
  padding: 12px 16px;
  border: 1px solid #bfe6d0;
  border-radius: var(--sprout-radius-control);
  background: #f2fbf6;
  color: #1d6b3f;
}

.modal-backdrop {
  position: fixed;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgb(74 46 59 / 32%);
  backdrop-filter: blur(3px);
  z-index: 40;
}

.modal {
  width: min(640px, 100%);
  max-height: min(90vh, 880px);
  overflow: auto;
  padding: 0;
}

.modal.narrow {
  width: min(420px, 100%);
  padding: 22px;
}

.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 20px 22px;
  border-bottom: 1px solid #f7d9e2;
}

.modal.narrow .modal-header {
  padding: 0 0 10px;
  border-bottom: 0;
}

.icon-button {
  width: 34px;
  height: 34px;
  border: 1px solid #f0bdcb;
  border-radius: 12px;
  background: #ffffff;
  color: #c94175;
  font-size: 20px;
  line-height: 1;
  cursor: pointer;
}

.composer {
  display: grid;
  gap: 16px;
  padding: 22px;
}

.composer label {
  display: grid;
  gap: 6px;
}

.composer label > span,
.choice-group legend {
  color: var(--sprout-text);
  font-size: 13px;
  font-weight: 700;
}

.composer label > small {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.choice-group {
  border: 0;
  padding: 0;
  margin: 0;
}

.chip-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
}

.chip {
  min-height: 36px;
  padding: 0 14px;
  border: 1px solid #f0bdcb;
  border-radius: 999px;
  background: #ffffff;
  color: #6b4f5a;
  font: inherit;
  cursor: pointer;
  transition:
    transform var(--sprout-duration-fast) var(--sprout-ease-out),
    border-color var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease,
    color var(--sprout-duration-fast) ease;
}

.chip:hover {
  border-color: #e98aa9;
  color: #c94175;
}

.chip.selected {
  border-color: #d94f83;
  background: #fff0f4;
  color: #c94175;
  font-weight: 700;
}

.chip:active {
  transform: scale(0.97);
}

.form-hint {
  margin: 0;
  color: #b12848;
  font-size: 13px;
}

.hint-muted {
  color: var(--sprout-text-muted);
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

.confirm-copy {
  margin: 0 0 18px;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

.visually-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
}

.toast-enter-active,
.toast-leave-active,
.fade-enter-active,
.fade-leave-active {
  transition: opacity 180ms ease;
}

.toast-enter-from,
.toast-leave-to,
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
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

  .filter-bar {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .page-header,
  .filter-bar,
  .pagination,
  .modal-footer {
    flex-direction: column;
  }

  .page-header,
  .pagination {
    align-items: stretch;
  }

  .header-actions {
    width: 100%;
  }

  .header-actions button {
    flex: 1;
  }

  .filter-bar,
  .field-grid {
    grid-template-columns: 1fr;
  }

  .state-panel {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
