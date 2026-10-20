<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import type {
  UiTextDraftInput,
  UiTextEntry,
  UiTextPlatform,
  UiTextStatus,
  UiTextVersionRef,
  UiTextVersionSummary,
} from '@/api/adminUiText'
import {
  UI_TEXT_PAGE_SIZE,
  uiTextLanguageLabel,
  uiTextPlatformLabel,
  uiTextStatusLabel,
  useUiTextStore,
} from '@/features/ui_text/application/uiTextStore'
import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS } from '@/features/auth/domain/adminPermission'
import { canManageContent } from '@/features/content/domain/contentPermission'
import UiTextDraftFields, {
  type UiTextDraftForm,
  type UiTextEntryForm,
} from '@/features/ui_text/presentation/UiTextDraftFields.vue'

const languages = ['zh-CN', 'en-US']
const platforms: UiTextPlatform[] = ['parent_app', 'admin_web', 'device_firmware']
const statuses: UiTextStatus[] = ['draft', 'in_review', 'published', 'withdrawn']

const store = useUiTextStore()
const authStore = useAuthStore()
const canManage = computed(() =>
  canManageContent(authStore.can(ADMIN_PERMISSIONS.contentReview)),
)

const isCreateOpen = ref(false)
const isEditOpen = ref(false)
const publishTarget = ref<UiTextVersionSummary | null>(null)
const publishKeyId = ref('')
const publishSignature = ref('')
const rejectTarget = ref<UiTextVersionSummary | null>(null)
const rejectReason = ref('')
const keywordInput = ref('')

const createForm = reactive<UiTextDraftForm>(emptyDraftForm())
const editForm = reactive<UiTextDraftForm>(emptyDraftForm())

const activeSummary = computed<UiTextVersionSummary | null>(() => {
  const selectedRef = store.selectedRef
  if (selectedRef === null) {
    return null
  }
  const matched = store.versions.find((item) => sameVersion(item, selectedRef))
  if (matched !== undefined) {
    return matched
  }
  const detail = store.selectedDetail
  if (detail === null) {
    return null
  }
  return {
    package: detail.package,
    entryCount: detail.entries.length,
    isPublished: detail.package.status === 'published',
    isNewest: false,
    publishedFor: false,
  }
})

onMounted(() => {
  void store.load()
})

function openCreate(): void {
  if (!canManage.value) {
    return
  }
  store.clearMessages()
  Object.assign(createForm, emptyDraftForm())
  isCreateOpen.value = true
}

function closeCreate(): void {
  isCreateOpen.value = false
}

async function submitCreate(): Promise<void> {
  if (!canManage.value) {
    return
  }
  const result = await store.createDraft(toDraftInput(createForm))
  if (result.succeeded) {
    isCreateOpen.value = false
  }
}

function openEdit(): void {
  if (!canManage.value) {
    return
  }
  const detail = store.selectedDetail
  if (detail === null || detail.package.status !== 'draft') {
    return
  }
  store.clearMessages()
  editForm.packageId = detail.package.packageId
  editForm.language = detail.package.language
  editForm.platform = detail.package.platform
  editForm.minClientVersion = detail.package.minClientVersion
  editForm.entries = detail.entries.map((entry) => ({
    uiTextKey: entry.uiTextKey,
    value: entry.value,
    maxLength: entry.maxLength,
    variablesText: entry.variables.join(', '),
  }))
  if (editForm.entries.length === 0) {
    editForm.entries.push(emptyEntryForm())
  }
  isEditOpen.value = true
}

async function submitEdit(): Promise<void> {
  if (!canManage.value) {
    return
  }
  const selectedRef = store.selectedRef
  if (selectedRef === null) {
    return
  }
  const result = await store.updateDraft(selectedRef, toDraftInput(editForm))
  if (result.succeeded) {
    isEditOpen.value = false
  }
}

function addEntry(form: UiTextDraftForm): void {
  form.entries.push(emptyEntryForm())
}

function removeEntry(form: UiTextDraftForm, index: number): void {
  if (form.entries.length <= 1) {
    return
  }
  form.entries.splice(index, 1)
}

