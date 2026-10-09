<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import type {
  CreateOtaReleaseInput,
  OtaDeployment,
  OtaRelease,
  OtaReleaseChannel,
  OtaTargetType,
  UpdateOtaReleaseInput,
} from '@/api/adminOta'
import type { DownloadFile } from '@/api/adminDownloadFiles'
import {
  otaDeploymentRailClass,
  otaDeploymentStatusLabel,
  otaReleaseChannelLabel,
  otaReleaseRailClass,
  otaReleaseStatusLabel,
  otaTargetTypeLabel,
  formatOtaRate,
  otaSignatureStatusLabel,
  useDeviceOtaStore,
} from '@/features/ota/application/deviceOtaStore'

type OtaActionKind = 'publish' | 'pause' | 'withdraw' | 'rollback'

interface PendingAction {
  kind: OtaActionKind
  release: OtaRelease
}

const store = useDeviceOtaStore()
const isReleaseFormOpen = ref(false)
const isUploadOpen = ref(false)
const isFilePickerOpen = ref(false)
const isDetailOpen = ref(false)
const editingRelease = ref<OtaRelease | null>(null)
const pendingAction = ref<PendingAction | null>(null)
const formError = ref('')
const uploadFormError = ref('')
const uploadForm = reactive({
  file: null as File | null,
  filename: '',
  firmwareVersion: '',
  channel: 'stable' as OtaReleaseChannel,
  hardwareRevision: '',
  overwrite: false,
})
const releaseForm = reactive<CreateOtaReleaseInput>({
  firmwareVersion: '',
  hardwareRevision: '',
  channel: 'stable',
  artifactKey: '',
  artifactUrl: '',
  sha256: '',
  sizeBytes: 0,
  signatureKeyId: '',
  signatureAlgorithm: 'ed25519',
  signature: '',
  rollbackAllowed: false,
  minSourceVersion: '',
  releaseNotes: '',
  targetType: 'all',
  targetId: '',
  canaryPercent: 0,
})

const isEditing = computed(() => editingRelease.value !== null)
const releaseFormTitle = computed(() => (isEditing.value ? '编辑固件版本' : '新建固件版本'))
const detailRelease = computed(() => store.detail?.release ?? null)
const deploymentStart = computed(() =>
  store.deploymentTotal === 0 ? 0 : (store.deploymentPage - 1) * store.filters.pageSize + 1,
)
const deploymentEnd = computed(() =>
  store.deploymentTotal === 0
    ? 0
    : Math.min(store.deploymentPage * store.filters.pageSize, store.deploymentTotal),
)
const canGoPreviousDeploymentPage = computed(
  () => store.deploymentPage > 1 && !store.isLoadingDetail,
)
const canGoNextDeploymentPage = computed(
  () => store.deploymentPage < store.deploymentPageCount && !store.isLoadingDetail,
)
const rollbackTargetVersion = computed(() => store.detail?.rollback.targetVersion ?? '')
const rollbackRequested = computed(
  () => detailRelease.value !== null && detailRelease.value.status === 'withdrawn',
)

onMounted(() => {
  void store.load()
})

function openCreateRelease(): void {
  store.clearMessages()
  editingRelease.value = null
  resetReleaseForm()
  formError.value = ''
  isReleaseFormOpen.value = true
}

function openEditRelease(release: OtaRelease): void {
  store.clearMessages()
  editingRelease.value = release
  releaseForm.firmwareVersion = release.firmwareVersion
  releaseForm.hardwareRevision = release.hardwareRevision
  releaseForm.channel = release.channel
  releaseForm.artifactKey = release.artifactKey
  releaseForm.artifactUrl = release.artifactUrl
  releaseForm.sha256 = release.sha256
  releaseForm.sizeBytes = release.sizeBytes
  releaseForm.signatureKeyId = release.signatureKeyId
  releaseForm.signatureAlgorithm = release.signatureAlgorithm
  releaseForm.signature = release.signature
  releaseForm.rollbackAllowed = release.rollbackAllowed
  releaseForm.minSourceVersion = release.minSourceVersion
  releaseForm.releaseNotes = release.releaseNotes
  releaseForm.targetType = release.targetType
  releaseForm.targetId = release.targetId
  releaseForm.canaryPercent = release.canaryPercent
  formError.value = ''
  isReleaseFormOpen.value = true
}

function closeReleaseForm(): void {
  if (store.isSubmitting) {
    return
  }
  isReleaseFormOpen.value = false
  editingRelease.value = null
  formError.value = ''
}

function resetReleaseForm(): void {
  releaseForm.firmwareVersion = ''
  releaseForm.hardwareRevision = ''
  releaseForm.channel = 'stable'
  releaseForm.artifactKey = ''
  releaseForm.artifactUrl = ''
  releaseForm.sha256 = ''
  releaseForm.sizeBytes = 0
  releaseForm.signatureKeyId = ''
  releaseForm.signatureAlgorithm = 'ed25519'
  releaseForm.signature = ''
  releaseForm.rollbackAllowed = false
  releaseForm.minSourceVersion = ''
  releaseForm.releaseNotes = ''
  releaseForm.targetType = 'all'
  releaseForm.targetId = ''
  releaseForm.canaryPercent = 0
}

async function submitReleaseForm(): Promise<void> {
  formError.value = validateReleaseForm()
  if (formError.value !== '') {
    return
  }
  const payload: CreateOtaReleaseInput = {
    firmwareVersion: releaseForm.firmwareVersion.trim(),
    hardwareRevision: releaseForm.hardwareRevision.trim(),
    channel: releaseForm.channel,
    artifactKey: releaseForm.artifactKey.trim(),
    artifactUrl: releaseForm.artifactUrl.trim(),
    sha256: releaseForm.sha256.trim().toLowerCase(),
    sizeBytes: Math.max(0, Math.trunc(releaseForm.sizeBytes)),
    signatureKeyId: releaseForm.signatureKeyId.trim(),
    signatureAlgorithm: releaseForm.signatureAlgorithm,
    signature: releaseForm.signature.trim(),
    rollbackAllowed: releaseForm.rollbackAllowed,
    minSourceVersion: releaseForm.minSourceVersion.trim(),
    releaseNotes: releaseForm.releaseNotes.trim(),
    targetType: releaseForm.targetType,
    targetId: releaseForm.targetId.trim(),
    canaryPercent: clampPercentage(releaseForm.canaryPercent),
  }
  const succeeded =
    editingRelease.value === null
      ? await store.createRelease(payload)
      : await store.updateRelease(editingRelease.value.releaseId, {
          ...payload,
          expectedVersion: editingRelease.value.recordVersion,
        })
  if (succeeded) {
    closeReleaseForm()
  }
}

