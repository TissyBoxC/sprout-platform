import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/display_api.dart';
import '../domain/display_policy.dart';

/// Owns the guardian's eye-care display policy for the family.
class DisplayController extends AsyncNotifier<DisplayPolicy> {
  late DisplayApi _api;

  @override
  Future<DisplayPolicy> build() async {
    _api = ref.read(displayApiProvider);
    return _api.fetchPolicy();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_api.fetchPolicy);
  }

  /// Persists one policy revision. The server re-validates every bound, so a
  /// rejected value surfaces a guardian-facing message and keeps the current
  /// state instead of trusting the optimistic edit.
  Future<DisplayPolicy> save(DisplayPolicy policy) async {
    final updated = await _api.updatePolicy(policy);
    state = AsyncValue.data(updated);
    return updated;
  }

  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

/// Read-only display capability for one device.
final displayCapabilityProvider =
    FutureProvider.family<DisplayCapability, String>((ref, deviceId) {
      return ref.read(displayApiProvider).fetchCapability(deviceId);
    });

final displayApiProvider = Provider<DisplayApi>((ref) {
  return HttpDisplayApi(ref.read(apiClientProvider));
});

final displayControllerProvider =
    AsyncNotifierProvider<DisplayController, DisplayPolicy>(
      DisplayController.new,
    );
