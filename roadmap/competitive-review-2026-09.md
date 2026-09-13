# Runner platform comparison

Reviewed 13 September 2026. This is a comparison of public documentation,
not a benchmark or code audit. Default-branch documentation can describe
unreleased work; validate against a selected release before making claims.
"Not verified" is not a claim that a feature is absent.

| Offering | Model and licence | Documented strengths | Zoomies comparison and resulting work |
| --- | --- | --- | --- |
| [GARM](https://github.com/cloudbase/garm) | Self-hosted; Apache-2.0 | Single binary, SQLite, built-in UI, external providers, webhook pools and native scale sets; GitHub/GHES/Gitea | Closest architectural competitor. A UI and no Kubernetes are shared features. Prove easier migration/private-host operations; evaluate provider and scale-set gaps through ZF-213/214/217. Its README warns that main may differ from stable releases. |
| [ARC](https://github.com/actions/actions-runner-controller) | Kubernetes operator; Apache-2.0 | Autoscaling runner scale sets, repository/organisation/enterprise scope, Helm deployment, collaboration with the GitHub Actions team | Strong fit for existing Kubernetes operators. Zoomies fits ordinary hosts without requiring a cluster. Benchmark recovery and concurrency rather than claiming greater reliability; ZF-211/217. |
| [Terraform AWS runners](https://github.com/github-aws-runners/terraform-aws-github-runner) | Self-managed Terraform module; MIT | AWS event-driven EC2/spot lifecycle, Lambda scaling, scale-to-zero, custom AMIs/subnets, Linux x64/arm64 and Windows | Strong AWS infrastructure-as-code alternative. Zoomies offers a provider-independent existing-host workflow; improve configuration portability and one receiver before broad providers, ZF-213/214/215. The old Philips Labs repository is archived; use this successor. |
| [TestFlows Hetzner runners](https://github.com/testflows/TestFlows-GitHub-Hetzner-Runners) | Self-hosted Hetzner-focused project; [Apache-2.0](https://github.com/testflows/TestFlows-GitHub-Hetzner-Runners/blob/main/LICENSE) | Standby pools, server-type/location fallback, ARM64, persistent cache volumes, cost estimates and embedded monitoring dashboard | Direct competitor for inexpensive cloud CI, not merely a runner image. Its documented setup uses a classic token and separate project per repository. Zoomies should emphasise App-based multi-host administration and improve caching, fallback and cost evidence: ZF-212/214/215. |
| [Docker GitHub Actions runner](https://github.com/myoung34/docker-github-actions-runner) | Container runner project; [GPL-3.0](https://github.com/myoung34/docker-github-actions-runner/blob/master/LICENSE) | Packages self-hosted runners for Docker operation | A lower-level alternative for users content to assemble orchestration themselves. Zoomies' differentiator is the integrated controller, scheduling, fleet UI and assisted migration. Do not equate a runner image with a managed fleet product. |
| [Cirun](https://docs.cirun.io/) | Managed service; open-source components do not establish an open-source hosted control plane | Bring-your-own-cloud provisioning, on-premises agents, platform breadth and own-storage caching | Commercial comparator. Private hardware is not exclusive to Zoomies. Emphasise control-plane ownership and the free self-hosted product; close selected operational and performance gaps. |

## Capabilities that should drive the plan

| Capability | Evidence | Response |
| --- | --- | --- |
| VM and OS breadth | [Cirun on-premises](https://docs.cirun.io/on-prem) documents Linux/macOS VM and Docker executors; [Windows](https://docs.cirun.io/os-platform/windows) documents cloud runners | ZF-216: qualify the Windows process implementation already in Zoomies; distinguish persistent host state from VM reset; stage GPU/macOS/VM work |
| Cache performance | [Cirun caching](https://docs.cirun.io/caching/) documents automatic Linux/AWS integration and a separate S3-compatible action | ZF-212: start with compatible recipes and measured cold/warm builds; specify isolation, retention and storage failure behaviour |
| Infrastructure lifecycle | [GARM providers](https://github.com/cloudbase/garm), [AWS module](https://github.com/github-aws-runners/terraform-aws-github-runner), [Hetzner runners](https://github.com/testflows/TestFlows-GitHub-Hetzner-Runners) | ZF-214: one testable capacity receiver before a provider catalogue; make desired capacity idempotent and deletion ownership-aware |
| Portability and setup | AWS Terraform and Cirun YAML give reproducible configuration approaches | ZF-213: secret-free pool manifests, drift review and preserved workflow labels |
| GitHub integration | ARC and GARM describe native scale sets | ZF-217: time-boxed assessment using measured benefits; retain the existing webhook/poller path until a decision |
| Commercial comparison | [Cirun pricing](https://cirun.io/) lists free open-source use, $29/month for 3 private repositories and $79/month for 10, with infrastructure extra | Zoomies' free private-repository software is meaningful; do not compare software fees as though they include compute, storage or operator time |

## Positioning and evidence

Zoomies is a free, open-source fleet controller with guided setup, assisted
migration and integrated private-host management. GARM demonstrates that a
single binary, SQLite and a UI are useful design choices rather than exclusive
claims. TestFlows also has a dashboard. Cirun also runs on private hardware.

The recommended priority is current qualification evidence, administration and
resource boundaries, repeatable lifecycle, caching and host UX, followed by one
capacity integration and staged platform expansion. Do not publish claims of
faster execution, stronger isolation, unique home-lab support or production
readiness without evidence.

The earlier website-only Cirun comparison understated Zoomies' Windows
implementation: the current package record marks ZF-206 implemented but awaiting
live qualification. Report both facts. Consult
[support and measurement](support-and-measurement.md) and
[package status](progress.md) before repeating any support claim.
