import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/video_companion/application/video_companion_controller.dart';
import 'package:parent_app/features/video_companion/data/video_companion_api.dart';
import 'package:parent_app/features/video_companion/domain/video_companion.dart';
import 'package:parent_app/features/video_companion/presentation/video_companion_page.dart';

void main() {
  testWidgets('shows contacts and recent calls', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          videoCompanionApiProvider.overrideWithValue(_FakeVideoApi()),
        ],
        child: const MaterialApp(home: VideoCompanionPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('视频陪伴'), findsOneWidget);
    expect(find.text('妈妈'), findsOneWidget);
    expect(find.text('最近通话'), findsOneWidget);
    expect(find.text('已结束'), findsOneWidget);
    expect(find.text('画质良好 · 已挂断'), findsOneWidget);
    await tester.pumpAndSettle();
  });

  testWidgets('ends an active call through the controller', (tester) async {
    final api = _FakeVideoApi(activeCall: true);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [videoCompanionApiProvider.overrideWithValue(api)],
        child: const MaterialApp(home: VideoCompanionPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('通话中'), findsOneWidget);
    await tester.tap(find.text('结束'));
    await tester.pumpAndSettle();

    expect(api.endedCallId, 'call-1');
  });
}

class _FakeVideoApi implements VideoCompanionApi {
  _FakeVideoApi({this.activeCall = false});

  final bool activeCall;
  String? endedCallId;

  @override
  Future<List<VideoContact>> fetchContacts() async {
    return const <VideoContact>[
      VideoContact(
        id: 'contact-1',
        childId: '',
        displayName: '妈妈',
        role: 'guardian',
        enabled: true,
      ),
    ];
  }

  @override
  Future<List<VideoCall>> fetchHistory() async {
    return <VideoCall>[
      VideoCall(
        id: 'call-1',
        deviceId: 'device-1',
        contactId: 'contact-1',
        initiator: 'guardian',
        state: activeCall ? 'active' : 'ended',
        quality: 'good',
        endReason: activeCall ? '' : 'hangup',
      ),
    ];
  }

  @override
  Future<VideoContact> createContact(VideoContact contact) async => contact;

  @override
  Future<VideoContact> updateContact(VideoContact contact) async => contact;

  @override
  Future<void> deleteContact(String contactId) async {}

  @override
  Future<VideoCall> startCall({
    required String deviceId,
    required String contactId,
  }) async {
    return const VideoCall(
      id: 'call-new',
      deviceId: 'device-1',
      contactId: 'contact-1',
      initiator: 'guardian',
      state: 'ringing',
      quality: 'unknown',
      endReason: '',
    );
  }

  @override
  Future<VideoCall> endCall(String callId) async {
    endedCallId = callId;
    return VideoCall(
      id: callId,
      deviceId: 'device-1',
      contactId: 'contact-1',
      initiator: 'guardian',
      state: 'ended',
      quality: 'good',
      endReason: 'hangup',
    );
  }

  @override
  Future<VideoCapability> fetchCapability(String deviceId) async {
    return const VideoCapability(
      supported: true,
      callState: 'idle',
      quality: 'good',
      cameraAllowed: true,
      microphoneAllowed: true,
    );
  }
}
