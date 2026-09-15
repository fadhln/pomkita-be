-- Phase 4 acknowledgement rollback.

DROP TABLE IF EXISTS ack_supersessions;
DROP TABLE IF EXISTS ack_head;
DROP TABLE IF EXISTS ack_decisions;
DROP TYPE IF EXISTS ack_decision;
