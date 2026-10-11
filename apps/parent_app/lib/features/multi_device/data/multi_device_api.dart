import '../../../core/network/api_client.dart';
import '../domain/multi_device.dart';

/// Guardian-facing multi-device contract.
abstract interface class MultiDeviceApi {
  Future<List<DeviceGroup>> fetchGroups();

  Future<DeviceGroup> createGroup({
    required String name,
    required String routeScope,
  });

  Future<void> deleteGroup(String groupId);

  Future<List<GroupMember>> fetchMembers(String groupId);

  Future<GroupMember> addMember({
    required String groupId,
    required String deviceId,
  });

  Future<void> removeMember({
    required String groupId,
    required String deviceId,
  });

  Future<List<RoutedMessage>> fetchMessages();

  Future<RoutedMessage> routeMessage(RoutedMessage message);

  Future<List<ContentSyncState>> fetchContentStates();
}

/// Production implementation backed by the authenticated API client.
class HttpMultiDeviceApi implements MultiDeviceApi {
  const HttpMultiDeviceApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<List<DeviceGroup>> fetchGroups() async {
    final response = await _apiClient.get('/api/v1/device-mesh/groups');
    final groups = _data(response)['groups'];
    if (groups is! List) {
      return const <DeviceGroup>[];
    }
    return groups
        .whereType<Map>()
        .map((item) => DeviceGroup.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  @override
  Future<DeviceGroup> createGroup({
    required String name,
    required String routeScope,
  }) async {
    final response = await _apiClient.post(
      '/api/v1/device-mesh/groups',
      body: <String, Object?>{'name': name, 'route_scope': routeScope},
    );
    return DeviceGroup.fromJson(_data(response));
  }

  @override
  Future<void> deleteGroup(String groupId) async {
    await _apiClient.delete('/api/v1/device-mesh/groups/$groupId');
  }

  @override
  Future<List<GroupMember>> fetchMembers(String groupId) async {
    final response = await _apiClient.get(
      '/api/v1/device-mesh/groups/$groupId/members',
    );
    final members = _data(response)['members'];
    if (members is! List) {
      return const <GroupMember>[];
    }
    return members
        .whereType<Map>()
        .map((item) => GroupMember.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  @override
  Future<GroupMember> addMember({
    required String groupId,
    required String deviceId,
  }) async {
    final response = await _apiClient.post(
      '/api/v1/device-mesh/groups/$groupId/members',
      body: <String, Object?>{'device_id': deviceId},
    );
    return GroupMember.fromJson(_data(response));
  }

  @override
  Future<void> removeMember({
    required String groupId,
    required String deviceId,
  }) async {
    await _apiClient.delete(
      '/api/v1/device-mesh/groups/$groupId/members/$deviceId',
    );
  }

  @override
  Future<List<RoutedMessage>> fetchMessages() async {
    final response = await _apiClient.get('/api/v1/device-mesh/messages');
    final messages = _data(response)['messages'];
    if (messages is! List) {
      return const <RoutedMessage>[];
    }
    return messages
        .whereType<Map>()
        .map((item) => RoutedMessage.fromJson(Map<String, Object?>.from(item)))
        .toList(growable: false);
  }

  @override
  Future<RoutedMessage> routeMessage(RoutedMessage message) async {
    final response = await _apiClient.post(
      '/api/v1/device-mesh/messages',
      body: message.toJson(),
    );
    return RoutedMessage.fromJson(_data(response));
  }

  @override
  Future<List<ContentSyncState>> fetchContentStates() async {
    final response = await _apiClient.get('/api/v1/device-mesh/content');
    final states = _data(response)['states'];
    if (states is! List) {
      return const <ContentSyncState>[];
    }
    return states
        .whereType<Map>()
        .map(
          (item) => ContentSyncState.fromJson(Map<String, Object?>.from(item)),
        )
        .toList(growable: false);
  }
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing multi-device data');
  }
  return Map<String, Object?>.from(data);
}
