/// Stores sensitive values such as refresh tokens.
abstract interface class SecureStore {
  Future<String?> read(String key);

  Future<void> write(String key, String value);

  Future<void> delete(String key);
}

/// Keys shared by the current session format and earlier app installs.
abstract final class SessionStoreKeys {
  static const accessToken = 'auth_access_token';
  static const refreshToken = 'auth_refresh_token';
}

/// A validated token pair that belongs to one guardian session.
class StoredSession {
  const StoredSession({required this.accessToken, required this.refreshToken});

  final String accessToken;
  final String refreshToken;

  bool get hasRefreshToken => refreshToken.isNotEmpty;
}

/// Optional capability for stores that persist the pair as one encrypted value.
abstract interface class SessionSecureStore implements SecureStore {
  Future<StoredSession?> readSession();

  Future<void> writeSession(StoredSession session);

  Future<void> clearSession();
}

/// Persists a token pair through the best available storage capability.
///
/// Older installations stored access and refresh tokens under separate keys.
/// Reading falls back to those keys so an upgrade does not sign the guardian
/// out; writing removes both copies after the current format succeeds.
class SessionStore {
  const SessionStore(this._secureStore);

  final SecureStore _secureStore;

  Future<StoredSession?> read() async {
    final secureStore = _secureStore;
    if (secureStore is SessionSecureStore) {
      final session = await secureStore.readSession();
      if (session != null && session.hasRefreshToken) {
        return session;
      }
    }

    final accessToken =
        await secureStore.read(SessionStoreKeys.accessToken) ?? '';
    final refreshToken =
        await secureStore.read(SessionStoreKeys.refreshToken) ?? '';
    if (accessToken.isEmpty && refreshToken.isEmpty) {
      return null;
    }
    return StoredSession(accessToken: accessToken, refreshToken: refreshToken);
  }

  Future<void> write(StoredSession session) async {
    final secureStore = _secureStore;
    if (secureStore is SessionSecureStore) {
      await secureStore.writeSession(session);
      return;
    }

    // Refresh first so an interrupted write leaves a recoverable pair.
    await secureStore.write(
      SessionStoreKeys.refreshToken,
      session.refreshToken,
    );
    await secureStore.write(SessionStoreKeys.accessToken, session.accessToken);
  }

  Future<void> clear() async {
    final secureStore = _secureStore;
    if (secureStore is SessionSecureStore) {
      await secureStore.clearSession();
    }
    await secureStore.delete(SessionStoreKeys.accessToken);
    await secureStore.delete(SessionStoreKeys.refreshToken);
  }
}
