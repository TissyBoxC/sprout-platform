import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:parent_app/features/device/application/device_binding_controller.dart';
import 'package:parent_app/features/device/data/device_binding_api.dart';
import 'package:parent_app/features/device/domain/device_provisioning_status.dart';
import 'package:parent_app/features/device/presentation/device_list_page.dart';

void main() {
  testWidgets('shows provisioning and session state for a bound device', (
    tester,
  ) async {
    await tester.pumpWidget(
      _buildApp(
        DeviceBindingState(
          isScanning: false,
          devices: [],
          bindings: [
            BoundDevice(
              deviceId: 'device_demo_001',
              deviceName: '初芽',
              hardwareModel: 'sprout_initial',
              firmwareVersion: '0.7.0',
              capabilities: ['wifi'],
              lifecycleStatus: 'active',
              boundAt: _boundAt,
              runtime: DeviceRuntimeStatus(
                isOnline: true,
                connectionState: 'online',
                transport: 'wifi',
                networkQuality: 'good',
                rssiDbm: -58,
                latencyMs: 42,
                packetLossPercent: 1,
                timeSyncState: 'synchronized',
                lastSyncedAt: null,
                offlineState: 'online',
                offlineReason: 'none',
                fallbackActive: false,
                pendingTelemetry: 0,
                reportedAt: null,
                receivedAt: null,
                provisioning: DeviceProvisioningStatus(
                  state: 'provisioned',
                  wifiConfigured: true,
                  sessionState: 'ready',
                  lastProvisionedAt: null,
                ),
              ),
            ),
          ],
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('已配网'), findsOneWidget);
    expect(find.text('连接正常'), findsOneWidget);
    expect(find.text('重新连接设备'), findsNothing);
  });

  testWidgets('warns and offers reconnect when device session is revoked', (
    tester,
  ) async {
    await tester.pumpWidget(
      _buildApp(
        DeviceBindingState(
          isScanning: false,
          devices: [],
          bindings: [
            BoundDevice(
              deviceId: 'device_demo_002',
              deviceName: '初芽',
              hardwareModel: 'sprout_initial',
              firmwareVersion: '0.7.0',
              capabilities: ['wifi'],
              lifecycleStatus: 'active',
              boundAt: _boundAt,
              runtime: DeviceRuntimeStatus(
                isOnline: false,
                connectionState: 'offline',
                transport: 'wifi',
                networkQuality: 'unknown',
                rssiDbm: 0,
                latencyMs: 0,
                packetLossPercent: 0,
                timeSyncState: 'unsynchronized',
                lastSyncedAt: null,
                offlineState: 'offline',
                offlineReason: 'authentication_failed',
                fallbackActive: false,
                pendingTelemetry: 0,
                reportedAt: null,
                receivedAt: null,
                provisioning: DeviceProvisioningStatus(
                  state: 'provisioned',
                  wifiConfigured: true,
                  sessionState: 'revoked',
                  lastProvisionedAt: null,
                ),
              ),
            ),
          ],
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('设备连接已失效'), findsWidgets);
    expect(find.text('重新连接设备'), findsOneWidget);

    await tester.tap(find.text('重新连接设备'));
    await tester.pumpAndSettle();

    expect(find.text('请让“初芽”保持开机，并让设备显示绑定二维码。'), findsOneWidget);
    expect(find.text('扫描设备绑定码'), findsWidgets);
  });
}

final _boundAt = DateTime.utc(2026, 10, 1, 8);

Widget _buildApp(DeviceBindingState state) {
  final router = GoRouter(
    initialLocation: '/devices',
    routes: [
      GoRoute(path: '/devices', builder: (_, _) => const DeviceListPage()),
      GoRoute(
        path: '/devices/scan',
        builder: (_, _) => const Scaffold(body: Center(child: Text('扫描设备绑定码'))),
      ),
      GoRoute(
        path: '/devices/provision',
        builder: (_, _) => const Scaffold(body: Center(child: Text('连接新设备'))),
      ),
    ],
  );
  return ProviderScope(
    overrides: [
      deviceBindingControllerProvider.overrideWith(
        () => _FakeDeviceBindingController(state),
      ),
    ],
    child: MaterialApp.router(routerConfig: router),
  );
}

class _FakeDeviceBindingController extends DeviceBindingController {
  _FakeDeviceBindingController(this.initialState);

  final DeviceBindingState initialState;

  @override
  Future<DeviceBindingState> build() async => initialState;

  @override
  Future<void> refresh() async {}
}
