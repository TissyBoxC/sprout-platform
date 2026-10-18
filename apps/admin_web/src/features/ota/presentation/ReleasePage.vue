<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

import {
  useReleaseStore,
  type AdminRelease,
  type CreateReleaseInput,
} from '@/features/ota/application/releaseStore'

const store = useReleaseStore()
const isCreateOpen = ref(false)
const pendingDelete = ref<AdminRelease | null>(null)
const pendingPublish = ref<AdminRelease | null>(null)
const pendingNotesRecovery = ref<AdminRelease | null>(null)
const createForm = reactive<CreateReleaseInput>({
  version: store.adminBuildVersion,
  channel: 'stable',
  kind: 'resource',
  platform: 'android',
  downloadUrl: '',
  sha256: '',
  releaseNotes: '',
  isMandatory: false,
  minimumSupportedVersion: '',
})
const isBusy = computed(
  () =>
    store.isLoading ||
    store.isSubmitting ||
    store.isResolvingArtifact,
)

onMounted(() => {
  void store.load()
})

function openCreate(): void {
  if (isBusy.value) {
    return
  }
  isCreateOpen.value = true
  store.lastMessage = ''
  if (!createForm.version) {
    createForm.version = store.adminBuildVersion
  }
}

function startPublish(release: AdminRelease): void {
  if (isBusy.value || release.status !== 'draft') {
    return
  }
  pendingPublish.value = release
}

async function resolveArtifact(): Promise<void> {
  if (isBusy.value) {
    return
  }
  const artifactInput = await store.resolveArtifact({
    version: createForm.version,
    kind: createForm.kind,
    platform: createForm.platform,
  })
  if (artifactInput === null) {
    return
  }
  createForm.downloadUrl = artifactInput.downloadUrl
  createForm.sha256 = artifactInput.sha256
  createForm.version = artifactInput.version
  createForm.kind = artifactInput.kind
  createForm.platform = artifactInput.platform
}

async function createRelease(): Promise<void> {
  if (isBusy.value) {
    return
  }
  const succeeded = await store.create(createForm)
  if (!succeeded) {
    return
  }
  resetCreateForm()
  isCreateOpen.value = false
}

function startNotesRecovery(release: AdminRelease): void {
  if (isBusy.value || release.status !== 'draft') {
    return
  }
  pendingNotesRecovery.value = release
}

function resetCreateForm(): void {
  createForm.version = store.adminBuildVersion
  createForm.channel = 'stable'
  createForm.kind = 'resource'
  createForm.platform = 'android'
  createForm.downloadUrl = ''
  createForm.sha256 = ''
  createForm.releaseNotes = ''
  createForm.isMandatory = false
  createForm.minimumSupportedVersion = ''
}

async function confirmPublish(): Promise<void> {
  if (
    pendingPublish.value === null ||
    isBusy.value ||
    pendingPublish.value.status !== 'draft' ||
    !pendingPublish.value.releaseNotes.trim()
  ) {
    return
  }
  const succeeded = await store.publish(pendingPublish.value.version)
  if (succeeded) {
    pendingPublish.value = null
  }
}

async function confirmDelete(): Promise<void> {
  if (pendingDelete.value === null || store.isSubmitting) {
    return
  }
  const succeeded = await store.remove(pendingDelete.value.version)
  if (succeeded) {
    pendingDelete.value = null
  }
}

async function confirmNotesRecovery(): Promise<void> {
  const release = pendingNotesRecovery.value
  if (release === null || store.isSubmitting) {
    return
  }
  const succeeded = await store.remove(release.version)
  if (!succeeded) {
    return
  }
  pendingNotesRecovery.value = null
  openCreate()
  createForm.version = release.version
  createForm.channel = normalizeChannel(release.channel)
  createForm.kind = normalizeKind(release.kind)
  createForm.platform = normalizePlatform(release.platform)
  createForm.downloadUrl = release.downloadUrl
  createForm.sha256 = release.sha256
  createForm.releaseNotes = ''
  createForm.isMandatory = release.isMandatory
  createForm.minimumSupportedVersion = release.minimumSupportedVersion
}

function kindLabel(value: string): string {
  return (
    {
      resource: '资源更新',
      client: '客户端更新',
      firmware: '设备固件',
    }[value] ?? '其他更新'
  )
}

