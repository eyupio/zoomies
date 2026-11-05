---
layout: compare
icon: material/compare-horizontal
title: "ARC alternatives: runner controllers compared"
description: >-
  Self-hosted GitHub Actions runner controllers compared: ARC, GARM, runscaler,
  gharp, the AWS Terraform module, Cirun and Zoomies, and which need Kubernetes.
---

# GitHub Actions runner controllers compared

This page compares ways to run GitHub Actions jobs on runners other than GitHub's
own. If you are looking for an alternative to GitHub Actions itself, such as
GitLab CI or Buildkite, this is not that comparison: here your workflows stay as
they are and only the machines that run them change.

There are several good ways to run GitHub Actions jobs somewhere other than
GitHub's own runners, and most people arrive here looking for an alternative to
[actions-runner-controller (ARC)](../actions-runner-controller.md), usually
because they do not want to run Kubernetes. Zoomies is one of the answers, and
for many setups it is not the right one. These pages say where each tool is
stronger, where Zoomies is, and how to move between them.

Each page is written from the project's public documentation, dated, and linked
so you can check it. They are comparisons of documentation, not benchmarks, and
a feature one page does not mention is not a claim that it is missing.

## Which of them need Kubernetes

| | Needs Kubernetes | Web UI | Licence | Runners run on |
| --- | --- | --- | --- | --- |
| **Zoomies** | No | Yes | AGPL-3.0 | Linux hosts you own or rent |
| [ARC](../actions-runner-controller.md) | Yes | No, metrics only | Apache-2.0 | A Kubernetes or OpenShift cluster |
| [GARM](garm.md) | No | Yes | Apache-2.0 | Providers you configure |
| [runscaler](runscaler.md) | No | None documented | MIT | Docker, or macOS VMs through Tart |
| [gharp](gharp.md) | No | A dashboard | Apache-2.0 | Docker on one host |
| [AWS Terraform module](terraform-aws-github-runner.md) | No | Your AWS tooling | MIT | EC2 in your account |
| [TestFlows Hetzner runners](testflows-hetzner-runners.md) | No | An embedded dashboard | Apache-2.0 | Hetzner Cloud servers |
| [Cirun](cirun.md) | No | A managed service | Not applicable | Your cloud, or on-premises agents |

Each cell comes from the project's own documentation, as the linked page says.
Where a project's documentation is silent the cell says so, and does not guess.

## At a glance

| | Model and licence | Runs on | Pick it when |
| --- | --- | --- | --- |
| **Zoomies** | Self-hosted; [AGPL-3.0](https://github.com/eyupio/zoomies/blob/main/LICENSE) | Linux hosts you own or rent, anywhere an agent can dial out from | You have machines, not a cluster, and want one place to see and run the fleet |
| [ARC](../actions-runner-controller.md) | Kubernetes operator; Apache-2.0 | A Kubernetes or OpenShift cluster | You already operate Kubernetes |
| [GARM](garm.md) | Self-hosted; Apache-2.0 | Providers you configure | You want a provider catalogue, or GitHub Enterprise Server or Gitea |
| [runscaler](runscaler.md) | Self-hosted; MIT | Docker, or macOS VMs through Tart | You need macOS runners, or want the smallest setup on one host |
| [gharp](gharp.md) | Self-hosted; Apache-2.0 | Docker on one host | You want one container and a dashboard for a few repositories |
| [AWS Terraform module](terraform-aws-github-runner.md) | Terraform module; MIT | EC2, in your AWS account | Your infrastructure is AWS and Terraform |
| [TestFlows Hetzner runners](testflows-hetzner-runners.md) | Self-hosted; Apache-2.0 | Hetzner Cloud servers | You want cheap, standby cloud runners on Hetzner |
| [Cirun](cirun.md) | Managed service | Your cloud account, or on-premises agents | You would rather have a service run the control plane |
| [Blacksmith, WarpBuild and RunsOn](../hosted-runner-services.md) | Services and a licensed AWS tool | Their cloud, or yours | You do not want machines to look after, or you need macOS |
| [GitHub-hosted runners](../costs.md) | GitHub's service | GitHub's virtual machines | The jobs are public, or the free minutes cover you |

## How to choose

1. **Do you already run Kubernetes?** If someone on the team is comfortable
   operating it, [ARC](../actions-runner-controller.md) is likely the answer.
   If not, every other row above runs without it.
2. **Is your infrastructure one cloud, described in code?** A cloud-native
   module such as [the AWS one](terraform-aws-github-runner.md) puts CI inside
   the account and the tooling you already use.
3. **Do you have machines already, in a rack, an office or a home lab?** That is
   the case Zoomies is built for. [Runners in your home lab](../home-lab.md)
   and [runners without Kubernetes](../runners-without-kubernetes.md) plan it.
4. **Do you need macOS runners?** [runscaler](runscaler.md) boots them on
   Apple Silicon, and the [hosted services](../hosted-runner-services.md) offer
   them too. Zoomies has no macOS runner image.
5. **Would you rather pay to not run it?** A hosted service trades money for
   not looking after hosts. [Costs](../costs.md) has the sums.

If it is still unclear, the [FAQ](../faq.md) answers what people ask before they
self-host runners, and the [quick start](../quickstart.md) takes a fresh host to a
running job in about five minutes.
