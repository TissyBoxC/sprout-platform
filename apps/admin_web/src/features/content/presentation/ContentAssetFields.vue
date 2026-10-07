<script setup lang="ts">
import type { DownloadFile } from '@/api/adminDownloadFiles'

export interface ContentAssetForm {
  assetSource: 'manual' | 'download'
  assetKey: string
  sha256: string
  sizeBytes: number
}

const props = defineProps<{
  form: ContentAssetForm
  errors: Record<string, string>
  downloadFiles: DownloadFile[]
  isLoadingDownloadFiles: boolean
  downloadFilesError: string
}>()

const emit = defineEmits<{
  toggleSource: [source: 'manual' | 'download']
  loadDownloadFiles: []
  selectDownloadFile: [file: DownloadFile]
}>()

function useSource(source: 'manual' | 'download'): void {
  emit('toggleSource', source)
  if (source === 'download' && props.downloadFiles.length === 0) {
    emit('loadDownloadFiles')
  }
}

function onDownloadFileChange(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLSelectElement) || target.value === '') {
    return
  }
  const file = props.downloadFiles.find((item) => item.relativePath === target.value)
  if (file !== undefined) {
    emit('selectDownloadFile', file)
  }
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '未填写'
  }
  const units = ['B', 'KB', 'MB', 'GB']
  const exponent = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  const scaled = value / 1024 ** exponent
  return `${scaled.toFixed(exponent === 0 || scaled >= 100 ? 0 : 1)} ${units[exponent]}`
}
</script>

<template>
  <div class="asset-fields">
    <div class="asset-heading">
      <div>
        <span class="field-label">内容文件</span>
        <p class="field-hint">
          从下载文件中选择已经上传的文件，或手动填写文件位置和校验值。
        </p>
      </div>
      <div class="source-toggle" role="group" aria-label="内容文件录入方式">
        <button
          type="button"
          :class="{ active: form.assetSource === 'manual' }"
          @click="useSource('manual')"
        >
          手动填写
        </button>
        <button
          type="button"
          :class="{ active: form.assetSource === 'download' }"
          @click="useSource('download')"
        >
          选择下载文件
        </button>
      </div>
    </div>

    <div v-if="form.assetSource === 'download'" class="download-picker">
      <div v-if="isLoadingDownloadFiles" class="inline-state">正在读取下载文件…</div>
      <div v-else-if="downloadFilesError" class="inline-state error">
        {{ downloadFilesError }}
        <button type="button" class="text-button" @click="emit('loadDownloadFiles')">
          重新加载下载文件
        </button>
      </div>
      <div v-else-if="downloadFiles.length === 0" class="inline-state">
        下载服务器上还没有文件。可以先在下载文件页上传，再回到这里选择。
      </div>
      <label v-else>
        <span>选择已经上传的文件</span>
        <select :value="form.assetKey" @change="onDownloadFileChange">
          <option value="">请选择文件</option>
          <option
            v-for="file in downloadFiles"
            :key="file.relativePath"
            :value="file.relativePath"
          >
            {{ file.name }} · {{ formatBytes(file.sizeBytes) }} · {{ file.relativePath }}
          </option>
        </select>
      </label>
    </div>

    <div class="field-grid">
      <label class="span-two">
        <span>文件位置</span>
        <input
          v-model.trim="form.assetKey"
          type="text"
          placeholder="例如 1.0.0/stable/all/resource/story-001.zip"
        />
        <small v-if="errors.assetKey" class="field-error">{{ errors.assetKey }}</small>
      </label>
      <label>
        <span>校验值</span>
        <input
          v-model.trim="form.sha256"
          type="text"
          spellcheck="false"
          placeholder="64 位小写十六进制"
        />
        <small v-if="errors.sha256" class="field-error">{{ errors.sha256 }}</small>
      </label>
      <label>
        <span>文件大小（字节）</span>
        <input v-model.number="form.sizeBytes" type="number" min="1" step="1" />
        <small v-if="errors.sizeBytes" class="field-error">{{ errors.sizeBytes }}</small>
      </label>
    </div>
  </div>
</template>

<style scoped>
.asset-fields {
  display: grid;
  gap: 15px;
  padding: 16px;
  border: 1px solid var(--sprout-outline-soft);
  border-radius: 18px;
  background: #fffbfc;
}

.asset-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
}

.field-label {
  color: var(--sprout-text);
  font-weight: 700;
}

.field-hint {
  margin: 5px 0 0;
  color: var(--sprout-text-muted);
  font-size: 13px;
  line-height: 1.5;
}

.source-toggle {
  display: inline-flex;
  flex: 0 0 auto;
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
  font: inherit;
  font-weight: 700;
  cursor: pointer;
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

.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.field-grid .span-two {
  grid-column: 1 / -1;
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
  font-weight: 400;
}

select:focus,
input:focus {
  border-color: #d94f83;
  background: #ffffff;
  box-shadow: 0 0 0 3px rgb(217 79 131 / 12%);
  outline: none;
}

.field-error {
  color: #b3261e;
  font-size: 12px;
  font-weight: 600;
}

.inline-state {
  padding: 14px 16px;
  border-radius: 15px;
  background: #fff7fa;
  color: var(--sprout-text-muted);
  font-weight: 400;
  line-height: 1.6;
}

.inline-state.error {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  background: #fff0f2;
  color: #a12b4a;
}

.text-button {
  min-height: 32px;
  padding: 0 12px;
  border: 1px solid #e9a5b8;
  border-radius: 11px;
  background: #ffffff;
  color: #b23a68;
  font: inherit;
  font-weight: 700;
  font-size: 12px;
  cursor: pointer;
}

@media (max-width: 520px) {
  .asset-heading {
    flex-direction: column;
  }

  .field-grid {
    grid-template-columns: 1fr;
  }

  .field-grid .span-two {
    grid-column: auto;
  }
}
</style>
