import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/local_command/data/local_command_api.dart';
import 'package:parent_app/features/local_command/domain/local_command.dart';

void main() {
  test('parses the local command set envelope', () async {
    final api = HttpLocalCommandApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'revision': 3,
            'commands': <Object?>[
              <String, Object?>{
                'command_id': 'cmd_play',
                'phrase': '听故事',
                'action': 'play_package',
                'argument': 'story_001',
                'enabled': true,
                'revision': 3,
              },
              <String, Object?>{
                'command_id': 'cmd_pause',
                'phrase': '停一下',
                'action': 'pause',
                'argument': '',
                'enabled': false,
                'revision': 3,
              },
            ],
          },
        },
      ),
    );

    final set = await api.fetchSet();

    expect(set.revision, 3);
    expect(set.commands, hasLength(2));
    expect(set.commands.first.action, LocalCommandAction.playPackage);
    expect(set.commands.first.argument, 'story_001');
    expect(set.commands[1].action, LocalCommandAction.pause);
    expect(set.commands[1].enabled, isFalse);
  });

  test('publishes the whole set with the action api values', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{'revision': 1, 'commands': <Object?>[]},
      },
    );
    final api = HttpLocalCommandApi(client);

    await api.replaceSet([
      const LocalCommand(
        commandId: 'cmd_volume',
        phrase: '小声一点',
        action: LocalCommandAction.setVolume,
        argument: '30',
        enabled: true,
        revision: 0,
      ),
    ]);

    expect(client.putPath, '/api/v1/local-commands');
    final body = client.lastPutBody! as Map<String, Object?>;
    final commands = body['commands']! as List<Object?>;
    final command = commands.single! as Map<String, Object?>;
    expect(command['action'], 'set_volume');
    expect(command['argument'], '30');
  });

  test('delete targets the encoded command path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{'command_id': 'cmd_play'},
      },
    );
    final api = HttpLocalCommandApi(client);

    await api.deleteCommand('cmd_play');

    expect(client.deletePath, '/api/v1/local-commands/cmd_play');
  });
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  String? putPath;
  Object? lastPutBody;
  String? deletePath;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async => response;

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    putPath = path;
    lastPutBody = body;
    return response;
  }

  @override
  Future<Map<String, Object?>> delete(String path) async {
    deletePath = path;
    return response;
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) {
    throw UnimplementedError();
  }
}
