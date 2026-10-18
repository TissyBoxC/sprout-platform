import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/power_api.dart';
import '../domain/power_policy.dart';

/// Owns the guardian's battery and low-power policy for the family.
class PowerController extends AsyncNotifier<PowerPolicy> {
  late PowerApi _api;

  @override
  Future<PowerPolicy> build() async {
    _api = ref.read(powerApiProvider);
    return _api.fetchPolicy();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_api.fetchPolicy);
  }

  /// Persists one policy revision. The server re-validates every bound, so a
  /// rejected value surfaces a guardian-facing message and keeps the current
  /// state instead of trusting the optimistic edit.
  Future<PowerPolicy> save(PowerPolicy policy) async {
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

/// Read-only battery capability for one device.
final powerCapabilityProvider = FutureProvider.family<PowerCapability, String>((
  ref,
  deviceId,
) {
  return ref.read(powerApiProvider).fetchCapability(deviceId);
});

final powerApiProvider = Provider<PowerApi>((ref) {
  return HttpPowerApi(ref.read(apiClientProvider));
});

final powerControllerProvider =
    AsyncNotifierProvider<PowerController, PowerPolicy>(PowerController.new);
