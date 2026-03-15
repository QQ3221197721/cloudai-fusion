# CloudAI Fusion Governance

This document describes the governance model for the CloudAI Fusion project.

## Principles

- **Open**: CloudAI Fusion is an open-source project under the Apache License 2.0.
- **Transparent**: All decisions, discussions, and changes happen in public channels.
- **Merit-based**: Influence is earned through contributions, not through affiliation.
- **Welcoming**: We welcome contributors of all backgrounds and experience levels.

## Project Roles

### Users

Anyone who uses CloudAI Fusion. Users are encouraged to participate by:

- Filing bug reports and feature requests
- Participating in GitHub Discussions
- Helping other users in community channels

### Contributors

Anyone who contributes code, documentation, tests, or other artifacts.
Contributors must sign the [DCO](DCO) (Developer Certificate of Origin) via
`Signed-off-by` in their commits.

**Requirements:**
- At least 1 merged pull request

**Privileges:**
- Listed in the Contributors section of README
- Can be assigned to issues

### Reviewers

Trusted contributors who have demonstrated expertise in one or more project
areas. Reviewers help maintain code quality through pull request reviews.

**Requirements:**
- At least 10 merged pull requests with substantive changes
- Demonstrated deep understanding of at least one project area
- Consistent participation over at least 3 months
- Nominated by a Maintainer, approved by majority vote of Maintainers

**Privileges:**
- Listed as Reviewer in CODEOWNERS for their area of expertise
- Can approve pull requests (approval still requires a Maintainer merge)
- Invited to reviewer-only discussions

**Responsibilities:**
- Review pull requests in their area within 5 business days
- Provide constructive, actionable feedback
- Help mentor new contributors

### Maintainers

Maintainers are responsible for the overall health and direction of the project.
They have write access to the repository and are responsible for releases.

**Requirements:**
- At least 6 months of active contribution as a Reviewer
- Broad understanding of the project architecture
- Demonstrated leadership in the community
- Nominated by an existing Maintainer, approved by supermajority (2/3) of existing Maintainers

**Privileges:**
- Write access to the repository
- Ability to merge pull requests
- Release management
- Listed in the Maintainers section of README and CODEOWNERS

**Responsibilities:**
- Ensure the overall quality and consistency of the codebase
- Participate in release planning and execution
- Mentor Reviewers and Contributors
- Respond to security reports
- Participate in governance decisions

### Project Lead

The Project Lead is responsible for the overall vision and strategic direction
of the project. Initially, this role belongs to the project founder.

**Selection:**
- Elected by Maintainers via supermajority (2/3) vote
- Serves a 1-year term, renewable
- Can be replaced by a supermajority (2/3) vote of Maintainers at any time

**Responsibilities:**
- Set project vision and roadmap priorities
- Break ties in governance decisions
- Represent the project externally (conferences, CNCF meetings)
- Final escalation point for Code of Conduct issues

## Current Maintainers

| Name | GitHub | Area of Focus |
|------|--------|---------------|
| *(To be filled)* | — | Project Lead |

## Decision Making

### Lazy Consensus

Most decisions are made through **lazy consensus**: a proposal is shared, and if
no objections are raised within 5 business days, it is considered approved.

This applies to:
- Feature designs and API changes
- Dependency updates
- Documentation changes
- Non-breaking refactors

### Voting

For significant decisions, formal voting is required:

| Decision Type | Voters | Threshold |
|---------------|--------|-----------|
| Feature/API change | Maintainers | Lazy consensus (5 days) |
| New Reviewer | Maintainers | Majority (>50%) |
| New Maintainer | Maintainers | Supermajority (2/3) |
| Project Lead election | Maintainers | Supermajority (2/3) |
| Governance change | Maintainers | Supermajority (2/3) |
| License change | All Maintainers | Unanimous |
| Code of Conduct update | Maintainers | Supermajority (2/3) |

### Voting Process

1. A proposal is submitted as a GitHub Issue with the `governance` label
2. Discussion period: minimum 7 calendar days
3. Voting period: 7 calendar days
4. Votes are cast via emoji reactions on the issue:
   - :+1: (thumbs up) = In favor
   - :-1: (thumbs down) = Against
   - :eyes: (eyes) = Abstain
5. Results are recorded in the issue and announced in community channels

## Conflict Resolution

1. **Technical disputes**: Attempt resolution via GitHub discussion or PR comments
2. **Escalation to Maintainers**: If no consensus, Maintainers discuss and vote
3. **Escalation to Project Lead**: If Maintainers are deadlocked, the Project Lead
   decides
4. **Code of Conduct violations**: Reported to conduct@cloudai-fusion.io, handled
   per the [Code of Conduct](CODE_OF_CONDUCT.md) enforcement guidelines

## Stepping Down / Emeritus

Maintainers or Reviewers who can no longer fulfill their responsibilities may
step down gracefully:

1. Notify the Maintainers via email or GitHub
2. Remain in an **Emeritus** status (listed with honor, no active responsibilities)
3. Can return to active status by demonstrating renewed participation, subject to
   Maintainer approval

**Involuntary removal** (inactivity):
- Reviewers: No reviews or contributions for 6 months → moved to Emeritus after notification
- Maintainers: No contributions for 6 months → Maintainer vote to move to Emeritus

## Meetings

- **Community Meeting**: Monthly, open to all (agenda posted 3 days prior)
- **Maintainer Meeting**: Bi-weekly, open to Maintainers and invited Reviewers
- Meeting notes are published in the `docs/meeting-notes/` directory

## Sub-projects

As the project grows, specific areas may be managed as sub-projects with their
own Reviewers and scope:

| Sub-project | Scope | Lead |
|-------------|-------|------|
| Core Platform | API Server, Auth, RBAC | TBD |
| AI Engine | Python AI agents, LLM integration | TBD |
| Scheduler | GPU scheduling, topology awareness | TBD |
| Cloud Providers | Multi-cloud SDK integrations | TBD |
| Infrastructure | Helm, monitoring, CI/CD | TBD |

## Amendments

This governance document can be amended by a supermajority (2/3) vote of active
Maintainers. Proposed amendments must be submitted as a pull request with at
least 14 calendar days for discussion before voting begins.

## References

- [CNCF Project Governance Requirements](https://github.com/cncf/toc/blob/main/process/graduation_criteria.md)
- [Kubernetes Governance](https://github.com/kubernetes/community/blob/master/governance.md)
- [Envoy Governance](https://github.com/envoyproxy/envoy/blob/main/GOVERNANCE.md)
