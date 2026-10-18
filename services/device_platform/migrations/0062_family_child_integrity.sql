-- Enforce that every child reference written by a family-scoped module
-- belongs to the same family. Before this migration the child UUID was only
-- checked for shape or referenced child_profiles(id) without family ownership,
-- allowing a family to associate records with another family's child.
--
-- child_profiles.id is already the primary key. Adding the composite unique
-- key lets the dependent tables use a composite foreign key while retaining
-- their existing single-column foreign keys for delete behaviour.

ALTER TABLE child_profiles
    ADD CONSTRAINT child_profiles_family_id_id_unique
    UNIQUE (family_id, id);

ALTER TABLE companionship_growth_records
    ADD CONSTRAINT companionship_growth_records_child_family_fk
    FOREIGN KEY (family_id, child_id)
    REFERENCES child_profiles (family_id, id)
    ON DELETE SET NULL (child_id);

ALTER TABLE companionship_habit_reminders
    ADD CONSTRAINT companionship_habit_reminders_child_family_fk
    FOREIGN KEY (family_id, child_id)
    REFERENCES child_profiles (family_id, id)
    ON DELETE SET NULL (child_id);

ALTER TABLE english_practice_attempts
    ADD CONSTRAINT english_practice_attempts_child_family_fk
    FOREIGN KEY (family_id, child_id)
    REFERENCES child_profiles (family_id, id)
    ON DELETE SET NULL (child_id);

ALTER TABLE alarm_reminders
    ADD CONSTRAINT alarm_reminders_child_family_fk
    FOREIGN KEY (family_id, child_id)
    REFERENCES child_profiles (family_id, id)
    ON DELETE SET NULL (child_id);

ALTER TABLE wearable_contacts
    ADD CONSTRAINT wearable_contacts_child_family_fk
    FOREIGN KEY (family_id, child_id)
    REFERENCES child_profiles (family_id, id)
    ON DELETE SET NULL (child_id);

ALTER TABLE wearable_geofences
    ADD CONSTRAINT wearable_geofences_child_family_fk
    FOREIGN KEY (family_id, child_id)
    REFERENCES child_profiles (family_id, id)
    ON DELETE SET NULL (child_id);

ALTER TABLE video_contacts
    ADD CONSTRAINT video_contacts_child_family_fk
    FOREIGN KEY (family_id, child_id)
    REFERENCES child_profiles (family_id, id)
    ON DELETE SET NULL (child_id);
