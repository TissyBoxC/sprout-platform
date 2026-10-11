/// Local action a device performs with no network.
enum LocalCommandAction {
  playPackage('play_package', '播放内容', requiresArgument: true),
  pause('pause', '暂停播放'),
  resume('resume', '继续播放'),
  stop('stop', '停止播放'),
  setVolume('set_volume', '调节音量', requiresArgument: true),
  sleep('sleep', '进入休眠');

  const LocalCommandAction(
    this.apiValue,
    this.label, {
    this.requiresArgument = false,
  });

  final String apiValue;
  final String label;

  /// Whether the action needs a target argument, e.g. a package id.
  final bool requiresArgument;

  static LocalCommandAction? fromApiValue(String value) {
    for (final action in LocalCommandAction.values) {
      if (action.apiValue == value) {
        return action;
      }
    }
    return null;
  }
}

/// One guardian-published local command macro.
class LocalCommand {
  const LocalCommand({
    required this.commandId,
    required this.phrase,
    required this.action,
    required this.argument,
    required this.enabled,
    required this.revision,
  });

  final String commandId;
  final String phrase;
  final LocalCommandAction action;
  final String argument;
  final bool enabled;
  final int revision;

  factory LocalCommand.fromJson(Map<String, Object?> json) {
    final action = LocalCommandAction.fromApiValue(_string(json['action']));
    if (action == null) {
      throw const FormatException('unknown local command action');
    }
    return LocalCommand(
      commandId: _string(json['command_id']),
      phrase: _string(json['phrase']),
      action: action,
      argument: _string(json['argument']),
      enabled: json['enabled'] == true,
      revision: _asInt(json['revision']),
    );
  }

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'command_id': commandId,
      'phrase': phrase,
      'action': action.apiValue,
      'argument': argument,
      'enabled': enabled,
    };
  }
}

/// A family's local command set plus the revision a device syncs against.
class LocalCommandSet {
  const LocalCommandSet({required this.revision, required this.commands});

  const LocalCommandSet.empty() : revision = 0, commands = const [];

  final int revision;
  final List<LocalCommand> commands;

  bool get isEmpty => commands.isEmpty;

  factory LocalCommandSet.fromJson(Map<String, Object?> json) {
    final commands = json['commands'];
    return LocalCommandSet(
      revision: _asInt(json['revision']),
      commands: commands is List
          ? commands
                .whereType<Map>()
                .map(
                  (item) => LocalCommand.fromJson(
                    Map<String, Object?>.from(item),
                  ),
                )
                .toList(growable: false)
          : const <LocalCommand>[],
    );
  }
}

String _string(Object? value) => value is String ? value : '';

int _asInt(Object? value) => value is num ? value.toInt() : 0;
