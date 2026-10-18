<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'

import {
  type DownloadArtifactKind,
  type DownloadArtifactPlatform,
  type DownloadFile,
} from '@/api/adminDownloadFiles'
import {
  artifactKindLabel,
  artifactPlatformLabel,
  useDownloadFilesStore,
} from '@/features/downloads/application/downloadFilesStore'
import { useAuthStore } from '@/features/auth/application/authStore'
import { ADMIN_PERMISSIONS } from '@/features/auth/domain/adminPermission'

const store = useDownloadFilesStore()
const authStore = useAuthStore()
const canManageStorage = computed(() => authStore.can(ADMIN_PERMISSIONS.storageManage))
const isUploadOpen = ref(false)
const pendingDelete = ref<DownloadFile | null>(null)
const uploadForm = reactive({
  file: null as File | null,
  filename: '',
  version: '',
  kind: 'client' as DownloadArtifactKind,
  platform: 'android' as DownloadArtifactPlatform,
  releaseNotes: '',
})

const versionArtifactRows = computed(() => store.versionArtifacts)
const uploadTargetPath = computed(() => store.previewUploadPath(uploadForm))
const uploadTargetDirectory = computed(() => {
  const segments = uploadTargetPath.value.split('/')
  segments.pop()
  return segments.join('/')
})
const allVersionsRegistered = computed(
  () =>
    store.versionArtifacts.length > 0 &&
    store.versionArtifacts.every((group) => group.artifacts.every((file) => file.isIndexed)),
)
const uploadConflictMessage = ref('')
const isCheckingUploadConflict = ref(false)
let conflictCheckSequence = 0

onMounted(async () => {
  await store.load()
})

watch(
  () => [uploadForm.version, uploadForm.kind, uploadForm.platform, uploadForm.filename],
  () => {
    void refreshUploadConflict()
  },
)

const hasPendingUploadCompletion = computed(() => {
  const state = store.uploadState
  if (state.uploadedFilePath === '') {
    return false
  }
  if (
    state.registrationState === 'failed' ||
    state.registrationState === 'skipped' ||
    state.registrationState === 'notes_missing'
  ) {
    return true
  }
  return (
    state.registrationState === 'registered' &&
    (state.indexState !== 'refreshed' || state.listState !== 'refreshed')
  )
})

function openUpload(): void {
  if (!hasPendingUploadCompletion.value) {
    store.clearMessages()
  } else {
    restorePendingUploadForm()
  }
  isUploadOpen.value = true
}

function closeUpload(): void {
  if (store.uploadState.isUploading || store.uploadState.isRegistering) {
    return
  }
  if (hasPendingUploadCompletion.value) {
    return
  }
  isUploadOpen.value = false
  resetUploadForm()
}

function selectUploadFile(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLInputElement)) {
    return
  }
  const selected = target.files?.[0] ?? null
  uploadForm.file = selected
  if (selected !== null && uploadForm.filename.trim() === '') {
    uploadForm.filename = selected.name
  }
}

async function submitUpload(): Promise<void> {
  if (uploadForm.file === null || !uploadForm.filename.trim()) {
    return
  }
  if (store.uploadState.isUploading || store.uploadState.isRegistering) {
    return
  }
  await refreshUploadConflict()
  if (uploadConflictMessage.value) {
    return
  }
  if (isCheckingUploadConflict.value) {
    return
  }
  await performUpload()
}

async function refreshUploadConflict(): Promise<void> {
  const requestSequence = ++conflictCheckSequence
  if (!uploadForm.version.trim() || !uploadForm.filename.trim()) {
    uploadConflictMessage.value = ''
    isCheckingUploadConflict.value = false
    return
  }
  isCheckingUploadConflict.value = true
  try {
    const conflict = await store.checkUploadConflict({
      file: uploadForm.file ?? new File([], uploadForm.filename),
      filename: uploadForm.filename,
      version: uploadForm.version,
      kind: uploadForm.kind,
      platform: uploadForm.platform,
      overwrite: false,
      releaseNotes: uploadForm.releaseNotes,
    })
    if (requestSequence !== conflictCheckSequence) {
      return
    }
    uploadConflictMessage.value = conflict?.message ?? ''
  } catch {
    if (requestSequence !== conflictCheckSequence) {
      return
    }
    uploadConflictMessage.value = ''
  } finally {
    if (requestSequence === conflictCheckSequence) {
      isCheckingUploadConflict.value = false
    }
  }
}

