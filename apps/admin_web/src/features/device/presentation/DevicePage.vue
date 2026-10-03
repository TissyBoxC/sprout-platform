<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import {
  useDeviceStore,
  type AdminDevice,
  type DeviceCommandType,
} from '@/features/device/application/deviceStore'

const store = useDeviceStore()
const selectedDevice = ref<AdminDevice | null>(null)
const activeCommand = ref<DeviceCommandType>('refresh_configuration')

const onlineCount = computed(
  () => store.devices.filter((device) => device.runtime?.isOnline).length,
)

const canUnbindSelectedDevice = computed(() => Boolean(selectedDevice.value?.parentAccountId))

onMounted(() => {
  void store.load()
})

async function openDevice(device: AdminDevice): Promise<void> {
  selectedDevice.value = device
  await store.loadCommands(device.deviceId)
}

async function sendCommand(): Promise<void> {
  if (selectedDevice.value === null) {
    return
  }
  await store.sendCommand(selectedDevice.value.deviceId, activeCommand.value)
}

async function unbindSelectedDevice(): Promise<void> {
  const device = selectedDevice.value
  if (device === null || !device.parentAccountId) {
    return
  }

  const confirmed = window.confirm(
    `确定解除“${device.deviceName || device.deviceId}”的绑定吗？解除后设备将失去与当前家长账号的关联，并需要重新绑定。`,
  )
  if (!confirmed) {
    return
  }

  const succeeded = await store.unbind(device.parentAccountId, device.deviceId)
  if (succeeded) {
    selectedDevice.value = null
  }
}

function lifecycleStatusLabel(value: string): string {
  return (
    {
      active: '已绑定',
      inactive: '已解绑',
      pending: '待绑定',
      revoked: '已解除',
    }[value] ?? '未知状态'
  )
}

function capabilityLabel(value: string): string {
  return (
    {
      audio_input: '麦克风',
      audio_output: '扬声器',
      wifi: '无线网络',
      camera: '摄像头',
      display: '屏幕',
      touch: '触摸',
      led: '指示灯',
      battery: '电池',
      cellular_4g: '移动网络',
      motion: '动作感应',
      bluetooth_audio: '蓝牙音频',
      video_call: '视频通话',
      location: '定位',
      geofence: '电子围栏',
      sos: '紧急求助',
      multi_device: '多设备协同',
    }[value] ?? value
  )
}

function networkQualityLabel(value: string): string {
  return (
    {
      excellent: '很好',
      good: '良好',
      fair: '一般',
      poor: '较差',
      unknown: '未知',
    }[value] ?? '未知'
  )
}

function timeSyncLabel(value: string): string {
  return (
    {
      synchronized: '已校准',
      synchronizing: '校准中',
      unsynchronized: '待校准',
    }[value] ?? '待校准'
  )
}

function commandLabel(value: DeviceCommandType): string {
  return (
    {
      refresh_configuration: '刷新配置',
      reconnect_network: '重连网络',
      resync_time: '重新校准时间',
    }[value] ?? value
  )
}

