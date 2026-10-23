import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/providers.dart';
import '../../family/application/family_controller.dart';
import '../data/memory_api.dart';
import '../domain/child_memory.dart';

/// Resolves the signed-in family id before the memory API can address it.
///
/// Memory routes are family-addressed in the path while the platform also
/// checks that the family belongs to this guardian, so the id has to come from
/// the session rather than from the screen.
final memoryFamilyIdProvider = FutureProvider<String>((ref) async {
  final family = await ref.watch(familyControllerProvider.future);
  return family.capability.familyId;
});

final memoryApiProvider = Provider<MemoryApi?>((ref) {
  final familyId = ref.watch(memoryFamilyIdProvider).value;
  if (familyId == null || familyId.isEmpty) {
    // Nothing may be requested before the family is known: an empty family id
    // would reach the platform as a permission error rather than a load state.
    // The controller awaits the id first, so this null only guards misuse.
    return null;
  }
  return HttpMemoryApi(ref.watch(apiClientProvider), familyId);
});

/// How many memories each child in the family has.
///
/// A read failure yields an empty map rather than an error state: the counts
/// only annotate the child chips, and a screen that cannot draw its chips
/// because a badge failed would hide the memories that did load.
final familyMemoryCountsProvider = FutureProvider<Map<String, int>>((
  ref,
) async {
  final api = ref.watch(memoryApiProvider);
  if (api == null) {
    return const <String, int>{};
  }
  try {
    return await api.fetchFamilyMemoryCounts();
  } on Object {
    return const <String, int>{};
  }
});

/// One child the guardian may inspect memories for.
class MemoryChild {
  const MemoryChild({required this.id, required this.name});

  final String id;
  final String name;
}

/// Everything the memory screen renders for one child.
class ChildMemoryState {
  const ChildMemoryState({
    required this.childId,
    required this.memories,
    this.isSaving = false,
  });

  const ChildMemoryState.initial()
    : childId = '',
      memories = const <ChildMemory>[],
      isSaving = false;

  final String childId;
  final List<ChildMemory> memories;
  final bool isSaving;

  bool get isEmpty => memories.isEmpty;
}

/// Loads and edits one child's long-term memories.
///
/// Every write is guardian-authored, so a value set here wins over whatever the
/// device reported for the same key; the screen states that so a guardian is not
/// surprised when their correction sticks.
class ChildMemoryController extends AsyncNotifier<ChildMemoryState> {
  int _requestGeneration = 0;

  @override
  Future<ChildMemoryState> build() async {
    // The child list arrives from the page; nothing is loaded until it selects
    // one, so the initial state is empty rather than a failed request.
    return const ChildMemoryState.initial();
  }

  /// Loads one child's memories and returns them for rendering.
  Future<void> load(String childId) async {
    final generation = ++_requestGeneration;
    state = const AsyncValue<ChildMemoryState>.loading();
    final next = await AsyncValue.guard(() async {
      final api = await _api();
      final memories = await api.fetchMemories(childId);
      return ChildMemoryState(childId: childId, memories: memories);
    });
    if (generation != _requestGeneration) {
      return;
    }
    state = next;
  }

  /// Saves one memory and refreshes the list so the revision stays accurate.
  Future<void> save({
    required String childId,
    required String memoryKey,
    required String memoryValue,
  }) async {
    final current = state.value;
    if (current != null) {
      state = AsyncValue<ChildMemoryState>.data(
        ChildMemoryState(
          childId: current.childId,
          memories: current.memories,
          isSaving: true,
        ),
      );
    }
    try {
      final api = await _api();
      await api.saveMemory(
        childId: childId,
        memoryKey: memoryKey,
        memoryValue: memoryValue,
      );
      await load(childId);
    } catch (error, stackTrace) {
      state = AsyncValue<ChildMemoryState>.error(error, stackTrace);
    }
  }

  /// Deletes one memory.
  Future<void> remove({
    required String childId,
    required String memoryKey,
  }) async {
    try {
      final api = await _api();
      await api.deleteMemory(childId: childId, memoryKey: memoryKey);
      await load(childId);
    } catch (error, stackTrace) {
      state = AsyncValue<ChildMemoryState>.error(error, stackTrace);
    }
  }

  /// Deletes every memory for one child.
  Future<void> clear(String childId) async {
    try {
      final api = await _api();
      await api.clearMemories(childId);
      await load(childId);
    } catch (error, stackTrace) {
      state = AsyncValue<ChildMemoryState>.error(error, stackTrace);
    }
  }

  Future<MemoryApi> _api() async {
    await ref.read(memoryFamilyIdProvider.future);
    final api = ref.read(memoryApiProvider);
    if (api == null) {
      throw StateError('memory api is not ready');
    }
    return api;
  }
}

final childMemoryControllerProvider =
    AsyncNotifierProvider<ChildMemoryController, ChildMemoryState>(
      ChildMemoryController.new,
    );
