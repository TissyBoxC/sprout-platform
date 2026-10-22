import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../../../core/storage/secure_store.dart';
import '../../family/application/family_controller.dart';
import '../data/care_view_api.dart';
import '../domain/care_view.dart';

/// Resolves the signed-in family before a live view can be addressed.
final careViewFamilyIdProvider = FutureProvider<String>((ref) async {
  final family = await ref.watch(familyControllerProvider.future);
  return family.capability.familyId;
});

final careViewApiProvider = Provider<CareViewApi?>((ref) {
  final familyId = ref.watch(careViewFamilyIdProvider).value;
  if (familyId == null || familyId.isEmpty) {
    return null;
  }
  return HttpCareViewApi(ref.watch(apiClientProvider), familyId);
});

/// What the parent app needs to attach to the picture stream of one view.
class CareViewStreamRequest {
  const CareViewStreamRequest({
    required this.streamPath,
    required this.deviceId,
  });

  final String streamPath;
  final String deviceId;
}

/// Opens the picture stream for one live view.
///
/// The relay only attaches a stream the device already started, so this is used
/// once the platform reports streaming. The guardian's own credential travels
/// in the upgrade header rather than in the address, so a shared or pasted
/// address never carries it.
typedef CareViewStreamOpener =
    Future<WebSocketChannel> Function(CareViewStreamRequest request);

final careViewStreamOpenerProvider = Provider<CareViewStreamOpener>((ref) {
  final apiBaseUrl = ref.watch(appConfigProvider).apiBaseUrl;
  final sessionStore = SessionStore(ref.watch(secureStoreProvider));
  return (request) async {
    final path = request.streamPath.trim();
    if (path.isEmpty) {
      throw const FormatException('care view stream path is empty');
    }
    final endpoint = apiBaseUrl.resolve(path);
    // Pictures of a child may only travel over an encrypted connection.
    if (endpoint.scheme != 'https' && endpoint.scheme != 'wss') {
      throw const AppException(
        kind: AppErrorKind.network,
        message: '网络连接不稳定，请检查后重试',
        retryable: true,
      );
    }
    final session = await sessionStore.read();
    return IOWebSocketChannel.connect(
      endpoint.replace(
        scheme: 'wss',
        queryParameters: <String, String>{'device_id': request.deviceId},
      ),
      headers: <String, dynamic>{
        'Authorization': 'Bearer ${session?.accessToken ?? ''}',
      },
      connectTimeout: const Duration(seconds: 10),
      // The relay answers a keepalive on a fixed interval, so a silently dead
      // picture is detected instead of being shown as still live.
      pingInterval: const Duration(seconds: 25),
    );
  };
});

/// What the guardian's live view currently shows.
class CareViewUiState {
  const CareViewUiState({
    this.view,
    this.latestFrame,
    this.framesReceived = 0,
    this.isLoading = false,
    this.error,
  });

  /// The view as the platform last reported it, or null before one is asked.
  final CareView? view;

  /// The most recent picture, or null before the first one arrives.
  final Uint8List? latestFrame;
  final int framesReceived;

  /// True while the guardian's request is being answered.
  final bool isLoading;

  /// A refusal or a transport failure, already safe to show.
  final Object? error;

  bool get hasView => view != null;

  CareViewUiState copyWith({
    CareView? view,
    bool clearView = false,
    Uint8List? latestFrame,
    bool clearFrame = false,
    int? framesReceived,
    bool? isLoading,
    Object? error,
    bool clearError = false,
  }) {
    return CareViewUiState(
      view: clearView ? null : (view ?? this.view),
      latestFrame: clearFrame ? null : (latestFrame ?? this.latestFrame),
      framesReceived: framesReceived ?? this.framesReceived,
      isLoading: isLoading ?? this.isLoading,
      error: clearError ? null : (error ?? this.error),
    );
  }
}

/// Drives one live look from the guardian's side.
///
/// The controller follows one view from requested to finished, so a device that
/// accepts, refuses, or ends the look shows up on the parent screen without the
/// guardian refreshing. The deadline is enforced locally too: when the clock
/// passes it the view is ended explicitly instead of waiting to be told.
///
/// State is mutable and read with `.value` in the interface; the family keeps
/// one controller per device for the whole session so a half-finished look is
/// never silently restarted.
class CareViewController extends Notifier<CareViewUiState> {
  CareViewController(this.deviceId);

  final String deviceId;
  Timer? _poll;
  Timer? _deadline;
  DateTime? _deadlineFor;
  bool _isStreamOpened = false;
  WebSocketChannel? _stream;

  /// True while this controller is attached to the provider, so a timer that
  /// fires after the screen closed cannot touch a disposed notifier.
  bool _isMounted = false;

  @override
  CareViewUiState build() {
    _isMounted = true;
    ref.onDispose(() {
      _isMounted = false;
      _stopFollowing();
    });
    final initial = initialState;
    // A view that is already open when this controller is built (for example
    // after the window is rebuilt) still has to police its own deadline, so the
    // clock is started here rather than only on a fresh start.
    if (initial.view != null && !initial.view!.isFinished) {
      _schedulePoll();
      _scheduleDeadline(initial.view!);
    }
    return initial;
  }

  /// The state this controller starts with. Subclasses reopen a view by
  /// overriding this instead of [build].
  CareViewUiState get initialState => const CareViewUiState();

  /// Asks the device and starts following the answer.
  ///
  /// Throws the translated failure when the platform refuses, so the caller can
  /// show the reason while the window stays on its idle card.
  Future<CareView> start() async {
    final api = await _api();
    state = state.copyWith(isLoading: true, clearError: true);
    try {
      final view = await api.start(deviceId);
      _show(view);
      return view;
    } catch (error) {
      state = state.copyWith(isLoading: false, error: error);
      rethrow;
    }
  }