function commandStatusLabel(value: string): string {
  return (
    {
      pending: '等待设备连接',
      delivered: '设备已收到',
      acknowledged: '已完成',
      failed: '未完成',
    }[value] ?? value
  )
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
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">设备运营</p>
        <h1>设备管理</h1>
        <p class="page-description">
          查看每台设备的联网状态和最后连接时间，并在需要时发送维护操作。
        </p>
      </div>
      <button type="button" :disabled="store.isLoading" @click="store.load">重新加载</button>
    </header>

    <div class="summary">
      <span
        ><strong>{{ store.devices.length }}</strong> 台已绑定设备</span
      >
      <span
        ><strong>{{ onlineCount }}</strong> 台在线</span
      >
    </div>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="success-message">
        {{ store.lastMessage }}
      </p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.error" class="error-message">{{ store.error.message }}</p>
    </Transition>

    <Transition name="page" mode="out-in">
      <div v-if="store.isLoading" key="loading" class="state-panel">
        <span class="state-spinner" aria-hidden="true"></span>
        正在读取设备…
      </div>
      <div v-else-if="store.devices.length === 0" key="empty" class="state-panel">
        <span class="state-icon" aria-hidden="true">☆</span>
        还没有绑定设备。家长完成设备连接后，设备会显示在这里。
      </div>
      <div v-else key="table" class="table-shell">
        <table>
          <thead>
            <tr>
              <th>设备</th>
              <th>状态</th>
              <th>生命周期</th>
              <th>网络</th>
              <th>时间</th>
              <th>最后连接</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="device in store.devices" :key="device.deviceId">
              <td>
                <span class="device-name">{{ device.deviceName || '未命名设备' }}</span>
                <span class="device-meta">
                  {{ device.hardwareModel || '初芽' }} ·
                  {{ device.firmwareVersion || '版本未知' }}
                </span>
              </td>
              <td>
                <span :class="['status', device.runtime?.isOnline ? 'online' : 'offline']">
                  {{ device.runtime?.isOnline ? '在线' : '离线' }}
                </span>
              </td>
              <td>
                <span class="lifecycle-status">
                  {{ lifecycleStatusLabel(device.lifecycleStatus) }}
                </span>
              </td>
              <td>
                <span v-if="device.runtime">
                  {{ networkQualityLabel(device.runtime.networkQuality) }}
                  <small v-if="device.runtime.rssiDbm < 0">
                    {{ device.runtime.rssiDbm }} dBm
                  </small>
                </span>
                <span v-else>尚未上报</span>
              </td>
              <td>
                <span v-if="device.runtime">
                  {{ timeSyncLabel(device.runtime.timeSyncState) }}
                </span>
                <span v-else>尚未上报</span>
              </td>
              <td>{{ formatTime(device.runtime?.receivedAt ?? '') }}</td>
              <td>
                <button type="button" @click="openDevice(device)">查看与维护</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="selectedDevice" class="dialog-backdrop" @click.self="selectedDevice = null">
        <section class="dialog">
          <h2>{{ selectedDevice.deviceName || '未命名设备' }}</h2>
          <p class="device-id">{{ selectedDevice.deviceId }}</p>
          <dl class="detail-grid">
            <div>
              <dt>绑定状态</dt>
              <dd>{{ lifecycleStatusLabel(selectedDevice.lifecycleStatus) }}</dd>
            </div>
            <div>
              <dt>最近上报</dt>
              <dd>{{ formatTime(selectedDevice.runtime?.receivedAt ?? '') }}</dd>
            </div>
          </dl>
          <div class="capability-panel">
            <h3>设备能力</h3>
            <div v-if="selectedDevice.capabilities.length === 0" class="command-empty">
              这台设备还没有上报可用能力。
            </div>
            <div v-else class="capability-list">
              <span v-for="capability in selectedDevice.capabilities" :key="capability">
                {{ capabilityLabel(capability) }}
              </span>
            </div>
          </div>
          <label>
            <span>维护操作</span>
            <select v-model="activeCommand">
              <option value="refresh_configuration">刷新配置</option>
              <option value="reconnect_network">重连网络</option>
              <option value="resync_time">重新校准时间</option>
            </select>
          </label>
          <button type="button" :disabled="store.isSubmitting" @click="sendCommand">
            {{ store.isSubmitting ? '正在发送…' : '发送操作' }}
          </button>

          <h3>最近操作</h3>
          <div
            v-if="(store.commands[selectedDevice.deviceId] ?? []).length === 0"
            class="command-empty"
          >
            还没有维护记录。
          </div>
          <ul v-else class="command-list">
            <li v-for="command in store.commands[selectedDevice.deviceId]" :key="command.commandId">
              <span>{{ commandLabel(command.commandType) }}</span>
              <small>
                {{ commandStatusLabel(command.status) }} ·
                {{ formatTime(command.createdAt) }}
              </small>
            </li>
          </ul>
          <section v-if="canUnbindSelectedDevice" class="danger-zone">
            <h3>解除绑定</h3>
            <p>解除后设备会失去与当前家长账号的关联，需要重新绑定才能继续使用。</p>
            <button
              type="button"
              class="danger"
              :disabled="store.isSubmitting"
              @click="unbindSelectedDevice"
            >
              {{ store.isSubmitting ? '正在解除…' : '解除绑定' }}
            </button>
          </section>
          <div class="dialog-actions">
            <button type="button" class="secondary" @click="selectedDevice = null">关闭</button>
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
  margin-bottom: 20px;
}

