import 'package:flutter/foundation.dart';

/// Runtime configuration shared by the parent application.
class AppConfig {
  const AppConfig({required this.apiBaseUrl, required this.requestTimeout});

  final Uri apiBaseUrl;
  final Duration requestTimeout;

  /// Rejects a release build that would silently use the emulator default.
  ///
  /// Debug builds may keep the local default; release builds must pass an
  /// explicit HTTPS endpoint through `API_BASE_URL`.
  factory AppConfig.fromEnvironment() {
    const apiBaseUrl = String.fromEnvironment(
      'API_BASE_URL',
      defaultValue: 'http://10.0.2.2:8081',
    );
    const configuredTimeoutSeconds = int.fromEnvironment(
      'API_TIMEOUT_SECONDS',
      defaultValue: 15,
    );
    final uri = Uri.tryParse(apiBaseUrl);
    if (uri == null || uri.host.isEmpty) {
      throw StateError('API_BASE_URL must be a valid HTTP(S) endpoint');
    }
    final isSecure = uri.scheme == 'https';
    final isLocalDebug =
        !kReleaseMode &&
        uri.scheme == 'http' &&
        _isLoopbackOrPrivateHost(uri.host);
    if (!isSecure && !isLocalDebug) {
      throw StateError(
        'Release builds require an HTTPS API_BASE_URL; local HTTP is debug-only',
      );
    }
    if (configuredTimeoutSeconds <= 0) {
      throw StateError('API_TIMEOUT_SECONDS must be positive');
    }
    return AppConfig(
      apiBaseUrl: uri,
      requestTimeout: Duration(seconds: configuredTimeoutSeconds),
    );
  }
}

bool _isLoopbackOrPrivateHost(String host) {
  if (host == 'localhost' || host == '127.0.0.1' || host == '::1') {
    return true;
  }
  final parts = host.split('.');
  if (parts.length != 4) {
    return false;
  }
  final octets = parts.map(int.tryParse).toList(growable: false);
  if (octets.any((octet) => octet == null || octet < 0 || octet > 255)) {
    return false;
  }
  final first = octets[0]!;
  final second = octets[1]!;
  return first == 10 ||
      (first == 172 && second >= 16 && second <= 31) ||
      (first == 192 && second == 168) ||
      first == 127;
}
