# Checkpoint Schema

## Impl checkpoint

# Impl checkpoint — <repo>

- Feature:
- Contract version:
- Branch:
- Commit SHA:            # human commits later; worker writes "pending"
- Files changed:
- Endpoints implemented:
- Screens/components (FE):
- Tests run:             # command → result
- Evidence:              # git diff --stat, file paths
- Known risks:
- Open questions:

## QA checkpoint

# QA checkpoint — <feature>

- Contract version:
- Conformance mock vs real BE: PASS/FAIL
- Gates:
  - mirror hash:
  - m-commit-gate:
  - type-check:
  - lint:
  - format:
  - unit:
  - build:
  - e2e:
- Product: PASS/FAIL
- Env: OK/BLOCKED
- Failed tests:
- Raw logs:
- Risk review files:     # concurrency/money/migrations/auth

## Acceptance report

# Acceptance report — <feature>

- Contract version:
- BE commit:
- FE commit:
- QA result:
- Risk review:
- Open issues:
- Recommendation: approve commit / needs fix