async function performUpload(): Promise<void> {
  if (uploadForm.file === null) {
    return
  }
  const result = await store.upload({
    file: uploadForm.file,
    filename: uploadForm.filename,
    version: uploadForm.version,
    kind: uploadForm.kind,
    platform: uploadForm.platform,
    overwrite: false,
    releaseNotes: uploadForm.releaseNotes,
  })
  if (uploadFlowCompleted(result)) {
    isUploadOpen.value = false
    resetUploadForm()
  }
}

async function retryUploadCompletion(): Promise<void> {
  if (store.uploadState.isUploading || store.uploadState.isRegistering) {
    return
  }
  if (uploadForm.releaseNotes.trim() !== '') {
    store.applyReleaseNotes(uploadForm.releaseNotes)
  }
  const result = await store.retryUploadCompletion()
  if (uploadFlowCompleted(result)) {
    isUploadOpen.value = false
    resetUploadForm()
  }
}

async function handleUploadReviewAction(): Promise<void> {
  const action = store.uploadReview.action
  if (action === 'retry_registration') {
    await retryUploadCompletion()
    return
  }
  if (action === 'refresh_index') {
    await store.refreshUploadedIndex()
    closeUploadAfterCompletion()
    return
  }
  if (action === 'reload_files') {
    await store.reloadFiles()
    closeUploadAfterCompletion()
  }
}

function uploadFlowCompleted(result: {
  uploaded: boolean
  registered: boolean
  indexRefreshed: boolean
  listRefreshed: boolean
}): boolean {
  return result.uploaded && result.registered && result.indexRefreshed && result.listRefreshed
}

function closeUploadAfterCompletion(): void {
  if (
    store.uploadState.registrationState === 'registered' &&
    store.uploadState.indexState === 'refreshed' &&
    store.uploadState.listState === 'refreshed'
  ) {
    isUploadOpen.value = false
    resetUploadForm()
  }
}

async function confirmDelete(): Promise<void> {
  if (pendingDelete.value === null) {
    return
  }
  const succeeded = await store.remove(pendingDelete.value)
  if (succeeded) {
    pendingDelete.value = null
  }
}

function resetUploadForm(): void {
  uploadForm.file = null
  uploadForm.filename = ''
  uploadForm.version = ''
  uploadForm.kind = 'client'
  uploadForm.platform = 'android'
  uploadForm.releaseNotes = ''
  conflictCheckSequence += 1
  uploadConflictMessage.value = ''
  isCheckingUploadConflict.value = false
}

function restorePendingUploadForm(): void {
  const pending = store.pendingUploadMetadata()
  if (pending === null) {
    return
  }
  if (uploadForm.file === null) {
    uploadForm.filename = pending.filename
  }
  uploadForm.version = pending.version
  uploadForm.kind = pending.kind
  uploadForm.platform = pending.platform
  if (uploadForm.releaseNotes.trim() === '') {
    uploadForm.releaseNotes = pending.releaseNotes
  }
}

function onDirectoryChange(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLSelectElement)) {
    return
  }
  store.setFilters({ directory: target.value })
}

function onVersionChange(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLSelectElement)) {
    return
  }
  store.setFilters({ version: target.value })
}

function onKindChange(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLSelectElement)) {
    return
  }
  const value = target.value
  store.setFilters({ kind: value === 'all' ? 'all' : (value as DownloadArtifactKind) })
}

function onStatusChange(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLSelectElement)) {
    return
  }
  const value = target.value
  store.setFilters({ status: value === 'indexed' || value === 'pending' ? value : 'all' })
}

function fileKindIcon(kind: DownloadArtifactKind): string {
  return (
    {
      resource: '◇',
      client: '▣',
      firmware: '◇',
      unknown: '·',
    }[kind] ?? '·'
  )
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const exponent = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  const scaled = value / 1024 ** exponent
  const precision = exponent === 0 || scaled >= 100 ? 1 : 2
  return `${scaled.toFixed(precision)} ${units[exponent]}`
}

