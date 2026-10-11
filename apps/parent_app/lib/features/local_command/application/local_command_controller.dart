import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/local_command_api.dart';
import '../domain/local_command.dart';

/// Owns the guardian's editable local command set.
class LocalCommandController extends AsyncNotifier<LocalCommandSet> {
  late final LocalCommandApi _api;

  @override
  Future<LocalCommandSet> build() async {
    _api = ref.read(localCommandApiProvider);
    return _api.fetchSet();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_api.fetchSet);
  }

  /// Publishes a full command set. The caller assembles the exact set it wants
  /// stored; the platform validates and swaps it atomically.
  Future<LocalCommandSet> save(List<LocalCommand> commands) async {
    final saved = await _api.replaceSet(commands);
    state = AsyncData(saved);
    return saved;
  }

  /// Removes one command and refreshes the revision.
  Future<void> delete(String commandId) async {
    await _api.deleteCommand(commandId);
    await refresh();
  }

  /// Flips one command's enabled flag without touching the others.
  Future<void> toggleEnabled(String commandId) async {
    final current = state.value;
    if (current == null) {
      return;
    }
    final updated = [
      for (final command in current.commands)
        if (command.commandId == commandId)
          LocalCommand(
            commandId: command.commandId,
            phrase: command.phrase,
            action: command.action,
            argument: command.argument,
            enabled: !command.enabled,
            revision: command.revision,
          )
        else
          command,
    ];
    await save(updated);
  }

  /// Maps a failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final localCommandApiProvider = Provider<LocalCommandApi>((ref) {
  return HttpLocalCommandApi(ref.read(apiClientProvider));
});

final localCommandControllerProvider =
    AsyncNotifierProvider<LocalCommandController, LocalCommandSet>(
      LocalCommandController.new,
    );
