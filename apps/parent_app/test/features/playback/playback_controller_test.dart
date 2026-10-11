import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/playback/application/playback_controller.dart';
import 'package:parent_app/features/playback/data/playback_api.dart';
import 'package:parent_app/features/playback/domain/playback_request.dart';

void main() {
  test('loads the family requests on first build', () async {
    final api = _FakePlaybackApi(
      requests: [
        _request('req-1', 'device-1', PlaybackStatus.pending),
        _request('req-2', 'device-2', PlaybackStatus.completed),
      ],
    );
    final container = ProviderContainer(
      overrides: [playbackApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(playbackControllerProvider.future);

    expect(state.requests, hasLength(2));
    expect(state.deviceFilter, isNull);
    expect(state.forDevice('device-1'), hasLength(1));
    expect(api.lastDeviceFilter, isNull);
  });

  test('filters by device and clears the filter again', () async {
    final api = _FakePlaybackApi(
      requests: [
        _request('req-1', 'device-1', PlaybackStatus.pending),
        _request('req-2', 'device-2', PlaybackStatus.pending),
      ],
    );
    final container = ProviderContainer(
      overrides: [playbackApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(playbackControllerProvider.future);
    final controller = container.read(playbackControllerProvider.notifier);

    await controller.filterByDevice('device-1');
    expect(api.lastDeviceFilter, 'device-1');
    expect(
      container.read(playbackControllerProvider).value?.requests,
      hasLength(1),
    );

    // Filtering by the same device is a no-op.
    final before = api.listCalls;
    await controller.filterByDevice('device-1');
    expect(api.listCalls, before);

    await controller.filterByDevice(null);
    expect(api.lastDeviceFilter, isNull);
    expect(
      container.read(playbackControllerProvider).value?.requests,
      hasLength(2),
    );
  });

  test('queues a request then reloads the list', () async {
    final api = _FakePlaybackApi(requests: const []);
    final container = ProviderContainer(
      overrides: [playbackApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(playbackControllerProvider.future);
    final controller = container.read(playbackControllerProvider.notifier);

    final created = await controller.requestPlayback(
      deviceId: 'device-1',
      packageId: 'lesson_story',
      note: '睡前播放',
    );

    expect(created.status, PlaybackStatus.pending);
    expect(
      container.read(playbackControllerProvider).value?.requests,
      hasLength(1),
    );
    expect(api.lastCreatedDeviceId, 'device-1');
    expect(api.lastCreatedPackageId, 'lesson_story');
  });

  test('cancels a pending request then reloads', () async {
    final api = _FakePlaybackApi(
      requests: [_request('req-1', 'device-1', PlaybackStatus.pending)],
    );
    final container = ProviderContainer(
      overrides: [playbackApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(playbackControllerProvider.future);
    final controller = container.read(playbackControllerProvider.notifier);

    final cancelled = await controller.cancelRequest('req-1');

    expect(cancelled.status, PlaybackStatus.cancelled);
    expect(api.cancelledRequestIds, ['req-1']);
    expect(
      container.read(playbackControllerProvider).value?.requests.single.status,
      PlaybackStatus.cancelled,
    );
  });

  test('propagates a load failure as an error state', () async {
    final api = _FakePlaybackApi(requests: const [], failLoad: true);
    final container = ProviderContainer(
      overrides: [playbackApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final subscription = container.listen(
      playbackControllerProvider,
      (_, _) {},
    );
    addTearDown(subscription.close);
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(container.read(playbackControllerProvider).hasError, isTrue);
  });
}

PlaybackRequest _request(String id, String deviceId, PlaybackStatus status) {
  return PlaybackRequest(
    id: id,
    deviceId: deviceId,
    packageId: 'lesson_story',
    source: 'guardian',
    status: status,
    note: '',
    requestedAt: DateTime.utc(2026, 10, 11, 9),
    acknowledgedAt: null,
    completedAt: null,
    outcome: '',
  );
}

class _FakePlaybackApi implements PlaybackApi {
  _FakePlaybackApi({required List<PlaybackRequest> requests, this.failLoad = false})
    : _requests = [...requests];

  final List<PlaybackRequest> _requests;
  final bool failLoad;
  int listCalls = 0;
  String? lastDeviceFilter;
  String? lastCreatedDeviceId;
  String? lastCreatedPackageId;
  final List<String> cancelledRequestIds = [];
  int _sequence = 0;

  @override
  Future<List<PlaybackRequest>> fetchRequests({
    String? deviceId,
    String? status,
  }) async {
    listCalls += 1;
    lastDeviceFilter = deviceId;
    if (failLoad) {
      throw Exception('network down');
    }
    if (deviceId == null || deviceId.isEmpty) {
      return List<PlaybackRequest>.unmodifiable(_requests);
    }
    return List<PlaybackRequest>.unmodifiable(
      _requests.where((request) => request.deviceId == deviceId),
    );
  }

  @override
  Future<PlaybackRequest> createRequest({
    required String deviceId,
    required String packageId,
    String note = '',
  }) async {
    _sequence += 1;
    lastCreatedDeviceId = deviceId;
    lastCreatedPackageId = packageId;
    final request = PlaybackRequest(
      id: 'req-new-$_sequence',
      deviceId: deviceId,
      packageId: packageId,
      source: 'guardian',
      status: PlaybackStatus.pending,
      note: note,
      requestedAt: DateTime.utc(2026, 10, 11, 9),
      acknowledgedAt: null,
      completedAt: null,
      outcome: '',
    );
    _requests.insert(0, request);
    return request;
  }

  @override
  Future<PlaybackRequest> cancelRequest(String requestId) async {
    cancelledRequestIds.add(requestId);
    final index = _requests.indexWhere((request) => request.id == requestId);
    if (index < 0) {
      throw Exception('missing request');
    }
    final existing = _requests[index];
    final cancelled = PlaybackRequest(
      id: existing.id,
      deviceId: existing.deviceId,
      packageId: existing.packageId,
      source: existing.source,
      status: PlaybackStatus.cancelled,
      note: existing.note,
      requestedAt: existing.requestedAt,
      acknowledgedAt: existing.acknowledgedAt,
      completedAt: existing.completedAt,
      outcome: existing.outcome,
    );
    _requests[index] = cancelled;
    return cancelled;
  }
}
