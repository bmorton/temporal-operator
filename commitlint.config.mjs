export default {
  extends: ["@commitlint/config-conventional"],
  rules: {
    // Pre-1.0: don't fail on length/casing/punctuation — pure noise here.
    "header-max-length": [0],
    "body-max-line-length": [0],
    "footer-max-line-length": [0],
    "subject-case": [0],
    "subject-full-stop": [0],
    // Keep the conventional-commit shape as a gentle nudge (warning only).
    // release-please derives the changelog/version bump from the type prefix.
    "type-empty": [1, "never"],
    "type-case": [1, "always", "lower-case"],
    "type-enum": [
      1,
      "always",
      [
        "feat",
        "fix",
        "docs",
        "style",
        "refactor",
        "perf",
        "test",
        "build",
        "ci",
        "chore",
        "revert",
      ],
    ],
  },
};
