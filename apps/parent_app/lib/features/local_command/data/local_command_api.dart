import '../../../core/network/api_client.dart';
import '../domain/local_command.dart';

/// Guardian-facing local command contracts.
///
/// The platform scopes every set to the authenticated family, so the client
/// never sends an owner id. Publishing replaces the whole set atomically.
abstract interface class LocalCommandApi {
  Future<LocalCommandSet> fetchSet();

  /// Replaces the family's whole command set and returns the new revision.
  Future<LocalCommandSet> replaceSet(List<LocalCommand> commands);

  /// Removes one command.
  Future<void> deleteCommand(String commandId);
}

/// Production implementation backed by the authenticated API client.
class HttpLocalCommandApi implements LocalCommandApi {
  const HttpLocalCommandApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<LocalCommandSet> fetchSet() async {
    final response = await _apiClient.get('/api/v1/local-commands');
    return LocalCommandSet.fromJson(_data(response));
  }

  @override
  Future<LocalCommandSet> replaceSet(List<LocalCommand> commands) async {
    final response = await _apiClient.put(
      '/api/v1/local-commands',
      body: <String, Object?>{
        'commands': [
          for (final command in commands) command.toJson(),
        ],
      },
    );
    return LocalCommandSet.fromJson(_data(response));
  }

  @override
  Future<void> deleteCommand(String commandId) async {
    await _apiClient.delete(
      '/api/v1/local-commands/${Uri.encodeComponent(commandId)}',
    );
  }
}

Map<String, Object?> _data(Map<String, Object?> response) {
  final data = response['data'];
  if (data is! Map) {
    throw const FormatException('missing local command data');
  }
  return Map<String, Object?>.from(data);
}
