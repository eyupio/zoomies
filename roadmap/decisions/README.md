# Decision records

One file per decision, numbered in the order they were taken, never renumbered
and never deleted. A decision that is reversed gets a new record that says so
and links back; the old one is marked superseded.

Each record has five parts and no more:

* **Status**: proposed, accepted, superseded by NNNN.
* **Date**, and who took it. A decision the owner has to ratify says so in the
  status line until they have.
* **Context**: the facts that made the decision necessary, with links to the
  code or the evidence. Short.
* **Decision**: one paragraph, in the imperative.
* **Consequences**: what it costs, what it rules out, and what has to change
  because of it.

The decisions the roadmap asks the owner to ratify are listed in
[ROADMAP.md](../../ROADMAP.md); each links to its record here.

| Record | Decision | Status |
| --- | --- | --- |
| [0001](0001-planning-documents-live-beside-the-code.md) | Planning documents live in `ROADMAP.md` and `roadmap/`, outside the published site | proposed |
| [0002](0002-choose-the-model-by-what-the-stage-risks.md) | Choose the Claude model and effort by what a stage risks, and record both per package | proposed |
