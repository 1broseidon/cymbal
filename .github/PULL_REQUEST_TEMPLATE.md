## Summary

- What changed?
- Why is this needed?

<!--
Trivial PRs (≤20 changed lines, or labeled `trivial` / `docs` / `dependencies`)
skip the rest of this template. For those, a short `## Summary` is enough.

For larger PRs, fill in the sections below. Only `## Summary` is strictly
required by CI; `## Checklist` boxes are checked as warnings (not failures)
so reviewers can still merge when unchecked items are justified.
-->

## Testing

- Commands run:
  - `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go test ./...`
- Extra validation:
  - N/A

## Checklist

- [ ] I ran the required checks locally, or I explained why I could not in the Testing section.
- [ ] I reviewed the diff for unrelated, generated, or vendored changes.
- [ ] I updated docs, benchmarks, or release notes when behavior changed, or I explained why no updates were needed.
- [ ] I filled out the Security Notes and Risks / Rollout sections below.

## Security Notes

- User input, file parsing, shell execution, or network behavior touched:
- New dependencies or vendored code added:
- Secrets, credentials, or tokens touched:
- Follow-up needed:

## Risks / Rollout

- Risk level:
- Rollback plan:
