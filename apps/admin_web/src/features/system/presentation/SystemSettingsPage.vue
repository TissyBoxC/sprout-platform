<script setup lang="ts">
import { computed, onMounted } from 'vue'

import type { AIGatewayChildPolicy } from '@/api/adminOperations'
import { useSystemSettingsStore } from '@/features/system/application/systemSettingsStore'

const store = useSystemSettingsStore()
const selectedDefaultModel = computed({
  get: () => store.settings.defaultModels[0] ?? '',
  set: (value: string) => {
    store.settings.defaultModels = value ? [value] : []
  },
})

const childPolicy = computed<AIGatewayChildPolicy | null>(
  () => store.childPolicy,
)
const childPolicyTiers = computed(() => childPolicy.value?.tiers ?? [])

onMounted(() => {
  void store.load()
})

const AGE_TIER_LABELS: Record<string, string> = {
  age_3_4: '3–4 岁',
  age_5_6: '5–6 岁',
  age_7_8: '7–8 岁',
}

function ageTierLabel(ageTier: string): string {
  return AGE_TIER_LABELS[ageTier] ?? ageTier
}

function ageBandLabel(ageBand: string): string {
  const trimmed = ageBand.trim()
  if (!trimmed) {
    return '未指定'
  }
  return /^\d+-\d+$/.test(trimmed) ? `${trimmed.replace('-', '–')} 岁` : trimmed
}

function contentRatingLabel(contentRating: string): string {
  switch (contentRating.trim().toUpperCase()) {
    case 'G':
      return '全年龄适用'
    case 'PG':
      return '需家长陪同'
    case '':
      return '未标注'
    default:
      return contentRating
  }
}

function tierBindingLabel(bound: boolean, ageBandMissing: boolean): string {
  if (bound) {
    return '已对齐'
  }
  return ageBandMissing ? '策略指向的年龄层缺失' : '尚未绑定年龄层'
}
</script>