async function submitVersion(summary: UiTextVersionSummary): Promise<void> {
  if (!canManage.value) {
    return
  }
  await store.submitVersion(refOf(summary))
}

async function approveVersion(summary: UiTextVersionSummary): Promise<void> {
  if (!canManage.value) {
    return
  }
  await store.approveVersion(refOf(summary))
}

function openReject(summary: UiTextVersionSummary): void {
  if (!canManage.value) {
    return
  }
  rejectTarget.value = summary
  rejectReason.value = ''
}

async function confirmReject(): Promise<void> {
  if (!canManage.value) {
    return
  }
  const target = rejectTarget.value
  if (target === null || rejectReason.value.trim() === '') {
    return
  }
  const result = await store.rejectVersion(refOf(target), rejectReason.value)
  if (result.succeeded) {
    rejectTarget.value = null
  }
}

function openPublish(summary: UiTextVersionSummary): void {
  if (!canManage.value) {
    return
  }
  publishTarget.value = summary
  publishKeyId.value = summary.package.signatureKeyId
  publishSignature.value = ''
}

async function confirmPublish(): Promise<void> {
  if (!canManage.value) {
    return
  }
  const target = publishTarget.value
  if (target === null) {
    return
  }
  const result = await store.publishVersion(
    refOf(target),
    publishKeyId.value,
    publishSignature.value,
  )
  if (result.succeeded) {
    publishTarget.value = null
  }
}

async function withdrawVersion(summary: UiTextVersionSummary): Promise<void> {
  if (!canManage.value) {
    return
  }
  await store.withdrawVersion(refOf(summary))
}

function applyFilters(): void {
  store.setFilters({ keyword: keywordInput.value })
  void store.load()
}

function resetFilters(): void {
  keywordInput.value = ''
  store.resetFilters()
  void store.load()
}

function changePage(delta: number): void {
  store.setPage(store.page + delta)
  void store.load()
}

function selectVersion(summary: UiTextVersionSummary): void {
  void store.openVersion(refOf(summary))
}

function emptyDraftForm(): UiTextDraftForm {
  return {
    packageId: '',
    language: 'zh-CN',
    platform: 'parent_app',
    minClientVersion: '',
    entries: [emptyEntryForm()],
  }
}

function emptyEntryForm(): UiTextEntryForm {
  return { uiTextKey: '', value: '', maxLength: 2000, variablesText: '' }
}

function toDraftInput(form: UiTextDraftForm): UiTextDraftInput {
  return {
    packageId: form.packageId.trim(),
    language: form.language.trim(),
    platform: form.platform,
    minClientVersion: form.minClientVersion.trim(),
    entries: form.entries.filter((entry) => entry.uiTextKey.trim() !== '').map(toEntry),
  }
}

function toEntry(entry: UiTextEntryForm): UiTextEntry {
  return {
    uiTextKey: entry.uiTextKey.trim(),
    value: entry.value,
    maxLength: entry.maxLength,
    variables: entry.variablesText
      .split(',')
      .map((value) => value.trim())
      .filter((value) => value !== ''),
  }
}

function refOf(summary: UiTextVersionSummary): UiTextVersionRef {
  return {
    packageId: summary.package.packageId,
    language: summary.package.language,
    platform: summary.package.platform,
    packageVersion: summary.package.packageVersion,
  }
}

function sameVersion(summary: UiTextVersionSummary, ref: UiTextVersionRef): boolean {
  return (
    summary.package.packageId === ref.packageId &&
    summary.package.language === ref.language &&
    summary.package.platform === ref.platform &&
    summary.package.packageVersion === ref.packageVersion
  )
}

function refKey(summary: UiTextVersionSummary): string {
  const ref = refOf(summary)
  return `${ref.packageId}|${ref.language}|${ref.platform}|${ref.packageVersion}`
}

