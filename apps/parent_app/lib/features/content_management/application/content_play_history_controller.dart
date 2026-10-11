import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../data/content_management_api.dart';
import '../domain/content_package.dart';
import 'content_library_controller.dart' show contentManagementApiProvider;

/// State rendered by the guardian playback history view.
class ContentPlayHistoryState {
  const ContentPlayHistoryState({
    required this.records,
    this.deviceFilter,
  });

  const ContentPlayHistoryState.initial()
    : records = const [],
      deviceFilter = null;

  final List<ContentPlayRecord> records;

  /// When set, the list is narrowed to one owned device.
  final String? deviceFilter;

  bool get isEmpty => records.isEmpty;

  /// Distinct device ids present in the current result, newest first.
  List<String> get deviceIds {
    final seen = <String>{};
    final ids = <String>[];
    for (final record in records) {
      if (seen.add(record.deviceId)) {
        ids.add(record.deviceId);
      }
    }
    return ids;
  }
}

/// Owns the guardian playback history so the view can refresh and filter
/// without holding a device session.
class ContentPlayHistoryController
    extends AsyncNotifier<ContentPlayHistoryState> {
  late final ContentManagementApi _api;
  String? _deviceFilter;

  @override
  Future<ContentPlayHistoryState> build() async {
    _api = ref.read(contentManagementApiProvider);
    return _load(const ContentPlayHistoryState.initial());
  }

  Future<void> refresh() async {
    final current = state.value ?? const ContentPlayHistoryState.initial();
    state = await AsyncValue.guard(() => _load(current));
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
    final current = state.value ?? const ContentPlayHistoryState.initial();
    await refresh();
    // Keep the filter on the rendered state even when the reload errored.
    final latest = state.value ?? current;
    state = AsyncData(
      ContentPlayHistoryState(
        records: latest.records,
        deviceFilter: normalized,
      ),
    );
  }

  Future<ContentPlayHistoryState> _load(
    ContentPlayHistoryState current,
  ) async {
    final records = await _api.fetchPlayHistory(deviceId: _deviceFilter);
    return ContentPlayHistoryState(
      records: records,
      deviceFilter: _deviceFilter,
    );
  }

  /// Maps a load failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final contentPlayHistoryControllerProvider =
    AsyncNotifierProvider<
      ContentPlayHistoryController,
      ContentPlayHistoryState
    >(ContentPlayHistoryController.new);
