# Go (golang) specific instructions

- Never use `context.Context` values (`WithValue`/`Value`) to influence code behavior (control flow, feature flags, etc.). Context values are for request-scoped metadata only (trace IDs, deadlines).
- Modularize: every package must be as self-sufficient as possible. Use interfaces to abstract inter-package dependencies.
- Maintain auto-test coverage above 90%. Increase coverage via meaningful test cases, not synthetic boilerplate.
