/// Age ranges approved for the 初芽 onboarding flow.
enum ChildAgeTier { age3To4, age5To6, age7To8 }

extension ChildAgeTierMetadata on ChildAgeTier {
  /// Stable wire value stored by the platform.
  String get wireValue => switch (this) {
    ChildAgeTier.age3To4 => 'age_3_4',
    ChildAgeTier.age5To6 => 'age_5_6',
    ChildAgeTier.age7To8 => 'age_7_8',
  };

  /// Chinese label shown to guardians.
  String get label => switch (this) {
    ChildAgeTier.age3To4 => '3 至 4 岁',
    ChildAgeTier.age5To6 => '5 至 6 岁',
    ChildAgeTier.age7To8 => '7 至 8 岁',
  };

  static ChildAgeTier fromWireValue(String value) {
    return switch (value) {
      'age_3_4' => ChildAgeTier.age3To4,
      'age_5_6' => ChildAgeTier.age5To6,
      'age_7_8' => ChildAgeTier.age7To8,
      _ => throw FormatException('unsupported age tier: $value'),
    };
  }
}

/// Content categories that can be enabled for a child.
enum ChildContentCategory {
  story,
  nurseryRhyme,
  poetry,
  english,
  encyclopedia,
  bedtime,
}

extension ChildContentCategoryMetadata on ChildContentCategory {
  /// Stable wire value stored by the platform.
  String get wireValue => switch (this) {
    ChildContentCategory.story => 'story',
    ChildContentCategory.nurseryRhyme => 'nursery_rhyme',
    ChildContentCategory.poetry => 'poetry',
    ChildContentCategory.english => 'english',
    ChildContentCategory.encyclopedia => 'encyclopedia',
    ChildContentCategory.bedtime => 'bedtime',
  };

  /// Chinese label shown to guardians.
  String get label => switch (this) {
    ChildContentCategory.story => '故事',
    ChildContentCategory.nurseryRhyme => '儿歌',
    ChildContentCategory.poetry => '古诗',
    ChildContentCategory.english => '英语启蒙',
    ChildContentCategory.encyclopedia => '百科',
    ChildContentCategory.bedtime => '睡前',
  };

  static ChildContentCategory fromWireValue(String value) {
    return switch (value) {
      'story' => ChildContentCategory.story,
      'nursery_rhyme' => ChildContentCategory.nurseryRhyme,
      'poetry' => ChildContentCategory.poetry,
      'english' => ChildContentCategory.english,
      'encyclopedia' => ChildContentCategory.encyclopedia,
      'bedtime' => ChildContentCategory.bedtime,
      _ => throw FormatException('unsupported content category: $value'),
    };
  }
}

/// Guardian-owned child profile used for age and content decisions.
class ChildProfile {
  const ChildProfile({
    required this.childId,
    required this.familyId,
    required this.nickname,
    required this.ageTier,
    required this.interests,
    required this.contentCategories,
    required this.guardianConsent,
    required this.createdAt,
    required this.updatedAt,
  });

  final String childId;
  final String familyId;
  final String nickname;
  final ChildAgeTier ageTier;
  final List<String> interests;
  final List<ChildContentCategory> contentCategories;
  final bool guardianConsent;
  final DateTime createdAt;
  final DateTime updatedAt;

  factory ChildProfile.fromJson(Map<String, Object?> json) {
    return ChildProfile(
      childId: _requiredString(json['child_id'], 'child_id'),
      familyId: _requiredString(json['family_id'], 'family_id'),
      nickname: _requiredString(json['nickname'], 'nickname'),
      ageTier: ChildAgeTierMetadata.fromWireValue(
        _requiredString(json['age_tier'], 'age_tier'),
      ),
      interests: _stringList(json['interests']),
      contentCategories: _stringList(
        json['content_categories'],
      ).map(ChildContentCategoryMetadata.fromWireValue).toList(growable: false),
      guardianConsent: json['guardian_consent'] == true,
      createdAt: _requiredDateTime(json['created_at'], 'created_at'),
      updatedAt: _requiredDateTime(json['updated_at'], 'updated_at'),
    );
  }

  Map<String, Object?> toCreateJson() {
    return {
      'nickname': nickname,
      'age_tier': ageTier.wireValue,
      'interests': interests,
      'content_categories': contentCategories
          .map((category) => category.wireValue)
          .toList(growable: false),
      'guardian_consent': true,
    };
  }

  Map<String, Object?> toUpdateJson() {
    return {
      'nickname': nickname,
      'age_tier': ageTier.wireValue,
      'interests': interests,
      'content_categories': contentCategories
          .map((category) => category.wireValue)
          .toList(growable: false),
    };
  }
}

/// Validated values collected by the child profile form.
class ChildProfileDraft {
  const ChildProfileDraft({
    required this.nickname,
    required this.ageTier,
    required this.interests,
    required this.contentCategories,
    required this.guardianConsent,
  });

  final String nickname;
  final ChildAgeTier ageTier;
  final List<String> interests;
  final List<ChildContentCategory> contentCategories;
  final bool guardianConsent;

  Map<String, Object?> toCreateJson() {
    return {
      'nickname': nickname,
      'age_tier': ageTier.wireValue,
      'interests': interests,
      'content_categories': contentCategories
          .map((category) => category.wireValue)
          .toList(growable: false),
      'guardian_consent': guardianConsent,
    };
  }

  Map<String, Object?> toUpdateJson() {
    return {
      'nickname': nickname,
      'age_tier': ageTier.wireValue,
      'interests': interests,
      'content_categories': contentCategories
          .map((category) => category.wireValue)
          .toList(growable: false),
    };
  }
}

/// Reusable interest suggestions that match the platform identifier rules.
const childInterestSuggestions = <String>[
  'animals',
  'space',
  'nature',
  'music',
  'drawing',
  'sports',
  'science',
  'stories',
];

/// Chinese labels for the interest suggestions.
const childInterestLabels = <String, String>{
  'animals': '动物',
  'space': '太空',
  'nature': '自然',
  'music': '音乐',
  'drawing': '画画',
  'sports': '运动',
  'science': '科学',
  'stories': '故事',
};

String _requiredString(Object? value, String field) {
  if (value is String && value.trim().isNotEmpty) {
    return value.trim();
  }
  throw FormatException('missing $field');
}

List<String> _stringList(Object? value) {
  if (value is! List) {
    return const [];
  }
  return value.whereType<String>().toList(growable: false);
}

DateTime _requiredDateTime(Object? value, String field) {
  if (value is String) {
    final parsed = DateTime.tryParse(value);
    if (parsed != null) {
      return parsed;
    }
  }
  throw FormatException('missing $field');
}