function formatTime(value: string): string {
  if (!value) {
    return '尚未更新'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '尚未更新'
  }
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function sha256Summary(value: string): string {
  if (!value) {
    return '还没有校验值'
  }
  return value.length > 20 ? `${value.slice(0, 20)}…` : value
}
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">版本运营</p>
        <h1>下载文件</h1>
        <p class="page-description">
          查看下载服务器上的真实文件，手动上传新版本，并确认发布索引是否已经登记。
        </p>
      </div>
      <div v-if="canManageStorage" class="header-actions">
        <button
          type="button"
          class="secondary"
          :disabled="
            store.isLoading ||
            store.isRefreshingIndex ||
            store.uploadState.isUploading ||
            store.uploadState.isRegistering
          "
          @click="store.refreshIndex"
        >
          {{ store.isRefreshingIndex ? '正在刷新索引…' : '刷新发布索引' }}
        </button>
        <button
          type="button"
          class="primary"
          :disabled="
            store.isLoading ||
            store.isRefreshingIndex ||
            store.uploadState.isUploading ||
            store.uploadState.isRegistering
          "
          @click="openUpload"
        >
          上传文件
        </button>
      </div>
    </header>

    <p v-if="!canManageStorage" class="readonly-notice">
      你当前只有查看权限。可以查看下载服务器上的文件，但不能上传、删除或刷新发布索引。
    </p>

    <div class="index-banner" :class="{ warning: !store.indexStatus.isAvailable }">
      <span class="banner-mark" aria-hidden="true">☆</span>
      <div>
        <strong>
          {{ store.indexStatus.isAvailable ? '发布索引可用' : '还没有读取到发布索引' }}
        </strong>
        <p>
          已登记 {{ store.indexStatus.indexedFileCount }} 个文件，待登记
          {{ store.indexStatus.pendingFileCount }} 个。最近刷新：
          {{ formatTime(store.indexStatus.refreshedAt) }}
        </p>
        <p v-if="store.indexStatus.errorCode" class="banner-error">
          发布索引暂时无法读取。请刷新索引；如果仍然失败，请稍后再试。
        </p>
      </div>
      <RouterLink v-if="canManageStorage" to="/releases"> 前往内容发布 </RouterLink>
    </div>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="success-message" role="status">
        {{ store.lastMessage }}
      </p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.error" class="error-message" role="alert">
        {{ store.error.message }}
      </p>
    </Transition>

    <div class="summary-grid" aria-label="下载文件概览">
      <div>
        <span>全部文件</span>
        <strong>{{ store.files.length }}</strong>
      </div>
      <div>
        <span>已登记发布</span>
        <strong>{{ store.indexedFileCount }}</strong>
      </div>
      <div :class="{ attention: store.pendingFileCount > 0 }">
        <span>待登记发布</span>
        <strong>{{ store.pendingFileCount }}</strong>
      </div>
      <div>
        <span>文件总大小</span>
        <strong class="size-value">{{ formatBytes(store.totalSizeBytes) }}</strong>
      </div>
    </div>

    <section class="filter-panel" aria-label="文件筛选">
      <div class="filter-heading">
        <div>
          <h2>文件筛选</h2>
          <p>按目录、版本、类型和发布登记状态查看当前真实文件。</p>
        </div>
        <button type="button" class="text-button" @click="store.resetFilters">清除筛选</button>
      </div>
      <div class="filter-grid">
        <label>
          <span>目录</span>
          <select :value="store.filters.directory" @change="onDirectoryChange">
            <option value="">全部目录</option>
            <option v-for="directory in store.directories" :key="directory" :value="directory">
              {{ directory }}
            </option>
          </select>
        </label>
        <label>
          <span>版本</span>
          <select :value="store.filters.version" @change="onVersionChange">
            <option value="">全部版本</option>
            <option v-for="version in store.versions" :key="version" :value="version">
              {{ version }}
            </option>
          </select>
        </label>
        <label>
          <span>文件类型</span>
          <select :value="store.filters.kind" @change="onKindChange">
            <option value="all">全部类型</option>
            <option value="resource">资源更新</option>
            <option value="client">客户端更新</option>
            <option value="firmware">设备固件</option>
            <option value="unknown">未标注</option>
          </select>
        </label>
        <label>
          <span>发布状态</span>
          <select :value="store.filters.status" @change="onStatusChange">
            <option value="all">全部状态</option>
            <option value="indexed">已登记发布</option>
            <option value="pending">待登记发布</option>
          </select>
        </label>
      </div>
    </section>

    <Transition name="page" mode="out-in">
      <div :key="store.isLoading ? 'loading' : store.visibleFiles.length === 0 ? 'empty' : 'table'">
        <div v-if="store.isLoading" class="state-panel">
          <span class="state-spinner" aria-hidden="true"></span>
          正在读取下载服务器文件…
        </div>
        <div v-else-if="store.visibleFiles.length === 0" class="state-panel">
          <span class="state-icon" aria-hidden="true">☆</span>
          <span>当前筛选条件下还没有文件。上传新版本或调整筛选后再查看。</span>
        </div>
        <div v-else class="table-shell">
          <table>
            <thead>
              <tr>
                <th>文件</th>
                <th>目录</th>
                <th>版本</th>
                <th>类型 / 平台</th>
                <th>大小</th>
                <th>修改时间</th>
                <th>校验值</th>
                <th>发布登记</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="file in store.visibleFiles" :key="file.relativePath">
                <td>
                  <div class="file-name">
                    <span class="file-kind" aria-hidden="true">{{ fileKindIcon(file.kind) }}</span>
                    <div>
                      <strong>{{ file.name }}</strong>
                      <small>{{ file.relativePath }}</small>
                    </div>
                  </div>
                </td>
                <td>{{ file.directory || '根目录' }}</td>
                <td class="version-cell">{{ file.version || '未标注' }}</td>
                <td>
                  <span class="type-pill">{{ artifactKindLabel(file.kind) }}</span>
                  <small class="platform-copy">{{ artifactPlatformLabel(file.platform) }}</small>
                </td>
                <td>{{ formatBytes(file.sizeBytes) }}</td>
                <td class="time-cell">{{ formatTime(file.modifiedAt) }}</td>
                <td>
                  <code :title="file.sha256 || '还没有校验值'">
                    {{ sha256Summary(file.sha256) }}
                  </code>
                </td>
                <td>
                  <span :class="['status', file.isIndexed ? 'indexed' : 'pending']">
                    {{ file.isIndexed ? '已登记' : '待登记' }}
                  </span>
                </td>
                <td>
                  <button
                    v-if="canManageStorage"
                    type="button"
                    class="danger"
                    :disabled="store.isDeleting(file.relativePath)"
                    @click="pendingDelete = file"
                  >
                    {{ store.isDeleting(file.relativePath) ? '正在删除…' : '删除' }}
                  </button>
                  <span v-else class="read-only-copy">只读</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </Transition>

    <section class="version-panel">
      <div class="panel-heading">
        <div>
          <h2>按版本查看</h2>
          <p>每个版本列出已经存在的文件，并标明是否已经登记到内容发布。</p>
        </div>
        <span :class="['version-status', allVersionsRegistered ? 'complete' : 'pending']">
          {{ allVersionsRegistered ? '全部版本已登记' : '仍有版本待登记' }}
        </span>
      </div>
      <div v-if="versionArtifactRows.length === 0" class="version-empty">
        还没有带版本号的文件。上传时填写版本号后，会在这里按版本汇总。
      </div>
      <ul v-else class="version-list">
        <li v-for="group in versionArtifactRows" :key="group.version">
          <div class="version-group-heading">
            <strong>{{ group.version }}</strong>
            <span>
              {{ group.artifacts.filter((file) => file.isIndexed).length }} /
              {{ group.artifacts.length }} 已登记
            </span>
          </div>
          <ul>
            <li v-for="file in group.artifacts" :key="file.relativePath">
              <span>{{ file.name }}</span>
              <span>{{ artifactKindLabel(file.kind) }}</span>
              <span>{{ artifactPlatformLabel(file.platform) }}</span>
              <span :class="['status', file.isIndexed ? 'indexed' : 'pending']">
                {{ file.isIndexed ? '已登记' : '待登记' }}
              </span>
            </li>
          </ul>
        </li>
      </ul>
    </section>

    <Transition name="modal">
      <div
        v-if="isUploadOpen"
        class="dialog-backdrop"
        @click.self="
          !store.uploadState.isUploading && !store.uploadState.isRegistering
            ? closeUpload()
            : undefined
        "
      >
        <form
          class="dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="download-upload-title"
          @submit.prevent="submitUpload"
        >
          <p class="eyebrow">上传文件</p>
          <h2 id="download-upload-title">上传到下载服务器</h2>
          <p class="dialog-hint">
            上传完成后会登记为待发布版本并刷新发布索引。请先填写本次更新的中文说明，确认后再发布。
          </p>
          <div class="field-grid">
            <label class="span-two file-picker">
              <span>本地文件</span>
              <input type="file" required @change="selectUploadFile" />
            </label>
            <label>
              <span>目标目录</span>
              <input
                :value="uploadTargetDirectory || '填写版本号后自动确定'"
                type="text"
                readonly
                aria-readonly="true"
              />
            </label>
            <label>
              <span>文件名</span>
              <input
                v-model.trim="uploadForm.filename"
                type="text"
                required
                placeholder="例如 app-0.12.3.apk"
              />
            </label>
            <label>
              <span>版本号</span>
              <input v-model.trim="uploadForm.version" type="text" placeholder="例如 0.12.3" />
            </label>
            <label>
              <span>文件类型</span>
              <select v-model="uploadForm.kind">
                <option value="resource">资源更新</option>
                <option value="client">客户端更新</option>
                <option value="firmware">设备固件</option>
              </select>
            </label>
            <label>
              <span>适用平台</span>
              <select v-model="uploadForm.platform">
                <option value="android">Android</option>
                <option value="ios">iOS</option>
                <option value="esp32_s3">初芽设备</option>
                <option value="all">全部平台</option>
              </select>
            </label>
            <label class="span-two">
              <span>更新说明</span>
              <textarea
                v-model.trim="uploadForm.releaseNotes"
                rows="4"
                maxlength="4000"
                required
                aria-describedby="release-notes-hint"
                placeholder="写下这个版本相较上一版的改动，发布后家长会看到"
              ></textarea>
            </label>
          </div>

          <p v-if="uploadConflictMessage" class="inline-error" role="alert">
            {{ uploadConflictMessage }}
          </p>
          <p v-else-if="!uploadForm.releaseNotes" id="release-notes-hint" class="inline-hint">
            更新说明填写后，文件会自动登记为待发布版本；缺失说明时不会生成无法补充的草稿。
          </p>

          <div
            v-if="
              store.uploadState.isUploading ||
              store.uploadState.isRegistering ||
              store.uploadState.error
            "
            class="upload-status"
          >
            <div>
              <strong>{{ store.uploadState.fileName || '文件' }}</strong>
              <span>
                {{
                  store.uploadState.isRegistering
                    ? '正在登记版本'
                    : store.uploadState.isUploading
                      ? `${store.uploadState.progress}%`
                      : '上传已完成'
                }}
              </span>
            </div>
            <progress :value="store.uploadState.progress" max="100"></progress>
            <ol class="upload-stages" aria-label="上传处理进度">
              <li :class="store.uploadStages.upload">文件上传</li>
              <li :class="store.uploadStages.registration">版本登记</li>
              <li :class="store.uploadStages.index">发布索引</li>
              <li :class="store.uploadStages.list">文件列表</li>
            </ol>
            <small v-if="store.uploadState.uploadedFilePath" class="upload-path">
              {{ store.uploadState.uploadedFilePath }}
            </small>
            <small v-if="store.uploadReview.summary" class="upload-review">
              {{ store.uploadReview.summary }}
            </small>
            <small v-if="store.uploadState.error" class="upload-error">
              {{ store.uploadState.error }}
            </small>
            <small
              v-if="
                store.uploadStage === 'registration_required' && store.uploadState.uploadedFilePath
              "
              class="inline-hint"
            >
              请在下方补填更新说明后再次提交登记；文件已经保存在下载服务器， 不需要重新上传。
            </small>
            <div
              v-if="
                store.uploadReview.actionLabel && store.uploadReview.action !== 'manage_release'
              "
              class="upload-actions"
            >
              <button
                type="button"
                class="secondary"
                :disabled="
                  store.uploadState.isUploading ||
                  store.uploadState.isRegistering ||
                  store.isRefreshingIndex ||
                  store.isLoading ||
                  (store.uploadReview.action === 'retry_registration' &&
                    uploadForm.releaseNotes.trim() === '')
                "
                @click="handleUploadReviewAction"
              >
                {{ store.uploadReview.actionLabel }}
              </button>
            </div>
            <div
              v-else-if="store.uploadReview.canManageRelease || store.uploadStage === 'registered'"
              class="upload-actions"
            >
              <RouterLink class="review-link" to="/releases"> 前往内容发布 </RouterLink>
            </div>
          </div>

          <div class="dialog-actions">
            <p v-if="hasPendingUploadCompletion" class="pending-completion-hint">
              文件已经上传，请先完成登记、索引同步和列表刷新，再关闭窗口。
            </p>
            <button
              type="button"
              class="secondary"
              :disabled="
                store.uploadState.isUploading ||
                store.uploadState.isRegistering ||
                hasPendingUploadCompletion
              "
              @click="closeUpload"
            >
              取消
            </button>
            <button
              type="submit"
              class="primary"
              :disabled="
                store.uploadState.isUploading ||
                store.uploadState.isRegistering ||
                store.isLoading ||
                store.isRefreshingIndex ||
                Boolean(uploadConflictMessage) ||
                !uploadForm.releaseNotes
              "
            >
              {{
                store.uploadState.isUploading
                  ? '正在上传…'
                  : store.uploadState.isRegistering
                    ? '正在登记版本…'
                    : '上传并登记版本'
              }}
            </button>
          </div>
          <p class="upload-target-hint">将保存为：{{ uploadTargetPath }}</p>
        </form>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="pendingDelete" class="dialog-backdrop" @click.self="pendingDelete = null">
        <section
          class="dialog confirm-dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="download-delete-title"
        >
          <p class="eyebrow">删除文件</p>
          <h2 id="download-delete-title">删除 {{ pendingDelete.name }}？</h2>
          <p>删除后无法恢复。已经使用这个文件的版本记录不会自动删除，需要单独处理。</p>
          <div class="dialog-actions">
            <button type="button" class="secondary" @click="pendingDelete = null">取消</button>
            <button
              type="button"
              class="danger-button"
              :disabled="store.isDeleting(pendingDelete.relativePath)"
              @click="confirmDelete"
            >
              {{ store.isDeleting(pendingDelete.relativePath) ? '正在删除…' : '确认删除' }}
            </button>
          </div>
        </section>
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
p {
  overflow-wrap: anywhere;
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
.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
}

.readonly-notice {
  margin: 14px 0 0;
  padding: 12px 14px;
  border: 1px solid #b7def2;
  border-radius: 14px;
  background: #f3faff;
  color: #27627f;
  line-height: 1.55;
}

.read-only-copy {
  display: inline-block;
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.pending-completion-hint {
  flex: 1 1 260px;
  margin: 0;
  color: #a12b4a;
  font-size: 13px;
  line-height: 1.55;
}

button {
  min-height: 40px;
  padding: 0 16px;
  border: 1px solid #e9a5b8;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: #b23a68;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

button.primary {
  border-color: #d94f83;
  background: linear-gradient(180deg, #d94f83, #c94175);
  color: #ffffff;
  box-shadow: 0 8px 18px rgb(217 79 131 / 18%);
}

button:not(:disabled):hover {
  border-color: #d94f83;
  background: #fff7fa;
  color: #b23a68;
  box-shadow: 0 9px 20px rgb(217 79 131 / 12%);
}

button.primary:not(:disabled):hover {
  background: linear-gradient(180deg, #c94175, #b23a68);
  color: #ffffff;
}

button.danger {
  color: #a12b4a;
}

button.danger-button {
  border-color: #b3261e;
  background: #b3261e;
  color: #ffffff;
}

button:disabled {
  cursor: wait;
  opacity: 0.55;
}

.index-banner {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 14px;
  margin-top: 14px;
  padding: 14px 16px;
  border: 1px solid #bfe3cb;
  border-radius: 18px;
  background: #f2fbf5;
  color: #1d6b3f;
}

.index-banner.warning {
  border-color: #f3d18e;
  background: #fffaf0;
  color: #6b4f2a;
}

.banner-mark {
  display: grid;
  width: 30px;
  height: 30px;
  place-items: center;
  border-radius: 11px;
  background: #dff5e6;
  color: #1d6b3f;
  font-size: 18px;
}

.index-banner.warning .banner-mark {
  background: #ffe9ad;
  color: #8a5a00;
}

.index-banner strong,
.index-banner p {
  margin: 0;
}

.index-banner p {
  margin-top: 4px;
  font-size: 13px;
}

.index-banner a {
  color: inherit;
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
}

.banner-error {
  color: #a12b4a !important;
}

.success-message,
.error-message {
  margin: 14px 0 0;
}

.success-message {
  color: #1d6b3f;
}

.error-message {
  color: #a12b4a;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
  margin-top: 18px;
}

.summary-grid > div {
  display: grid;
  gap: 6px;
  padding: 16px 18px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 18px;
  background: #ffffff;
  box-shadow: 0 8px 22px rgb(194 91 128 / 5%);
}

.summary-grid span {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.summary-grid strong {
  color: var(--sprout-text);
  font-size: 24px;
}

.summary-grid .attention {
  border-color: #efb4c5;
  background: #fff7fa;
}

.summary-grid .size-value {
  font-size: 20px;
}

.filter-panel,
.version-panel {
  margin-top: 18px;
  overflow: hidden;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.filter-heading,
.panel-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 18px 20px;
  border-bottom: 1px solid var(--sprout-outline-soft);
}

.filter-heading h2,
.filter-heading p,
.panel-heading h2,
.panel-heading p {
  margin: 0;
}

.filter-heading h2,
.panel-heading h2 {
  color: var(--sprout-text);
  font-size: 17px;
}

.filter-heading p,
.panel-heading p {
  margin-top: 4px;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.filter-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
  padding: 18px 20px 20px;
}

label {
  display: grid;
  gap: 7px;
  color: #4a2e3b;
  font-size: 13px;
  font-weight: 700;
}

select,
input {
  width: 100%;
  min-height: 42px;
  padding: 0 13px;
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

select:focus,
input:focus {
  border-color: #d94f83;
  background: #ffffff;
  box-shadow: 0 0 0 3px rgb(217 79 131 / 12%);
  outline: none;
}

input[readonly] {
  background: #fff2f5;
  color: #7b5263;
  cursor: default;
}

.dialog textarea {
  width: 100%;
  min-height: 104px;
  padding: 11px 13px;
  border: 1px solid #f0bdcb;
  border-radius: 14px;
  background: #fff8fa;
  color: #4a2e3b;
  font: inherit;
  line-height: 1.55;
  resize: vertical;
}

.dialog textarea:focus {
  border-color: #d94f83;
  outline: none;
  box-shadow: 0 0 0 3px rgb(217 79 131 / 12%);
}

.text-button {
  min-height: 32px;
  padding: 0 12px;
  border-radius: 11px;
  background: #fff7fa;
  font-size: 12px;
}

.table-shell {
  margin-top: 18px;
  overflow: hidden;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  padding: 14px 15px;
  border-bottom: 1px solid var(--sprout-outline-soft);
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
  background: #fff9fb;
}

tbody tr:last-child td {
  border-bottom: 0;
}

.file-name {
  display: flex;
  min-width: 220px;
  align-items: flex-start;
  gap: 10px;
}

.file-kind {
  display: grid;
  width: 30px;
  height: 30px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 11px;
  background: #fff2f5;
  color: #d94f83;
}

.file-name div {
  display: grid;
  min-width: 0;
  gap: 4px;
}

.file-name strong {
  overflow-wrap: anywhere;
}

td small,
.file-name small {
  display: block;
  color: var(--sprout-text-muted);
  font-size: 12px;
  line-height: 1.45;
  overflow-wrap: anywhere;
}

.version-cell {
  color: #7b5263;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
}

.type-pill {
  display: inline-flex;
  padding: 4px 9px;
  border-radius: 999px;
  background: #fff2f5;
  color: #b23a68;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.platform-copy,
.time-cell {
  white-space: nowrap;
}

code {
  display: block;
  max-width: 180px;
  color: #5b3b49;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.status {
  display: inline-flex;
  width: fit-content;
  padding: 5px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.status.indexed {
  background: #e7f8ee;
  color: #1d6b3f;
}

.status.pending {
  background: #fff3cd;
  color: #755600;
}

.state-panel {
  display: flex;
  min-height: 156px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-top: 18px;
  padding: 28px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  color: var(--sprout-text-muted);
  text-align: center;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
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

.version-status {
  padding: 5px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.version-status.complete {
  background: #e7f8ee;
  color: #1d6b3f;
}

.version-status.pending {
  background: #fff3cd;
  color: #755600;
}

.version-empty {
  padding: 24px 20px;
  color: var(--sprout-text-muted);
}

.version-list,
.version-list ul {
  margin: 0;
  padding: 0;
  list-style: none;
}

.version-list > li {
  border-bottom: 1px solid var(--sprout-outline-soft);
}

.version-list > li:last-child {
  border-bottom: 0;
}

.version-group-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 20px;
  background: #fffbfc;
}

.version-group-heading strong {
  color: var(--sprout-pink-deep);
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
}

.version-group-heading span {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.version-list ul li {
  display: grid;
  grid-template-columns: minmax(180px, 1.5fr) 1fr 1fr auto;
  align-items: center;
  gap: 12px;
  padding: 11px 20px;
  border-top: 1px solid #fbeaf0;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.dialog-backdrop {
  position: fixed;
  z-index: 20;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgb(74 46 59 / 35%);
  backdrop-filter: blur(3px);
}

.dialog {
  display: grid;
  width: min(100%, 620px);
  max-height: min(840px, calc(100vh - 40px));
  gap: 15px;
  overflow: auto;
  padding: 28px;
  border: 1px solid var(--sprout-outline);
  border-radius: 28px;
  background: #ffffff;
  box-shadow: 0 28px 72px rgb(74 46 59 / 22%);
}

.dialog h2,
.dialog p {
  margin: 0;
}

.dialog h2 {
  color: var(--sprout-text);
}

.dialog-hint,
.confirm-dialog p {
  color: var(--sprout-text-muted);
  line-height: 1.65;
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.field-grid .span-two {
  grid-column: 1 / -1;
}

.file-picker input {
  padding: 9px 10px;
}

.upload-status {
  display: grid;
  gap: 8px;
  padding: 13px 14px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 16px;
  background: #fffbfc;
}

.upload-status > div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  color: var(--sprout-text);
}

.upload-path {
  color: var(--sprout-text-muted);
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.upload-review {
  color: var(--sprout-text);
  font-size: 13px;
  line-height: 1.55;
}

.upload-actions {
  display: flex;
  justify-content: flex-end;
}

.upload-stages {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.upload-stages li {
  padding: 4px 9px;
  border: 1px solid #f0bdcb;
  border-radius: 999px;
  background: #fff7fa;
  color: #7b5263;
  font-size: 12px;
}

.upload-stages li.done {
  border-color: #bfe3cb;
  background: #f2fbf5;
  color: #1d6b3f;
}

.upload-stages li.pending {
  border-color: #f3d18e;
  background: #fffaf0;
  color: #755600;
}

.upload-stages li.failed,
.upload-stages li.conflict {
  border-color: #efb4c5;
  background: #fff5f8;
  color: #a12b4a;
}

.upload-stages li.blocked {
  opacity: 0.68;
}

.review-link {
  display: inline-flex;
  min-height: 40px;
  align-items: center;
  padding: 0 16px;
  border: 1px solid #d94f83;
  border-radius: var(--sprout-radius-control);
  background: #d94f83;
  color: #ffffff;
  font-size: 13px;
  font-weight: 700;
  text-decoration: none;
}

.review-link:hover {
  background: #c94175;
}

.inline-error,
.inline-hint {
  margin: 0;
  padding: 10px 12px;
  border-radius: 13px;
  font-size: 13px;
  line-height: 1.55;
}

.inline-error {
  border: 1px solid #efb4c5;
  background: #fff5f8;
  color: #a12b4a;
}

.inline-hint {
  border: 1px solid #f7d9e2;
  background: #fffbfc;
  color: var(--sprout-text-muted);
}

progress {
  width: 100%;
  height: 10px;
  overflow: hidden;
  border: 0;
  border-radius: 999px;
  background: #f7d9e2;
}

progress::-webkit-progress-bar {
  background: #f7d9e2;
}

progress::-webkit-progress-value {
  background: linear-gradient(90deg, #f7a8bf, #d94f83);
}

progress::-moz-progress-bar {
  background: linear-gradient(90deg, #f7a8bf, #d94f83);
}

.upload-error {
  color: #a12b4a;
}

.upload-target-hint {
  padding: 10px 12px;
  border-radius: 13px;
  background: #fff7fa;
  color: #7b5263;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  overflow-wrap: anywhere;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1120px) {
  .summary-grid,
  .filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .table-shell {
    overflow-x: auto;
  }

  table {
    min-width: 1180px;
  }
}

@media (max-width: 760px) {
  .page-header {
    display: grid;
  }

  .header-actions {
    justify-content: flex-start;
  }

  .index-banner {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .index-banner a {
    grid-column: 1 / -1;
  }

  .version-list ul li {
    grid-template-columns: 1fr auto;
  }
}

@media (max-width: 520px) {
  .summary-grid,
  .filter-grid,
  .field-grid {
    grid-template-columns: 1fr;
  }

  .field-grid .span-two {
    grid-column: auto;
  }
}
</style>
