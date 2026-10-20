import axios from 'axios'

import type { APIResponseEnvelope } from '../../../../packages/contracts/generated/typescript/envelope'

/// Categories let pages choose a safe next action without reading raw errors.
export type ApiErrorKind =
  | 'unauthenticated'
  | 'insufficient_permission'
  | 'not_found'
  | 'validation'
  | 'rate_limited'
  | 'network'
  | 'service_unavailable'
  | 'unexpected'

/// Mapped error safe for logs and UI state.
export interface ApiError {
  kind: ApiErrorKind
  message: string
  retryable: boolean
}

/// Maps transport and contract errors to stable user-facing categories.
export function mapApiError(error: unknown): ApiError {
  if (axios.isAxiosError(error)) {
    const status = error.response?.status
    const envelope = error.response?.data as APIResponseEnvelope | undefined
    const code = envelope?.error?.code

    if (code === 'invalid_mfa_code') {
      return { kind: 'validation', message: '验证码不正确，请重新输入', retryable: false }
    }
    if (code === 'mfa_challenge_expired') {
      return { kind: 'unauthenticated', message: '验证已过期，请重新登录', retryable: false }
    }
    if (code === 'invalid_credentials') {
      return { kind: 'validation', message: '邮箱或密码不正确', retryable: false }
    }
    if (code === 'mfa_not_configured') {
      return {
        kind: 'service_unavailable',
        message: '管理员验证尚未设置，请联系维护人员',
        retryable: false,
      }
    }
    if (code === 'invalid_request') {
      return { kind: 'validation', message: '请检查填写的内容', retryable: false }
    }
    if (code === 'service_not_upgradable') {
      return {
        kind: 'validation',
        message: '这个服务当前已是最新版本，无需升级',
        retryable: false,
      }
    }
    if (code === 'already_current') {
      return {
        kind: 'validation',
        message: '所有服务都已是最新版本，无需升级',
        retryable: false,
      }
    }
    if (code === 'upgrade_in_progress') {
      return {
        kind: 'validation',
        message: '已有升级任务正在进行，请等待完成',
        retryable: false,
      }
    }
    if (code === 'file_immutable' || code === 'published_file_immutable') {
      return {
        kind: 'validation',
        message: '这个文件已经用于发布，不能再替换或删除。请发布新版本并使用新的文件名。',
        retryable: false,
      }
    }
    if (code === 'file_exists') {
      return {
        kind: 'validation',
        message: '文件已经存在，请确认替换后再上传。',
        retryable: false,
      }
    }
    if (status === 401) {
      return { kind: 'unauthenticated', message: '登录已过期，请重新登录', retryable: false }
    }
    if (status === 403) {
      return { kind: 'insufficient_permission', message: '你没有权限访问此页面', retryable: false }
    }
    if (status === 404) {
      return { kind: 'not_found', message: '没有找到这条内容', retryable: false }
    }
    if (status === 422) {
      return { kind: 'validation', message: '请检查填写的内容', retryable: false }
    }
    if (status === 429) {
      return { kind: 'rate_limited', message: '操作太频繁，请稍后重试', retryable: true }
    }
    if (!error.response) {
      return { kind: 'network', message: '网络连接不稳定，请检查后重试', retryable: true }
    }
    if (status !== undefined && status >= 500) {
      return { kind: 'service_unavailable', message: '服务暂时不可用，请稍后重试', retryable: true }
    }
  }

  return { kind: 'unexpected', message: '操作没有完成，请稍后重试', retryable: true }
}
