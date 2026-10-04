<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import type {
  AdminDiagnosticDevice,
  DeviceBootEvent,
  DeviceDiagnosticsSnapshot,
  DeviceHealthState,
  DeviceInteractionEvent,
  DeviceModuleFailure,
  DeviceRecoveryEvent,
} from '@/api/adminDeviceDiagnostics'
import { useDeviceDiagnosticsStore } from '@/features/audit/application/deviceDiagnosticsStore'

const store = useDeviceDiagnosticsStore()
const searchQuery = ref('')

const visibleDevices = computed(() => {
  const query = searchQuery.value.trim().toLocaleLowerCase('zh-CN')
  if (!query) {
    return store.devices
  }
  return store.devices.filter((device) =>
    [device.deviceName, device.hardwareModel, device.firmwareVersion]
      .join(' ')
      .toLocaleLowerCase('zh-CN')
      .includes(query),
  )
})

onMounted(() => {
  void store.loadDevices()
})

async function selectDevice(device: AdminDiagnosticDevice): Promise<void> {
  await store.selectDevice(device.deviceId)
}

function healthLabel(value: DeviceHealthState): string {
  return (
    {
      healthy: '运行正常',
      degraded: '需要留意',
      faulted: '需要处理',
      unknown: '状态待确认',
    }[value] ?? '状态待确认'
  )
}

function healthDescription(value: DeviceHealthState): string {
  return (
    {
      healthy: '最近上报中没有发现需要处理的异常。',
      degraded: '设备仍可使用，但出现了需要关注的异常。',
      faulted: '设备近期出现持续故障，建议尽快检查。',
      unknown: '设备还没有提供足够的运行信息。',
    }[value] ?? '设备还没有提供足够的运行信息。'
  )
}

function deviceHealth(deviceId: string): DeviceHealthState {
  return store.snapshots[deviceId]?.healthState ?? 'unknown'
}

function resetReasonLabel(value: string): string {
  return (
    {
      power_on: '开机启动',
      software: '软件重启',
      software_restart: '软件重启',
      panic: '异常重启',
      interrupt_watchdog: '系统守护重启',
      task_watchdog: '任务守护重启',
      watchdog: '系统守护重启',
      brownout: '供电波动后恢复',
      deep_sleep: '唤醒启动',
      external: '外部复位',
    }[value] ?? '其他启动原因'
  )
}

function moduleLabel(value: string): string {
  return (
    {
      audio_service: '语音服务',
      device_binding_client: '设备绑定',
      device_runtime_reporter: '运行状态上报',
      display_service: '显示服务',
      error_recovery: '故障恢复',
      firmware_updater: '系统更新',
      mqtt_client: '消息连接',
      network_manager: '网络连接',
      parent_policy: '家庭设置',
      storage: '本地存储',
      wifi_manager: '无线网络',
    }[value] ?? '设备系统'
  )
}

function failureLabel(value: string): string {
  return (
    {
      ESP_ERR_INVALID_ARG: '配置信息不完整',
      ESP_ERR_INVALID_STATE: '设备状态异常',
      ESP_ERR_NO_MEM: '设备空间不足',
      ESP_ERR_TIMEOUT: '连接等待超时',
      ESP_FAIL: '操作没有完成',
    }[value] ?? '操作没有完成'
  )
}

