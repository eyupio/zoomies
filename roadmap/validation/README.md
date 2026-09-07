# Validation evidence

One file per gate or per measured baseline, named for the commit it describes.
A file here is a record of what was run, on what, with what result, and what
was not run. It is not a summary of what should be true.

Rules:

* Name the exact commit, the toolchain versions, and the machine or runner
  class. A number without the setup that produced it is not evidence.
* Separate *passed*, *failed*, *not run* and *blocked*. A test that skipped
  itself for want of a credential or a daemon is *not run*, and a gate that
  counts it as passed is lying.
* Record the denominator with every rate. "99% of attempts" means nothing
  without the count and the exclusions.
* Link to the thing itself where one exists: a workflow run, a pull request, a
  log kept somewhere durable. Do not paste a log body here; a redacted excerpt
  is enough.
* When a later change invalidates a result, say so in the file that held it
  rather than deleting it, so the history of what was believed and why stays
  readable.

Files:

| File | What it records |
| --- | --- |
| [baseline-6d12a72.md](baseline-6d12a72.md) | The reconciled baseline for the follow-on roadmap: what was run against `main` at `6d12a72` on 6 September 2026 and what was not. |
| [gate-f-readiness-2cc7d9c.md](gate-f-readiness-2cc7d9c.md) | What Gate F still needs at the end of Assignment A: which of its targets can be measured at all, what has never run, and what is the owner's to supply. |

Gate F, when it is attempted, gets a file of its own here, with the counts the
roadmap asks for and the exclusions listed beside them.
