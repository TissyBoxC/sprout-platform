<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import type {
  ContentAgeTier,
  ContentCategory,
  ContentDraftInput,
  ContentPackage,
  ContentPackageCreateInput,
  ContentStatus,
  ContentVersion,
} from '@/api/adminContent'
import type { DownloadFile } from '@/api/adminDownloadFiles'
import {
  CONTENT_PAGE_SIZE,
  MAX_CONTENT_SIZE_BYTES,
  contentAgeTierLabel,
  contentCategoryLabel,
  contentReviewActionLabel,
  contentStatusLabel,
  isPackageIdValid,
  isSha256Valid,
  latestVersion,
  packageAgeTiers,
  packageCategory,
  packageTitle,
  useContentLibraryStore,
} from '@/features/content/application/contentLibraryStore'
import ContentAssetFields from '@/features/content/presentation/ContentAssetFields.vue'

type AssetSource = 'manual' | 'download'

interface DraftForm {
  packageId: string
  title: string
  category: ContentCategory
  ageTiers: ContentAgeTier[]
  assetSource: AssetSource
  assetKey: string
  sha256: string
  sizeBytes: number
}

const store = useContentLibraryStore()

const categories: ContentCategory[] = [
  'story',
  'nursery_rhyme',
  'poetry',
  'english',
  'encyclopedia',
  'bedtime',
]
const ageTiers: ContentAgeTier[] = ['age_3_4', 'age_5_6', 'age_7_8']
const statuses: ContentStatus[] = [
  'draft',
  'in_review',
  'published',
  'withdrawn',
  'archived',
]

const isCreateOpen = ref(false)
const isEditOpen = ref(false)
const selectedVersionValue = ref(0)
const rejectTarget = ref<ContentVersion | null>(null)
const rejectReason = ref('')
const rejectError = ref('')
const formErrors = reactive<Record<string, string>>({})

const createForm = reactive<DraftForm>(emptyDraftForm())
const editForm = reactive<DraftForm>(emptyDraftForm())

const keywordInput = ref('')

const activePackage = computed(() => store.selectedPackage)
const versionList = computed<ContentVersion[]>(() => activePackage.value?.versions ?? [])
const selectedVersion = computed<ContentVersion | null>(() => {
  const versions = versionList.value
  return (
    versions.find((version) => version.packageVersion === selectedVersionValue.value) ??
    versions[0] ??
    null
  )
})
const selectedHistory = computed(() => {
  const version = selectedVersion.value
  if (version === null || activePackage.value === null) {
    return []
  }
  // The package detail carries the full review log; narrow it to the version so
  // each tab shows only its own auditable decisions.
  return activePackage.value.history.filter(
    (entry) => entry.packageVersion === version.packageVersion,
  )
})
const selectedDownloadUrl = computed(() =>
  selectedVersion.value === null ? '' : store.downloadUrlForVersion(selectedVersion.value),
)
const selectableDownloadFiles = computed(() =>
  [...store.downloadFiles].sort((left, right) =>
    left.relativePath.localeCompare(right.relativePath),
  ),
)

onMounted(async () => {
  await store.load()
})

function openCreate(): void {
  store.clearMessages()
  Object.assign(createForm, emptyDraftForm())
  clearFormErrors()
  isCreateOpen.value = true
}

function closeCreate(): void {
  isCreateOpen.value = false
}

async function submitCreate(): Promise<void> {
  if (!validateDraft(createForm)) {
    return
  }
  const input: ContentPackageCreateInput = {
    packageId: createForm.packageId.trim(),
    title: createForm.title.trim(),
    category: createForm.category,
    ageTiers: [...createForm.ageTiers],
    assetKey: createForm.assetKey.trim(),
    sha256: createForm.sha256.trim(),
    sizeBytes: Number(createForm.sizeBytes),
  }
  const result = await store.createPackage(input)
  if (result.succeeded) {
    closeCreate()
  }
}

async function openEdit(): Promise<void> {
  const version = selectedVersion.value
  if (activePackage.value === null || version === null) {
    return
  }
  Object.assign(editForm, {
    packageId: activePackage.value.packageId,
    title: version.title,
    category: version.category,
    ageTiers: [...version.ageTiers],
    assetSource: 'manual' as AssetSource,
    assetKey: version.assetKey,
    sha256: version.sha256,
    sizeBytes: version.sizeBytes,
  })
  clearFormErrors()
  await store.loadDownloadFiles()
  isEditOpen.value = true
}

function closeEdit(): void {
  isEditOpen.value = false
}

