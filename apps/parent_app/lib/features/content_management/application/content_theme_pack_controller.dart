import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../data/content_management_api.dart';
import '../domain/content_package.dart';
import 'content_library_controller.dart' show contentManagementApiProvider;

/// Owns the published theme packs shown to the guardian.
class ContentThemePackController extends AsyncNotifier<List<ContentThemePack>> {
  late ContentManagementApi _api;

  @override
  Future<List<ContentThemePack>> build() async {
    _api = ref.read(contentManagementApiProvider);
    return _api.fetchThemePacks();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(() => _api.fetchThemePacks());
  }

  /// Maps a load failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final contentThemePackControllerProvider =
    AsyncNotifierProvider<ContentThemePackController, List<ContentThemePack>>(
      ContentThemePackController.new,
    );
