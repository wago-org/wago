# Funcref import-cycle lifetime

Date: 2026-09-02

The Starshine engine-state lane found an executable-range leak in repeated
linked-module cases. The first million-case attempt stopped at case 30,984 when
the fixed 4,096-entry executable range table filled.

The reduced case has four instances:

1. An owner exports a table, memory, and mutable numeric global.
2. Two relay instances import and re-export all three resources.
3. A consumer imports the resources and installs its own function in the
   owner's table with an active element segment.

The table retained the closed consumer so its function stayed callable. The
consumer retained the owner through its imported memory and global. Reverse
close therefore left the owner with two resource roots and the consumer with
one. Repeating the exact three-support graph filled the executable range table
on request 1,365.

When an imported owner table takes over a closed consumer's lifetime, the
consumer now transfers its memory, global, and table attachments from that same
owner. Attachments from other owners do not transfer. An open downstream
importer still retains the owner. With no downstream importer, owner close can
release the table, which releases the consumer and completes both physical
teardowns.

The transferred-attachment record stays live through the consumer's complete
memory teardown. This is required for shared memory zero, which uses a separate
threaded-control release branch. Removing the record earlier can detach the
same import twice and consume another live instance's importer count.

`TestReverseCloseReexportChainReleasesFuncrefCycle` checks the reduced graph and
all instance root counts. Its modules use direct Core 2 binary fixtures so the
test also builds under TinyGo and runs on targets without complete Core 3
support. The exact generated graph passed 5,000 repeated worker requests after
the fix. The complete 1,000,000-case differential run then passed without
filling the range table.
