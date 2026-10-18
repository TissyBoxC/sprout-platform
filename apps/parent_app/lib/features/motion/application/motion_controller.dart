import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/motion_api.dart';
import '../domain/motion_policy.dart';

/// Owns the guardian's motion and motor policy for the family.
class MotionController extends AsyncNotifier<MotionPolicy> {
  late MotionApi _api;

  @override
  Future<MotionPolicy> build() async {
    _api = ref.read(motionApiProvider);
    return _api.fetchPolicy();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_api.fetchPolicy);
  }

  /// Persists one policy revision. The server re-validates every bound, so a
  /// rejected value surfaces a guardian-facing message and keeps the current
  /// state instead of trusting the optimistic edit.
  Future<MotionPolicy> save(MotionPolicy policy) async {
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

/// Read-only motion capability for one device.
final motionCapabilityProvider =
    FutureProvider.family<MotionCapability, String>((ref, deviceId) {
      return ref.read(motionApiProvider).fetchCapability(deviceId);
    });

final motionApiProvider = Provider<MotionApi>((ref) {
  return HttpMotionApi(ref.read(apiClientProvider));
});

final motionControllerProvider =
    AsyncNotifierProvider<MotionController, MotionPolicy>(MotionController.new);
