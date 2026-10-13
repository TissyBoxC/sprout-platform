import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import 'secure_store.dart';

/// Platform-backed secure storage for access and refresh tokens.
class FlutterSecureStore implements SessionSecureStore {
  FlutterSecureStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();

  static const _sessionKey = 'auth_session';

  final FlutterSecureStorage _storage;

  @override
  Future<String?> read(String key) {
    return _storage.read(key: key);
  }

  @override
  Future<void> write(String key, String value) {
    return _storage.write(key: key, value: value);
  }

  @override
  Future<void> delete(String key) {
    return _storage.delete(key: key);
  }

  @override
  Future<StoredSession?> readSession() async {
    final decoded = _decodeSession(await _storage.read(key: _sessionKey));
    if (decoded != null) {
      return decoded;
    }

    final legacySession = StoredSession(
      accessToken: await _storage.read(key: SessionStoreKeys.accessToken) ?? '',
      refreshToken:
          await _storage.read(key: SessionStoreKeys.refreshToken) ?? '',
    );
    if (!legacySession.hasRefreshToken) {
      return null;
    }

    await writeSession(legacySession);
    return legacySession;
  }

  @override
  Future<void> writeSession(StoredSession session) async {
    await _storage.write(
      key: _sessionKey,
      value: jsonEncode(<String, String>{
        'access_token': session.accessToken,
        'refresh_token': session.refreshToken,
      }),
    );
    await _deleteLegacySession();
  }

  @override
  Future<void> clearSession() async {
    await _storage.delete(key: _sessionKey);
    await _deleteLegacySession();
  }

  StoredSession? _decodeSession(String? encoded) {
    if (encoded == null || encoded.isEmpty) {
      return null;
    }
    try {
      final decoded = jsonDecode(encoded);
      if (decoded is! Map) {
        return null;
      }
      final accessToken = decoded['access_token'];
      final refreshToken = decoded['refresh_token'];
      if (accessToken is! String || refreshToken is! String) {
        return null;
      }
      return StoredSession(
        accessToken: accessToken,
        refreshToken: refreshToken,
      );
    } on FormatException {
      return null;
    }
  }

  Future<void> _deleteLegacySession() async {
    await _storage.delete(key: SessionStoreKeys.accessToken);
    await _storage.delete(key: SessionStoreKeys.refreshToken);
  }
}