function formatTime(value: string): string {
  if (!value) {
    return '时间待补充'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '时间待补充'
  }
  return date.toLocaleString('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatUptime(value: number): string {
  if (value <= 0) {
    return '刚刚启动'
  }
  if (value < 60_000) {
    return `启动 ${Math.max(1, Math.round(value / 1000))} 秒后上报`
  }
  if (value < 3_600_000) {
    return `启动 ${Math.round(value / 60_000)} 分钟后上报`
  }
  return `启动 ${(value / 3_600_000).toFixed(1)} 小时后上报`
}

function retentionSummary(snapshot: DeviceDiagnosticsSnapshot): string {
  const parts = [
    snapshot.retentionBootEvents > 0 ? `启动记录 ${snapshot.retentionBootEvents} 条` : '',
    snapshot.retentionFailures > 0 ? `故障记录 ${snapshot.retentionFailures} 条` : '',
    snapshot.retentionRecoveryEvents > 0 ? `恢复记录 ${snapshot.retentionRecoveryEvents} 条` : '',
    snapshot.retentionInteractionEvents > 0
      ? `交互记录 ${snapshot.retentionInteractionEvents} 条`
      : '',
  ].filter(Boolean)
  return parts.length > 0
    ? `平台会保留最近的 ${parts.join('、')}，更早的记录会自动清理。`
    : '平台会保留最近的启动、故障、恢复和交互记录，更早的记录会自动清理。'
}

function eventKey(event: { eventId: string }): string {
  return event.eventId
}

function bootEvents(snapshot: DeviceDiagnosticsSnapshot): DeviceBootEvent[] {
  return snapshot.bootEvents
}

function moduleFailures(snapshot: DeviceDiagnosticsSnapshot): DeviceModuleFailure[] {
  return snapshot.failures
}

function recoveryEvents(snapshot: DeviceDiagnosticsSnapshot): DeviceRecoveryEvent[] {
  return snapshot.recoveryEvents
}

function interactionEvents(snapshot: DeviceDiagnosticsSnapshot): DeviceInteractionEvent[] {
  return snapshot.interactionEvents
}

function interactionLabel(eventType: string): string {
  return (
    {
      wake_detected: '设备已唤醒',
      wake_rejected: '唤醒未采纳',
      button_gesture: '按键操作',
      indicator_state: '指示灯状态变化',
      factory_reset_requested: '设备请求恢复出厂',
      factory_reset_cancelled: '设备取消恢复出厂',
      factory_reset_completed: '设备完成恢复出厂',
      factory_reset_failed: '设备恢复出厂失败',
    }[eventType] ?? '设备交互'
  )
}

function interactionDetail(event: DeviceInteractionEvent): string {
  const detail = event.detailCode.trim()
  if (detail === '') {
    return '设备已记录一次交互。'
  }
  return `操作标识：${detail}`
}

function formatDuration(value: number): string {
  if (value <= 0) {
    return ''
  }
  if (value < 1000) {
    return `持续 ${value} 毫秒`
  }
  return `持续 ${(value / 1000).toFixed(1)} 秒`
}
</script>

<template>
  <section class="diagnostics-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">设备健康</p>
        <h1>设备诊断</h1>
        <p class="page-description">
          查看设备的启动记录、最近故障和恢复情况，及时发现问题并安排维护。
        </p>
      </div>
      <button
        type="button"
        class="refresh-button"
        :disabled="store.isLoadingDevices || store.isLoadingDiagnostics"
        @click="store.refresh"
      >
        {{ store.isLoadingDevices || store.isLoadingDiagnostics ? '正在刷新…' : '刷新诊断' }}
      </button>
    </header>

    <Transition name="toast">
      <p v-if="store.error && store.devices.length > 0" class="error-message" role="alert">
        {{ store.error.message }}
      </p>
    </Transition>

    <Transition name="page" mode="out-in">
      <div
        v-if="store.isLoadingDevices && store.devices.length === 0"
        key="loading"
        class="state-panel"
      >
        <span class="state-spinner" aria-hidden="true"></span>
        正在读取设备…
      </div>
      <div v-else-if="store.error" key="error" class="state-panel error-state">
        <span class="state-icon" aria-hidden="true">!</span>
        <span>{{ store.error.message }}</span>
        <button type="button" @click="store.loadDevices">重新加载设备</button>
      </div>
      <div v-else-if="store.devices.length === 0" key="empty" class="state-panel">
        <span class="state-icon" aria-hidden="true">☆</span>
        <span>还没有绑定设备。家长完成设备连接后，这里会显示设备健康信息。</span>
      </div>
      <div v-else key="content" class="diagnostics-layout">
        <aside class="device-panel">
          <div class="panel-heading">
            <div>
              <h2>选择设备</h2>
              <p>{{ store.devices.length }} 台设备，{{ store.onlineDeviceCount }} 台在线</p>
            </div>
          </div>
          <label class="search-field">
            <span class="sr-only">搜索设备</span>
            <input v-model="searchQuery" type="search" placeholder="搜索设备名称或型号" />
          </label>
          <div v-if="visibleDevices.length === 0" class="device-empty">
            没有找到符合搜索条件的设备。
          </div>
          <ul v-else class="device-list" aria-label="设备列表">
            <li v-for="device in visibleDevices" :key="device.deviceId">
              <button
                type="button"
                :class="['device-option', { selected: store.selectedDeviceId === device.deviceId }]"
                :aria-pressed="store.selectedDeviceId === device.deviceId"
                @click="selectDevice(device)"
              >
                <span class="device-option-main">
                  <strong>{{ device.deviceName || '未命名设备' }}</strong>
                  <span
                    :class="['health-dot', deviceHealth(device.deviceId)]"
                    :aria-label="healthLabel(deviceHealth(device.deviceId))"
                  ></span>
                </span>
                <span class="device-option-meta">
                  {{ device.hardwareModel || '初芽' }} ·
                  {{ device.firmwareVersion || '版本待补充' }}
                </span>
                <span :class="['online-state', device.isOnline ? 'online' : 'offline']">
                  {{ device.isOnline ? '在线' : '离线' }}
                </span>
              </button>
            </li>
          </ul>
        </aside>

        <main class="diagnostics-content">
          <template v-if="store.selectedDevice">
            <section class="device-overview">
              <div class="overview-main">
                <p class="eyebrow">当前设备</p>
                <h2>{{ store.selectedDevice.deviceName || '未命名设备' }}</h2>
                <p class="overview-meta">
                  {{ store.selectedDevice.hardwareModel || '初芽' }} ·
                  {{ store.selectedDevice.firmwareVersion || '版本待补充' }} ·
                  {{ store.selectedDevice.isOnline ? '在线' : '离线' }}
                </p>
              </div>
              <div v-if="store.snapshot" :class="['health-summary', store.snapshot.healthState]">
                <span class="health-summary-label">{{
                  healthLabel(store.snapshot.healthState)
                }}</span>
                <small>{{ healthDescription(store.snapshot.healthState) }}</small>
              </div>
              <div v-else class="health-summary unknown">
                <span class="health-summary-label">状态待确认</span>
                <small>正在等待设备提供运行信息。</small>
              </div>
            </section>

            <Transition name="toast">
              <p v-if="store.diagnosticsError" class="inline-warning" role="alert">
                {{ store.diagnosticsError.message }}
                <button type="button" @click="store.loadDiagnostics()">重新加载</button>
              </p>
            </Transition>

            <div v-if="store.isLoadingDiagnostics && !store.snapshot" class="state-panel compact">
              <span class="state-spinner" aria-hidden="true"></span>
              正在读取设备健康信息…
            </div>
            <div v-else-if="!store.snapshot" class="state-panel compact">
              <span class="state-icon" aria-hidden="true">☆</span>
              <span>这台设备还没有上报健康信息，设备联网后会自动补充。</span>
            </div>
            <template v-else>
              <section class="metric-grid" aria-label="诊断摘要">
                <article class="metric-card">
                  <p class="metric-label">当前状态</p>
                  <p :class="['metric-value', 'health-text', store.snapshot.healthState]">
                    {{ healthLabel(store.snapshot.healthState) }}
                  </p>
                  <p class="metric-detail">最近同步 {{ formatTime(store.snapshot.updatedAt) }}</p>
                </article>
                <article class="metric-card">
                  <p class="metric-label">故障记录</p>
                  <p class="metric-value">{{ store.snapshot.errorCount }}</p>
                  <p class="metric-detail">
                    {{
                      store.snapshot.latestFailure
                        ? `最近一次 ${formatTime(store.snapshot.latestFailure.reportedAt)}`
                        : '最近没有故障记录'
                    }}
                  </p>
                </article>
                <article class="metric-card">
                  <p class="metric-label">恢复记录</p>
                  <p class="metric-value">{{ store.snapshot.recoveryCount }}</p>
                  <p class="metric-detail">
                    {{
                      store.snapshot.recoveryCount > 0
                        ? '设备已从部分异常中恢复'
                        : '最近没有恢复记录'
                    }}
                  </p>
                </article>
                <article class="metric-card">
                  <p class="metric-label">最近上报</p>
                  <p class="metric-value time-value">{{ formatTime(store.snapshot.updatedAt) }}</p>
                  <p class="metric-detail">
                    {{
                      store.lastLoadedAt ? `本次刷新 ${formatTime(store.lastLoadedAt)}` : '等待刷新'
                    }}
                  </p>
                </article>
              </section>

              <section class="detail-grid">
                <article class="detail-panel">
                  <header class="panel-heading">
                    <div>
                      <h2>启动历史</h2>
                      <p>最近的设备启动和重启记录。</p>
                    </div>
                    <span class="panel-count">{{ bootEvents(store.snapshot).length }} 条</span>
                  </header>
                  <div v-if="bootEvents(store.snapshot).length === 0" class="list-empty">
                    还没有启动记录。设备下一次启动后会自动显示。
                  </div>
                  <ul v-else class="event-list">
                    <li v-for="event in bootEvents(store.snapshot)" :key="eventKey(event)">
                      <span class="event-icon boot" aria-hidden="true">↑</span>
                      <div class="event-body">
                        <div class="event-title">
                          <strong>{{ resetReasonLabel(event.resetReason) }}</strong>
                          <time>{{ formatTime(event.reportedAt) }}</time>
                        </div>
                        <p>
                          {{ formatUptime(event.uptimeMs) }} · 第 {{ event.bootCount }} 次启动 ·
                          版本 {{ event.firmwareVersion || '待补充' }}
                        </p>
                      </div>
                    </li>
                  </ul>
                </article>

                <article class="detail-panel">
                  <header class="panel-heading">
                    <div>
                      <h2>最近故障</h2>
                      <p>按时间倒序显示最近出现的问题。</p>
                    </div>
                    <span class="panel-count">{{ moduleFailures(store.snapshot).length }} 条</span>
                  </header>
                  <div v-if="moduleFailures(store.snapshot).length === 0" class="list-empty">
                    最近没有故障记录，设备运行状态良好。
                  </div>
                  <ul v-else class="event-list">
                    <li v-for="failure in moduleFailures(store.snapshot)" :key="eventKey(failure)">
                      <span class="event-icon failure" aria-hidden="true">!</span>
                      <div class="event-body">
                        <div class="event-title">
                          <strong>{{ moduleLabel(failure.moduleName) }}出现异常</strong>
                          <time>{{ formatTime(failure.reportedAt) }}</time>
                        </div>
                        <p>
                          {{ failureLabel(failure.errorCode) }} · 已出现
                          {{ failure.failureCount }} 次 · 版本
                          {{ failure.firmwareVersion || '待补充' }}
                        </p>
                      </div>
                    </li>
                  </ul>
                </article>

                <article class="detail-panel recovery-panel">
                  <header class="panel-heading">
                    <div>
                      <h2>恢复事件</h2>
                      <p>记录设备从异常中恢复的时间。</p>
                    </div>
                    <span class="panel-count">{{ recoveryEvents(store.snapshot).length }} 条</span>
                  </header>
                  <div v-if="recoveryEvents(store.snapshot).length === 0" class="list-empty">
                    还没有恢复记录。设备恢复正常后会自动显示。
                  </div>
                  <ul v-else class="event-list">
                    <li v-for="event in recoveryEvents(store.snapshot)" :key="eventKey(event)">
                      <span class="event-icon recovery" aria-hidden="true">✓</span>
                      <div class="event-body">
                        <div class="event-title">
                          <strong>{{ moduleLabel(event.moduleName) }}已恢复</strong>
                          <time>{{ formatTime(event.reportedAt) }}</time>
                        </div>
                        <p>版本 {{ event.firmwareVersion || '待补充' }}</p>
                      </div>
                    </li>
                  </ul>
                </article>

                <article class="detail-panel recovery-panel">
                  <header class="panel-heading">
                    <div>
                      <h2>交互事件</h2>
                      <p>记录唤醒、按键、指示灯和恢复出厂相关操作。</p>
                    </div>
                    <span class="panel-count">{{ interactionEvents(store.snapshot).length }} 条</span>
                  </header>
                  <div v-if="interactionEvents(store.snapshot).length === 0" class="list-empty">
                    最近没有交互记录。设备发生唤醒、按键或恢复出厂操作后会显示在这里。
                  </div>
                  <ul v-else class="event-list">
                    <li
                      v-for="event in interactionEvents(store.snapshot)"
                      :key="eventKey(event)"
                    >
                      <span class="event-icon boot" aria-hidden="true">•</span>
                      <div class="event-body">
                        <div class="event-title">
                          <strong>{{ interactionLabel(event.eventType) }}</strong>
                          <time>{{ formatTime(event.reportedAt) }}</time>
                        </div>
                        <p>
                          {{ interactionDetail(event) }}
                          <template v-if="formatDuration(event.durationMs)">
                            · {{ formatDuration(event.durationMs) }}
                          </template>
                        </p>
                      </div>
                    </li>
                  </ul>
                </article>

                <article class="retention-panel">
                  <header class="panel-heading">
                    <div>
                      <h2>记录保留说明</h2>
                      <p>帮助控制维护成本，同时保留近期最重要的信息。</p>
                    </div>
                  </header>
                  <p>{{ retentionSummary(store.snapshot) }}</p>
                  <dl class="retention-grid">
                    <div>
                      <dt>启动历史</dt>
                      <dd>最多 {{ store.snapshot.retentionBootEvents || 0 }} 条</dd>
                    </div>
                    <div>
                      <dt>故障记录</dt>
                      <dd>最多 {{ store.snapshot.retentionFailures || 0 }} 条</dd>
                    </div>
                    <div>
                      <dt>恢复记录</dt>
                      <dd>最多 {{ store.snapshot.retentionRecoveryEvents || 0 }} 条</dd>
                    </div>
                    <div>
                      <dt>交互记录</dt>
                      <dd>最多 {{ store.snapshot.retentionInteractionEvents || 0 }} 条</dd>
                    </div>
                  </dl>
                </article>
              </section>
            </template>
          </template>
          <div v-else class="state-panel compact">
            <span class="state-icon" aria-hidden="true">☆</span>
            <span>请从左侧选择一台设备查看健康信息。</span>
          </div>
        </main>
      </div>
    </Transition>
  </section>
</template>

<style scoped>
.diagnostics-page {
  min-width: 0;
}

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

.refresh-button {
  min-height: 40px;
  flex: 0 0 auto;
  padding: 0 18px;
  border: 1px solid #d94f83;
  border-radius: var(--sprout-radius-control);
  background: #d94f83;
  color: #ffffff;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
  box-shadow: 0 10px 24px rgb(217 79 131 / 16%);
}

.refresh-button:disabled {
  cursor: wait;
  opacity: 0.55;
}

.refresh-button:not(:disabled):hover {
  background: #c94175;
  box-shadow: var(--sprout-shadow-hover);
  transform: translateY(-1px);
}

.diagnostics-layout {
  display: grid;
  min-width: 0;
  grid-template-columns: 280px minmax(0, 1fr);
  gap: 18px;
  align-items: start;
}

.device-panel,
.device-overview,
.metric-card,
.detail-panel,
.retention-panel,
.state-panel {
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.device-panel {
  position: sticky;
  top: 20px;
  overflow: hidden;
  padding: 16px;
}

.panel-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  padding: 18px 20px;
  border-bottom: 1px solid #f7d9e2;
}

.device-panel .panel-heading {
  padding: 2px 2px 14px;
  border-bottom: 0;
}

.panel-heading h2,
.panel-heading p {
  margin: 0;
}

.panel-heading h2 {
  color: var(--sprout-text);
  font-size: 17px;
}

.panel-heading p {
  margin-top: 4px;
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.search-field {
  display: block;
  margin-bottom: 12px;
}

.search-field input {
  width: 100%;
  min-height: 42px;
  padding: 0 13px;
  border: 1px solid #f0bdcb;
  border-radius: 14px;
  background: #fff8fa;
  color: var(--sprout-text);
  font: inherit;
}

.search-field input:focus {
  border-color: #d94f83;
  background: #ffffff;
  outline: none;
  box-shadow: 0 0 0 3px rgb(247 168 191 / 24%);
}

.device-list {
  display: grid;
  max-height: min(560px, calc(100vh - 300px));
  gap: 8px;
  margin: 0;
  padding: 0;
  overflow: auto;
  list-style: none;
}

.device-list li {
  min-width: 0;
}

.device-option {
  display: grid;
  width: 100%;
  gap: 5px;
  padding: 12px 13px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #fffbfc;
  color: var(--sprout-text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.device-option:hover {
  border-color: #e9a5b8;
  background: #fff7fa;
  transform: translateX(2px);
}

.device-option.selected {
  border-color: #d94f83;
  background: #fff0f4;
  box-shadow: 0 8px 20px rgb(217 79 131 / 10%);
}

.device-option-main {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.device-option-main strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.device-option-meta {
  overflow: hidden;
  color: var(--sprout-text-muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.online-state {
  justify-self: start;
  padding: 3px 9px;
  border-radius: 999px;
  background: #fff0f2;
  color: #a12b4a;
  font-size: 11px;
  font-weight: 700;
}

.online-state.online {
  background: #e7f8ee;
  color: #1d6b3f;
}

.health-dot {
  width: 9px;
  height: 9px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: #d4b8c2;
  box-shadow: 0 0 0 3px rgb(212 184 194 / 18%);
}

.health-dot.healthy {
  background: #4ea675;
  box-shadow: 0 0 0 3px rgb(78 166 117 / 18%);
}

.health-dot.degraded {
  background: #d99a27;
  box-shadow: 0 0 0 3px rgb(217 154 39 / 18%);
}

.health-dot.faulted {
  background: #c73c63;
  box-shadow: 0 0 0 3px rgb(199 60 99 / 18%);
}

.device-empty,
.list-empty {
  padding: 18px;
  color: var(--sprout-text-muted);
  font-size: 13px;
  line-height: 1.6;
}

.diagnostics-content {
  display: grid;
  min-width: 0;
  gap: 16px;
}

.device-overview {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 20px 22px;
}

.overview-main {
  min-width: 0;
}

.overview-main h2 {
  margin: 0;
  color: var(--sprout-text);
  font-size: 22px;
}

.overview-meta {
  margin: 7px 0 0;
  color: var(--sprout-text-muted);
}

.health-summary {
  display: grid;
  min-width: 180px;
  gap: 3px;
  padding: 13px 15px;
  border: 1px solid #f0bdcb;
  border-radius: 17px;
  background: #fff7fa;
}

.health-summary-label {
  color: var(--sprout-text);
  font-weight: 700;
}

.health-summary small {
  color: var(--sprout-text-muted);
  line-height: 1.45;
}

.health-summary.healthy {
  border-color: #b8e1c8;
  background: #f0fbf4;
}

.health-summary.degraded {
  border-color: #efd39a;
  background: #fffaf0;
}

.health-summary.faulted {
  border-color: #edb8c6;
  background: #fff2f5;
}

.inline-warning {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin: 0;
  padding: 12px 15px;
  border: 1px solid #efd39a;
  border-radius: 15px;
  background: #fffaf0;
  color: #755600;
}

.inline-warning button {
  min-height: 34px;
  padding: 0 12px;
  border: 1px solid #d99a27;
  border-radius: 17px;
  background: #ffffff;
  color: #755600;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
}

.metric-card {
  padding: 17px 18px;
}

.metric-label,
.metric-detail {
  margin: 0;
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.metric-value {
  margin: 8px 0 6px;
  color: var(--sprout-text);
  font-size: 25px;
  font-weight: 700;
}

.metric-value.time-value {
  font-size: 18px;
  line-height: 1.35;
}

.health-text.healthy {
  color: #1d6b3f;
}

.health-text.degraded {
  color: #755600;
}

.health-text.faulted {
  color: #a12b4a;
}

.detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.detail-panel,
.retention-panel {
  min-width: 0;
  overflow: hidden;
}

.recovery-panel {
  grid-column: 1 / -1;
}

.panel-count {
  flex: 0 0 auto;
  padding: 4px 9px;
  border-radius: 999px;
  background: #fff2f5;
  color: #b23a68;
  font-size: 12px;
  font-weight: 700;
}

.event-list {
  display: grid;
  max-height: 390px;
  gap: 0;
  margin: 0;
  padding: 0;
  overflow: auto;
  list-style: none;
}

.event-list li {
  display: flex;
  align-items: flex-start;
  gap: 11px;
  padding: 13px 18px;
  border-bottom: 1px solid #f7d9e2;
}

.event-list li:last-child {
  border-bottom: 0;
}

.event-icon {
  display: grid;
  width: 28px;
  height: 28px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 10px;
  font-weight: 800;
}

.event-icon.boot {
  background: #eef7ff;
  color: #256798;
}

.event-icon.failure {
  background: #fff0f2;
  color: #a12b4a;
}

.event-icon.recovery {
  background: #e7f8ee;
  color: #1d6b3f;
}

.event-body {
  min-width: 0;
  flex: 1;
}

.event-title {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.event-title strong {
  color: var(--sprout-text);
  overflow-wrap: anywhere;
}

.event-title time {
  flex: 0 0 auto;
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.event-body p,
.retention-panel > p {
  margin: 5px 0 0;
  color: var(--sprout-text-muted);
  font-size: 13px;
  line-height: 1.55;
}

.retention-panel > p {
  margin: 0;
  padding: 16px 20px 0;
}

.retention-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin: 0;
  padding: 16px 20px 20px;
}

.retention-grid div {
  padding: 13px;
  border-radius: 15px;
  background: #fff8fa;
}

.retention-grid dt {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.retention-grid dd {
  margin: 5px 0 0;
  color: var(--sprout-text);
  font-weight: 700;
}

.state-panel {
  display: flex;
  min-height: 156px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 28px;
  color: var(--sprout-text-muted);
  text-align: center;
}

.state-panel.compact {
  min-height: 130px;
}

.state-panel.error-state {
  display: grid;
  min-height: 220px;
  justify-items: center;
  align-content: center;
}

.state-panel.error-state .state-icon {
  background: #fff0f2;
  color: #a12b4a;
}

.state-panel.error-state button {
  min-height: 38px;
  padding: 0 16px;
  border: 1px solid #e9a5b8;
  border-radius: 19px;
  background: #ffffff;
  color: #c94175;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

.state-panel.error-state button:hover {
  border-color: #d94f83;
  background: #fff7fa;
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
  color: #a12b4a;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 1180px) {
  .metric-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 900px) {
  .diagnostics-layout {
    grid-template-columns: 1fr;
  }

  .device-panel {
    position: static;
  }

  .device-list {
    display: flex;
    max-height: none;
    overflow-x: auto;
    padding-bottom: 3px;
  }

  .device-list li {
    min-width: 230px;
  }

  .detail-grid {
    grid-template-columns: 1fr;
  }

  .recovery-panel {
    grid-column: auto;
  }
}

@media (max-width: 720px) {
  .page-header,
  .device-overview {
    display: grid;
  }

  .refresh-button {
    width: 100%;
  }

  .health-summary {
    min-width: 0;
  }

  .retention-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 520px) {
  .metric-grid {
    grid-template-columns: 1fr;
  }

  .event-title {
    display: grid;
    gap: 3px;
  }
}
</style>