function formatTime(value: string): string {
  if (!value) {
    return '—'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '—'
  }
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">界面文案</p>
        <h1>界面文案管理</h1>
        <p class="page-description">
          文案经过审核和签名发布后才会下发；未发布的修改不会被用户看到。
        </p>
      </div>
      <div class="header-actions">
        <button type="button" class="secondary" :disabled="store.isLoading" @click="store.load">
          刷新列表
        </button>
        <button v-if="canManage" type="button" class="primary" @click="openCreate">
          新建文案草稿
        </button>
      </div>
    </header>

    <p v-if="!canManage" class="content-readonly-notice">
      你当前只有查看权限。可以查看文案版本、审核记录和条目内容，但新建、编辑、审核、发布与撤回由具备内容审核权限的管理员完成。
    </p>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="success-message">{{ store.lastMessage }}</p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.lastWarning" class="warning-message" role="alert">
        {{ store.lastWarning }}
      </p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.error" class="error-message">{{ store.error.message }}</p>
    </Transition>

    <form class="filter-bar" @submit.prevent="applyFilters">
      <label>
        <span>文案标识</span>
        <input v-model.trim="keywordInput" type="search" placeholder="例如 home_welcome" />
      </label>
      <label>
        <span>语言</span>
        <select v-model="store.filters.language">
          <option value="">全部语言</option>
          <option v-for="language in languages" :key="language" :value="language">
            {{ uiTextLanguageLabel(language) }}
          </option>
        </select>
      </label>
      <label>
        <span>平台</span>
        <select v-model="store.filters.platform">
          <option value="all">全部平台</option>
          <option v-for="platform in platforms" :key="platform" :value="platform">
            {{ uiTextPlatformLabel(platform) }}
          </option>
        </select>
      </label>
      <label>
        <span>状态</span>
        <select v-model="store.filters.status">
          <option value="all">全部状态</option>
          <option v-for="status in statuses" :key="status" :value="status">
            {{ uiTextStatusLabel(status) }}
          </option>
        </select>
      </label>
      <div class="filter-actions">
        <button type="submit" class="secondary" :disabled="store.isLoading">筛选</button>
        <button type="button" class="ghost" @click="resetFilters">重置</button>
      </div>
    </form>

    <Transition name="page" mode="out-in">
      <div v-if="store.isLoading" key="loading" class="state-panel">
        <span class="state-spinner" aria-hidden="true"></span>
        正在读取文案版本…
      </div>
      <div v-else-if="store.versions.length === 0" key="empty" class="state-panel">
        <span>还没有文案版本。新建草稿后，会显示在这里。</span>
      </div>
      <div v-else key="table" class="table-shell">
        <table>
          <thead>
            <tr>
              <th>文案标识</th>
              <th>语言</th>
              <th>平台</th>
              <th>版本</th>
              <th>状态</th>
              <th>条数</th>
              <th>更新时间</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="summary in store.versions"
              :key="refKey(summary)"
              @click="selectVersion(summary)"
            >
              <td>
                <span class="package-id">{{ summary.package.packageId }}</span>
                <span v-if="summary.isPublished" class="pill published">当前发布</span>
              </td>
              <td>{{ uiTextLanguageLabel(summary.package.language) }}</td>
              <td>{{ uiTextPlatformLabel(summary.package.platform) }}</td>
              <td>v{{ summary.package.packageVersion }}</td>
              <td>
                <span :class="['status', summary.package.status]">
                  {{ uiTextStatusLabel(summary.package.status) }}
                </span>
              </td>
              <td>{{ summary.entryCount }}</td>
              <td>{{ formatTime(summary.package.updatedAt) }}</td>
              <td>
                <div v-if="canManage" class="row-actions" @click.stop>
                  <button
                    v-if="summary.package.status === 'draft'"
                    type="button"
                    :disabled="store.isSubmitting"
                    @click="submitVersion(summary)"
                  >
                    提交审核
                  </button>
                  <button
                    v-if="summary.package.status === 'in_review'"
                    type="button"
                    :disabled="store.isSubmitting"
                    @click="approveVersion(summary)"
                  >
                    通过审核
                  </button>
                  <button
                    v-if="summary.package.status === 'in_review'"
                    type="button"
                    class="ghost"
                    :disabled="store.isSubmitting"
                    @click="openReject(summary)"
                  >
                    驳回
                  </button>
                  <button
                    v-if="summary.package.status === 'in_review'"
                    type="button"
                    class="primary"
                    :disabled="store.isSubmitting"
                    @click="openPublish(summary)"
                  >
                    发布
                  </button>
                  <button
                    v-if="summary.package.status === 'published'"
                    type="button"
                    class="danger"
                    :disabled="store.isSubmitting"
                    @click="withdrawVersion(summary)"
                  >
                    撤回
                  </button>
                </div>
                <span v-else class="read-only-copy">只读</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </Transition>

    <footer v-if="store.total > UI_TEXT_PAGE_SIZE" class="pagination">
      <button
        type="button"
        class="ghost"
        :disabled="store.page <= 1 || store.isLoading"
        @click="changePage(-1)"
      >
        上一页
      </button>
      <span>第 {{ store.page }} / {{ store.pageCount }} 页（共 {{ store.total }} 个版本）</span>
      <button
        type="button"
        class="ghost"
        :disabled="store.page >= store.pageCount || store.isLoading"
        @click="changePage(1)"
      >
        下一页
      </button>
    </footer>

    <Transition name="modal">
      <div v-if="store.selectedRef" class="dialog-backdrop" @click.self="store.closeVersion">
        <section class="dialog detail-dialog">
          <header class="dialog-header">
            <div>
              <h2>
                {{ store.selectedDetail?.package.packageId ?? store.selectedRef.packageId }}
                <small>v{{ store.selectedRef.packageVersion }}</small>
              </h2>
              <p class="dialog-hint">
                {{ uiTextLanguageLabel(store.selectedRef.language) }} ·
                {{ uiTextPlatformLabel(store.selectedRef.platform) }}
              </p>
            </div>
            <button type="button" class="ghost" @click="store.closeVersion">关闭</button>
          </header>

          <div v-if="store.isDetailLoading" class="state-panel compact">
            <span class="state-spinner" aria-hidden="true"></span>
            正在读取版本…
          </div>
          <template v-else-if="store.selectedDetail">
            <dl class="meta-grid">
              <div>
                <dt>状态</dt>
                <dd>
                  <span :class="['status', store.selectedDetail.package.status]">
                    {{ uiTextStatusLabel(store.selectedDetail.package.status) }}
                  </span>
                </dd>
              </div>
              <div>
                <dt>最低客户端版本</dt>
                <dd>{{ store.selectedDetail.package.minClientVersion || '不限' }}</dd>
              </div>
              <div>
                <dt>发布修订号</dt>
                <dd>{{ store.selectedDetail.package.revision }}</dd>
              </div>
              <div>
                <dt>签名密钥</dt>
                <dd>{{ store.selectedDetail.package.signatureKeyId || '尚未签名' }}</dd>
              </div>
              <div>
                <dt>发布时间</dt>
                <dd>{{ formatTime(store.selectedDetail.package.publishedAt) }}</dd>
              </div>
              <div>
                <dt>更新时间</dt>
                <dd>{{ formatTime(store.selectedDetail.package.updatedAt) }}</dd>
              </div>
            </dl>

            <div class="entries">
              <h3>文案条目（{{ store.selectedDetail.entries.length }}）</h3>
              <ul>
                <li v-for="entry in store.selectedDetail.entries" :key="entry.uiTextKey">
                  <code>{{ entry.uiTextKey }}</code>
                  <p>{{ entry.value }}</p>
                  <small>
                    最长 {{ entry.maxLength }} 字
                    <template v-if="entry.variables.length">
                      · 变量 {{ entry.variables.join(', ') }}
                    </template>
                  </small>
                </li>
              </ul>
            </div>

            <div v-if="canManage" class="dialog-actions">
              <button
                v-if="store.selectedDetail.package.status === 'draft'"
                type="button"
                class="secondary"
                @click="openEdit"
              >
                编辑草稿
              </button>
              <button
                v-if="activeSummary && store.selectedDetail.package.status === 'draft'"
                type="button"
                class="primary"
                :disabled="store.isSubmitting"
                @click="activeSummary && submitVersion(activeSummary)"
              >
                提交审核
              </button>
            </div>
          </template>
        </section>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="isCreateOpen && canManage" class="dialog-backdrop" @click.self="closeCreate">
        <form class="dialog" @submit.prevent="submitCreate">
          <h2>新建文案草稿</h2>
          <p class="dialog-hint">一个文案标识按语言和平台独立管理，版本号由平台自动分配。</p>
          <UiTextDraftFields
            :form="createForm"
            :lock-identity="false"
            @add-entry="addEntry(createForm)"
            @remove-entry="removeEntry(createForm, $event)"
          />
          <div class="dialog-actions">
            <button type="button" class="secondary" @click="closeCreate">取消</button>
            <button type="submit" class="primary" :disabled="store.isSubmitting">
              {{ store.isSubmitting ? '正在创建…' : '创建草稿' }}
            </button>
          </div>
        </form>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="isEditOpen && canManage" class="dialog-backdrop" @click.self="isEditOpen = false">
        <form class="dialog" @submit.prevent="submitEdit">
          <h2>编辑文案草稿</h2>
          <p class="dialog-hint">提交审核后条目将不可修改，请确认全部文案准确无误。</p>
          <UiTextDraftFields
            :form="editForm"
            :lock-identity="true"
            @add-entry="addEntry(editForm)"
            @remove-entry="removeEntry(editForm, $event)"
          />
          <div class="dialog-actions">
            <button type="button" class="secondary" @click="isEditOpen = false">取消</button>
            <button type="submit" class="primary" :disabled="store.isSubmitting">
              {{ store.isSubmitting ? '正在保存…' : '保存草稿' }}
            </button>
          </div>
        </form>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="rejectTarget && canManage" class="dialog-backdrop" @click.self="rejectTarget = null">
        <form class="dialog confirm-dialog" @submit.prevent="confirmReject">
          <h2>驳回这个版本？</h2>
          <p>驳回后版本会回到草稿状态，请填写驳回原因供创建人修改。</p>
          <label>
            <span>驳回原因</span>
            <textarea v-model.trim="rejectReason" rows="3" required></textarea>
          </label>
          <div class="dialog-actions">
            <button type="button" class="secondary" @click="rejectTarget = null">取消</button>
            <button type="submit" class="danger-button" :disabled="store.isSubmitting">
              {{ store.isSubmitting ? '正在驳回…' : '确认驳回' }}
            </button>
          </div>
        </form>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="publishTarget && canManage" class="dialog-backdrop" @click.self="publishTarget = null">
        <form class="dialog confirm-dialog" @submit.prevent="confirmPublish">
          <h2>发布这个版本？</h2>
          <p>
            发布后，{{ uiTextLanguageLabel(publishTarget.package.language) }} ·
            {{ uiTextPlatformLabel(publishTarget.package.platform) }}
            的客户端会收到 v{{ publishTarget.package.packageVersion }} 的文案。
          </p>
          <label>
            <span>签名密钥编号</span>
            <input
              v-model.trim="publishKeyId"
              type="text"
              required
              placeholder="例如 release-2026"
            />
          </label>
          <label>
            <span>分离签名</span>
            <textarea
              v-model.trim="publishSignature"
              rows="3"
              required
              placeholder="Base64 编码的 Ed25519 签名"
            ></textarea>
          </label>
          <div class="dialog-actions">
            <button type="button" class="secondary" @click="publishTarget = null">取消</button>
            <button type="submit" class="primary" :disabled="store.isSubmitting">
              {{ store.isSubmitting ? '正在发布…' : '确认发布' }}
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
  margin-bottom: 20px;
  padding: 24px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 8px 24px rgb(194 91 128 / 5%);
}

