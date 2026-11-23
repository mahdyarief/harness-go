-- The model a run used is only reproducible together with its version: a
-- provider can change behaviour under the same name.
ALTER TABLE runs
    ADD COLUMN model_version text NOT NULL DEFAULT '' CHECK (length(model_version) <= 200);