function validateReleaseForm(): string {
  if (!releaseForm.firmwareVersion.trim()) {
    return '请填写固件版本号。'
  }
  if (!releaseForm.artifactUrl.trim()) {
    return '请选择或填写固件下载地址。'
  }
  if (!/^https?:\/\//i.test(releaseForm.artifactUrl.trim())) {
    return '下载地址需要以 http:// 或 https:// 开头。'
  }
  if (!/^[a-fA-F0-9]{64}$/.test(releaseForm.sha256.trim())) {
    return 'SHA-256 校验值需要 64 位十六进制字符。'
  }
  if (!releaseForm.signatureKeyId.trim()) {
    return '请填写签名密钥编号，便于设备确认固件来源。'
  }
  if (!releaseForm.signature.trim()) {
    return '请填写固件签名，平台发布前会验证签名。'
  }
  if (releaseForm.sizeBytes <= 0) {
    return '文件大小需要大于 0。'
  }
  if (releaseForm.artifactKey.trim() === '') {
    return '请填写固件对象键。'
  }
  if (releaseForm.targetType !== 'all' && releaseForm.targetId.trim() === '') {
    return '请填写目标设备或设备分组。'
  }
  if (
    !Number.isFinite(releaseForm.canaryPercent) ||
    releaseForm.canaryPercent < 0 ||
    releaseForm.canaryPercent > 100
  ) {
    return '灰度比例需要在 0 到 100 之间。'
  }
  return ''
}

function openUpload(): void {
  store.clearMessages()
  uploadForm.file = null
  uploadForm.filename = ''
  uploadForm.firmwareVersion = releaseForm.firmwareVersion
  uploadForm.channel = releaseForm.channel
  uploadForm.hardwareRevision = releaseForm.hardwareRevision
  uploadForm.overwrite = false
  uploadFormError.value = ''
  void store.loadFirmwareFiles()
  isUploadOpen.value = true
}

function closeUpload(): void {
  if (store.uploading) {
    return
  }
  isUploadOpen.value = false
  uploadFormError.value = ''
}

function selectUploadFile(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLInputElement)) {
    return
  }
  const file = target.files?.[0] ?? null
  uploadForm.file = file
  if (file !== null) {
    uploadForm.filename = file.name
    if (!uploadForm.firmwareVersion.trim()) {
      uploadForm.firmwareVersion = versionFromFileName(file.name)
    }
  }
}

async function submitUpload(): Promise<void> {
  uploadFormError.value = ''
  if (uploadForm.file === null) {
    uploadFormError.value = '请选择要上传的固件文件。'
    return
  }
  if (!uploadForm.filename.trim()) {
    uploadFormError.value = '请填写固件文件名。'
    return
  }
  if (!uploadForm.firmwareVersion.trim()) {
    uploadFormError.value = '请填写固件版本号。'
    return
  }
  const uploaded = await store.uploadFirmware({
    file: uploadForm.file,
    filename: uploadForm.filename.trim(),
    firmwareVersion: uploadForm.firmwareVersion.trim(),
    channel: uploadForm.channel,
    hardwareRevision: uploadForm.hardwareRevision.trim(),
    overwrite: uploadForm.overwrite,
  })
  if (uploaded === null) {
    return
  }
  applyFirmwareFile(uploaded)
  closeUpload()
}

function openFilePicker(): void {
  void store.loadFirmwareFiles()
  isFilePickerOpen.value = true
}

function selectFirmwareFile(file: DownloadFile): void {
  applyFirmwareFile(file)
  isFilePickerOpen.value = false
}

function applyFirmwareFile(file: DownloadFile): void {
  releaseForm.artifactUrl = file.downloadUrl
  releaseForm.artifactKey = file.relativePath
  releaseForm.sha256 = file.sha256
  releaseForm.sizeBytes = file.sizeBytes
  if (!releaseForm.firmwareVersion) {
    releaseForm.firmwareVersion = file.version
  }
}

function requestAction(kind: OtaActionKind, release: OtaRelease): void {
  store.clearMessages()
  pendingAction.value = { kind, release }
}

function closeActionDialog(): void {
  if (store.isSubmitting) {
    return
  }
  pendingAction.value = null
}

async function confirmAction(): Promise<void> {
  const action = pendingAction.value
  if (action === null) {
    return
  }
  let succeeded = false
  if (action.kind === 'publish') {
    succeeded = await store.publishRelease(action.release)
  } else if (action.kind === 'pause') {
    succeeded = await store.pauseRelease(action.release)
  } else if (action.kind === 'withdraw') {
    succeeded = await store.withdrawRelease(action.release)
  } else if (action.kind === 'rollback') {
    succeeded = await store.rollbackRelease(action.release)
  }
  if (succeeded) {
    closeActionDialog()
  }
}

function openDetail(release: OtaRelease): void {
  store.clearMessages()
  store.selectRelease(release)
  isDetailOpen.value = true
}

function closeDetail(): void {
  isDetailOpen.value = false
  store.closeRelease()
}

async function loadDeploymentPage(page: number): Promise<void> {
  await store.loadDeploymentPage(page)
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB']
  const exponent = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  const scaled = value / 1024 ** exponent
  const precision = exponent === 0 || scaled >= 100 ? 1 : 2
  return `${scaled.toFixed(precision)} ${units[exponent]}`
}

