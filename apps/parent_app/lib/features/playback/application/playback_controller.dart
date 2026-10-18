import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/playback_api.dart';
import '../domain/playback_request.dart';

/// State rendered by the on-demand playback page.
class PlaybackState {
  const PlaybackState({required this.requests, this.deviceFilter});

  const PlaybackState.initial() : requests = const [], deviceFilter = null;

  final List<PlaybackRequest> requests;

  /// When set, the list is narrowed to one device.
  final String? deviceFilter;

  bool get isEmpty => requests.isEmpty;

  /// Requests for one device, preserving the newest-first server order.
  List<PlaybackRequest> forDevice(String deviceId) {
    return [
      for (final request in requests)
        if (request.deviceId == deviceId) request,
    ];
  }
}

/// Owns the family's on-demand playback requests so the page can refresh,
/// filter, queue, and cancel without holding a device session.
class PlaybackController extends AsyncNotifier<PlaybackState> {
  late PlaybackApi _api;
  String? _deviceFilter;

  @override
  Future<PlaybackState> build() async {
    _api = ref.read(playbackApiProvider);
    _deviceFilter = null;
    return _load();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_load);
  }

  /// Narrows the list to one device, or clears the filter with `null`.
  Future<void> filterByDevice(String? deviceId) async {
    final normalized = (deviceId != null && deviceId.isNotEmpty)
        ? deviceId
        : null;
    if (normalized == _deviceFilter) {
      return;
    }
    _deviceFilter = normalized;
    await refresh();
  }

  /// Queues one package to play on a bound device and reloads the list.
  Future<PlaybackRequest> requestPlayback({
    required String deviceId,
    required String packageId,
    String note = '',
  }) async {
    final created = await _api.createRequest(
      deviceId: deviceId,
      packageId: packageId,
      note: note,
    );
    await refresh();
    return created;
  }

  /// Cancels a pending request and reloads the list.
  Future<PlaybackRequest> cancelRequest(String requestId) async {
    final cancelled = await _api.cancelRequest(requestId);
    await refresh();
    return cancelled;
  }

  Future<PlaybackState> _load() async {
    final requests = await _api.fetchRequests(deviceId: _deviceFilter);
    return PlaybackState(requests: requests, deviceFilter: _deviceFilter);
  }

  /// Maps a failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final playbackApiProvider = Provider<PlaybackApi>((ref) {
  return HttpPlaybackApi(ref.read(apiClientProvider));
});

final playbackControllerProvider =
    AsyncNotifierProvider<PlaybackController, PlaybackState>(
      PlaybackController.new,
    );
