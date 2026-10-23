import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../../family/application/family_controller.dart';
import '../data/video_session_api.dart';
import '../domain/video_session.dart';
import '../presentation/video_session_copy.dart';

/// Resolves the signed-in family before the video-call policy is addressed.
final videoSessionFamilyIdProvider = FutureProvider<String>((ref) async {
  final family = await ref.watch(familyControllerProvider.future);
  return family.capability.familyId;
});

/// Everything the 视频通话 screen renders.
///
/// The policy and the device list are read separately on purpose: a device list
/// that fails must not hide the settings the guardian came to change, and a
/// policy that fails must not leave a device status standing alone.
class VideoSessionState {
  const VideoSessionState({
    this.policy,
    this.qualities = VideoQualityFloor.values,
    this.devices = const <VideoSessionDevice>[],
    this.isLoading = false,
    this.isLoadingDevices = false,
    this.isSaving = false,
    this.error,
    this.devicesError,
    this.notice,
  });

  /// The stored policy, absent until the first read succeeded.
  final VideoSessionPolicy? policy;

  /// The bands the platform serves with the policy.
  final List<VideoQualityFloor> qualities;
  final List<VideoSessionDevice> devices;
  final bool isLoading;
  final bool isLoadingDevices;
  final bool isSaving;

  /// A failure that stopped the policy read. Without a policy there is nothing
  /// to edit, so the screen offers a retry instead of the controls.
  final Object? error;

  /// A failure that stopped the device list while the policy stayed readable.
  final Object? devicesError;

  /// The outcome of the last save, for the sentence under the button.
  final VideoSessionNotice? notice;

  bool get hasPolicy => policy != null;

  /// True when at least one device reported, which is when a status card has
  /// something true to say.
  bool get hasDeviceReport => devices.isNotEmpty;

  VideoSessionState copyWith({
    VideoSessionPolicy? policy,
    List<VideoQualityFloor>? qualities,
    List<VideoSessionDevice>? devices,
    bool? isLoading,
    bool? isLoadingDevices,
    bool? isSaving,
    Object? error,
    bool clearError = false,
    Object? devicesError,
    bool clearDevicesError = false,
    VideoSessionNotice? notice,
    bool clearNotice = false,
  }) {
    return VideoSessionState(
      policy: policy ?? this.policy,
      qualities: qualities ?? this.qualities,
      devices: devices ?? this.devices,
      isLoading: isLoading ?? this.isLoading,
      isLoadingDevices: isLoadingDevices ?? this.isLoadingDevices,
      isSaving: isSaving ?? this.isSaving,
      error: clearError ? null : (error ?? this.error),
      devicesError: clearDevicesError
          ? null
          : (devicesError ?? this.devicesError),
      notice: clearNotice ? null : (notice ?? this.notice),
    );
  }
}

/// What the screen says after a save.
enum VideoSessionNotice { saved, invalidPolicy, failed }

/// Reads and saves the family video-call policy and its device statuses.
///
/// Every read is addressed through the signed-in family, so the browser of the
/// screen never holds an owner id. A save always reloads the policy so the
/// revision shown is the one the platform stored, including a revision another
/// guardian published from a second phone at the same moment.
class VideoSessionController extends Notifier<VideoSessionState> {
  VideoSessionApi? _api;
  int _generation = 0;

  @override
  VideoSessionState build() {
    final provider = ref.watch(videoSessionApiProvider);
    final api = provider.value;
    if (provider.isLoading) {
      // Before the family is known there is nothing to ask for, so the screen
      // waits instead of requesting a path it knows would be refused.
      return const VideoSessionState(isLoading: true);
    }
    if (api == null) {
      // A failed family read is reported as itself: it is the reason there is
      // nothing to edit, and its own sentence is the actionable one.
      return VideoSessionState(isLoading: false, error: provider.error);
    }
    _api = api;
    _load(api);
    return const VideoSessionState(isLoading: true);
  }

  Future<void> _load(VideoSessionApi api) async {
    final generation = ++_generation;
    try {
      final snapshot = await api.fetchPolicy();
      if (generation != _generation) {
        return;
      }
      state = state.copyWith(
        policy: snapshot.policy,
        qualities: snapshot.qualities,
        isLoading: false,
        clearError: true,
      );
    } on Object catch (error) {
      if (generation != _generation) {
        return;
      }
      state = state.copyWith(isLoading: false, error: error);
      return;
    }
    await _loadDevices(api, generation);
  }