async function submitEdit(): Promise<void> {
  if (activePackage.value === null || !validateDraft(editForm)) {
    return
  }
  const input: ContentDraftInput = {
    title: editForm.title.trim(),
    category: editForm.category,
    ageTiers: [...editForm.ageTiers],
    assetKey: editForm.assetKey.trim(),
    sha256: editForm.sha256.trim(),
    sizeBytes: Number(editForm.sizeBytes),
  }
  const result = await store.updateDraft(
    activePackage.value.packageId,
    selectedVersion.value?.packageVersion ?? 0,
    input,
  )
  if (result.succeeded) {
    closeEdit()
  }
}

async function openPackage(contentPackage: ContentPackage): Promise<void> {
  selectedVersionValue.value = latestVersion(contentPackage)?.packageVersion ?? 0
  await store.openPackage(contentPackage.packageId)
  const version = selectedVersion.value
  selectedVersionValue.value = version?.packageVersion ?? 0
  // Download links come from the inventory, not from typed text; load quietly
  // and let the drawer degrade to "链接待生成" when it is unavailable.
  void store.loadDownloadFiles()
}

function closePackage(): void {
  selectedVersionValue.value = 0
  store.closePackage()
}

function selectVersion(version: ContentVersion): void {
  selectedVersionValue.value = version.packageVersion
}

async function runStatusAction(
  action: 'submit' | 'approve' | 'publish' | 'withdraw' | 'archive',
): Promise<void> {
  if (activePackage.value === null || selectedVersion.value === null) {
    return
  }
  const packageId = activePackage.value.packageId
  const version = selectedVersion.value.packageVersion
  const actionMap = {
    submit: store.submitVersion,
    approve: store.approveVersion,
    publish: store.publishVersion,
    withdraw: store.withdrawVersion,
    archive: store.archiveVersion,
  }
  await actionMap[action](packageId, version)
}

function openReject(): void {
  if (selectedVersion.value === null) {
    return
  }
  rejectTarget.value = selectedVersion.value
  rejectReason.value = ''
  rejectError.value = ''
}

function closeReject(): void {
  rejectTarget.value = null
  rejectReason.value = ''
  rejectError.value = ''
}

async function confirmReject(): Promise<void> {
  if (activePackage.value === null || rejectTarget.value === null) {
    return
  }
  const reason = rejectReason.value.trim()
  if (!reason) {
    rejectError.value = '请填写驳回理由，便于作者修改后再提交。'
    return
  }
  const result = await store.rejectVersion(
    activePackage.value.packageId,
    rejectTarget.value.packageVersion,
    reason,
  )
  if (result.succeeded) {
    closeReject()
  } else if (result.error !== null) {
    rejectError.value = result.error.message
  }
}

function canSubmit(version: ContentVersion | null): boolean {
  return version?.status === 'draft'
}

function canReview(version: ContentVersion | null): boolean {
  return version?.status === 'in_review'
}

function canPublish(version: ContentVersion | null): boolean {
  return version?.status === 'in_review'
}

function canWithdraw(version: ContentVersion | null): boolean {
  return version?.status === 'published'
}

function canArchive(version: ContentVersion | null): boolean {
  // Archive is the terminal step after withdrawal; publishing again goes
  // through the review flow, so a published version must be withdrawn first.
  return version?.status === 'withdrawn'
}

function canEdit(version: ContentVersion | null): boolean {
  return version?.status === 'draft'
}

function handleSearch(): void {
  store.setFilters({ keyword: keywordInput.value })
  void store.load()
}

function handleClearFilters(): void {
  keywordInput.value = ''
  store.resetFilters()
  void store.load()
}

function goToPage(nextPage: number): void {
  store.setPage(nextPage)
  void store.load()
}

function onCategoryFilterChange(event: Event): void {
  const value = selectValue(event)
  store.setFilters({ category: value === 'all' ? 'all' : (value as ContentCategory) })
  void store.load()
}

function onAgeTierFilterChange(event: Event): void {
  const value = selectValue(event)
  store.setFilters({ ageTier: value === 'all' ? 'all' : (value as ContentAgeTier) })
  void store.load()
}

function onStatusFilterChange(event: Event): void {
  const value = selectValue(event)
  store.setFilters({ status: value === 'all' ? 'all' : (value as ContentStatus) })
  void store.load()
}

function toggleAgeTier(form: DraftForm, tier: ContentAgeTier): void {
  if (form.ageTiers.includes(tier)) {
    form.ageTiers = form.ageTiers.filter((value) => value !== tier)
    return
  }
  form.ageTiers = [...form.ageTiers, tier]
}

