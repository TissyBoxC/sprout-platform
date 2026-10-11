/// Guardian-facing multi-device contract types.
///
/// A family owns device groups, the family linkage that carries messages across
/// its devices, and the content versions that must reach eventual consistency.
/// Removing a device only changes its group membership; the other devices keep
/// their data.
library;

/// One family device group.
class DeviceGroup {
  const DeviceGroup({
    required this.id,
    required this.name,
    required this.routeScope,
    required this.memberCount,
  });

  final String id;
  final String name;
  final String routeScope;
  final int memberCount;

  Map<String, Object?> toJson() {
    return <String, Object?>{'name': name, 'route_scope': routeScope};
  }

  factory DeviceGroup.fromJson(Map<String, Object?> json) {
    return DeviceGroup(
      id: json['id']?.toString() ?? '',
      name: json['name']?.toString() ?? '',
      routeScope: json['route_scope']?.toString() ?? 'group',
      memberCount: _intValue(json['member_count'], 0),
    );
  }
}

/// One device in a group.
class GroupMember {
  const GroupMember({required this.groupId, required this.deviceId});

  final String groupId;
  final String deviceId;

  factory GroupMember.fromJson(Map<String, Object?> json) {
    return GroupMember(
      groupId: json['group_id']?.toString() ?? '',
      deviceId: json['device_id']?.toString() ?? '',
    );
  }
}

/// One routed message.
class RoutedMessage {
  const RoutedMessage({
    required this.id,
    required this.sourceDevice,
    required this.scope,
    required this.target,
    required this.body,
  });

  final String id;
  final String sourceDevice;
  final String scope;
  final String target;
  final String body;

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'source_device': sourceDevice,
      'scope': scope,
      'target': target,
      'body': body,
    };
  }

  factory RoutedMessage.fromJson(Map<String, Object?> json) {
    return RoutedMessage(
      id: json['id']?.toString() ?? '',
      sourceDevice: json['source_device']?.toString() ?? '',
      scope: json['scope']?.toString() ?? 'family',
      target: json['target']?.toString() ?? '',
      body: json['body']?.toString() ?? '',
    );
  }
}

/// One content-sync state.
class ContentSyncState {
  const ContentSyncState({
    required this.contentKey,
    required this.version,
    required this.conflictCount,
    required this.syncState,
  });

  final String contentKey;
  final int version;
  final int conflictCount;
  final String syncState;

  bool get hasConflict => syncState == 'conflict' || conflictCount > 0;

  factory ContentSyncState.fromJson(Map<String, Object?> json) {
    return ContentSyncState(
      contentKey: json['content_key']?.toString() ?? '',
      version: _intValue(json['version'], 0),
      conflictCount: _intValue(json['conflict_count'], 0),
      syncState: json['sync_state']?.toString() ?? 'pending',
    );
  }
}

int _intValue(Object? value, int fallback) {
  if (value is int) {
    return value;
  }
  if (value is num) {
    return value.toInt();
  }
  return int.tryParse(value?.toString() ?? '') ?? fallback;
}
