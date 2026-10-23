import '../../../core/network/api_client.dart';
import '../domain/child_memory.dart';

/// Guardian-facing access to a child's long-term memories.
///
/// The platform scopes every call to the signed-in family, so the client never
/// sends an owner id. A memory holds a fact about a child, which is why this
/// module exposes the full value only to the guardian and never to the device
/// screen.
abstract interface class MemoryApi {
  /// Lists every memory stored for one child.
  Future<List<ChildMemory>> fetchMemories(String childId);

  /// Counts the memories of every child in the family in one read.
  ///
  /// The per-child read cannot answer "which child does the device know
  /// something about", and a guardian deciding what to review needs that
  /// overview before opening any one child.
  Future<Map<String, int>> fetchFamilyMemoryCounts();

  /// Creates or replaces one memory. A guardian write always wins over a value
  /// the device reported earlier for the same key.
  Future<ChildMemory> saveMemory({
    required String childId,
    required String memoryKey,
    required String memoryValue,
  });

  /// Removes one memory so the device stops recalling it.
  Future<void> deleteMemory({
    required String childId,
    required String memoryKey,
  });

  /// Removes every memory for one child.
  Future<void> clearMemories(String childId);
}

/// Production implementation backed by the authenticated API client.
///
/// The platform checks that [familyId] belongs to the signed-in guardian, so
/// the client passes the family id the session reported rather than inventing
/// one. Memory routes are the one guardian surface that is addressed by family
/// in the path, which is why this API needs it while the companionship
/// endpoints do not.
class HttpMemoryApi implements MemoryApi {
  const HttpMemoryApi(this._apiClient, this._familyId);

  final ApiClient _apiClient;
  final String _familyId;

  String _path(String childId, [String? memoryKey]) {
    final base =
        '/api/v1/families/${Uri.encodeComponent(_familyId)}'
        '/children/${Uri.encodeComponent(childId)}/memories';
    if (memoryKey == null) {
      return base;
    }
    return '$base/${Uri.encodeComponent(memoryKey)}';
  }

  @override
  Future<List<ChildMemory>> fetchMemories(String childId) async {
    final response = await _apiClient.get(_path(childId));
    final data = response['data'];
    final records = data is Map ? data['memories'] : null;
    if (records is! List) {
      return const <ChildMemory>[];
    }
    return records
        .whereType<Map>()
        .map((record) => ChildMemory.fromJson(record.cast<String, dynamic>()))
        .toList(growable: false);
  }

  @override
  Future<Map<String, int>> fetchFamilyMemoryCounts() async {
    final response = await _apiClient.get(
      '/api/v1/families/${Uri.encodeComponent(_familyId)}/memories',
    );
    final data = response['data'];
    final children = data is Map ? data['children'] : null;
    if (children is! List) {
      return const <String, int>{};
    }
    final counts = <String, int>{};
    for (final child in children.whereType<Map>()) {
      final childId = child['child_id']?.toString() ?? '';
      if (childId.isEmpty) {
        continue;
      }
      final memories = child['memories'];
      counts[childId] = memories is List ? memories.length : 0;
    }
    return counts;
  }

  @override
  Future<ChildMemory> saveMemory({
    required String childId,
    required String memoryKey,
    required String memoryValue,
  }) async {
    final response = await _apiClient.put(
      _path(childId),
      body: <String, dynamic>{
        'memory_key': memoryKey,
        'memory_value': memoryValue,
      },
    );
    final data = response['data'];
    final record = data is Map ? data['memory'] : null;
    if (record is Map) {
      return ChildMemory.fromJson(record.cast<String, dynamic>());
    }
    // The platform returns the stored record; falling back keeps the screen
    // usable if a future response nests it differently.
    return ChildMemory(
      id: '',
      childId: childId,
      memoryKey: memoryKey,
      memoryValue: memoryValue,
      confidence: 1,
      source: MemorySource.guardian,
      revision: 0,
      updatedAt: null,
    );
  }

  @override
  Future<void> deleteMemory({
    required String childId,
    required String memoryKey,
  }) async {
    await _apiClient.delete(_path(childId, memoryKey));
  }

  @override
  Future<void> clearMemories(String childId) async {
    await _apiClient.delete(_path(childId));
  }
}