function applyDownloadFile(form: DraftForm, file: DownloadFile): void {
  form.assetKey = file.relativePath
  form.sha256 = file.sha256
  form.sizeBytes = file.sizeBytes
}

function validateDraft(form: DraftForm): boolean {
  clearFormErrors()
  const title = form.title.trim()
  if (!title) {
    formErrors.title = '请填写内容标题。'
  } else if (title.length > 128) {
    formErrors.title = '标题最多 128 个字符。'
  }

  const packageId = form.packageId.trim()
  if (!isEditOpen.value) {
    if (!packageId) {
      formErrors.packageId = '请填写内容编号。'
    } else if (!isPackageIdValid(packageId, form.category)) {
      formErrors.packageId = `内容编号需形如 content_${form.category}_001，只能使用小写字母和下划线。`
    }
  }

  if (form.ageTiers.length === 0) {
    formErrors.ageTiers = '请至少选择一个适合年龄层。'
  }

  if (!form.assetKey.trim()) {
    formErrors.assetKey = '请填写内容文件的存放位置，或从下载文件中选择。'
  }

  if (!isSha256Valid(form.sha256)) {
    formErrors.sha256 = '校验值需要是 64 位小写十六进制字符。'
  }

  const size = Number(form.sizeBytes)
  if (!Number.isFinite(size) || size < 1 || size > MAX_CONTENT_SIZE_BYTES) {
    formErrors.sizeBytes = '文件大小需要在 1 字节到 4 GiB 之间。'
  }

  return Object.keys(formErrors).length === 0
}

function clearFormErrors(): void {
  for (const key of Object.keys(formErrors)) {
    delete formErrors[key]
  }
}

function emptyDraftForm(): DraftForm {
  return {
    packageId: '',
    title: '',
    category: 'story',
    ageTiers: [],
    assetSource: 'manual',
    assetKey: '',
    sha256: '',
    sizeBytes: 0,
  }
}

function selectValue(event: Event): string {
  const target = event.target
  return target instanceof HTMLSelectElement ? target.value : 'all'
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '未填写'
  }
  const units = ['B', 'KB', 'MB', 'GB']
  const exponent = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  const scaled = value / 1024 ** exponent
  const precision = exponent === 0 || scaled >= 100 ? 0 : 1
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
  return value.length > 18 ? `${value.slice(0, 18)}…` : value
}

/// Content versions are allocated as monotonically increasing integers, so the
/// UI shows them as "第 N 版" instead of inventing a semantic version string.
function versionLabel(value: number | null): string {
  return value === null ? '尚未创建' : `第 ${value} 版`
}

function categorySummary(value: ContentPackage): string {
  return contentCategoryLabel(packageCategory(value))
}

