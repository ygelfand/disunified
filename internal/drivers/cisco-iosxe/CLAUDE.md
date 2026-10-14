# Cisco IOS XE

Read `docs/drivers/cisco-iosxe.md` and common driver rules first.
NETCONF only: no SNMP dependency or fallback without an explicit scope decision.
The shared `internal/netconfrun` transport supports SSH and mutual TLS; vendor
filters and edits stay here. Do not infer speed capabilities from
link speed. Exclude phantom interface variants using platform inventory.
