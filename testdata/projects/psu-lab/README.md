# psu-lab

A synthetic test fixture, not an example: N-supply load sharing (`PsuUnit`) for
the plant simulation in `examples/it-cluster`. LAB1 has one supply, LAB4 four.

`lib/server.st` and `hw_types.st` are **symlinks** into `examples/it-cluster`,
so the test runs the real blocks and not a copy. A manifest can only compose
libraries from its own root and `lib/`, and `naut` has no extra-lib flag.
On Windows, clone with `core.symlinks=true` (and Developer Mode or an elevated
git) or these two files check out as plain text and `naut check` fails.

    naut check testdata/projects/psu-lab
    naut test testdata/projects/psu-lab