function ageTierSummary(value: ContentPackage): string {
  const tiers = packageAgeTiers(value)
  if (tiers.length === 0) {
    return '未设置'
  }
  return tiers.map(contentAgeTierLabel).join('、')
}
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">内容运营</p>
        <h1>内容库</h1>
        <p class="page-description">
          维护故事、儿歌、古诗等内容的版本与审核状态，发布后设备才能下载使用。
        </p>
      </div>
      <div class="header-actions">
        <button type="button" class="secondary" :disabled="store.isLoading" @click="store.load">
          {{ store.isLoading ? '正在加载…' : '重新加载' }}
        </button>
        <button type="button" class="primary" @click="openCreate">新建内容</button>
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
      </p>
    </Transition>

    <section class="filter-panel" aria-label="内容筛选">
      <div class="filter-heading">
        <div>
          <h2>内容筛选</h2>
          <p>按分类、适合年龄、状态和关键词查找内容包。</p>
        </div>
        <button type="button" class="text-button" @click="handleClearFilters">清除筛选</button>
      </div>
      <div class="filter-grid">
        <label>
          <span>分类</span>
          <select :value="store.filters.category" @change="onCategoryFilterChange">
            <option value="all">全部分类</option>
            <option v-for="category in categories" :key="category" :value="category">
              {{ contentCategoryLabel(category) }}
            </option>
          </select>
        </label>
        <label>
          <span>适合年龄</span>
          <select :value="store.filters.ageTier" @change="onAgeTierFilterChange">
            <option value="all">全部年龄</option>
            <option v-for="tier in ageTiers" :key="tier" :value="tier">
              {{ contentAgeTierLabel(tier) }}
            </option>
          </select>
        </label>
        <label>
          <span>状态</span>
          <select :value="store.filters.status" @change="onStatusFilterChange">
            <option value="all">全部状态</option>
            <option v-for="status in statuses" :key="status" :value="status">
              {{ contentStatusLabel(status) }}
            </option>
          </select>
        </label>
        <label class="keyword-field">
          <span>关键词</span>
          <input
            v-model.trim="keywordInput"
            type="search"
            placeholder="搜索标题或内容编号"
            @keyup.enter="handleSearch"
          />
        </label>
        <div class="filter-submit">
          <button type="button" class="primary" @click="handleSearch">搜索内容</button>
        </div>
      </div>
    </section>

    <Transition name="page" mode="out-in">
      <div :key="store.isLoading ? 'loading' : store.packages.length === 0 ? 'empty' : 'table'">
        <div v-if="store.isLoading" class="state-panel">
          <span class="state-spinner" aria-hidden="true"></span>
          正在读取内容库…
        </div>
        <div v-else-if="store.packages.length === 0" class="state-panel">
          <span class="state-icon" aria-hidden="true">☆</span>
          <span>当前没有内容。新建内容草稿后，它会显示在这里。</span>
        </div>
        <div v-else class="table-shell">
          <table>
            <thead>
              <tr>
                <th>内容</th>
                <th>分类</th>
                <th>适合年龄</th>
                <th>最新版本</th>
                <th>状态</th>
                <th>版本数</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="contentPackage in store.packages" :key="contentPackage.packageId">
                <td>
                  <div class="content-name">
                    <strong>{{ packageTitle(contentPackage) || '未命名内容' }}</strong>
                    <small>{{ contentPackage.packageId }}</small>
                  </div>
                </td>
                <td>{{ categorySummary(contentPackage) }}</td>
                <td>{{ ageTierSummary(contentPackage) }}</td>
                <td class="version-cell">
                  {{ versionLabel(latestVersion(contentPackage)?.packageVersion ?? null) }}
                </td>
                <td>
                  <span
                    v-if="latestVersion(contentPackage)"
                    :class="['status', latestVersion(contentPackage)?.status]"
                  >
                    {{ contentStatusLabel(latestVersion(contentPackage)!.status) }}
                  </span>
                  <span v-else class="status draft">草稿</span>
                </td>
                <td>{{ contentPackage.versions.length }}</td>
                <td>
                  <button type="button" @click="openPackage(contentPackage)">管理</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </Transition>

    <div v-if="store.total > CONTENT_PAGE_SIZE" class="pagination">
      <button
        type="button"
        class="secondary"
        :disabled="store.page <= 1 || store.isLoading"
        @click="goToPage(store.page - 1)"
      >
        上一页
      </button>
      <span>第 {{ store.page }} / {{ store.pageCount }} 页 · 共 {{ store.total }} 条</span>
      <button
        type="button"
        class="secondary"
        :disabled="store.page >= store.pageCount || store.isLoading"
        @click="goToPage(store.page + 1)"
      >
        下一页
      </button>
    </div>

    <Transition name="modal">
      <div v-if="isCreateOpen" class="dialog-backdrop" @click.self="closeCreate">
        <form
          class="dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="content-create-title"
          @submit.prevent="submitCreate"
        >
          <p class="eyebrow">新建内容</p>
          <h2 id="content-create-title">新建内容草稿</h2>
          <p class="dialog-hint">
            填写内容信息并关联已经上传的内容文件。草稿通过审核后才能发布给设备。
          </p>

          <div class="field-grid">
            <label>
              <span>内容编号</span>
              <input
                v-model.trim="createForm.packageId"
                type="text"
                placeholder="例如 content_story_001"
              />
              <small v-if="formErrors.packageId" class="field-error">
                {{ formErrors.packageId }}
              </small>
            </label>
            <label>
              <span>标题</span>
              <input
                v-model.trim="createForm.title"
                type="text"
                placeholder="例如 小兔子的一天"
              />
              <small v-if="formErrors.title" class="field-error">{{ formErrors.title }}</small>
            </label>
            <label>
              <span>分类</span>
              <select v-model="createForm.category">
                <option v-for="category in categories" :key="category" :value="category">
                  {{ contentCategoryLabel(category) }}
                </option>
              </select>
            </label>
          </div>

          <fieldset class="age-fieldset">
            <legend>适合年龄</legend>
            <div class="age-options">
              <label v-for="tier in ageTiers" :key="tier" class="age-option">
                <input
                  type="checkbox"
                  :checked="createForm.ageTiers.includes(tier)"
                  @change="toggleAgeTier(createForm, tier)"
                />
                <span>{{ contentAgeTierLabel(tier) }}</span>
              </label>
            </div>
            <small v-if="formErrors.ageTiers" class="field-error">{{ formErrors.ageTiers }}</small>
          </fieldset>

          <ContentAssetFields
            :form="createForm"
            :errors="formErrors"
            :download-files="selectableDownloadFiles"
            :is-loading-download-files="store.isLoadingDownloadFiles"
            :download-files-error="store.downloadFilesError?.message ?? ''"
            @toggle-source="
              (source) => {
                createForm.assetSource = source
              }
            "
            @load-download-files="store.loadDownloadFiles"
            @select-download-file="(file) => applyDownloadFile(createForm, file)"
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

    <Transition name="drawer">
      <div v-if="activePackage" class="drawer-backdrop" @click.self="closePackage">
        <aside
          class="content-drawer"
          role="dialog"
          aria-modal="true"
          aria-labelledby="content-detail-title"
        >
          <header class="drawer-header">
            <div>
              <p class="eyebrow">{{ categorySummary(activePackage) }}</p>
              <h2 id="content-detail-title">
                {{ packageTitle(activePackage) || '未命名内容' }}
              </h2>
              <p class="drawer-subtitle">
                {{ activePackage.packageId }} · 适合 {{ ageTierSummary(activePackage) }}
              </p>
            </div>
            <button
              type="button"
              class="icon-button"
              aria-label="关闭内容详情"
              @click="closePackage"
            >
              ×
            </button>
          </header>

          <div class="drawer-body">
            <div v-if="store.isDetailLoading" class="inline-state">正在读取版本信息…</div>
            <div v-else-if="versionList.length === 0" class="empty-section">
              <p>这个内容还没有版本。创建草稿并关联文件后，版本会显示在这里。</p>
            </div>
            <template v-else>
              <div class="version-tabs" role="tablist" aria-label="内容版本">
                <button
                  v-for="version in versionList"
                  :key="version.packageVersion"
                  type="button"
                  role="tab"
                  :aria-selected="selectedVersion?.packageVersion === version.packageVersion"
                  :class="[
                    'version-tab',
                    { active: selectedVersion?.packageVersion === version.packageVersion },
                  ]"
                  @click="selectVersion(version)"
                >
                  {{ versionLabel(version.packageVersion) }}
                  <span :class="['status-dot', version.status]" aria-hidden="true"></span>
                </button>
              </div>

              <section v-if="selectedVersion" class="detail-card">
                <div class="section-heading">
                  <div>
                    <h3>{{ versionLabel(selectedVersion.packageVersion) }}</h3>
                    <p>{{ selectedVersion.title || '未命名内容' }} · 审核通过后即可发布。</p>
                  </div>
                  <span :class="['status', selectedVersion.status]">
                    {{ contentStatusLabel(selectedVersion.status) }}
                  </span>
                </div>

                <dl class="meta-grid">
                  <div>
                    <dt>文件位置</dt>
                    <dd>{{ selectedVersion.assetKey || '未填写' }}</dd>
                  </div>
                  <div>
                    <dt>文件大小</dt>
                    <dd>{{ formatBytes(selectedVersion.sizeBytes) }}</dd>
                  </div>
                  <div>
                    <dt>校验值</dt>
                    <dd class="mono">{{ sha256Summary(selectedVersion.sha256) }}</dd>
                  </div>
                  <div>
                    <dt>发布时间</dt>
                    <dd>{{ formatTime(selectedVersion.publishedAt) }}</dd>
                  </div>
                  <div class="wide">
                    <dt>下载地址</dt>
                    <dd>
                      <a
                        v-if="selectedDownloadUrl"
                        :href="selectedDownloadUrl"
                        target="_blank"
                        rel="noreferrer"
                      >
                        {{ selectedDownloadUrl }}
                      </a>
                      <span v-else class="muted">还没有找到对应的下载文件，请先在下载文件页上传。</span>
                    </dd>
                  </div>
                </dl>

                <div class="action-row">
                  <button
                    v-if="canEdit(selectedVersion)"
                    type="button"
                    class="secondary"
                    @click="openEdit"
                  >
                    编辑草稿
                  </button>
                  <button
                    type="button"
                    class="primary"
                    :disabled="!canSubmit(selectedVersion) || store.isSubmitting"
                    @click="runStatusAction('submit')"
                  >
                    提交审核
                  </button>
                  <button
                    type="button"
                    class="primary"
                    :disabled="!canReview(selectedVersion) || store.isSubmitting"
                    @click="runStatusAction('approve')"
                  >
                    审核通过
                  </button>
                  <button
                    type="button"
                    class="danger"
                    :disabled="!canReview(selectedVersion) || store.isSubmitting"
                    @click="openReject"
                  >
                    审核驳回
                  </button>
                  <button
                    type="button"
                    class="primary"
                    :disabled="!canPublish(selectedVersion) || store.isSubmitting"
                    @click="runStatusAction('publish')"
                  >
                    发布
                  </button>
                  <button
                    type="button"
                    :disabled="!canWithdraw(selectedVersion) || store.isSubmitting"
                    @click="runStatusAction('withdraw')"
                  >
                    撤回
                  </button>
                  <button
                    type="button"
                    :disabled="!canArchive(selectedVersion) || store.isSubmitting"
                    @click="runStatusAction('archive')"
                  >
                    归档
                  </button>
                </div>
              </section>

              <section v-if="selectedVersion" class="detail-card">
                <div class="section-heading">
                  <div>
                    <h3>审核记录</h3>
                    <p>记录每一次提交、审核和状态变化，方便追溯。</p>
                  </div>
                </div>
                <ol v-if="selectedHistory.length" class="review-list">
                  <li
                    v-for="(entry, index) in selectedHistory"
                    :key="`${entry.action}-${index}`"
                  >
                    <div class="review-heading">
                      <strong>{{ contentReviewActionLabel(entry.action) }}</strong>
                      <span>{{ formatTime(entry.createdAt) }}</span>
                    </div>
                    <p class="review-actor">操作人：{{ entry.actor || '未知' }}</p>
                    <p v-if="entry.reason" class="review-reason">{{ entry.reason }}</p>
                  </li>
                </ol>
                <div v-else class="empty-section">
                  <p>这个版本还没有审核记录。</p>
                </div>
              </section>
            </template>
          </div>

          <footer class="drawer-footer">
            <button type="button" class="secondary" @click="closePackage">关闭</button>
          </footer>
        </aside>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="isEditOpen && activePackage" class="dialog-backdrop" @click.self="closeEdit">
        <form
          class="dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="content-edit-title"
          @submit.prevent="submitEdit"
        >
          <p class="eyebrow">编辑内容</p>
          <h2 id="content-edit-title">编辑内容草稿</h2>
          <p class="dialog-hint">只有草稿状态可以修改，已提交审核的版本需要先撤回或归档。</p>

          <div class="field-grid">
            <label>
              <span>内容编号</span>
              <input :value="editForm.packageId" type="text" readonly aria-readonly="true" />
            </label>
            <label>
              <span>标题</span>
              <input v-model.trim="editForm.title" type="text" />
              <small v-if="formErrors.title" class="field-error">{{ formErrors.title }}</small>
            </label>
            <label>
              <span>分类</span>
              <select v-model="editForm.category">
                <option v-for="category in categories" :key="category" :value="category">
                  {{ contentCategoryLabel(category) }}
                </option>
              </select>
            </label>
            <label>
              <span>版本</span>
              <input
                :value="versionLabel(selectedVersion?.packageVersion ?? null)"
                type="text"
                readonly
                aria-readonly="true"
              />
            </label>
          </div>

          <fieldset class="age-fieldset">
            <legend>适合年龄</legend>
            <div class="age-options">
              <label v-for="tier in ageTiers" :key="tier" class="age-option">
                <input
                  type="checkbox"
                  :checked="editForm.ageTiers.includes(tier)"
                  @change="toggleAgeTier(editForm, tier)"
                />
                <span>{{ contentAgeTierLabel(tier) }}</span>
              </label>
            </div>
            <small v-if="formErrors.ageTiers" class="field-error">{{ formErrors.ageTiers }}</small>
          </fieldset>

          <ContentAssetFields
            :form="editForm"
            :errors="formErrors"
            :download-files="selectableDownloadFiles"
            :is-loading-download-files="store.isLoadingDownloadFiles"
            :download-files-error="store.downloadFilesError?.message ?? ''"
            @toggle-source="
              (source) => {
                editForm.assetSource = source
              }
            "
            @load-download-files="store.loadDownloadFiles"
            @select-download-file="(file) => applyDownloadFile(editForm, file)"
          />

          <div class="dialog-actions">
            <button type="button" class="secondary" @click="closeEdit">取消</button>
            <button type="submit" class="primary" :disabled="store.isSubmitting">
              {{ store.isSubmitting ? '正在保存…' : '保存草稿' }}
            </button>
          </div>
        </form>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="rejectTarget" class="dialog-backdrop" @click.self="closeReject">
        <form
          class="dialog confirm-dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="content-reject-title"
          @submit.prevent="confirmReject"
        >
          <p class="eyebrow">审核驳回</p>
          <h2 id="content-reject-title">
            驳回{{ versionLabel(rejectTarget.packageVersion) }}？
          </h2>
          <p>请说明需要修改的地方，作者会根据理由调整后重新提交审核。</p>
          <label>
            <span>驳回理由</span>
            <textarea
              v-model.trim="rejectReason"
              rows="4"
              placeholder="例如 音频里有明显的杂音，请重新录制"
              required
            ></textarea>
          </label>
          <small v-if="rejectError" class="field-error">{{ rejectError }}</small>
          <div class="dialog-actions">
            <button type="button" class="secondary" @click="closeReject">取消</button>
            <button type="submit" class="danger-button" :disabled="store.isSubmitting">
              {{ store.isSubmitting ? '正在驳回…' : '确认驳回' }}
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
.dialog-hint,
.drawer-subtitle,
.section-heading p {
  margin: 8px 0 0;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

.header-actions,
.dialog-actions,
.action-row {
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
  cursor: not-allowed;
  opacity: 0.5;
}

.text-button {
  min-height: 32px;
  padding: 0 12px;
  border-radius: 11px;
  background: #fff7fa;
  font-size: 12px;
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

.filter-panel {
  margin-top: 18px;
  overflow: hidden;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.filter-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 18px 20px;
  border-bottom: 1px solid var(--sprout-outline-soft);
}

.filter-heading h2 {
  font-size: 17px;
}

.filter-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr)) auto;
  align-items: end;
  gap: 14px;
  padding: 18px 20px 20px;
}

