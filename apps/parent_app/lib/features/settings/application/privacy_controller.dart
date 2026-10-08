import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../../auth/application/auth_controller.dart';
import '../data/privacy_api.dart';
import '../domain/privacy_state.dart';
import 'privacy_export_store.dart';

/// Loads privacy state and performs data export and account lifecycle actions.
class PrivacyController extends AsyncNotifier<PrivacyStatus> {
  late final PrivacyApi _api;
  late final PrivacyExportStore _exportStore;
  PrivacyStatus? _lastSuccessful;
  PrivacyExportFile? _lastExport;

  PrivacyExportFile? get lastExport => _lastExport;

  @override
  Future<PrivacyStatus> build() async {
    _api = ref.read(privacyApiProvider);
    _exportStore = ref.read(privacyExportStoreProvider);
    final status = await _api.status();
    _lastSuccessful = status;
    return status;
  }

  /// Refreshes while retaining the last successful state on a transient error.
  Future<void> refresh() async {
    try {
      final status = await _api.status();
      _lastSuccessful = status;
      state = AsyncData(status);
    } on Object catch (error, stackTrace) {
      final previous = _lastSuccessful;
      if (previous == null) {
        state = AsyncError(error, stackTrace);
      } else {
        state = AsyncData(previous);
      }
      rethrow;
    }
  }

  Future<PrivacyExportFile> exportData() async {
    final payload = await _api.export();
    final file = await _exportStore.save(payload);
    _lastExport = file;
    return file;
  }

  Future<PrivacyDeletion> requestDeletion() async {
    final deletion = await _api.requestDeletion();
    _updateDeletion(deletion);
    return deletion;
  }

  Future<PrivacyDeletion> cancelDeletion() async {
    final deletion = await _api.cancelDeletion();
    _updateDeletion(deletion);
    return deletion;
  }

  Future<GuardianConsent> withdrawConsent() async {
    final consent = await _api.withdrawConsent();
    _updateConsent(consent);
    return consent;
  }

  void _updateConsent(GuardianConsent consent) {
    final current = state.value ?? _lastSuccessful;
    if (current == null) {
      return;
    }
    final next = current.copyWith(consent: consent);
    _lastSuccessful = next;
    state = AsyncData(next);
  }

  void _updateDeletion(PrivacyDeletion deletion) {
    final current = state.value ?? _lastSuccessful;
    if (current == null) {
      return;
    }
    final next = current.copyWith(deletion: deletion);
    _lastSuccessful = next;
    state = AsyncData(next);
  }
}

final privacyApiProvider = Provider<PrivacyApi>((ref) {
  return PrivacyApi(ref.read(apiClientProvider));
});

final privacyExportStoreProvider = Provider<PrivacyExportStore>((ref) {
  return PrivacyExportStore();
});

final privacyExportOpenerProvider = Provider<PrivacyExportOpener>((ref) {
  return const SystemPrivacyExportLauncher();
});

final privacyControllerProvider =
    AsyncNotifierProvider<PrivacyController, PrivacyStatus>(
      PrivacyController.new,
    );

/// Performs the local sign-out boundary after consent withdrawal or deletion.
final privacySessionCloserProvider = Provider<Future<void> Function()>((ref) {
  return () => ref.read(authControllerProvider.notifier).logout();
});

/// Returns copy safe to show for privacy operations.
String privacyErrorMessage(Object? error) {
  if (error is AppException && error.message.trim().isNotEmpty) {
    return error.message;
  }
  return '操作没有完成，请检查网络后重试';
}
