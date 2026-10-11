<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'

import type {
  OfflinePolicy,
  OfflinePolicyAcknowledgement,
  OfflinePolicyInput,
} from '@/api/adminContent'
import {
  BYTES_PER_MEGABYTE,
  OFFLINE_POLICY_LIMITS,
  useOfflinePolicyStore,
} from '@/features/content/application/offlinePolicyStore'
import { useFamilyAccountStore } from '@/features/family/application/familyAccountStore'

interface PolicyForm {
  offlineEnabled: boolean
  maxCachedPackages: number
  maxCacheMegabytes: number
  cacheTtlHours: number
  downloadOverWifiOnly: boolean
}

const store = useOfflinePolicyStore()
const familyStore = useFamilyAccountStore()

const formErrors = reactive<Record<string, string>>({})
const form = reactive<PolicyForm>(defaultForm())
const selectedFamilyId = ref('')

const isDirty = computed(() => {
  const current = store.policy
  return current !== null && !sameAsForm(current)
})

onMounted(async () => {
  await Promise.all([store.load(), familyStore.load()])
  syncFormFromPolicy()
})

watch(
  () => store.policy,
  () => {
    syncFormFromPolicy()
  },
)

function defaultForm(): PolicyForm {
  return {
    offlineEnabled: true,
    maxCachedPackages: 20,
    maxCacheMegabytes: 512,
    cacheTtlHours: 720,
    downloadOverWifiOnly: true,
  }
}

function syncFormFromPolicy(): void {
  const current = store.policy
  if (current === null) {
    return
  }
  Object.assign(form, {
    offlineEnabled: current.offlineEnabled,
    maxCachedPackages: current.maxCachedPackages,
    maxCacheMegabytes: Math.round(current.maxCacheBytes / BYTES_PER_MEGABYTE),
    cacheTtlHours: current.cacheTtlHours,
    downloadOverWifiOnly: current.downloadOverWifiOnly,
  })
  clearErrors()
}

function sameAsForm(current: OfflinePolicy): boolean {
  return (
    form.offlineEnabled === current.offlineEnabled &&
    form.maxCachedPackages === current.maxCachedPackages &&
    form.maxCacheMegabytes === Math.round(current.maxCacheBytes / BYTES_PER_MEGABYTE) &&
    form.cacheTtlHours === current.cacheTtlHours &&
    form.downloadOverWifiOnly === current.downloadOverWifiOnly
  )
}

function clearErrors(): void {
  for (const key of Object.keys(formErrors)) {
    delete formErrors[key]
  }
}

function validate(): boolean {
  clearErrors()
  const limits = OFFLINE_POLICY_LIMITS
  if (
    !Number.isInteger(form.maxCachedPackages) ||
    form.maxCachedPackages < limits.minCachedPackages ||
    form.maxCachedPackages > limits.maxCachedPackages
  ) {
    formErrors.maxCachedPackages = `可缓存内容包数量需在 ${limits.minCachedPackages} - ${limits.maxCachedPackages} 之间`
  }
  if (
    !Number.isFinite(form.maxCacheMegabytes) ||
    form.maxCacheMegabytes < limits.minCacheMegabytes ||
    form.maxCacheMegabytes > limits.maxCacheMegabytes
  ) {
    formErrors.maxCacheMegabytes = `缓存空间需在 ${limits.minCacheMegabytes} MB - ${limits.maxCacheMegabytes} MB 之间`
  }
  if (
    !Number.isInteger(form.cacheTtlHours) ||
    form.cacheTtlHours < limits.minCacheTtlHours ||
    form.cacheTtlHours > limits.maxCacheTtlHours
  ) {
    formErrors.cacheTtlHours = `缓存有效期需在 ${limits.minCacheTtlHours} - ${limits.maxCacheTtlHours} 小时之间`
  }
  return Object.keys(formErrors).length === 0
}

async function submit(): Promise<void> {
  if (!validate()) {
    return
  }
  const input: OfflinePolicyInput = {
    offlineEnabled: form.offlineEnabled,
    maxCachedPackages: form.maxCachedPackages,
    maxCacheBytes: Math.round(form.maxCacheMegabytes * BYTES_PER_MEGABYTE),
    cacheTtlHours: form.cacheTtlHours,
    downloadOverWifiOnly: form.downloadOverWifiOnly,
  }
  const result = await store.save(input)
  if (!result.succeeded && result.error) {
    formErrors.submit = result.error.message
  }
}

async function refreshAcknowledgements(): Promise<void> {
  await store.loadAcknowledgements(selectedFamilyId.value)
}

function resetForm(): void {
  syncFormFromPolicy()
}