function channelLabel(value: string): string {
  return (
    {
      stable: '稳定渠道',
      beta: '测试渠道',
      canary: '内测渠道',
    }[value] ?? '稳定渠道'
  )
}

function platformLabel(value: string): string {
  return (
    {
      android: 'Android',
      ios: 'iOS',
      esp32_s3: '初芽设备',
      all: '全部平台',
    }[value] ?? value
  )
}

function statusLabel(value: string): string {
  return (
    {
      draft: '待发布',
      published: '已发布',
      withdrawn: '已撤回',
    }[value] ?? '待确认'
  )
}

function formatTime(value: string): string {
  if (!value) {
    return '尚未发布'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '尚未发布'
  }
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

async function changePageSize(event: Event): Promise<void> {
  const target = event.target
  if (!(target instanceof HTMLSelectElement)) {
    return
  }
  await store.setPageSize(Number(target.value))
}

function normalizeChannel(value: string): CreateReleaseInput['channel'] {
  return value === 'beta' || value === 'canary' ? value : 'stable'
}

function normalizeKind(value: string): CreateReleaseInput['kind'] {
  return value === 'client' || value === 'firmware' ? value : 'resource'
}

function normalizePlatform(value: string): CreateReleaseInput['platform'] {
  return value === 'ios' || value === 'esp32_s3' || value === 'all'
    ? value
    : 'android'
}
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">版本运营</p>
        <h1>更新发布</h1>
        <p class="page-description">
          小更新通过资源包快速生效；完整安装包和固件会经过校验后再提供给用户。
        </p>
      </div>
      <div class="header-actions">
        <button type="button" class="secondary" :disabled="isBusy" @click="store.load()">
          刷新列表
        </button>
        <button type="button" class="primary" :disabled="isBusy" @click="openCreate">
          登记新版本
        </button>
      </div>
    </header>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="success-message">{{ store.lastMessage }}</p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.error" class="error-message">{{ store.error.message }}</p>
    </Transition>

    <Transition name="page" mode="out-in">
      <div v-if="store.isLoading && store.pagination.total === 0" key="loading" class="state-panel">
        <span class="state-spinner" aria-hidden="true"></span>
        正在读取更新记录…
      </div>
      <div
        v-else-if="store.releases.length === 0 && store.error"
        key="error"
        class="state-panel state-error"
      >
        <p>{{ store.error.message }}</p>
        <button
          type="button"
          class="primary"
          :disabled="isBusy"
          @click="store.retryCurrentPage"
        >
          重新加载这一页
        </button>
      </div>
      <div v-else-if="store.releases.length === 0" key="empty" class="empty-page">
        <div class="state-panel">
          <span class="state-icon" aria-hidden="true">☆</span>
          <div>
            <strong>
              {{
                store.pagination.page > 1
                  ? '这一页没有更新记录'
                  : '还没有更新记录'
              }}
            </strong>
            <p>
            {{
              store.pagination.page > 1
                ? '列表可能刚刚发生变化，请返回上一页、调整每页数量或重新加载。'
                : '登记新版本后，会显示在这里。'
            }}
            </p>
          </div>
          <div class="empty-actions">
            <button
              v-if="store.pagination.page > 1"
              type="button"
              class="secondary"
              :disabled="isBusy"
              @click="store.goToPage(store.pagination.page - 1)"
            >
              返回上一页
            </button>
            <button
              type="button"
              class="secondary"
              :disabled="isBusy"
              @click="store.retryCurrentPage"
            >
              重新加载
            </button>
            <button
              v-if="store.pagination.total === 0"
              type="button"
              class="primary"
              :disabled="isBusy"
              @click="openCreate"
            >
              登记新版本
            </button>
          </div>
        </div>
        <footer class="pagination pagination-empty">
          <p>
            第 {{ store.pageStart }}–{{ store.pageEnd }} 个版本，共
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
                :disabled="isBusy"
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
              :disabled="isBusy || !store.pagination.hasPrevious"
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
              :disabled="isBusy || !store.pagination.hasMore"
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
              <th>版本</th>
              <th>类型</th>
              <th>适用平台</th>
              <th>渠道</th>
              <th>状态</th>
              <th>发布时间</th>
              <th>更新说明</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="release in store.releases" :key="release.version">
              <td>
                <span class="release-version">{{ release.version }}</span>
                <span v-if="release.isMandatory" class="mandatory">必须升级</span>
              </td>
              <td>{{ kindLabel(release.kind) }}</td>
              <td>{{ platformLabel(release.platform) }}</td>
              <td>{{ channelLabel(release.channel) }}</td>
              <td>
                <span :class="['status', release.status]">
                  {{ statusLabel(release.status) }}
                </span>
              </td>
              <td>{{ formatTime(release.publishedAt) }}</td>
              <td class="notes">{{ release.releaseNotes || '未填写更新说明' }}</td>
              <td>
                <div class="row-actions">
                  <button
                    v-if="release.status === 'draft' && release.releaseNotes.trim()"
                    type="button"
                    :disabled="isBusy"
                    @click="startPublish(release)"
                  >
                    发布
                  </button>
                  <button
                    v-if="release.status === 'draft' && !release.releaseNotes.trim()"
                    type="button"
                    :disabled="isBusy"
                    @click="startNotesRecovery(release)"
                  >
                    补充更新说明
                  </button>
                  <button
                    type="button"
                    class="danger"
                    :disabled="isBusy"
                    @click="pendingDelete = release"
                  >
                    删除
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <footer class="pagination">
          <p>
            第 {{ store.pageStart }}–{{ store.pageEnd }} 个版本，共
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
                :disabled="isBusy"
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
              :disabled="isBusy || !store.pagination.hasPrevious"
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
              :disabled="isBusy || !store.pagination.hasMore"
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
        @click.self="!isBusy && (isCreateOpen = false)"
      >
        <form
          class="dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="release-create-title"
          @submit.prevent="createRelease"
        >
          <h2 id="release-create-title">登记新版本</h2>
          <p class="dialog-hint">
            版本号默认使用当前发布版本。更新文件会自动查找并填入，确认无误后再发布。
          </p>
          <fieldset class="form-fieldset" :disabled="store.isSubmitting || store.isResolvingArtifact">
            <div class="artifact-toolbar">
              <button
                type="button"
                class="secondary"
                :disabled="store.isResolvingArtifact || !createForm.version"
                @click="resolveArtifact"
              >
                {{ store.isResolvingArtifact ? '正在查找…' : '自动查找更新文件' }}
              </button>
              <span>按照版本、更新类型和适用平台查找。</span>
            </div>
            <div class="field-grid">
            <label>
              <span>版本号</span>
              <input v-model.trim="createForm.version" type="text" required placeholder="例如 0.8.0" />
            </label>
            <label>
              <span>更新类型</span>
              <select v-model="createForm.kind">
                <option value="resource">资源更新</option>
                <option value="client">客户端更新</option>
                <option value="firmware">设备固件</option>
              </select>
            </label>
            <label>
              <span>更新渠道</span>
              <select v-model="createForm.channel">
                <option value="stable">稳定渠道</option>
                <option value="beta">测试渠道</option>
                <option value="canary">内测渠道</option>
              </select>
            </label>
            <label>
              <span>适用平台</span>
              <select v-model="createForm.platform">
                <option value="android">Android</option>
                <option value="ios">iOS</option>
                <option value="esp32_s3">初芽设备</option>
                <option value="all">全部平台</option>
              </select>
            </label>
            <label class="span-two">
              <span>下载地址</span>
              <input
                v-model.trim="createForm.downloadUrl"
                type="url"
                required
                placeholder="自动查找后填入，也可以手动修正"
              />
            </label>
            <label class="span-two">
              <span>文件校验值</span>
              <input
                v-model.trim="createForm.sha256"
                type="text"
                required
                placeholder="自动查找后填入，也可以手动修正"
              />
            </label>
            <label class="span-two">
              <span>最低可用版本</span>
              <input
                v-model.trim="createForm.minimumSupportedVersion"
                type="text"
                placeholder="低于该版本时不再提供此更新"
              />
            </label>
            <label class="span-two">
              <span>更新说明</span>
              <textarea
                v-model.trim="createForm.releaseNotes"
                rows="4"
                required
                placeholder="说明这次更新解决什么问题、带来哪些变化"
              ></textarea>
            </label>
            </div>
            <label class="mandatory-row">
              <input v-model="createForm.isMandatory" type="checkbox" />
              <span>
                <strong>此版本必须升级</strong>
                <small>家长打开应用时会看到升级提醒，暂不能跳过。</small>
              </span>
            </label>
          </fieldset>
          <div class="dialog-actions">
            <button
              type="button"
              class="secondary"
              :disabled="store.isSubmitting || store.isResolvingArtifact"
              @click="isCreateOpen = false"
            >
              取消
            </button>
            <button
              type="submit"
              class="primary"
              :disabled="store.isSubmitting || store.isResolvingArtifact"
            >
              {{ store.isSubmitting ? '正在登记…' : '登记版本' }}
            </button>
          </div>
        </form>
      </div>
    </Transition>

    <Transition name="modal">
      <div
        v-if="pendingPublish"
        class="dialog-backdrop"
        @click.self="!store.isSubmitting && (pendingPublish = null)"
      >
        <section class="dialog confirm-dialog">
          <h2>发布这个版本？</h2>
          <p>
            发布后，使用 {{ channelLabel(pendingPublish.channel) }} 的用户会收到
            {{ pendingPublish.version }} 的更新提示。
          </p>
          <p v-if="pendingPublish.releaseNotes.trim()" class="publish-notes">
            {{ pendingPublish.releaseNotes }}
          </p>
          <p v-if="!pendingPublish.releaseNotes.trim()" class="publish-warning">
            当前记录没有更新说明。平台不支持在登记后直接编辑草稿，请删除这条记录，重新登记时填写说明后再发布。
          </p>
          <div class="dialog-actions">
            <button
              type="button"
              class="secondary"
              :disabled="store.isSubmitting"
              @click="pendingPublish = null"
            >
              取消
            </button>
            <button
              type="button"
              class="primary"
              :disabled="store.isSubmitting || !pendingPublish.releaseNotes.trim()"
              @click="confirmPublish"
            >
              {{ store.isSubmitting ? '正在发布…' : '确认发布' }}
            </button>
          </div>
        </section>
      </div>
    </Transition>

    <Transition name="modal">
      <div
        v-if="pendingNotesRecovery"
        class="dialog-backdrop"
        @click.self="!store.isSubmitting && (pendingNotesRecovery = null)"
      >
        <section
          class="dialog confirm-dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="release-notes-recovery-title"
        >
          <h2 id="release-notes-recovery-title">
            补充版本 {{ pendingNotesRecovery.version }} 的更新说明
          </h2>
          <p class="publish-warning">
            这条待发布记录没有更新说明，当前版本服务不支持直接编辑草稿。
          </p>
          <p>
            可以删除这条待发布记录，再打开登记表单。版本、类型、平台、下载地址和校验值会保留，只需要补写更新说明后重新登记。
          </p>
          <div class="dialog-actions">
            <button
              type="button"
              class="secondary"
              :disabled="store.isSubmitting"
              @click="pendingNotesRecovery = null"
            >
              暂不处理
            </button>
            <button
              type="button"
              class="danger-button"
              :disabled="store.isSubmitting"
              @click="confirmNotesRecovery"
            >
              {{ store.isSubmitting ? '正在准备…' : '删除并重新登记' }}
            </button>
          </div>
        </section>
      </div>
    </Transition>

    <Transition name="modal">
      <div
        v-if="pendingDelete"
        class="dialog-backdrop"
        @click.self="!store.isSubmitting && (pendingDelete = null)"
      >
        <section class="dialog confirm-dialog">
          <h2>删除这个版本？</h2>
          <p>
            删除后，版本 {{ pendingDelete.version }} 的记录将无法恢复。已经安装该版本的用户不受影响。
          </p>
          <div class="dialog-actions">
            <button
              type="button"
              class="secondary"
              :disabled="store.isSubmitting"
              @click="pendingDelete = null"
            >
              取消
            </button>
            <button
              type="button"
              class="danger-button"
              :disabled="store.isSubmitting"
              @click="confirmDelete"
            >
              {{ store.isSubmitting ? '正在删除…' : '删除版本' }}
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
.row-actions {
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
  cursor: wait;
  opacity: 0.55;
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

.empty-page {
  overflow: hidden;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.empty-page .state-panel {
  border: 0;
  border-radius: 0;
  box-shadow: none;
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
  color: #6b4f5a;
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
  color: #6b4f5a;
  font-size: 13px;
}

.pagination-controls select {
  min-height: 38px;
  padding: 0 10px;
  border: 1px solid #f0bdcb;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: #4a2e3b;
}

.state-error {
  flex-direction: column;
}

.state-error p {
  margin: 0;
}

.state-panel > div {
  text-align: left;
}

.state-panel strong {
  color: #4a2e3b;
}

.state-panel p {
  margin: 6px 0 0;
  color: #6b4f5a;
}

.state-error button {
  min-height: 38px;
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

.release-version,
.mandatory {
  display: block;
}

.release-version {
  font-weight: 700;
}

.mandatory {
  margin-top: 3px;
  color: #a12b4a;
  font-size: 12px;
}

.notes {
  max-width: 260px;
  white-space: normal;
}

.status {
  display: inline-flex;
  padding: 4px 10px;
  border-radius: 999px;
  background: #fff3cd;
  color: #755600;
  font-size: 13px;
}

.status.published {
  background: #e7f8ee;
  color: #1d6b3f;
}

.status.withdrawn {
  background: #fff0f2;
  color: #a12b4a;
}

.state-panel {
  display: flex;
  min-height: 132px;
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
  margin: 0 0 14px;
  color: #b3261e;
}

.success-message {
  margin: 0 0 14px;
  color: #1d6b3f;
}

.dialog-backdrop {
  position: fixed;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgb(74 46 59 / 35%);
  backdrop-filter: blur(3px);
}

.dialog {
  display: grid;
  width: min(100%, 560px);
  max-height: min(820px, calc(100vh - 40px));
  gap: 16px;
  overflow: auto;
  padding: 28px;
  border: 1px solid #f0bdcb;
  border-radius: 26px;
  background: #ffffff;
  box-shadow: 0 28px 72px rgb(74 46 59 / 22%);
}

.dialog h2,
.dialog p {
  margin: 0;
}

.dialog h2 {
  color: #4a2e3b;
}

.dialog-hint {
  margin-top: -6px !important;
  color: #6b4f5a;
}

.artifact-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #fffbfc;
}

.artifact-toolbar span {
  color: #6b4f5a;
  font-size: 13px;
}

.artifact-toolbar button {
  flex: 0 0 auto;
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.form-fieldset {
  display: grid;
  min-width: 0;
  gap: 16px;
  margin: 0;
  padding: 0;
  border: 0;
}

.field-grid label {
  display: grid;
  gap: 7px;
  color: #4a2e3b;
  font-weight: 600;
}

.field-grid .span-two {
  grid-column: 1 / -1;
}

input,
select,
textarea {
  width: 100%;
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

input,
select {
  min-height: 44px;
  padding: 0 14px;
}

textarea {
  min-height: 98px;
  resize: vertical;
  padding: 12px 14px;
  line-height: 1.55;
}

input:focus,
select:focus,
textarea:focus {
  border-color: #d94f83;
  background: #ffffff;
  box-shadow: 0 0 0 3px rgb(217 79 131 / 12%);
  outline: none;
}

.mandatory-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #fffbfc;
}

.mandatory-row input {
  width: 22px;
  min-width: 22px;
  min-height: 22px;
  accent-color: #d94f83;
}

.mandatory-row span {
  display: grid;
  gap: 3px;
}

.mandatory-row strong {
  color: #4a2e3b;
}

.mandatory-row small {
  color: #6b4f5a;
}

.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 6px;
}

.confirm-dialog p {
  color: #6b4f5a;
  line-height: 1.65;
}

.publish-notes {
  padding: 12px 14px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #fffbfc;
  white-space: pre-wrap;
}

.publish-warning {
  padding: 12px 14px;
  border: 1px solid #efb4c5;
  border-radius: 16px;
  background: #fff5f8;
  color: #a12b4a !important;
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
    min-width: 1080px;
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

@media (max-width: 620px) {
  .field-grid {
    grid-template-columns: 1fr;
  }

  .field-grid .span-two {
    grid-column: auto;
  }
}
</style>
