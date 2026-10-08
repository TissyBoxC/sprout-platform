import 'package:dio/dio.dart';

import '../contracts/generated/envelope.dart';

import 'app_exception.dart';

/// Maps transport and contract failures to stable user-facing errors.
AppException mapApiError(Object error) {
  if (error is DioException) {
    final status = error.response?.statusCode;
    final errorCode = _readErrorCode(error.response?.data);

    if (errorCode == 'invalid_credentials') {
      return AppException(
        kind: AppErrorKind.unauthenticated,
        message: '手机号、邮箱或密码不正确',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'invalid_verification_code') {
      return AppException(
        kind: AppErrorKind.validation,
        message: '验证码不正确，请重新输入',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'device_session_expired') {
      return AppException(
        kind: AppErrorKind.deviceSessionExpired,
        message: '设备登录已过期，请重新连接',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'phone_exists') {
      return AppException(
        kind: AppErrorKind.validation,
        message: '这个手机号已经注册过',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'email_exists') {
      return AppException(
        kind: AppErrorKind.validation,
        message: '这个邮箱已经注册过',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'invalid_guardian_name') {
      return AppException(
        kind: AppErrorKind.validation,
        message: '家长姓氏不能超过 40 个字',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'invalid_child_nickname') {
      return AppException(
        kind: AppErrorKind.validation,
        message: '宝贝姓名不能超过 40 个字',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'invalid_child_birthday') {
      return AppException(
        kind: AppErrorKind.validation,
        message: '请输入有效日期，例如 2021-06-01',
        retryable: false,
        cause: error,
      );
    }
    if (status == 401) {
      return AppException(
        kind: AppErrorKind.unauthenticated,
        message: '登录已过期，请重新登录',
        retryable: false,
        cause: error,
      );
    }
    if (status == 403) {
      return AppException(
        kind: AppErrorKind.insufficientPermission,
        message: '你没有权限访问此页面',
        retryable: false,
        cause: error,
      );
    }
    if (status == 404) {
      return AppException(
        kind: AppErrorKind.notFound,
        message: '没有找到这条内容',
        retryable: false,
        cause: error,
      );
    }
    if (status == 422 || errorCode == 'invalid_request') {
      final message = _readErrorMessage(error.response?.data);
      return AppException(
        kind: AppErrorKind.validation,
        message: message ?? '请检查填写的内容',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'phone_verification_unavailable') {
      return AppException(
        kind: AppErrorKind.serviceUnavailable,
        message: '短信验证暂时不可用，请稍后重试',
        retryable: true,
        cause: error,
      );
    }
    if (status == 429) {
      return AppException(
        kind: AppErrorKind.rateLimited,
        message: '操作太频繁，请稍后重试',
        retryable: true,
        cause: error,
      );
    }
    if (error.type == DioExceptionType.connectionError ||
        error.type == DioExceptionType.connectionTimeout ||
        error.type == DioExceptionType.receiveTimeout ||
        error.type == DioExceptionType.sendTimeout) {
      return AppException(
        kind: AppErrorKind.network,
        message: '网络连接不稳定，请检查后重试',
        retryable: true,
        cause: error,
      );
    }
    if (status != null && status >= 500) {
      return AppException(
        kind: AppErrorKind.serviceUnavailable,
        message: '服务暂时不可用，请稍后重试',
        retryable: true,
        cause: error,
      );
    }
  }

  return AppException(
    kind: AppErrorKind.unexpected,
    message: '操作没有完成，请稍后重试',
    retryable: true,
    cause: error,
  );
}

String? _readErrorCode(Object? responseData) {
  final ErrorResponseSchema? error = _readError(responseData);
  if (error == null) {
    return null;
  }
  return error.code;
}

String? _readErrorMessage(Object? responseData) {
  final error = _readError(responseData);
  return error?.message;
}

ErrorResponseSchema? _readError(Object? responseData) {
  if (responseData is Map) {
    final error = responseData['error'];
    if (error is Map) {
      final code = error['code'];
      final message = error['message'];
      final retryable = error['retryable'];
      if (code is String && message is String && retryable is bool) {
        return ErrorResponseSchema(
          code: code,
          message: message,
          retryable: retryable,
        );
      }
    }
  }
  return null;
}