.content-readonly-notice {
  margin: 0 0 18px;
  padding: 12px 15px;
  border: 1px solid #e6c56d;
  border-radius: 14px;
  background: #fff9e8;
  color: #7a5411;
  line-height: 1.6;
}

.read-only-copy {
  color: var(--sprout-text-muted);
  font-size: 12px;
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
  max-width: 720px;
  margin: 10px 0 0;
  color: var(--sprout-text-muted);
}

.header-actions,
.row-actions,
.filter-actions,
.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.filter-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 14px;
  margin-bottom: 18px;
  padding: 18px 20px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
}

.filter-bar label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 160px;
  color: var(--sprout-text-muted);
  font-size: 13px;
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
}

button.primary {
  border-color: #d94f83;
  background: #d94f83;
  color: #ffffff;
}

button.primary:not(:disabled):hover {
  background: #c94175;
}

button.ghost {
  border-color: transparent;
  background: transparent;
}

button.danger {
  border-color: #f0bdcb;
  color: #a12b4a;
}

button.danger-button {
  border-color: #b3261e;
  background: #b3261e;
  color: #ffffff;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

input,
select,
textarea {
  min-height: 38px;
  padding: 8px 10px;
  border: 1px solid var(--sprout-outline);
  border-radius: 12px;
  background: #ffffff;
  color: var(--sprout-text);
  font: inherit;
}

textarea {
  resize: vertical;
}

.table-shell,
.state-panel {
  overflow: hidden;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.state-panel {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 40px 20px;
  color: var(--sprout-text-muted);
}

.state-panel.compact {
  padding: 24px 16px;
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
  vertical-align: middle;
}

th {
  color: #6b4f5a;
  font-size: 13px;
}

tbody tr {
  cursor: pointer;
  transition: background-color var(--sprout-duration-fast) ease;
}

tbody tr:hover {
  background: #fff8fa;
}

tbody tr:last-child td {
  border-bottom: 0;
}

.package-id {
  font-weight: 700;
}

.pill {
  margin-left: 8px;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
}

.pill.published {
  background: #fce4ee;
  color: #b23a68;
}

.status {
  padding: 3px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
}

.status.draft {
  background: #f3eef0;
  color: #6b4f5a;
}

.status.in_review {
  background: #fff3d1;
  color: #9a6b00;
}

.status.published {
  background: #dff3e6;
  color: #1f7a45;
}

.status.withdrawn {
  background: #fde3e0;
  color: #a12b4a;
}

.pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
  margin-top: 18px;
  color: var(--sprout-text-muted);
}

.dialog-backdrop {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgb(74 46 59 / 32%);
  z-index: 40;
}

.dialog {
  width: min(760px, 100%);
  max-height: 88vh;
  overflow-y: auto;
  padding: 26px;
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 24px 60px rgb(74 46 59 / 24%);
}

.detail-dialog {
  width: min(820px, 100%);
}

.confirm-dialog {
  width: min(520px, 100%);
}

.dialog-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

h2 {
  margin: 0;
  color: var(--sprout-text);
}

h2 small {
  color: var(--sprout-text-muted);
  font-weight: 600;
}

.dialog-hint {
  margin: 8px 0 18px;
  color: var(--sprout-text-muted);
}

.confirm-dialog label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 14px;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.meta-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  margin: 18px 0;
}

