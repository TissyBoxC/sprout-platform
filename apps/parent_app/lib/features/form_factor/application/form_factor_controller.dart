import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/providers.dart';
import '../data/form_factor_api.dart';
import '../domain/form_factor_profile.dart';

/// Read-only composed form-factor capability for one device.
final formFactorCapabilityProvider =
    FutureProvider.family<FormFactorCapability, String>((ref, deviceId) {
  return ref.read(formFactorApiProvider).fetchCapability(deviceId);
});

final formFactorApiProvider = Provider<FormFactorApi>((ref) {
  return HttpFormFactorApi(ref.read(apiClientProvider));
});