  /// Ends an open view. Safe to call twice: a finished view is kept as is.
  ///
  /// The platform call is attempted even past the local deadline, because the
  /// stored view is what actually stops the camera.
  Future<void> end() async {
    final current = state.view;
    if (current == null || current.isFinished) {
      _stopFollowing();
      return;
    }
    _stopFollowing();
    final api = await _api();
    try {
      _show(await api.end(current.viewId));
    } catch (_) {
      // The guardian asked to stop. A failure notice here would suggest the
      // camera might still be on, so the window states the view is over.
      _show(current.expiringAt(DateTime.now().toUtc()));
    }
  }

  /// Reads the latest state once, for a guardian who wants to retry.
  Future<void> refresh() async {
    final current = state.view;
    if (current == null || current.isFinished) {
      return;
    }
    final api = await _api();
    try {
      _show(await api.fetch(current.viewId));
    } catch (error) {
      state = state.copyWith(error: error);
    }
  }

  /// Clears a finished view so the entry card returns to its idle state.
  void reset() {
    _stopFollowing();
    state = const CareViewUiState();
  }

  /// Opens the picture stream once. Repeated calls are ignored.
  void openStream() {
    if (_isStreamOpened || _stream != null) {
      return;
    }
    _isStreamOpened = true;
    unawaited(_connect());
  }

  Future<void> _connect() async {
    final current = state.view;
    if (current == null || !current.canShowPicture) {
      return;
    }
    try {
      final channel = await ref.read(careViewStreamOpenerProvider)(
        CareViewStreamRequest(
          streamPath: current.streamPath,
          deviceId: current.deviceId,
        ),
      );
      _stream = channel;
      channel.stream.listen(
        _onPicture,
        onDone: _detachStream,
        onError: (Object _) => _detachStream(),
        cancelOnError: true,
      );
    } on Object {
      // A picture stream that will not open is reported as an unavailable
      // picture by the window, not as a broken view.
      _detachStream();
    }
  }

  void _onPicture(Object? message) {
    if (message is! List<int> || message.isEmpty) {
      return;
    }
    final current = state.view;
    // A picture that arrives after the look stopped is dropped rather than
    // shown: a last frame would look like the camera is still running.
    if (!_isMounted || current == null || !current.canShowPicture) {
      return;
    }
    state = state.copyWith(
      latestFrame: Uint8List.fromList(message),
      framesReceived: state.framesReceived + 1,
    );
  }

  void _detachStream() {
    final channel = _stream;
    _stream = null;
    if (_isMounted) {
      state = state.copyWith(clearFrame: true, framesReceived: 0);
    }
    if (channel == null) {
      return;
    }
    unawaited(channel.sink.close());
  }

  void _show(CareView view) {
    final previous = state.view;
    // A refresh may not extend the window. The guardian was promised a bounded
    // look, and a platform value further out would quietly keep the camera on.
    final bounded = previous == null ? view : view.cappedAt(previous.deadline);
    state = CareViewUiState(
      view: bounded,
      isLoading: state.isLoading,
    );
    if (bounded.isFinished || bounded.isExpiredAt(DateTime.now().toUtc())) {
      _stopFollowing();
      return;
    }
    _schedulePoll();
    _scheduleDeadline(bounded);
  }

  void _schedulePoll() {
    _poll?.cancel();
    // Two seconds is fast enough for the device to appear to answer at once and
    // slow enough that a five-minute look costs a handful of reads.
    _poll = Timer.periodic(const Duration(seconds: 2), (timer) async {
      final current = state.view;
      if (current == null || current.isFinished) {
        timer.cancel();
        return;
      }
      final api = ref.read(careViewApiProvider);
      if (api == null) {
        return;
      }
      // A failed read must not clear the screen: the view may still be live,
      // and showing it as over would invite the guardian to look again.
      try {
        final updated = await api.fetch(current.viewId);
        if (!_isMounted) {
          return;
        }
        _show(updated);
      } catch (_) {
        return;
      }
    });
  }

  void _scheduleDeadline(CareView view) {
    // A refresh that reports the same deadline must not restart the clock: doing
    // so would push the end out by one poll interval every time and a bounded
    // look would never finish on its own.
    if (_deadline != null && view.deadline == _deadlineFor) {
      return;
    }
    _deadline?.cancel();
    _deadlineFor = view.deadline;
    final remaining = view.deadline.difference(DateTime.now().toUtc());
    _deadline = Timer(remaining.isNegative ? Duration.zero : remaining, () {
      final current = state.view;
      if (current == null || current.isFinished) {
        return;
      }
      // Past the deadline no picture may be shown, even if the platform has
      // not answered yet, so the last picture is dropped here.
      _stopFollowing();
      _show(current.expiringAt(DateTime.now().toUtc()));
    });
  }

  void _stopFollowing() {
    _poll?.cancel();
    _deadline?.cancel();
    _deadlineFor = null;
    _detachStream();
  }

  Future<CareViewApi> _api() async {
    await ref.read(careViewFamilyIdProvider.future);
    final api = ref.read(careViewApiProvider);
    if (api == null) {
      throw StateError('care view api is not ready');
    }
    return api;
  }
}

final careViewControllerProvider =
    NotifierProvider.family<CareViewController, CareViewUiState, String>(
      CareViewController.new,
    );

/// Recent looks for the family, newest first.
final careViewHistoryProvider = FutureProvider<List<CareView>>((ref) async {
  await ref.watch(careViewFamilyIdProvider.future);
  final api = ref.watch(careViewApiProvider);
  if (api == null) {
    return const <CareView>[];
  }
  return api.recent();
});
