# Contributing to Arrgo

First off, thanks for taking the time to contribute!

The following is a set of guidelines for contributing to Arrgo. These are mostly guidelines, not rules. Use your best judgment, and feel free to propose changes to this document in a pull request.

## Code of Conduct

This project and everyone participating in it is governed by a Code of Conduct. By participating, you are expected to uphold this code. Please report unacceptable behavior to the project maintainers.

## How Can I Contribute?

### Reporting Bugs

This section guides you through submitting a bug report for Arrgo. Following these guidelines helps maintainers and the community understand your report, reproduce the behavior, and find related reports.

- **Use the GitHub Issue Search** &mdash; check if the issue has already been reported.
- **Check if the issue has been fixed** &mdash; try to reproduce it using the latest `main` branch.
- **Use the Bug Report Template** &mdash; create a new issue and select "Bug Report". Fill out the template completely.

### Suggesting Enhancements

This section guides you through submitting an enhancement suggestion for Arrgo, including completely new features and minor improvements to existing functionality.

- **Use the GitHub Issue Search** &mdash; check if the enhancement has already been suggested.
- **Use the Feature Request Template** &mdash; create a new issue and select "Feature Request".

### Pull Requests

1.  **Fork the repo** and create your branch from `main`.
2.  If you've added code that should be tested, add tests.
3.  If you've changed APIs, update the documentation.
4.  Ensure the test suite passes.
5.  Make sure your code lints.
6.  Issue that pull request!

## Styleguides

### Git Commit Messages

-   Use the present tense ("Add feature" not "Added feature")
-   Use the imperative mood ("Move cursor to..." not "Moves cursor to...")
-   Limit the first line to 72 characters or less
-   Reference issues and pull requests liberally after the first line

### Coding Standards

-   **Go**: Follow effective Go standards. Run `go fmt` before committing.
-   **JavaScript/CSS**: Keep it clean and readable.
-   **Docker**: Ensure Dockerfiles/compose files follow best practices.

## Development Setup

1.  Clone your fork.
2.  Copy `.env.example` to `.env` and configure it.
3.  Run `docker-compose up --build` to start the development environment.

Thank you for contributing!
