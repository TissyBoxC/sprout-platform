import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/cellular_api.dart';
import '../domain/cellular_policy.dart';

/// Owns the guardian's cellular policy for the family.
class CellularController extends AsyncNotifier<CellularPolicy> {
  late CellularApi _api;

  @override
  Future<CellularPolicy> build() async {
    _api = ref.read(cellularApiProvider);
    return _api.fetchPolicy();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_api.fetchPolicy);
  }

  /// Persists one policy revision. The server re-validates every bound, so a
  /// rejected value surfaces a guardian-facing message and keeps the current
  /// state instead of trusting the optimistic edit.
  Future<CellularPolicy> save(CellularPolicy policy) async {
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

/// Read-only cellular capability for one device.
final cellularCapabilityProvider =
    FutureProvider.family<CellularCapability, String>((ref, deviceId) {
      return ref.read(cellularApiProvider).fetchCapability(deviceId);
    });

final cellularApiProvider = Provider<CellularApi>((ref) {
  return HttpCellularApi(ref.read(apiClientProvider));
});

final cellularControllerProvider =
    AsyncNotifierProvider<CellularController, CellularPolicy>(
      CellularController.new,
    );
