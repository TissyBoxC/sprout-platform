/// Category used by UI layers to choose a safe next action.
enum AppErrorKind {
  unauthenticated,
  accountDisabled,
  deviceSessionExpired,
  insufficientPermission,
  notFound,
  conflict,
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
/// A disabled account is a business state, not an expired session. Its
/// credentials stay available so the interface can explain the restriction
/// and support can restore the account without forcing a new sign-in.
bool isSessionRejected(Object error) {
  if (error is! AppException) {
    return false;
  }
  return error.kind == AppErrorKind.unauthenticated;
}

/// Returns whether a device rejected its own authentication session.
bool isDeviceSessionExpired(Object error) {
  return error is AppException &&
      error.kind == AppErrorKind.deviceSessionExpired;
}