function formatTime(value: string): string {
  if (!value) {
    return '尚未记录'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '尚未记录'
  }
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function sha256Summary(value: string): string {
  if (!value) {
    return '缺少校验值'
  }
  return value.length > 22 ? `${value.slice(0, 22)}…` : value
}

function rolloutLabel(release: OtaRelease): string {
  if (release.status === 'published' && release.canaryPercent > 0) {
    return `灰度 ${release.canaryPercent}%`
  }
  return release.canaryPercent > 0 ? `${release.canaryPercent}%` : '等待发布'
}

function targetLabel(release: OtaRelease): string {
  const target = release.targetId.trim()
  if (release.targetType !== 'all' && target) {
    return `${otaTargetTypeLabel(release.targetType)}：${target}`
  }
  return otaTargetTypeLabel(release.targetType)
}

function actionTitle(action: OtaActionKind): string {
  return {
    publish: '发布这个固件版本？',
    pause: '暂停这个版本的灰度发布？',
    withdraw: '撤回这个固件版本？',
    rollback: '回滚这个固件版本？',
  }[action]
}

function actionDescription(action: OtaActionKind, release: OtaRelease): string {
  const version = release.firmwareVersion || '该版本'
  return {
    publish: `发布后，${targetLabel(release)} 会按 ${release.canaryPercent || 100}% 的比例收到 ${version} 的更新任务。`,
    pause: `暂停后不会再向新的设备下发 ${version}，已经开始的设备升级不会被强制中断。`,
    withdraw: `撤回后 ${version} 会从可选版本中移除，后续设备不会再收到这个版本。`,
    rollback: `回滚会让已经安装 ${version} 的设备回到上一个稳定版本。设备可能在重启后短暂不可用。`,
  }[action]
}

function actionConfirmLabel(action: OtaActionKind): string {
  return {
    publish: '确认发布',
    pause: '确认暂停',
    withdraw: '确认撤回',
    rollback: '确认回滚',
  }[action]
}

function actionIsDangerous(action: OtaActionKind): boolean {
  return action === 'withdraw' || action === 'rollback'
}

function actionCanSubmit(): boolean {
  return pendingAction.value !== null && !store.isSubmitting
}

function statusClass(value: string): string {
  return value === 'rolled_back' ? 'rolled-back' : value
}

function clampPercentage(value: number): number {
  return Math.min(100, Math.max(0, Math.round(value)))
}

function versionFromFileName(filename: string): string {
  const match = /(\d+\.\d+\.\d+(?:[-.][0-9A-Za-z.-]+)?)/.exec(filename)
  return match?.[1] ?? ''
}

function deploymentProgress(deployment: OtaDeployment): number {
  return clampPercentage(deployment.progressPercent)
}
</script>

<template>
  <section class="ota-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">设备固件</p>
        <h1>设备 OTA</h1>
        <p class="page-description">
          管理固件版本、灰度范围、签名校验和设备升级进度。发布前会保留完整清单，失败设备可以单独重试。
        </p>
      </div>
      <div class="header-actions">
        <button type="button" class="secondary" :disabled="store.isLoading" @click="store.load">
          {{ store.isLoading ? '正在刷新…' : '刷新列表' }}
        </button>
        <button type="button" class="primary" @click="openCreateRelease">
          {{ isEditing ? '继续编辑' : '新建固件版本' }}
        </button>
      </div>
    </header>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="success-message" role="status">
        {{ store.lastMessage }}
      </p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.error" class="error-message" role="alert">
        {{ store.error.message }}
        <button type="button" class="text-button" @click="store.load">重新加载</button>
      </p>
    </Transition>

    <div v-if="store.statisticsDegraded" class="degraded-banner" role="status">
      <span aria-hidden="true">☆</span>
      <div>
        <strong>统计信息暂时没有更新</strong>
        <p>升级任务和版本列表仍然可以继续操作，刷新后会重新读取统计。</p>
      </div>
      <button type="button" class="text-button" @click="store.load">重新读取</button>
    </div>

    <div class="summary-grid" aria-label="设备 OTA 概览">
      <div>
        <span>已发布版本</span>
        <strong>{{ store.statistics.publishedReleaseCount }}</strong>
      </div>
      <div :class="{ attention: store.statistics.canaryReleaseCount > 0 }">
        <span>正在灰度</span>
        <strong>{{ store.statistics.canaryReleaseCount }}</strong>
      </div>
      <div :class="{ attention: store.statistics.activeDeploymentCount > 0 }">
        <span>升级进行中</span>
        <strong>{{ store.statistics.activeDeploymentCount }}</strong>
      </div>
      <div>
        <span>升级成功</span>
        <strong>{{ store.statistics.succeededDeploymentCount }}</strong>
      </div>
      <div :class="{ danger: store.statistics.failedDeploymentCount > 0 }">
        <span>升级失败</span>
        <strong>{{ store.statistics.failedDeploymentCount }}</strong>
      </div>
      <div>
        <span>回滚率</span>
        <strong>{{ formatOtaRate(store.statistics.rollbackRate) }}</strong>
      </div>
    </div>

    <section class="filter-panel" aria-label="固件版本筛选">
      <div class="filter-heading">
        <div>
          <h2>版本筛选</h2>
          <p>按发布状态、渠道和版本号查看设备固件版本。</p>
        </div>
        <span class="record-count">共 {{ store.releasesTotal }} 个版本</span>
      </div>
      <div class="filter-grid">
        <label>
          <span>发布状态</span>
          <select v-model="store.filters.status">
            <option value="all">全部状态</option>
            <option value="draft">草稿</option>
            <option value="published">已发布</option>
            <option value="paused">已暂停</option>
            <option value="withdrawn">已撤回</option>
            <option value="rolled_back">已回滚</option>
          </select>
        </label>
        <label>
          <span>发布渠道</span>
          <select v-model="store.filters.channel">
            <option value="all">全部渠道</option>
            <option value="stable">稳定渠道</option>
            <option value="canary">灰度渠道</option>
            <option value="internal">内部渠道</option>
          </select>
        </label>
        <label>
          <span>固件版本</span>
          <input
            v-model.trim="store.filters.firmwareVersion"
            type="search"
            placeholder="例如 1.2.0"
          />
        </label>
        <label>
          <span>每页数量</span>
          <select v-model.number="store.filters.pageSize" @change="store.applyFilters">
            <option :value="10">10 条</option>
            <option :value="20">20 条</option>
            <option :value="50">50 条</option>
          </select>
        </label>
      </div>
      <div class="filter-actions">
        <button
          type="button"
          class="secondary"
          :disabled="store.isLoading"
          @click="store.clearFilters"
        >
          清除筛选
        </button>
        <button
          type="button"
          class="primary"
          :disabled="store.isLoading"
          @click="store.applyFilters"
        >
          {{ store.isLoading ? '正在查询…' : '查询版本' }}
        </button>
      </div>
    </section>

    <Transition name="page" mode="out-in">
      <div :key="store.isLoading ? 'loading' : store.releases.length === 0 ? 'empty' : 'content'">
        <div v-if="store.isLoading" class="state-panel">
          <span class="state-spinner" aria-hidden="true"></span>
          正在读取设备固件版本…
        </div>
        <div v-else-if="store.releases.length === 0" class="state-panel">
          <span class="state-icon" aria-hidden="true">☆</span>
          <div>
            <strong>还没有设备固件版本</strong>
            <p>先上传固件文件，再登记第一版后即可开始灰度发布。</p>
          </div>
        </div>
        <div v-else key="content">
          <div class="table-shell">
            <div class="table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>版本</th>
                    <th>渠道</th>
                    <th>状态</th>
                    <th>灰度 / 目标</th>
                    <th>签名</th>
                    <th>文件</th>
                    <th>发布时间</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="release in store.releases"
                    :key="release.releaseId"
                    :data-release-id="release.releaseId"
                  >
                    <td>
                      <button
                        type="button"
                        class="version-link"
                        :aria-label="`查看 ${release.firmwareVersion} 详情`"
                        @click="openDetail(release)"
                      >
                        <strong>{{ release.firmwareVersion }}</strong>
                        <small>{{ release.hardwareRevision || '通用硬件版本' }}</small>
                      </button>
                    </td>
                    <td class="actions-cell">
                      <span :class="['channel-pill', release.channel]">
                        {{ otaReleaseChannelLabel(release.channel) }}
                      </span>
                    </td>
                    <td>
                      <span :class="['status', statusClass(release.status)]">
                        {{ otaReleaseStatusLabel(release.status) }}
                      </span>
                    </td>
                    <td>
                      <strong class="rollout">{{ rolloutLabel(release) }}</strong>
                      <small class="target-copy">{{ targetLabel(release) }}</small>
                    </td>
                    <td>
                      <span :class="['signature', release.signatureStatus]">
                        {{ otaSignatureStatusLabel(release.signatureStatus) }}
                      </span>
                      <small class="key-copy">{{
                        release.signatureKeyId || '未填写密钥编号'
                      }}</small>
                    </td>
                    <td>
                      <span class="file-copy">{{ release.artifactKey || '对象键未填写' }}</span>
                      <small class="size-copy">
                        {{ formatBytes(release.sizeBytes) }} ·
                        <code :title="release.sha256">{{ sha256Summary(release.sha256) }}</code>
                      </small>
                    </td>
                    <td class="time-cell">
                      {{ formatTime(release.publishedAt || release.updatedAt) }}
                    </td>
                    <td>
                      <div class="row-actions">
                        <button type="button" class="compact" @click="openDetail(release)">
                          详情
                        </button>
                        <button
                          v-if="release.status === 'draft' || release.status === 'paused'"
                          type="button"
                          class="compact"
                          :disabled="store.isSubmitting"
                          @click="openEditRelease(release)"
                        >
                          编辑
                        </button>
                        <button
                          v-if="release.status === 'draft' || release.status === 'paused'"
                          type="button"
                          class="compact primary"
                          :disabled="store.isSubmitting"
                          @click="requestAction('publish', release)"
                        >
                          发布
                        </button>
                        <button
                          v-if="release.status === 'published'"
                          type="button"
                          class="compact"
                          :disabled="store.isSubmitting"
                          @click="requestAction('pause', release)"
                        >
                          暂停
                        </button>
                        <button
                          v-if="release.status === 'published' || release.status === 'paused'"
                          type="button"
                          class="compact danger"
                          :disabled="store.isSubmitting"
                          @click="requestAction('withdraw', release)"
                        >
                          撤回
                        </button>
                        <button
                          v-if="release.status === 'published' && release.rollbackAllowed"
                          type="button"
                          class="compact danger"
                          :disabled="store.isSubmitting"
                          @click="requestAction('rollback', release)"
                        >
                          回滚
                        </button>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <footer class="pagination">
              <p>第 {{ store.filters.page }} / {{ store.pageCount }} 页</p>
              <div class="pagination-actions">
                <button
                  type="button"
                  class="secondary"
                  :disabled="store.filters.page <= 1 || store.isLoading"
                  @click="store.goToPage(store.filters.page - 1)"
                >
                  上一页
                </button>
                <button
                  type="button"
                  class="secondary"
                  :disabled="store.filters.page >= store.pageCount || store.isLoading"
                  @click="store.goToPage(store.filters.page + 1)"
                >
                  下一页
                </button>
              </div>
            </footer>
          </div>
        </div>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="isReleaseFormOpen" class="dialog-backdrop" @click.self="closeReleaseForm">
        <form
          class="dialog"
          role="dialog"
          aria-modal="true"
          :aria-label="releaseFormTitle"
          @submit.prevent="submitReleaseForm"
        >
          <p class="eyebrow">固件发布</p>
          <h2>{{ releaseFormTitle }}</h2>
          <p class="dialog-hint">
            固件发布依赖下载服务器上的真实文件。选择文件后会自动填入地址、校验值和大小，发布前请再确认一次。
          </p>
          <div class="artifact-toolbar">
            <button type="button" class="secondary" @click="openFilePicker">
              从下载服务器选择固件
            </button>
            <button type="button" class="secondary" :disabled="store.uploading" @click="openUpload">
              上传新固件
            </button>
            <span>仅显示已上传的固件文件。</span>
          </div>
          <div class="field-grid">
            <label>
              <span>固件版本号</span>
              <input
                v-model.trim="releaseForm.firmwareVersion"
                type="text"
                required
                placeholder="例如 1.2.0"
              />
            </label>
            <label>
              <span>硬件版本</span>
              <input
                v-model.trim="releaseForm.hardwareRevision"
                type="text"
                placeholder="留空表示通用版本"
              />
            </label>
            <label>
              <span>发布渠道</span>
              <select v-model="releaseForm.channel">
                <option value="stable">稳定渠道</option>
                <option value="canary">灰度渠道</option>
                <option value="internal">内部渠道</option>
              </select>
            </label>
            <label>
              <span>目标范围</span>
              <select v-model="releaseForm.targetType">
                <option value="all">全部符合条件的设备</option>
                <option value="group">指定设备分组</option>
                <option value="device">指定设备</option>
              </select>
            </label>
            <label>
              <span>灰度比例</span>
              <input
                v-model.number="releaseForm.canaryPercent"
                type="number"
                min="0"
                max="100"
                step="1"
              />
            </label>
            <label class="span-two">
              <span>{{
                releaseForm.targetType === 'group' ? '设备分组编号' : '目标设备编号'
              }}</span>
              <input
                v-model.trim="releaseForm.targetId"
                type="text"
                :disabled="releaseForm.targetType === 'all'"
                :placeholder="
                  releaseForm.targetType === 'all'
                    ? '全部符合条件的设备'
                    : releaseForm.targetType === 'group'
                      ? '输入需要接收更新的设备分组编号'
                      : '输入需要接收更新的设备编号'
                "
              />
            </label>
            <label class="span-two">
              <span>固件下载地址</span>
              <input
                v-model.trim="releaseForm.artifactUrl"
                type="url"
                required
                placeholder="从下载服务器选择后自动填入"
              />
            </label>
            <label>
              <span>对象键</span>
              <input v-model.trim="releaseForm.artifactKey" type="text" required />
            </label>
            <label>
              <span>文件大小（字节）</span>
              <input v-model.number="releaseForm.sizeBytes" type="number" min="1" required />
            </label>
            <label class="span-two">
              <span>SHA-256 校验值</span>
              <input v-model.trim="releaseForm.sha256" type="text" required />
            </label>
            <label class="span-two">
              <span>签名密钥编号</span>
              <input
                v-model.trim="releaseForm.signatureKeyId"
                type="text"
                required
                placeholder="用于设备验证固件来源"
              />
            </label>
            <label class="span-two">
              <span>固件签名</span>
              <textarea
                v-model.trim="releaseForm.signature"
                rows="3"
                required
                placeholder="发布前平台会使用对应的公钥验证这段 detached signature"
              ></textarea>
            </label>
            <label class="span-two">
              <span>最低可升级版本</span>
              <input
                v-model.trim="releaseForm.minSourceVersion"
                type="text"
                placeholder="低于该版本时不提供此更新"
              />
            </label>
            <label class="span-two">
              <span>版本说明</span>
              <textarea
                v-model.trim="releaseForm.releaseNotes"
                rows="4"
                required
                placeholder="说明这次更新解决的问题和主要变化"
              ></textarea>
            </label>
          </div>
          <label class="checkbox-row">
            <input v-model="releaseForm.rollbackAllowed" type="checkbox" />
            <span>
              <strong>允许回滚</strong>
              <small>设备升级失败时可以使用上一个稳定版本恢复。</small>
            </span>
          </label>
          <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
          <div class="dialog-actions">
            <button
              type="button"
              class="secondary"
              :disabled="store.isSubmitting"
              @click="closeReleaseForm"
            >
              取消
            </button>
            <button type="submit" class="primary" :disabled="store.isSubmitting">
              {{ store.isSubmitting ? '正在保存…' : isEditing ? '保存修改' : '创建版本' }}
            </button>
          </div>
        </form>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="isUploadOpen" class="dialog-backdrop" @click.self="closeUpload">
        <form
          class="dialog"
          role="dialog"
          aria-modal="true"
          aria-label="上传固件文件"
          @submit.prevent="submitUpload"
        >
          <p class="eyebrow">上传固件</p>
          <h2>上传到下载服务器</h2>
          <p class="dialog-hint">
            上传完成后会把文件信息填入发布表单，仍需创建版本后才能下发。已有同名文件时需要确认替换。
          </p>
          <div class="field-grid">
            <label class="span-two">
              <span>固件文件</span>
              <input type="file" required @change="selectUploadFile" />
            </label>
            <label>
              <span>文件名</span>
              <input v-model.trim="uploadForm.filename" type="text" required />
            </label>
            <label>
              <span>固件版本号</span>
              <input v-model.trim="uploadForm.firmwareVersion" type="text" required />
            </label>
            <label>
              <span>发布渠道</span>
              <select v-model="uploadForm.channel">
                <option value="stable">稳定渠道</option>
                <option value="canary">灰度渠道</option>
                <option value="internal">内部渠道</option>
              </select>
            </label>
            <label>
              <span>硬件版本</span>
              <input v-model.trim="uploadForm.hardwareRevision" type="text" />
            </label>
            <label class="checkbox-row span-two">
              <input v-model="uploadForm.overwrite" type="checkbox" />
              <span>允许替换同名文件</span>
            </label>
          </div>
          <div v-if="store.uploading || store.uploadProgress > 0" class="upload-status">
            <div>
              <strong>{{ uploadForm.filename || '固件文件' }}</strong>
              <span>{{ store.uploadProgress }}%</span>
            </div>
            <progress :value="store.uploadProgress" max="100"></progress>
          </div>
          <p v-if="uploadFormError" class="form-error" role="alert">{{ uploadFormError }}</p>
          <div class="dialog-actions">
            <button
              type="button"
              class="secondary"
              :disabled="store.uploading"
              @click="closeUpload"
            >
              取消
            </button>
            <button
              type="submit"
              class="primary"
              :disabled="store.uploading || store.uploadProgress >= 100"
            >
              {{ store.uploading ? '正在上传…' : '上传固件' }}
            </button>
          </div>
        </form>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="isFilePickerOpen" class="dialog-backdrop" @click.self="isFilePickerOpen = false">
        <section
          class="dialog file-picker-dialog"
          role="dialog"
          aria-modal="true"
          aria-label="选择固件文件"
        >
          <div class="dialog-heading">
            <div>
              <p class="eyebrow">下载服务器</p>
              <h2>选择固件文件</h2>
            </div>
            <button
              type="button"
              class="text-button"
              :disabled="store.isLoadingFiles"
              @click="store.loadFirmwareFiles()"
            >
              {{ store.isLoadingFiles ? '正在刷新…' : '刷新文件' }}
            </button>
          </div>
          <p class="dialog-hint">选择后会自动填入下载地址、SHA-256 和文件大小。</p>
          <div v-if="store.isLoadingFiles" class="state-panel compact-state">
            <span class="state-spinner" aria-hidden="true"></span>
            正在读取固件文件…
          </div>
          <div
            v-else-if="store.firmwareDownloadFiles.length === 0"
            class="state-panel compact-state"
          >
            <span class="state-icon" aria-hidden="true">☆</span>
            <span>下载服务器上还没有固件文件，可以先上传新固件。</span>
          </div>
          <ul v-else class="file-list">
            <li v-for="file in store.firmwareDownloadFiles" :key="file.relativePath">
              <div>
                <strong>{{ file.name }}</strong>
                <small>{{ file.relativePath }}</small>
                <small>{{ formatBytes(file.sizeBytes) }} · {{ sha256Summary(file.sha256) }}</small>
              </div>
              <button type="button" class="secondary compact" @click="selectFirmwareFile(file)">
                选择
              </button>
            </li>
          </ul>
          <div class="dialog-actions">
            <button type="button" class="secondary" @click="isFilePickerOpen = false">关闭</button>
          </div>
        </section>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="pendingAction" class="dialog-backdrop" @click.self="closeActionDialog">
        <section class="dialog confirm-dialog" role="dialog" aria-modal="true">
          <p class="eyebrow">需要确认</p>
          <h2>{{ actionTitle(pendingAction.kind) }}</h2>
          <p>{{ actionDescription(pendingAction.kind, pendingAction.release) }}</p>
          <div class="dialog-actions">
            <button
              type="button"
              class="secondary"
              :disabled="store.isSubmitting"
              @click="closeActionDialog"
            >
              取消
            </button>
            <button
              type="button"
              :class="actionIsDangerous(pendingAction.kind) ? 'danger-button' : 'primary'"
              :disabled="!actionCanSubmit()"
              @click="confirmAction"
            >
              {{ store.isSubmitting ? '正在处理…' : actionConfirmLabel(pendingAction.kind) }}
            </button>
          </div>
        </section>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="isDetailOpen" class="dialog-backdrop" @click.self="closeDetail">
        <section
          class="dialog detail-dialog"
          role="dialog"
          aria-modal="true"
          aria-label="固件版本详情"
        >
          <div class="dialog-heading">
            <div>
              <p class="eyebrow">版本详情</p>
              <h2>{{ detailRelease?.firmwareVersion || '正在读取版本…' }}</h2>
              <p v-if="detailRelease" class="detail-subtitle">
                {{ otaReleaseChannelLabel(detailRelease.channel) }} ·
                {{ otaReleaseStatusLabel(detailRelease.status) }} ·
                {{ detailRelease.hardwareRevision || '通用硬件版本' }}
              </p>
            </div>
            <button type="button" class="text-button" @click="closeDetail">关闭</button>
          </div>
          <div
            v-if="store.isLoadingDetail && store.detail === null"
            class="state-panel compact-state"
          >
            <span class="state-spinner" aria-hidden="true"></span>
            正在读取版本详情…
          </div>
          <template v-else-if="detailRelease">
            <dl class="detail-grid">
              <div>
                <dt>签名状态</dt>
                <dd>{{ otaSignatureStatusLabel(detailRelease.signatureStatus) }}</dd>
              </div>
              <div>
                <dt>签名密钥</dt>
                <dd>{{ detailRelease.signatureKeyId || '未填写' }}</dd>
              </div>
              <div>
                <dt>灰度比例</dt>
                <dd>{{ detailRelease.canaryPercent }}%</dd>
              </div>
              <div>
                <dt>目标范围</dt>
                <dd>{{ targetLabel(detailRelease) }}</dd>
              </div>
              <div>
                <dt>最低源版本</dt>
                <dd>{{ detailRelease.minSourceVersion || '不限制' }}</dd>
              </div>
              <div>
                <dt>允许回滚</dt>
                <dd>{{ detailRelease.rollbackAllowed ? '是' : '否' }}</dd>
              </div>
              <div>
                <dt>文件大小</dt>
                <dd>{{ formatBytes(detailRelease.sizeBytes) }}</dd>
              </div>
              <div>
                <dt>发布时间</dt>
                <dd>{{ formatTime(detailRelease.publishedAt) }}</dd>
              </div>
              <div class="span-two">
                <dt>下载地址</dt>
                <dd>
                  <code>{{ detailRelease.artifactUrl || '未填写' }}</code>
                </dd>
              </div>
              <div class="span-two">
                <dt>对象键</dt>
                <dd>
                  <code>{{ detailRelease.artifactKey || '未填写' }}</code>
                </dd>
              </div>
              <div class="span-two">
                <dt>SHA-256</dt>
                <dd>
                  <code>{{ detailRelease.sha256 || '未填写' }}</code>
                </dd>
              </div>
              <div class="span-two">
                <dt>固件签名</dt>
                <dd>
                  <code>{{ detailRelease.signature || '未填写' }}</code>
                </dd>
              </div>
              <div class="span-two">
                <dt>版本说明</dt>
                <dd class="notes">{{ detailRelease.releaseNotes || '没有填写版本说明。' }}</dd>
              </div>
            </dl>

            <section class="detail-section">
              <div class="section-heading">
                <h3>设备升级进度</h3>
                <span>{{ store.deploymentTotal }} 台设备</span>
              </div>
              <div class="deployment-filter">
                <label>
                  <span>状态筛选</span>
                  <select v-model="store.deploymentStatus" @change="store.applyDeploymentFilter">
                    <option value="all">全部状态</option>
                    <option value="queued">等待下发</option>
                    <option value="offered">已通知设备</option>
                    <option value="downloading">正在下载</option>
                    <option value="validating">正在校验</option>
                    <option value="installing">正在安装</option>
                    <option value="pending_verify">等待确认</option>
                    <option value="succeeded">升级成功</option>
                    <option value="failed">升级失败</option>
                    <option value="rolled_back">已回滚</option>
                  </select>
                </label>
              </div>
              <div v-if="store.visibleDeployments.length === 0" class="empty-copy">
                当前还没有设备升级记录。发布后设备领取任务时会显示在这里。
              </div>
              <div v-else class="table-scroll compact-table">
                <table>
                  <thead>
                    <tr>
                      <th>设备</th>
                      <th>硬件版本</th>
                      <th>状态</th>
                      <th>进度</th>
                      <th>失败信息</th>
                      <th>最近更新</th>
                      <th></th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr
                      v-for="deployment in store.visibleDeployments"
                      :key="deployment.deploymentId"
                    >
                      <td>
                        <strong>{{ deployment.deviceName || deployment.deviceId }}</strong>
                        <small>{{ deployment.deviceId }}</small>
                      </td>
                      <td>{{ deployment.hardwareRevision || '未记录' }}</td>
                      <td>
                        <span :class="['status', statusClass(deployment.status)]">
                          {{ otaDeploymentStatusLabel(deployment.status) }}
                        </span>
                      </td>
                      <td>
                        <div class="progress-cell">
                          <progress :value="deploymentProgress(deployment)" max="100"></progress>
                          <span>{{ deploymentProgress(deployment) }}%</span>
                        </div>
                      </td>
                      <td>
                        <span v-if="deployment.errorCode || deployment.errorMessage">
                          <strong class="failure-code">{{
                            deployment.errorCode || '升级未完成'
                          }}</strong>
                          <small>{{ deployment.errorMessage || '设备会在下次联网后重试。' }}</small>
                        </span>
                        <span v-else class="muted-copy">没有失败信息</span>
                      </td>
                      <td class="time-cell">{{ formatTime(deployment.updatedAt) }}</td>
                      <td>
                        <button
                          v-if="deployment.status === 'failed'"
                          type="button"
                          class="compact"
                          :disabled="store.isSubmitting"
                          @click="store.retryDeployment(deployment)"
                        >
                          重试
                        </button>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <div v-if="store.deploymentTotal > 0" class="pagination compact-pagination">
                <p>
                  第 {{ deploymentStart }}–{{ deploymentEnd }} 条，共 {{ store.deploymentTotal }} 条
                </p>
                <div class="pagination-actions">
                  <button
                    type="button"
                    class="secondary"
                    :disabled="!canGoPreviousDeploymentPage"
                    @click="loadDeploymentPage(store.deploymentPage - 1)"
                  >
                    上一页
                  </button>
                  <span>第 {{ store.deploymentPage }} / {{ store.deploymentPageCount }} 页</span>
                  <button
                    type="button"
                    class="secondary"
                    :disabled="!canGoNextDeploymentPage"
                    @click="loadDeploymentPage(store.deploymentPage + 1)"
                  >
                    下一页
                  </button>
                </div>
              </div>
            </section>

            <section class="detail-section">
              <div class="section-heading">
                <h3>版本历史</h3>
                <span>{{ store.detail?.versions.length ?? 0 }} 个版本</span>
              </div>
              <div v-if="(store.detail?.versions.length ?? 0) === 0" class="empty-copy">
                当前没有可对照的历史版本。
              </div>
              <div v-else class="table-scroll compact-table">
                <table>
                  <thead>
                    <tr>
                      <th>版本</th>
                      <th>渠道</th>
                      <th>状态</th>
                      <th>签名</th>
                      <th>发布时间</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="version in store.detail?.versions ?? []" :key="version.releaseId">
                      <td>
                        <button type="button" class="version-link" @click="openDetail(version)">
                          <strong>{{ version.firmwareVersion }}</strong>
                          <small>{{ version.hardwareRevision || '通用硬件版本' }}</small>
                        </button>
                      </td>
                      <td>{{ otaReleaseChannelLabel(version.channel) }}</td>
                      <td>{{ otaReleaseStatusLabel(version.status) }}</td>
                      <td>{{ otaSignatureStatusLabel(version.signatureStatus) }}</td>
                      <td class="time-cell">
                        {{ formatTime(version.publishedAt || version.updatedAt) }}
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <div v-if="detailRelease.rollbackAllowed" class="rollback-summary">
                <strong>可回滚版本</strong>
                <span>{{ rollbackTargetVersion || '发布时未指定回滚目标' }}</span>
                <small v-if="rollbackRequested">回滚请求已提交，等待设备执行。</small>
              </div>
            </section>
          </template>
        </section>
      </div>
    </Transition>
  </section>
</template>

<style scoped>
.ota-page {
  min-width: 0;
}

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
h3,
p {
  overflow-wrap: anywhere;
}

h1 {
  margin: 0;
  color: var(--sprout-text);
  font-size: 28px;
}

.page-description,
.dialog-hint,
.detail-subtitle {
  max-width: 760px;
  margin: 10px 0 0;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

.header-actions,
.dialog-actions,
.pagination-actions,
.row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
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

button.compact {
  min-height: 32px;
  padding: 0 11px;
  border-radius: 12px;
  font-size: 12px;
}

button.text-button {
  min-height: 30px;
  padding: 0 10px;
  border-radius: 10px;
  background: #fff7fa;
  font-size: 12px;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.success-message,
.error-message {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 14px 0 0;
  padding: 12px 15px;
  border-radius: 15px;
}

.success-message {
  border: 1px solid #bfe3cb;
  background: #f2fbf5;
  color: #1d6b3f;
}

.error-message {
  border: 1px solid #efb4c5;
  background: #fff7fa;
  color: #a12b4a;
}

.degraded-banner {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 14px;
  margin-top: 14px;
  padding: 14px 16px;
  border: 1px solid #f3d18e;
  border-radius: 18px;
  background: #fffaf0;
  color: #6b4f2a;
}

.degraded-banner > span {
  display: grid;
  width: 30px;
  height: 30px;
  place-items: center;
  border-radius: 11px;
  background: #ffe9ad;
  color: #8a5a00;
  font-size: 18px;
}

.degraded-banner p {
  margin: 4px 0 0;
  font-size: 13px;
  line-height: 1.5;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
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

.summary-grid .danger {
  border-color: #efb4c5;
  background: #fff3f5;
}

.filter-panel,
.table-shell,
.state-panel,
.detail-section {
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.filter-panel {
  margin-top: 18px;
  overflow: hidden;
}

.filter-heading,
.section-heading,
.dialog-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
}

.filter-heading {
  padding: 18px 20px;
  border-bottom: 1px solid var(--sprout-outline-soft);
}

.filter-heading h2,
.filter-heading p,
.section-heading h3 {
  margin: 0;
}

.filter-heading h2 {
  color: var(--sprout-text);
  font-size: 17px;
}

.filter-heading p {
  margin-top: 4px;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.record-count {
  flex: 0 0 auto;
  padding: 5px 10px;
  border-radius: 999px;
  background: #fff2f5;
  color: #b23a68;
  font-size: 12px;
  font-weight: 700;
}

.filter-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
  padding: 18px 20px 0;
}

.filter-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  padding: 16px 20px 20px;
}

label {
  display: grid;
  gap: 7px;
  color: var(--sprout-text);
  font-size: 13px;
  font-weight: 700;
}

input,
select,
textarea {
  width: 100%;
  min-height: 42px;
  padding: 0 13px;
  border: 1px solid #f0bdcb;
  border-radius: 14px;
  background: #fff8fa;
  color: var(--sprout-text);
  font: inherit;
  transition:
    border-color var(--sprout-duration-fast) ease,
    box-shadow var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease;
}

textarea {
  min-height: 92px;
  padding: 11px 13px;
  resize: vertical;
}

input:focus,
select:focus,
textarea:focus {
  border-color: #d94f83;
  background: #ffffff;
  box-shadow: 0 0 0 3px rgb(217 79 131 / 12%);
  outline: none;
}

.table-shell {
  margin-top: 18px;
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

td.actions-cell {
  min-width: 188px;
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

.version-link {
  display: grid;
  min-width: 120px;
  gap: 4px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--sprout-text);
  text-align: left;
}

.version-link strong {
  color: var(--sprout-pink-deep);
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 14px;
}

.version-link small,
td small {
  display: block;
  color: var(--sprout-text-muted);
  font-size: 12px;
  line-height: 1.45;
}

.channel-pill,
.signature,
.status {
  display: inline-flex;
  width: fit-content;
  padding: 5px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.channel-pill.stable {
  background: #e7f8ee;
  color: #1d6b3f;
}

.channel-pill.canary {
  background: #fff3cd;
  color: #755600;
}

.channel-pill.internal {
  background: #eaf5ff;
  color: #27627f;
}

.status.draft {
  background: #f5eef1;
  color: #6b4f5a;
}

.status.published,
.status.succeeded {
  background: #e7f8ee;
  color: #1d6b3f;
}

.status.paused,
.status.queued,
.status.offered,
.status.validating,
.status.pending_verify,
.status.downloading,
.status.installing {
  background: #eaf5ff;
  color: #27627f;
}

.status.withdrawn,
.status.rolled-back,
.status.failed {
  background: #fff0f2;
  color: #a12b4a;
}

.signature.verified {
  background: #e7f8ee;
  color: #1d6b3f;
}

.signature.pending {
  background: #fff3cd;
  color: #755600;
}

.signature.failed,
.signature.missing {
  background: #fff0f2;
  color: #a12b4a;
}

.rollout {
  display: block;
  color: var(--sprout-text);
}

.target-copy,
.key-copy,
.size-copy,
.time-cell,
.muted-copy {
  white-space: nowrap;
}

.file-copy {
  display: block;
  min-width: 160px;
  overflow-wrap: anywhere;
}

code,
pre {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
}

code {
  color: #5b3b49;
  font-size: 12px;
  overflow-wrap: anywhere;
}

.pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 16px;
  background: #fffbfc;
}

.pagination p,
.pagination span {
  margin: 0;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.state-panel {
  display: flex;
  min-height: 156px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-top: 18px;
  padding: 28px;
  color: var(--sprout-text-muted);
  text-align: center;
}

.state-panel strong,
.state-panel p {
  display: block;
  margin: 0;
}

.state-panel strong {
  color: var(--sprout-text);
}

.state-panel p {
  margin-top: 5px;
}

.compact-state {
  min-height: 120px;
  margin-top: 0;
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
  width: min(100%, 680px);
  max-height: min(880px, calc(100vh - 40px));
  gap: 15px;
  overflow: auto;
  padding: 28px;
  border: 1px solid var(--sprout-outline);
  border-radius: 28px;
  background: #ffffff;
  box-shadow: 0 28px 72px rgb(74 46 59 / 22%);
}

.detail-dialog {
  width: min(100%, 1120px);
}

.dialog h2,
.dialog p,
.dialog h3 {
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

.artifact-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 16px;
  background: #fffbfc;
}

.artifact-toolbar span {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.field-grid,
.detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.field-grid .span-two,
.detail-grid .span-two {
  grid-column: 1 / -1;
}

.checkbox-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 13px 14px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 16px;
  background: #fffbfc;
}

.checkbox-row input {
  width: 20px;
  min-width: 20px;
  min-height: 20px;
  accent-color: #d94f83;
}

.checkbox-row span {
  display: grid;
  gap: 3px;
}

.checkbox-row strong {
  color: var(--sprout-text);
}

.checkbox-row small {
  color: var(--sprout-text-muted);
}

.form-error {
  padding: 11px 13px;
  border: 1px solid #efb4c5;
  border-radius: 14px;
  background: #fff7fa;
  color: #a12b4a;
  font-size: 13px;
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

.file-list,
.audit-list {
  display: grid;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.file-list li,
.audit-list li {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  padding: 13px 14px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 16px;
  background: #fffbfc;
}

.file-list li div,
.audit-list li div {
  display: grid;
  min-width: 0;
  gap: 4px;
}

.file-list small,
.audit-list small {
  color: var(--sprout-text-muted);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.audit-list li > span {
  max-width: 420px;
  color: var(--sprout-text-muted);
  font-size: 13px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.detail-subtitle {
  margin-top: 6px;
}

.detail-grid {
  margin: 0;
  padding: 4px 0 0;
}

.detail-grid > div {
  min-width: 0;
  padding: 12px 14px;
  border-radius: 15px;
  background: #fffbfc;
}

.detail-grid dt {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.detail-grid dd {
  margin: 5px 0 0;
  color: var(--sprout-text);
  overflow-wrap: anywhere;
}

.detail-grid .notes {
  white-space: pre-wrap;
}

.detail-grid pre {
  max-height: 220px;
  margin: 0;
  overflow: auto;
  padding: 12px;
  border-radius: 12px;
  background: #35232b;
  color: #ffeff5;
  font-size: 12px;
  line-height: 1.55;
  white-space: pre-wrap;
}

.detail-section {
  display: grid;
  gap: 12px;
  padding: 16px;
  box-shadow: none;
}

.section-heading {
  align-items: center;
}

.section-heading h3 {
  color: var(--sprout-text);
  font-size: 16px;
}

.section-heading span {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.deployment-filter {
  display: flex;
  justify-content: flex-end;
}

.deployment-filter label {
  width: min(220px, 100%);
}

.empty-copy {
  padding: 16px;
  border-radius: 15px;
  background: #fffbfc;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.rollback-summary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px 14px;
  padding: 13px 14px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 16px;
  background: #fffbfc;
}

.rollback-summary strong {
  color: var(--sprout-text);
}

.rollback-summary span {
  color: #b23a68;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
}

.rollback-summary small {
  color: var(--sprout-text-muted);
}

.compact-table table {
  min-width: 920px;
}

.progress-cell {
  display: grid;
  min-width: 100px;
  gap: 5px;
}

.progress-cell span {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.version-cell {
  color: #7b5263;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  white-space: nowrap;
}

.failure-code {
  color: #a12b4a;
}

.compact-pagination {
  padding: 4px 0 0;
  background: transparent;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1180px) {
  .summary-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 820px) {
  .page-header,
  .filter-heading,
  .dialog-heading,
  .section-heading,
  .pagination {
    align-items: stretch;
    flex-direction: column;
  }

  .header-actions,
  .filter-actions,
  .pagination-actions {
    justify-content: flex-start;
  }

  .degraded-banner {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .degraded-banner button {
    grid-column: 1 / -1;
  }

  .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .field-grid,
  .detail-grid {
    grid-template-columns: 1fr;
  }

  .field-grid .span-two,
  .detail-grid .span-two {
    grid-column: auto;
  }

  .actions-cell .row-actions button {
    flex: 1 1 90px;
  }
}

@media (max-width: 520px) {
  .summary-grid,
  .filter-grid {
    grid-template-columns: 1fr;
  }

  .dialog {
    padding: 22px;
    border-radius: 22px;
  }

  .file-list li,
  .audit-list li {
    flex-direction: column;
  }
}
</style>
