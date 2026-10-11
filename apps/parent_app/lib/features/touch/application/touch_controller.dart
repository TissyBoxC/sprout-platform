import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/touch_api.dart';
import '../domain/touch_config.dart';

/// Owns the guardian's touch configuration for the family.
class TouchController extends AsyncNotifier<TouchConfig> {
  late final TouchApi _api;

  @override
  Future<TouchConfig> build() async {
    _api = ref.read(touchApiProvider);
    return _api.fetchConfig();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_api.fetchConfig);
  }

  /// Persists one configuration revision. The server re-validates every bound,
  /// so a rejected value surfaces a guardian-facing message and keeps the
  /// current state instead of trusting the optimistic edit.
  Future<TouchConfig> save(TouchConfig config) async {
    final updated = await _api.updateConfig(config);
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

/// Read-only touch capability for one device.
final touchCapabilityProvider =
    FutureProvider.family<TouchCapability, String>((ref, deviceId) {
      return ref.read(touchApiProvider).fetchCapability(deviceId);
    });

final touchApiProvider = Provider<TouchApi>((ref) {
  return HttpTouchApi(ref.read(apiClientProvider));
});

final touchControllerProvider =
    AsyncNotifierProvider<TouchController, TouchConfig>(TouchController.new);
