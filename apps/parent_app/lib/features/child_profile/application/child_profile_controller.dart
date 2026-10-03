import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/providers.dart';
import '../../auth/application/auth_controller.dart';
import '../data/child_profile_api.dart';
import '../domain/child_profile.dart';

/// Loads and mutates every child profile owned by the signed-in guardian.
class ChildProfileController extends AsyncNotifier<List<ChildProfile>> {
  late final ChildProfileApi _api;

  @override
  Future<List<ChildProfile>> build() {
    _api = ref.read(childProfileApiProvider);
    return _api.list();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_api.list);
  }

  Future<ChildProfile> create(ChildProfileDraft draft) async {
    final created = await _api.create(draft);
    await refresh();
    return created;
  }

  Future<ChildProfile> saveProfile(
    String childId,
    ChildProfileDraft draft,
  ) async {
    final updated = await _api.update(childId, draft);
    await refresh();
    return updated;
  }

  Future<void> delete(String childId) async {
    await _api.delete(childId);
    await refresh();
  }
}

final childProfileApiProvider = Provider<ChildProfileApi>((ref) {
  return ChildProfileApi(ref.read(apiClientProvider));
});

final childProfileControllerProvider =
    AsyncNotifierProvider<ChildProfileController, List<ChildProfile>>(
      ChildProfileController.new,
    );

/// Returns copy safe to display for the child profile module.
String childProfileErrorMessage(Object? error) {
  return authErrorMessage(error);
}