<template>
  <section>
    <header class="page-header">
      <div>
        <p class="eyebrow">平台设置</p>
        <h1>系统设置</h1>
        <p class="page-description">
          这里的设置会作为新账号和新设备的默认规则。已经单独调整过的账号不会被覆盖。
        </p>
      </div>
      <button type="button" class="primary" :disabled="store.isSaving" @click="store.save">
        {{ store.isSaving ? '正在保存…' : '保存系统设置' }}
      </button>
    </header>

    <Transition name="toast">
      <p v-if="store.lastMessage" class="success-message">{{ store.lastMessage }}</p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.error" class="error-message">{{ store.error.message }}</p>
    </Transition>
    <Transition name="toast">
      <p v-if="store.integrationNotice" class="notice-message">
        {{ store.integrationNotice }}
      </p>
    </Transition>

    <div v-if="store.isLoading" class="state-panel">
      <span class="state-spinner" aria-hidden="true"></span>
      正在读取系统设置…
    </div>

    <form v-else class="settings-layout" @submit.prevent="store.save">
      <section class="settings-section">
        <div class="section-heading">
          <div>
            <p class="section-index">01</p>
            <h2>AI 服务</h2>
          </div>
          <p>决定家长首次开通 AI 服务时的默认额度与可用范围。</p>
        </div>
        <div class="field-grid">
          <label>
            <span>初始额度</span>
            <input
              v-model.number="store.settings.defaultBalanceUsd"
              type="number"
              min="0"
              step="0.01"
              readonly
            />
            <small class="field-hint">
              与{{ store.defaultBalanceSource || ' AI 服务统一设置' }}保持一致，保存时会同步写入。
            </small>
          </label>
          <label>
            <span>默认同时对话数量</span>
            <input
              v-model.number="store.settings.defaultConcurrencyLimit"
              type="number"
              min="1"
              step="1"
            />
          </label>
          <label class="span-two">
            <span>默认模型</span>
            <select v-model="selectedDefaultModel">
              <option value="">暂时使用 AI 服务推荐模型</option>
              <option
                v-for="model in store.modelOptions"
                :key="model.id"
                :value="model.id"
              >
                {{ model.id }}
                {{ model.latencyMs === null ? '' : `· ${model.latencyMs} ms` }}
              </option>
            </select>
            <small class="field-hint">
              读取可用模型后自动选择延迟最低的一项，也可以在列表中调整。
            </small>
          </label>
        </div>
      </section>

      <section class="settings-section">
        <div class="section-heading">
          <div>
            <p class="section-index">02</p>
            <h2>账号与注册</h2>
          </div>
          <p>控制家长如何注册和登录；关闭注册不会影响已有账号。</p>
        </div>
        <div class="toggle-list">
          <label class="toggle-row">
            <span>
              <strong>允许新家长注册</strong>
              <small>关闭后，家长仍可使用已有账号登录。</small>
            </span>
            <input v-model="store.settings.registrationEnabled" type="checkbox" />
          </label>
          <label class="toggle-row">
            <span>
              <strong>注册时验证手机号</strong>
              <small>开启后，注册需要完成一次手机号确认。</small>
            </span>
            <input v-model="store.settings.smsVerificationEnabled" type="checkbox" />
          </label>
          <label class="toggle-row">
            <span>
              <strong>允许使用邮箱登录</strong>
              <small>家长仍可先用手机号登录，再在应用内绑定邮箱。</small>
            </span>
            <input v-model="store.settings.emailLoginEnabled" type="checkbox" />
          </label>
        </div>
      </section>

      <section class="settings-section">
        <div class="section-heading">
          <div>
            <p class="section-index">03</p>
            <h2>内容安全</h2>
          </div>
          <p>所有选项默认采用保护儿童安全的严格策略。</p>
        </div>
        <div class="toggle-list">
          <label class="toggle-row">
            <span>
              <strong>新账号默认开启未成年人模式</strong>
              <small>家长可以在自己的账号中调整可用范围。</small>
            </span>
            <input
              v-model="store.settings.minorModeDefaultEnabled"
              type="checkbox"
            />
          </label>
          <label class="toggle-row">
            <span>
              <strong>回复发送前进行内容审核</strong>
              <small>发现不适合儿童的内容时，不会发送到设备。</small>
            </span>
            <input
              v-model="store.settings.outputModerationEnabled"
              type="checkbox"
            />
          </label>
          <label class="toggle-row">
            <span>
              <strong>危机内容提示监护人</strong>
              <small>涉及自伤或受伤害风险时，提供求助提示并通知监护人。</small>
            </span>
            <input
              v-model="store.settings.crisisInterventionEnabled"
              type="checkbox"
            />
          </label>
        </div>

        <div class="gateway-policy">
          <div class="gateway-policy-heading">
            <div>
              <h3>儿童安全策略（网关生效）</h3>
              <p>读取 AI 网关当前实际执行的年龄分级策略，确认每个年龄段都被覆盖。</p>
            </div>
            <button
              type="button"
              class="text-button"
              :disabled="store.isChildPolicyLoading"
              @click="store.reloadChildPolicy"
            >
              {{ store.isChildPolicyLoading ? '正在读取…' : '重新读取' }}
            </button>
          </div>

          <div v-if="store.isChildPolicyLoading" class="inline-state">
            正在读取网关策略…
          </div>
          <div v-else-if="store.childPolicyError" class="inline-state error">
            {{ store.childPolicyError.message }}
            <button
              type="button"
              class="text-button"
              @click="store.reloadChildPolicy"
            >
              重新读取
            </button>
          </div>
          <div v-else-if="!childPolicy || !childPolicy.enabled" class="empty-section">
            <p>网关尚未启用儿童安全策略，目前不按年龄层限制可用模型与回复长度。</p>
          </div>
          <template v-else>
            <ul class="policy-summary">
              <li>
                <span>整体对齐</span>
                <strong :class="childPolicy.aligned ? 'ok' : 'warn'">
                  {{ childPolicy.aligned ? '全部年龄段已对齐' : '存在未覆盖的年龄段' }}
                </strong>
              </li>
              <li>
                <span>缺少策略标识时</span>
                <strong>
                  {{
                    childPolicy.requirePolicyVersion
                      ? '拒绝请求'
                      : `回退到 ${ageBandLabel(childPolicy.defaultAgeBand)}`
                  }}
                </strong>
              </li>
              <li>
                <span>安全边界注入</span>
                <strong>{{ childPolicy.injectSafetyBoundary ? '已开启' : '未开启' }}</strong>
              </li>
            </ul>

            <ul v-if="childPolicyTiers.length" class="policy-tier-list">
              <li v-for="tier in childPolicyTiers" :key="tier.ageTier">
                <div class="policy-tier-header">
                  <strong>{{ ageTierLabel(tier.ageTier) }}</strong>
                  <span
                    class="policy-badge"
                    :class="tier.bound ? 'ok' : 'warn'"
                  >
                    {{ tierBindingLabel(tier.bound, tier.ageBandMissing) }}
                  </span>
                </div>
                <p v-if="tier.bound" class="policy-tier-meta">
                  对应 {{ ageBandLabel(tier.ageBand) }} ·
                  {{ contentRatingLabel(tier.contentRating) }} ·
                  单次回复长度上限 {{ tier.maxOutputTokens }} ·
                  可用模型 {{ tier.allowedModels.length }} 个
                </p>
                <p v-else class="policy-tier-meta">
                  网关无法为这个年龄段套用安全策略，需要先在网关配置绑定。
                </p>
              </li>
            </ul>
            <p v-else class="field-hint">网关还没有返回任何年龄段配置。</p>
          </template>
        </div>
      </section>

      <section class="settings-section">
        <div class="section-heading">
          <div>
            <p class="section-index">04</p>
            <h2>更新与发布</h2>
          </div>
          <p>统一控制应用和设备的更新渠道，减少不同版本之间的兼容问题。</p>
        </div>
        <div class="field-grid">
          <label>
            <span>默认更新渠道</span>
            <select v-model="store.settings.releaseChannel">
              <option value="stable">稳定渠道</option>
              <option value="beta">测试渠道</option>
              <option value="canary">内测渠道</option>
            </select>
          </label>
          <label>
            <span>最低可登录版本</span>
            <input
              v-model.trim="store.settings.minimumClientVersion"
              type="text"
              placeholder="例如 0.8.0"
            />
          </label>
          <label>
            <span>必须升级的版本线</span>
            <input
              v-model.trim="store.settings.mandatoryUpdateThreshold"
              type="text"
              placeholder="低于该版本时必须升级"
            />
          </label>
          <div class="span-two policy-note">
            <strong>更新包由服务器下发</strong>
            <p>
              资源更新和完整安装包都使用登记版本中的安全下载地址与校验值。
              客户端会先校验文件，再执行资源替换或应用安装。
            </p>
          </div>
        </div>
      </section>

      <section class="settings-section">
        <div class="section-heading">
          <div>
            <p class="section-index">05</p>
            <h2>隐私与数据</h2>
          </div>
          <p>音视频上传默认关闭。需要上传时，应先获得监护人明确同意。</p>
        </div>
        <div class="field-grid">
          <label>
            <span>语音保留天数</span>
            <input
              v-model.number="store.settings.audioRetentionDays"
              type="number"
              min="0"
              step="1"
            />
          </label>
          <label>
            <span>图像保留天数</span>
            <input
              v-model.number="store.settings.imageRetentionDays"
              type="number"
              min="0"
              step="1"
            />
          </label>
          <label>
            <span>对话记录保留天数</span>
            <input
              v-model.number="store.settings.conversationRetentionDays"
              type="number"
              min="0"
              step="1"
            />
          </label>
        </div>
        <div class="toggle-list privacy-toggles">
          <div class="toggle-row readonly-row">
            <span>
              <strong>语音原文件上传</strong>
              <small>安全策略固定关闭。需要上传时必须先完成监护人逐项授权。</small>
            </span>
            <span class="readonly-value">固定关闭</span>
          </div>
          <div class="toggle-row readonly-row">
            <span>
              <strong>图像原文件上传</strong>
              <small>安全策略固定关闭。需要上传时必须先完成监护人逐项授权。</small>
            </span>
            <span class="readonly-value">固定关闭</span>
          </div>
        </div>
      </section>

      <div class="form-footer">
        <p>保存后，新设置会立即用于新的账号、设备和更新任务。</p>
        <button type="submit" class="primary" :disabled="store.isSaving">
          {{ store.isSaving ? '正在保存…' : '保存系统设置' }}
        </button>
      </div>
    </form>
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
  max-width: 760px;
  margin: 10px 0 0;
  color: var(--sprout-text-muted);
}

