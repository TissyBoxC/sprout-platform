<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import type {
  AdminServiceVersion,
  AdminServiceVersionOperation,
  ServiceVersionOperationStatus,
  ServiceVersionStatus,
} from '@/api/adminVersionManagement'
import {
  isUpgradeableService,
  useVersionManagementStore,
} from '@/features/version_management/application/versionManagementStore'

const store = useVersionManagementStore()
const isUpgradeAllOpen = ref(false)
let pollingTimer: ReturnType<typeof setInterval> | null = null

const currentServiceCount = computed(
  () => store.services.filter((service) => service.status === 'current').length,
)
const upgradeableServiceCount = computed(() => store.outdatedServices.length)
const serviceRows = computed(() => store.services)

onMounted(async () => {
  await store.load()
  syncPolling()
})

onBeforeUnmount(() => {
  stopPolling()
})

watch(
  () => store.hasActiveOperations,
  () => {
    syncPolling()
  },
)

function syncPolling(): void {
  stopPolling()
  if (!store.hasActiveOperations) {
    return
  }
  pollingTimer = setInterval(() => {
    void store.refreshOperations()
  }, 3000)
}

function stopPolling(): void {
  if (pollingTimer === null) {
    return
  }
  clearInterval(pollingTimer)
  pollingTimer = null
}

async function confirmAllUpgrade(): Promise<void> {
  const succeeded = await store.upgradeAll()
  if (succeeded) {
    isUpgradeAllOpen.value = false
  }
}

function statusLabel(status: ServiceVersionStatus): string {
  return (
    {
      current: '已是当前版本',
      outdated: '可以升级',
      unknown: '版本待确认',
      updating: '升级进行中',
      failed: '上次升级未完成',
    }[status] ?? '版本待确认'
  )
}

function operationStatusLabel(status: ServiceVersionOperationStatus): string {
  return (
    {
      queued: '等待开始',
      running: '升级进行中',
      succeeded: '升级完成',
      failed: '升级未完成',
      recovering: '正在恢复',
    }[status] ?? '状态待确认'
  )
}

function serviceReleaseState(serviceId: string) {
  return store.releaseStateForService(serviceId)
}

function targetVersion(service: AdminServiceVersion): string {
  const state = serviceReleaseState(service.id)
  return state.selectedVersion || service.latestVersion
}

function canSelectVersion(service: AdminServiceVersion): boolean {
  return isUpgradeableService(service)
}

function canStartUpgrade(service: AdminServiceVersion): boolean {
  const selectedVersion = targetVersion(service)
  return (
    canSelectVersion(service) &&
    selectedVersion !== '' &&
    selectedVersion !== service.currentVersion &&
    !store.isServiceUpgrading(service.id) &&
    !store.isUpgradingAll
  )
}

function versionOptionLabel(service: AdminServiceVersion, version: string): string {
  const state = serviceReleaseState(service.id)
  const release = state.releases.find((item) => item.version === version)
  const labels = [version]
  if (release?.isLatest || (!release && version === service.latestVersion)) {
    labels.push('最新版本')
  }
  if (release?.isCurrent || version === service.currentVersion) {
    labels.push('当前运行')
  }
  return labels.join(' · ')
}

function changeServiceVersion(serviceId: string, event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLSelectElement)) {
    return
  }
  store.selectServiceVersion(serviceId, target.value)
}

async function startServiceUpgrade(service: AdminServiceVersion): Promise<void> {
  await store.upgradeService(service, targetVersion(service))
}

