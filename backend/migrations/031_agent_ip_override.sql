-- ---------------------------------------------------------------------------
-- 031_agent_ip_override
-- Lets an operator pin the address an agent is recorded under.
--
-- Auto-detection reports what the host can see of itself, which is not always
-- the address anyone wants to read. A host behind NAT, or one whose default
-- route leaves through a public interface, reports an address that is correct
-- but useless for reaching it on the internal network.
--
-- The detected address is kept in ip_address rather than being overwritten, so
-- the two can be compared when one of them looks wrong.
-- ---------------------------------------------------------------------------

ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS ip_address_override VARCHAR(45);
