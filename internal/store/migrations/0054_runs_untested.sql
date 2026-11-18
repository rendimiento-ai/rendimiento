-- What of a branch's change no step of its run builds or tests (folders
-- like "mobile/", or single files): such a run is not merged by itself and
-- its GitHub check asks a person to merge it. Empty when the whole change
-- was tested, or for runs of the default branch. Only adds: the previous
-- version neither reads nor writes it.
ALTER TABLE runs ADD COLUMN IF NOT EXISTS untested TEXT[] NOT NULL DEFAULT '{}';
