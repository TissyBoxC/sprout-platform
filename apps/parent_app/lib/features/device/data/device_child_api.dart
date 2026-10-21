import '../../../core/network/api_client.dart';

/// How a device currently relates to a child.
///
/// The distinction matters to the guardian: `noChild` means the family has no
/// child profile yet, while `unassigned` means there are children and one must
/// be chosen. Showing the same screen for both would send the guardian to the
/// wrong action.
enum DeviceChildState { assigned, noChild, unassigned }

/// One child that a device has served, newest first.
class DeviceChildHistoryEntry {
  const DeviceChildHistoryEntry({
    required this.childId,
    required this.isActive,
    required this.activatedAt,
    this.deactivatedAt,
  });

  final String childId;
  final bool isActive;
  final DateTime? activatedAt;
  final DateTime? deactivatedAt;

  factory DeviceChildHistoryEntry.fromJson(Map<String, Object?> json) {
    return DeviceChildHistoryEntry(
      childId: json['child_id'] is String ? json['child_id']! as String : '',
      isActive: json['is_active'] == true,
      activatedAt: DateTime.tryParse('${json['activated_at'] ?? ''}'),
      deactivatedAt: DateTime.tryParse('${json['deactivated_at'] ?? ''}'),
    );
  }
}

/// The device's current child plus the family's child count.
class DeviceChildStatus {
  const DeviceChildStatus({
    required this.state,
    required this.childId,
    required this.childCount,
  });

  final DeviceChildState state;

  /// Empty unless [state] is [DeviceChildState.assigned].
  final String childId;
  final int childCount;

  bool get isAssigned => state == DeviceChildState.assigned;

  factory DeviceChildStatus.fromJson(Map<String, Object?> json) {
    return DeviceChildStatus(
      state: _stateFromJson('${json['state'] ?? ''}'),
      childId: json['child_id'] is String ? json['child_id']! as String : '',
      childCount: json['child_count'] is int ? json['child_count']! as int : 0,
    );
  }

  static DeviceChildState _stateFromJson(String value) {
    return switch (value) {
      'assigned' => DeviceChildState.assigned,
      'no_child' => DeviceChildState.noChild,
      // An unrecognised state is treated as "not chosen yet" so the guardian is
      // offered a choice rather than shown a wrong "assigned" screen.
      _ => DeviceChildState.unassigned,
    };
  }
}

/// Guardian-facing access to a device's current child.
abstract interface class DeviceChildApi {
  Future<DeviceChildStatus> fetchStatus(String deviceId);

  Future<List<DeviceChildHistoryEntry>> fetchHistory(String deviceId);

  Future<void> activate({required String deviceId, required String childId});

  Future<void> clear(String deviceId);
}

/// Production implementation backed by the authenticated API client.
class HttpDeviceChildApi implements DeviceChildApi {
  const HttpDeviceChildApi(this._apiClient, this._familyId);

  final ApiClient _apiClient;
  final String _familyId;

  String _base(String deviceId) {
    return '/api/v1/families/${Uri.encodeComponent(_familyId)}'
        '/devices/${Uri.encodeComponent(deviceId)}';
  }

  @override
  Future<DeviceChildStatus> fetchStatus(String deviceId) async {
    final response = await _apiClient.get('${_base(deviceId)}/child');
    final data = response['data'];
    return DeviceChildStatus.fromJson(
      data is Map ? Map<String, Object?>.from(data) : <String, Object?>{},
    );
  }

  @override
  Future<List<DeviceChildHistoryEntry>> fetchHistory(String deviceId) async {
    final response = await _apiClient.get('${_base(deviceId)}/child-history');
    final data = response['data'];
    final history = data is Map ? data['history'] : null;
    if (history is! List) {
      return const <DeviceChildHistoryEntry>[];
    }
    return history
        .whereType<Map>()
        .map(
          (entry) => DeviceChildHistoryEntry.fromJson(
            Map<String, Object?>.from(entry),
          ),
        )
        .toList(growable: false);
  }

  @override
  Future<void> activate({
    required String deviceId,
    required String childId,
  }) async {
    await _apiClient.put('${_base(deviceId)}/child', body: {'child_id': childId});
  }

  @override
  Future<void> clear(String deviceId) async {
    await _apiClient.delete('${_base(deviceId)}/child');
  }
}
