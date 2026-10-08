import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/device/data/device_binding_api.dart';

void main() {
  test('device status list maps online runtime and last connection', () async {
    final client = _RecordingApiClient();
    final api = DeviceBindingApi(client);

    final devices = await api.list();

    expect(client.path, '/api/v1/devices/status');
    expect(devices, hasLength(1));
    expect(devices.single.deviceName, '初芽');
    expect(devices.single.runtime?.isOnline, isTrue);
    expect(devices.single.runtime?.networkQuality, 'good');
    expect(
      devices.single.runtime?.receivedAt,
      DateTime.parse('2026-10-03T10:00:02Z'),
    );
  });

  test('device status parses provisioning state and session state', () async {
    final client = _RecordingApiClient(
      provisioning: {
        'state': 'provisioned',
        'wifi_configured': true,
        'session_state': 'reauth_required',
        'dropped_events': 4,
        'last_provisioned_at': '2026-10-03T09:58:00Z',
      },
    );
    final api = DeviceBindingApi(client);

    final devices = await api.list();

    final provisioning = devices.single.runtime?.provisioning;
    expect(provisioning, isNotNull);
    expect(provisioning?.state, 'provisioned');
    expect(provisioning?.wifiConfigured, isTrue);
    expect(provisioning?.sessionState, 'reauth_required');
    expect(provisioning?.droppedEvents, 4);
    expect(
      provisioning?.lastProvisionedAt,
      DateTime.parse('2026-10-03T09:58:00Z'),
    );
  });

  test('device status keeps old runtimes without provisioning', () async {
    final client = _RecordingApiClient();
    final api = DeviceBindingApi(client);

    final devices = await api.list();

    expect(devices.single.runtime, isNotNull);
    expect(devices.single.runtime?.provisioning, isNull);
  });

  test('device status tolerates invalid provisioning dates', () async {
    final client = _RecordingApiClient(
      provisioning: {
        'state': 'provisioning',
        'wifi_configured': false,
        'session_state': 'revoked',
        'last_provisioned_at': 'not-a-date',
      },
    );
    final api = DeviceBindingApi(client);

    final devices = await api.list();

    expect(devices.single.runtime?.provisioning?.lastProvisionedAt, isNull);
  });

  test('device status defaults missing dropped events for older firmware', () async {
    final client = _RecordingApiClient(
      provisioning: {
        'state': 'provisioned',
        'wifi_configured': true,
        'session_state': 'ready',
      },
    );
    final api = DeviceBindingApi(client);

    final devices = await api.list();

    expect(devices.single.runtime?.provisioning?.droppedEvents, 0);
  });
}

class _RecordingApiClient implements ApiClient {
  _RecordingApiClient({this.provisioning});

  final Map<String, Object?>? provisioning;
  String? path;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    this.path = path;
    return {
      'data': {
        'devices': [
          {
            'device_id': 'device_demo_001',
            'device_name': '初芽',
            'hardware_model': 'sprout_initial',
            'firmware_version': '0.3.0',
            'capabilities': ['display'],
            'bound_at': '2026-10-01T08:00:00Z',
            'updated_at': '2026-10-03T10:00:02Z',
            'runtime': {
              'is_online': true,
              'connection': {'state': 'online', 'transport': 'wifi'},
              'network_quality': {
                'level': 'good',
                'rssi_dbm': -58,
                'latency_ms': 42,
                'packet_loss_percent': 1,
              },
              'time_sync': {
                'state': 'synchronized',
                'source': 'sntp',
                'last_synced_at': '2026-10-03T10:00:00Z',
                'offset_ms': 12,
              },
              'offline': {
                'state': 'online',
                'reason': 'none',
                'fallback_active': false,
                'pending_telemetry': 0,
              },
              'reported_at': '2026-10-03T10:00:00Z',
              'received_at': '2026-10-03T10:00:02Z',
              if (provisioning != null) 'provisioning': provisioning,
            },
          },
        ],
      },
    };
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> delete(String path) {
    throw UnimplementedError();
  }
}
