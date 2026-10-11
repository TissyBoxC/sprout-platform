import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/multi_device_api.dart';
import '../domain/multi_device.dart';

/// Holds the family device groups, routed messages and content-sync state.
class MultiDeviceState {
  const MultiDeviceState({
    required this.groups,
    required this.messages,
    required this.content,
  });

  final List<DeviceGroup> groups;
  final List<RoutedMessage> messages;
  final List<ContentSyncState> content;

  bool get hasConflict => content.any((state) => state.hasConflict);

  MultiDeviceState copyWith({
    List<DeviceGroup>? groups,
    List<RoutedMessage>? messages,
    List<ContentSyncState>? content,
  }) {
    return MultiDeviceState(
      groups: groups ?? this.groups,
      messages: messages ?? this.messages,
      content: content ?? this.content,
    );
  }
}

/// Owns the multi-device settings for the family.
class MultiDeviceController extends AsyncNotifier<MultiDeviceState> {
  late final MultiDeviceApi _api;

  @override
  Future<MultiDeviceState> build() async {
    _api = ref.read(multiDeviceApiProvider);
    final groups = await _api.fetchGroups();
    final messages = await _api.fetchMessages();
    final content = await _api.fetchContentStates();
    return MultiDeviceState(
      groups: groups,
      messages: messages,
      content: content,
    );
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(build);
  }

  Future<DeviceGroup> createGroup({
    required String name,
    required String routeScope,
  }) async {
    final created = await _api.createGroup(name: name, routeScope: routeScope);
    final current = state.value;
    if (current != null) {
      state = AsyncValue.data(
        current.copyWith(groups: <DeviceGroup>[created, ...current.groups]),
      );
    }
    return created;
  }

  Future<void> deleteGroup(String groupId) async {
    await _api.deleteGroup(groupId);
    final current = state.value;
    if (current == null) {
      return;
    }
    state = AsyncValue.data(
      current.copyWith(
        groups: current.groups
            .where((group) => group.id != groupId)
            .toList(growable: false),
      ),
    );
  }

  Future<void> addMember({
    required String groupId,
    required String deviceId,
  }) async {
    await _api.addMember(groupId: groupId, deviceId: deviceId);
    final current = state.value;
    if (current == null) {
      return;
    }
    state = AsyncValue.data(
      current.copyWith(
        groups: current.groups
            .map(
              (group) => group.id == groupId
                  ? DeviceGroup(
                      id: group.id,
                      name: group.name,
                      routeScope: group.routeScope,
                      memberCount: group.memberCount + 1,
                    )
                  : group,
            )
            .toList(growable: false),
      ),
    );
  }

  Future<void> removeMember({
    required String groupId,
    required String deviceId,
  }) async {
    await _api.removeMember(groupId: groupId, deviceId: deviceId);
    final current = state.value;
    if (current == null) {
      return;
    }
    state = AsyncValue.data(
      current.copyWith(
        groups: current.groups
            .map(
              (group) => group.id == groupId && group.memberCount > 0
                  ? DeviceGroup(
                      id: group.id,
                      name: group.name,
                      routeScope: group.routeScope,
                      memberCount: group.memberCount - 1,
                    )
                  : group,
            )
            .toList(growable: false),
      ),
    );
  }

  Future<RoutedMessage> routeMessage(RoutedMessage message) async {
    final routed = await _api.routeMessage(message);
    final current = state.value;
    if (current != null) {
      state = AsyncValue.data(
        current.copyWith(
          messages: <RoutedMessage>[routed, ...current.messages],
        ),
      );
    }
    return routed;
  }

  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final multiDeviceApiProvider = Provider<MultiDeviceApi>((ref) {
  return HttpMultiDeviceApi(ref.read(apiClientProvider));
});

final multiDeviceControllerProvider =
    AsyncNotifierProvider<MultiDeviceController, MultiDeviceState>(
      MultiDeviceController.new,
    );
