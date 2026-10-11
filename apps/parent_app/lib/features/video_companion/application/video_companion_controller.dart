import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/video_companion_api.dart';
import '../domain/video_companion.dart';

/// Holds the guardian's video contacts and recent call history.
class VideoContactsState {
  const VideoContactsState({required this.contacts, required this.calls});

  final List<VideoContact> contacts;
  final List<VideoCall> calls;

  VideoContactsState copyWith({
    List<VideoContact>? contacts,
    List<VideoCall>? calls,
  }) {
    return VideoContactsState(
      contacts: contacts ?? this.contacts,
      calls: calls ?? this.calls,
    );
  }
}

/// Owns video contacts and call history for the family.
class VideoCompanionController extends AsyncNotifier<VideoContactsState> {
  late final VideoCompanionApi _api;

  @override
  Future<VideoContactsState> build() async {
    _api = ref.read(videoCompanionApiProvider);
    final contacts = await _api.fetchContacts();
    final calls = await _api.fetchHistory();
    return VideoContactsState(contacts: contacts, calls: calls);
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(build);
  }

  Future<VideoContact> createContact(VideoContact contact) async {
    final created = await _api.createContact(contact);
    final current = state.value ?? const VideoContactsState(
      contacts: <VideoContact>[],
      calls: <VideoCall>[],
    );
    state = AsyncValue.data(
      current.copyWith(contacts: <VideoContact>[created, ...current.contacts]),
    );
    return created;
  }

  Future<void> deleteContact(String contactId) async {
    await _api.deleteContact(contactId);
    final current = state.value;
    if (current == null) {
      return;
    }
    state = AsyncValue.data(
      current.copyWith(
        contacts: current.contacts
            .where((contact) => contact.id != contactId)
            .toList(growable: false),
      ),
    );
  }

  Future<VideoCall> startCall({
    required String deviceId,
    required String contactId,
  }) async {
    final call = await _api.startCall(deviceId: deviceId, contactId: contactId);
    final current = state.value;
    if (current != null) {
      state = AsyncValue.data(
        current.copyWith(calls: <VideoCall>[call, ...current.calls]),
      );
    }
    return call;
  }

  Future<void> endCall(String callId) async {
    final ended = await _api.endCall(callId);
    final current = state.value;
    if (current == null) {
      return;
    }
    state = AsyncValue.data(
      current.copyWith(
        calls: current.calls
            .map((call) => call.id == callId ? ended : call)
            .toList(growable: false),
      ),
    );
  }

  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

/// Read-only video capability for one device.
final videoCapabilityProvider =
    FutureProvider.family<VideoCapability, String>((ref, deviceId) {
  return ref.read(videoCompanionApiProvider).fetchCapability(deviceId);
});

final videoCompanionApiProvider = Provider<VideoCompanionApi>((ref) {
  return HttpVideoCompanionApi(ref.read(apiClientProvider));
});

final videoCompanionControllerProvider =
    AsyncNotifierProvider<VideoCompanionController, VideoContactsState>(
      VideoCompanionController.new,
    );
