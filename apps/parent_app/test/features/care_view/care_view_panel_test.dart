import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:parent_app/core/theme/app_theme.dart';
import 'package:parent_app/features/care_view/application/care_view_controller.dart';
import 'package:parent_app/features/care_view/data/care_view_api.dart';
import 'package:parent_app/features/care_view/domain/care_view.dart';
import 'package:parent_app/features/care_view/presentation/care_view_copy.dart';
import 'package:parent_app/features/care_view/presentation/care_view_page.dart';
import 'package:parent_app/features/care_view/presentation/care_view_panel.dart';


/// The confirmation dialog's own start button.
///
/// The page and the dialog both say 开始查看, so the dialog button is located by
/// widget type inside the dialog rather than by the last text match, which
/// depends on traversal order between the page and the overlay.
final Finder _confirmStartButton = find
    .descendant(
      of: find.byType(AlertDialog),
      matching: find.widgetWithText(FilledButton, '开始查看'),
    )
    .first;

void main() {
  group('CareViewPanel', () {
    testWidgets('the notice is shown before anything is sent', (tester) async {
      final api = _FakeCareViewApi();
      await tester.pumpWidget(_app(api: api));
      await tester.pumpAndSettle();

      await tester.tap(find.text('开始查看'));
      await tester.pumpAndSettle();

      // The guardian must learn what the child will see and hear first.
      expect(find.text('开始查看设备画面'), findsOneWidget);
      expect(find.textContaining('会显示提示并播放提示音'), findsOneWidget);
      expect(find.textContaining('孩子可以按键随时关闭'), findsOneWidget);
      expect(api.startedDeviceIds, isEmpty);
    });

    testWidgets('declining the notice never asks the device', (tester) async {
      final api = _FakeCareViewApi();
      await tester.pumpWidget(_app(api: api));
      await tester.pumpAndSettle();

      await tester.tap(find.text('开始查看'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('先不查看'));
      await tester.pumpAndSettle();

      expect(api.startedDeviceIds, isEmpty);
      expect(find.text('开始查看'), findsOneWidget);
    });

    testWidgets('accepting the notice starts the look', (tester) async {
      final api = _FakeCareViewApi();
      await tester.pumpWidget(_app(api: api));
      await tester.pumpAndSettle();

      await tester.tap(find.text('开始查看'));
      await tester.pumpAndSettle();
      await tester.tap(_confirmStartButton);
      await tester.pumpAndSettle();

      expect(api.startedDeviceIds, <String>['device-1']);
      expect(find.text('正在等待设备确认'), findsWidgets);
      expect(find.text('结束查看'), findsOneWidget);
    });

    testWidgets('requested says the device has not confirmed yet', (
      tester,
    ) async {
      await tester.pumpWidget(_app(view: _view(CareViewState.requested)));
      await tester.pumpAndSettle();

      expect(find.text('正在等待设备确认'), findsWidgets);
      expect(find.textContaining('20 秒'), findsWidgets);
      expect(find.textContaining('设备显示提示后才会开始传送画面'), findsWidgets);
      // Nothing may look like a live picture while the device is unconfirmed.
      expect(find.byType(Image), findsNothing);
      expect(find.text('结束查看'), findsOneWidget);
    });

    testWidgets('streaming shows the countdown and the child reminder', (
      tester,
    ) async {
      await tester.pumpWidget(
        _app(view: _view(CareViewState.streaming, streamPath: '/relay/x')),
      );
      await tester.pumpAndSettle();

      expect(find.text('正在查看'), findsOneWidget);
      expect(find.textContaining('剩余 4:'), findsOneWidget);
      expect(find.text('结束查看'), findsOneWidget);
      expect(find.textContaining('孩子可以随时按键关闭'), findsOneWidget);
    });

    testWidgets('stream unavailable never becomes a black picture', (
      tester,
    ) async {
      await tester.pumpWidget(
        _app(
          view: _view(
            CareViewState.streaming,
            isStreamUnavailable: true,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('暂时无法传送画面'), findsWidgets);
      expect(find.byType(Image), findsNothing);
      expect(find.text('结束查看'), findsOneWidget);
    });

    testWidgets('a busy device explains itself in Chinese', (tester) async {
      final api = _FakeCareViewApi(
        refusal: const CareViewRefused(CareViewRefusal.deviceBusy),
      );
      await tester.pumpWidget(_app(api: api));
      await tester.pumpAndSettle();

      await tester.tap(find.text('开始查看'));
      await tester.pumpAndSettle();
      await tester.tap(_confirmStartButton);
      await tester.pumpAndSettle();

      expect(find.text('这台设备正在被查看，请稍后再试'), findsOneWidget);
    });

    testWidgets('a disabled feature points to settings', (tester) async {
      final api = _FakeCareViewApi(
        refusal: const CareViewRefused(CareViewRefusal.disabled),
      );
      await tester.pumpWidget(_app(api: api));
      await tester.pumpAndSettle();

      await tester.tap(find.text('开始查看'));
      await tester.pumpAndSettle();
      await tester.tap(_confirmStartButton);
      await tester.pumpAndSettle();

      expect(find.text('还没有开启查看功能，请先在设置里开启'), findsOneWidget);
    });

    testWidgets('a refusal carries no technical detail', (tester) async {
      final api = _FakeCareViewApi(
        refusal: const CareViewRefused(CareViewRefusal.rateLimited),
      );
      await tester.pumpWidget(_app(api: api));
      await tester.pumpAndSettle();

      await tester.tap(find.text('开始查看'));
      await tester.pumpAndSettle();
      await tester.tap(_confirmStartButton);
      await tester.pumpAndSettle();

      expect(find.text('查看太频繁了，请稍后再试'), findsOneWidget);
      expect(_findLeakedTerms(tester), isNull);
    });

    testWidgets('an unexpected failure stays calm and generic', (tester) async {
      final api = _FakeCareViewApi(
        refusal: DioException(
          requestOptions: RequestOptions(path: '/api/v1/families/f/devices/d'),
          type: DioExceptionType.connectionError,
          message: 'SocketException: connection refused (127.0.0.1:8081)',
        ),
      );
      await tester.pumpWidget(_app(api: api));
      await tester.pumpAndSettle();

      await tester.tap(find.text('开始查看'));
      await tester.pumpAndSettle();
      await tester.tap(_confirmStartButton);
      await tester.pumpAndSettle();

      expect(_findLeakedTerms(tester), isNull);
      expect(find.text('暂时无法开始查看，请稍后再试'), findsOneWidget);
    });

    testWidgets('an ended look explains the five minute limit', (tester) async {
      await tester.pumpWidget(
        _app(view: _view(CareViewState.ended, finishReason: 'expired')),
      );
      await tester.pumpAndSettle();

      expect(find.text('查看已结束'), findsWidgets);
      expect(find.textContaining('一次查看最多 5 分钟'), findsOneWidget);
      expect(find.text('结束查看'), findsNothing);
    });

    testWidgets('reaching the deadline ends the look and stops the picture', (
      tester,
    ) async {
      final api = _FakeCareViewApi();
      await tester.pumpWidget(
        _app(
          api: api,
          view: _view(
            CareViewState.streaming,
            streamPath: '/relay/x',
            // A few seconds left: the window must stop itself instead of
            // waiting for the platform to say so.
            deadline: DateTime.now().toUtc().add(
              const Duration(seconds: 3),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('正在查看'), findsOneWidget);

      await tester.pump(const Duration(seconds: 4));
      await tester.pumpAndSettle();

      expect(find.text('查看已结束'), findsWidgets);
      expect(find.textContaining('一次查看最多 5 分钟'), findsOneWidget);
      expect(find.text('结束查看'), findsNothing);
    });

    testWidgets('ending a look calls the platform explicitly', (tester) async {
      final api = _FakeCareViewApi();
      await tester.pumpWidget(
        _app(api: api, view: _view(CareViewState.streaming)),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.text('结束查看'));
      await tester.pumpAndSettle();

      expect(api.endedViewIds, <String>['view-1']);
    });

    testWidgets('declined and failed states each say what happened', (
      tester,
    ) async {
      await tester.pumpWidget(_app(view: _view(CareViewState.declined)));
      await tester.pumpAndSettle();
      expect(find.text('设备这次没有开始传送'), findsWidgets);
      expect(find.textContaining('孩子'), findsWidgets);

      // A second ProviderScope of the same type reuses the element, so the
      // previous container would answer again. Unmounting first keeps the two
      // states independent.
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pumpWidget(
        _app(view: _view(CareViewState.failed, finishReason: 'request_timeout')),
      );
      await tester.pumpAndSettle();
      expect(find.text('这次查看没有开始'), findsWidgets);
      expect(find.textContaining('20 秒'), findsWidgets);
      // The failure says why nothing was sent; it does not repeat the waiting
      // copy, which would describe a state that is already over.
      expect(find.textContaining('没有确认'), findsWidgets);
    });

    testWidgets('a look the child stopped is named as such', (tester) async {
      await tester.pumpWidget(
        _app(view: _view(CareViewState.ended, finishReason: 'child_closed')),
      );
      await tester.pumpAndSettle();

      expect(find.text('孩子关闭了这次查看'), findsWidgets);
    });
  });

  group('CareViewPage', () {
    testWidgets('leaving the page ends an open look', (tester) async {
      final api = _FakeCareViewApi();
      await tester.pumpWidget(_page(api: api, view: _view(CareViewState.streaming)));
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('返回'));
      await tester.pumpAndSettle();

      expect(api.endedViewIds, <String>['view-1']);
    });

    testWidgets('leaving the page never ends a finished look twice', (
      tester,
    ) async {
      final api = _FakeCareViewApi();
      await tester.pumpWidget(
        _page(api: api, view: _view(CareViewState.ended)),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('返回'));
      await tester.pumpAndSettle();

      expect(api.endedViewIds, isEmpty);
    });
  });

  group('CareView model', () {
    test('a view without a picture path cannot show a picture', () {
      expect(_view(CareViewState.streaming).canShowPicture, isFalse);
    });

    test('an unavailable picture cannot be shown either', () {
      final view = _view(
        CareViewState.streaming,
        streamPath: '/relay/x',
        isStreamUnavailable: true,
      );
      expect(view.canShowPicture, isFalse);
    });

    test('an unknown state never counts as streaming', () {
      final now = DateTime.now().toUtc().toIso8601String();
      final view = CareView.fromJson(<String, Object?>{
        'session_id': 'view-1',
        'state': 'something_new',
        'started_at': now,
        'deadline': now,
      });
      expect(view.state, CareViewState.unknown);
      expect(view.canShowPicture, isFalse);
    });
  });
}

/// Returns the first technical term found on screen, or null when clean.
String? _findLeakedTerms(WidgetTester tester) {
  const banned = <String>[
    'API',
    'token',
    'Token',
    'WebSocket',
    'websocket',
    'session',
    'Session',
    'status',
    'socket',
    'Socket',
    '127.0.0.1',
    'care_view',
    '[object Object]',
    'undefined',
    'null',
    'NaN',
    'exception',
    'Exception',
  ];
  final texts = tester
      .widgetList<Text>(find.byType(Text))
      .map((text) => text.data ?? text.textSpan?.toPlainText() ?? '')
      .toList(growable: false);
  for (final text in texts) {
    for (final word in banned) {
      if (text.contains(word)) {
        return word;
      }
    }
  }
  return null;
}

CareView _view(
  CareViewState state, {
  String streamPath = '',
  bool isStreamUnavailable = false,
  String finishReason = '',
  DateTime? deadline,
}) {
  final now = DateTime.now().toUtc();
  final isOpen =
      state == CareViewState.requested || state == CareViewState.streaming;
  return CareView(
    viewId: 'view-1',
    deviceId: 'device-1',
    state: state,
    startedAt: now,
    deadline: deadline ?? now.add(const Duration(minutes: 5)),
    finishedAt: isOpen ? null : now,
    finishReason: finishReason,
    allowsFrames: state == CareViewState.streaming,
    streamPath: streamPath,
    isStreamUnavailable: isStreamUnavailable,
  );
}

Widget _app({CareView? view, _FakeCareViewApi? api}) {
  return _scope(
    view: view,
    api: api ?? _FakeCareViewApi(),
    child: MaterialApp(
      theme: AppTheme.light,
      home: Scaffold(
        body: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: const CareViewPanel(deviceId: 'device-1', deviceName: '初芽'),
        ),
      ),
    ),
  );
}

/// The real page inside a router, because leaving a page is router navigation.
Widget _page({CareView? view, _FakeCareViewApi? api}) {
  final router = GoRouter(
    initialLocation: '/devices/device-1/care-view',
    routes: <RouteBase>[
      GoRoute(
        path: '/devices',
        builder: (context, state) => const Scaffold(
          body: Text('设备列表占位'),
        ),
      ),
      GoRoute(
        path: '/devices/:deviceId/care-view',
        builder: (context, state) => const CareViewPage(
          deviceId: 'device-1',
          deviceName: '初芽',
        ),
      ),
    ],
  );
  return _scope(
    view: view,
    api: api ?? _FakeCareViewApi(),
    child: MaterialApp.router(
      theme: AppTheme.light,
      routerConfig: router,
    ),
  );
}

Widget _scope({
  required CareView? view,
  required _FakeCareViewApi api,
  required Widget child,
}) {
  // A refresh must report the same window the seeded view opened with, so the
  // deadline the test chose is the one the window actually honours.
  api.fetchView = view;
  return ProviderScope(
    overrides: [
      careViewApiProvider.overrideWithValue(<CareViewApi>[api][0] as CareViewApi?),
      // The controller resolves the family before every call; without this the
      // lookup never completes and no request is ever made.
      careViewFamilyIdProvider.overrideWith((ref) async => 'family-1'),
      // Pictures are covered at the boundary; the window is tested without
      // opening a connection.
      careViewStreamOpenerProvider.overrideWithValue(
        (request) => throw StateError('no stream in widget tests'),
      ),
      careViewControllerProvider('device-1').overrideWith(() {
        return _SeededController(view, deviceId: 'device-1');
      }),
    ],
    child: child is MaterialApp ? child : MaterialApp(theme: AppTheme.light, home: child),
  );
}

/// The production controller with a starting view instead of an empty one.
class _SeededController extends CareViewController {
  _SeededController(this.initial, {required String deviceId})
    : super(deviceId);

  final CareView? initial;

  @override
  CareViewUiState get initialState =>
      initial == null ? const CareViewUiState() : CareViewUiState(view: initial);
}

class _FakeCareViewApi implements CareViewApi {
  _FakeCareViewApi({this.refusal});

  final Object? refusal;
  /// The view a refresh reports, so the deadline stays whatever it started as.
  CareView? fetchView;
  final List<String> startedDeviceIds = <String>[];
  final List<String> endedViewIds = <String>[];

  @override
  Future<CareView> start(String deviceId) async {
    if (refusal != null) {
      throw refusal!;
    }
    startedDeviceIds.add(deviceId);
    return _view(CareViewState.requested);
  }

  @override
  Future<CareView> fetch(String viewId) async {
    // A refresh reports the same window the look opened with; inventing a fresh
    // five minutes here would hide a controller that never ends on its own.
    return fetchView ?? _view(CareViewState.streaming, streamPath: '/relay/x');
  }

  @override
  Future<CareView> end(String viewId) async {
    endedViewIds.add(viewId);
    return _view(CareViewState.ended);
  }

  @override
  Future<List<CareView>> recent({int limit = 20}) async => <CareView>[];
}