  Future<void> _loadDevices(VideoSessionApi api, int generation) async {
    state = state.copyWith(isLoadingDevices: true, clearDevicesError: true);
    try {
      final devices = await api.fetchDevices();
      if (generation != _generation) {
        return;
      }
      state = state.copyWith(devices: devices, isLoadingDevices: false);
    } on Object catch (error) {
      if (generation != _generation) {
        return;
      }
      state = state.copyWith(isLoadingDevices: false, devicesError: error);
    }
  }

  /// Reads the policy and the device statuses again, for the retry button.
  Future<void> refresh() async {
    final api = _api ?? ref.read(videoSessionApiProvider).value;
    if (api == null) {
      // No API means the family read is what failed, so that read is repeated
      // instead of the screen reloading itself into the same failure.
      ref.invalidate(videoSessionFamilyIdProvider);
      return;
    }
    _api = api;
    state = state.copyWith(isLoading: true, clearError: true, clearNotice: true);
    await _load(api);
  }

  /// Reads only the device statuses again, after a failed device read.
  Future<void> refreshDevices() async {
    final api = _api ?? ref.read(videoSessionApiProvider).value;
    if (api == null) {
      return;
    }
    await _loadDevices(api, _generation);
  }

  /// Stores one policy revision and returns whether it reached the platform.
  ///
  /// The draft is clamped to the platform's bounds before it is sent, because a
  /// refused save tells the guardian nothing they can act on while the controls
  /// already sit inside the allowed range.
  Future<bool> save(VideoSessionPolicy draft) async {
    final api = _api ?? ref.read(videoSessionApiProvider).value;
    if (api == null) {
      return false;
    }
    final sanitized = _clamped(draft);
    state = state.copyWith(isSaving: true, clearNotice: true);
    try {
      final saved = await api.publishPolicy(sanitized);
      if (!ref.mounted) {
        return false;
      }
      state = state.copyWith(policy: saved, isSaving: false);
      state = state.copyWith(notice: VideoSessionNotice.saved);
      // Binding a new revision can change which devices are out of date, so the
      // device card is read again instead of keeping the previous answer.
      await _loadDevices(api, _generation);
      return true;
    } on Object catch (error) {
      if (!ref.mounted) {
        return false;
      }
      state = state.copyWith(
        isSaving: false,
        notice: error is VideoSessionRefused &&
                error.refusal == VideoSessionRefusal.invalidPolicy
            ? VideoSessionNotice.invalidPolicy
            : VideoSessionNotice.failed,
      );
      return false;
    }
  }

  VideoSessionPolicy _clamped(VideoSessionPolicy draft) {
    return draft.copyWith(
      ringTimeoutSeconds: draft.ringTimeoutSeconds.clamp(
        VideoSessionPolicy.minimumRingTimeoutSeconds,
        VideoSessionPolicy.maximumRingTimeoutSeconds,
      ),
      maxSessionSeconds: draft.maxSessionSeconds.clamp(
        VideoSessionPolicy.minimumSessionSeconds,
        VideoSessionPolicy.maximumSessionSeconds,
      ),
    );
  }

  /// Maps a failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is VideoSessionRefused) {
      return error.message;
    }
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final videoSessionApiProvider = FutureProvider<VideoSessionApi>((ref) async {
  // The family is resolved first so a signed-out or family-less session fails
  // closed with the sign-in sentence instead of issuing a request that can only
  // be refused. The policy route itself is family-scoped through the session.
  final familyId = await ref.watch(videoSessionFamilyIdProvider.future);
  if (familyId.isEmpty) {
    throw const AppException(
      kind: AppErrorKind.unauthenticated,
      message: '登录已过期，请重新登录',
      retryable: false,
    );
  }
  return HttpVideoSessionApi(ref.watch(apiClientProvider));
});

final videoSessionControllerProvider =
    NotifierProvider<VideoSessionController, VideoSessionState>(
      VideoSessionController.new,
    );