function formatTime(value: string): string {
  if (!value) {
    return '尚未检查'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '尚未检查'
  }
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function operationDuration(operation: AdminServiceVersionOperation): string {
  const startedAt = new Date(operation.startedAt).getTime()
  const finishedAt = new Date(operation.finishedAt).getTime()
  if (!Number.isFinite(startedAt) || !Number.isFinite(finishedAt)) {
    return ''
  }
  const seconds = Math.max(1, Math.round((finishedAt - startedAt) / 1000))
  return `用时 ${seconds} 秒`
}

function operationTargetLabel(operation: AdminServiceVersionOperation): string {
  if (operation.targetService === 'all') {
    return '全部可升级服务'
  }
  return (
    store.services.find((service) => service.id === operation.targetService)?.displayName ||
    operation.targetService ||
    '服务'
  )
}

function safeOperationLog(value: string): string {
  return value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(
      (line) =>
        line !== '' &&
        !/^goroutine\b/i.test(line) &&
        !/^panic:/i.test(line) &&
        !/^(at |\t?[A-Za-z]:\\|\/)/.test(line),
    )
    .map((line) =>
      line
        .replace(
          /((?:token|password|secret|authorization|api[_-]?key)\s*[:=]\s*)\S+/gi,
          '$1[已隐藏]',
        )
        .slice(0, 240),
    )
    .slice(-12)
    .join('\n')
}

function operationLog(operation: AdminServiceVersionOperation | null): string {
  return operation === null ? '' : safeOperationLog(operation.logTail)
}
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">品牌服务</p>
        <h1>服务版本</h1>
        <p class="page-description">
          管理“如此萌屋”品牌下正在运行的服务，检查仓库版本并完成安全升级。
        </p>
      </div>
      <div class="header-actions">
        <button
          type="button"
          class="secondary"
          :disabled="store.isChecking || store.isUpgrading"
          @click="store.checkForUpdates"
        >
          {{ store.isChecking ? '正在检查…' : '检查更新' }}
        </button>
        <button
          type="button"
          class="primary"
          :disabled="upgradeableServiceCount === 0 || store.isUpgrading || store.isChecking"
          @click="isUpgradeAllOpen = true"
        >
          {{ store.isUpgradingAll ? '正在升级…' : '全部升级' }}
        </button>
      </div>
    </header>

    <div class="self-upgrade-note">
      <span aria-hidden="true">☆</span>
      <p>管理端自身升级时，页面会短暂重载，完成后自动恢复。升级记录会保留，不需要重复操作。</p>
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

    <Transition name="page" mode="out-in">
      <div
        :key="store.isLoading ? 'loading' : store.services.length === 0 ? 'empty' : 'content'"
      >
        <div v-if="store.isLoading" class="state-panel">
          <span class="state-spinner" aria-hidden="true"></span>
          正在读取服务版本…
        </div>
        <div v-else-if="store.services.length === 0" class="state-panel">
          <span class="state-icon" aria-hidden="true">☆</span>
          <span>还没有可管理的服务。完成服务接入后，会显示在这里。</span>
        </div>
        <template v-else>
          <div class="summary-grid" aria-label="服务版本概览">
          <div>
            <span>全部服务</span>
            <strong>{{ store.services.length }}</strong>
          </div>
          <div>
            <span>已是当前版本</span>
            <strong>{{ currentServiceCount }}</strong>
          </div>
          <div :class="{ attention: upgradeableServiceCount > 0 }">
            <span>可以升级</span>
            <strong>{{ upgradeableServiceCount }}</strong>
          </div>
          <div>
            <span>最近检查</span>
            <strong class="time-value">
              {{ formatTime(store.snapshot?.checkedAt ?? '') }}
            </strong>
          </div>
        </div>

        <section class="service-group">
          <div class="group-heading">
            <div>
              <h2>全部服务</h2>
              <p>每个服务一行。品牌服务可选择任意已发布版本，基础设施服务只展示当前状态。</p>
            </div>
            <span>{{ serviceRows.length }} 个服务</span>
          </div>
          <div class="table-shell">
          <table>
            <thead>
              <tr>
                <th>服务</th>
                <th>当前版本</th>
                <th>最新版本</th>
                <th>状态</th>
                <th>选择版本</th>
                <th>升级操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="service in serviceRows" :key="service.id">
                <td>
                  <div class="service-name">
                    <strong>{{ service.displayName || service.id }}</strong>
                    <span v-if="service.isSelf" class="self-badge">管理端自身</span>
                  </div>
                  <code>{{ service.image || '暂未提供镜像信息' }}</code>
                </td>
                <td class="version-cell">{{ service.currentVersion || '未知' }}</td>
                <td class="version-cell">{{ service.latestVersion || '待检查' }}</td>
                <td>
                  <span :class="['status', service.status]">
                    {{ statusLabel(service.status) }}
                  </span>
                  <a
                    v-if="service.releaseUrl"
                    class="release-link"
                    :href="service.releaseUrl"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    查看版本说明
                  </a>
                </td>
                <td>
                  <div class="version-picker">
                    <template v-if="canSelectVersion(service)">
                      <label :for="`service-version-${service.id}`">
                        {{ service.displayName || service.id }}版本
                      </label>
                      <select
                        :id="`service-version-${service.id}`"
                        :value="targetVersion(service)"
                        :disabled="
                          serviceReleaseState(service.id).isLoading ||
                          store.isServiceUpgrading(service.id) ||
                          store.isUpgradingAll
                        "
                        @change="changeServiceVersion(service.id, $event)"
                      >
                        <option
                          v-for="release in serviceReleaseState(service.id).releases"
                          :key="release.version"
                          :value="release.version"
                        >
                          {{ versionOptionLabel(service, release.version) }}
                        </option>
                        <option
                          v-if="serviceReleaseState(service.id).releases.length === 0"
                          :value="targetVersion(service)"
                        >
                          {{ versionOptionLabel(service, targetVersion(service)) }}
                        </option>
                      </select>
                      <small v-if="serviceReleaseState(service.id).isLoading"
                        >正在读取可选版本…</small
                      >
                      <template v-else-if="serviceReleaseState(service.id).error">
                        <small class="picker-error">
                          {{ serviceReleaseState(service.id).error }}
                        </small>
                        <button
                          type="button"
                          class="text-button"
                          @click="store.loadServiceReleases(service)"
                        >
                          重新读取版本
                        </button>
                      </template>
                    </template>
                    <small v-else class="read-only-copy">
                      固定镜像，由部署配置统一维护
                    </small>
                  </div>
                </td>
                <td>
                  <div class="row-action">
                    <template v-if="canSelectVersion(service)">
                      <button
                        type="button"
                        class="upgrade-button"
                        :disabled="!canStartUpgrade(service)"
                        @click="startServiceUpgrade(service)"
                      >
                        {{
                          store.isServiceUpgrading(service.id)
                            ? '正在升级…'
                            : `升级到 ${targetVersion(service) || '所选版本'}`
                        }}
                      </button>
                    </template>
                    <small v-else class="read-only-copy">不需要升级操作</small>
                    <small class="checked-at">
                      {{ formatTime(service.lastCheckedAt) }}
                    </small>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
          </div>
          </section>

          <section class="operations-panel">
          <div class="panel-heading">
            <div>
              <h2>升级记录</h2>
              <p>升级进行时每 3 秒更新一次，完成后自动停止刷新。</p>
            </div>
            <span v-if="store.hasActiveOperations" class="live-indicator">
              <i aria-hidden="true"></i>
              正在更新
            </span>
          </div>
          <div v-if="store.operations.length === 0" class="operation-empty">
            还没有升级记录。开始升级后，进度会显示在这里。
          </div>
          <ul v-else class="operation-list">
            <li v-for="operation in store.operations.slice(0, 8)" :key="operation.id">
              <div>
                <strong>{{ operationTargetLabel(operation) }}</strong>
                <small>
                  {{ operation.currentVersion || '未知' }} →
                  {{ operation.targetVersion || '目标版本待确认' }}
                </small>
              </div>
              <span :class="['operation-status', operation.status]">
                {{ operationStatusLabel(operation.status) }}
              </span>
              <time>{{ formatTime(operation.startedAt) }}</time>
              <button type="button" @click="store.selectOperation(operation.id)">查看进度</button>
            </li>
          </ul>
          </section>
        </template>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="isUpgradeAllOpen" class="dialog-backdrop" @click.self="isUpgradeAllOpen = false">
        <section class="dialog" role="dialog" aria-modal="true">
          <span class="dialog-mark" aria-hidden="true">☆</span>
          <h2>升级全部服务？</h2>
          <p>将依次升级 {{ upgradeableServiceCount }} 个可以升级的服务。相关功能可能短暂不可用。</p>
          <p class="warning-copy">
            管理端自身升级时页面会短暂重载，完成后自动恢复。升级记录会保留。
          </p>
          <div class="dialog-actions">
            <button
              type="button"
              class="secondary"
              :disabled="store.isUpgradingAll"
              @click="isUpgradeAllOpen = false"
            >
              暂不升级
            </button>
            <button
              type="button"
              class="primary"
              :disabled="store.isUpgradingAll"
              @click="confirmAllUpgrade"
            >
              {{ store.isUpgradingAll ? '正在开始…' : '立即升级全部服务' }}
            </button>
          </div>
        </section>
      </div>
    </Transition>

    <Transition name="modal">
      <div
        v-if="store.selectedOperation"
        class="dialog-backdrop"
        @click.self="store.closeOperation"
      >
        <section class="dialog operation-dialog" role="dialog" aria-modal="true">
          <div class="operation-dialog-heading">
            <div>
              <p class="eyebrow">升级进度</p>
              <h2>{{ operationTargetLabel(store.selectedOperation) }}</h2>
            </div>
            <span :class="['operation-status', store.selectedOperation.status]">
              {{ operationStatusLabel(store.selectedOperation.status) }}
            </span>
          </div>
          <dl class="operation-meta">
            <div>
              <dt>版本变化</dt>
              <dd>
                {{ store.selectedOperation.currentVersion || '未知' }}
                →
                {{ store.selectedOperation.targetVersion || '目标版本待确认' }}
              </dd>
            </div>
            <div>
              <dt>开始时间</dt>
              <dd>{{ formatTime(store.selectedOperation.startedAt) }}</dd>
            </div>
            <div>
              <dt>完成时间</dt>
              <dd>
                {{ formatTime(store.selectedOperation.finishedAt) }}
                <small v-if="operationDuration(store.selectedOperation)">
                  · {{ operationDuration(store.selectedOperation) }}
                </small>
              </dd>
            </div>
          </dl>
          <p v-if="store.selectedOperation.message" class="operation-message">
            {{ store.selectedOperation.message }}
          </p>
          <div v-if="operationLog(store.selectedOperation)" class="log-panel">
            <h3>运行记录</h3>
            <pre>{{ operationLog(store.selectedOperation) }}</pre>
          </div>
          <div class="dialog-actions">
            <button type="button" class="secondary" @click="store.closeOperation">关闭</button>
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
h3,
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
  justify-content: flex-end;
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
  background: linear-gradient(180deg, #b23a68, #962c55);
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
  background: linear-gradient(180deg, #a12b5c, #85264b);
  color: #ffffff;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.54;
}

.self-upgrade-note {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 14px;
  padding: 12px 16px;
  border: 1px solid #f3d18e;
  border-radius: 16px;
  background: #fffaf0;
  color: #6b4f2a;
}

.self-upgrade-note span {
  display: grid;
  width: 28px;
  height: 28px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 10px;
  background: #ffe9ad;
  color: #8a5a00;
}

.self-upgrade-note p {
  margin: 0;
  line-height: 1.55;
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

.summary-grid .time-value {
  font-size: 15px;
  line-height: 1.5;
}

.table-shell {
  overflow: hidden;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.service-group {
  margin-top: 18px;
}

.group-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 18px;
  margin-bottom: 10px;
}

.group-heading h2,
.group-heading p {
  margin: 0;
}

.group-heading h2 {
  color: var(--sprout-text);
  font-size: 17px;
}

.group-heading p {
  margin-top: 4px;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.group-heading > span {
  color: var(--sprout-text-muted);
  font-size: 13px;
  white-space: nowrap;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  padding: 15px 16px;
  border-bottom: 1px solid var(--sprout-outline-soft);
  text-align: left;
  vertical-align: top;
}

th {
  color: var(--sprout-text-muted);
  font-size: 13px;
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

.service-name {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 7px;
}

.service-name strong {
  font-size: 15px;
}

.service-name p,
td p {
  margin: 5px 0 0;
  color: var(--sprout-text-muted);
}

.service-name small,
td small {
  display: block;
  margin-top: 5px;
  color: #856674;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 11px;
}

.self-badge {
  padding: 3px 8px;
  border-radius: 999px;
  background: #eef8ff;
  color: #27627f;
  font-size: 11px;
  font-weight: 700;
}

code {
  display: block;
  max-width: 280px;
  color: #5b3b49;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  line-height: 1.5;
  overflow-wrap: anywhere;
  white-space: normal;
}

.status,
.operation-status {
  display: inline-flex;
  align-items: center;
  width: fit-content;
  padding: 5px 10px;
  border-radius: 999px;
  background: #f5eef1;
  color: #6b4f5a;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.status.current,
.operation-status.succeeded {
  background: #e7f8ee;
  color: #1d6b3f;
}

.status.outdated,
.operation-status.queued {
  background: #fff3cd;
  color: #755600;
}

.status.updating,
.operation-status.running,
.operation-status.recovering {
  background: #eaf5ff;
  color: #27627f;
}

.status.failed,
.operation-status.failed {
  background: #fff0f2;
  color: #a12b4a;
}

.release-link {
  display: block;
  width: fit-content;
  margin-top: 8px;
  color: #b23a68;
  font-size: 12px;
  font-weight: 700;
  text-decoration: none;
}

.release-link:hover {
  color: #8e2852;
  text-decoration: underline;
}

.upgrade-button {
  white-space: nowrap;
}

.version-cell {
  color: #7b5263;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
}

.version-picker {
  display: grid;
  min-width: 220px;
  gap: 7px;
}

.version-picker label {
  color: var(--sprout-text-muted);
  font-size: 12px;
  font-weight: 700;
}

.version-picker select {
  width: 100%;
  min-height: 38px;
  padding: 0 34px 0 12px;
  border: 1px solid #e9a5b8;
  border-radius: 12px;
  background-color: #ffffff;
  color: var(--sprout-text);
  font: inherit;
  font-size: 13px;
}

.version-picker select:focus {
  border-color: #d94f83;
  outline: 3px solid rgb(217 79 131 / 16%);
}

.version-picker select:disabled {
  cursor: not-allowed;
  opacity: 0.62;
}

.picker-error {
  color: #a12b4a;
  font-family: inherit;
}

.text-button {
  width: fit-content;
  min-height: 30px;
  padding: 0 10px;
  border-radius: 10px;
  background: #fff7fa;
  font-size: 12px;
}

.read-only-copy {
  margin: 0;
  color: var(--sprout-text-muted);
  font-size: 12px;
  line-height: 1.5;
}

.row-action {
  display: grid;
  min-width: 190px;
  gap: 7px;
}

.checked-at {
  color: #8a6b78;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 11px;
}

.operations-panel {
  margin-top: 18px;
  overflow: hidden;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.panel-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 19px 22px;
  border-bottom: 1px solid var(--sprout-outline-soft);
}

.panel-heading h2,
.panel-heading p {
  margin: 0;
}

.panel-heading h2 {
  color: var(--sprout-text);
  font-size: 18px;
}

.panel-heading p {
  margin-top: 5px;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.live-indicator {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  color: #27627f;
  font-size: 13px;
  font-weight: 700;
}

.live-indicator i {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #66ccff;
  box-shadow: 0 0 0 5px rgb(102 204 255 / 15%);
  animation: pulse 1.6s ease-in-out infinite;
}

.operation-empty {
  padding: 24px 22px;
  color: var(--sprout-text-muted);
}

.operation-list {
  display: grid;
  margin: 0;
  padding: 0;
  list-style: none;
}

.operation-list li {
  display: grid;
  grid-template-columns: minmax(180px, 1.2fr) auto auto auto;
  align-items: center;
  gap: 16px;
  padding: 14px 22px;
  border-bottom: 1px solid var(--sprout-outline-soft);
}

.operation-list li:last-child {
  border-bottom: 0;
}

.operation-list div {
  display: grid;
  gap: 4px;
}

.operation-list small,
.operation-list time {
  color: var(--sprout-text-muted);
  font-size: 12px;
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

.state-icon,
.dialog-mark {
  display: grid;
  place-items: center;
  background: #fff2f5;
  color: #d94f83;
}

.state-icon {
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  border-radius: 12px;
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
  width: min(100%, 560px);
  max-height: min(820px, calc(100vh - 40px));
  gap: 15px;
  overflow: auto;
  padding: 28px;
  border: 1px solid var(--sprout-outline);
  border-radius: 28px;
  background: #ffffff;
  box-shadow: 0 28px 72px rgb(74 46 59 / 22%);
}

.dialog-mark {
  width: 42px;
  height: 42px;
  border-radius: 15px;
  font-size: 22px;
}

.dialog h2,
.dialog p,
.dialog h3 {
  margin: 0;
}

.dialog h2 {
  color: var(--sprout-text);
}

.dialog p {
  color: var(--sprout-text-muted);
  line-height: 1.65;
}

.warning-copy {
  padding: 13px 15px;
  border: 1px solid #f3d18e;
  border-radius: 16px;
  background: #fffaf0;
  color: #6b4f2a !important;
}

.operation-dialog-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.operation-meta {
  display: grid;
  gap: 10px;
  margin: 0;
}

.operation-meta > div {
  display: grid;
  grid-template-columns: 90px minmax(0, 1fr);
  gap: 12px;
  padding: 10px 12px;
  border-radius: 14px;
  background: #fff9fb;
}

.operation-meta dt {
  color: var(--sprout-text-muted);
}

.operation-meta dd {
  margin: 0;
  color: var(--sprout-text);
}

.operation-message {
  padding: 12px 14px;
  border-radius: 14px;
  background: #fff2f5;
}

.log-panel {
  display: grid;
  gap: 8px;
}

.log-panel h3 {
  font-size: 14px;
}

.log-panel pre {
  max-height: 240px;
  margin: 0;
  overflow: auto;
  padding: 14px;
  border-radius: 14px;
  background: #35232b;
  color: #ffeff5;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  line-height: 1.55;
  white-space: pre-wrap;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@keyframes pulse {
  50% {
    opacity: 0.5;
    transform: scale(0.82);
  }
}

@media (max-width: 1120px) {
  .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .table-shell {
    overflow-x: auto;
  }

  table {
    min-width: 1040px;
  }
}

@media (max-width: 760px) {
  .page-header {
    display: grid;
  }

  .header-actions {
    justify-content: flex-start;
  }

  .operation-list li {
    grid-template-columns: 1fr auto;
  }

  .operation-list time {
    grid-column: 1 / -1;
  }
}

@media (max-width: 520px) {
  .summary-grid {
    grid-template-columns: 1fr;
  }

  .operation-meta > div {
    grid-template-columns: 1fr;
    gap: 4px;
  }
}
</style>
