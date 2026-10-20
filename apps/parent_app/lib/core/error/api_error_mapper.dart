import 'package:dio/dio.dart';

import '../contracts/generated/envelope.dart';

import 'app_exception.dart';

/// Maps transport and contract failures to stable user-facing errors.
AppException mapApiError(Object error) {
  if (error is DioException) {
    final status = error.response?.statusCode;
    final errorCode = _readErrorCode(error.response?.data);

    // Domain errors carry actionable meanings and must be resolved before
    // generic 403/404/409 handling.
    final domainError = _mapDomainError(errorCode, error);
    if (domainError != null) {
      return domainError;
    }
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
    if (errorCode == 'member_exists') {
      return AppException(
        kind: AppErrorKind.conflict,
        message: '该账号已经是家庭成员',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'last_owner') {
      return AppException(
        kind: AppErrorKind.conflict,
        message: '家庭必须保留一位所有者',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'invitation_state') {
      return AppException(
        kind: AppErrorKind.conflict,
        message: '该邀请已处理或已存在待处理邀请',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'invitation_expired' ||
        errorCode == 'invalid_invitation') {
      return AppException(
        kind: AppErrorKind.validation,
        message: '邀请无效或已过期，请让家人重新邀请',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'invalid_member') {
      return AppException(
        kind: AppErrorKind.insufficientPermission,
        message: '当前角色没有执行该操作的权限',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'family_not_found') {
      return AppException(
        kind: AppErrorKind.notFound,
        message: '没有找到这个成员或邀请',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'account_disabled') {
      return AppException(
        kind: AppErrorKind.accountDisabled,
        message: '账号暂时无法使用，请联系客服处理',
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
    if (errorCode == 'email_login_disabled') {
      return AppException(
        kind: AppErrorKind.validation,
        message: '邮箱登录暂时关闭，请使用手机号登录',
        retryable: false,
        cause: error,
      );
    }
    if (errorCode == 'registration_disabled') {
      return AppException(
        kind: AppErrorKind.validation,
        message: '当前暂时不能注册新账号',
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
    if (status == 409 || errorCode == 'parent_policy_conflict') {
      return AppException(
        kind: AppErrorKind.conflict,
        message: '设置已在其他页面更新，请重新加载后再修改',
        retryable: true,
        cause: error,
      );
    }
    if (status == 422 || errorCode == 'invalid_request') {
      return AppException(
        kind: AppErrorKind.validation,
        message: '请检查填写的内容',
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

AppException? _mapDomainError(String? code, DioException error) {
  final message = switch (code) {
    'token_expired' => '绑定码已过期，请在设备上重新生成',
    'token_used' => '这个绑定码已经使用过，请在设备上重新生成',
    'device_already_bound' => '这台设备已经绑定到当前账号或其他家庭',
    'device_disabled' => '这台设备暂时停用，请联系客服处理',
    'token_not_found' => '没有找到这个绑定码，请重新扫描设备上的二维码',
    'device_not_found' => '没有找到这台设备，请确认设备已开机后重试',
    _ => null,
  };
  if (message == null) {
    return null;
  }
  final kind = switch (code) {
    'token_expired' => AppErrorKind.validation,
    'token_used' || 'device_already_bound' => AppErrorKind.conflict,
    'device_disabled' => AppErrorKind.accountDisabled,
    'token_not_found' || 'device_not_found' => AppErrorKind.notFound,
    _ => AppErrorKind.unexpected,
  };
  return AppException(
    kind: kind,
    message: message,
    retryable: code == 'device_not_found',
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
