import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/offline_policy_api.dart';
import '../domain/offline_policy.dart';

/// Owns the guardian's view of the platform-wide offline download policy.
class OfflinePolicyController extends AsyncNotifier<OfflinePolicy> {
  late OfflinePolicyApi _api;

  @override
  Future<OfflinePolicy> build() async {
    _api = ref.read(offlinePolicyApiProvider);
    return _api.fetchPolicy();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_api.fetchPolicy);
  }

  /// Publishes a new revision. The platform validates every bound before the
  /// policy takes effect, so a rejected value keeps the previous revision.
  Future<OfflinePolicy> publish(OfflinePolicy policy) async {
    final saved = await _api.publishPolicy(policy);
    state = AsyncData(saved);
    return saved;
  }

  /// Maps a failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final offlinePolicyApiProvider = Provider<OfflinePolicyApi>((ref) {
  return HttpOfflinePolicyApi(ref.read(apiClientProvider));
});

final offlinePolicyControllerProvider =
    AsyncNotifierProvider<OfflinePolicyController, OfflinePolicy>(
      OfflinePolicyController.new,
    );