function acknowledgementLabel(entry: OfflinePolicyAcknowledgement): string {
  return entry.upToDate ? '已是最新' : `落后版本（当前 ${entry.revision}）`
}
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">内容运营</p>
        <h1>离线下载策略</h1>
        <p class="page-description">
          统一设置设备可离线缓存的内容规模与有效期，保存后会作为新版本下发给全部设备。
        </p>
      </div>
      <div class="header-actions">
        <span class="revision-badge">
          当前版本
          <strong>{{ store.policy?.revision ?? 0 }}</strong>
        </span>
        <button type="button" :disabled="store.isLoading" @click="store.load">
          {{ store.isLoading ? '正在加载…' : '重新加载' }}
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
      </p>
    </Transition>

    <div v-if="store.isLoading && store.policy === null" class="state-panel">
      <span class="state-spinner" aria-hidden="true"></span>
      正在读取离线下载策略…
    </div>

    <form v-else class="policy-form" @submit.prevent="submit">
      <div class="toggle-row">
        <label class="switch">
          <input v-model="form.offlineEnabled" type="checkbox" />
          <span class="switch-track" aria-hidden="true"></span>
        </label>
        <div class="toggle-copy">
          <strong>启用离线下载</strong>
          <span>关闭后设备仅在联网时播放内容，不再保留离线缓存。</span>
        </div>
      </div>

      <div class="field-grid">
        <label class="field">
          <span>可缓存内容包数量</span>
          <input v-model.number="form.maxCachedPackages" type="number" min="1" max="200" step="1" />
          <small class="field-hint">单台设备最多保留的内容包版本数。</small>
          <small v-if="formErrors.maxCachedPackages" class="field-error">
            {{ formErrors.maxCachedPackages }}
          </small>
        </label>

        <label class="field">
          <span>缓存空间上限（MB）</span>
          <input
            v-model.number="form.maxCacheMegabytes"
            type="number"
            min="16"
            max="8192"
            step="16"
          />
          <small class="field-hint">单台设备离线内容占用的最大磁盘空间。</small>
          <small v-if="formErrors.maxCacheMegabytes" class="field-error">
            {{ formErrors.maxCacheMegabytes }}
          </small>
        </label>

        <label class="field">
          <span>缓存有效期（小时）</span>
          <input v-model.number="form.cacheTtlHours" type="number" min="1" max="8760" step="1" />
          <small class="field-hint">超过有效期且未播放的缓存会被清理。</small>
          <small v-if="formErrors.cacheTtlHours" class="field-error">
            {{ formErrors.cacheTtlHours }}
          </small>
        </label>
      </div>

      <div class="toggle-row">
        <label class="switch">
          <input v-model="form.downloadOverWifiOnly" type="checkbox" />
          <span class="switch-track" aria-hidden="true"></span>
        </label>
        <div class="toggle-copy">
          <strong>仅通过 Wi-Fi 下载</strong>
          <span>避免设备使用移动流量缓存内容。</span>
        </div>
      </div>

      <small v-if="formErrors.submit" class="field-error">{{ formErrors.submit }}</small>

      <div class="form-actions">
        <button
          type="button"
          class="secondary"
          :disabled="store.isSubmitting || !isDirty"
          @click="resetForm"
        >
          还原
        </button>
        <button type="submit" class="primary" :disabled="store.isSubmitting || !isDirty">
          {{ store.isSubmitting ? '正在保存…' : '保存并下发' }}
        </button>
      </div>
    </form>

    <section class="ack-section" aria-label="设备接收情况">
      <header class="ack-header">
        <div>
          <h2>设备接收情况</h2>
          <p class="page-description">选择家庭后查看其设备已应用到的策略版本。</p>
        </div>
        <div class="ack-controls">
          <select v-model="selectedFamilyId">
            <option value="">请选择家庭</option>
            <option
              v-for="family in familyStore.families"
              :key="family.parentAccountId"
              :value="family.parentAccountId"
            >
              {{ family.guardianFamilyName || family.parentDisplayName || family.phone }}
            </option>
          </select>
          <button
            type="button"
            :disabled="selectedFamilyId === '' || store.isLoadingAcknowledgements"
            @click="refreshAcknowledgements"
          >
            {{ store.isLoadingAcknowledgements ? '正在查询…' : '查询' }}
          </button>
        </div>
      </header>

      <p v-if="store.acknowledgementsError" class="error-message" role="alert">
        {{ store.acknowledgementsError.message }}
      </p>

      <div v-if="selectedFamilyId === ''" class="state-panel">选择家庭以查看设备版本。</div>
      <div v-else-if="store.isLoadingAcknowledgements" class="state-panel">
        <span class="state-spinner" aria-hidden="true"></span>
        正在查询设备…
      </div>
      <div v-else-if="store.acknowledgements.length === 0" class="state-panel">
        该家庭暂无可上报离线策略的设备。
      </div>
      <div v-else class="table-shell">
        <table>
          <thead>
            <tr>
              <th>设备</th>
              <th>已应用版本</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="entry in store.acknowledgements" :key="entry.deviceId">
              <td>
                <span class="device-id">{{ entry.deviceId }}</span>
              </td>
              <td>{{ entry.revision }}</td>
              <td>
                <span :class="['status', entry.upToDate ? 'status-ok' : 'status-behind']">
                  {{ acknowledgementLabel(entry) }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
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

h1 {
  margin: 0;
  color: var(--sprout-text);
  font-size: 28px;
}

h2 {
  margin: 0;
  color: var(--sprout-text);
  font-size: 20px;
}

.page-description {
  margin: 8px 0 0;
  color: var(--sprout-text-muted);
  line-height: 1.6;
}

.header-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.revision-badge {
  display: flex;
  align-items: baseline;
  gap: 6px;
  padding: 6px 12px;
  border-radius: 999px;
  background: #fff0f5;
  color: #a12b4a;
  font-size: 13px;
}

.revision-badge strong {
  font-size: 18px;
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
}

button:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.success-message,
.error-message {
  margin: 16px 0 0;
  padding: 12px 16px;
  border-radius: var(--sprout-radius-control);
}

.success-message {
  background: #eaf6ef;
  color: #1f6b45;
}

.error-message {
  background: #fdecef;
  color: #a12b4a;
}

.policy-form {
  display: flex;
  flex-direction: column;
  gap: 20px;
  margin-top: 20px;
  padding: 24px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
}

.toggle-row {
  display: flex;
  align-items: center;
  gap: 14px;
}

.toggle-copy {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.toggle-copy strong {
  color: var(--sprout-text);
}

.toggle-copy span {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.switch {
  position: relative;
  display: inline-flex;
  flex: 0 0 auto;
  width: 46px;
  height: 26px;
}

.switch input {
  position: absolute;
  opacity: 0;
  width: 100%;
  height: 100%;
  margin: 0;
  cursor: pointer;
}

.switch-track {
  width: 46px;
  height: 26px;
  border-radius: 999px;
  background: #e4ddda;
  transition: background 0.18s ease;
}

.switch-track::after {
  content: '';
  position: absolute;
  top: 3px;
  left: 3px;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: #ffffff;
  box-shadow: 0 1px 3px rgb(52 33 42 / 30%);
  transition: transform 0.18s ease;
}

.switch input:checked + .switch-track {
  background: #d94f83;
}

.switch input:checked + .switch-track::after {
  transform: translateX(20px);
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 8px;
  color: var(--sprout-text);
  font-weight: 600;
}

.field input {
  padding: 10px 12px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  font: inherit;
  color: var(--sprout-text);
}

.field-hint {
  color: var(--sprout-text-muted);
  font-weight: 400;
}

.field-error {
  color: #a12b4a;
  font-weight: 600;
}

.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

.ack-section {
  margin-top: 24px;
  padding: 24px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
}

.ack-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
}

.ack-controls {
  display: flex;
  gap: 10px;
}

.ack-controls select {
  min-height: 40px;
  padding: 0 12px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  font: inherit;
  color: var(--sprout-text);
}

.state-panel {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 18px;
  padding: 22px;
  border: 1px dashed var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  color: var(--sprout-text-muted);
}

.state-spinner {
  width: 16px;
  height: 16px;
  border: 2px solid #f3c6d6;
  border-top-color: #d94f83;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.table-shell {
  margin-top: 18px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  overflow: hidden;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  padding: 14px 16px;
  text-align: left;
  border-bottom: 1px solid var(--sprout-outline);
  vertical-align: top;
}

th {
  color: var(--sprout-text-muted);
  font-size: 13px;
  font-weight: 700;
}

.device-id {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.status {
  display: inline-block;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 13px;
  font-weight: 700;
}

.status-ok {
  background: #eaf6ef;
  color: #1f6b45;
}

.status-behind {
  background: #fdecef;
  color: #a12b4a;
}

.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.18s ease;
}

.toast-enter-from,
.toast-leave-to {
  opacity: 0;
}

@media (max-width: 720px) {
  .page-header,
  .ack-header {
    flex-direction: column;
  }

  .field-grid {
    grid-template-columns: 1fr;
  }
}
</style>
