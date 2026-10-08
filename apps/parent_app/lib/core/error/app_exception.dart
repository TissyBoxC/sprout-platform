/// Category used by UI layers to choose a safe next action.
enum AppErrorKind {
  unauthenticated,
  deviceSessionExpired,
  insufficientPermission,
  notFound,
  validation,
  rateLimited,
  network,
  serviceUnavailable,
  unexpected,
}

/// Error that has already been translated for user-facing state.
class AppException implements Exception {
  const AppException({
    required this.kind,
    required this.message,
    required this.retryable,
    this.cause,
  });

  final AppErrorKind kind;
  final String message;
  final bool retryable;
  final Object? cause;

  @override
  String toString() => 'AppException($kind)';
}

/// Returns whether the server explicitly rejected the current session.
///
/// Network failures, timeouts, and server errors are transient and must keep
/// the local session so the user can retry without signing in again.
bool isSessionRejected(Object error) {
  if (error is! AppException) {
    return false;
  }
  return error.kind == AppErrorKind.unauthenticated ||
      error.kind == AppErrorKind.insufficientPermission;
}

/// Returns whether a device rejected its own authentication session.
bool isDeviceSessionExpired(Object error) {
  return error is AppException &&
      error.kind == AppErrorKind.deviceSessionExpired;
}
