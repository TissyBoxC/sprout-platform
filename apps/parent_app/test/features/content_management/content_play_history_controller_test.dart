import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/content_management/application/content_library_controller.dart';
import 'package:parent_app/features/content_management/application/content_play_history_controller.dart';
import 'package:parent_app/features/content_management/data/content_management_api.dart';
import 'package:parent_app/features/content_management/domain/content_package.dart';

void main() {
  test('loads the merged history on first build', () async {
    final api = _FakeContentManagementApi(
      history: [
        _record('play-1', 'sprout_device_001', seconds: 30),
        _record('play-2', 'sprout_device_002', seconds: 60),
      ],
    );
    final container = ProviderContainer(
      overrides: [contentManagementApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(
      contentPlayHistoryControllerProvider.future,
    );

    expect(state.records, hasLength(2));
    expect(state.deviceIds, ['sprout_device_001', 'sprout_device_002']);
    expect(state.deviceFilter, isNull);
    expect(api.lastDeviceFilter, isNull);
  });

  test('filters the history to one device and clears the filter again',
      () async {
    final api = _FakeContentManagementApi(
      history: [_record('play-1', 'sprout_device_001', seconds: 30)],
    );
    final container = ProviderContainer(
      overrides: [contentManagementApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(contentPlayHistoryControllerProvider.future);
    final controller = container.read(
      contentPlayHistoryControllerProvider.notifier,
    );

    await controller.filterByDevice('sprout_device_001');
    expect(api.lastDeviceFilter, 'sprout_device_001');
    expect(
      container.read(contentPlayHistoryControllerProvider).value?.deviceFilter,
      'sprout_device_001',
    );

    // Filtering by the same device is a no-op: no extra request is sent.
    final before = api.calls;
    await controller.filterByDevice('sprout_device_001');
    expect(api.calls, before);

    await controller.filterByDevice(null);
    expect(api.lastDeviceFilter, isNull);
    expect(
      container.read(contentPlayHistoryControllerProvider).value?.deviceFilter,
      isNull,
    );
  });

  test('propagates a load failure as an error state', () async {
    final api = _FakeContentManagementApi(
      history: const [],
      failHistory: true,
    );
    final container = ProviderContainer(
      overrides: [contentManagementApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    // Keep a subscription alive so the provider stays mounted while it fails.
    final subscription = container.listen(
      contentPlayHistoryControllerProvider,
      (_, _) {},
    );
    addTearDown(subscription.close);
    // Let the async build settle into the error state.
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(container.read(contentPlayHistoryControllerProvider).hasError, isTrue);
  });
}

ContentPlayRecord _record(String id, String deviceId, {required int seconds}) {
  return ContentPlayRecord(
    id: id,
    deviceId: deviceId,
    packageId: 'story_001',
    packageVersion: 1,
    outcome: PlayOutcome.completed,
    playedSeconds: seconds,
    playedAt: DateTime.utc(2026, 10, 9, 12),
  );
}

class _FakeContentManagementApi implements ContentManagementApi {
  _FakeContentManagementApi({
    required List<ContentPlayRecord> history,
    this.failHistory = false,
  }) : _history = [...history];

  final List<ContentPlayRecord> _history;
  final bool failHistory;
  int calls = 0;
  String? lastDeviceFilter;

  @override
  Future<List<ContentPlayRecord>> fetchPlayHistory({String? deviceId}) async {
    calls += 1;
    lastDeviceFilter = deviceId;
    if (failHistory) {
      throw Exception('network down');
    }
    if (deviceId == null) {
      return List<ContentPlayRecord>.unmodifiable(_history);
    }
    return List<ContentPlayRecord>.unmodifiable(
      _history.where((record) => record.deviceId == deviceId),
    );
  }

  @override
  Future<ContentCatalog> fetchCatalog({
    required int sinceRevision,
    ContentAgeTier? ageTier,
    ContentCategory? category,
  }) {
    throw UnimplementedError();
  }

  @override
  Future<ContentDownloadSpec> fetchDownloadSpec(String packageId) {
    throw UnimplementedError();
  }

  @override
  Future<List<ContentFavorite>> fetchFavorites() async =>
      const <ContentFavorite>[];

  @override
  Future<void> addFavorite(String packageId) async {}

  @override
  Future<void> removeFavorite(String packageId) async {}

  @override
  Future<List<ContentThemePack>> fetchThemePacks() async =>
      const <ContentThemePack>[];
}