.filter-submit {
  display: flex;
}

label {
  display: grid;
  gap: 7px;
  color: #4a2e3b;
  font-size: 13px;
  font-weight: 700;
}

select,
input,
textarea {
  width: 100%;
  min-height: 42px;
  padding: 0 13px;
  border: 1px solid #f0bdcb;
  border-radius: 14px;
  background: #fff8fa;
  color: #4a2e3b;
  font: inherit;
  font-weight: 400;
  transition:
    border-color var(--sprout-duration-fast) ease,
    box-shadow var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease;
}

textarea {
  min-height: 108px;
  padding: 11px 13px;
  resize: vertical;
}

select:focus,
input:focus,
textarea:focus {
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

.field-error {
  color: #b3261e;
  font-size: 12px;
  font-weight: 600;
}

.table-shell,
.state-panel {
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
  padding: 14px 16px;
  border-bottom: 1px solid var(--sprout-outline-soft);
  text-align: left;
  vertical-align: top;
}

th {
  color: #6b4f5a;
  font-size: 13px;
  white-space: nowrap;
}

td {
  color: #4a2e3b;
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

.content-name {
  display: grid;
  gap: 4px;
}

.content-name small {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.version-cell {
  color: #7b5263;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  white-space: nowrap;
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

.status.draft {
  background: #f2f0f4;
  color: #5b4a55;
}

.status.in_review {
  background: #fff3cd;
  color: #755600;
}

.status.published {
  background: #e7f8ee;
  color: #1d6b3f;
}

.status.withdrawn {
  background: #fff0f2;
  color: #a12b4a;
}

.status.archived {
  background: #eef1f4;
  color: #55606b;
}

.state-panel {
  display: flex;
  min-height: 150px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 28px;
  color: var(--sprout-text-muted);
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

.pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
  margin-top: 18px;
  color: var(--sprout-text-muted);
  font-size: 13px;
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
  width: min(100%, 720px);
  max-height: min(880px, calc(100vh - 40px));
  gap: 16px;
  overflow: auto;
  padding: 28px;
  border: 1px solid var(--sprout-outline);
  border-radius: 28px;
  background: #ffffff;
  box-shadow: 0 28px 72px rgb(74 46 59 / 22%);
}

.confirm-dialog {
  width: min(100%, 520px);
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.age-fieldset {
  margin: 0;
  padding: 14px 16px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 16px;
}

.age-fieldset legend {
  padding: 0 6px;
  color: #4a2e3b;
  font-size: 13px;
  font-weight: 700;
}

.age-options {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.age-option {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 13px;
  border: 1px solid #f7d9e2;
  border-radius: 14px;
  background: #fffbfc;
  font-weight: 400;
  cursor: pointer;
}

.age-option input {
  width: 18px;
  height: 18px;
  min-height: auto;
  padding: 0;
  accent-color: #d94f83;
}

.asset-fields {
  display: grid;
  gap: 14px;
  padding: 16px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 18px;
  background: #fffbfc;
}

.source-toggle {
  display: inline-flex;
  gap: 4px;
  padding: 4px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 999px;
  background: #ffffff;
}

.source-toggle button {
  min-height: 34px;
  padding: 0 14px;
  border: 0;
  border-radius: 999px;
  background: transparent;
  color: var(--sprout-text-muted);
  font-weight: 700;
}

.source-toggle button.active {
  background: #d94f83;
  color: #ffffff;
  box-shadow: 0 6px 14px rgb(217 79 131 / 20%);
}

.download-picker {
  display: grid;
  gap: 10px;
}

.inline-state,
.empty-section {
  padding: 14px 16px;
  border-radius: 15px;
  background: #fff7fa;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

.empty-section {
  display: grid;
  justify-items: start;
  gap: 10px;
}

.drawer-backdrop {
  display: flex;
  justify-content: flex-end;
}

.content-drawer {
  display: grid;
  width: min(100%, 780px);
  height: 100%;
  grid-template-rows: auto minmax(0, 1fr) auto;
  border-left: 1px solid #f0bdcb;
  background:
    radial-gradient(circle at 96% 2%, rgb(102 204 255 / 10%), transparent 16rem),
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

.version-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.version-tab {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 38px;
  padding: 0 14px;
  border: 1px solid var(--sprout-outline);
  border-radius: 999px;
  background: #ffffff;
  color: var(--sprout-text-muted);
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 13px;
}

.version-tab.active {
  border-color: #d94f83;
  background: #fff0f4;
  color: #c94175;
  font-weight: 700;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #c9b6bf;
}

.status-dot.draft {
  background: #b7a6b0;
}

.status-dot.in_review {
  background: #e0a300;
}

.status-dot.published {
  background: #2c9a5b;
}

.status-dot.withdrawn {
  background: #c94175;
}

.status-dot.archived {
  background: #7b8794;
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

.meta-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  margin: 0;
}

.meta-grid .wide {
  grid-column: 1 / -1;
}

.meta-grid dt {
  color: var(--sprout-text-muted);
  font-size: 12px;
  font-weight: 700;
}

.meta-grid dd {
  margin: 5px 0 0;
  color: var(--sprout-text);
  overflow-wrap: anywhere;
}

.meta-grid .mono {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 13px;
}

.meta-grid a {
  color: #c94175;
  font-weight: 700;
  overflow-wrap: anywhere;
}

.muted {
  color: var(--sprout-text-muted);
}

.action-row {
  padding-top: 6px;
  border-top: 1px solid var(--sprout-outline-soft);
}

.review-list {
  display: grid;
  gap: 12px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.review-list li {
  display: grid;
  gap: 4px;
  padding: 13px 15px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #fffbfc;
}

.review-heading {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.review-heading strong {
  color: var(--sprout-text);
}

.review-heading span,
.review-actor {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.review-actor,
.review-reason {
  margin: 0;
}

.review-reason {
  margin-top: 4px;
  color: var(--sprout-text);
  line-height: 1.6;
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

.drawer-enter-active .content-drawer,
.drawer-leave-active .content-drawer {
  transition: transform var(--sprout-duration-slow) var(--sprout-ease-out);
}

.drawer-enter-from,
.drawer-leave-to {
  opacity: 0;
}

.drawer-enter-from .content-drawer,
.drawer-leave-to .content-drawer {
  transform: translateX(24px);
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1120px) {
  .filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .table-shell {
    overflow-x: auto;
  }

  table {
    min-width: 960px;
  }
}

@media (max-width: 760px) {
  .page-header {
    display: grid;
  }

  .header-actions {
    justify-content: flex-start;
  }

  .drawer-header,
  .drawer-body,
  .drawer-footer {
    padding-right: 18px;
    padding-left: 18px;
  }
}

@media (max-width: 520px) {
  .filter-grid,
  .field-grid,
  .meta-grid {
    grid-template-columns: 1fr;
  }

  .meta-grid .wide {
    grid-column: auto;
  }

  .action-row button {
    flex: 1 1 100%;
  }
}
</style>
