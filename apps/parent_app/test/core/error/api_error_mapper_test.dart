import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/error/api_error_mapper.dart';
import 'package:parent_app/core/error/app_exception.dart';

void main() {
  test('maps expired login without retry', () {
    final exception = mapApiError(
      DioException(
        requestOptions: RequestOptions(path: '/api/parent/v1/auth/me'),
        response: Response<void>(
          requestOptions: RequestOptions(path: '/api/parent/v1/auth/me'),
          statusCode: 401,
        ),
      ),
    );

    expect(exception.kind, AppErrorKind.unauthenticated);
    expect(exception.message, '登录已过期，请重新登录');
    expect(exception.retryable, isFalse);
  });

  test('keeps device session expiry distinct from guardian login expiry', () {
    final exception = mapApiError(
      DioException(
        requestOptions: RequestOptions(
          path: '/api/v1/devices/device_demo_001/provisioning-token',
        ),
        response: Response<Map<String, Object?>>(
          requestOptions: RequestOptions(
            path: '/api/v1/devices/device_demo_001/provisioning-token',
          ),
          statusCode: 401,
          data: const {
            'error': {
              'code': 'device_session_expired',
              'message': '设备登录已过期，请重新连接',
              'retryable': false,
            },
          },
        ),
      ),
    );

    expect(exception.kind, AppErrorKind.deviceSessionExpired);
    expect(exception.message, '设备登录已过期，请重新连接');
    expect(exception.retryable, isFalse);
  });

  test('maps connection failures to retryable network state', () {
    final exception = mapApiError(
      DioException(
        requestOptions: RequestOptions(path: '/api/parent/v1/devices'),
        type: DioExceptionType.connectionError,
      ),
    );

    expect(exception.kind, AppErrorKind.network);
    expect(exception.retryable, isTrue);
  });

  test('keeps invalid credentials distinct from an expired session', () {
    final exception = mapApiError(
      DioException(
        requestOptions: RequestOptions(path: '/api/v1/auth/login'),
        response: Response<Map<String, Object?>>(
          requestOptions: RequestOptions(path: '/api/v1/auth/login'),
          statusCode: 401,
          data: const {
            'error': {
              'code': 'invalid_credentials',
              'message': '手机号、邮箱或密码不正确',
              'retryable': false,
            },
          },
        ),
      ),
    );

    expect(exception.kind, AppErrorKind.unauthenticated);
    expect(exception.message, '手机号、邮箱或密码不正确');
    expect(exception.retryable, isFalse);
  });

  test('maps verification failures to field-level guidance', () {
    final exception = mapApiError(
      DioException(
        requestOptions: RequestOptions(path: '/api/v1/auth/register'),
        response: Response<Map<String, Object?>>(
          requestOptions: RequestOptions(path: '/api/v1/auth/register'),
          statusCode: 401,
          data: const {
            'error': {
              'code': 'invalid_verification_code',
              'message': '验证码不正确，请重新输入',
              'retryable': false,
            },
          },
        ),
      ),
    );

    expect(exception.kind, AppErrorKind.validation);
    expect(exception.message, '验证码不正确，请重新输入');
  });

  test('maps duplicate account identifiers to clear validation messages', () {
    final phoneException = mapApiError(
      DioException(
        requestOptions: RequestOptions(path: '/api/v1/auth/register'),
        response: Response<Map<String, Object?>>(
          requestOptions: RequestOptions(path: '/api/v1/auth/register'),
          statusCode: 409,
          data: const {
            'error': {
              'code': 'phone_exists',
              'message': '这个手机号已经注册过',
              'retryable': false,
            },
          },
        ),
      ),
    );
    final emailException = mapApiError(
      DioException(
        requestOptions: RequestOptions(path: '/api/v1/auth/email'),
        response: Response<Map<String, Object?>>(
          requestOptions: RequestOptions(path: '/api/v1/auth/email'),
          statusCode: 409,
          data: const {
            'error': {
              'code': 'email_exists',
              'message': '这个邮箱已经注册过',
              'retryable': false,
            },
          },
        ),
      ),
    );

    expect(phoneException.kind, AppErrorKind.validation);
    expect(phoneException.message, '这个手机号已经注册过');
    expect(emailException.kind, AppErrorKind.validation);
    expect(emailException.message, '这个邮箱已经注册过');
  });

  test('keeps a safe service message for validation failures', () {
    final exception = mapApiError(
      DioException(
        requestOptions: RequestOptions(path: '/api/v1/auth/register'),
        response: Response<Map<String, Object?>>(
          requestOptions: RequestOptions(path: '/api/v1/auth/register'),
          statusCode: 422,
          data: const {
            'error': {
              'code': 'invalid_phone',
              'message': '请输入有效的手机号',
              'retryable': false,
            },
          },
        ),
      ),
    );

    expect(exception.kind, AppErrorKind.validation);
    expect(exception.message, '请输入有效的手机号');
  });
}