.page-header button {
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

.page-header button:disabled {
  cursor: wait;
  opacity: 0.55;
}

.page-header button:not(:disabled):hover {
  border-color: #d94f83;
  background: #fff7fa;
  box-shadow: 0 8px 18px rgb(217 79 131 / 10%);
}

.summary {
  display: flex;
  gap: 18px;
  margin-bottom: 16px;
  color: #6b4f5a;
}

.summary strong {
  color: #4a2e3b;
}

.table-shell,
.state-panel {
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
  border-bottom: 1px solid #f7d9e2;
  text-align: left;
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

.device-name,
.device-meta,
td small {
  display: block;
}

.device-meta,
td small {
  margin-top: 2px;
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

.status.online {
  background: #e7f8ee;
  color: #1d6b3f;
}

.status.offline {
  background: #fff0f2;
  color: #a12b4a;
}

.lifecycle-status {
  color: #4a2e3b;
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
  color: #b3261e;
}

.success-message {
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
  width: min(100%, 480px);
  max-height: min(720px, calc(100vh - 40px));
  gap: 16px;
  overflow: auto;
  padding: 28px;
  border: 1px solid #f0bdcb;
  border-radius: 26px;
  background: #ffffff;
  box-shadow: 0 28px 72px rgb(74 46 59 / 22%);
}

.dialog h2,
.dialog h3 {
  margin: 0;
  color: #4a2e3b;
}

.dialog h3 {
  margin-top: 6px;
  font-size: 15px;
}

.device-id {
  margin: 0;
  overflow-wrap: anywhere;
  color: #6b4f5a;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
}

.detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin: 0;
}

.detail-grid div {
  padding: 12px 14px;
  border-radius: 14px;
  background: #fff8fa;
}

.detail-grid dt {
  color: #6b4f5a;
  font-size: 12px;
}

.detail-grid dd {
  margin: 4px 0 0;
  color: #4a2e3b;
  font-weight: 700;
}

.capability-panel {
  display: grid;
  gap: 10px;
}

.capability-panel h3 {
  margin: 0;
}

.capability-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.capability-list span {
  padding: 6px 10px;
  border: 1px solid #f2bfcb;
  border-radius: 999px;
  background: #fff8fa;
  color: #6b4f5a;
  font-size: 12px;
}

.dialog label {
  display: grid;
  gap: 7px;
  color: #4a2e3b;
  font-weight: 600;
}

.dialog select {
  min-height: 44px;
  padding: 0 14px;
  border: 1px solid #f0bdcb;
  border-radius: 14px;
  background: #fff8fa;
  color: #4a2e3b;
  font: inherit;
}

.dialog > button[type='button'] {
  min-height: 44px;
  border: 1px solid #d94f83;
  border-radius: 16px;
  background: #d94f83;
  color: #ffffff;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

.dialog > button[type='button']:disabled {
  cursor: wait;
  opacity: 0.55;
}

.command-empty {
  color: #6b4f5a;
}

.command-list {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.command-list li {
  display: grid;
  gap: 2px;
  padding: 10px 12px;
  border-radius: 14px;
  background: #fff8fa;
}

.command-list small {
  color: #6b4f5a;
}

.danger-zone {
  display: grid;
  gap: 10px;
  padding: 16px;
  border: 1px solid #f2bfcb;
  border-radius: 18px;
  background: #fff7f9;
}

.danger-zone h3,
.danger-zone p {
  margin: 0;
}

.danger-zone p {
  color: #6b4f5a;
  font-size: 13px;
}

.danger-zone .danger {
  min-height: 42px;
  border: 1px solid #c73c63;
  border-radius: 14px;
  background: #c73c63;
  color: #ffffff;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

.danger-zone .danger:disabled {
  cursor: wait;
  opacity: 0.55;
}

.dialog-actions {
  display: flex;
  justify-content: flex-end;
  margin-top: 4px;
}

.dialog-actions .secondary {
  min-height: 38px;
  padding: 0 16px;
  border: 1px solid #f0bdcb;
  border-radius: 19px;
  background: #ffffff;
  color: #c94175;
  font: inherit;
  cursor: pointer;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 760px) {
  .page-header {
    display: grid;
  }

  .table-shell {
    overflow-x: auto;
  }

  table {
    min-width: 840px;
  }
}

.page-header {
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
  margin: 12px 0 0;
  color: var(--sprout-text-muted);
}
</style>
