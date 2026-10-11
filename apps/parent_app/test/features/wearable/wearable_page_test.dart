import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/wearable/application/wearable_controller.dart';
import 'package:parent_app/features/wearable/data/wearable_api.dart';
import 'package:parent_app/features/wearable/domain/wearable.dart';
import 'package:parent_app/features/wearable/presentation/wearable_page.dart';

void main() {
  testWidgets('shows policy, contacts, geofence and devices', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [wearableApiProvider.overrideWithValue(_FakeWearableApi())],
        child: const MaterialApp(home: WearablePage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('穿戴与定位'), findsOneWidget);
    expect(find.text('允许采集与上传位置'), findsOneWidget);
    expect(find.text('家庭电子围栏'), findsOneWidget);
    expect(find.text('紧急联系人'), findsOneWidget);
    expect(find.text('奶奶'), findsOneWidget);
    expect(find.text('设备定位'), findsOneWidget);
    await tester.pumpAndSettle();
  });

  testWidgets('revokes the location permission through the controller', (
    tester,
  ) async {
    final api = _FakeWearableApi(granted: true);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [wearableApiProvider.overrideWithValue(api)],
        child: const MaterialApp(home: WearablePage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('允许采集与上传位置'), findsOneWidget);
    await tester.tap(find.byType(Switch));
    await tester.pumpAndSettle();

    expect(api.savedPolicy?.permissionGranted, isFalse);
  });

  testWidgets('cancels an active SOS', (tester) async {
    final api = _FakeWearableApi(activeSos: true);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [wearableApiProvider.overrideWithValue(api)],
        child: const MaterialApp(home: WearablePage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('SOS 紧急求助生效中'), findsOneWidget);
    await tester.tap(find.text('解除'));
    await tester.pumpAndSettle();

    expect(api.cancelledSosId, 'sos-1');
  });
}

class _FakeWearableApi implements WearableApi {
  _FakeWearableApi({this.granted = false, this.activeSos = false});

  final bool granted;
  final bool activeSos;
  LocationPolicy? savedPolicy;
  String? cancelledSosId;

  @override
  Future<LocationPolicy> fetchPolicy() async {
    return LocationPolicy(
      enabled: granted,
      permissionGranted: granted,
      precision: 'balanced',
      uploadIntervalSeconds: 300,
      retentionDays: 7,
      geofenceRadiusMeters: 500,
    );
  }

  @override
  Future<LocationPolicy> updatePolicy(LocationPolicy policy) async {
    savedPolicy = policy;
    return policy;
  }

  @override
  Future<List<EmergencyContact>> fetchContacts() async {
    return const <EmergencyContact>[
      EmergencyContact(
        id: 'contact-1',
        childId: '',
        displayName: '奶奶',
        phone: '13800000000',
        role: 'guardian',
        priority: 1,
        enabled: true,
      ),
    ];
  }

  @override
  Future<EmergencyContact> createContact(EmergencyContact contact) async =>
      contact;

  @override
  Future<EmergencyContact> updateContact(EmergencyContact contact) async =>
      contact;

  @override
  Future<void> deleteContact(String contactId) async {}

  @override
  Future<HomeGeofence?> fetchGeofence() async {
    return const HomeGeofence(
      id: 'geofence-1',
      childId: '',
      latitude: 1.35,
      longitude: 103.81,
      radiusMeters: 500,
      enabled: true,
    );
  }

  @override
  Future<HomeGeofence> updateGeofence(HomeGeofence geofence) async => geofence;

  @override
  Future<List<WearableDeviceStatus>> fetchDeviceStatuses() async {
    return const <WearableDeviceStatus>[
      WearableDeviceStatus(
        deviceId: 'device-1',
        locationEnabled: true,
        permissionGranted: true,
        precision: 'balanced',
        sosState: 'idle',
        latitude: 1.35,
        longitude: 103.81,
        accuracyMeters: 8,
        geofenceState: 'inside',
      ),
    ];
  }

  @override
  Future<List<SosEvent>> fetchActiveSos() async {
    if (!activeSos) {
      return const <SosEvent>[];
    }
    return const <SosEvent>[
      SosEvent(
        id: 'sos-1',
        deviceId: 'device-1',
        latitude: 1.35,
        longitude: 103.81,
        state: 'triggered',
      ),
    ];
  }

  @override
  Future<List<SosEvent>> fetchSosHistory() async => const <SosEvent>[];

  @override
  Future<SosEvent> cancelSos(String eventId) async {
    cancelledSosId = eventId;
    return SosEvent(
      id: eventId,
      deviceId: 'device-1',
      latitude: 1.35,
      longitude: 103.81,
      state: 'cancelled',
    );
  }
}