button {
  min-height: 40px;
  padding: 0 18px;
  border: 1px solid #e9a5b8;
  border-radius: var(--sprout-radius-control);
  background: #ffffff;
  color: #c94175;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

button.primary {
  border-color: #d94f83;
  background: #d94f83;
  color: #ffffff;
}

button.primary:not(:disabled):hover {
  background: #c94175;
  box-shadow: 0 10px 22px rgb(217 79 131 / 18%);
}

button:disabled {
  cursor: wait;
  opacity: 0.55;
}

.state-panel {
  display: flex;
  min-height: 160px;
  align-items: center;
  justify-content: center;
  gap: 12px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  color: var(--sprout-text-muted);
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.state-spinner {
  width: 18px;
  height: 18px;
  border: 2px solid #f0bdcb;
  border-top-color: #d94f83;
  border-radius: 50%;
  animation: spin 700ms linear infinite;
}

.settings-layout {
  display: grid;
  gap: 18px;
}

.settings-section {
  padding: 24px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 20px;
  padding-bottom: 16px;
  border-bottom: 1px solid #f7d9e2;
}

.section-heading h2,
.section-heading p {
  margin: 0;
}

.section-heading h2 {
  color: var(--sprout-text);
  font-size: 19px;
}

.section-heading > p {
  max-width: 520px;
  color: var(--sprout-text-muted);
  font-size: 14px;
  line-height: 1.6;
}

.section-index {
  margin-bottom: 5px !important;
  color: #d94f83;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.field-grid label {
  display: grid;
  gap: 7px;
  color: var(--sprout-text);
  font-weight: 600;
}

.field-grid .span-two {
  grid-column: 1 / -1;
}

.policy-note {
  padding: 14px 16px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #fffbfc;
}

.policy-note strong {
  color: var(--sprout-text);
}

.policy-note p {
  margin: 5px 0 0;
  color: var(--sprout-text-muted);
  font-size: 13px;
  line-height: 1.6;
}

.field-hint {
  color: var(--sprout-text-muted);
  font-size: 13px;
  font-weight: 400;
  line-height: 1.55;
}

.notice-message {
  margin: 0 0 14px;
  padding: 12px 14px;
  border: 1px solid #f0d39b;
  border-radius: 14px;
  background: #fffaf0;
  color: #755600;
}

input[readonly] {
  cursor: not-allowed;
  background: #f7f1f3;
  color: #6b4f5a;
}

input,
select,
textarea {
  width: 100%;
  border: 1px solid #f0bdcb;
  border-radius: 15px;
  background: #fff8fa;
  color: var(--sprout-text);
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
  min-height: 94px;
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

.toggle-list {
  display: grid;
  gap: 10px;
}

.toggle-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 22px;
  padding: 14px 16px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #fffbfc;
  cursor: pointer;
  transition:
    border-color var(--sprout-duration-fast) ease,
    background-color var(--sprout-duration-fast) ease,
    transform var(--sprout-duration-fast) var(--sprout-ease-out);
}

.toggle-row:hover {
  border-color: #e9a5b8;
  background: #fff7fa;
  transform: translateY(-1px);
}

.toggle-row span {
  display: grid;
  gap: 3px;
}

.toggle-row strong {
  color: var(--sprout-text);
}

.toggle-row small {
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.toggle-row input {
  width: 42px;
  min-width: 42px;
  min-height: 24px;
  height: 24px;
  accent-color: #d94f83;
}

.readonly-row {
  cursor: default;
}

.readonly-row:hover {
  transform: none;
}

.readonly-value {
  display: inline-flex;
  min-height: 28px;
  align-items: center;
  padding: 0 11px;
  border-radius: 999px;
  background: #fff0f2;
  color: #a12b4a;
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
}

.privacy-toggles {
  margin-top: 16px;
}

.gateway-policy {
  margin-top: 18px;
  padding: 18px;
  border: 1px solid #f7d9e2;
  border-radius: 16px;
  background: #fffbfc;
}

.gateway-policy-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  margin-bottom: 14px;
}

.gateway-policy-heading h3,
.gateway-policy-heading p {
  margin: 0;
}

.gateway-policy-heading h3 {
  color: var(--sprout-text);
  font-size: 16px;
}

.gateway-policy-heading p {
  margin-top: 4px;
  color: var(--sprout-text-muted);
  font-size: 13px;
  line-height: 1.55;
}

.text-button {
  min-height: 30px;
  padding: 0 12px;
  border: 1px solid #e9a5b8;
  border-radius: 999px;
  background: #ffffff;
  color: #c94175;
  font: inherit;
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
  cursor: pointer;
}

.inline-state,
.empty-section {
  padding: 14px 16px;
  border: 1px solid #f7d9e2;
  border-radius: 14px;
  background: #ffffff;
  color: var(--sprout-text-muted);
  font-size: 13px;
}

.inline-state.error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-color: #f3c2c0;
  color: #b3261e;
}

.empty-section p {
  margin: 0;
  line-height: 1.6;
}

.policy-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin: 0 0 14px;
  padding: 0;
  list-style: none;
}

