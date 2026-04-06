## Summary

- What changed?
- Why is this needed?

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