.meta-grid dt {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.meta-grid dd {
  margin: 4px 0 0;
  color: var(--sprout-text);
  font-weight: 600;
}

.entries h3 {
  margin: 0 0 10px;
  color: var(--sprout-text);
  font-size: 15px;
}

.entries ul {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.entries li {
  padding: 12px 14px;
  border: 1px solid var(--sprout-outline);
  border-radius: 14px;
}

.entries code {
  color: #b23a68;
  font-weight: 700;
}

.entries p {
  margin: 6px 0;
  color: var(--sprout-text);
  white-space: pre-wrap;
}

.entries small {
  color: var(--sprout-text-muted);
}

.dialog-actions {
  justify-content: flex-end;
  margin-top: 20px;
}

.success-message,
.error-message {
  margin: 0 0 14px;
  padding: 12px 16px;
  border-radius: 14px;
  font-weight: 600;
}

.success-message {
  background: #e6f6ec;
  color: #1f7a45;
}

.warning-message {
  margin: 0 0 14px;
  padding: 12px 16px;
  border-radius: 14px;
  background: #fff3d4;
  color: #7a5411;
  font-weight: 600;
}

.error-message {
  background: #fde3e0;
  color: #a12b4a;
}

.state-spinner {
  width: 16px;
  height: 16px;
  border: 2px solid var(--sprout-outline);
  border-top-color: var(--sprout-pink-strong);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 720px) {
  .page-header {
    flex-direction: column;
  }

  .meta-grid {
    grid-template-columns: 1fr;
  }
}
</style>