.policy-summary li {
  display: grid;
  gap: 4px;
  padding: 12px 14px;
  border: 1px solid #f7d9e2;
  border-radius: 14px;
  background: #ffffff;
}

.policy-summary span {
  color: var(--sprout-text-muted);
  font-size: 12px;
}

.policy-summary strong {
  color: var(--sprout-text);
  font-size: 14px;
}

.policy-summary strong.ok,
.policy-badge.ok {
  color: #1d6b3f;
}

.policy-summary strong.warn,
.policy-badge.warn {
  color: #a12b4a;
}

.policy-tier-list {
  display: grid;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.policy-tier-list li {
  padding: 14px 16px;
  border: 1px solid #f7d9e2;
  border-radius: 14px;
  background: #ffffff;
}

.policy-tier-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.policy-tier-header strong {
  color: var(--sprout-text);
  font-size: 15px;
}

.policy-badge {
  display: inline-flex;
  min-height: 24px;
  align-items: center;
  padding: 0 10px;
  border-radius: 999px;
  background: #fff0f2;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.policy-badge.ok {
  background: #eaf6ef;
}

.policy-tier-meta {
  margin: 8px 0 0;
  color: var(--sprout-text-muted);
  font-size: 13px;
  line-height: 1.6;
}

.form-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 18px 22px;
  border: 1px solid var(--sprout-outline);
  border-radius: var(--sprout-radius-card);
  background: #ffffff;
  box-shadow: 0 10px 28px rgb(194 91 128 / 6%);
}

.form-footer p {
  margin: 0;
  color: var(--sprout-text-muted);
}

.error-message {
  margin: 0 0 14px;
  color: #b3261e;
}

.success-message {
  margin: 0 0 14px;
  color: #1d6b3f;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (max-width: 760px) {
  .page-header,
  .gateway-policy-heading,
  .section-heading,
  .form-footer {
    display: grid;
  }

  .field-grid {
    grid-template-columns: 1fr;
  }

  .field-grid .span-two {
    grid-column: auto;
  }

  .policy-summary {
    grid-template-columns: 1fr;
  }
}
</style>
