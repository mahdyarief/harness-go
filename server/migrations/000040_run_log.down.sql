DROP TRIGGER run_events_immutable ON run_events;
DROP FUNCTION reject_run_event_mutation();

DROP POLICY run_event_select ON run_events;
DROP POLICY run_event_insert ON run_events;
DROP POLICY run_select ON runs;
DROP POLICY run_insert ON runs;
DROP POLICY run_update ON runs;

DROP TABLE run_events;
DROP TABLE runs;
