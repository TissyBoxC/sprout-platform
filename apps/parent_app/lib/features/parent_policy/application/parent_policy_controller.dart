import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/providers.dart';
import '../../auth/application/auth_controller.dart';
import '../data/parent_policy_api.dart';
import '../domain/parent_policy.dart';

/// Loads and updates the policy for one child.
class ParentPolicyController extends AsyncNotifier<ParentPolicy> {
  ParentPolicyController(this.childId);

  final String childId;
  late final ParentPolicyApi _api;

  @override
  Future<ParentPolicy> build() {
    _api = ref.read(parentPolicyApiProvider);
    return _api.get(childId);
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(() => _api.get(childId));
  }

  Future<ParentPolicy> savePolicy(ParentPolicyDraft draft) async {
    final updated = await _api.update(childId, draft);
    state = AsyncData(updated);
    return updated;
  }
}

final parentPolicyApiProvider = Provider<ParentPolicyApi>((ref) {
  return ParentPolicyApi(ref.read(apiClientProvider));
});

final parentPolicyControllerProvider =
    AsyncNotifierProvider.family<ParentPolicyController, ParentPolicy, String>(
      ParentPolicyController.new,
    );

/// Returns copy safe to display for the parent policy module.
String parentPolicyErrorMessage(Object? error) {
  return authErrorMessage(error);
}
