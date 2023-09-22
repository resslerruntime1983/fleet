# Git Hooks

This document aims to guide you through the benefits and utilities of implementing Git Hooks in your
development workflow

## Introduction

Git hooks are scripts that Git executes before or after events such as `commit` or `push`.  This
document discusses the benefits of using a `push` hook for Fleet Developers.

## Benefits

### Reduced Waiting Time

Imagine pushing a commit and then realizing that there was a minor issue (ie. `make
lint-go`), forcing you to restart the entire CI process, including tests that can take up to
~30 minutes to
complete

### Streamlined Workflow

Reduce the feedback loop and aid rapid development

### Saving CI Resources

By reducing the number of failed builds, you free up CI resources for other tasks

## Getting Started

1. Copy the `pre-push` to your fleet repo hooks directory

    ```bash
    cp ./git-hooks/backend/setup/pre-push ./.git/hooks/
    chmod +x ./.git/hooks/pre-push
    ```

2. Edit the `pre-push` file and specify the scripts you want to run.  Filenames must match scripts in the
`./git-hooks/backend/hooks/` directory

    ```bash
    declare -a USED_HOOKS=(
    "compile-go"
    "db-schema"
    "lint-go"
    )
    ```

## Contributing Ideas

- Update/Add a script to the `hooks` directory and promote it in Slack!
- Add `./git-hooks/frontend/`
- ??????

