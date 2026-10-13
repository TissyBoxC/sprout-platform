import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'config/app_config.dart';
import 'network/api_client.dart';
import 'network/session_manager.dart';
import 'storage/flutter_secure_store.dart';
import 'storage/secure_store.dart';

/// Platform secure storage shared by authentication and session recovery.
final secureStoreProvider = Provider<SecureStore>((ref) {
  return FlutterSecureStore();
});

/// Runtime configuration loaded from compile-time values.
final appConfigProvider = Provider<AppConfig>((ref) {
  return AppConfig.fromEnvironment();
});

/// Shared session persistence and refresh coordinator.
final sessionManagerProvider = Provider<SessionManager>((ref) {
  return SessionManager(
    secureStore: ref.watch(secureStoreProvider),
    appConfig: ref.watch(appConfigProvider),
  );
});

/// Authenticated HTTP boundary for all feature data sources.
final apiClientProvider = Provider<ApiClient>((ref) {
  final secureStore = ref.watch(secureStoreProvider);
  final appConfig = ref.watch(appConfigProvider);
  final sessionManager = ref.watch(sessionManagerProvider);
  return DioApiClient(
    config: ApiClientConfig(appConfig: appConfig),
    secureStore: secureStore,
    refreshSession: sessionManager.refresh,
  );
});
