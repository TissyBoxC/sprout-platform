import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/multi_device/application/multi_device_controller.dart';
import 'package:parent_app/features/multi_device/data/multi_device_api.dart';
import 'package:parent_app/features/multi_device/domain/multi_device.dart';
import 'package:parent_app/features/multi_device/presentation/multi_device_page.dart';

void main() {
  testWidgets('shows groups, linkage messages and content sync state', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          multiDeviceApiProvider.overrideWithValue(_FakeMultiDeviceApi()),
        ],
        child: const MaterialApp(home: MultiDevicePage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('多设备'), findsOneWidget);
    expect(find.text('设备组'), findsOneWidget);
    expect(find.text('客厅设备'), findsOneWidget);
    expect(find.text('家庭联动消息'), findsOneWidget);
    expect(find.text('晚安啦'), findsOneWidget);
    expect(find.text('内容同步'), findsOneWidget);
    expect(find.text('story-1'), findsOneWidget);
    expect(find.textContaining('有冲突'), findsOneWidget);
  });

  testWidgets('creates a group and adds it to the list', (tester) async {
    final api = _FakeMultiDeviceApi();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [multiDeviceApiProvider.overrideWithValue(api)],
        child: const MaterialApp(home: MultiDevicePage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byTooltip('创建设备组'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).first, '书房设备');
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();

    expect(api.createdName, '书房设备');
    expect(find.text('书房设备'), findsOneWidget);
  });

  testWidgets('routes a message through the controller', (tester) async {
    final api = _FakeMultiDeviceApi();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [multiDeviceApiProvider.overrideWithValue(api)],
        child: const MaterialApp(home: MultiDevicePage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('发送'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).last, '起床啦');
    await tester.tap(find.widgetWithText(FilledButton, '发送'));
    await tester.pumpAndSettle();

    expect(api.routedBody, '起床啦');
    expect(find.text('起床啦'), findsOneWidget);
  });
}

class _FakeMultiDeviceApi implements MultiDeviceApi {
  String? createdName;
  String? routedBody;
  final List<DeviceGroup> _groups = <DeviceGroup>[
    const DeviceGroup(
      id: 'group-1',
      name: '客厅设备',
      routeScope: 'family',
      memberCount: 2,
    ),
  ];

  @override
  Future<List<DeviceGroup>> fetchGroups() async =>
      List<DeviceGroup>.unmodifiable(_groups);

  @override
  Future<DeviceGroup> createGroup({
    required String name,
    required String routeScope,
  }) async {
    createdName = name;
    final group = DeviceGroup(
      id: 'group-${_groups.length + 1}',
      name: name,
      routeScope: routeScope,
      memberCount: 0,
    );
    _groups.insert(0, group);
    return group;
  }

  @override
  Future<void> deleteGroup(String groupId) async {
    _groups.removeWhere((group) => group.id == groupId);
  }

  @override
  Future<List<GroupMember>> fetchMembers(String groupId) async =>
      const <GroupMember>[];

  @override
  Future<GroupMember> addMember({
    required String groupId,
    required String deviceId,
  }) async {
    return GroupMember(groupId: groupId, deviceId: deviceId);
  }

  @override
  Future<void> removeMember({
    required String groupId,
    required String deviceId,
  }) async {}

  @override
  Future<List<RoutedMessage>> fetchMessages() async {
    return const <RoutedMessage>[
      RoutedMessage(
        id: 'message-1',
        sourceDevice: 'device-1',
        scope: 'family',
        target: '',
        body: '晚安啦',
      ),
    ];
  }

  @override
  Future<RoutedMessage> routeMessage(RoutedMessage message) async {
    routedBody = message.body;
    return RoutedMessage(
      id: 'message-2',
      sourceDevice: 'device-1',
      scope: message.scope,
      target: message.target,
      body: message.body,
    );
  }

  @override
  Future<List<ContentSyncState>> fetchContentStates() async {
    return const <ContentSyncState>[
      ContentSyncState(
        contentKey: 'story-1',
        version: 3,
        conflictCount: 1,
        syncState: 'conflict',
      ),
    ];
  }
}
